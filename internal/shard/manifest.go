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
func validateManifest(m *shardingManifest) error {
	if m.Version != manifestVersion {
		return fmt.Errorf("%w: unsupported version %d", ErrCorruptManifest, m.Version)
	}
	if m.Hash != hashAlgorithm {
		return fmt.Errorf("%w: unknown hash algorithm %q", ErrCorruptManifest, m.Hash)
	}
	seenIDs := make(map[int]bool, len(m.Shards))
	seenNames := make(map[string]bool, len(m.Shards))
	for _, s := range m.Shards {
		if seenIDs[s.ID] {
			return fmt.Errorf("%w: duplicate shard ID %d", ErrCorruptManifest, s.ID)
		}
		seenIDs[s.ID] = true
		if seenNames[s.Name] {
			return fmt.Errorf("%w: duplicate shard name %q", ErrCorruptManifest, s.Name)
		}
		seenNames[s.Name] = true
		if filepath.IsAbs(s.Path) {
			return fmt.Errorf("%w: absolute shard path %q", ErrCorruptManifest, s.Path)
		}
		cleaned := filepath.Clean(s.Path)
		if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
			return fmt.Errorf("%w: path traversal in shard path %q", ErrCorruptManifest, s.Path)
		}
	}
	return nil
}
