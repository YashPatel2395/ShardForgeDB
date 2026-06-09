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

// manifestTmpSuffix is the suffix used for temp files during atomic writes.
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
// if no file exists yet (second return value false), and ErrCorruptManifest for
// any decode or validation error.
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
	if err := validateTableEntries(m.Tables); err != nil {
		return nil, false, err
	}
	return &m, true, nil
}

// validateTableEntries checks every tableEntry for correctness. It is called
// by loadManifest after the top-level fields have been validated.
//
// Validation rules per entry:
//   - ID must be non-zero
//   - IDs must be unique
//   - SSTablePath and BloomPath must be non-empty
//   - both paths must be local (not absolute, no ".." traversal, not empty)
//   - MinKey and MaxKey must be valid base64 strings
//   - if both decoded keys are non-empty, MinKey must be <= MaxKey
//   - Count must be non-zero
func validateTableEntries(tables []tableEntry) error {
	seenIDs := make(map[uint64]bool, len(tables))
	for i, te := range tables {
		if te.ID == 0 {
			return fmt.Errorf("%w: table[%d] has zero ID", ErrCorruptManifest, i)
		}
		if seenIDs[te.ID] {
			return fmt.Errorf("%w: duplicate table ID %d at index %d", ErrCorruptManifest, te.ID, i)
		}
		seenIDs[te.ID] = true

		if te.SSTablePath == "" {
			return fmt.Errorf("%w: table[%d] has empty SSTablePath", ErrCorruptManifest, i)
		}
		if te.BloomPath == "" {
			return fmt.Errorf("%w: table[%d] has empty BloomPath", ErrCorruptManifest, i)
		}

		// filepath.IsLocal returns true for paths that are not absolute, do not
		// contain ".." components, and are not empty. Available since Go 1.20.
		if !filepath.IsLocal(te.SSTablePath) {
			return fmt.Errorf("%w: table[%d] SSTablePath is not a safe relative path: %q",
				ErrCorruptManifest, i, te.SSTablePath)
		}
		if !filepath.IsLocal(te.BloomPath) {
			return fmt.Errorf("%w: table[%d] BloomPath is not a safe relative path: %q",
				ErrCorruptManifest, i, te.BloomPath)
		}

		minKeyBytes, err := base64.StdEncoding.DecodeString(te.MinKey)
		if err != nil {
			return fmt.Errorf("%w: table[%d] MinKey is not valid base64: %v",
				ErrCorruptManifest, i, err)
		}
		maxKeyBytes, err := base64.StdEncoding.DecodeString(te.MaxKey)
		if err != nil {
			return fmt.Errorf("%w: table[%d] MaxKey is not valid base64: %v",
				ErrCorruptManifest, i, err)
		}
		if len(minKeyBytes) > 0 && len(maxKeyBytes) > 0 && string(minKeyBytes) > string(maxKeyBytes) {
			return fmt.Errorf("%w: table[%d] MinKey > MaxKey", ErrCorruptManifest, i)
		}

		if te.Count == 0 {
			return fmt.Errorf("%w: table[%d] has zero Count", ErrCorruptManifest, i)
		}
	}
	return nil
}

// saveManifest atomically writes m to dir/MANIFEST.json.
func saveManifest(dir string, m *manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("engine: manifest encode: %w", err)
	}
	path := filepath.Join(dir, manifestFilename)
	return writeFileAtomic(path, data, 0o600)
}

// writeFileAtomic atomically writes data to path by writing to a temp file in
// the same directory, syncing, closing, and renaming. The temp file is removed
// on any error.
//
// Limitation: the parent directory is not fsynced after the rename, which
// means on a crash the rename may be lost. This is a known limitation of this
// phase.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmpPath := path + manifestTmpSuffix
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("engine: atomic write tmp create %s: %w", path, err)
	}

	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("engine: atomic write tmp write %s: %w", path, err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("engine: atomic write tmp sync %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("engine: atomic write tmp close %s: %w", path, err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("engine: atomic write rename %s: %w", path, err)
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
