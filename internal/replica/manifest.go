package replica

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	manifestFileName     = "REPLICATION.json"
	manifestVersion      = 1
	defaultReplicaPrefix = "replica"
	replogDir            = "replog"
	replogFile           = "log.dat"
	replicasDir          = "replicas"
	appliedFileName      = "APPLIED"
	// commitFileName is the durable file that records the last committed log
	// index. It is written atomically (temp file + rename) on every successful
	// Put/Delete and loaded on Open. If missing, commitIndex is 0.
	commitFileName = "COMMIT"
)

// replicaEntry is one replica record inside the manifest.
type replicaEntry struct {
	ID   int    `json:"id"`
	Role Role   `json:"role"`
	Name string `json:"name"`
	Path string `json:"path"`
}

// replicationManifest is the on-disk configuration stored in REPLICATION.json.
type replicationManifest struct {
	Version       int            `json:"version"`
	ReplicaCount  int            `json:"replica_count"`
	LeaderID      int            `json:"leader_id"`
	ReplicaPrefix string         `json:"replica_prefix"`
	Replicas      []replicaEntry `json:"replicas"`
}

// writeManifest atomically writes m to <dir>/REPLICATION.json.
func writeManifest(dir string, m *replicationManifest) error {
	if err := validateManifest(m); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("%w: marshal: %v", ErrCorruptManifest, err)
	}
	data = append(data, '\n')

	dst := filepath.Join(dir, manifestFileName)
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("replica: write manifest temp: %w", err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("replica: rename manifest: %w", err)
	}
	return nil
}

// readManifest reads and validates the manifest at <dir>/REPLICATION.json.
func readManifest(dir string) (*replicationManifest, error) {
	path := filepath.Join(dir, manifestFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("%w: read: %v", ErrCorruptManifest, err)
	}
	var m replicationManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%w: unmarshal: %v", ErrCorruptManifest, err)
	}
	if err := validateManifest(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

// validateManifest checks structural and semantic invariants.
func validateManifest(m *replicationManifest) error {
	if m.Version != manifestVersion {
		return fmt.Errorf("%w: unsupported version %d", ErrCorruptManifest, m.Version)
	}
	if m.ReplicaCount <= 0 {
		return fmt.Errorf("%w: replica_count must be > 0, got %d", ErrCorruptManifest, m.ReplicaCount)
	}
	if len(m.Replicas) != m.ReplicaCount {
		return fmt.Errorf("%w: replicas list length %d != replica_count %d",
			ErrCorruptManifest, len(m.Replicas), m.ReplicaCount)
	}

	seenIDs := make(map[int]bool, len(m.Replicas))
	seenNames := make(map[string]bool, len(m.Replicas))
	seenPaths := make(map[string]bool, len(m.Replicas))
	leaderCount := 0

	for _, r := range m.Replicas {
		// ID range.
		if r.ID < 0 || r.ID >= m.ReplicaCount {
			return fmt.Errorf("%w: replica ID %d out of range [0, %d)",
				ErrCorruptManifest, r.ID, m.ReplicaCount)
		}
		if seenIDs[r.ID] {
			return fmt.Errorf("%w: duplicate replica ID %d", ErrCorruptManifest, r.ID)
		}
		seenIDs[r.ID] = true

		// Name.
		if r.Name == "" {
			return fmt.Errorf("%w: replica %d has empty name", ErrCorruptManifest, r.ID)
		}
		if seenNames[r.Name] {
			return fmt.Errorf("%w: duplicate replica name %q", ErrCorruptManifest, r.Name)
		}
		seenNames[r.Name] = true

		// Path.
		if r.Path == "" {
			return fmt.Errorf("%w: replica %d has empty path", ErrCorruptManifest, r.ID)
		}
		if filepath.IsAbs(r.Path) {
			return fmt.Errorf("%w: absolute replica path %q", ErrCorruptManifest, r.Path)
		}
		cleaned := filepath.Clean(r.Path)
		if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
			return fmt.Errorf("%w: path traversal in replica path %q", ErrCorruptManifest, r.Path)
		}
		if cleaned != r.Path {
			return fmt.Errorf("%w: replica path %q is not clean (expected %q)",
				ErrCorruptManifest, r.Path, cleaned)
		}
		if seenPaths[r.Path] {
			return fmt.Errorf("%w: duplicate replica path %q", ErrCorruptManifest, r.Path)
		}
		seenPaths[r.Path] = true

		// Role.
		if r.Role == RoleLeader {
			leaderCount++
		}
	}

	// Verify all IDs 0..ReplicaCount-1 are present.
	for i := 0; i < m.ReplicaCount; i++ {
		if !seenIDs[i] {
			return fmt.Errorf("%w: missing replica ID %d", ErrCorruptManifest, i)
		}
	}

	// Exactly one leader required.
	if leaderCount == 0 {
		return fmt.Errorf("%w: no leader replica found", ErrCorruptManifest)
	}
	if leaderCount > 1 {
		return fmt.Errorf("%w: %d leader replicas found (exactly one required)", ErrCorruptManifest, leaderCount)
	}

	// LeaderID must match the declared leader role.
	found := false
	for _, r := range m.Replicas {
		if r.ID == m.LeaderID {
			found = true
			if r.Role != RoleLeader {
				return fmt.Errorf("%w: leader_id %d has role %q, not leader",
					ErrCorruptManifest, m.LeaderID, r.Role)
			}
		}
	}
	if !found {
		return fmt.Errorf("%w: leader_id %d not found in replicas", ErrCorruptManifest, m.LeaderID)
	}

	return nil
}

// pathExists reports whether path exists.
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
