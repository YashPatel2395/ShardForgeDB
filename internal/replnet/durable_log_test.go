package replnet

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDurableLog_AppendAndRetrieve(t *testing.T) {
	dir := t.TempDir()
	dl, err := OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer dl.Close()

	e1, err := dl.Append(OpPut, "a", "1")
	if err != nil {
		t.Fatalf("append put: %v", err)
	}
	if e1.Seq != 1 {
		t.Errorf("seq: want 1, got %d", e1.Seq)
	}

	e2, err := dl.Append(OpDelete, "b", "")
	if err != nil {
		t.Fatalf("append delete: %v", err)
	}
	if e2.Seq != 2 {
		t.Errorf("seq: want 2, got %d", e2.Seq)
	}

	entries, err := dl.EntriesAfter(0, 0)
	if err != nil {
		t.Fatalf("entries after 0: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(entries))
	}
	if entries[0].Op != OpPut || entries[0].Key != "a" || entries[0].Value != "1" {
		t.Errorf("entry[0] mismatch: %+v", entries[0])
	}
	if entries[1].Op != OpDelete || entries[1].Key != "b" {
		t.Errorf("entry[1] mismatch: %+v", entries[1])
	}
}

func TestDurableLog_EntriesAfter_Cursor(t *testing.T) {
	dir := t.TempDir()
	dl, err := OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer dl.Close()

	for i := 0; i < 5; i++ {
		if _, err := dl.Append(OpPut, "k", "v"); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	entries, err := dl.EntriesAfter(3, 0)
	if err != nil {
		t.Fatalf("entries after 3: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2, got %d", len(entries))
	}
	if entries[0].Seq != 4 || entries[1].Seq != 5 {
		t.Errorf("unexpected seqs: %d, %d", entries[0].Seq, entries[1].Seq)
	}
}

func TestDurableLog_EntriesAfter_Limit(t *testing.T) {
	dir := t.TempDir()
	dl, err := OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer dl.Close()

	for i := 0; i < 10; i++ {
		if _, err := dl.Append(OpPut, "k", "v"); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	entries, err := dl.EntriesAfter(0, 3)
	if err != nil {
		t.Fatalf("entries after 0 limit 3: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("want 3, got %d", len(entries))
	}
}

func TestDurableLog_SurvivesRestart(t *testing.T) {
	dir := t.TempDir()

	// Write 3 entries.
	dl1, err := OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("open 1: %v", err)
	}
	if _, err := dl1.Append(OpPut, "x", "hello"); err != nil {
		t.Fatalf("append: %v", err)
	}
	if _, err := dl1.Append(OpPut, "y", "world"); err != nil {
		t.Fatalf("append: %v", err)
	}
	if _, err := dl1.Append(OpDelete, "x", ""); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := dl1.Close(); err != nil {
		t.Fatalf("close 1: %v", err)
	}

	// Reopen and verify replay.
	dl2, err := OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("open 2 (replay): %v", err)
	}
	defer dl2.Close()

	entries, err := dl2.EntriesAfter(0, 0)
	if err != nil {
		t.Fatalf("entries after replay: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("want 3 entries after restart, got %d", len(entries))
	}
	if entries[0].Key != "x" || entries[0].Value != "hello" {
		t.Errorf("entry[0] mismatch: %+v", entries[0])
	}
	if entries[2].Op != OpDelete || entries[2].Key != "x" {
		t.Errorf("entry[2] mismatch: %+v", entries[2])
	}

	// Verify nextSeq continues correctly.
	e, err := dl2.Append(OpPut, "z", "new")
	if err != nil {
		t.Fatalf("append after restart: %v", err)
	}
	if e.Seq != 4 {
		t.Errorf("next seq after restart: want 4, got %d", e.Seq)
	}
}

func TestDurableLog_FirstAvailableSeq(t *testing.T) {
	dir := t.TempDir()
	dl, err := OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer dl.Close()

	if got := dl.FirstAvailableSeq(); got != 0 {
		t.Errorf("empty: want 0, got %d", got)
	}

	if _, err := dl.Append(OpPut, "k", "v"); err != nil {
		t.Fatalf("append: %v", err)
	}
	if got := dl.FirstAvailableSeq(); got != 1 {
		t.Errorf("after first: want 1, got %d", got)
	}

	if _, err := dl.Append(OpPut, "k2", "v2"); err != nil {
		t.Fatalf("append: %v", err)
	}
	if got := dl.FirstAvailableSeq(); got != 1 {
		t.Errorf("after second: still want 1, got %d", got)
	}
}

func TestDurableLog_Stats(t *testing.T) {
	dir := t.TempDir()
	dl, err := OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer dl.Close()

	st, err := dl.Stats()
	if err != nil {
		t.Fatalf("stats empty: %v", err)
	}
	if st.Count != 0 || st.LastSeq != 0 || !st.Durable {
		t.Errorf("empty stats: %+v", st)
	}

	dl.Append(OpPut, "k", "v")
	dl.Append(OpDelete, "k", "")

	st, err = dl.Stats()
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if st.Count != 2 {
		t.Errorf("count: want 2, got %d", st.Count)
	}
	if st.LastSeq != 2 {
		t.Errorf("last_seq: want 2, got %d", st.LastSeq)
	}
	if st.FirstAvailableSeq != 1 {
		t.Errorf("first_available_seq: want 1, got %d", st.FirstAvailableSeq)
	}
	if !st.Durable {
		t.Error("durable: want true")
	}
	if st.JournalBytes == 0 {
		t.Error("journal_bytes: want > 0")
	}
}

func TestDurableLog_TimestampPreservedOnRestart(t *testing.T) {
	dir := t.TempDir()

	dl1, err := OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	before := time.Now().UTC().Truncate(time.Second)
	e, err := dl1.Append(OpPut, "ts", "val")
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	after := time.Now().UTC().Add(time.Second)
	dl1.Close()

	// Reopen and check timestamp survived.
	dl2, err := OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer dl2.Close()

	entries, _ := dl2.EntriesAfter(0, 0)
	if len(entries) == 0 {
		t.Fatal("no entries after reopen")
	}
	got := entries[0].Timestamp
	if got.Before(before) || got.After(after) {
		t.Errorf("timestamp out of range: got %v, want [%v, %v]", got, before, after)
	}
	_ = e
}

func TestDurableLog_EmptyAfterClose(t *testing.T) {
	dir := t.TempDir()
	dl, err := OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	dl.Close()

	if _, err := dl.Append(OpPut, "k", "v"); !errors.Is(err, ErrClosed) {
		t.Errorf("Append after Close: want ErrClosed, got %v", err)
	}
	if _, err := dl.EntriesAfter(0, 0); !errors.Is(err, ErrClosed) {
		t.Errorf("EntriesAfter after Close: want ErrClosed, got %v", err)
	}
	if _, err := dl.Stats(); !errors.Is(err, ErrClosed) {
		t.Errorf("Stats after Close: want ErrClosed, got %v", err)
	}
}

func TestDurableLog_InvalidOp(t *testing.T) {
	dir := t.TempDir()
	dl, err := OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer dl.Close()

	if _, err := dl.Append("badop", "k", "v"); !errors.Is(err, ErrInvalidEntry) {
		t.Errorf("bad op: want ErrInvalidEntry, got %v", err)
	}
	if _, err := dl.Append(OpPut, "", "v"); !errors.Is(err, ErrInvalidEntry) {
		t.Errorf("empty key: want ErrInvalidEntry, got %v", err)
	}
}

func TestDurableLog_CorruptRecord(t *testing.T) {
	dir := t.TempDir()
	dl, err := OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	dl.Append(OpPut, "k", "v")
	dl.Close()

	// Corrupt the CRC bytes (offset 4..7) of the first record.
	path := filepath.Join(dir, journalFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	if len(data) < 8 {
		t.Fatal("journal too short to corrupt")
	}
	data[4] ^= 0xFF // flip bits in CRC
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write corrupted journal: %v", err)
	}

	_, err = OpenDurableLog(dir)
	if !errors.Is(err, ErrCorruptedJournal) {
		t.Errorf("corrupt record: want ErrCorruptedJournal, got %v", err)
	}
}

func TestDurableLog_PartialTailTruncated(t *testing.T) {
	dir := t.TempDir()
	dl, err := OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	dl.Append(OpPut, "k1", "v1")
	dl.Append(OpPut, "k2", "v2")
	dl.Close()

	// Truncate the file to remove the last few bytes of the second record.
	path := filepath.Join(dir, journalFileName)
	fi, _ := os.Stat(path)
	os.Truncate(path, fi.Size()-3)

	// Should open cleanly (partial record is silently trimmed).
	dl2, err := OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("open after partial tail: %v", err)
	}
	defer dl2.Close()

	entries, err := dl2.EntriesAfter(0, 0)
	if err != nil {
		t.Fatalf("entries: %v", err)
	}
	// Only the first complete record should survive.
	if len(entries) != 1 {
		t.Errorf("want 1 entry (partial tail trimmed), got %d", len(entries))
	}
}

func TestDurableLog_JournalFileCreated(t *testing.T) {
	dir := t.TempDir()
	dl, err := OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	dl.Close()

	if _, err := os.Stat(filepath.Join(dir, journalFileName)); err != nil {
		t.Errorf("journal file not created: %v", err)
	}
}

// ── syncFn injection tests ─────────────────────────────────────────────────────

// TestDurableLog_SyncSuccess_EntryVisible verifies that after a successful Append
// (syncFn returns nil) the entry is visible via EntriesAfter.
func TestDurableLog_SyncSuccess_EntryVisible(t *testing.T) {
	dir := t.TempDir()
	dl, err := OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer dl.Close()

	// Override syncFn with a no-op that always succeeds.
	dl.syncFn = func() error { return nil }

	e, err := dl.Append(OpPut, "key", "val")
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if e.Seq != 1 {
		t.Errorf("seq: want 1, got %d", e.Seq)
	}

	entries, err := dl.EntriesAfter(0, 0)
	if err != nil {
		t.Fatalf("entries after: %v", err)
	}
	if len(entries) != 1 || entries[0].Key != "key" {
		t.Errorf("entries: want [{key}], got %v", entries)
	}
}

// TestDurableLog_SyncFailure_ReturnsError verifies that when syncFn returns an error,
// Append propagates it to the caller.
func TestDurableLog_SyncFailure_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	dl, err := OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer dl.Close()

	syncErr := errors.New("disk full (injected)")
	dl.syncFn = func() error { return syncErr }

	_, err = dl.Append(OpPut, "key", "val")
	if err == nil {
		t.Fatal("Append with failing syncFn: want error, got nil")
	}
	if !errors.Is(err, syncErr) {
		t.Errorf("Append error: want to wrap syncErr, got %v", err)
	}
}

// TestDurableLog_SyncFailure_DoesNotAdvanceNextSeq verifies that when syncFn fails,
// nextSeq is not incremented so the next successful Append reuses the same sequence.
func TestDurableLog_SyncFailure_DoesNotAdvanceNextSeq(t *testing.T) {
	dir := t.TempDir()
	dl, err := OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer dl.Close()

	// Fail the first sync.
	dl.syncFn = func() error { return errors.New("sync fail") }
	if _, err := dl.Append(OpPut, "k", "v"); err == nil {
		t.Fatal("expected error from failing syncFn")
	}

	// Restore good sync; next Append must get seq=1 (not seq=2).
	dl.syncFn = dl.f.Sync
	e, err := dl.Append(OpPut, "k", "v")
	if err != nil {
		t.Fatalf("second append: %v", err)
	}
	if e.Seq != 1 {
		t.Errorf("seq after sync failure: want 1, got %d", e.Seq)
	}
}

// TestDurableLog_SyncFailure_DoesNotAddToIndex verifies that when syncFn fails,
// no entry is added to the in-memory index.
func TestDurableLog_SyncFailure_DoesNotAddToIndex(t *testing.T) {
	dir := t.TempDir()
	dl, err := OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer dl.Close()

	dl.syncFn = func() error { return errors.New("sync fail") }
	dl.Append(OpPut, "k", "v") //nolint:errcheck — intentional failure

	// Index must be empty; EntriesAfter must return nothing.
	entries, err := dl.EntriesAfter(0, 0)
	if err != nil {
		t.Fatalf("entries after failed append: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("index after sync failure: want 0 entries, got %d", len(entries))
	}
}

// TestDurableLog_ReopenAfterSyncedAppend_EntryPreserved verifies that an entry
// written with a successful syncFn survives a process restart.
func TestDurableLog_ReopenAfterSyncedAppend_EntryPreserved(t *testing.T) {
	dir := t.TempDir()

	dl1, err := OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// Fail first append (sync error + truncation).
	failErr := errors.New("transient disk error")
	dl1.syncFn = func() error { return failErr }
	if _, err := dl1.Append(OpPut, "bad", "entry"); err == nil {
		t.Fatal("expected error from failing syncFn")
	}

	// Restore sync; succeed second append.
	dl1.syncFn = dl1.f.Sync
	e, err := dl1.Append(OpPut, "good", "entry")
	if err != nil {
		t.Fatalf("second append: %v", err)
	}
	if e.Seq != 1 {
		t.Errorf("seq: want 1, got %d", e.Seq)
	}
	dl1.Close()

	// Reopen: only the successfully synced entry must be present.
	dl2, err := OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer dl2.Close()

	entries, err := dl2.EntriesAfter(0, 0)
	if err != nil {
		t.Fatalf("entries after reopen: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries after reopen: want 1, got %d", len(entries))
	}
	if entries[0].Key != "good" {
		t.Errorf("entry after reopen: want key 'good', got %q", entries[0].Key)
	}
}
