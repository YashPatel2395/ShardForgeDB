package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func tmpDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

func openEngine(t *testing.T, dir string) *Engine {
	t.Helper()
	e, err := Open(Options{Dir: dir})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return e
}

func mustPut(t *testing.T, e *Engine, key, value string) {
	t.Helper()
	if err := e.Put([]byte(key), []byte(value)); err != nil {
		t.Fatalf("Put(%q): %v", key, err)
	}
}

func mustDelete(t *testing.T, e *Engine, key string) {
	t.Helper()
	if err := e.Delete([]byte(key)); err != nil {
		t.Fatalf("Delete(%q): %v", key, err)
	}
}

func mustFlush(t *testing.T, e *Engine) {
	t.Helper()
	if err := e.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
}

func mustGet(t *testing.T, e *Engine, key string) (string, bool) {
	t.Helper()
	v, ok, err := e.Get([]byte(key))
	if err != nil {
		t.Fatalf("Get(%q): %v", key, err)
	}
	return string(v), ok
}

// ── Test 1: Open creates directory layout ─────────────────────────────────────

func TestOpen_CreatesDirectoryLayout(t *testing.T) {
	dir := tmpDir(t)
	e := openEngine(t, dir)
	e.Close()

	if _, err := os.Stat(filepath.Join(dir, sstablesDir)); err != nil {
		t.Errorf("sstables/ not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, walFilename)); err != nil {
		t.Errorf("wal.log not created: %v", err)
	}
}

// ── Test 2: Open rejects empty Dir ────────────────────────────────────────────

func TestOpen_RejectsEmptyDir(t *testing.T) {
	_, err := Open(Options{Dir: ""})
	if !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("expected ErrInvalidOptions for empty Dir, got %v", err)
	}
}

// ── Test 3: Put then Get before flush ─────────────────────────────────────────

func TestPut_ThenGet_BeforeFlush(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	mustPut(t, e, "hello", "world")
	v, ok := mustGet(t, e, "hello")
	if !ok {
		t.Fatal("Get returned false for existing key")
	}
	if v != "world" {
		t.Errorf("Get = %q, want %q", v, "world")
	}
}

// ── Test 4: Get missing key returns false ─────────────────────────────────────

func TestGet_MissingKey_ReturnsFalse(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	_, ok := mustGet(t, e, "no-such-key")
	if ok {
		t.Error("Get returned true for missing key")
	}
}

// ── Test 5: Delete hides MemTable value ───────────────────────────────────────

func TestDelete_HidesMemTableValue(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	mustPut(t, e, "k", "v")
	mustDelete(t, e, "k")
	_, ok := mustGet(t, e, "k")
	if ok {
		t.Error("key visible after Delete in MemTable")
	}
}

// ── Test 6: Delete hides flushed SSTable value ────────────────────────────────

func TestDelete_HidesSSTableValue(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	mustPut(t, e, "k", "v")
	mustFlush(t, e)

	// Delete in MemTable should shadow the SSTable value.
	mustDelete(t, e, "k")
	_, ok := mustGet(t, e, "k")
	if ok {
		t.Error("key visible after Delete when value is in SSTable")
	}
}

// ── Test 7: Put after Delete restores key ─────────────────────────────────────

func TestPut_AfterDelete_RestoresKey(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	mustPut(t, e, "k", "v1")
	mustDelete(t, e, "k")
	mustPut(t, e, "k", "v2")
	v, ok := mustGet(t, e, "k")
	if !ok || v != "v2" {
		t.Errorf("Put after Delete: got (%q, %v), want (v2, true)", v, ok)
	}
}

// ── Test 8: Empty key Put rejected ────────────────────────────────────────────

func TestPut_EmptyKeyRejected(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	if err := e.Put(nil, []byte("v")); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("expected ErrInvalidKey for nil key, got %v", err)
	}
	if err := e.Put([]byte{}, []byte("v")); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("expected ErrInvalidKey for empty key, got %v", err)
	}
}

// ── Test 9: Empty key Delete rejected ────────────────────────────────────────

func TestDelete_EmptyKeyRejected(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	if err := e.Delete(nil); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("expected ErrInvalidKey for nil key, got %v", err)
	}
	if err := e.Delete([]byte{}); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("expected ErrInvalidKey for empty key, got %v", err)
	}
}

// ── Test 10: Returned Get value is mutation-safe ──────────────────────────────

func TestGet_ReturnedValueMutationSafe(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	mustPut(t, e, "k", "original")
	v, _, _ := e.Get([]byte("k"))
	copy(v, "XXXXXXXX")

	v2, ok, _ := e.Get([]byte("k"))
	if !ok || string(v2) != "original" {
		t.Errorf("stored value mutated by caller: got %q", v2)
	}
}

// ── Test 11: Scan from MemTable returns sorted live entries ───────────────────

func TestScan_MemTable_SortedLiveEntries(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	mustPut(t, e, "c", "3")
	mustPut(t, e, "a", "1")
	mustPut(t, e, "b", "2")

	entries, err := e.Scan(nil, nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	for i := 1; i < len(entries); i++ {
		if string(entries[i].Key) <= string(entries[i-1].Key) {
			t.Errorf("Scan not sorted at %d", i)
		}
	}
}

// ── Test 12: Scan respects start inclusive ────────────────────────────────────

func TestScan_StartInclusive(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	mustPut(t, e, "a", "1")
	mustPut(t, e, "b", "2")
	mustPut(t, e, "c", "3")

	entries, _ := e.Scan([]byte("b"), nil)
	if len(entries) != 2 {
		t.Fatalf("got %d, want 2", len(entries))
	}
	if string(entries[0].Key) != "b" {
		t.Errorf("first key = %q, want b", entries[0].Key)
	}
}

// ── Test 13: Scan respects end exclusive ──────────────────────────────────────

func TestScan_EndExclusive(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	mustPut(t, e, "a", "1")
	mustPut(t, e, "b", "2")
	mustPut(t, e, "c", "3")

	entries, _ := e.Scan(nil, []byte("c"))
	if len(entries) != 2 {
		t.Fatalf("got %d, want 2", len(entries))
	}
	if string(entries[1].Key) != "b" {
		t.Errorf("last key = %q, want b", entries[1].Key)
	}
}

// ── Test 14: Scan excludes deleted keys ───────────────────────────────────────

func TestScan_ExcludesDeletedKeys(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	mustPut(t, e, "a", "1")
	mustPut(t, e, "b", "2")
	mustDelete(t, e, "b")

	entries, _ := e.Scan(nil, nil)
	if len(entries) != 1 || string(entries[0].Key) != "a" {
		t.Errorf("Scan returned unexpected entries: %v", entries)
	}
}

// ── Test 15: Flush empty MemTable is no-op ────────────────────────────────────

func TestFlush_EmptyMemTable_IsNoOp(t *testing.T) {
	dir := tmpDir(t)
	e := openEngine(t, dir)
	defer e.Close()

	mustFlush(t, e)

	// No SSTable files should exist.
	sstDir := filepath.Join(dir, sstablesDir)
	entries, _ := os.ReadDir(sstDir)
	if len(entries) != 0 {
		t.Errorf("expected no SSTable files, got %d", len(entries))
	}
}

// ── Test 16: Flush creates SSTable file ──────────────────────────────────────

func TestFlush_CreatesSSTableFile(t *testing.T) {
	dir := tmpDir(t)
	e := openEngine(t, dir)
	defer e.Close()

	mustPut(t, e, "k", "v")
	mustFlush(t, e)

	sstPath := filepath.Join(dir, sstablesDir, "table-000001.sst")
	if _, err := os.Stat(sstPath); err != nil {
		t.Errorf("SSTable file not created: %v", err)
	}
}

// ── Test 17: Flush creates Bloom sidecar ─────────────────────────────────────

func TestFlush_CreatesBloomSidecar(t *testing.T) {
	dir := tmpDir(t)
	e := openEngine(t, dir)
	defer e.Close()

	mustPut(t, e, "k", "v")
	mustFlush(t, e)

	bloomPath := filepath.Join(dir, sstablesDir, "table-000001.bloom")
	if _, err := os.Stat(bloomPath); err != nil {
		t.Errorf("Bloom sidecar not created: %v", err)
	}
}

// ── Test 18: Flush updates manifest ──────────────────────────────────────────

func TestFlush_UpdatesManifest(t *testing.T) {
	dir := tmpDir(t)
	e := openEngine(t, dir)
	defer e.Close()

	mustPut(t, e, "k", "v")
	mustFlush(t, e)

	m, _, err := loadManifest(dir)
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}
	if len(m.Tables) != 1 {
		t.Errorf("manifest has %d tables, want 1", len(m.Tables))
	}
	if m.Tables[0].SSTablePath == "" {
		t.Error("manifest SSTablePath empty")
	}
	if m.Tables[0].BloomPath == "" {
		t.Error("manifest BloomPath empty")
	}
}

// ── Test 19: Get after Flush reads from SSTable ───────────────────────────────

func TestGet_AfterFlush_ReadsFromSSTable(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	mustPut(t, e, "k", "v")
	mustFlush(t, e)

	v, ok := mustGet(t, e, "k")
	if !ok || v != "v" {
		t.Errorf("Get after Flush: got (%q, %v)", v, ok)
	}
}

// ── Test 20: Bloom negative skip for missing key after flush ──────────────────

func TestGet_MissingKey_AfterFlush_BloomNegativeSkip(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	// Insert two keys that bracket the missing key so the bounds check passes
	// ("apple" < "missing-key" < "zebra") and only the Bloom filter can skip.
	mustPut(t, e, "apple", "yes")
	mustPut(t, e, "zebra", "yes")
	mustFlush(t, e)

	before := e.Stats().BloomNegativeSkips

	// Query a key in the SSTable's [min,max] range that was never inserted.
	_, ok, _ := e.Get([]byte("missing-key"))
	if ok {
		t.Error("Get returned true for absent key")
	}

	after := e.Stats().BloomNegativeSkips
	if after <= before {
		t.Errorf("BloomNegativeSkips did not increase: before=%d after=%d", before, after)
	}
}

// ── Test 21: Restart after Put without Flush replays WAL ─────────────────────

func TestRestart_AfterPut_ReplaysWAL(t *testing.T) {
	dir := tmpDir(t)

	e1 := openEngine(t, dir)
	mustPut(t, e1, "k", "v")
	e1.Close()

	e2 := openEngine(t, dir)
	defer e2.Close()

	v, ok := mustGet(t, e2, "k")
	if !ok || v != "v" {
		t.Errorf("after restart got (%q, %v), want (v, true)", v, ok)
	}
}

// ── Test 22: Restart after Flush loads SSTable and Bloom ─────────────────────

func TestRestart_AfterFlush_LoadsSSTableAndBloom(t *testing.T) {
	dir := tmpDir(t)

	e1 := openEngine(t, dir)
	mustPut(t, e1, "k", "v")
	mustFlush(t, e1)
	e1.Close()

	e2 := openEngine(t, dir)
	defer e2.Close()

	if e2.Stats().SSTableCount != 1 {
		t.Errorf("SSTableCount = %d, want 1", e2.Stats().SSTableCount)
	}
	v, ok := mustGet(t, e2, "k")
	if !ok || v != "v" {
		t.Errorf("after restart got (%q, %v)", v, ok)
	}
}

// ── Test 23: Restart after Delete without Flush replays tombstone ─────────────

func TestRestart_AfterDelete_ReplaysWAL(t *testing.T) {
	dir := tmpDir(t)

	e1 := openEngine(t, dir)
	mustPut(t, e1, "k", "v")
	mustDelete(t, e1, "k")
	e1.Close()

	e2 := openEngine(t, dir)
	defer e2.Close()

	_, ok := mustGet(t, e2, "k")
	if ok {
		t.Error("deleted key visible after restart WAL replay")
	}
}

// ── Test 24: Restart after Delete and Flush preserves deletion ────────────────

func TestRestart_AfterDeleteAndFlush_PreservesDeletion(t *testing.T) {
	dir := tmpDir(t)

	e1 := openEngine(t, dir)
	mustPut(t, e1, "k", "v")
	mustFlush(t, e1)
	mustDelete(t, e1, "k")
	mustFlush(t, e1)
	e1.Close()

	e2 := openEngine(t, dir)
	defer e2.Close()

	_, ok := mustGet(t, e2, "k")
	if ok {
		t.Error("deleted key visible after flush+restart")
	}
}

// ── Test 25: Newer MemTable value wins over older SSTable value ───────────────

func TestGet_NewerMemTableWinsOverSSTable(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	mustPut(t, e, "k", "old")
	mustFlush(t, e)
	mustPut(t, e, "k", "new")

	v, ok := mustGet(t, e, "k")
	if !ok || v != "new" {
		t.Errorf("got (%q, %v), want (new, true)", v, ok)
	}
}

// ── Test 26: Newer SSTable value wins over older SSTable value ────────────────

func TestGet_NewerSSTableWinsOverOlderSSTable(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	mustPut(t, e, "k", "v1")
	mustFlush(t, e)
	mustPut(t, e, "k", "v2")
	mustFlush(t, e)

	v, ok := mustGet(t, e, "k")
	if !ok || v != "v2" {
		t.Errorf("got (%q, %v), want (v2, true)", v, ok)
	}
}

// ── Test 27: Scan merges MemTable and multiple SSTables by highest Seq ─────────

func TestScan_MergesMultipleSources(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	mustPut(t, e, "a", "1")
	mustPut(t, e, "b", "old-b")
	mustFlush(t, e)

	mustPut(t, e, "b", "new-b")
	mustPut(t, e, "c", "3")
	mustFlush(t, e)

	mustPut(t, e, "d", "4")

	entries, err := e.Scan(nil, nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	// Expected: a=1, b=new-b, c=3, d=4
	if len(entries) != 4 {
		t.Fatalf("got %d entries, want 4", len(entries))
	}
	m := make(map[string]string)
	for _, en := range entries {
		m[string(en.Key)] = string(en.Value)
	}
	if m["b"] != "new-b" {
		t.Errorf("b = %q, want new-b", m["b"])
	}
	// All expected keys present.
	for _, k := range []string{"a", "b", "c", "d"} {
		if _, ok := m[k]; !ok {
			t.Errorf("key %q missing from Scan result", k)
		}
	}
}

// ── Test 28: Tombstone in newer SSTable suppresses older value ─────────────────

func TestScan_TombstoneInNewerSSTableSuppressesOlder(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	mustPut(t, e, "k", "v")
	mustFlush(t, e)

	mustDelete(t, e, "k")
	mustFlush(t, e)

	entries, _ := e.Scan(nil, nil)
	for _, en := range entries {
		if string(en.Key) == "k" {
			t.Error("deleted key appeared in Scan result")
		}
	}
}

// ── Test 29: Orphan SSTable not in manifest is ignored ────────────────────────

func TestOpen_OrphanSSTableIgnored(t *testing.T) {
	dir := tmpDir(t)
	e := openEngine(t, dir)
	mustPut(t, e, "k", "v")
	e.Close()

	// Write an orphan SSTable file that is not in the manifest.
	orphan := filepath.Join(dir, sstablesDir, "table-999999.sst")
	os.WriteFile(orphan, []byte("garbage"), 0o600)

	// Engine should open successfully and ignore the orphan.
	e2, err := Open(Options{Dir: dir})
	if err != nil {
		t.Fatalf("Open with orphan file: %v", err)
	}
	defer e2.Close()

	v, ok := mustGet(t, e2, "k")
	if !ok || v != "v" {
		t.Errorf("got (%q, %v) after open with orphan", v, ok)
	}
}

// ── Test 30: Corrupt manifest returns error ────────────────────────────────────

func TestOpen_CorruptManifest_ReturnsError(t *testing.T) {
	dir := tmpDir(t)
	e := openEngine(t, dir)
	e.Close()

	// Overwrite manifest with garbage.
	os.WriteFile(filepath.Join(dir, manifestFilename), []byte("not-json{{{{"), 0o600)

	_, err := Open(Options{Dir: dir})
	if !errors.Is(err, ErrCorruptManifest) {
		t.Fatalf("expected ErrCorruptManifest, got %v", err)
	}
}

// ── Test 31: Corrupt Bloom sidecar returns error ──────────────────────────────

func TestOpen_CorruptBloomSidecar_ReturnsError(t *testing.T) {
	dir := tmpDir(t)
	e := openEngine(t, dir)
	mustPut(t, e, "k", "v")
	mustFlush(t, e)
	e.Close()

	// Overwrite Bloom sidecar with garbage.
	bloomPath := filepath.Join(dir, sstablesDir, "table-000001.bloom")
	os.WriteFile(bloomPath, []byte("corrupt"), 0o600)

	_, err := Open(Options{Dir: dir})
	if err == nil {
		t.Fatal("expected error for corrupt Bloom sidecar, got nil")
	}
}

// ── Test 32: Corrupt SSTable returns error ────────────────────────────────────

func TestOpen_CorruptSSTable_ReturnsError(t *testing.T) {
	dir := tmpDir(t)
	e := openEngine(t, dir)
	mustPut(t, e, "k", "v")
	mustFlush(t, e)
	e.Close()

	// Overwrite SSTable with garbage.
	sstPath := filepath.Join(dir, sstablesDir, "table-000001.sst")
	os.WriteFile(sstPath, []byte("not-an-sstable"), 0o600)

	_, err := Open(Options{Dir: dir})
	if err == nil {
		t.Fatal("expected error for corrupt SSTable, got nil")
	}
}

// ── Test 33: Close then Get returns ErrClosed ─────────────────────────────────

func TestClose_ThenGet_ReturnsErrClosed(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	e.Close()

	_, _, err := e.Get([]byte("k"))
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
}

// ── Test 34: Close then Put returns ErrClosed ─────────────────────────────────

func TestClose_ThenPut_ReturnsErrClosed(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	e.Close()

	if err := e.Put([]byte("k"), []byte("v")); !errors.Is(err, ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
}

// ── Test 35: Close then Flush returns ErrClosed ───────────────────────────────

func TestClose_ThenFlush_ReturnsErrClosed(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	e.Close()

	if err := e.Flush(); !errors.Is(err, ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
}

// ── Test 36: Close is idempotent (second Close returns nil) ───────────────────

func TestClose_Idempotent(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	if err := e.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := e.Close(); err != nil {
		t.Fatalf("second Close returned error: %v", err)
	}
}

// ── Test 37: Concurrent Put/Get/Delete/Scan is race-safe ──────────────────────

func TestConcurrent_RaceSafe(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	// Seed some data.
	for i := 0; i < 50; i++ {
		mustPut(t, e, fmt.Sprintf("key-%04d", i), fmt.Sprintf("val-%04d", i))
	}

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(4)

		go func(id int) { // writers
			defer wg.Done()
			for i := 0; i < 25; i++ {
				e.Put([]byte(fmt.Sprintf("writer-%d-%d", id, i)), []byte("v"))
			}
		}(g)

		go func(id int) { // readers
			defer wg.Done()
			for i := 0; i < 25; i++ {
				e.Get([]byte(fmt.Sprintf("key-%04d", i%50)))
			}
		}(g)

		go func(id int) { // deleters
			defer wg.Done()
			for i := 0; i < 10; i++ {
				e.Delete([]byte(fmt.Sprintf("key-%04d", i%50)))
			}
		}(g)

		go func() { // scanners
			defer wg.Done()
			for i := 0; i < 5; i++ {
				e.Scan(nil, nil)
			}
		}()
	}
	wg.Wait()
}

// ── Test 38: Sequence numbers remain monotonic across restart ─────────────────

func TestSeq_MonotonicAcrossRestart(t *testing.T) {
	dir := tmpDir(t)

	e1 := openEngine(t, dir)
	mustPut(t, e1, "a", "1")
	mustPut(t, e1, "b", "2")
	seqBefore := e1.Stats().NextSeq
	e1.Close()

	e2 := openEngine(t, dir)
	defer e2.Close()

	seqAfter := e2.Stats().NextSeq
	if seqAfter < seqBefore {
		t.Errorf("NextSeq went backwards: before=%d after=%d", seqBefore, seqAfter)
	}

	// New writes must get strictly higher seqs.
	mustPut(t, e2, "c", "3")
	seqFinal := e2.Stats().NextSeq
	if seqFinal <= seqAfter {
		t.Errorf("NextSeq did not advance after Put: %d -> %d", seqAfter, seqFinal)
	}
}

// ── Test 39: Manifest paths are relative ──────────────────────────────────────

func TestManifest_PathsAreRelative(t *testing.T) {
	dir := tmpDir(t)
	e := openEngine(t, dir)
	mustPut(t, e, "k", "v")
	mustFlush(t, e)
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

// ── Test 40: Large workload — 10,000 keys, flush, restart, spot-check ─────────

func TestLargeWorkload_10k(t *testing.T) {
	dir := tmpDir(t)
	n := 10_000

	e1 := openEngine(t, dir)
	for i := 0; i < n; i++ {
		mustPut(t, e1, fmt.Sprintf("key-%08d", i), fmt.Sprintf("val-%08d", i))
	}
	mustFlush(t, e1)
	e1.Close()

	e2 := openEngine(t, dir)
	defer e2.Close()

	// Spot-check several keys.
	for _, idx := range []int{0, 1, 500, 4999, 9999} {
		key := fmt.Sprintf("key-%08d", idx)
		want := fmt.Sprintf("val-%08d", idx)
		v, ok := mustGet(t, e2, key)
		if !ok || v != want {
			t.Errorf("key %q: got (%q, %v)", key, v, ok)
		}
	}

	// Scan range.
	entries, err := e2.Scan([]byte("key-00005000"), []byte("key-00006000"))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(entries) != 1000 {
		t.Errorf("Scan [5000,6000): got %d entries, want 1000", len(entries))
	}
}

// ── Additional: Stats reports SSTable count ───────────────────────────────────

func TestStats_ReportsSSTableCount(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	mustPut(t, e, "k", "v")
	mustFlush(t, e)
	mustPut(t, e, "k2", "v2")
	mustFlush(t, e)

	if e.Stats().SSTableCount != 2 {
		t.Errorf("SSTableCount = %d, want 2", e.Stats().SSTableCount)
	}
}

// ── Additional: Scan with no entries ─────────────────────────────────────────

func TestScan_Empty(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	entries, err := e.Scan(nil, nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

// ── Additional: Close then Scan returns ErrClosed ────────────────────────────

func TestClose_ThenScan_ReturnsErrClosed(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	e.Close()

	_, err := e.Scan(nil, nil)
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("expected ErrClosed for Scan after Close, got %v", err)
	}
}

// ── Additional: Flush count tracked in Stats ──────────────────────────────────

func TestFlush_CountTrackedInStats(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	mustPut(t, e, "k", "v")
	mustFlush(t, e)
	mustPut(t, e, "k2", "v2")
	mustFlush(t, e)

	if e.Stats().FlushCount != 2 {
		t.Errorf("FlushCount = %d, want 2", e.Stats().FlushCount)
	}
}

// ── Additional: Seq monotonic within single session ───────────────────────────

func TestSeq_MonotonicWithinSession(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	var prevSeq uint64
	for i := 0; i < 10; i++ {
		mustPut(t, e, fmt.Sprintf("k%d", i), "v")
		cur := e.Stats().NextSeq
		if cur <= prevSeq {
			t.Errorf("NextSeq not increasing: prev=%d cur=%d", prevSeq, cur)
		}
		prevSeq = cur
	}
}

// ── Review fix 1: Bloom stats are race-safe under concurrent Get ──────────────

// TestConcurrentGet_AfterFlush_BloomStatsRaceSafe launches many goroutines
// that all call Get on a key absent from the SSTable but within its [min,max]
// range, so the Bloom filter is consulted on every call. The test must pass
// under go test -race and verify that BloomChecks and BloomNegativeSkips both
// increase.
func TestConcurrentGet_AfterFlush_BloomStatsRaceSafe(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	// Two keys that bracket the missing key so the bounds check passes and
	// the Bloom filter is always consulted ("alpha" < "missing" < "zebra").
	mustPut(t, e, "alpha", "a")
	mustPut(t, e, "zebra", "z")
	mustFlush(t, e)

	before := e.Stats()

	const goroutines = 32
	const iters = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				_, ok, err := e.Get([]byte("missing"))
				if err != nil {
					t.Errorf("Get: %v", err)
					return
				}
				if ok {
					t.Errorf("Get returned true for absent key")
					return
				}
			}
		}()
	}
	wg.Wait()

	after := e.Stats()
	total := uint64(goroutines * iters)
	if after.BloomChecks <= before.BloomChecks {
		t.Errorf("BloomChecks did not increase: before=%d after=%d", before.BloomChecks, after.BloomChecks)
	}
	if after.BloomNegativeSkips <= before.BloomNegativeSkips {
		t.Errorf("BloomNegativeSkips did not increase: before=%d after=%d", before.BloomNegativeSkips, after.BloomNegativeSkips)
	}
	if got := after.BloomChecks - before.BloomChecks; got != total {
		t.Errorf("BloomChecks delta = %d, want %d", got, total)
	}
	if got := after.BloomNegativeSkips - before.BloomNegativeSkips; got != total {
		t.Errorf("BloomNegativeSkips delta = %d, want %d", got, total)
	}
}

// ── Review fix 2: Atomic Bloom sidecar write leaves no temp file ──────────────

// TestAtomicBloomWrite_NoTempFileLeftAfterFlush verifies that after a
// successful Flush, no .tmp files remain in the sstables directory.
func TestAtomicBloomWrite_NoTempFileLeftAfterFlush(t *testing.T) {
	dir := tmpDir(t)
	e := openEngine(t, dir)
	defer e.Close()

	mustPut(t, e, "k1", "v1")
	mustPut(t, e, "k2", "v2")
	mustFlush(t, e)

	// A second flush ensures we clean up across multiple calls.
	mustPut(t, e, "k3", "v3")
	mustFlush(t, e)

	pattern := filepath.Join(dir, sstablesDir, "*.tmp")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(matches) != 0 {
		t.Errorf("found leftover .tmp files after successful Flush: %v", matches)
	}
}

// TestFlush_BloomWriteFailureDoesNotCommitManifest verifies that if the
// sstables directory is made unwritable after a first successful flush, a
// second flush fails and leaves the manifest unchanged.
//
// This test uses os.Chmod to simulate write failure. It is skipped when
// running as root because root bypasses permission checks, making the
// simulation unreliable.
func TestFlush_BloomWriteFailureDoesNotCommitManifest(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("cannot simulate write failure as root")
	}

	dir := tmpDir(t)
	e := openEngine(t, dir)
	defer func() {
		// Restore permissions before cleanup so TempDir can remove files.
		os.Chmod(filepath.Join(dir, sstablesDir), 0o700)
		e.Close()
	}()

	// First flush succeeds; manifest now has 1 table.
	mustPut(t, e, "k1", "v1")
	mustFlush(t, e)

	if n := e.Stats().SSTableCount; n != 1 {
		t.Fatalf("expected 1 SSTable after first flush, got %d", n)
	}

	// Make sstables/ unwritable so the next SSTable (and Bloom) write fails.
	if err := os.Chmod(filepath.Join(dir, sstablesDir), 0o555); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	mustPut(t, e, "k2", "v2")
	err := e.Flush()
	if err == nil {
		t.Fatal("expected Flush to fail with unwritable sstables dir, got nil")
	}

	// Restore permissions before reading manifest.
	os.Chmod(filepath.Join(dir, sstablesDir), 0o700)

	// Manifest must still record only 1 table (the failed flush must not have
	// updated it).
	m, _, merr := loadManifest(dir)
	if merr != nil {
		t.Fatalf("loadManifest: %v", merr)
	}
	if len(m.Tables) != 1 {
		t.Errorf("manifest has %d tables after failed flush, want 1", len(m.Tables))
	}
}

// ── Review fix 3: First Open writes manifest to disk ─────────────────────────

// TestOpen_CreatesManifest verifies that the very first Open writes an empty
// MANIFEST.json with version=1, NextFileID=1, NextSeq=1, and zero tables.
func TestOpen_CreatesManifest(t *testing.T) {
	dir := tmpDir(t)

	manifestPath := filepath.Join(dir, manifestFilename)
	if _, err := os.Stat(manifestPath); err == nil {
		t.Fatal("MANIFEST.json exists before first Open — test precondition failed")
	}

	e := openEngine(t, dir)
	e.Close()

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("MANIFEST.json not found after Open: %v", err)
	}

	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parse MANIFEST.json: %v", err)
	}
	if m.Version != 1 {
		t.Errorf("Version = %d, want 1", m.Version)
	}
	if m.NextFileID != 1 {
		t.Errorf("NextFileID = %d, want 1", m.NextFileID)
	}
	if m.NextSeq != 1 {
		t.Errorf("NextSeq = %d, want 1", m.NextSeq)
	}
	if len(m.Tables) != 0 {
		t.Errorf("Tables = %v, want []", m.Tables)
	}
}

// ── Review fix 4: Manifest validation — per-entry checks ─────────────────────

// writeRawManifest writes arbitrary JSON to MANIFEST.json in dir, bypassing
// the engine's saveManifest so we can inject corrupt data.
func writeRawManifest(t *testing.T, dir string, v any) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal raw manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, manifestFilename), data, 0o600); err != nil {
		t.Fatalf("write raw manifest: %v", err)
	}
}

// baseValidManifest returns the smallest valid manifest JSON structure with
// the given single table entry.
func baseManifestWithTable(te map[string]any) map[string]any {
	return map[string]any{
		"version":      1,
		"next_file_id": 2,
		"next_seq":     10,
		"tables":       []any{te},
	}
}

// validTableEntry returns a table entry that passes all validation.
// MinKey = encodeKey([]byte("a")), MaxKey = encodeKey([]byte("z")).
func validTableEntry() map[string]any {
	return map[string]any{
		"id":           1,
		"sstable_path": "sstables/table-000001.sst",
		"bloom_path":   "sstables/table-000001.bloom",
		"count":        100,
		"min_key":      "YQ==", // base64("a")
		"max_key":      "eg==", // base64("z")
	}
}

func TestOpen_ManifestRejectsAbsolutePaths(t *testing.T) {
	for _, tc := range []struct {
		name  string
		field string
		path  string
	}{
		{"abs_sstable", "sstable_path", "/etc/passwd"},
		{"abs_bloom", "bloom_path", "/etc/passwd"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := tmpDir(t)
			// Create sstables dir so Open doesn't fail on MkdirAll.
			os.MkdirAll(filepath.Join(dir, sstablesDir), 0o700)

			te := validTableEntry()
			te[tc.field] = tc.path
			writeRawManifest(t, dir, baseManifestWithTable(te))

			_, err := Open(Options{Dir: dir})
			if err != nil {
				err2 := err // shadow to avoid loop-variable capture
				_ = err2
			}
			if !errors.Is(err, ErrCorruptManifest) {
				t.Errorf("expected ErrCorruptManifest for absolute %s %q, got %v",
					tc.field, tc.path, err)
			}
		})
	}
}

func TestOpen_ManifestRejectsPathTraversal(t *testing.T) {
	for _, tc := range []struct {
		name  string
		field string
		path  string
	}{
		{"traversal_sstable", "sstable_path", "../../../etc/passwd"},
		{"traversal_bloom", "bloom_path", "sstables/../../../etc/passwd"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := tmpDir(t)
			os.MkdirAll(filepath.Join(dir, sstablesDir), 0o700)

			te := validTableEntry()
			te[tc.field] = tc.path
			writeRawManifest(t, dir, baseManifestWithTable(te))

			_, err := Open(Options{Dir: dir})
			if !errors.Is(err, ErrCorruptManifest) {
				t.Errorf("expected ErrCorruptManifest for traversal %s %q, got %v",
					tc.field, tc.path, err)
			}
		})
	}
}

func TestOpen_ManifestRejectsDuplicateTableIDs(t *testing.T) {
	dir := tmpDir(t)
	os.MkdirAll(filepath.Join(dir, sstablesDir), 0o700)

	te1 := validTableEntry()
	te2 := validTableEntry()
	te2["sstable_path"] = "sstables/table-000002.sst"
	te2["bloom_path"] = "sstables/table-000002.bloom"
	// Both entries have id=1 → duplicate.

	writeRawManifest(t, dir, map[string]any{
		"version":      1,
		"next_file_id": 3,
		"next_seq":     10,
		"tables":       []any{te1, te2},
	})

	_, err := Open(Options{Dir: dir})
	if !errors.Is(err, ErrCorruptManifest) {
		t.Errorf("expected ErrCorruptManifest for duplicate IDs, got %v", err)
	}
}

func TestOpen_ManifestRejectsBadBase64Keys(t *testing.T) {
	for _, tc := range []struct {
		name  string
		field string
	}{
		{"bad_min_key", "min_key"},
		{"bad_max_key", "max_key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := tmpDir(t)
			os.MkdirAll(filepath.Join(dir, sstablesDir), 0o700)

			te := validTableEntry()
			te[tc.field] = "!!not-valid-base64!!"
			writeRawManifest(t, dir, baseManifestWithTable(te))

			_, err := Open(Options{Dir: dir})
			if !errors.Is(err, ErrCorruptManifest) {
				t.Errorf("expected ErrCorruptManifest for bad base64 %s, got %v", tc.field, err)
			}
		})
	}
}

func TestOpen_ManifestRejectsMinKeyGreaterThanMaxKey(t *testing.T) {
	dir := tmpDir(t)
	os.MkdirAll(filepath.Join(dir, sstablesDir), 0o700)

	te := validTableEntry()
	// min_key = "z", max_key = "a" — min > max.
	te["min_key"] = "eg==" // base64("z")
	te["max_key"] = "YQ==" // base64("a")
	writeRawManifest(t, dir, baseManifestWithTable(te))

	_, err := Open(Options{Dir: dir})
	if !errors.Is(err, ErrCorruptManifest) {
		t.Errorf("expected ErrCorruptManifest for MinKey > MaxKey, got %v", err)
	}
}

func TestOpen_ManifestRejectsEmptyTablePaths(t *testing.T) {
	for _, tc := range []struct {
		name  string
		field string
	}{
		{"empty_sstable_path", "sstable_path"},
		{"empty_bloom_path", "bloom_path"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := tmpDir(t)
			os.MkdirAll(filepath.Join(dir, sstablesDir), 0o700)

			te := validTableEntry()
			te[tc.field] = ""
			writeRawManifest(t, dir, baseManifestWithTable(te))

			_, err := Open(Options{Dir: dir})
			if !errors.Is(err, ErrCorruptManifest) {
				t.Errorf("expected ErrCorruptManifest for empty %s, got %v", tc.field, err)
			}
		})
	}
}
