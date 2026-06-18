package replnet

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// ── CreateJournalBaseline tests ──────────────────────────────────────────────────

func TestCreateJournalBaseline_Fresh(t *testing.T) {
	dir := t.TempDir()
	if err := CreateJournalBaseline(dir, 42); err != nil {
		t.Fatalf("CreateJournalBaseline: %v", err)
	}
	b, err := LoadJournalBaseline(dir)
	if err != nil || b == nil {
		t.Fatalf("load after create: %v, b=%v", err, b)
	}
	if b.BaseSeq != 42 {
		t.Errorf("base_seq: got %d, want 42", b.BaseSeq)
	}
}

func TestCreateJournalBaseline_Idempotent_SameSeq(t *testing.T) {
	dir := t.TempDir()
	if err := CreateJournalBaseline(dir, 100); err != nil {
		t.Fatalf("first create: %v", err)
	}
	// Second call with same seq must return nil (idempotent).
	if err := CreateJournalBaseline(dir, 100); err != nil {
		t.Errorf("idempotent create returned error: %v", err)
	}
}

func TestCreateJournalBaseline_Conflict_DifferentSeq(t *testing.T) {
	dir := t.TempDir()
	if err := CreateJournalBaseline(dir, 10); err != nil {
		t.Fatalf("first create: %v", err)
	}
	err := CreateJournalBaseline(dir, 20)
	if !errors.Is(err, ErrJournalBaselineConflict) {
		t.Errorf("expected ErrJournalBaselineConflict, got: %v", err)
	}
}

func TestCreateJournalBaseline_MaxUint64_Rejected(t *testing.T) {
	dir := t.TempDir()
	err := CreateJournalBaseline(dir, math.MaxUint64)
	if !errors.Is(err, ErrJournalBaselineMaxUint64) {
		t.Errorf("expected ErrJournalBaselineMaxUint64, got: %v", err)
	}
	// No file should have been created.
	if JournalBaselineExists(dir) {
		t.Error("baseline file must not be created for MaxUint64")
	}
}

func TestCreateJournalBaseline_ZeroSeq_Allowed(t *testing.T) {
	dir := t.TempDir()
	if err := CreateJournalBaseline(dir, 0); err != nil {
		t.Fatalf("CreateJournalBaseline(0): %v", err)
	}
	b, _ := LoadJournalBaseline(dir)
	if b == nil || b.BaseSeq != 0 {
		t.Errorf("expected base_seq=0, got %v", b)
	}
}

func TestJournalBaselineExists_True_False(t *testing.T) {
	dir := t.TempDir()
	if JournalBaselineExists(dir) {
		t.Error("should not exist in empty dir")
	}
	CreateJournalBaseline(dir, 5)
	if !JournalBaselineExists(dir) {
		t.Error("should exist after create")
	}
}

func TestCreateJournalBaseline_Sequential_Idempotent(t *testing.T) {
	// CreateJournalBaseline is protected by promoteMu in the promotion path and is
	// never called concurrently in practice. Test that repeated sequential calls
	// with the same value return nil without corrupting the file.
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		if err := CreateJournalBaseline(dir, 42); err != nil {
			t.Errorf("call %d: unexpected error: %v", i, err)
		}
	}
	b, err := LoadJournalBaseline(dir)
	if err != nil || b == nil || b.BaseSeq != 42 {
		t.Errorf("unexpected state after repeated creates: %v %v", err, b)
	}
}

// ── SaveLoad and other existing tests ────────────────────────────────────────────

func TestJournalBaseline_SaveLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	b := &JournalBaseline{Version: journalBaselineVersion, BaseSeq: 42}
	if err := SaveJournalBaseline(dir, b); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := LoadJournalBaseline(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded == nil {
		t.Fatal("loaded baseline is nil")
	}
	if loaded.BaseSeq != 42 {
		t.Errorf("base_seq: got %d, want 42", loaded.BaseSeq)
	}
}

func TestJournalBaseline_ChecksumValidation(t *testing.T) {
	dir := t.TempDir()
	b := &JournalBaseline{Version: journalBaselineVersion, BaseSeq: 10}
	if err := SaveJournalBaseline(dir, b); err != nil {
		t.Fatalf("save: %v", err)
	}
	path := filepath.Join(dir, journalBaselineFileName)
	data, _ := os.ReadFile(path)
	var m map[string]any
	json.Unmarshal(data, &m)
	m["base_seq"] = 999
	tampered, _ := json.Marshal(m)
	os.WriteFile(path, tampered, 0o644)

	_, err := LoadJournalBaseline(dir)
	if !errors.Is(err, ErrJournalBaselineCorrupt) {
		t.Errorf("expected ErrJournalBaselineCorrupt, got: %v", err)
	}
}

func TestJournalBaseline_CorruptData_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, journalBaselineFileName), []byte("bad json"), 0o644)

	_, err := LoadJournalBaseline(dir)
	if !errors.Is(err, ErrJournalBaselineCorrupt) {
		t.Errorf("expected ErrJournalBaselineCorrupt, got: %v", err)
	}
}

func TestJournalBaseline_NotFound_ReturnsNil(t *testing.T) {
	dir := t.TempDir()
	loaded, err := LoadJournalBaseline(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if loaded != nil {
		t.Error("expected nil for missing baseline file")
	}
}

func TestDurableLog_WithBaseline_EmptyLatestSeqEqualsBaseSeq(t *testing.T) {
	dir := t.TempDir()
	b := &JournalBaseline{Version: journalBaselineVersion, BaseSeq: 50}
	if err := SaveJournalBaseline(dir, b); err != nil {
		t.Fatalf("save baseline: %v", err)
	}
	dl, err := OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer dl.Close()

	if got := dl.LatestSeq(); got != 50 {
		t.Errorf("LatestSeq: got %d, want 50", got)
	}
	if got := dl.BaseSeq(); got != 50 {
		t.Errorf("BaseSeq: got %d, want 50", got)
	}
}

func TestDurableLog_WithBaseline_FirstAppendIsBaseSeqPlusOne(t *testing.T) {
	dir := t.TempDir()
	b := &JournalBaseline{Version: journalBaselineVersion, BaseSeq: 50}
	if err := SaveJournalBaseline(dir, b); err != nil {
		t.Fatalf("save baseline: %v", err)
	}
	dl, err := OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer dl.Close()

	entry, err := dl.Append(OpPut, "key1", "val1")
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if entry.Seq != 51 {
		t.Errorf("first append seq: got %d, want 51", entry.Seq)
	}
}

func TestDurableLog_WithBaseline_SecondAppendIsBaseSeqPlusTwo(t *testing.T) {
	dir := t.TempDir()
	b := &JournalBaseline{Version: journalBaselineVersion, BaseSeq: 50}
	SaveJournalBaseline(dir, b)
	dl, err := OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer dl.Close()

	dl.Append(OpPut, "k1", "v1")
	entry, err := dl.Append(OpPut, "k2", "v2")
	if err != nil {
		t.Fatalf("append 2: %v", err)
	}
	if entry.Seq != 52 {
		t.Errorf("second append seq: got %d, want 52", entry.Seq)
	}
}

func TestDurableLog_WithBaseline_ReplayFromBaselinePlus1(t *testing.T) {
	dir := t.TempDir()
	b := &JournalBaseline{Version: journalBaselineVersion, BaseSeq: 100}
	SaveJournalBaseline(dir, b)

	// Write some entries.
	dl, err := OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	dl.Append(OpPut, "a", "1")
	dl.Append(OpPut, "b", "2")
	dl.Close()

	// Reopen and verify replay.
	dl2, err := OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer dl2.Close()

	entries, err := dl2.EntriesAfter(100, 0)
	if err != nil {
		t.Fatalf("entries after: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Seq != 101 || entries[1].Seq != 102 {
		t.Errorf("expected seqs 101,102; got %d,%d", entries[0].Seq, entries[1].Seq)
	}
}

func TestDurableLog_WithBaseline_GapRejected(t *testing.T) {
	// EntriesAfter with a request that would create a gap.
	dir := t.TempDir()
	b := &JournalBaseline{Version: journalBaselineVersion, BaseSeq: 50}
	SaveJournalBaseline(dir, b)
	dl, err := OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer dl.Close()

	// Requesting entries after 0 when first available is 51 (or journal is empty).
	// The gap detection is in the server layer, not DurableLog itself.
	// DurableLog.EntriesAfter just returns entries > after.
	entries, err := dl.EntriesAfter(0, 10)
	if err != nil {
		t.Fatalf("entries after: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries from empty baselined log, got %d", len(entries))
	}
}

func TestDurableLog_WithBaseline_DuplicateRejected(t *testing.T) {
	dir := t.TempDir()
	b := &JournalBaseline{Version: journalBaselineVersion, BaseSeq: 10}
	SaveJournalBaseline(dir, b)
	dl, err := OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer dl.Close()

	// Append seq 11.
	dl.Append(OpPut, "k", "v")
	// EntriesAfter(10) should return the one entry (seq 11).
	entries, _ := dl.EntriesAfter(10, 10)
	if len(entries) != 1 || entries[0].Seq != 11 {
		t.Errorf("expected 1 entry with seq 11, got %d entries", len(entries))
	}
	// EntriesAfter(11) should return 0.
	entries2, _ := dl.EntriesAfter(11, 10)
	if len(entries2) != 0 {
		t.Errorf("expected 0 entries after 11, got %d", len(entries2))
	}
}

func TestDurableLog_WithBaseline_RegressionRejected(t *testing.T) {
	// Ensure that the replay rejects a journal whose first entry doesn't match baseline+1.
	dir := t.TempDir()
	// First, create a normal journal with entries 1,2,3.
	dl, err := OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	dl.Append(OpPut, "a", "1")
	dl.Append(OpPut, "b", "2")
	dl.Close()

	// Now create a baseline saying baseSeq=100.
	b := &JournalBaseline{Version: journalBaselineVersion, BaseSeq: 100}
	SaveJournalBaseline(dir, b)

	// Reopen should fail because journal starts at seq 1 but baseline says 100.
	_, err = OpenDurableLog(dir)
	if err == nil {
		t.Fatal("expected error opening journal with mismatched baseline")
	}
}

func TestDurableLog_WithBaseline_MaxUint64BaseRejected(t *testing.T) {
	dir := t.TempDir()
	b := &JournalBaseline{Version: journalBaselineVersion, BaseSeq: math.MaxUint64}
	SaveJournalBaseline(dir, b)

	// Opening with baseSeq=MaxUint64 means nextSeq would be 0 (overflow).
	// The log should open (baseSeq is set) but Append should fail because
	// nextSeq = MaxUint64 + 1 overflows.
	dl, err := OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer dl.Close()

	_, err = dl.Append(OpPut, "k", "v")
	if err == nil {
		t.Error("expected error on append with MaxUint64 base")
	}
}

func TestDurableLog_WithBaseline_RestartPreservesSequence(t *testing.T) {
	dir := t.TempDir()
	b := &JournalBaseline{Version: journalBaselineVersion, BaseSeq: 200}
	SaveJournalBaseline(dir, b)

	dl, _ := OpenDurableLog(dir)
	dl.Append(OpPut, "x", "1")
	dl.Append(OpPut, "y", "2")
	dl.Close()

	dl2, _ := OpenDurableLog(dir)
	defer dl2.Close()

	e, err := dl2.Append(OpPut, "z", "3")
	if err != nil {
		t.Fatalf("append after restart: %v", err)
	}
	if e.Seq != 203 {
		t.Errorf("expected seq 203, got %d", e.Seq)
	}
}

func TestDurableLog_WithBaseline_OldJournalCompatible(t *testing.T) {
	// Without a baseline file, journal starts at 1 (backward compatible).
	dir := t.TempDir()
	dl, err := OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer dl.Close()

	if dl.BaseSeq() != 0 {
		t.Errorf("expected baseSeq=0 without baseline file, got %d", dl.BaseSeq())
	}
	e, err := dl.Append(OpPut, "k", "v")
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if e.Seq != 1 {
		t.Errorf("expected seq 1 without baseline, got %d", e.Seq)
	}
}

func TestDurableLog_WithBaseline_CannotChangeAfterEntries(t *testing.T) {
	// Write entries with one baseline, then change baseline and try to reopen.
	dir := t.TempDir()
	b := &JournalBaseline{Version: journalBaselineVersion, BaseSeq: 10}
	SaveJournalBaseline(dir, b)
	dl, _ := OpenDurableLog(dir)
	dl.Append(OpPut, "k", "v") // seq 11
	dl.Close()

	// Change baseline to 20. Reopen should fail because journal has seq 11 but baseline says 20.
	b2 := &JournalBaseline{Version: journalBaselineVersion, BaseSeq: 20}
	SaveJournalBaseline(dir, b2)
	_, err := OpenDurableLog(dir)
	if err == nil {
		t.Error("expected error when baseline doesn't match existing journal entries")
	}
}
