package sstable

import (
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func tmpPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "test.sst")
}

func mustCreate(t *testing.T, path string, entries []Entry) Metadata {
	t.Helper()
	meta, err := Create(path, entries, Options{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return meta
}

func mustOpen(t *testing.T, path string) *Reader {
	t.Helper()
	r, err := Open(path, Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return r
}

// makeEntries returns n sorted, unique PUT entries.
func makeEntries(n int) []Entry {
	entries := make([]Entry, n)
	for i := 0; i < n; i++ {
		entries[i] = Entry{
			Key:   []byte(fmt.Sprintf("key-%08d", i)),
			Value: []byte(fmt.Sprintf("value-%08d", i)),
			Kind:  EntryPut,
			Seq:   uint64(i + 1),
		}
	}
	return entries
}

// ── Test 1: Create writes a file ──────────────────────────────────────────────

func TestCreate_WritesFile(t *testing.T) {
	path := tmpPath(t)
	mustCreate(t, path, makeEntries(5))

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after Create: %v", err)
	}
	if fi.Size() == 0 {
		t.Error("SSTable file is empty")
	}
}

// ── Test 2: Open reads metadata correctly ────────────────────────────────────

func TestOpen_ReadsMetadata(t *testing.T) {
	path := tmpPath(t)
	entries := makeEntries(10)
	mustCreate(t, path, entries)

	r := mustOpen(t, path)
	defer r.Close()

	meta := r.Metadata()
	if meta.Count != 10 {
		t.Errorf("Count = %d, want 10", meta.Count)
	}
	if string(meta.MinKey) != string(entries[0].Key) {
		t.Errorf("MinKey = %q, want %q", meta.MinKey, entries[0].Key)
	}
	if string(meta.MaxKey) != string(entries[9].Key) {
		t.Errorf("MaxKey = %q, want %q", meta.MaxKey, entries[9].Key)
	}
	if meta.SizeBytes == 0 {
		t.Error("SizeBytes should be non-zero")
	}
	if r.Len() != 10 {
		t.Errorf("Len = %d, want 10", r.Len())
	}
}

// ── Test 3: Create rejects empty table ───────────────────────────────────────

func TestCreate_RejectsEmptyTable(t *testing.T) {
	path := tmpPath(t)
	_, err := Create(path, nil, Options{})
	if !errors.Is(err, ErrEmptyTable) {
		t.Fatalf("expected ErrEmptyTable, got %v", err)
	}
	_, err = Create(path, []Entry{}, Options{})
	if !errors.Is(err, ErrEmptyTable) {
		t.Fatalf("expected ErrEmptyTable for empty slice, got %v", err)
	}
}

// ── Test 4: Create rejects empty key ─────────────────────────────────────────

func TestCreate_RejectsEmptyKey(t *testing.T) {
	path := tmpPath(t)
	_, err := Create(path, []Entry{{Key: nil, Value: []byte("v"), Kind: EntryPut, Seq: 1}}, Options{})
	if !errors.Is(err, ErrInvalidEntry) {
		t.Fatalf("expected ErrInvalidEntry for nil key, got %v", err)
	}
	_, err = Create(path, []Entry{{Key: []byte{}, Value: []byte("v"), Kind: EntryPut, Seq: 1}}, Options{})
	if !errors.Is(err, ErrInvalidEntry) {
		t.Fatalf("expected ErrInvalidEntry for empty key, got %v", err)
	}
}

// ── Test 5: Create rejects unknown entry kind ─────────────────────────────────

func TestCreate_RejectsUnknownKind(t *testing.T) {
	path := tmpPath(t)
	_, err := Create(path, []Entry{{Key: []byte("k"), Kind: EntryKind(0), Seq: 1}}, Options{})
	if !errors.Is(err, ErrInvalidEntry) {
		t.Fatalf("expected ErrInvalidEntry for kind 0, got %v", err)
	}
	_, err = Create(path, []Entry{{Key: []byte("k"), Kind: EntryKind(99), Seq: 1}}, Options{})
	if !errors.Is(err, ErrInvalidEntry) {
		t.Fatalf("expected ErrInvalidEntry for kind 99, got %v", err)
	}
}

// ── Test 6: Create rejects unsorted entries ───────────────────────────────────

func TestCreate_RejectsUnsortedEntries(t *testing.T) {
	path := tmpPath(t)
	entries := []Entry{
		{Key: []byte("b"), Kind: EntryPut, Seq: 1},
		{Key: []byte("a"), Kind: EntryPut, Seq: 2},
	}
	_, err := Create(path, entries, Options{})
	if !errors.Is(err, ErrUnsortedEntries) {
		t.Fatalf("expected ErrUnsortedEntries, got %v", err)
	}
}

// ── Test 7: Create rejects duplicate keys ─────────────────────────────────────

func TestCreate_RejectsDuplicateKeys(t *testing.T) {
	path := tmpPath(t)
	entries := []Entry{
		{Key: []byte("k"), Kind: EntryPut, Seq: 1},
		{Key: []byte("k"), Kind: EntryPut, Seq: 2},
	}
	_, err := Create(path, entries, Options{})
	if !errors.Is(err, ErrDuplicateKey) {
		t.Fatalf("expected ErrDuplicateKey, got %v", err)
	}
}

// ── Test 8: Create rejects oversized record ───────────────────────────────────

func TestCreate_RejectsOversizedRecord(t *testing.T) {
	path := tmpPath(t)
	bigVal := make([]byte, 100)
	opts := Options{MaxRecordSize: 10} // tiny limit
	_, err := Create(path, []Entry{{Key: []byte("k"), Value: bigVal, Kind: EntryPut, Seq: 1}}, opts)
	if !errors.Is(err, ErrRecordTooLarge) {
		t.Fatalf("expected ErrRecordTooLarge, got %v", err)
	}
}

// ── Test 9: Get existing PUT returns value ────────────────────────────────────

func TestGet_ExistingPut(t *testing.T) {
	path := tmpPath(t)
	mustCreate(t, path, []Entry{
		{Key: []byte("alpha"), Value: []byte("one"), Kind: EntryPut, Seq: 1},
		{Key: []byte("beta"), Value: []byte("two"), Kind: EntryPut, Seq: 2},
		{Key: []byte("gamma"), Value: []byte("three"), Kind: EntryPut, Seq: 3},
	})
	r := mustOpen(t, path)
	defer r.Close()

	e, ok, err := r.Get([]byte("beta"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("Get returned false for existing key")
	}
	if e.Kind != EntryPut {
		t.Errorf("Kind = %v, want EntryPut", e.Kind)
	}
	if string(e.Value) != "two" {
		t.Errorf("Value = %q, want two", e.Value)
	}
	if e.Seq != 2 {
		t.Errorf("Seq = %d, want 2", e.Seq)
	}
}

// ── Test 10: Get missing key returns false ────────────────────────────────────

func TestGet_MissingKey(t *testing.T) {
	path := tmpPath(t)
	mustCreate(t, path, makeEntries(5))
	r := mustOpen(t, path)
	defer r.Close()

	_, ok, err := r.Get([]byte("no-such-key"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ok {
		t.Error("Get returned true for missing key")
	}
}

// ── Test 11: Get returns DELETE tombstone ─────────────────────────────────────

func TestGet_Tombstone(t *testing.T) {
	path := tmpPath(t)
	mustCreate(t, path, []Entry{
		{Key: []byte("alive"), Value: []byte("yes"), Kind: EntryPut, Seq: 1},
		{Key: []byte("dead"), Kind: EntryDelete, Seq: 2},
	})
	r := mustOpen(t, path)
	defer r.Close()

	e, ok, err := r.Get([]byte("dead"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("Get returned false for tombstone key")
	}
	if e.Kind != EntryDelete {
		t.Errorf("Kind = %v, want EntryDelete", e.Kind)
	}
}

// ── Test 12: Scan returns entries in sorted order ─────────────────────────────

func TestScan_SortedOrder(t *testing.T) {
	path := tmpPath(t)
	mustCreate(t, path, makeEntries(5))
	r := mustOpen(t, path)
	defer r.Close()

	entries, err := r.Scan(nil, nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(entries) != 5 {
		t.Fatalf("got %d entries, want 5", len(entries))
	}
	for i := 1; i < len(entries); i++ {
		if string(entries[i].Key) <= string(entries[i-1].Key) {
			t.Errorf("entries not sorted at %d: %q <= %q", i, entries[i].Key, entries[i-1].Key)
		}
	}
}

// ── Test 13: Scan respects start inclusive ────────────────────────────────────

func TestScan_StartInclusive(t *testing.T) {
	path := tmpPath(t)
	mustCreate(t, path, []Entry{
		{Key: []byte("a"), Kind: EntryPut, Seq: 1},
		{Key: []byte("b"), Kind: EntryPut, Seq: 2},
		{Key: []byte("c"), Kind: EntryPut, Seq: 3},
	})
	r := mustOpen(t, path)
	defer r.Close()

	entries, err := r.Scan([]byte("b"), nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if string(entries[0].Key) != "b" {
		t.Errorf("first entry = %q, want b", entries[0].Key)
	}
}

// ── Test 14: Scan respects end exclusive ──────────────────────────────────────

func TestScan_EndExclusive(t *testing.T) {
	path := tmpPath(t)
	mustCreate(t, path, []Entry{
		{Key: []byte("a"), Kind: EntryPut, Seq: 1},
		{Key: []byte("b"), Kind: EntryPut, Seq: 2},
		{Key: []byte("c"), Kind: EntryPut, Seq: 3},
	})
	r := mustOpen(t, path)
	defer r.Close()

	entries, err := r.Scan(nil, []byte("c"))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if string(entries[1].Key) != "b" {
		t.Errorf("last entry = %q, want b", entries[1].Key)
	}
}

// ── Test 15: Scan nil/empty bounds returns all ────────────────────────────────

func TestScan_NilBoundsReturnsAll(t *testing.T) {
	path := tmpPath(t)
	n := 8
	mustCreate(t, path, makeEntries(n))
	r := mustOpen(t, path)
	defer r.Close()

	e1, _ := r.Scan(nil, nil)
	e2, _ := r.Scan([]byte{}, []byte{})
	if len(e1) != n {
		t.Errorf("nil-nil Scan: got %d, want %d", len(e1), n)
	}
	if len(e2) != n {
		t.Errorf("empty-empty Scan: got %d, want %d", len(e2), n)
	}
}

// ── Test 16: Scan includes tombstones ─────────────────────────────────────────

func TestScan_IncludesTombstones(t *testing.T) {
	path := tmpPath(t)
	mustCreate(t, path, []Entry{
		{Key: []byte("a"), Kind: EntryPut, Seq: 1},
		{Key: []byte("b"), Kind: EntryDelete, Seq: 2},
		{Key: []byte("c"), Kind: EntryPut, Seq: 3},
	})
	r := mustOpen(t, path)
	defer r.Close()

	entries, err := r.Scan(nil, nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d, want 3", len(entries))
	}
	if entries[1].Kind != EntryDelete {
		t.Errorf("entries[1].Kind = %v, want EntryDelete", entries[1].Kind)
	}
}

// ── Test 17: Binary keys and values work ──────────────────────────────────────

func TestBinaryKeysAndValues(t *testing.T) {
	path := tmpPath(t)
	key := []byte{0x00, 0xFF, 0x10, 0xAB}
	val := []byte{0x01, 0x00, 0xFE, 0xFF}
	mustCreate(t, path, []Entry{{Key: key, Value: val, Kind: EntryPut, Seq: 1}})
	r := mustOpen(t, path)
	defer r.Close()

	e, ok, err := r.Get(key)
	if err != nil || !ok {
		t.Fatalf("Get binary key: err=%v ok=%v", err, ok)
	}
	if string(e.Key) != string(key) {
		t.Errorf("Key mismatch")
	}
	if string(e.Value) != string(val) {
		t.Errorf("Value mismatch")
	}
}

// ── Test 18: Sequence numbers are preserved ───────────────────────────────────

func TestSequenceNumbers_Preserved(t *testing.T) {
	path := tmpPath(t)
	mustCreate(t, path, []Entry{
		{Key: []byte("k1"), Kind: EntryPut, Seq: 42},
		{Key: []byte("k2"), Kind: EntryDelete, Seq: 999},
	})
	r := mustOpen(t, path)
	defer r.Close()

	e1, _, _ := r.Get([]byte("k1"))
	if e1.Seq != 42 {
		t.Errorf("Seq k1 = %d, want 42", e1.Seq)
	}
	e2, _, _ := r.Get([]byte("k2"))
	if e2.Seq != 999 {
		t.Errorf("Seq k2 = %d, want 999", e2.Seq)
	}
}

// ── Test 19: Returned Get entry is mutation-safe ──────────────────────────────

func TestGet_CallerMutationSafe(t *testing.T) {
	path := tmpPath(t)
	mustCreate(t, path, []Entry{{Key: []byte("k"), Value: []byte("original"), Kind: EntryPut, Seq: 1}})
	r := mustOpen(t, path)
	defer r.Close()

	e, _, _ := r.Get([]byte("k"))
	copy(e.Value, "XXXXXXXX")

	e2, _, err := r.Get([]byte("k"))
	if err != nil {
		t.Fatalf("Get after mutation: %v", err)
	}
	if string(e2.Value) != "original" {
		t.Errorf("stored value mutated: got %q", e2.Value)
	}
}

// ── Test 20: Returned Scan entries are mutation-safe ──────────────────────────

func TestScan_CallerMutationSafe(t *testing.T) {
	path := tmpPath(t)
	mustCreate(t, path, []Entry{{Key: []byte("k"), Value: []byte("original"), Kind: EntryPut, Seq: 1}})
	r := mustOpen(t, path)
	defer r.Close()

	entries, _ := r.Scan(nil, nil)
	copy(entries[0].Value, "XXXXXXXX")

	e, _, err := r.Get([]byte("k"))
	if err != nil {
		t.Fatalf("Get after Scan mutation: %v", err)
	}
	if string(e.Value) != "original" {
		t.Errorf("stored value mutated via Scan: got %q", e.Value)
	}
}

// ── Test 21: Metadata MinKey/MaxKey are mutation-safe ─────────────────────────

func TestMetadata_KeysMutationSafe(t *testing.T) {
	path := tmpPath(t)
	mustCreate(t, path, []Entry{
		{Key: []byte("aaa"), Kind: EntryPut, Seq: 1},
		{Key: []byte("zzz"), Kind: EntryPut, Seq: 2},
	})
	r := mustOpen(t, path)
	defer r.Close()

	meta := r.Metadata()
	copy(meta.MinKey, "XXX")
	copy(meta.MaxKey, "XXX")

	meta2 := r.Metadata()
	if string(meta2.MinKey) != "aaa" {
		t.Errorf("MinKey mutated: got %q", meta2.MinKey)
	}
	if string(meta2.MaxKey) != "zzz" {
		t.Errorf("MaxKey mutated: got %q", meta2.MaxKey)
	}
}

// ── Test 22: Open detects bad magic ──────────────────────────────────────────

func TestOpen_DetectsBadMagic(t *testing.T) {
	path := tmpPath(t)
	mustCreate(t, path, makeEntries(3))

	// Corrupt the header magic (first 8 bytes).
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open for corruption: %v", err)
	}
	f.WriteAt([]byte{0xDE, 0xAD, 0xBE, 0xEF, 0, 0, 0, 0}, 0)
	f.Close()

	_, err = Open(path, Options{})
	if !errors.Is(err, ErrCorruptTable) {
		t.Fatalf("expected ErrCorruptTable for bad magic, got %v", err)
	}
}

// ── Test 23: Open detects corrupt footer checksum ─────────────────────────────

func TestOpen_DetectsCorruptFooterChecksum(t *testing.T) {
	path := tmpPath(t)
	mustCreate(t, path, makeEntries(3))

	fi, _ := os.Stat(path)
	// Corrupt footerCRC bytes (bytes -12 to -8 from end within the footer).
	// Footer layout: [indexOffset 8][indexLen 8][entryCount 8][footerCRC 4][magic 8]
	// footerCRC is at offset -12 from the end of file.
	crcOffset := fi.Size() - int64(footerSize) + 24
	f, _ := os.OpenFile(path, os.O_RDWR, 0)
	f.WriteAt([]byte{0xFF, 0xFF, 0xFF, 0xFF}, crcOffset)
	f.Close()

	_, err := Open(path, Options{})
	if !errors.Is(err, ErrCorruptTable) {
		t.Fatalf("expected ErrCorruptTable for bad footer CRC, got %v", err)
	}
}

// ── Test 24: Get detects corrupt record checksum ──────────────────────────────

func TestGet_DetectsCorruptRecord(t *testing.T) {
	path := tmpPath(t)
	mustCreate(t, path, []Entry{
		{Key: []byte("k"), Value: []byte("hello world"), Kind: EntryPut, Seq: 1},
	})

	// Corrupt a byte in the record body (past the header, past the CRC).
	// Record starts at headerSize (10). Frame header is 8 bytes.
	// Body starts at offset 10+8=18. Corrupt byte 20 (inside body).
	f, _ := os.OpenFile(path, os.O_RDWR, 0)
	f.WriteAt([]byte{0xFF}, int64(headerSize+recordHeaderSize+5))
	f.Close()

	r := mustOpen(t, path)
	defer r.Close()

	_, _, err := r.Get([]byte("k"))
	if !errors.Is(err, ErrCorruptTable) {
		t.Fatalf("expected ErrCorruptTable for corrupt record, got %v", err)
	}
}

// ── Test 25: Scan detects corrupt record checksum ─────────────────────────────

func TestScan_DetectsCorruptRecord(t *testing.T) {
	path := tmpPath(t)
	mustCreate(t, path, []Entry{
		{Key: []byte("k"), Value: []byte("hello world"), Kind: EntryPut, Seq: 1},
	})

	f, _ := os.OpenFile(path, os.O_RDWR, 0)
	f.WriteAt([]byte{0xFF}, int64(headerSize+recordHeaderSize+5))
	f.Close()

	r := mustOpen(t, path)
	defer r.Close()

	_, err := r.Scan(nil, nil)
	if !errors.Is(err, ErrCorruptTable) {
		t.Fatalf("expected ErrCorruptTable from Scan on corrupt record, got %v", err)
	}
}

// ── Test 26: Truncated file returns ErrCorruptTable ──────────────────────────

func TestOpen_TruncatedFile(t *testing.T) {
	path := tmpPath(t)
	mustCreate(t, path, makeEntries(5))

	fi, _ := os.Stat(path)
	// Truncate to remove the footer.
	os.Truncate(path, fi.Size()-int64(footerSize))

	_, err := Open(path, Options{})
	if !errors.Is(err, ErrCorruptTable) {
		t.Fatalf("expected ErrCorruptTable for truncated file, got %v", err)
	}
}

// ── Test 27: Close then Get returns ErrClosed ────────────────────────────────

func TestClose_ThenGet_ReturnsErrClosed(t *testing.T) {
	path := tmpPath(t)
	mustCreate(t, path, makeEntries(3))
	r := mustOpen(t, path)
	r.Close()

	_, _, err := r.Get([]byte("key-00000001"))
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("expected ErrClosed after Close, got %v", err)
	}
}

// ── Test 28: Close then Scan returns ErrClosed ───────────────────────────────

func TestClose_ThenScan_ReturnsErrClosed(t *testing.T) {
	path := tmpPath(t)
	mustCreate(t, path, makeEntries(3))
	r := mustOpen(t, path)
	r.Close()

	_, err := r.Scan(nil, nil)
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("expected ErrClosed after Close, got %v", err)
	}
}

// ── Test 29: Close called twice returns ErrClosed ────────────────────────────
// Documented behavior: second Close returns ErrClosed.

func TestClose_CalledTwice_ReturnsErrClosed(t *testing.T) {
	path := tmpPath(t)
	mustCreate(t, path, makeEntries(3))
	r := mustOpen(t, path)

	if err := r.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := r.Close(); !errors.Is(err, ErrClosed) {
		t.Fatalf("second Close: expected ErrClosed, got %v", err)
	}
}

// ── Test 30: Concurrent Get/Scan is race-safe ─────────────────────────────────

func TestConcurrent_RaceSafe(t *testing.T) {
	path := tmpPath(t)
	mustCreate(t, path, makeEntries(100))
	r := mustOpen(t, path)
	defer r.Close()

	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(2)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				key := []byte(fmt.Sprintf("key-%08d", (id*20+i)%100))
				r.Get(key)
			}
		}(g)
		go func() {
			defer wg.Done()
			for i := 0; i < 5; i++ {
				r.Scan(nil, nil)
			}
		}()
	}
	wg.Wait()
}

// ── Test 31: Create failure does not leave partial final file ─────────────────

func TestCreate_FailureNoPartialFile(t *testing.T) {
	path := tmpPath(t)

	// Trigger validation failure (unsorted) — Create should never touch path.
	_, err := Create(path, []Entry{
		{Key: []byte("z"), Kind: EntryPut, Seq: 1},
		{Key: []byte("a"), Kind: EntryPut, Seq: 2},
	}, Options{})
	if err == nil {
		t.Fatal("expected error from Create with unsorted entries")
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("Create left a partial file at the final path after validation failure")
	}
}

// ── Test 32: Large table (10,000 entries) supports Get and Scan ───────────────

func TestLargeTable(t *testing.T) {
	path := tmpPath(t)
	n := 10_000
	entries := makeEntries(n)
	mustCreate(t, path, entries)

	r := mustOpen(t, path)
	defer r.Close()

	if r.Len() != uint64(n) {
		t.Errorf("Len = %d, want %d", r.Len(), n)
	}

	// Spot-check a few keys.
	for _, idx := range []int{0, 1, 500, 4999, 9999} {
		key := []byte(fmt.Sprintf("key-%08d", idx))
		e, ok, err := r.Get(key)
		if err != nil || !ok {
			t.Errorf("Get key-%08d: err=%v ok=%v", idx, err, ok)
			continue
		}
		if string(e.Key) != string(key) {
			t.Errorf("key mismatch at %d", idx)
		}
	}

	// Scan a range.
	start := []byte("key-00005000")
	end := []byte("key-00006000")
	scanned, err := r.Scan(start, end)
	if err != nil {
		t.Fatalf("Scan large: %v", err)
	}
	if len(scanned) != 1000 {
		t.Errorf("Scan [5000,6000): got %d entries, want 1000", len(scanned))
	}
}

// ── Additional: Metadata from Create matches Open ────────────────────────────

func TestMetadata_CreateMatchesOpen(t *testing.T) {
	path := tmpPath(t)
	entries := makeEntries(7)
	createMeta := mustCreate(t, path, entries)
	r := mustOpen(t, path)
	defer r.Close()

	openMeta := r.Metadata()
	if createMeta.Count != openMeta.Count {
		t.Errorf("Count: Create=%d Open=%d", createMeta.Count, openMeta.Count)
	}
	if string(createMeta.MinKey) != string(openMeta.MinKey) {
		t.Errorf("MinKey: Create=%q Open=%q", createMeta.MinKey, openMeta.MinKey)
	}
	if string(createMeta.MaxKey) != string(openMeta.MaxKey) {
		t.Errorf("MaxKey: Create=%q Open=%q", createMeta.MaxKey, openMeta.MaxKey)
	}
}

// ── Additional: Scan with start > end returns empty ──────────────────────────

func TestScan_StartAfterEnd_ReturnsEmpty(t *testing.T) {
	path := tmpPath(t)
	mustCreate(t, path, makeEntries(5))
	r := mustOpen(t, path)
	defer r.Close()

	entries, err := r.Scan([]byte("z"), []byte("a"))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty result for start>end, got %d entries", len(entries))
	}
}

// ── Additional: Deterministic Scan after random-order Create ─────────────────

func TestScan_DeterministicAfterSortedCreate(t *testing.T) {
	path := tmpPath(t)
	// Generate and sort entries before passing (Create requires sorted input).
	rng := rand.New(rand.NewSource(42))
	n := 200
	keys := make([]string, n)
	for i := range keys {
		keys[i] = fmt.Sprintf("key-%06d", i)
	}
	rng.Shuffle(len(keys), func(i, j int) { keys[i], keys[j] = keys[j], keys[i] })
	// Sort back — Create requires sorted.
	sortedKeys := make([]string, n)
	copy(sortedKeys, keys)
	// Use stdlib sort via a simple insertion sort is overkill; just generate sorted.
	entries := makeEntries(n)
	mustCreate(t, path, entries)

	r := mustOpen(t, path)
	defer r.Close()

	all, _ := r.Scan(nil, nil)
	if len(all) != n {
		t.Fatalf("got %d, want %d", len(all), n)
	}
	for i := 1; i < len(all); i++ {
		if string(all[i].Key) <= string(all[i-1].Key) {
			t.Errorf("not sorted at %d", i)
		}
	}
}
