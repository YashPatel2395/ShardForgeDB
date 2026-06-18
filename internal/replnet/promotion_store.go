package replnet

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"time"
)

const promotionRecordVersion = 1
const promotionFileName = "promotion_record.json"

// ErrPromotionRecordNotFound is returned when no promotion record exists on disk.
var ErrPromotionRecordNotFound = errors.New("replnet: promotion record not found")

// ErrPromotionRecordCorrupt is returned when the promotion record is structurally invalid.
var ErrPromotionRecordCorrupt = errors.New("replnet: promotion record is corrupt or invalid")

// PromotionRecord is persisted by a follower node when it is promoted to primary.
// On restart, the presence of this record overrides the static config role.
type PromotionRecord struct {
	Version              int    `json:"version"`
	NodeID               string `json:"node_id"`
	NewRole              string `json:"new_role"` // always "primary"
	SourcePrimaryNodeID  string `json:"source_primary_node_id"`
	SourcePrimaryBaseURL string `json:"source_primary_base_url"`
	QuiesceID            string `json:"quiesce_id"`
	InheritedLastSeq     uint64 `json:"inherited_last_seq"`
	PromotedAt           string `json:"promoted_at"` // RFC3339Nano UTC
	Checksum             uint32 `json:"checksum"`
}

// PromotionChecksum computes the IEEE CRC32 checksum over a PromotionRecord with Checksum=0.
func PromotionChecksum(r *PromotionRecord) uint32 {
	tmp := *r
	tmp.Checksum = 0
	data, _ := json.Marshal(tmp)
	return crc32.ChecksumIEEE(data)
}

// NewPromotionRecord creates a new PromotionRecord with a UTC timestamp.
func NewPromotionRecord(nodeID, sourcePrimaryNodeID, sourcePrimaryBaseURL, quiesceID string, inheritedLastSeq uint64) *PromotionRecord {
	return &PromotionRecord{
		Version:              promotionRecordVersion,
		NodeID:               nodeID,
		NewRole:              "primary",
		SourcePrimaryNodeID:  sourcePrimaryNodeID,
		SourcePrimaryBaseURL: sourcePrimaryBaseURL,
		QuiesceID:            quiesceID,
		InheritedLastSeq:     inheritedLastSeq,
		PromotedAt:           time.Now().UTC().Format(time.RFC3339Nano),
	}
}

// SavePromotionRecord atomically persists a PromotionRecord to the data directory.
// Write protocol: temp file -> fsync -> rename -> dir-sync (best-effort).
func SavePromotionRecord(dir string, r *PromotionRecord) error {
	r.Checksum = PromotionChecksum(r)
	data, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("marshal promotion record: %w", err)
	}
	path := filepath.Join(dir, promotionFileName)
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create promotion tmp: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("write promotion tmp: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("sync promotion tmp: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("close promotion tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename promotion record: %w", err)
	}
	// Best-effort directory sync.
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}

// LoadPromotionRecord reads and validates a PromotionRecord from the data directory.
// Returns ErrPromotionRecordNotFound if the file does not exist.
// Returns ErrPromotionRecordCorrupt if the file is invalid.
func LoadPromotionRecord(dir string) (*PromotionRecord, error) {
	path := filepath.Join(dir, promotionFileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrPromotionRecordNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read promotion record: %w", err)
	}
	var r PromotionRecord
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPromotionRecordCorrupt, err)
	}
	if r.Version != promotionRecordVersion {
		return nil, fmt.Errorf("%w: unsupported version %d", ErrPromotionRecordCorrupt, r.Version)
	}
	want := PromotionChecksum(&r)
	if r.Checksum != want {
		return nil, fmt.Errorf("%w: checksum mismatch (got %d, want %d)", ErrPromotionRecordCorrupt, r.Checksum, want)
	}
	return &r, nil
}

// PromotionRecordExists returns true if a promotion record file exists in dir.
func PromotionRecordExists(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, promotionFileName))
	return err == nil
}
