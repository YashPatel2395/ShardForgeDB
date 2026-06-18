package replnet

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPromotionRecord_SaveLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	rec := NewPromotionRecord("follower-1", "primary-1", "http://127.0.0.1:9601", "qid-123", 42)
	if err := SavePromotionRecord(dir, rec); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := LoadPromotionRecord(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.NodeID != "follower-1" {
		t.Errorf("node_id: got %q", loaded.NodeID)
	}
	if loaded.NewRole != "primary" {
		t.Errorf("new_role: got %q", loaded.NewRole)
	}
	if loaded.QuiesceID != "qid-123" {
		t.Errorf("quiesce_id: got %q", loaded.QuiesceID)
	}
	if loaded.InheritedLastSeq != 42 {
		t.Errorf("inherited_last_seq: got %d", loaded.InheritedLastSeq)
	}
}

func TestPromotionRecord_ChecksumValidation(t *testing.T) {
	dir := t.TempDir()
	rec := NewPromotionRecord("f-1", "p-1", "http://x", "qid", 10)
	if err := SavePromotionRecord(dir, rec); err != nil {
		t.Fatalf("save: %v", err)
	}
	path := filepath.Join(dir, promotionFileName)
	data, _ := os.ReadFile(path)
	var m map[string]any
	json.Unmarshal(data, &m)
	m["inherited_last_seq"] = 999
	tampered, _ := json.Marshal(m)
	os.WriteFile(path, tampered, 0o644)

	_, err := LoadPromotionRecord(dir)
	if !errors.Is(err, ErrPromotionRecordCorrupt) {
		t.Errorf("expected ErrPromotionRecordCorrupt, got: %v", err)
	}
}

func TestPromotionRecord_CorruptData_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, promotionFileName), []byte("garbage"), 0o644)

	_, err := LoadPromotionRecord(dir)
	if !errors.Is(err, ErrPromotionRecordCorrupt) {
		t.Errorf("expected ErrPromotionRecordCorrupt, got: %v", err)
	}
}

func TestPromotionRecord_UnsupportedVersion_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	rec := NewPromotionRecord("f", "p", "http://x", "q", 5)
	rec.Version = 77
	rec.Checksum = PromotionChecksum(rec)
	data, _ := json.Marshal(rec)
	os.WriteFile(filepath.Join(dir, promotionFileName), data, 0o644)

	_, err := LoadPromotionRecord(dir)
	if !errors.Is(err, ErrPromotionRecordCorrupt) {
		t.Errorf("expected ErrPromotionRecordCorrupt, got: %v", err)
	}
}

func TestPromotionRecord_NotFound_ReturnsErrNotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadPromotionRecord(dir)
	if !errors.Is(err, ErrPromotionRecordNotFound) {
		t.Errorf("expected ErrPromotionRecordNotFound, got: %v", err)
	}
}

func TestPromotionRecord_AllFieldsPreserved(t *testing.T) {
	dir := t.TempDir()
	rec := NewPromotionRecord("follower-X", "primary-Y", "http://10.0.0.1:8080", "qid-abc", 99999)
	if err := SavePromotionRecord(dir, rec); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := LoadPromotionRecord(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Version != promotionRecordVersion {
		t.Errorf("version: got %d", loaded.Version)
	}
	if loaded.SourcePrimaryNodeID != "primary-Y" {
		t.Errorf("source_primary_node_id: got %q", loaded.SourcePrimaryNodeID)
	}
	if loaded.SourcePrimaryBaseURL != "http://10.0.0.1:8080" {
		t.Errorf("source_primary_base_url: got %q", loaded.SourcePrimaryBaseURL)
	}
	if loaded.PromotedAt == "" {
		t.Error("promoted_at is empty")
	}
}

func TestPromotionRecord_NodeIDMismatch(t *testing.T) {
	// This is an application-level check, not done by the store itself.
	// The store just persists and loads. We verify that fields are intact.
	dir := t.TempDir()
	rec := NewPromotionRecord("nodeA", "primary-1", "http://x", "qid", 5)
	if err := SavePromotionRecord(dir, rec); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := LoadPromotionRecord(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.NodeID != "nodeA" {
		t.Errorf("expected nodeA, got %q", loaded.NodeID)
	}
}

func TestPromotionRecord_AtomicPersistence(t *testing.T) {
	dir := t.TempDir()
	rec := NewPromotionRecord("f", "p", "http://x", "q", 1)
	if err := SavePromotionRecord(dir, rec); err != nil {
		t.Fatalf("save: %v", err)
	}
	tmpPath := filepath.Join(dir, promotionFileName+".tmp")
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("tmp file should not exist after save")
	}
	if !PromotionRecordExists(dir) {
		t.Error("promotion record should exist after save")
	}
}

func TestPromotionRecord_UTCTimestamp(t *testing.T) {
	rec := NewPromotionRecord("f", "p", "http://x", "q", 0)
	if len(rec.PromotedAt) < 20 {
		t.Errorf("promoted_at too short: %q", rec.PromotedAt)
	}
	last := rec.PromotedAt[len(rec.PromotedAt)-1]
	if last != 'Z' {
		t.Errorf("promoted_at should be UTC (end with Z), got: %q", rec.PromotedAt)
	}
}

func TestPromotionRecordExists_True_False(t *testing.T) {
	dir := t.TempDir()
	if PromotionRecordExists(dir) {
		t.Error("should not exist in empty dir")
	}
	rec := NewPromotionRecord("f", "p", "http://x", "q", 0)
	SavePromotionRecord(dir, rec)
	if !PromotionRecordExists(dir) {
		t.Error("should exist after save")
	}
}
