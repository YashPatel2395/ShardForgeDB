package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/YashPatel2395/ShardForgeDB/internal/sstable"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// mustCompact calls Compact and fails the test on error.
func mustCompact(t *testing.T, e *Engine) {
	t.Helper()
	if err := e.Compact(); err != nil {
		t.Fatalf("Compact: %v", err)
	}
}

// mustScan returns all live entries or fails. Nil start/end scans everything.
func mustScan(t *testing.T, e *Engine) []Entry {
	t.Helper()
	entries, err := e.Scan(nil, nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	return entries
}

// scanKeys extracts the string keys from a []Entry slice.
func scanKeys(entries []Entry) []string {
	keys := make([]string, len(entries))
	for i, en := range entries {
		keys[i] = string(en.Key)
	}
	return keys
}

// ── Test 1: Compact on empty engine is no-op ──────────────────────────────────

func TestCompact_EmptyEngine_IsNoOp(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	if err := e.Compact(); err != nil {
		t.Fatalf("Compact on empty engine: %v", err)
	}
	s := e.Stats()
	if s.SSTableCount != 0 {
		t.Errorf("SSTableCount = %d, want 0", s.SSTableCount)
	}
	if s.CompactionCount != 0 {
		t.Errorf("CompactionCount = %d, want 0 (was no-op)", s.CompactionCount)
	}
}

// ── Test 2: Compact with zero SSTables is no-op ───────────────────────────────

func TestCompact_ZeroSSTables_IsNoOp(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	// Write to MemTable but do not flush — zero SSTables.
	mustPut(t, e, "k", "v")

	if err := e.Compact(); err != nil {
		t.Fatalf("Compact with zero SSTables: %v", err)
	}
	if n := e.Stats().SSTableCount; n != 0 {
		t.Errorf("SSTableCount = %d, want 0", n)
	}
	// MemTable entry still readable.
	v, ok := mustGet(t, e, "k")
	if !ok || v != "v" {
		t.Errorf("Get after no-op compact: got (%q, %v)", v, ok)
	}
}

// ── Test 3: Compact with one SSTable is no-op ────────────────────────────────

func TestCompact_OneSsTable_IsNoOp(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	mustPut(t, e, "k", "v")
	mustFlush(t, e)

	if err := e.Compact(); err != nil {
		t.Fatalf("Compact with 1 SSTable: %v", err)
	}
	if n := e.Stats().SSTableCount; n != 1 {
		t.Errorf("SSTableCount = %d, want 1 (no-op)", n)
	}
	// Data still readable.
	v, ok := mustGet(t, e, "k")
	if !ok || v != "v" {
		t.Errorf("Get: got (%q, %v)", v, ok)
	}
}

// ── Test 4: Compact merges two SSTables into one ─────────────────────────────

func TestCompact_MergesTwoSSTablesIntoOne(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	mustPut(t, e, "a", "1")
	mustFlush(t, e)
	mustPut(t, e, "b", "2")
	mustFlush(t, e)

	if n := e.Stats().SSTableCount; n != 2 {
		t.Fatalf("expected 2 SSTables before compact, got %d", n)
	}

	mustCompact(t, e)

	if n := e.Stats().SSTableCount; n != 1 {
		t.Errorf("SSTableCount after compact = %d, want 1", n)
	}
	if v, ok := mustGet(t, e, "a"); !ok || v != "1" {
		t.Errorf("key a: got (%q, %v)", v, ok)
	}
	if v, ok := mustGet(t, e, "b"); !ok || v != "2" {
		t.Errorf("key b: got (%q, %v)", v, ok)
	}
}

// ── Test 5: Compact preserves latest value by sequence number ─────────────────

func TestCompact_PreservesLatestValueBySeq(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	// Write "k"="v1" to SSTable 1.
	mustPut(t, e, "k", "v1")
	mustFlush(t, e)
	// Overwrite "k"="v2" to SSTable 2 (higher seq).
	mustPut(t, e, "k", "v2")
	mustFlush(t, e)

	mustCompact(t, e)

	v, ok := mustGet(t, e, "k")
	if !ok || v != "v2" {
		t.Errorf("expected latest value v2, got (%q, %v)", v, ok)
	}
}

// ── Test 6: Compact drops overwritten older values ────────────────────────────

func TestCompact_DropsOverwrittenOlderValues(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	// Write 3 versions of the same key across 3 SSTables.
	for _, v := range []string{"v1", "v2", "v3"} {
		mustPut(t, e, "k", v)
		mustFlush(t, e)
	}

	mustCompact(t, e)

	if n := e.Stats().SSTableCount; n != 1 {
		t.Errorf("SSTableCount = %d, want 1", n)
	}
	if n := e.Stats().LastCompactionOutputEntries; n != 1 {
		t.Errorf("LastCompactionOutputEntries = %d, want 1", n)
	}
	v, ok := mustGet(t, e, "k")
	if !ok || v != "v3" {
		t.Errorf("expected v3, got (%q, %v)", v, ok)
	}
}

// ── Test 7: Compact preserves live keys across many SSTables ──────────────────

func TestCompact_PreservesLiveKeysAcrossManySSTables(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	const n = 5
	for i := 0; i < n; i++ {
		mustPut(t, e, fmt.Sprintf("key-%03d", i), fmt.Sprintf("val-%03d", i))
		mustFlush(t, e)
	}

	mustCompact(t, e)

	if n2 := e.Stats().SSTableCount; n2 != 1 {
		t.Errorf("SSTableCount = %d, want 1", n2)
	}
	for i := 0; i < n; i++ {
		key := fmt.Sprintf("key-%03d", i)
		want := fmt.Sprintf("val-%03d", i)
		v, ok := mustGet(t, e, key)
		if !ok || v != want {
			t.Errorf("key %q: got (%q, %v)", key, v, ok)
		}
	}
}

// ── Test 8: Compact drops tombstones in full compaction ───────────────────────

func TestCompact_DropsTombstonesInFullCompaction(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	mustPut(t, e, "k", "v")
	mustFlush(t, e)
	mustDelete(t, e, "k")
	mustFlush(t, e)

	mustCompact(t, e)

	// Key is gone; tombstone was dropped.
	_, ok, err := e.Get([]byte("k"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ok {
		t.Error("expected key to be absent after compaction dropped tombstone")
	}
}

// ── Test 9: Deleted key remains not found after compaction ────────────────────

func TestCompact_DeletedKeyRemainsAbsent(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	mustPut(t, e, "gone", "v")
	mustPut(t, e, "live", "v")
	mustFlush(t, e)
	mustDelete(t, e, "gone")
	mustFlush(t, e)

	mustCompact(t, e)

	if _, ok, _ := e.Get([]byte("gone")); ok {
		t.Error("deleted key returned true after compaction")
	}
	if v, ok := mustGet(t, e, "live"); !ok || v != "v" {
		t.Errorf("live key: got (%q, %v)", v, ok)
	}
}

// ── Test 10: All-deleted compaction removes all SSTables ─────────────────────

func TestCompact_AllDeleted_RemovesAllSSTables(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	mustPut(t, e, "k1", "v1")
	mustPut(t, e, "k2", "v2")
	mustFlush(t, e)
	mustDelete(t, e, "k1")
	mustDelete(t, e, "k2")
	mustFlush(t, e)

	mustCompact(t, e)

	if n := e.Stats().SSTableCount; n != 0 {
		t.Errorf("SSTableCount = %d, want 0", n)
	}
	if n := e.Stats().LastCompactionOutputEntries; n != 0 {
		t.Errorf("LastCompactionOutputEntries = %d, want 0", n)
	}
}

// ── Test 11: Scan before and after compaction returns same live results ────────

func TestCompact_ScanBeforeAndAfterReturnsSameLiveResults(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	for i := 0; i < 6; i++ {
		mustPut(t, e, fmt.Sprintf("k%d", i), fmt.Sprintf("v%d", i))
	}
	mustFlush(t, e)
	mustDelete(t, e, "k2")
	mustDelete(t, e, "k4")
	mustFlush(t, e)

	before := mustScan(t, e)
	mustCompact(t, e)
	after := mustScan(t, e)

	if len(before) != len(after) {
		t.Errorf("scan length mismatch: before=%d after=%d", len(before), len(after))
	}
	for i := range before {
		if string(before[i].Key) != string(after[i].Key) ||
			string(before[i].Value) != string(after[i].Value) {
			t.Errorf("entry[%d] mismatch: before=(%q,%q) after=(%q,%q)",
				i, before[i].Key, before[i].Value, after[i].Key, after[i].Value)
		}
	}
}

// ── Test 12: Get before and after compaction returns same live values ──────────

func TestCompact_GetBeforeAndAfterReturnsSameValues(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	keys := []string{"a", "b", "c", "d"}
	for _, k := range keys {
		mustPut(t, e, k, "val-"+k)
	}
	mustFlush(t, e)
	mustDelete(t, e, "b")
	mustFlush(t, e)

	before := make(map[string]string)
	for _, k := range keys {
		v, ok, _ := e.Get([]byte(k))
		if ok {
			before[k] = string(v)
		}
	}

	mustCompact(t, e)

	for _, k := range keys {
		v, ok, _ := e.Get([]byte(k))
		bv, bOk := before[k]
		if ok != bOk || (ok && string(v) != bv) {
			t.Errorf("key %q: before=(%q,%v) after=(%q,%v)", k, bv, bOk, v, ok)
		}
	}
}

// ── Test 13: MemTable value overrides compacted SSTable value ─────────────────

func TestCompact_MemTableValueOverridesSSTable(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	// Two SSTables with "k"="sst-val".
	mustPut(t, e, "k", "sst-val")
	mustFlush(t, e)
	mustPut(t, e, "other", "x")
	mustFlush(t, e)

	mustCompact(t, e)

	// Now write a newer value to MemTable only (not flushed).
	mustPut(t, e, "k", "mem-val")

	// MemTable must win over compacted SSTable.
	v, ok := mustGet(t, e, "k")
	if !ok || v != "mem-val" {
		t.Errorf("expected mem-val, got (%q, %v)", v, ok)
	}
}

// ── Test 14: MemTable tombstone overrides compacted SSTable value ─────────────

func TestCompact_MemTableTombstoneOverridesSSTable(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	mustPut(t, e, "k", "v")
	mustFlush(t, e)
	mustPut(t, e, "other", "x")
	mustFlush(t, e)

	mustCompact(t, e) // "k" survives in compacted SSTable

	// Delete from MemTable (not flushed) — MemTable tombstone wins.
	mustDelete(t, e, "k")

	_, ok, err := e.Get([]byte("k"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ok {
		t.Error("expected tombstone in MemTable to hide compacted SSTable value")
	}
}

// ── Test 15: Compaction does not modify WAL ───────────────────────────────────

func TestCompact_DoesNotModifyWAL(t *testing.T) {
	dir := tmpDir(t)
	e := openEngine(t, dir)

	// Put entries that will be in MemTable / WAL only.
	mustPut(t, e, "wal-key", "wal-val")

	// Also create 2 SSTables so compact actually runs.
	mustPut(t, e, "sst1", "v1")
	mustFlush(t, e)
	mustPut(t, e, "sst2", "v2")
	mustFlush(t, e)

	// Compact merges the 2 SSTables; WAL is untouched.
	mustCompact(t, e)

	// Close without flushing MemTable.
	e.Close()

	// Reopen — WAL replay must recover wal-key.
	e2 := openEngine(t, dir)
	defer e2.Close()

	v, ok := mustGet(t, e2, "wal-key")
	if !ok || v != "wal-val" {
		t.Errorf("WAL key not recovered after compact: got (%q, %v)", v, ok)
	}
	// SSTable keys also available.
	if v, ok := mustGet(t, e2, "sst1"); !ok || v != "v1" {
		t.Errorf("sst1: got (%q, %v)", v, ok)
	}
}

// ── Test 16: Restart after compaction loads compacted SSTable ─────────────────

func TestCompact_RestartLoadsCompactedSSTable(t *testing.T) {
	dir := tmpDir(t)
	e := openEngine(t, dir)

	for i := 0; i < 4; i++ {
		mustPut(t, e, fmt.Sprintf("k%d", i), fmt.Sprintf("v%d", i))
		mustFlush(t, e)
	}
	mustCompact(t, e)
	e.Close()

	e2 := openEngine(t, dir)
	defer e2.Close()

	if n := e2.Stats().SSTableCount; n != 1 {
		t.Errorf("SSTableCount after restart = %d, want 1", n)
	}
	for i := 0; i < 4; i++ {
		key := fmt.Sprintf("k%d", i)
		want := fmt.Sprintf("v%d", i)
		v, ok := mustGet(t, e2, key)
		if !ok || v != want {
			t.Errorf("%q: got (%q, %v)", key, v, ok)
		}
	}
}

// ── Test 17: Restart after all-deleted compaction loads zero SSTables ─────────

func TestCompact_RestartAfterAllDeletedLoadsZeroSSTables(t *testing.T) {
	dir := tmpDir(t)
	e := openEngine(t, dir)

	mustPut(t, e, "k", "v")
	mustFlush(t, e)
	mustDelete(t, e, "k")
	mustFlush(t, e)

	mustCompact(t, e)
	e.Close()

	e2 := openEngine(t, dir)
	defer e2.Close()

	if n := e2.Stats().SSTableCount; n != 0 {
		t.Errorf("SSTableCount after restart = %d, want 0", n)
	}
	if _, ok, _ := e2.Get([]byte("k")); ok {
		t.Error("deleted key found after restart post-compaction")
	}
}

// ── Test 18: Old SSTable files are removed after compaction ───────────────────

func TestCompact_OldFilesRemovedAfterCompaction(t *testing.T) {
	dir := tmpDir(t)
	e := openEngine(t, dir)

	mustPut(t, e, "a", "1")
	mustFlush(t, e)
	mustPut(t, e, "b", "2")
	mustFlush(t, e)

	// Record the old SSTable paths before compaction.
	oldPaths := make([]string, len(e.tables))
	for i, th := range e.tables {
		oldPaths[i] = filepath.Join(dir, th.sstPath)
	}

	mustCompact(t, e)
	e.Close()

	// Old SSTable files must be gone.
	for _, p := range oldPaths {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("old SSTable file still exists after compaction: %s", p)
		}
	}
}

// ── Test 19: Bloom sidecar exists for compacted SSTable ───────────────────────

func TestCompact_BloomSidecarExistsForCompactedSSTable(t *testing.T) {
	dir := tmpDir(t)
	e := openEngine(t, dir)

	mustPut(t, e, "a", "1")
	mustFlush(t, e)
	mustPut(t, e, "b", "2")
	mustFlush(t, e)

	mustCompact(t, e)

	if n := e.Stats().SSTableCount; n != 1 {
		t.Fatalf("SSTableCount = %d, want 1", n)
	}

	// The single compacted table handle must have a valid bloom sidecar on disk.
	th := e.tables[0]
	bloomAbs := filepath.Join(dir, th.bloomPath)
	if _, err := os.Stat(bloomAbs); err != nil {
		t.Errorf("Bloom sidecar missing: %v", err)
	}
	e.Close()
}

// ── Test 20: Bloom negative skip works after compaction ───────────────────────

func TestCompact_BloomNegativeSkipWorksAfterCompaction(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	// Insert keys that bracket a missing key so bounds check passes.
	mustPut(t, e, "apple", "1")
	mustFlush(t, e)
	mustPut(t, e, "zebra", "2")
	mustFlush(t, e)

	mustCompact(t, e)

	before := e.Stats().BloomNegativeSkips

	_, ok, _ := e.Get([]byte("missing-middle"))
	if ok {
		t.Error("Get returned true for absent key")
	}

	after := e.Stats().BloomNegativeSkips
	if after <= before {
		t.Errorf("BloomNegativeSkips did not increase: %d -> %d", before, after)
	}
}

// ── Test 21: Manifest contains one table after multi-table compaction ──────────

func TestCompact_ManifestContainsOneTableAfterMultiTableCompaction(t *testing.T) {
	dir := tmpDir(t)
	e := openEngine(t, dir)

	for i := 0; i < 5; i++ {
		mustPut(t, e, fmt.Sprintf("k%d", i), "v")
		mustFlush(t, e)
	}
	mustCompact(t, e)
	e.Close()

	m, _, err := loadManifest(dir)
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}
	if len(m.Tables) != 1 {
		t.Errorf("manifest has %d tables, want 1", len(m.Tables))
	}
}

// ── Test 22: Manifest contains zero tables after all-deleted compaction ────────

func TestCompact_ManifestContainsZeroTablesAfterAllDeletedCompaction(t *testing.T) {
	dir := tmpDir(t)
	e := openEngine(t, dir)

	mustPut(t, e, "k", "v")
	mustFlush(t, e)
	mustDelete(t, e, "k")
	mustFlush(t, e)

	mustCompact(t, e)
	e.Close()

	m, _, err := loadManifest(dir)
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}
	if len(m.Tables) != 0 {
		t.Errorf("manifest has %d tables, want 0", len(m.Tables))
	}
}

// ── Test 23: Manifest paths remain relative after compaction ──────────────────

func TestCompact_ManifestPathsRemainRelativeAfterCompaction(t *testing.T) {
	dir := tmpDir(t)
	e := openEngine(t, dir)

	mustPut(t, e, "a", "1")
	mustFlush(t, e)
	mustPut(t, e, "b", "2")
	mustFlush(t, e)

	mustCompact(t, e)
	e.Close()

	m, _, err := loadManifest(dir)
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}
	for _, te := range m.Tables {
		if filepath.IsAbs(te.SSTablePath) {
			t.Errorf("SSTablePath is absolute: %s", te.SSTablePath)
		}
		if filepath.IsAbs(te.BloomPath) {
			t.Errorf("BloomPath is absolute: %s", te.BloomPath)
		}
	}
}

// ── Test 24: Compact after Close returns ErrClosed ───────────────────────────

func TestCompact_AfterClose_ReturnsErrClosed(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	e.Close()

	err := e.Compact()
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
}

// ── Test 25: Concurrent Put/Get/Scan/Compact is race-safe ─────────────────────

func TestCompact_Concurrent_RaceSafe(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	// Seed two SSTables so Compact actually does something.
	for i := 0; i < 5; i++ {
		mustPut(t, e, fmt.Sprintf("k%d", i), fmt.Sprintf("v%d", i))
	}
	mustFlush(t, e)
	for i := 5; i < 10; i++ {
		mustPut(t, e, fmt.Sprintf("k%d", i), fmt.Sprintf("v%d", i))
	}
	mustFlush(t, e)

	var wg sync.WaitGroup

	// Concurrent writers.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 30; i++ {
			e.Put([]byte(fmt.Sprintf("writer-%d", i)), []byte("v"))
		}
	}()

	// Concurrent readers.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 30; i++ {
			e.Get([]byte(fmt.Sprintf("k%d", i%10)))
		}
	}()

	// Concurrent scanners.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			e.Scan(nil, nil)
		}
	}()

	// Concurrent compactions (one or more will be no-ops after first succeeds).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 5; i++ {
			e.Compact()
		}
	}()

	wg.Wait()
}

// ── Test 26: Compaction preserves sequence numbers of surviving entries ────────

func TestCompact_PreservesSequenceNumbers(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	mustPut(t, e, "a", "1")
	mustFlush(t, e)
	mustPut(t, e, "b", "2")
	mustFlush(t, e)

	// Record seq before compaction via Scan.
	before := mustScan(t, e)
	seqBefore := make(map[string]uint64)
	for _, en := range before {
		seqBefore[string(en.Key)] = en.Seq
	}

	mustCompact(t, e)

	after := mustScan(t, e)
	for _, en := range after {
		if want, ok := seqBefore[string(en.Key)]; ok {
			if en.Seq != want {
				t.Errorf("key %q: seq before=%d after=%d", en.Key, want, en.Seq)
			}
		}
	}
}

// ── Test 27: Compaction uses a new file ID and does not overwrite existing files

func TestCompact_UsesNewFileIDAndDoesNotOverwriteExistingFiles(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	mustPut(t, e, "a", "1") // will be table-000001
	mustFlush(t, e)
	mustPut(t, e, "b", "2") // will be table-000002
	mustFlush(t, e)

	beforeID := e.nextFileID // should be 3

	mustCompact(t, e)

	afterID := e.nextFileID
	if afterID <= beforeID {
		t.Errorf("nextFileID did not advance: before=%d after=%d", beforeID, afterID)
	}
	// Compacted table ID must be >= beforeID (i.e., a new file, not an existing one).
	if len(e.tables) != 1 || e.tables[0].id < beforeID {
		t.Errorf("compacted table ID %d is not a new ID (expected >= %d)", e.tables[0].id, beforeID)
	}
}

// ── Test 28: Corrupt input SSTable causes Compact to return error ─────────────

func TestCompact_CorruptSSTableCausesError(t *testing.T) {
	dir := tmpDir(t)
	e := openEngine(t, dir)

	mustPut(t, e, "a", "1")
	mustFlush(t, e)
	mustPut(t, e, "b", "2")
	mustFlush(t, e)

	// Corrupt the first SSTable's data by overwriting bytes after it's open.
	sstPath := filepath.Join(dir, e.tables[0].sstPath)
	f, err := os.OpenFile(sstPath, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open SSTable for corruption: %v", err)
	}
	// Overwrite bytes 20..40 with zeros — this corrupts a data record's checksum.
	garbage := make([]byte, 20)
	if _, err := f.WriteAt(garbage, 20); err != nil {
		f.Close()
		t.Fatalf("corrupt SSTable: %v", err)
	}
	f.Close()

	err = e.Compact()
	if err == nil {
		t.Fatal("expected Compact to fail on corrupt SSTable, got nil")
	}
	if !errors.Is(err, ErrCompactionFailed) {
		t.Errorf("expected ErrCompactionFailed, got %v", err)
	}
	e.Close()
}

// ── Test 29: Corrupt Bloom sidecar does not block Compact ─────────────────────
//
// Our implementation loads the Bloom filter at Open time and holds it in
// memory. During Compact, we read SSTables directly via Reader.Scan and build
// a fresh Bloom filter from scratch — we never re-read the old sidecar file.
// Therefore, corrupting a sidecar file on disk after a successful Open does not
// affect Compact: compaction proceeds normally and creates a new valid sidecar.

func TestCompact_CorruptBloomSidecarDoesNotBlockCompact(t *testing.T) {
	dir := tmpDir(t)
	e := openEngine(t, dir)

	mustPut(t, e, "a", "1")
	mustFlush(t, e)
	mustPut(t, e, "b", "2")
	mustFlush(t, e)

	// Corrupt the Bloom sidecar of the first table on disk after it is loaded.
	bloomPath := filepath.Join(dir, e.tables[0].bloomPath)
	if err := os.WriteFile(bloomPath, []byte("corrupted!"), 0o600); err != nil {
		t.Fatalf("corrupt bloom sidecar: %v", err)
	}

	// Compact reads SSTables only; the corrupt sidecar is ignored.
	if err := e.Compact(); err != nil {
		t.Fatalf("Compact failed despite corrupt sidecar (unexpected): %v", err)
	}
	if n := e.Stats().SSTableCount; n != 1 {
		t.Errorf("SSTableCount = %d, want 1", n)
	}
	// New sidecar on disk must be valid.
	th := e.tables[0]
	newBloomPath := filepath.Join(dir, th.bloomPath)
	data, err := os.ReadFile(newBloomPath)
	if err != nil || len(data) == 0 {
		t.Errorf("new Bloom sidecar missing or empty: %v", err)
	}
	e.Close()
}

// ── Test 30: Large workload — many flushes, compact, restart, spot-check ──────

func TestCompact_LargeWorkload(t *testing.T) {
	dir := tmpDir(t)
	e := openEngine(t, dir)

	const n = 500
	const flushEvery = 50

	for i := 0; i < n; i++ {
		mustPut(t, e, fmt.Sprintf("key-%06d", i), fmt.Sprintf("val-%06d", i))
		if (i+1)%flushEvery == 0 {
			mustFlush(t, e)
		}
	}

	tablesBefore := e.Stats().SSTableCount
	if tablesBefore < 2 {
		t.Fatalf("expected >=2 SSTables before compact, got %d", tablesBefore)
	}

	mustCompact(t, e)

	if n2 := e.Stats().SSTableCount; n2 != 1 {
		t.Errorf("SSTableCount after compact = %d, want 1", n2)
	}

	e.Close()

	e2 := openEngine(t, dir)
	defer e2.Close()

	if n2 := e2.Stats().SSTableCount; n2 != 1 {
		t.Errorf("SSTableCount after restart = %d, want 1", n2)
	}

	for _, idx := range []int{0, 49, 100, 249, 499} {
		key := fmt.Sprintf("key-%06d", idx)
		want := fmt.Sprintf("val-%06d", idx)
		v, ok := mustGet(t, e2, key)
		if !ok || v != want {
			t.Errorf("key %q: got (%q, %v)", key, v, ok)
		}
	}

	entries, err := e2.Scan([]byte("key-000100"), []byte("key-000200"))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(entries) != 100 {
		t.Errorf("Scan [100,200): got %d entries, want 100", len(entries))
	}
}

// ── Test 31: CompactionCount incremented by each successful Compact ────────────

func TestCompact_StatsCompactionCountIncremented(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	// With only 1 SSTable, compact is a no-op — count stays 0.
	mustPut(t, e, "k", "v")
	mustFlush(t, e)
	mustCompact(t, e)
	if n := e.Stats().CompactionCount; n != 0 {
		t.Errorf("CompactionCount = %d, want 0 after no-op", n)
	}

	// With 2+ SSTables, compact runs — count increments.
	mustPut(t, e, "k2", "v2")
	mustFlush(t, e)
	mustCompact(t, e)
	if n := e.Stats().CompactionCount; n != 1 {
		t.Errorf("CompactionCount = %d, want 1 after actual compaction", n)
	}

	// Another pair of SSTables → second compaction.
	mustPut(t, e, "k3", "v3")
	mustFlush(t, e)
	mustPut(t, e, "k4", "v4")
	mustFlush(t, e)
	mustCompact(t, e)
	if n := e.Stats().CompactionCount; n != 2 {
		t.Errorf("CompactionCount = %d, want 2", n)
	}
}

// ── Test 32: Stats.LastCompactionInputTables reflects number of merged tables ──

func TestCompact_StatsLastCompactionInputTables(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	for i := 0; i < 4; i++ {
		mustPut(t, e, fmt.Sprintf("k%d", i), "v")
		mustFlush(t, e)
	}

	mustCompact(t, e)

	if n := e.Stats().LastCompactionInputTables; n != 4 {
		t.Errorf("LastCompactionInputTables = %d, want 4", n)
	}
}

// ── Test 33: Failed compacted-reader open leaves old state usable ─────────────
//
// If opening the new compacted SSTable reader fails (after writing the SSTable
// and Bloom sidecar but before committing the manifest), Compact must:
//   - return an error wrapping ErrCompactionFailed
//   - leave e.tables unchanged (old readers still work)
//   - leave the manifest unchanged (still lists old two tables)
//   - not increment any compaction stats

func TestCompact_NewReaderOpenFailureLeavesOldStateUsable(t *testing.T) {
	dir := tmpDir(t)
	e := openEngine(t, dir)
	defer e.Close()

	// Create two SSTables.
	mustPut(t, e, "apple", "1")
	mustFlush(t, e)
	mustPut(t, e, "banana", "2")
	mustFlush(t, e)

	// Verify both keys are readable before the failed compact.
	if v, ok := mustGet(t, e, "apple"); !ok || v != "1" {
		t.Fatalf("pre-compact: apple: got (%q, %v)", v, ok)
	}
	if v, ok := mustGet(t, e, "banana"); !ok || v != "2" {
		t.Fatalf("pre-compact: banana: got (%q, %v)", v, ok)
	}

	// Snapshot manifest state before compaction.
	mBefore, _, err := loadManifest(dir)
	if err != nil {
		t.Fatalf("loadManifest before: %v", err)
	}
	if len(mBefore.Tables) != 2 {
		t.Fatalf("expected 2 tables in manifest before compact, got %d", len(mBefore.Tables))
	}

	// Snapshot compaction stats before.
	statsBefore := e.Stats()

	// Install test hook: make the compacted SSTable reader open fail.
	orig := openSSTableReader
	defer func() { openSSTableReader = orig }()
	openSSTableReader = func(path string, opts sstable.Options) (*sstable.Reader, error) {
		return nil, fmt.Errorf("injected open failure")
	}

	// Compact must fail with ErrCompactionFailed.
	compactErr := e.Compact()
	if compactErr == nil {
		t.Fatal("expected Compact to return an error, got nil")
	}
	if !errors.Is(compactErr, ErrCompactionFailed) {
		t.Errorf("expected ErrCompactionFailed, got %v", compactErr)
	}

	// Restore hook; Get and Scan use already-open readers so they are unaffected
	// even while the hook is installed, but restore early for clarity.
	openSSTableReader = orig

	// Old in-memory state must be completely unchanged.
	if n := e.Stats().SSTableCount; n != 2 {
		t.Errorf("SSTableCount = %d after failed compact, want 2", n)
	}
	if v, ok := mustGet(t, e, "apple"); !ok || v != "1" {
		t.Errorf("apple: got (%q, %v) after failed compact", v, ok)
	}
	if v, ok := mustGet(t, e, "banana"); !ok || v != "2" {
		t.Errorf("banana: got (%q, %v) after failed compact", v, ok)
	}

	// Manifest must still list exactly the original two tables with the same IDs.
	mAfter, _, err := loadManifest(dir)
	if err != nil {
		t.Fatalf("loadManifest after: %v", err)
	}
	if len(mAfter.Tables) != 2 {
		t.Errorf("manifest has %d tables after failed compact, want 2", len(mAfter.Tables))
	}
	for i, te := range mAfter.Tables {
		if te.ID != mBefore.Tables[i].ID {
			t.Errorf("manifest table[%d] ID changed: before=%d after=%d",
				i, mBefore.Tables[i].ID, te.ID)
		}
	}

	// Compaction stats must not have been touched.
	statsAfter := e.Stats()
	if statsAfter.CompactionCount != statsBefore.CompactionCount {
		t.Errorf("CompactionCount changed: before=%d after=%d",
			statsBefore.CompactionCount, statsAfter.CompactionCount)
	}
	if statsAfter.LastCompactionInputTables != statsBefore.LastCompactionInputTables {
		t.Errorf("LastCompactionInputTables changed: before=%d after=%d",
			statsBefore.LastCompactionInputTables, statsAfter.LastCompactionInputTables)
	}
}
