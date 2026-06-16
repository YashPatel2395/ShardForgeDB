package replnet

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
)

const quiesceIntentVersion = 1
const quiesceIntentFileName = "quiesce_intent.json"

// ErrQuiesceIntentNotFound is returned when no quiesce intent file exists on disk.
var ErrQuiesceIntentNotFound = errors.New("replnet: quiesce intent not found")

// ErrQuiesceIntentCorrupt is returned when the quiesce intent file is structurally invalid.
var ErrQuiesceIntentCorrupt = errors.New("replnet: quiesce intent is corrupt or invalid")

// QuiesceIntentRecord is written BEFORE the write gate is closed. It serves as a crash-safe
// fence: if the node crashes after the intent is persisted but before the final QuiesceRecord
// is committed, startup will detect the intent-only state and restore the write fence rather
// than silently restoring writes.
//
// Ordering contract:
//  1. Generate quiesce ID.
//  2. SaveQuiesceIntent (before closing write gate) — crash-safe fence start.
//  3. writeGate.Quiesce() — drain in-flight writes.
//  4. Read final committed sequence from durable log.
//  5. SaveQuiesceRecord — commit quiesce.
//  6. RemoveQuiesceIntent — cleanup (crash-safe: intent without final → quiesce_failed_fenced on restart).
type QuiesceIntentRecord struct {
	Version        int    `json:"version"`
	QuiesceID      string `json:"quiesce_id"`
	PrimaryNodeID  string `json:"primary_node_id"`
	PrimaryBaseURL string `json:"primary_base_url"`
	IntentAt       string `json:"intent_at"` // RFC3339Nano UTC
	Checksum       uint32 `json:"checksum"`
}

// QuiesceIntentChecksum computes the IEEE CRC32 checksum over a QuiesceIntentRecord with Checksum=0.
func QuiesceIntentChecksum(r *QuiesceIntentRecord) uint32 {
	tmp := *r
	tmp.Checksum = 0
	data, _ := json.Marshal(tmp)
	return crc32.ChecksumIEEE(data)
}

// SaveQuiesceIntent atomically persists a QuiesceIntentRecord to the data directory.
// Write protocol: temp file → fsync → rename → dir-sync (best-effort).
// Must be called BEFORE closing the write gate; see ordering contract above.
func SaveQuiesceIntent(dir string, r *QuiesceIntentRecord) error {
	r.Checksum = QuiesceIntentChecksum(r)
	data, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("marshal quiesce intent: %w", err)
	}
	path := filepath.Join(dir, quiesceIntentFileName)
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create quiesce intent tmp: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("write quiesce intent tmp: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("sync quiesce intent tmp: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("close quiesce intent tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename quiesce intent: %w", err)
	}
	// Best-effort directory sync.
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}

// LoadQuiesceIntent reads and validates a QuiesceIntentRecord from the data directory.
// Returns ErrQuiesceIntentNotFound if the file does not exist.
// Returns ErrQuiesceIntentCorrupt if the file is invalid.
func LoadQuiesceIntent(dir string) (*QuiesceIntentRecord, error) {
	path := filepath.Join(dir, quiesceIntentFileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrQuiesceIntentNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read quiesce intent: %w", err)
	}
	var r QuiesceIntentRecord
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrQuiesceIntentCorrupt, err)
	}
	if r.Version != quiesceIntentVersion {
		return nil, fmt.Errorf("%w: unsupported version %d", ErrQuiesceIntentCorrupt, r.Version)
	}
	want := QuiesceIntentChecksum(&r)
	if r.Checksum != want {
		return nil, fmt.Errorf("%w: checksum mismatch (got %d, want %d)", ErrQuiesceIntentCorrupt, r.Checksum, want)
	}
	return &r, nil
}

// QuiesceIntentExists returns true if a quiesce intent file exists in dir.
func QuiesceIntentExists(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, quiesceIntentFileName))
	return err == nil
}

// RemoveQuiesceIntent removes the quiesce intent file from dir.
// Called after the final QuiesceRecord is successfully persisted.
// A best-effort directory sync follows the removal.
// Returns nil if the file does not exist (idempotent).
func RemoveQuiesceIntent(dir string) error {
	path := filepath.Join(dir, quiesceIntentFileName)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove quiesce intent: %w", err)
	}
	// Best-effort directory sync so removal is visible after a crash.
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}
