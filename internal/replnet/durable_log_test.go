package replnet

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ── core append / retrieve tests ───────────────────────────────────────────────

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

// ── replay sequence boundary tests ─────────────────────────────────────────────
//
// These tests craft raw journal records with controlled sequence numbers to verify
// that replay() enforces the invariants stated in its doc comment.

// writeRawJournal writes the given records to a new journal file in dir and returns
// the path. Uses encodeJournalRecord so each record has a valid CRC.
func writeRawJournal(t *testing.T, dir string, records []struct {
	seq uint64
	op  Operation
	key string
}) string {
	t.Helper()
	path := filepath.Join(dir, journalFileName)
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create journal: %v", err)
	}
	for _, r := range records {
		rec, err := encodeJournalRecord(r.seq, r.op, r.key, "val", time.Now().UnixNano())
		if err != nil {
			t.Fatalf("encode seq=%d: %v", r.seq, err)
		}
		if _, err := f.Write(rec); err != nil {
			t.Fatalf("write seq=%d: %v", r.seq, err)
		}
	}
	f.Close()
	return path
}

func TestDurableLog_Replay_FirstSeqZero_Rejected(t *testing.T) {
	dir := t.TempDir()
	writeRawJournal(t, dir, []struct {
		seq uint64
		op  Operation
		key string
	}{{seq: 0, op: OpPut, key: "k"}})

	_, err := OpenDurableLog(dir)
	if !errors.Is(err, ErrCorruptedJournal) {
		t.Errorf("seq 0: want ErrCorruptedJournal, got %v", err)
	}
}

func TestDurableLog_Replay_FirstSeqNotOne_Rejected(t *testing.T) {
	dir := t.TempDir()
	// First record has seq=5 — valid CRC but invalid as first seq.
	writeRawJournal(t, dir, []struct {
		seq uint64
		op  Operation
		key string
	}{{seq: 5, op: OpPut, key: "k"}})

	_, err := OpenDurableLog(dir)
	if !errors.Is(err, ErrCorruptedJournal) {
		t.Errorf("first seq=5: want ErrCorruptedJournal, got %v", err)
	}
}

func TestDurableLog_Replay_DuplicateSeq_Rejected(t *testing.T) {
	dir := t.TempDir()
	// Seqs: 1, 1 — duplicate.
	writeRawJournal(t, dir, []struct {
		seq uint64
		op  Operation
		key string
	}{
		{seq: 1, op: OpPut, key: "k1"},
		{seq: 1, op: OpPut, key: "k2"},
	})

	_, err := OpenDurableLog(dir)
	if !errors.Is(err, ErrCorruptedJournal) {
		t.Errorf("duplicate seq=1: want ErrCorruptedJournal, got %v", err)
	}
}

func TestDurableLog_Replay_RegressedSeq_Rejected(t *testing.T) {
	dir := t.TempDir()
	// Seqs: 1, 2, 1 — regression.
	writeRawJournal(t, dir, []struct {
		seq uint64
		op  Operation
		key string
	}{
		{seq: 1, op: OpPut, key: "k1"},
		{seq: 2, op: OpPut, key: "k2"},
		{seq: 1, op: OpPut, key: "k3"},
	})

	_, err := OpenDurableLog(dir)
	if !errors.Is(err, ErrCorruptedJournal) {
		t.Errorf("regressed seq: want ErrCorruptedJournal, got %v", err)
	}
}

func TestDurableLog_Replay_SkippedSeq_Rejected(t *testing.T) {
	dir := t.TempDir()
	// Seqs: 1, 3 — gap (seq 2 missing).
	writeRawJournal(t, dir, []struct {
		seq uint64
		op  Operation
		key string
	}{
		{seq: 1, op: OpPut, key: "k1"},
		{seq: 3, op: OpPut, key: "k3"},
	})

	_, err := OpenDurableLog(dir)
	if !errors.Is(err, ErrCorruptedJournal) {
		t.Errorf("skipped seq: want ErrCorruptedJournal, got %v", err)
	}
}

func TestDurableLog_Replay_MaxUint64Seq_Rejected(t *testing.T) {
	dir := t.TempDir()
	// A seq == MaxUint64 would cause nextSeq = MaxUint64+1 = 0 (overflow).
	writeRawJournal(t, dir, []struct {
		seq uint64
		op  Operation
		key string
	}{{seq: math.MaxUint64, op: OpPut, key: "k"}})

	_, err := OpenDurableLog(dir)
	if !errors.Is(err, ErrCorruptedJournal) {
		t.Errorf("seq MaxUint64: want ErrCorruptedJournal, got %v", err)
	}
}

func TestDurableLog_Replay_NextSeqNeverWraps(t *testing.T) {
	// Write a journal whose last seq is MaxUint64-1. After replay nextSeq = MaxUint64.
	// Append must refuse rather than wrapping nextSeq to 0.
	dir := t.TempDir()
	// We can't write 2^64-1 sequential records, so we write a single record with
	// seq = MaxUint64-1.  This is a crafted journal; it would never occur from normal
	// Append calls (since the Append guard prevents seq from reaching MaxUint64-1
	// without being caught earlier), but it ensures the replay → Append boundary holds.
	writeRawJournal(t, dir, []struct {
		seq uint64
		op  Operation
		key string
	}{{seq: math.MaxUint64 - 1, op: OpPut, key: "k"}})

	// First record has seq = MaxUint64-1, not 1 — so replay rejects it.
	_, err := OpenDurableLog(dir)
	if err == nil {
		t.Fatal("expected error for first seq = MaxUint64-1 (not 1)")
	}
	if !errors.Is(err, ErrCorruptedJournal) {
		t.Errorf("want ErrCorruptedJournal, got %v", err)
	}
	// The important property: if somehow replay allowed nextSeq = MaxUint64,
	// the Append overflow guard would refuse. Verified by TestDurableLog_AppendOverflowGuard.
}

func TestDurableLog_AppendOverflowGuard(t *testing.T) {
	// Directly set nextSeq = MaxUint64 to verify the guard in Append.
	dir := t.TempDir()
	dl, err := OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer dl.Close()

	// Bypass Append to set nextSeq artificially.
	dl.nextSeq = math.MaxUint64

	_, err = dl.Append(OpPut, "k", "v")
	if err == nil {
		t.Fatal("want overflow error when nextSeq=MaxUint64, got nil")
	}
	// Verify nextSeq was NOT advanced to 0.
	if dl.nextSeq != math.MaxUint64 {
		t.Errorf("nextSeq after overflow guard: want MaxUint64, got %d", dl.nextSeq)
	}
}

// ── syncFn injection tests ─────────────────────────────────────────────────────

// syncOnceFailFn returns a syncFn that fails on the first call and succeeds on all
// subsequent calls. This lets the main-sync fail (triggering rollback) while the
// rollback-sync succeeds (leaving the log in a clean, non-poisoned state).
func syncOnceFailFn(failErr error) func() error {
	calls := 0
	return func() error {
		calls++
		if calls == 1 {
			return failErr
		}
		return nil
	}
}

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

// TestDurableLog_SyncFailure_ReturnsError verifies that when the main syncFn fails
// and the rollback sync succeeds (log not poisoned), Append returns the original
// sync error.
func TestDurableLog_SyncFailure_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	dl, err := OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer dl.Close()

	syncErr := errors.New("disk full (injected)")
	// Fail main sync; succeed rollback sync so log is not poisoned.
	dl.syncFn = syncOnceFailFn(syncErr)

	_, err = dl.Append(OpPut, "key", "val")
	if err == nil {
		t.Fatal("Append with failing syncFn: want error, got nil")
	}
	if !errors.Is(err, syncErr) {
		t.Errorf("Append error: want to wrap syncErr, got %v", err)
	}
}

// TestDurableLog_SyncFailure_DoesNotAdvanceNextSeq verifies that when the main sync
// fails (and rollback succeeds), nextSeq is not incremented, so the next successful
// Append reuses the same sequence number.
func TestDurableLog_SyncFailure_DoesNotAdvanceNextSeq(t *testing.T) {
	dir := t.TempDir()
	dl, err := OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer dl.Close()

	// Fail main sync (call 1); rollback sync succeeds (call 2). Log not poisoned.
	dl.syncFn = syncOnceFailFn(errors.New("sync fail"))
	if _, err := dl.Append(OpPut, "k", "v"); err == nil {
		t.Fatal("expected error from failing syncFn")
	}

	// Restore real sync; next Append must get seq=1 (not seq=2).
	dl.syncFn = dl.f.Sync
	e, err := dl.Append(OpPut, "k", "v")
	if err != nil {
		t.Fatalf("second append: %v", err)
	}
	if e.Seq != 1 {
		t.Errorf("seq after sync failure: want 1, got %d", e.Seq)
	}
}

// TestDurableLog_SyncFailure_DoesNotAddToIndex verifies that when the main sync fails
// (and rollback succeeds), no entry is added to the in-memory index.
func TestDurableLog_SyncFailure_DoesNotAddToIndex(t *testing.T) {
	dir := t.TempDir()
	dl, err := OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer dl.Close()

	// Fail main sync; rollback sync succeeds. Log not poisoned.
	dl.syncFn = syncOnceFailFn(errors.New("sync fail"))
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

// TestDurableLog_ReopenAfterSyncedAppend_EntryPreserved verifies that an entry written
// with a successful sync survives a process restart, even when a prior Append failed
// (main sync failed, rollback sync succeeded → log not poisoned).
func TestDurableLog_ReopenAfterSyncedAppend_EntryPreserved(t *testing.T) {
	dir := t.TempDir()

	dl1, err := OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	// Fail first sync; rollback sync succeeds (log not poisoned).
	dl1.syncFn = syncOnceFailFn(errors.New("transient disk error"))
	if _, err := dl1.Append(OpPut, "bad", "entry"); err == nil {
		t.Fatal("expected error from failing syncFn")
	}

	// Restore real sync; succeed second append.
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

// ── rollback failure / poisoned-log tests ─────────────────────────────────────

// TestDurableLog_RollbackSyncFails_PoisonsLog verifies that when the main sync fails
// AND the rollback sync also fails (because syncFn always fails), the log is marked
// poisoned and subsequent Append calls return ErrPoisonedLog.
func TestDurableLog_RollbackSyncFails_PoisonsLog(t *testing.T) {
	dir := t.TempDir()
	dl, err := OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer dl.Close()

	// syncFn always fails: main sync fails → rollback sync also fails → log poisoned.
	dl.syncFn = func() error { return errors.New("permanent disk error") }

	_, err = dl.Append(OpPut, "k", "v")
	if err == nil {
		t.Fatal("Append with always-failing syncFn: want error, got nil")
	}
	if !errors.Is(err, ErrPoisonedLog) {
		t.Errorf("want ErrPoisonedLog in error chain, got %v", err)
	}

	// Subsequent Append must also return ErrPoisonedLog (no matter the syncFn).
	dl.syncFn = func() error { return nil } // restore so the check is purely on poisoned state
	_, err = dl.Append(OpPut, "k2", "v2")
	if !errors.Is(err, ErrPoisonedLog) {
		t.Errorf("second Append after poison: want ErrPoisonedLog, got %v", err)
	}
}

// TestDurableLog_WriteAndTruncateFail_PoisonsLog verifies that when Write fails and the
// rollback Truncate also fails, the log is marked poisoned.
//
// Mechanism: replace dl.f with a read-only file descriptor pointing at the same path.
//   - f.Seek(0, SeekCurrent) succeeds (seeks are position queries; no write permission needed).
//   - f.Write(...) fails with EBADF / "write: bad file descriptor" on most platforms.
//   - rollback calls f.Truncate(...) which also fails (truncate requires write permission).
//   - The double failure marks the log poisoned and returns ErrPoisonedLog.
func TestDurableLog_WriteAndTruncateFail_PoisonsLog(t *testing.T) {
	dir := t.TempDir()
	dl, err := OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// DO NOT defer dl.Close() — we replace dl.f below.

	// Replace dl.f with a read-only descriptor to the same file.
	// The existing write-capable fd is closed to avoid a leak.
	roPath := dl.path
	dl.f.Close()
	roFD, openErr := os.Open(roPath) // O_RDONLY
	if openErr != nil {
		t.Fatalf("open read-only fd: %v", openErr)
	}
	dl.f = roFD
	// dl.closed is still false, dl.poisoned is still false.

	_, err = dl.Append(OpPut, "k", "v")
	if err == nil {
		t.Fatal("Append on read-only fd: want error, got nil")
	}
	// The log must be poisoned because Write failed and rollback Truncate also failed.
	if !errors.Is(err, ErrPoisonedLog) {
		t.Errorf("want ErrPoisonedLog after write+truncate failure, got %v", err)
	}

	// Future Appends must also be refused.
	_, err = dl.Append(OpPut, "k2", "v2")
	if !errors.Is(err, ErrPoisonedLog) {
		t.Errorf("second Append after poison: want ErrPoisonedLog, got %v", err)
	}
	roFD.Close()
}

// TestDurableLog_PoisonedLog_RejectsFutureAppend verifies that once poisoned, every
// subsequent Append returns ErrPoisonedLog regardless of the input.
func TestDurableLog_PoisonedLog_RejectsFutureAppend(t *testing.T) {
	dir := t.TempDir()
	dl, err := OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer dl.Close()

	// Directly poison the log (simulating the internal state after rollback failure).
	dl.poisoned = true

	for _, op := range []Operation{OpPut, OpDelete} {
		_, err := dl.Append(op, "k", "v")
		if !errors.Is(err, ErrPoisonedLog) {
			t.Errorf("Append(%s) on poisoned log: want ErrPoisonedLog, got %v", op, err)
		}
	}
}

// TestDurableLog_SuccessfulRollback_AllowsSequenceReuse verifies that when the main
// sync fails but rollback succeeds, the sequence number is correctly reused by the next
// successful Append (i.e., no gap is introduced in the journal).
func TestDurableLog_SuccessfulRollback_AllowsSequenceReuse(t *testing.T) {
	dir := t.TempDir()
	dl, err := OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer dl.Close()

	// Write seq=1 successfully.
	if _, err := dl.Append(OpPut, "first", "val"); err != nil {
		t.Fatalf("first append: %v", err)
	}

	// Fail main sync for seq=2; rollback succeeds.
	dl.syncFn = syncOnceFailFn(errors.New("transient"))
	if _, err := dl.Append(OpPut, "second-failed", "val"); err == nil {
		t.Fatal("expected sync failure")
	}

	// Restore real sync. Next Append must be seq=2 (not seq=3).
	dl.syncFn = dl.f.Sync
	e, err := dl.Append(OpPut, "second-retry", "val")
	if err != nil {
		t.Fatalf("retry append: %v", err)
	}
	if e.Seq != 2 {
		t.Errorf("seq after rollback: want 2, got %d", e.Seq)
	}

	// Journal must have exactly seq=1 and seq=2 (no gap, no dup).
	entries, err := dl.EntriesAfter(0, 0)
	if err != nil {
		t.Fatalf("entries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(entries))
	}
	if entries[0].Seq != 1 || entries[1].Seq != 2 {
		t.Errorf("seqs: want [1,2], got [%d,%d]", entries[0].Seq, entries[1].Seq)
	}
	if entries[1].Key != "second-retry" {
		t.Errorf("key: want 'second-retry', got %q", entries[1].Key)
	}
}
