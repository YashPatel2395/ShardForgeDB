package wal

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func openTemp(t *testing.T, opts Options) (*WAL, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.wal")
	w, err := Open(path, opts)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { w.Close() })
	return w, path
}

func mustAppend(t *testing.T, w *WAL, r Record) uint64 {
	t.Helper()
	seq, err := w.Append(r)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	return seq
}

func collect(t *testing.T, w *WAL) []Record {
	t.Helper()
	var recs []Record
	if err := w.Replay(func(r Record) error {
		recs = append(recs, r)
		return nil
	}); err != nil {
		t.Fatalf("Replay: %v", err)
	}
	return recs
}

// ── Test 1: Open creates the file ─────────────────────────────────────────────

func TestOpen_CreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.wal")
	w, err := Open(path, Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer w.Close()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("WAL file not created: %v", err)
	}
}

// ── Test 2: Append one PUT and replay it ──────────────────────────────────────

func TestAppendReplay_OnePut(t *testing.T) {
	w, _ := openTemp(t, Options{})

	seq := mustAppend(t, w, Record{Type: RecordPut, Key: []byte("hello"), Value: []byte("world")})
	if seq != 1 {
		t.Fatalf("first seq = %d, want 1", seq)
	}

	recs := collect(t, w)
	if len(recs) != 1 {
		t.Fatalf("replayed %d records, want 1", len(recs))
	}
	r := recs[0]
	if r.Type != RecordPut {
		t.Errorf("Type = %v, want Put", r.Type)
	}
	if string(r.Key) != "hello" {
		t.Errorf("Key = %q, want hello", r.Key)
	}
	if string(r.Value) != "world" {
		t.Errorf("Value = %q, want world", r.Value)
	}
	if r.Seq != 1 {
		t.Errorf("Seq = %d, want 1", r.Seq)
	}
}

// ── Test 3: Multiple records preserve append order ────────────────────────────

func TestAppendReplay_OrderPreserved(t *testing.T) {
	w, _ := openTemp(t, Options{})

	keys := []string{"alpha", "beta", "gamma", "delta"}
	for i, k := range keys {
		mustAppend(t, w, Record{Type: RecordPut, Key: []byte(k), Value: []byte(fmt.Sprintf("v%d", i))})
	}

	recs := collect(t, w)
	if len(recs) != len(keys) {
		t.Fatalf("replayed %d records, want %d", len(recs), len(keys))
	}
	for i, r := range recs {
		if string(r.Key) != keys[i] {
			t.Errorf("record[%d].Key = %q, want %q", i, r.Key, keys[i])
		}
		if r.Seq != uint64(i+1) {
			t.Errorf("record[%d].Seq = %d, want %d", i, r.Seq, i+1)
		}
	}
}

// ── Test 4: Replay empty WAL ──────────────────────────────────────────────────

func TestReplay_EmptyWAL(t *testing.T) {
	w, _ := openTemp(t, Options{})
	recs := collect(t, w)
	if len(recs) != 0 {
		t.Fatalf("expected 0 records from empty WAL, got %d", len(recs))
	}
}

// ── Test 5: DELETE tombstone works ────────────────────────────────────────────

func TestAppendReplay_DeleteTombstone(t *testing.T) {
	w, _ := openTemp(t, Options{})

	mustAppend(t, w, Record{Type: RecordPut, Key: []byte("k"), Value: []byte("v")})
	mustAppend(t, w, Record{Type: RecordDelete, Key: []byte("k")})

	recs := collect(t, w)
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2", len(recs))
	}
	if recs[1].Type != RecordDelete {
		t.Errorf("second record type = %v, want Delete", recs[1].Type)
	}
	if len(recs[1].Value) != 0 {
		t.Errorf("Delete record should have empty value, got %q", recs[1].Value)
	}
}

// ── Test 6: Binary keys and values ────────────────────────────────────────────

func TestAppendReplay_BinaryData(t *testing.T) {
	w, _ := openTemp(t, Options{})

	key := []byte{0x00, 0xFF, 0x10, 0xAB, 0xCD}
	val := []byte{0x01, 0x02, 0x03, 0x00, 0xFF, 0xFE}
	mustAppend(t, w, Record{Type: RecordPut, Key: key, Value: val})

	recs := collect(t, w)
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}
	if string(recs[0].Key) != string(key) {
		t.Errorf("Key mismatch: got %v want %v", recs[0].Key, key)
	}
	if string(recs[0].Value) != string(val) {
		t.Errorf("Value mismatch: got %v want %v", recs[0].Value, val)
	}
}

// ── Test 7: Reject unknown record type ────────────────────────────────────────

func TestAppend_RejectsUnknownType(t *testing.T) {
	w, _ := openTemp(t, Options{})

	_, err := w.Append(Record{Type: 99, Key: []byte("k")})
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("expected ErrInvalidRecord, got %v", err)
	}
}

// ── Test 8: Reject empty key ──────────────────────────────────────────────────

func TestAppend_RejectsEmptyKey(t *testing.T) {
	w, _ := openTemp(t, Options{})

	_, err := w.Append(Record{Type: RecordPut, Key: nil, Value: []byte("v")})
	if !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("expected ErrInvalidRecord, got %v", err)
	}
}

// ── Test 9: Reject oversized record ───────────────────────────────────────────

func TestAppend_RejectsOversizedRecord(t *testing.T) {
	w, _ := openTemp(t, Options{MaxRecordSize: 32})

	bigVal := make([]byte, 100)
	_, err := w.Append(Record{Type: RecordPut, Key: []byte("k"), Value: bigVal})
	if !errors.Is(err, ErrRecordTooLarge) {
		t.Fatalf("expected ErrRecordTooLarge, got %v", err)
	}
}

// ── Test 10: Append after Close returns ErrClosed ─────────────────────────────

func TestAppend_AfterClose(t *testing.T) {
	w, _ := openTemp(t, Options{})
	w.Close()

	_, err := w.Append(Record{Type: RecordPut, Key: []byte("k"), Value: []byte("v")})
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
}

// ── Test 11: Replay detects checksum corruption ───────────────────────────────

func TestReplay_DetectsChecksumCorruption(t *testing.T) {
	w, path := openTemp(t, Options{})

	// Write two records so the corruption is not at the tail.
	mustAppend(t, w, Record{Type: RecordPut, Key: []byte("k1"), Value: []byte("v1")})
	mustAppend(t, w, Record{Type: RecordPut, Key: []byte("k2"), Value: []byte("v2")})
	w.Close()

	// Corrupt the CRC of the first frame (bytes 4–7).
	f, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte{0xDE, 0xAD, 0xBE, 0xEF}, 4); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	w2, err := Open(path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer w2.Close()

	err = w2.Replay(func(Record) error { return nil })
	if !errors.Is(err, ErrCorruptRecord) {
		t.Fatalf("expected ErrCorruptRecord, got %v", err)
	}
}

// ── Test 12: Incomplete final header is not an error ─────────────────────────

func TestReplay_IncompleteFinalHeader(t *testing.T) {
	w, path := openTemp(t, Options{})
	mustAppend(t, w, Record{Type: RecordPut, Key: []byte("k"), Value: []byte("v")})
	w.Close()

	// Append 3 bytes (less than the 8-byte header) to simulate a torn write.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte{0x01, 0x02, 0x03})
	f.Close()

	w2, err := Open(path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer w2.Close()

	recs := collect(t, w2)
	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
}

// ── Test 13: Incomplete final body is not an error ───────────────────────────

func TestReplay_IncompleteFinalBody(t *testing.T) {
	w, path := openTemp(t, Options{})
	mustAppend(t, w, Record{Type: RecordPut, Key: []byte("k"), Value: []byte("v")})
	w.Close()

	// Read the file to get the body length of the first record, then append a
	// full header pointing to a body that we intentionally do not write.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	bodyLen := binary.LittleEndian.Uint32(data[0:4])
	_ = bodyLen

	// Append a valid-looking header but no body.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	var hdr [frameHeaderSize]byte
	binary.LittleEndian.PutUint32(hdr[0:4], 100) // claims 100-byte body
	binary.LittleEndian.PutUint32(hdr[4:8], 0)   // fake CRC
	f.Write(hdr[:])
	f.Close()

	w2, err := Open(path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer w2.Close()

	recs := collect(t, w2)
	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
}

// ── Test 14: Mid-file corruption is not silently ignored ─────────────────────

func TestReplay_MidFileCorruptionNotIgnored(t *testing.T) {
	w, path := openTemp(t, Options{})
	// Three records so that corrupting the first is definitely not at the tail.
	mustAppend(t, w, Record{Type: RecordPut, Key: []byte("a"), Value: []byte("1")})
	mustAppend(t, w, Record{Type: RecordPut, Key: []byte("b"), Value: []byte("2")})
	mustAppend(t, w, Record{Type: RecordPut, Key: []byte("c"), Value: []byte("3")})
	w.Close()

	// Corrupt the first record's CRC field.
	f, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteAt([]byte{0xFF, 0xFF, 0xFF, 0xFF}, 4)
	f.Close()

	w2, _ := Open(path, Options{})
	defer w2.Close()

	err = w2.Replay(func(Record) error { return nil })
	if err == nil {
		t.Fatal("expected corruption error, got nil")
	}
	if !errors.Is(err, ErrCorruptRecord) {
		t.Fatalf("expected ErrCorruptRecord, got %v", err)
	}
}

// ── Test 15: Concurrent appends are race-safe ────────────────────────────────

func TestAppend_ConcurrentSafe(t *testing.T) {
	w, _ := openTemp(t, Options{})

	const goroutines = 20
	const perGoroutine = 50

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				_, err := w.Append(Record{
					Type:  RecordPut,
					Key:   []byte(fmt.Sprintf("g%d-k%d", id, i)),
					Value: []byte("v"),
				})
				if err != nil {
					t.Errorf("goroutine %d: Append error: %v", id, err)
					return
				}
			}
		}(g)
	}
	wg.Wait()

	recs := collect(t, w)
	if len(recs) != goroutines*perGoroutine {
		t.Fatalf("expected %d records, got %d", goroutines*perGoroutine, len(recs))
	}

	// Verify all sequence numbers are unique.
	seen := make(map[uint64]bool, len(recs))
	for _, r := range recs {
		if seen[r.Seq] {
			t.Errorf("duplicate sequence number %d", r.Seq)
		}
		seen[r.Seq] = true
	}
}

// ── Test 16: Callback error stops replay ─────────────────────────────────────

func TestReplay_CallbackErrorStopsReplay(t *testing.T) {
	w, _ := openTemp(t, Options{})
	for i := 0; i < 5; i++ {
		mustAppend(t, w, Record{Type: RecordPut, Key: []byte(fmt.Sprintf("k%d", i)), Value: []byte("v")})
	}

	sentinel := errors.New("stop here")
	count := 0
	err := w.Replay(func(r Record) error {
		count++
		if count == 2 {
			return sentinel
		}
		return nil
	})

	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
	if count != 2 {
		t.Fatalf("callback called %d times, want 2", count)
	}
}

// ── Test 17: Caller mutation after Append does not affect replay ──────────────

func TestAppend_CallerMutationSafe(t *testing.T) {
	w, _ := openTemp(t, Options{})

	key := []byte("original-key")
	val := []byte("original-val")
	mustAppend(t, w, Record{Type: RecordPut, Key: key, Value: val})

	// Mutate the slices after Append.
	copy(key, "mutated-key!")
	copy(val, "mutated-val!")

	recs := collect(t, w)
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}
	if string(recs[0].Key) != "original-key" {
		t.Errorf("Key = %q, want original-key", recs[0].Key)
	}
	if string(recs[0].Value) != "original-val" {
		t.Errorf("Value = %q, want original-val", recs[0].Value)
	}
}

// ── Test 18: Reopen existing WAL appends without losing old records ───────────

func TestReopen_PreservesOldRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reopen.wal")

	// First session: write 3 records.
	w1, err := Open(path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	mustAppend(t, w1, Record{Type: RecordPut, Key: []byte("a"), Value: []byte("1")})
	mustAppend(t, w1, Record{Type: RecordPut, Key: []byte("b"), Value: []byte("2")})
	mustAppend(t, w1, Record{Type: RecordPut, Key: []byte("c"), Value: []byte("3")})
	w1.Close()

	// Second session: reopen and add 2 more records.
	w2, err := Open(path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer w2.Close()

	if err := w2.Replay(func(Record) error { return nil }); err != nil {
		t.Fatalf("Replay on reopen: %v", err)
	}

	mustAppend(t, w2, Record{Type: RecordPut, Key: []byte("d"), Value: []byte("4")})
	mustAppend(t, w2, Record{Type: RecordPut, Key: []byte("e"), Value: []byte("5")})

	// Replay again and expect all 5.
	var recs []Record
	w2.Replay(func(r Record) error { //nolint:errcheck
		recs = append(recs, r)
		return nil
	})
	if len(recs) != 5 {
		t.Fatalf("expected 5 records after reopen, got %d", len(recs))
	}
	wantKeys := []string{"a", "b", "c", "d", "e"}
	for i, r := range recs {
		if string(r.Key) != wantKeys[i] {
			t.Errorf("record[%d].Key = %q, want %q", i, r.Key, wantKeys[i])
		}
	}
}
