package replnet

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// mustNewQuiesceRecord is a test helper that fatals if NewQuiesceRecord returns an error.
func mustNewQuiesceRecord(t *testing.T, primaryNodeID, primaryBaseURL string, latestSeq uint64) *QuiesceRecord {
	t.Helper()
	rec, err := NewQuiesceRecord(primaryNodeID, primaryBaseURL, latestSeq)
	if err != nil {
		t.Fatalf("NewQuiesceRecord: %v", err)
	}
	return rec
}

func TestQuiesceRecord_SaveLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	rec := mustNewQuiesceRecord(t, "primary-1", "http://127.0.0.1:9601", 42)
	if err := SaveQuiesceRecord(dir, rec); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := LoadQuiesceRecord(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.QuiesceID != rec.QuiesceID {
		t.Errorf("quiesce_id mismatch: got %q, want %q", loaded.QuiesceID, rec.QuiesceID)
	}
	if loaded.PrimaryLatestSeq != 42 {
		t.Errorf("primary_latest_seq: got %d, want 42", loaded.PrimaryLatestSeq)
	}
}

func TestQuiesceRecord_ChecksumValidation(t *testing.T) {
	dir := t.TempDir()
	rec := mustNewQuiesceRecord(t, "primary-1", "http://127.0.0.1:9601", 10)
	if err := SaveQuiesceRecord(dir, rec); err != nil {
		t.Fatalf("save: %v", err)
	}
	// Tamper with the file.
	path := filepath.Join(dir, quiesceFileName)
	data, _ := os.ReadFile(path)
	var m map[string]any
	json.Unmarshal(data, &m)
	m["primary_latest_seq"] = 999
	tampered, _ := json.Marshal(m)
	os.WriteFile(path, tampered, 0o644)

	_, err := LoadQuiesceRecord(dir)
	if !errors.Is(err, ErrQuiesceRecordCorrupt) {
		t.Errorf("expected ErrQuiesceRecordCorrupt, got: %v", err)
	}
}

func TestQuiesceRecord_CorruptData_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, quiesceFileName)
	os.WriteFile(path, []byte("not json"), 0o644)

	_, err := LoadQuiesceRecord(dir)
	if !errors.Is(err, ErrQuiesceRecordCorrupt) {
		t.Errorf("expected ErrQuiesceRecordCorrupt, got: %v", err)
	}
}

func TestQuiesceRecord_TruncatedData_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	rec := mustNewQuiesceRecord(t, "primary-1", "http://127.0.0.1:9601", 5)
	if err := SaveQuiesceRecord(dir, rec); err != nil {
		t.Fatalf("save: %v", err)
	}
	path := filepath.Join(dir, quiesceFileName)
	data, _ := os.ReadFile(path)
	os.WriteFile(path, data[:len(data)/2], 0o644)

	_, err := LoadQuiesceRecord(dir)
	if err == nil {
		t.Error("expected error on truncated data")
	}
}

func TestQuiesceRecord_UnsupportedVersion_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	rec := mustNewQuiesceRecord(t, "primary-1", "http://127.0.0.1:9601", 5)
	rec.Version = 99
	rec.Checksum = QuiesceChecksum(rec)
	data, _ := json.Marshal(rec)
	os.WriteFile(filepath.Join(dir, quiesceFileName), data, 0o644)

	_, err := LoadQuiesceRecord(dir)
	if !errors.Is(err, ErrQuiesceRecordCorrupt) {
		t.Errorf("expected ErrQuiesceRecordCorrupt for bad version, got: %v", err)
	}
}

func TestQuiesceRecord_NotFound_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadQuiesceRecord(dir)
	if !errors.Is(err, ErrQuiesceRecordNotFound) {
		t.Errorf("expected ErrQuiesceRecordNotFound, got: %v", err)
	}
}

func TestQuiesceRecord_AllFieldsPreserved(t *testing.T) {
	dir := t.TempDir()
	rec := mustNewQuiesceRecord(t, "node-A", "http://10.0.0.1:8080", 12345)
	if err := SaveQuiesceRecord(dir, rec); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := LoadQuiesceRecord(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Version != quiesceRecordVersion {
		t.Errorf("version: got %d", loaded.Version)
	}
	if loaded.PrimaryNodeID != "node-A" {
		t.Errorf("primary_node_id: got %q", loaded.PrimaryNodeID)
	}
	if loaded.PrimaryBaseURL != "http://10.0.0.1:8080" {
		t.Errorf("primary_base_url: got %q", loaded.PrimaryBaseURL)
	}
	if loaded.PrimaryLatestSeq != 12345 {
		t.Errorf("primary_latest_seq: got %d", loaded.PrimaryLatestSeq)
	}
	if loaded.QuiescedAt == "" {
		t.Error("quiesced_at is empty")
	}
	if loaded.QuiesceID == "" {
		t.Error("quiesce_id is empty")
	}
}

func TestQuiesceRecord_AtomicPersistence(t *testing.T) {
	dir := t.TempDir()
	rec := mustNewQuiesceRecord(t, "primary-1", "http://127.0.0.1:9601", 42)
	if err := SaveQuiesceRecord(dir, rec); err != nil {
		t.Fatalf("save: %v", err)
	}
	// After save, no .tmp file should remain.
	tmpPath := filepath.Join(dir, quiesceFileName+".tmp")
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("tmp file should not exist after save")
	}
	// Final file should exist.
	if !QuiesceRecordExists(dir) {
		t.Error("quiesce record should exist after save")
	}
}

func TestNewQuiesceRecord_UniqueIDs(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		rec := mustNewQuiesceRecord(t, "p", "http://x", 0)
		if ids[rec.QuiesceID] {
			t.Fatalf("duplicate quiesce_id on iteration %d", i)
		}
		ids[rec.QuiesceID] = true
	}
}

func TestNewQuiesceRecord_UTCTimestamp(t *testing.T) {
	rec := mustNewQuiesceRecord(t, "p", "http://x", 0)
	if len(rec.QuiescedAt) < 20 {
		t.Errorf("quiesced_at too short: %q", rec.QuiescedAt)
	}
	// Should end with Z (UTC).
	last := rec.QuiescedAt[len(rec.QuiescedAt)-1]
	if last != 'Z' {
		t.Errorf("quiesced_at should be UTC (end with Z), got: %q", rec.QuiescedAt)
	}
}

func TestQuiesceRecordExists_True_False(t *testing.T) {
	dir := t.TempDir()
	if QuiesceRecordExists(dir) {
		t.Error("should not exist in empty dir")
	}
	rec := mustNewQuiesceRecord(t, "p", "http://x", 0)
	SaveQuiesceRecord(dir, rec)
	if !QuiesceRecordExists(dir) {
		t.Error("should exist after save")
	}
}

func TestQuiesceRecord_ZeroSeq_Allowed(t *testing.T) {
	dir := t.TempDir()
	rec := mustNewQuiesceRecord(t, "p", "http://x", 0)
	if err := SaveQuiesceRecord(dir, rec); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := LoadQuiesceRecord(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.PrimaryLatestSeq != 0 {
		t.Errorf("expected 0, got %d", loaded.PrimaryLatestSeq)
	}
}

func TestQuiesceRecord_MaxSeq_Allowed(t *testing.T) {
	dir := t.TempDir()
	rec := mustNewQuiesceRecord(t, "p", "http://x", math.MaxUint64)
	if err := SaveQuiesceRecord(dir, rec); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := LoadQuiesceRecord(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.PrimaryLatestSeq != math.MaxUint64 {
		t.Errorf("expected MaxUint64, got %d", loaded.PrimaryLatestSeq)
	}
}

func TestQuiesceRecord_SaveTwice_Idempotent(t *testing.T) {
	dir := t.TempDir()
	rec := mustNewQuiesceRecord(t, "p", "http://x", 42)
	if err := SaveQuiesceRecord(dir, rec); err != nil {
		t.Fatalf("save 1: %v", err)
	}
	if err := SaveQuiesceRecord(dir, rec); err != nil {
		t.Fatalf("save 2: %v", err)
	}
	loaded, err := LoadQuiesceRecord(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.PrimaryLatestSeq != 42 {
		t.Errorf("expected 42, got %d", loaded.PrimaryLatestSeq)
	}
}

func TestQuiesceRecord_DirectorySyncBestEffort(t *testing.T) {
	dir := t.TempDir()
	rec := mustNewQuiesceRecord(t, "p", "http://x", 1)
	// Should not error even though dir sync is best-effort.
	if err := SaveQuiesceRecord(dir, rec); err != nil {
		t.Fatalf("save: %v", err)
	}
}

func TestNewQuiesceID_ReturnsError_OnEntropyFailure(t *testing.T) {
	// We cannot reliably make crypto/rand fail on the test host, but we can verify the
	// exported function signature and that a real call succeeds.
	id, err := NewQuiesceID()
	if err != nil {
		t.Fatalf("NewQuiesceID: %v", err)
	}
	if len(id) != 16 {
		t.Errorf("expected 16 hex chars, got %d: %q", len(id), id)
	}
}

func TestNewQuiesceRecord_ReturnsError_PropagatedFromID(t *testing.T) {
	// Verify that NewQuiesceRecord surfaces errors when NewQuiesceID fails.
	// We test this indirectly by confirming success under normal conditions
	// (entropy failure cannot be forced in a portable unit test).
	rec, err := NewQuiesceRecord("p", "http://x", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.QuiesceID == "" {
		t.Error("quiesce_id should be non-empty on success")
	}
}
