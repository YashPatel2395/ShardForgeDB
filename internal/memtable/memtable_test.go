package memtable

import (
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"testing"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func newMT(t *testing.T) *MemTable {
	t.Helper()
	return New(Options{})
}

func mustPut(t *testing.T, m *MemTable, key, value string, seq uint64) {
	t.Helper()
	if err := m.Put([]byte(key), []byte(value), seq); err != nil {
		t.Fatalf("Put(%q, %q): %v", key, value, err)
	}
}

func mustDelete(t *testing.T, m *MemTable, key string, seq uint64) {
	t.Helper()
	if err := m.Delete([]byte(key), seq); err != nil {
		t.Fatalf("Delete(%q): %v", key, err)
	}
}

// ── Test 1: New returns an empty MemTable ─────────────────────────────────────

func TestNew_ReturnsEmpty(t *testing.T) {
	m := newMT(t)
	if m.Len() != 0 {
		t.Errorf("Len = %d, want 0", m.Len())
	}
	if m.ApproxBytes() != 0 {
		t.Errorf("ApproxBytes = %d, want 0", m.ApproxBytes())
	}
	if m.ShouldFlush() {
		t.Error("ShouldFlush should be false on empty MemTable")
	}
}

// ── Test 2: Put then Get returns value ────────────────────────────────────────

func TestPutGet_Basic(t *testing.T) {
	m := newMT(t)
	mustPut(t, m, "hello", "world", 1)

	e, ok := m.Get([]byte("hello"))
	if !ok {
		t.Fatal("Get returned false for existing key")
	}
	if e.Kind != EntryPut {
		t.Errorf("Kind = %v, want EntryPut", e.Kind)
	}
	if string(e.Key) != "hello" {
		t.Errorf("Key = %q, want hello", e.Key)
	}
	if string(e.Value) != "world" {
		t.Errorf("Value = %q, want world", e.Value)
	}
	if e.Seq != 1 {
		t.Errorf("Seq = %d, want 1", e.Seq)
	}
}

// ── Test 3: Get missing key returns false ─────────────────────────────────────

func TestGet_MissingKey(t *testing.T) {
	m := newMT(t)
	_, ok := m.Get([]byte("nope"))
	if ok {
		t.Error("Get returned true for missing key")
	}
}

// ── Test 4: Put rejects empty key ─────────────────────────────────────────────

func TestPut_RejectsEmptyKey(t *testing.T) {
	m := newMT(t)
	err := m.Put([]byte{}, []byte("val"), 1)
	if !errors.Is(err, ErrEmptyKey) {
		t.Fatalf("expected ErrEmptyKey, got %v", err)
	}
	err = m.Put(nil, []byte("val"), 1)
	if !errors.Is(err, ErrEmptyKey) {
		t.Fatalf("expected ErrEmptyKey for nil key, got %v", err)
	}
}

// ── Test 5: Delete rejects empty key ──────────────────────────────────────────

func TestDelete_RejectsEmptyKey(t *testing.T) {
	m := newMT(t)
	err := m.Delete([]byte{}, 1)
	if !errors.Is(err, ErrEmptyKey) {
		t.Fatalf("expected ErrEmptyKey, got %v", err)
	}
	err = m.Delete(nil, 1)
	if !errors.Is(err, ErrEmptyKey) {
		t.Fatalf("expected ErrEmptyKey for nil key, got %v", err)
	}
}

// ── Test 6: Delete stores tombstone ───────────────────────────────────────────

func TestDelete_StoresTombstone(t *testing.T) {
	m := newMT(t)
	mustPut(t, m, "k", "v", 1)
	mustDelete(t, m, "k", 2)

	e, ok := m.Get([]byte("k"))
	if !ok {
		t.Fatal("Get returned false after Delete")
	}
	if e.Kind != EntryDelete {
		t.Errorf("Kind = %v, want EntryDelete", e.Kind)
	}
	if len(e.Value) != 0 {
		t.Errorf("tombstone Value should be empty, got %q", e.Value)
	}
}

// ── Test 7: Put replaces existing key ─────────────────────────────────────────

func TestPut_ReplacesExistingKey(t *testing.T) {
	m := newMT(t)
	mustPut(t, m, "k", "old", 1)
	mustPut(t, m, "k", "new", 2)

	if m.Len() != 1 {
		t.Errorf("Len = %d, want 1 after replace", m.Len())
	}
	e, ok := m.Get([]byte("k"))
	if !ok {
		t.Fatal("Get returned false")
	}
	if string(e.Value) != "new" {
		t.Errorf("Value = %q, want new", e.Value)
	}
	if e.Seq != 2 {
		t.Errorf("Seq = %d, want 2", e.Seq)
	}
}

// ── Test 8: Delete replaces existing Put with tombstone ───────────────────────

func TestDelete_ReplacesExistingPut(t *testing.T) {
	m := newMT(t)
	mustPut(t, m, "k", "val", 1)
	mustDelete(t, m, "k", 2)

	if m.Len() != 1 {
		t.Errorf("Len = %d, want 1", m.Len())
	}
	e, ok := m.Get([]byte("k"))
	if !ok {
		t.Fatal("Get returned false after tombstone")
	}
	if e.Kind != EntryDelete {
		t.Errorf("Kind = %v, want EntryDelete", e.Kind)
	}
}

// ── Test 9: Put after Delete restores value ───────────────────────────────────

func TestPut_AfterDelete_RestoresValue(t *testing.T) {
	m := newMT(t)
	mustPut(t, m, "k", "v1", 1)
	mustDelete(t, m, "k", 2)
	mustPut(t, m, "k", "v2", 3)

	e, ok := m.Get([]byte("k"))
	if !ok {
		t.Fatal("Get returned false")
	}
	if e.Kind != EntryPut {
		t.Errorf("Kind = %v, want EntryPut", e.Kind)
	}
	if string(e.Value) != "v2" {
		t.Errorf("Value = %q, want v2", e.Value)
	}
}

// ── Test 10: Scan returns keys in sorted order ────────────────────────────────

func TestScan_SortedOrder(t *testing.T) {
	m := newMT(t)
	// Insert in non-alphabetical order.
	mustPut(t, m, "cherry", "c", 3)
	mustPut(t, m, "apple", "a", 1)
	mustPut(t, m, "banana", "b", 2)

	entries := m.Scan(nil, nil)
	if len(entries) != 3 {
		t.Fatalf("Scan returned %d entries, want 3", len(entries))
	}
	want := []string{"apple", "banana", "cherry"}
	for i, e := range entries {
		if string(e.Key) != want[i] {
			t.Errorf("entries[%d].Key = %q, want %q", i, e.Key, want[i])
		}
	}
}

// ── Test 11: Scan respects start inclusive ────────────────────────────────────

func TestScan_StartInclusive(t *testing.T) {
	m := newMT(t)
	mustPut(t, m, "a", "1", 1)
	mustPut(t, m, "b", "2", 2)
	mustPut(t, m, "c", "3", 3)

	entries := m.Scan([]byte("b"), nil)
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if string(entries[0].Key) != "b" {
		t.Errorf("first entry key = %q, want b", entries[0].Key)
	}
	if string(entries[1].Key) != "c" {
		t.Errorf("second entry key = %q, want c", entries[1].Key)
	}
}

// ── Test 12: Scan respects end exclusive ──────────────────────────────────────

func TestScan_EndExclusive(t *testing.T) {
	m := newMT(t)
	mustPut(t, m, "a", "1", 1)
	mustPut(t, m, "b", "2", 2)
	mustPut(t, m, "c", "3", 3)

	entries := m.Scan(nil, []byte("c"))
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if string(entries[1].Key) != "b" {
		t.Errorf("last entry key = %q, want b", entries[1].Key)
	}
}

// ── Test 13: Scan with nil/empty bounds returns all entries ───────────────────

func TestScan_NilBounds_ReturnsAll(t *testing.T) {
	m := newMT(t)
	for i := 0; i < 5; i++ {
		mustPut(t, m, fmt.Sprintf("k%d", i), "v", uint64(i))
	}
	if len(m.Scan(nil, nil)) != 5 {
		t.Errorf("nil-nil Scan returned %d, want 5", len(m.Scan(nil, nil)))
	}
	if len(m.Scan([]byte{}, []byte{})) != 5 {
		t.Errorf("empty-empty Scan returned %d, want 5", len(m.Scan(nil, nil)))
	}
}

// ── Test 14: Scan includes tombstones ────────────────────────────────────────

func TestScan_IncludesTombstones(t *testing.T) {
	m := newMT(t)
	mustPut(t, m, "a", "val", 1)
	mustDelete(t, m, "b", 2) // tombstone for never-put key
	mustPut(t, m, "c", "val", 3)

	entries := m.Scan(nil, nil)
	if len(entries) != 3 {
		t.Fatalf("Scan returned %d entries, want 3", len(entries))
	}
	if entries[1].Kind != EntryDelete {
		t.Errorf("entries[1].Kind = %v, want EntryDelete", entries[1].Kind)
	}
}

// ── Test 15: Len returns unique key count ─────────────────────────────────────

func TestLen_UniqueKeyCount(t *testing.T) {
	m := newMT(t)
	mustPut(t, m, "x", "1", 1)
	mustPut(t, m, "x", "2", 2) // replace
	mustPut(t, m, "y", "3", 3)
	mustDelete(t, m, "y", 4) // tombstone still counts
	if m.Len() != 2 {
		t.Errorf("Len = %d, want 2", m.Len())
	}
}

// ── Test 16: ApproxBytes increases on insert ──────────────────────────────────

func TestApproxBytes_IncreasesOnInsert(t *testing.T) {
	m := newMT(t)
	before := m.ApproxBytes()
	mustPut(t, m, "key", "val", 1)
	if m.ApproxBytes() <= before {
		t.Errorf("ApproxBytes did not increase: before=%d after=%d", before, m.ApproxBytes())
	}
}

// ── Test 17: ApproxBytes updates on replace with larger value ─────────────────

func TestApproxBytes_UpdatesOnReplaceWithLargerValue(t *testing.T) {
	m := newMT(t)
	mustPut(t, m, "k", "short", 1)
	small := m.ApproxBytes()
	mustPut(t, m, "k", "a-much-longer-value", 2)
	if m.ApproxBytes() <= small {
		t.Errorf("ApproxBytes should increase on larger replace: %d → %d", small, m.ApproxBytes())
	}
}

// ── Test 18: ApproxBytes updates on replace with smaller value ────────────────

func TestApproxBytes_UpdatesOnReplaceWithSmallerValue(t *testing.T) {
	m := newMT(t)
	mustPut(t, m, "k", "a-much-longer-value", 1)
	large := m.ApproxBytes()
	mustPut(t, m, "k", "x", 2)
	if m.ApproxBytes() >= large {
		t.Errorf("ApproxBytes should decrease on smaller replace: %d → %d", large, m.ApproxBytes())
	}
}

// ── Test 19: ApproxBytes updates on delete tombstone ─────────────────────────

func TestApproxBytes_UpdatesOnDeleteTombstone(t *testing.T) {
	m := newMT(t)
	mustPut(t, m, "k", "value-with-bytes", 1)
	withValue := m.ApproxBytes()
	mustDelete(t, m, "k", 2) // tombstone has no value bytes
	withTombstone := m.ApproxBytes()
	// Tombstone has no value, so bytes should decrease.
	if withTombstone >= withValue {
		t.Errorf("ApproxBytes should decrease after tombstone: put=%d tomb=%d", withValue, withTombstone)
	}
}

// ── Test 20: ShouldFlush false below threshold ────────────────────────────────

func TestShouldFlush_FalseBelow(t *testing.T) {
	m := New(Options{MaxBytes: 1 << 30}) // 1 GiB — won't flush on a single put
	mustPut(t, m, "k", "v", 1)
	if m.ShouldFlush() {
		t.Errorf("ShouldFlush should be false (bytes=%d, max=%d)", m.ApproxBytes(), m.opts.MaxBytes)
	}
}

// ── Test 21: ShouldFlush true at or above threshold ───────────────────────────

func TestShouldFlush_TrueAtOrAbove(t *testing.T) {
	// One entry uses: len("k")+len("v")+entryOverhead = 1+1+64 = 66 bytes.
	// Set MaxBytes=66 so we flush exactly at the first entry.
	const key, val = "k", "v"
	maxBytes := uint64(len(key)) + uint64(len(val)) + entryOverhead
	m := New(Options{MaxBytes: maxBytes})
	mustPut(t, m, key, val, 1)
	if !m.ShouldFlush() {
		t.Errorf("ShouldFlush should be true: bytes=%d max=%d", m.ApproxBytes(), maxBytes)
	}
}

// ── Test 22: Caller mutation after Put does not affect stored data ─────────────

func TestPut_CallerMutationSafe(t *testing.T) {
	m := newMT(t)
	key := []byte("mykey")
	val := []byte("myval")
	mustPut(t, m, string(key), string(val), 1)

	// Mutate the slices after Put.
	copy(key, "xxxxx")
	copy(val, "xxxxx")

	e, ok := m.Get([]byte("mykey"))
	if !ok {
		t.Fatal("Get returned false")
	}
	if string(e.Key) != "mykey" {
		t.Errorf("Key = %q, want mykey", e.Key)
	}
	if string(e.Value) != "myval" {
		t.Errorf("Value = %q, want myval", e.Value)
	}
}

// ── Test 23: Caller mutation after Get does not affect stored data ─────────────

func TestGet_CallerMutationSafe(t *testing.T) {
	m := newMT(t)
	mustPut(t, m, "k", "original", 1)

	e, _ := m.Get([]byte("k"))
	copy(e.Value, "xxxxxxxx") // mutate the returned copy

	e2, _ := m.Get([]byte("k"))
	if string(e2.Value) != "original" {
		t.Errorf("stored value was mutated: got %q", e2.Value)
	}
}

// ── Test 24: Caller mutation after Scan does not affect stored data ────────────

func TestScan_CallerMutationSafe(t *testing.T) {
	m := newMT(t)
	mustPut(t, m, "k", "original", 1)

	entries := m.Scan(nil, nil)
	copy(entries[0].Value, "xxxxxxxx")

	e, _ := m.Get([]byte("k"))
	if string(e.Value) != "original" {
		t.Errorf("stored value was mutated via Scan result: got %q", e.Value)
	}
}

// ── Test 25: Binary keys and values work ──────────────────────────────────────

func TestPut_BinaryData(t *testing.T) {
	m := newMT(t)
	key := []byte{0x00, 0xFF, 0x10, 0xAB}
	val := []byte{0x01, 0x00, 0xFE, 0xFF, 0x00}
	if err := m.Put(key, val, 1); err != nil {
		t.Fatalf("Put binary: %v", err)
	}
	e, ok := m.Get(key)
	if !ok {
		t.Fatal("Get binary key returned false")
	}
	if string(e.Key) != string(key) {
		t.Errorf("Key mismatch")
	}
	if string(e.Value) != string(val) {
		t.Errorf("Value mismatch")
	}
}

// ── Test 26: Sequence numbers are preserved ───────────────────────────────────

func TestSequenceNumbers_Preserved(t *testing.T) {
	m := newMT(t)
	mustPut(t, m, "k", "v1", 42)
	e, _ := m.Get([]byte("k"))
	if e.Seq != 42 {
		t.Errorf("Seq = %d, want 42", e.Seq)
	}

	mustPut(t, m, "k", "v2", 99)
	e2, _ := m.Get([]byte("k"))
	if e2.Seq != 99 {
		t.Errorf("Seq after replace = %d, want 99", e2.Seq)
	}

	mustDelete(t, m, "k", 100)
	e3, _ := m.Get([]byte("k"))
	if e3.Seq != 100 {
		t.Errorf("Seq of tombstone = %d, want 100", e3.Seq)
	}
}

// ── Test 27: Concurrent Put/Get/Delete/Scan is race-safe ─────────────────────

func TestConcurrent_RaceSafe(t *testing.T) {
	m := newMT(t)

	const goroutines = 16
	var wg sync.WaitGroup

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				key := fmt.Sprintf("g%d-k%d", id, i)
				if err := m.Put([]byte(key), []byte("v"), uint64(i)); err != nil {
					t.Errorf("Put: %v", err)
					return
				}
			}
		}(g)

		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				key := fmt.Sprintf("g%d-k%d", id, i*2)
				m.Get([]byte(key))
			}
		}(g)

		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				key := fmt.Sprintf("g%d-k%d", id, i*5)
				m.Delete([]byte(key), uint64(i+1000))
			}
		}(g)

		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				m.Scan(nil, nil)
			}
		}()
	}

	wg.Wait()
}

// ── Test 28: Scan result is deterministic after many unordered inserts ─────────

func TestScan_Deterministic(t *testing.T) {
	m := newMT(t)

	// Insert 100 keys in a pseudo-random order using a fixed seed.
	rng := rand.New(rand.NewSource(12345))
	keys := make([]string, 100)
	for i := range keys {
		keys[i] = fmt.Sprintf("key-%04d", i)
	}
	rng.Shuffle(len(keys), func(i, j int) { keys[i], keys[j] = keys[j], keys[i] })
	for i, k := range keys {
		mustPut(t, m, k, "v", uint64(i))
	}

	entries := m.Scan(nil, nil)
	if len(entries) != 100 {
		t.Fatalf("Scan returned %d entries, want 100", len(entries))
	}

	// Verify lexicographic order.
	sorted := make([]string, 100)
	for i, e := range entries {
		sorted[i] = string(e.Key)
	}
	if !sort.StringsAreSorted(sorted) {
		t.Error("Scan result is not sorted")
	}
}

// ── Test 29: Delete missing key still creates tombstone ───────────────────────

func TestDelete_MissingKey_CreatesTombstone(t *testing.T) {
	m := newMT(t)
	mustDelete(t, m, "never-put", 1)

	if m.Len() != 1 {
		t.Errorf("Len = %d, want 1 after Delete of missing key", m.Len())
	}
	e, ok := m.Get([]byte("never-put"))
	if !ok {
		t.Fatal("Get returned false for tombstone")
	}
	if e.Kind != EntryDelete {
		t.Errorf("Kind = %v, want EntryDelete", e.Kind)
	}
}

// ── Test 30: Repeated delete updates sequence number ─────────────────────────

func TestDelete_RepeatedUpdatesSeq(t *testing.T) {
	m := newMT(t)
	mustDelete(t, m, "k", 1)
	mustDelete(t, m, "k", 5)

	if m.Len() != 1 {
		t.Errorf("Len = %d, want 1 after repeated Delete", m.Len())
	}
	e, ok := m.Get([]byte("k"))
	if !ok {
		t.Fatal("Get returned false")
	}
	if e.Seq != 5 {
		t.Errorf("Seq = %d, want 5 (latest Delete seq)", e.Seq)
	}
}
