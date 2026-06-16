package replnet

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"time"
)

const quiesceRecordVersion = 1
const quiesceFileName = "quiesce_record.json"

// ErrQuiesceRecordNotFound is returned when no quiesce record exists on disk.
var ErrQuiesceRecordNotFound = errors.New("replnet: quiesce record not found")

// ErrQuiesceRecordCorrupt is returned when the quiesce record is structurally invalid.
var ErrQuiesceRecordCorrupt = errors.New("replnet: quiesce record is corrupt or invalid")

// QuiesceRecord is persisted by a primary node when it enters the write-quiesced state.
// It captures the final sequence number so followers can verify they are fully caught up
// before promotion.
type QuiesceRecord struct {
	Version          int    `json:"version"`
	QuiesceID        string `json:"quiesce_id"`
	PrimaryNodeID    string `json:"primary_node_id"`
	PrimaryBaseURL   string `json:"primary_base_url"`
	PrimaryLatestSeq uint64 `json:"primary_latest_seq"`
	QuiescedAt       string `json:"quiesced_at"` // RFC3339Nano UTC
	Checksum         uint32 `json:"checksum"`
}

// NewQuiesceID generates a cryptographically random 16-hex-character quiesce identifier.
// Returns an error if the OS entropy source is unavailable; callers must abort the
// quiesce rather than proceed with an all-zero or predictable ID.
func NewQuiesceID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("replnet: generate quiesce ID: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// QuiesceChecksum computes the IEEE CRC32 checksum over a QuiesceRecord with Checksum=0.
func QuiesceChecksum(r *QuiesceRecord) uint32 {
	tmp := *r
	tmp.Checksum = 0
	data, _ := json.Marshal(tmp)
	return crc32.ChecksumIEEE(data)
}

// SaveQuiesceRecord atomically persists a QuiesceRecord to the data directory.
// Write protocol: temp file -> fsync -> rename -> dir-sync (best-effort).
func SaveQuiesceRecord(dir string, r *QuiesceRecord) error {
	r.Checksum = QuiesceChecksum(r)
	data, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("marshal quiesce record: %w", err)
	}
	path := filepath.Join(dir, quiesceFileName)
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create quiesce tmp: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("write quiesce tmp: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("sync quiesce tmp: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("close quiesce tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename quiesce record: %w", err)
	}
	// Best-effort directory sync (durable on Linux, no-op on macOS HFS+).
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}

// LoadQuiesceRecord reads and validates a QuiesceRecord from the data directory.
// Returns ErrQuiesceRecordNotFound if the file does not exist.
// Returns ErrQuiesceRecordCorrupt if the file is invalid.
func LoadQuiesceRecord(dir string) (*QuiesceRecord, error) {
	path := filepath.Join(dir, quiesceFileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrQuiesceRecordNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read quiesce record: %w", err)
	}
	var r QuiesceRecord
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrQuiesceRecordCorrupt, err)
	}
	if r.Version != quiesceRecordVersion {
		return nil, fmt.Errorf("%w: unsupported version %d", ErrQuiesceRecordCorrupt, r.Version)
	}
	want := QuiesceChecksum(&r)
	if r.Checksum != want {
		return nil, fmt.Errorf("%w: checksum mismatch (got %d, want %d)", ErrQuiesceRecordCorrupt, r.Checksum, want)
	}
	return &r, nil
}

// QuiesceRecordExists returns true if a quiesce record file exists in dir.
func QuiesceRecordExists(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, quiesceFileName))
	return err == nil
}

// NewQuiesceRecord creates a new QuiesceRecord with a random quiesce ID and UTC timestamp.
// Returns an error if the OS entropy source is unavailable; callers must not persist a
// record with a predictable ID.
func NewQuiesceRecord(primaryNodeID, primaryBaseURL string, latestSeq uint64) (*QuiesceRecord, error) {
	qID, err := NewQuiesceID()
	if err != nil {
		return nil, err
	}
	return &QuiesceRecord{
		Version:          quiesceRecordVersion,
		QuiesceID:        qID,
		PrimaryNodeID:    primaryNodeID,
		PrimaryBaseURL:   primaryBaseURL,
		PrimaryLatestSeq: latestSeq,
		QuiescedAt:       time.Now().UTC().Format(time.RFC3339Nano),
	}, nil
}
