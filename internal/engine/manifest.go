package engine

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// manifestVersion is the current on-disk manifest schema version.
const manifestVersion = 1

// manifestFilename is the canonical MANIFEST path relative to the engine Dir.
const manifestFilename = "MANIFEST.json"

// manifestTmpSuffix is the suffix used for the temp file during atomic writes.
const manifestTmpSuffix = ".tmp"

// tableEntry is one row in the manifest, representing a single flushed SSTable
// and its companion Bloom sidecar.
type tableEntry struct {
	// ID is a monotonically increasing file identifier assigned at flush time.
	ID uint64 `json:"id"`
	// SSTablePath is the SSTable file path relative to the engine Dir.
	SSTablePath string `json:"sstable_path"`
	// BloomPath is the Bloom sidecar file path relative to the engine Dir.
	BloomPath string `json:"bloom_path"`
	// Count is the number of entries (live + tombstones) in the SSTable.
	Count uint64 `json:"count"`
	// MinKey and MaxKey are base64-encoded to support arbitrary binary keys.
	MinKey string `json:"min_key"`
	MaxKey string `json:"max_key"`
}

// manifest is the root JSON object written to MANIFEST.json.
type manifest struct {
	// Version is the schema version. Currently always 1.
	Version int `json:"version"`
	// NextFileID is the next table ID to assign at flush time.
	NextFileID uint64 `json:"next_file_id"`
	// NextSeq is the next sequence number the engine will assign after restart.
	// It is updated whenever the manifest is saved.
	NextSeq uint64 `json:"next_seq"`
	// Tables is the ordered list of SSTables, oldest first.
	Tables []tableEntry `json:"tables"`
}

// newManifest returns a fresh, empty manifest.
func newManifest() *manifest {
	return &manifest{
		Version:    manifestVersion,
		NextFileID: 1,
		NextSeq:    1,
		Tables:     []tableEntry{},
	}
}

// loadManifest reads and parses the manifest from dir. Returns a new manifest
// if no file exists yet, and ErrCorruptManifest for any decode or validation
// error.
func loadManifest(dir string) (*manifest, bool, error) {
	path := filepath.Join(dir, manifestFilename)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return newManifest(), false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("%w: read %s: %v", ErrCorruptManifest, path, err)
	}

	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, false, fmt.Errorf("%w: json decode: %v", ErrCorruptManifest, err)
	}
	if m.Version != manifestVersion {
		return nil, false, fmt.Errorf("%w: unsupported version %d (want %d)",
			ErrManifestVersion, m.Version, manifestVersion)
	}
	if m.NextFileID == 0 {
		return nil, false, fmt.Errorf("%w: NextFileID is zero", ErrCorruptManifest)
	}
	if m.NextSeq == 0 {
		return nil, false, fmt.Errorf("%w: NextSeq is zero", ErrCorruptManifest)
	}
	return &m, true, nil
}

// saveManifest atomically writes m to dir/MANIFEST.json by writing a temp
// file and renaming it. The temp file is synced before rename.
//
// Limitation: the parent directory is not fsynced after rename, which means
// on a crash the rename may be lost. This is a known limitation of this phase.
func saveManifest(dir string, m *manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("engine: manifest encode: %w", err)
	}

	tmpPath := filepath.Join(dir, manifestFilename+manifestTmpSuffix)
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("engine: manifest tmp create: %w", err)
	}

	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("engine: manifest tmp write: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("engine: manifest tmp sync: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("engine: manifest tmp close: %w", err)
	}

	finalPath := filepath.Join(dir, manifestFilename)
	if err := os.Rename(tmpPath, finalPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("engine: manifest rename: %w", err)
	}
	return nil
}

// encodeKey base64-encodes a raw key for safe JSON storage.
func encodeKey(key []byte) string {
	return base64.StdEncoding.EncodeToString(key)
}

// decodeKey decodes a base64-encoded key from the manifest.
func decodeKey(s string) ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("engine: base64 decode key: %w", err)
	}
	return b, nil
}
