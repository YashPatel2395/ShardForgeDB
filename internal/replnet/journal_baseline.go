package replnet

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"math"
	"os"
	"path/filepath"
)

const journalBaselineVersion = 1
const journalBaselineFileName = "journal_baseline.json"

// ErrJournalBaselineCorrupt is returned when the journal baseline file is invalid.
var ErrJournalBaselineCorrupt = errors.New("replnet: journal baseline is corrupt or invalid")

// ErrJournalBaselineConflict is returned by CreateJournalBaseline when a baseline file
// already exists with a different base_seq. The caller must not overwrite it.
var ErrJournalBaselineConflict = errors.New("replnet: journal baseline already exists with different base_seq")

// ErrJournalBaselineMaxUint64 is returned by CreateJournalBaseline when baseSeq is
// math.MaxUint64, which would cause nextSeq to overflow on the first Append.
var ErrJournalBaselineMaxUint64 = errors.New("replnet: journal baseline base_seq is MaxUint64 (would overflow)")

// JournalBaseline records the sequence number that a promoted primary inherits
// from the old primary. The new DurableLog starts numbering from BaseSeq+1.
type JournalBaseline struct {
	Version  int    `json:"version"`
	BaseSeq  uint64 `json:"base_seq"`
	Checksum uint32 `json:"checksum"`
}

// JournalBaselineChecksum computes the IEEE CRC32 checksum.
func JournalBaselineChecksum(b *JournalBaseline) uint32 {
	tmp := *b
	tmp.Checksum = 0
	data, _ := json.Marshal(tmp)
	return crc32.ChecksumIEEE(data)
}

// SaveJournalBaseline atomically persists a JournalBaseline to the data directory.
func SaveJournalBaseline(dir string, b *JournalBaseline) error {
	b.Checksum = JournalBaselineChecksum(b)
	data, err := json.Marshal(b)
	if err != nil {
		return fmt.Errorf("marshal journal baseline: %w", err)
	}
	path := filepath.Join(dir, journalBaselineFileName)
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create journal baseline tmp: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("write journal baseline tmp: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("sync journal baseline tmp: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("close journal baseline tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename journal baseline: %w", err)
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}

// JournalBaselineExists returns true if a journal baseline file exists in dir.
func JournalBaselineExists(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, journalBaselineFileName))
	return err == nil
}

// CreateJournalBaseline creates a journal baseline idempotently.
//   - If no baseline exists, it creates one with the given baseSeq.
//   - If a baseline already exists with the same baseSeq, it returns nil (idempotent).
//   - If a baseline exists with a different baseSeq, it returns ErrJournalBaselineConflict.
//   - If baseSeq == math.MaxUint64, it returns ErrJournalBaselineMaxUint64.
func CreateJournalBaseline(dir string, baseSeq uint64) error {
	if baseSeq == math.MaxUint64 {
		return ErrJournalBaselineMaxUint64
	}
	existing, err := LoadJournalBaseline(dir)
	if err != nil {
		return fmt.Errorf("load existing journal baseline: %w", err)
	}
	if existing != nil {
		if existing.BaseSeq == baseSeq {
			return nil // idempotent: same value
		}
		return fmt.Errorf("%w: existing=%d requested=%d", ErrJournalBaselineConflict, existing.BaseSeq, baseSeq)
	}
	b := &JournalBaseline{
		Version: journalBaselineVersion,
		BaseSeq: baseSeq,
	}
	return SaveJournalBaseline(dir, b)
}

// LoadJournalBaseline reads a JournalBaseline from the data directory.
// Returns (nil, nil) if the file does not exist (no baseline = start at seq 1).
// Returns ErrJournalBaselineCorrupt if the file is invalid.
func LoadJournalBaseline(dir string) (*JournalBaseline, error) {
	path := filepath.Join(dir, journalBaselineFileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil // no baseline file = default behavior
	}
	if err != nil {
		return nil, fmt.Errorf("read journal baseline: %w", err)
	}
	var b JournalBaseline
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrJournalBaselineCorrupt, err)
	}
	if b.Version != journalBaselineVersion {
		return nil, fmt.Errorf("%w: unsupported version %d", ErrJournalBaselineCorrupt, b.Version)
	}
	want := JournalBaselineChecksum(&b)
	if b.Checksum != want {
		return nil, fmt.Errorf("%w: checksum mismatch (got %d, want %d)", ErrJournalBaselineCorrupt, b.Checksum, want)
	}
	return &b, nil
}
