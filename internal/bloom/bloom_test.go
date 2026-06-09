package bloom

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"math"
	"sync"
	"testing"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func mustNew(t *testing.T, n uint64, p float64) *Filter {
	t.Helper()
	f, err := New(Options{ExpectedItems: n, FalsePositiveRate: p})
	if err != nil {
		t.Fatalf("New(%d, %g): %v", n, p, err)
	}
	return f
}

func mustMarshal(t *testing.T, f *Filter) []byte {
	t.Helper()
	data, err := f.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	return data
}

// ── Test 1: New rejects ExpectedItems == 0 ───────────────────────────────────

func TestNew_RejectsZeroExpectedItems(t *testing.T) {
	_, err := New(Options{ExpectedItems: 0, FalsePositiveRate: 0.01})
	if !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("expected ErrInvalidOptions for ExpectedItems=0, got %v", err)
	}
}

// ── Test 2+3: New rejects all invalid FalsePositiveRate values ───────────────

func TestNew_RejectsInvalidFPR(t *testing.T) {
	cases := []float64{
		0,
		-0.1,
		-1,
		1.0,
		1.1,
		2.0,
		math.Inf(1),
		math.Inf(-1),
		math.NaN(),
	}
	for _, p := range cases {
		_, err := New(Options{ExpectedItems: 100, FalsePositiveRate: p})
		if !errors.Is(err, ErrInvalidOptions) {
			t.Errorf("expected ErrInvalidOptions for FPR=%g, got %v", p, err)
		}
	}
}

// ── Test 4: New computes non-zero bit count and hash count ────────────────────

func TestNew_ComputesNonZeroParams(t *testing.T) {
	f := mustNew(t, 1000, 0.01)
	meta := f.Metadata()
	if meta.BitCount == 0 {
		t.Error("BitCount is zero")
	}
	if meta.HashCount == 0 {
		t.Error("HashCount is zero")
	}
	t.Logf("n=1000 p=0.01 → m=%d k=%d", meta.BitCount, meta.HashCount)
}

// ── Test 5: Add rejects nil key ──────────────────────────────────────────────

func TestAdd_RejectsNilKey(t *testing.T) {
	f := mustNew(t, 100, 0.01)
	if err := f.Add(nil); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("expected ErrInvalidKey for nil key, got %v", err)
	}
}

// ── Test 6: Add rejects empty key ────────────────────────────────────────────

func TestAdd_RejectsEmptyKey(t *testing.T) {
	f := mustNew(t, 100, 0.01)
	if err := f.Add([]byte{}); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("expected ErrInvalidKey for empty key, got %v", err)
	}
}

// ── Test 7: MightContain returns false for nil/empty key ─────────────────────

func TestMightContain_NilOrEmptyReturnsFalse(t *testing.T) {
	f := mustNew(t, 100, 0.01)
	if f.MightContain(nil) {
		t.Error("MightContain(nil) should return false")
	}
	if f.MightContain([]byte{}) {
		t.Error("MightContain([]byte{}) should return false")
	}
}

// ── Test 8: Added key is found ────────────────────────────────────────────────

func TestMightContain_AddedKeyFound(t *testing.T) {
	f := mustNew(t, 100, 0.01)
	key := []byte("hello")
	if err := f.Add(key); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if !f.MightContain(key) {
		t.Error("MightContain returned false for inserted key")
	}
}

// ── Test 9: Multiple added keys are found ────────────────────────────────────

func TestMightContain_MultipleKeysFound(t *testing.T) {
	f := mustNew(t, 1000, 0.01)
	keys := []string{"alpha", "beta", "gamma", "delta", "epsilon"}
	for _, k := range keys {
		if err := f.Add([]byte(k)); err != nil {
			t.Fatalf("Add(%q): %v", k, err)
		}
	}
	for _, k := range keys {
		if !f.MightContain([]byte(k)) {
			t.Errorf("MightContain(%q) returned false after Add", k)
		}
	}
}

// ── Test 10: Duplicate Add does not break membership ─────────────────────────

func TestAdd_DuplicateDoesNotBreakMembership(t *testing.T) {
	f := mustNew(t, 100, 0.01)
	key := []byte("duplicate")
	for i := 0; i < 100; i++ {
		if err := f.Add(key); err != nil {
			t.Fatalf("Add iteration %d: %v", i, err)
		}
	}
	if !f.MightContain(key) {
		t.Error("MightContain returned false after 100 duplicate Adds")
	}
}

// ── Test 11: Missing key usually returns false ────────────────────────────────

func TestMightContain_MissingKeyReturnsFalse(t *testing.T) {
	f := mustNew(t, 10000, 0.001)
	// Add a handful of known keys.
	for i := 0; i < 10; i++ {
		f.Add([]byte(fmt.Sprintf("present-%d", i)))
	}
	// A key never inserted in a large filter should return false.
	if f.MightContain([]byte("definitely-not-added")) {
		// This is technically possible (false positive) but at 0.001 FPR and
		// 10 inserts into a 10k-sized filter the probability is negligible.
		t.Error("MightContain unexpectedly returned true for clearly missing key")
	}
}

// ── Test 12: No false negatives for 10,000 inserted keys ────────────────────

func TestNoFalseNegatives_10k(t *testing.T) {
	n := 10_000
	f := mustNew(t, uint64(n), 0.01)
	keys := make([][]byte, n)
	for i := 0; i < n; i++ {
		keys[i] = []byte(fmt.Sprintf("key-%08d", i))
		if err := f.Add(keys[i]); err != nil {
			t.Fatalf("Add key %d: %v", i, err)
		}
	}
	for i, k := range keys {
		if !f.MightContain(k) {
			t.Fatalf("false negative at key %d (%q)", i, k)
		}
	}
}

// ── Test 13: False positive rate within reasonable bound ──────────────────────

func TestFalsePositiveRate_WithinBound(t *testing.T) {
	n := 10_000
	targetFPR := 0.01
	f := mustNew(t, uint64(n), targetFPR)

	// Insert n keys.
	for i := 0; i < n; i++ {
		f.Add([]byte(fmt.Sprintf("insert-%08d", i)))
	}

	// Query n keys that were never inserted.
	falsePositives := 0
	queries := n
	for i := 0; i < queries; i++ {
		if f.MightContain([]byte(fmt.Sprintf("query-%08d", i))) {
			falsePositives++
		}
	}

	actualFPR := float64(falsePositives) / float64(queries)
	// Allow up to 3× the target FPR as the tolerance.
	if actualFPR > targetFPR*3 {
		t.Errorf("FPR %f exceeds 3× target %f (%d/%d false positives)",
			actualFPR, targetFPR, falsePositives, queries)
	}
	t.Logf("FPR: target=%.4f actual=%.4f (%d/%d)", targetFPR, actualFPR, falsePositives, queries)
}

// ── Test 14: Metadata reports correct fields ──────────────────────────────────

func TestMetadata_ReportsCorrectFields(t *testing.T) {
	opts := Options{ExpectedItems: 500, FalsePositiveRate: 0.05}
	f := mustNew(t, opts.ExpectedItems, opts.FalsePositiveRate)

	f.Add([]byte("k1"))
	f.Add([]byte("k2"))

	meta := f.Metadata()
	if meta.BitCount == 0 {
		t.Error("BitCount is zero")
	}
	if meta.HashCount == 0 {
		t.Error("HashCount is zero")
	}
	if meta.ExpectedItems != opts.ExpectedItems {
		t.Errorf("ExpectedItems = %d, want %d", meta.ExpectedItems, opts.ExpectedItems)
	}
	if meta.FalsePositiveRate != opts.FalsePositiveRate {
		t.Errorf("FalsePositiveRate = %g, want %g", meta.FalsePositiveRate, opts.FalsePositiveRate)
	}
	if meta.InsertedItems != 2 {
		t.Errorf("InsertedItems = %d, want 2", meta.InsertedItems)
	}
}

// ── Test 15: MarshalBinary then UnmarshalBinary preserves membership ──────────

func TestMarshalUnmarshal_PreservesMembership(t *testing.T) {
	f := mustNew(t, 1000, 0.01)
	keys := []string{"foo", "bar", "baz"}
	for _, k := range keys {
		f.Add([]byte(k))
	}

	data := mustMarshal(t, f)
	f2, err := UnmarshalBinary(data)
	if err != nil {
		t.Fatalf("UnmarshalBinary: %v", err)
	}

	for _, k := range keys {
		if !f2.MightContain([]byte(k)) {
			t.Errorf("MightContain(%q) false after roundtrip", k)
		}
	}
	if f2.MightContain([]byte("never-added")) != f.MightContain([]byte("never-added")) {
		t.Error("MightContain mismatch for missing key after roundtrip")
	}
}

// ── Test 16: MarshalBinary is deterministic ───────────────────────────────────

func TestMarshalBinary_IsDeterministic(t *testing.T) {
	f1 := mustNew(t, 1000, 0.01)
	f2 := mustNew(t, 1000, 0.01)
	for i := 0; i < 50; i++ {
		key := []byte(fmt.Sprintf("key-%04d", i))
		f1.Add(key)
		f2.Add(key)
	}
	d1 := mustMarshal(t, f1)
	d2 := mustMarshal(t, f2)
	if !bytes.Equal(d1, d2) {
		t.Error("MarshalBinary produced different bytes for same inputs")
	}
}

// ── Test 17: Unmarshal rejects too-short data ─────────────────────────────────

func TestUnmarshal_RejectsTooShort(t *testing.T) {
	for _, n := range []int{0, 1, minSerialSize - 1} {
		_, err := UnmarshalBinary(make([]byte, n))
		if !errors.Is(err, ErrCorruptFilter) {
			t.Errorf("expected ErrCorruptFilter for %d bytes, got %v", n, err)
		}
	}
}

// ── Test 18: Unmarshal rejects bad leading magic ──────────────────────────────

func TestUnmarshal_RejectsBadLeadingMagic(t *testing.T) {
	f := mustNew(t, 100, 0.01)
	data := mustMarshal(t, f)
	binary.LittleEndian.PutUint64(data[0:8], 0xDEADBEEFCAFEBABE)
	_, err := UnmarshalBinary(data)
	if !errors.Is(err, ErrCorruptFilter) {
		t.Fatalf("expected ErrCorruptFilter for bad leading magic, got %v", err)
	}
}

// ── Test 19: Unmarshal rejects bad trailing magic ─────────────────────────────

func TestUnmarshal_RejectsBadTrailingMagic(t *testing.T) {
	f := mustNew(t, 100, 0.01)
	data := mustMarshal(t, f)
	// Corrupt last 8 bytes.
	binary.LittleEndian.PutUint64(data[len(data)-8:], 0xDEADBEEFCAFEBABE)
	// Also corrupt CRC to pass the CRC check, then corrupt trailing magic.
	// Actually: corrupt trailing magic first, CRC check runs before trailing magic check —
	// we need to check whether the implementation checks CRC or trailing magic first.
	// Our implementation checks CRC before trailing magic. So we must also patch the CRC.
	crcEnd := len(data) - serialTrailerSize
	newCRC := crc32.Checksum(data[:crcEnd], crcTable)
	binary.LittleEndian.PutUint32(data[crcEnd:crcEnd+4], newCRC)
	_, err := UnmarshalBinary(data)
	if !errors.Is(err, ErrCorruptFilter) {
		t.Fatalf("expected ErrCorruptFilter for bad trailing magic, got %v", err)
	}
}

// ── Test 20: Unmarshal rejects unsupported version ────────────────────────────

func TestUnmarshal_RejectsUnsupportedVersion(t *testing.T) {
	f := mustNew(t, 100, 0.01)
	data := mustMarshal(t, f)
	// Overwrite version field (bytes 8-9) with 99.
	binary.LittleEndian.PutUint16(data[8:10], 99)
	// Recompute CRC to pass checksum check (version is in CRC range).
	crcEnd := len(data) - serialTrailerSize
	newCRC := crc32.Checksum(data[:crcEnd], crcTable)
	binary.LittleEndian.PutUint32(data[crcEnd:crcEnd+4], newCRC)
	_, err := UnmarshalBinary(data)
	if !errors.Is(err, ErrCorruptFilter) {
		t.Fatalf("expected ErrCorruptFilter for unsupported version, got %v", err)
	}
}

// ── Test 21: Unmarshal rejects checksum mismatch ─────────────────────────────

func TestUnmarshal_RejectsChecksumMismatch(t *testing.T) {
	f := mustNew(t, 100, 0.01)
	data := mustMarshal(t, f)
	// Flip a bit in the word data (if any words exist; filter always has >= 1 word).
	wordStart := serialHeaderSize
	data[wordStart] ^= 0x01 // flip LSB of first word byte — CRC not recomputed
	_, err := UnmarshalBinary(data)
	if !errors.Is(err, ErrCorruptFilter) {
		t.Fatalf("expected ErrCorruptFilter for checksum mismatch, got %v", err)
	}
}

// ── Test 22: Unmarshal rejects inconsistent word count ────────────────────────

func TestUnmarshal_RejectsInconsistentWordCount(t *testing.T) {
	f := mustNew(t, 100, 0.01)
	data := mustMarshal(t, f)
	// Change wordCount to bitCount/64 + 5 (inconsistent with bitCount).
	// wordCount field is at bytes 46-53 in the header.
	meta := f.Metadata()
	badWordCount := (meta.BitCount+63)/64 + 5
	binary.LittleEndian.PutUint64(data[46:54], badWordCount)
	// Do NOT recompute CRC — length check fires first, or CRC mismatch fires.
	_, err := UnmarshalBinary(data)
	if !errors.Is(err, ErrCorruptFilter) {
		t.Fatalf("expected ErrCorruptFilter for inconsistent wordCount, got %v", err)
	}
}

// ── Test 23: Unmarshal rejects zero bit count ─────────────────────────────────

func TestUnmarshal_RejectsZeroBitCount(t *testing.T) {
	f := mustNew(t, 100, 0.01)
	data := mustMarshal(t, f)
	// Set bitCount (bytes 10-17) to zero.
	binary.LittleEndian.PutUint64(data[10:18], 0)
	// Recompute CRC to pass checksum.
	crcEnd := len(data) - serialTrailerSize
	newCRC := crc32.Checksum(data[:crcEnd], crcTable)
	binary.LittleEndian.PutUint32(data[crcEnd:crcEnd+4], newCRC)
	_, err := UnmarshalBinary(data)
	if !errors.Is(err, ErrCorruptFilter) {
		t.Fatalf("expected ErrCorruptFilter for zero bitCount, got %v", err)
	}
}

// ── Test 24: Unmarshal rejects zero hash count ────────────────────────────────

func TestUnmarshal_RejectsZeroHashCount(t *testing.T) {
	f := mustNew(t, 100, 0.01)
	data := mustMarshal(t, f)
	// Set hashCount (bytes 18-21) to zero.
	binary.LittleEndian.PutUint32(data[18:22], 0)
	// Recompute CRC.
	crcEnd := len(data) - serialTrailerSize
	newCRC := crc32.Checksum(data[:crcEnd], crcTable)
	binary.LittleEndian.PutUint32(data[crcEnd:crcEnd+4], newCRC)
	_, err := UnmarshalBinary(data)
	if !errors.Is(err, ErrCorruptFilter) {
		t.Fatalf("expected ErrCorruptFilter for zero hashCount, got %v", err)
	}
}

// ── Test 24b: Unmarshal rejects zero expectedItems ────────────────────────────

func TestUnmarshal_RejectsZeroExpectedItems(t *testing.T) {
	f := mustNew(t, 100, 0.01)
	data := mustMarshal(t, f)
	// Overwrite expectedItems bytes [22:30] with zero.
	binary.LittleEndian.PutUint64(data[22:30], 0)
	// Recompute CRC so the checksum check passes — the expectedItems check must
	// then catch the zero value independently.
	crcEnd := len(data) - serialTrailerSize
	newCRC := crc32.Checksum(data[:crcEnd], crcTable)
	binary.LittleEndian.PutUint32(data[crcEnd:crcEnd+4], newCRC)
	_, err := UnmarshalBinary(data)
	if !errors.Is(err, ErrCorruptFilter) {
		t.Fatalf("expected ErrCorruptFilter for zero expectedItems, got %v", err)
	}
}

// ── Test 25: Unmarshal rejects excessive word count ──────────────────────────

func TestUnmarshal_RejectsExcessiveWordCount(t *testing.T) {
	// Construct a minimal valid-looking header with wordCount > maxWords.
	// We don't need the actual word data; the check must fire before allocation.
	// We'll build a raw buffer that looks valid up to the wordCount field.
	buf := make([]byte, minSerialSize)
	binary.LittleEndian.PutUint64(buf[0:8], filterMagic)
	binary.LittleEndian.PutUint16(buf[8:10], filterVersion)
	// bitCount must be consistent with wordCount. Set both to maxWords+1 * 64.
	overBits := (maxWords + 1) * 64
	binary.LittleEndian.PutUint64(buf[10:18], overBits)
	binary.LittleEndian.PutUint32(buf[18:22], 1) // hashCount
	binary.LittleEndian.PutUint64(buf[22:30], 1) // expectedItems
	binary.LittleEndian.PutUint64(buf[30:38], 0) // insertedItems
	binary.LittleEndian.PutUint64(buf[38:46], math.Float64bits(0.01))
	binary.LittleEndian.PutUint64(buf[46:54], maxWords+1) // wordCount > cap

	_, err := UnmarshalBinary(buf)
	if !errors.Is(err, ErrCorruptFilter) {
		t.Fatalf("expected ErrCorruptFilter for excessive wordCount, got %v", err)
	}
}

// ── Test 26: Mutating marshaled bytes after Unmarshal does not affect filter ──

func TestUnmarshal_CallerMutationSafe(t *testing.T) {
	f := mustNew(t, 1000, 0.01)
	for i := 0; i < 50; i++ {
		f.Add([]byte(fmt.Sprintf("key-%04d", i)))
	}
	data := mustMarshal(t, f)
	f2, err := UnmarshalBinary(data)
	if err != nil {
		t.Fatalf("UnmarshalBinary: %v", err)
	}

	// Flip all bits in data after unmarshalling.
	for i := range data {
		data[i] ^= 0xFF
	}

	// f2 must still work correctly.
	if !f2.MightContain([]byte("key-0000")) {
		t.Error("filter affected by mutation of source data after Unmarshal")
	}
}

// ── Test 27: Concurrent Add and MightContain is race-safe ─────────────────────

func TestConcurrent_RaceSafe(t *testing.T) {
	f := mustNew(t, 10000, 0.01)

	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(2)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				f.Add([]byte(fmt.Sprintf("writer-%d-%d", id, i)))
			}
		}(g)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				f.MightContain([]byte(fmt.Sprintf("reader-%d-%d", id, i)))
			}
		}(g)
	}
	wg.Wait()
}

// ── Test 28: Bit positions near word boundaries work correctly ────────────────

func TestBitPositions_WordBoundaries(t *testing.T) {
	// Create a filter whose bit count is exactly a multiple of 64 so that
	// boundary bits (63, 64, 127, 128 …) are exercised.
	// We can't force specific bit positions without knowing the hash output, but
	// we can at least verify that Add/MightContain round-trip correctly across
	// a large key space that statistically exercises boundary regions.
	f := mustNew(t, 1000, 0.01)
	keys := make([][]byte, 500)
	for i := range keys {
		keys[i] = []byte(fmt.Sprintf("boundary-test-%05d", i))
		if err := f.Add(keys[i]); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	for _, k := range keys {
		if !f.MightContain(k) {
			t.Fatalf("false negative for boundary key %q", k)
		}
	}
}

// ── Test 29: Large filter — 100,000 keys ─────────────────────────────────────

func TestLargeFilter_100k(t *testing.T) {
	n := 100_000
	f := mustNew(t, uint64(n), 0.01)
	for i := 0; i < n; i++ {
		if err := f.Add([]byte(fmt.Sprintf("large-%09d", i))); err != nil {
			t.Fatalf("Add %d: %v", i, err)
		}
	}
	// No false negatives.
	for i := 0; i < n; i++ {
		if !f.MightContain([]byte(fmt.Sprintf("large-%09d", i))) {
			t.Fatalf("false negative at key %d", i)
		}
	}
	meta := f.Metadata()
	if meta.InsertedItems != uint64(n) {
		t.Errorf("InsertedItems = %d, want %d", meta.InsertedItems, n)
	}
}

// ── Test 30: Serialization roundtrip preserves metadata ──────────────────────

func TestMarshalUnmarshal_PreservesMetadata(t *testing.T) {
	opts := Options{ExpectedItems: 2000, FalsePositiveRate: 0.005}
	f := mustNew(t, opts.ExpectedItems, opts.FalsePositiveRate)
	for i := 0; i < 100; i++ {
		f.Add([]byte(fmt.Sprintf("meta-key-%04d", i)))
	}
	before := f.Metadata()

	data := mustMarshal(t, f)
	f2, err := UnmarshalBinary(data)
	if err != nil {
		t.Fatalf("UnmarshalBinary: %v", err)
	}
	after := f2.Metadata()

	if before.BitCount != after.BitCount {
		t.Errorf("BitCount: before=%d after=%d", before.BitCount, after.BitCount)
	}
	if before.HashCount != after.HashCount {
		t.Errorf("HashCount: before=%d after=%d", before.HashCount, after.HashCount)
	}
	if before.ExpectedItems != after.ExpectedItems {
		t.Errorf("ExpectedItems: before=%d after=%d", before.ExpectedItems, after.ExpectedItems)
	}
	if before.FalsePositiveRate != after.FalsePositiveRate {
		t.Errorf("FalsePositiveRate: before=%g after=%g", before.FalsePositiveRate, after.FalsePositiveRate)
	}
	if before.InsertedItems != after.InsertedItems {
		t.Errorf("InsertedItems: before=%d after=%d", before.InsertedItems, after.InsertedItems)
	}
}

// ── Additional: Metadata is a value copy (mutation-safe) ──────────────────────

func TestMetadata_MutationSafe(t *testing.T) {
	f := mustNew(t, 100, 0.01)
	f.Add([]byte("k"))
	m := f.Metadata()
	m.InsertedItems = 9999 // mutate the copy
	if f.Metadata().InsertedItems != 1 {
		t.Error("Metadata mutation affected the filter")
	}
}

// ── Additional: InsertedItems counts Add calls, not unique keys ───────────────

func TestInsertedItems_CountsAddCalls(t *testing.T) {
	f := mustNew(t, 100, 0.01)
	key := []byte("same-key")
	for i := 0; i < 5; i++ {
		f.Add(key)
	}
	if f.Metadata().InsertedItems != 5 {
		t.Errorf("InsertedItems = %d, want 5 (counts calls not unique keys)",
			f.Metadata().InsertedItems)
	}
}

// ── Additional: calcParams matches expected values for known inputs ────────────

func TestCalcParams_KnownValues(t *testing.T) {
	// For n=1000, p=0.01:
	// m = ceil(1000 * -ln(0.01) / ln(2)^2) = ceil(9585.058...) = 9586
	// k = round(9586/1000 * ln(2)) = round(6.641...) = 7
	m, k := calcParams(1000, 0.01)
	if m != 9586 {
		t.Errorf("m for n=1000 p=0.01: got %d, want 9586", m)
	}
	if k != 7 {
		t.Errorf("k for n=1000 p=0.01: got %d, want 7", k)
	}
	t.Logf("n=1000 p=0.01 → m=%d k=%d", m, k)
}

// ── Additional: empty filter has no membership ────────────────────────────────

func TestEmptyFilter_NoMembership(t *testing.T) {
	f := mustNew(t, 1000, 0.01)
	// No keys added — all queries should return false.
	for i := 0; i < 100; i++ {
		if f.MightContain([]byte(fmt.Sprintf("key-%04d", i))) {
			t.Errorf("empty filter returned true for key %d", i)
		}
	}
}
