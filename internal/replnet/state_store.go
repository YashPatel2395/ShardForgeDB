package replnet

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// stateVersion is the current schema version for the replication state file.
// Increment this constant when the schema changes incompatibly.
const stateVersion = 1

// stateFileName is the name of the JSON file that stores the follower's replication cursor.
const stateFileName = "replication_state.json"

// stateFileTmp is the temp file used for atomic writes.
const stateFileTmp = "replication_state.json.tmp"

// persistedState is the JSON-serialisable form of the follower cursor.
// All fields except Checksum are covered by the checksum.
type persistedState struct {
	Version        int    `json:"version"`
	FollowerNodeID string `json:"follower_node_id"`
	PrimaryURL     string `json:"primary_url"`
	LastAppliedSeq uint64 `json:"last_applied_seq"`
	UpdatedAt      string `json:"updated_at"` // RFC3339 UTC
	Checksum       uint32 `json:"checksum"`
}

// checksumState computes the IEEE CRC32 over all state fields except Checksum.
// The checksum is deterministic: same inputs always produce the same output.
func checksumState(version int, followerNodeID, primaryURL string, seq uint64, updatedAt string) uint32 {
	h := crc32.NewIEEE()
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(version))
	h.Write(buf[:])
	h.Write([]byte(followerNodeID))
	h.Write([]byte(primaryURL))
	binary.LittleEndian.PutUint64(buf[:], seq)
	h.Write(buf[:])
	h.Write([]byte(updatedAt))
	return h.Sum32()
}

// ReplicationStateStore persists a follower's replication cursor (lastAppliedSeq) to disk
// using atomic temp→fsync→rename writes.
//
// The state file is identity-bound: it stores the follower node ID and the primary URL.
// Loading a state file for a different node or primary returns an error.
//
// Scope: follower nodes only. Thread-safe.
type ReplicationStateStore struct {
	mu             sync.Mutex
	dataDir        string
	followerNodeID string
	primaryURL     string
	seq            uint64 // in-memory cache; matches persisted value
}

// NewReplicationStateStore opens (or creates) the replication state for the follower
// in dataDir. followerNodeID and primaryURL are bound to the state file: if the file
// exists with different identity fields, an identity-mismatch error is returned.
//
// If the state file is absent, the cursor starts at 0 (fresh follower).
// A corrupt state file (bad checksum or invalid JSON) returns ErrCorruptedState.
// An unsupported version field returns ErrUnsupportedStateVersion.
// An identity mismatch returns ErrFollowerIdentityMismatch or ErrPrimaryIdentityMismatch.
func NewReplicationStateStore(dataDir, followerNodeID, primaryURL string) (*ReplicationStateStore, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("replnet: state store: mkdir %s: %w", dataDir, err)
	}

	ss := &ReplicationStateStore{
		dataDir:        dataDir,
		followerNodeID: followerNodeID,
		primaryURL:     primaryURL,
	}

	path := filepath.Join(dataDir, stateFileName)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		// Fresh follower: cursor starts at 0.
		return ss, nil
	}
	if err != nil {
		return nil, fmt.Errorf("replnet: state store: read %s: %w", path, err)
	}

	var ps persistedState
	if err := json.Unmarshal(data, &ps); err != nil {
		return nil, fmt.Errorf("%w: json decode %s: %v", ErrCorruptedState, path, err)
	}

	// Version check must come before identity check.
	if ps.Version != stateVersion {
		return nil, fmt.Errorf("%w: file has version %d, node expects %d",
			ErrUnsupportedStateVersion, ps.Version, stateVersion)
	}
	// Identity checks: reject state files created by a different follower or for a different primary.
	// Never silently reset to zero; the operator must diagnose and reseed instead.
	if ps.FollowerNodeID != followerNodeID {
		return nil, fmt.Errorf("%w: state file bound to node %q, this node is %q",
			ErrFollowerIdentityMismatch, ps.FollowerNodeID, followerNodeID)
	}
	if ps.PrimaryURL != primaryURL {
		return nil, fmt.Errorf("%w: state file bound to primary %q, configured primary is %q",
			ErrPrimaryIdentityMismatch, ps.PrimaryURL, primaryURL)
	}

	// Checksum covers all fields except itself.
	expected := checksumState(ps.Version, ps.FollowerNodeID, ps.PrimaryURL, ps.LastAppliedSeq, ps.UpdatedAt)
	if ps.Checksum != expected {
		return nil, fmt.Errorf("%w: checksum mismatch in %s (stored=%d, computed=%d)",
			ErrCorruptedState, path, ps.Checksum, expected)
	}

	ss.seq = ps.LastAppliedSeq
	return ss, nil
}

// LastAppliedSeq returns the current replication cursor.
func (ss *ReplicationStateStore) LastAppliedSeq() uint64 {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.seq
}

// AdvanceTo persists newSeq as the follower's replication cursor and updates the
// in-memory cache.
//
// Monotonicity rules:
//   - newSeq == current seq: no-op (idempotent; safe to call on replay).
//   - newSeq < current seq: returns ErrInvalidSeqRegression (regression is not allowed;
//     the caller has a logic error if this occurs in normal operation).
//   - newSeq > current seq: persists to disk and advances in-memory cursor.
//
// Write protocol: write temp file → fsync temp file → rename (atomic on POSIX) →
// fsync parent directory (no-op on macOS; durable on Linux).
func (ss *ReplicationStateStore) AdvanceTo(newSeq uint64) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	if newSeq < ss.seq {
		return fmt.Errorf("%w: AdvanceTo(%d) but current cursor is %d",
			ErrInvalidSeqRegression, newSeq, ss.seq)
	}
	if newSeq == ss.seq {
		return nil // idempotent: already at this seq
	}

	updatedAt := time.Now().UTC().Format(time.RFC3339Nano)
	ps := persistedState{
		Version:        stateVersion,
		FollowerNodeID: ss.followerNodeID,
		PrimaryURL:     ss.primaryURL,
		LastAppliedSeq: newSeq,
		UpdatedAt:      updatedAt,
	}
	ps.Checksum = checksumState(ps.Version, ps.FollowerNodeID, ps.PrimaryURL, ps.LastAppliedSeq, ps.UpdatedAt)

	data, err := json.Marshal(ps)
	if err != nil {
		return fmt.Errorf("replnet: state store: marshal: %w", err)
	}

	tmpPath := filepath.Join(ss.dataDir, stateFileTmp)
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("replnet: state store: open tmp: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("replnet: state store: write tmp: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("replnet: state store: fsync tmp: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("replnet: state store: close tmp: %w", err)
	}

	finalPath := filepath.Join(ss.dataDir, stateFileName)
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return fmt.Errorf("replnet: state store: rename to %s: %w", finalPath, err)
	}

	// fsync the parent directory to guarantee the rename (dirent update) is durable.
	// On macOS, directory fsync is a no-op (silently succeeds); on Linux it persists
	// the directory entry so the rename survives a power failure.
	if dirFd, err := os.Open(ss.dataDir); err == nil {
		_ = dirFd.Sync() // best-effort; never fatal
		dirFd.Close()
	}

	ss.seq = newSeq
	return nil
}
