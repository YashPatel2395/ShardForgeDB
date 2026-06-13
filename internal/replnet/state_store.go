package replnet

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"sync"
)

// stateFileName is the name of the JSON file that stores the follower's replication cursor.
const stateFileName = "replication_state.json"

// stateFileTmp is the temp file used for atomic writes.
const stateFileTmp = "replication_state.json.tmp"

// persistedState is the JSON-serialisable form of the follower cursor.
type persistedState struct {
	LastAppliedSeq uint64 `json:"last_applied_seq"`
	Checksum       uint32 `json:"checksum"`
}

// checksumSeq computes the IEEE CRC32 of the 8-byte little-endian encoding of seq.
func checksumSeq(seq uint64) uint32 {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], seq)
	return crc32.ChecksumIEEE(b[:])
}

// ReplicationStateStore persists a follower's replication cursor (lastAppliedSeq) to disk
// using atomic temp→fsync→rename writes.
//
// Scope: follower nodes only. Thread-safe.
type ReplicationStateStore struct {
	mu      sync.Mutex
	dataDir string
	seq     uint64 // in-memory cache
}

// NewReplicationStateStore opens (or creates) the replication state for the follower
// in dataDir. If the state file exists and has a valid checksum, its lastAppliedSeq
// is loaded as the initial cursor. If the file is absent, the cursor starts at 0.
// A corrupt state file returns ErrCorruptedState.
func NewReplicationStateStore(dataDir string) (*ReplicationStateStore, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("replnet: state store: mkdir %s: %w", dataDir, err)
	}

	ss := &ReplicationStateStore{dataDir: dataDir}

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
	if ps.Checksum != checksumSeq(ps.LastAppliedSeq) {
		return nil, fmt.Errorf("%w: checksum mismatch in %s", ErrCorruptedState, path)
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
// in-memory cache. newSeq must be >= current seq; regressions are ignored silently
// (idempotent on restart replay).
//
// Write protocol: write temp file → fsync → rename (atomic on POSIX).
func (ss *ReplicationStateStore) AdvanceTo(newSeq uint64) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	if newSeq <= ss.seq {
		return nil // idempotent: already at or past this seq
	}

	ps := persistedState{
		LastAppliedSeq: newSeq,
		Checksum:       checksumSeq(newSeq),
	}
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

	ss.seq = newSeq
	return nil
}
