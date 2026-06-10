package shard

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	manifestFileName    = "SHARDING.json"
	manifestVersion     = 1
	hashAlgorithm       = "fnv64a"
	defaultVirtualNodes = 128
	defaultShardPrefix  = "shard"
)

// shardEntry is one shard record inside the manifest.
type shardEntry struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

// shardingManifest is the on-disk sharding configuration stored in SHARDING.json.
//
// JSON layout:
//
//	{
//	  "version":       1,
//	  "shard_count":   4,
//	  "virtual_nodes": 128,
//	  "hash":          "fnv64a",
//	  "shard_prefix":  "shard",
//	  "shards": [
//	    {"id": 0, "name": "shard-0000", "path": "shards/shard-0000"},
//	    ...
//	  ]
//	}
type shardingManifest struct {
	Version      int          `json:"version"`
	ShardCount   int          `json:"shard_count"`
	VirtualNodes int          `json:"virtual_nodes"`
	Hash         string       `json:"hash"`
	ShardPrefix  string       `json:"shard_prefix"`
	Shards       []shardEntry `json:"shards"`
}

// writeManifest atomically writes m to <dir>/SHARDING.json via a temp file.
// The manifest is validated before writing.
func writeManifest(dir string, m *shardingManifest) error {
	if err := validateManifest(m); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("%w: marshal: %v", ErrCorruptManifest, err)
	}
	// Append newline for clean diffs.
	data = append(data, '\n')

	dst := filepath.Join(dir, manifestFileName)
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("shard: write manifest temp: %w", err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp) // best-effort cleanup
		return fmt.Errorf("shard: rename manifest: %w", err)
	}
	return nil
}

// readManifest reads and validates the manifest at <dir>/SHARDING.json.
// Returns nil, nil if no manifest file exists yet.
func readManifest(dir string) (*shardingManifest, error) {
	path := filepath.Join(dir, manifestFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("%w: read: %v", ErrCorruptManifest, err)
	}
	var m shardingManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%w: unmarshal: %v", ErrCorruptManifest, err)
	}
	if err := validateManifest(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

// validateManifest checks structural and semantic invariants of the manifest.
//
// Checks performed:
//   - version must equal manifestVersion (1)
//   - hash algorithm must be "fnv64a"
//   - shard_count must be > 0
//   - virtual_nodes must be > 0
//   - len(shards) must equal shard_count
//   - each shard ID must be in [0, shard_count)
//   - no duplicate shard IDs
//   - all IDs in [0, shard_count) must be present
//   - no empty shard name
//   - no duplicate shard name
//   - no empty shard path
//   - no absolute shard path
//   - no path traversal (../)
//   - path must be clean (filepath.Clean(path) == path)
//   - no duplicate shard path
func validateManifest(m *shardingManifest) error {
	if m.Version != manifestVersion {
		return fmt.Errorf("%w: unsupported version %d", ErrCorruptManifest, m.Version)
	}
	if m.Hash != hashAlgorithm {
		return fmt.Errorf("%w: unknown hash algorithm %q", ErrCorruptManifest, m.Hash)
	}
	if m.ShardCount <= 0 {
		return fmt.Errorf("%w: shard_count must be > 0, got %d", ErrCorruptManifest, m.ShardCount)
	}
	if m.VirtualNodes <= 0 {
		return fmt.Errorf("%w: virtual_nodes must be > 0, got %d", ErrCorruptManifest, m.VirtualNodes)
	}
	if len(m.Shards) != m.ShardCount {
		return fmt.Errorf("%w: shards list length %d != shard_count %d",
			ErrCorruptManifest, len(m.Shards), m.ShardCount)
	}

	seenIDs := make(map[int]bool, len(m.Shards))
	seenNames := make(map[string]bool, len(m.Shards))
	seenPaths := make(map[string]bool, len(m.Shards))

	for _, s := range m.Shards {
		// ID range check.
		if s.ID < 0 || s.ID >= m.ShardCount {
			return fmt.Errorf("%w: shard ID %d out of range [0, %d)",
				ErrCorruptManifest, s.ID, m.ShardCount)
		}
		if seenIDs[s.ID] {
			return fmt.Errorf("%w: duplicate shard ID %d", ErrCorruptManifest, s.ID)
		}
		seenIDs[s.ID] = true

		// Name checks.
		if s.Name == "" {
			return fmt.Errorf("%w: shard %d has empty name", ErrCorruptManifest, s.ID)
		}
		if seenNames[s.Name] {
			return fmt.Errorf("%w: duplicate shard name %q", ErrCorruptManifest, s.Name)
		}
		seenNames[s.Name] = true

		// Path checks.
		if s.Path == "" {
			return fmt.Errorf("%w: shard %d has empty path", ErrCorruptManifest, s.ID)
		}
		if filepath.IsAbs(s.Path) {
			return fmt.Errorf("%w: absolute shard path %q", ErrCorruptManifest, s.Path)
		}
		cleaned := filepath.Clean(s.Path)
		if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
			return fmt.Errorf("%w: path traversal in shard path %q", ErrCorruptManifest, s.Path)
		}
		if cleaned != s.Path {
			return fmt.Errorf("%w: shard path %q is not clean (expected %q)",
				ErrCorruptManifest, s.Path, cleaned)
		}
		if seenPaths[s.Path] {
			return fmt.Errorf("%w: duplicate shard path %q", ErrCorruptManifest, s.Path)
		}
		seenPaths[s.Path] = true
	}

	// Verify all IDs 0..ShardCount-1 are present (pigeonhole confirms this when
	// combined with the length check and range check above, but explicit is better).
	for i := 0; i < m.ShardCount; i++ {
		if !seenIDs[i] {
			return fmt.Errorf("%w: missing shard ID %d", ErrCorruptManifest, i)
		}
	}

	return nil
}
