// Package bloom implements a deterministic, serializable Bloom filter for
// ShardForgeDB SSTable membership testing.
//
// A Bloom filter is a space-efficient probabilistic data structure that answers
// set-membership queries with no false negatives and a bounded false-positive
// rate. This is used to avoid unnecessary disk reads: before seeking into an
// SSTable for a missing key, the engine checks the filter and skips the file
// if the filter definitively says the key is absent.
//
// # Parameter formulas
//
// Given n = ExpectedItems and p = FalsePositiveRate:
//
//	m = ceil( -n * ln(p) / (ln(2)^2) )   // bit count
//	k = round( (m / n) * ln(2) )          // hash count, minimum 1
//
// # Hash strategy
//
// Double hashing with two deterministic FNV-1a 64-bit variants:
//
//	h1(x) = FNV-1a-64(x)                     // standard FNV-1a seed
//	h2(x) = FNV-1a-64(salt_bytes || x)       // secondary seed (salt = 0x9e3779b97f4a7c15)
//
// The i-th bit position for key x is:
//
//	pos_i = (h1(x) + i * h2(x)) mod m
//
// If h2 is zero it is replaced with 0x9e3779b97f4a7c15 (a well-distributed
// odd constant) to prevent degenerate bit patterns.
//
// # Bitset layout
//
// Bits are packed into a []uint64 slice. Bit b lives in word b/64 at bit
// position b%64. All reads/writes are protected by a sync.RWMutex.
//
// # Serialization format
//
//	[magic       8 bytes  ]  0x_42_4c_4f_4f_4d_46_4c_54  ("BLOOMFLT")
//	[version     2 bytes  ]  uint16, little-endian, currently 1
//	[bitCount    8 bytes  ]  uint64, little-endian
//	[hashCount   4 bytes  ]  uint32, little-endian
//	[expectedItems 8 bytes]  uint64, little-endian
//	[insertedItems 8 bytes]  uint64, little-endian
//	[fprBits     8 bytes  ]  math.Float64bits(FalsePositiveRate), little-endian
//	[wordCount   8 bytes  ]  uint64, little-endian
//	[words       wordCount*8 bytes ]
//	[crc32       4 bytes  ]  IEEE CRC-32 over all bytes from magic through words
//	[magic       8 bytes  ]  trailing sentinel, same value as leading magic
//
// # Known limitations
//
//   - Not wired into SSTable yet; integration is the Engine's responsibility.
//   - No scalable / partitioned Bloom filter.
//   - No counting Bloom filter; Delete is not supported.
//   - No compression of the bit array.
//   - InsertedItems counts successful Add calls, not unique keys; duplicate adds
//     increment the counter.
//   - False positives are possible by design.
package bloom

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"hash/fnv"
	"math"
	"sync"
)

// ── Exported errors ───────────────────────────────────────────────────────────

// ErrInvalidOptions is returned when Options fields are out of range.
var ErrInvalidOptions = errors.New("bloom: invalid options")

// ErrInvalidKey is returned when a nil or empty key is passed to Add.
var ErrInvalidKey = errors.New("bloom: invalid key")

// ErrCorruptFilter is returned when UnmarshalBinary detects a format or
// checksum error.
var ErrCorruptFilter = errors.New("bloom: corrupt filter")

// ErrFilterTooLarge is returned when the calculated or serialized filter
// exceeds an internal safety cap.
var ErrFilterTooLarge = errors.New("bloom: filter too large")

// ── Constants ─────────────────────────────────────────────────────────────────

const (
	// magic is the 8-byte sentinel used at the start and end of serialized data.
	// ASCII: "BLOOMFLT"
	filterMagic uint64 = 0x544c_464d_4f4f_4c42

	filterVersion uint16 = 1

	// maxWords is the maximum number of uint64 words the serializer will accept.
	// This caps the in-memory bit array at 64 GiB, preventing OOM from corrupt
	// wordCount fields during UnmarshalBinary.
	maxWords uint64 = 1 << 30 // 2^30 words = 8 GiB of bits

	// maxBits is the corresponding bit count cap derived from maxWords.
	maxBits uint64 = maxWords * 64

	// h2Salt is XOR'd into the key before computing h2 to produce a decorrelated
	// secondary hash. Value: the fractional part of the golden ratio * 2^64.
	h2Salt uint64 = 0x9e37_79b9_7f4a_7c15

	// h2Fallback is used when h2 computes to zero, ensuring the double-hashing
	// sequence visits distinct positions.
	h2Fallback uint64 = h2Salt

	// serialHeaderSize is the number of bytes before the word array.
	// magic(8)+version(2)+bitCount(8)+hashCount(4)+expectedItems(8)+
	// insertedItems(8)+fprBits(8)+wordCount(8) = 54
	serialHeaderSize = 8 + 2 + 8 + 4 + 8 + 8 + 8 + 8 // 54

	// serialTrailerSize is crc32(4) + trailing magic(8) = 12.
	serialTrailerSize = 4 + 8

	// minSerialSize is the smallest valid serialized blob (no words).
	minSerialSize = serialHeaderSize + serialTrailerSize
)

var crcTable = crc32.MakeTable(crc32.IEEE)

// ── Options ───────────────────────────────────────────────────────────────────

// Options configures a new Bloom filter.
type Options struct {
	// ExpectedItems is the number of keys expected to be inserted.
	// Must be greater than zero.
	ExpectedItems uint64

	// FalsePositiveRate is the target false-positive probability.
	// Must be in the open interval (0, 1). Typical values: 0.01 (1%).
	FalsePositiveRate float64
}

// ── Metadata ──────────────────────────────────────────────────────────────────

// Metadata describes a Filter's parameters and current state.
// All fields are value types; mutating the returned struct does not affect the
// filter.
type Metadata struct {
	// BitCount is the number of bits in the underlying bit array.
	BitCount uint64
	// HashCount is the number of independent hash positions per key.
	HashCount uint32
	// ExpectedItems is the number of items the filter was sized for.
	ExpectedItems uint64
	// FalsePositiveRate is the target FPR used when constructing the filter.
	FalsePositiveRate float64
	// InsertedItems is the number of successful Add calls made so far.
	// This counts Add calls, not unique keys. Duplicate adds are counted.
	InsertedItems uint64
}

// ── Filter ────────────────────────────────────────────────────────────────────

// Filter is a deterministic, serializable Bloom filter.
//
// Concurrent calls to Add and MightContain are safe. Concurrent calls to
// MarshalBinary, UnmarshalBinary, and Metadata are also safe.
type Filter struct {
	mu            sync.RWMutex
	words         []uint64 // packed bit array
	bitCount      uint64
	hashCount     uint32
	expectedItems uint64
	insertedItems uint64
	fpr           float64
}

// ── New ───────────────────────────────────────────────────────────────────────

// New constructs a Bloom filter sized for opts.ExpectedItems keys at the target
// opts.FalsePositiveRate.
//
// Errors: ErrInvalidOptions if ExpectedItems == 0 or FalsePositiveRate is not
// in (0, 1), or ErrFilterTooLarge if the required bit count exceeds the
// internal safety cap.
func New(opts Options) (*Filter, error) {
	if opts.ExpectedItems == 0 {
		return nil, fmt.Errorf("%w: ExpectedItems must be > 0", ErrInvalidOptions)
	}
	if math.IsNaN(opts.FalsePositiveRate) || math.IsInf(opts.FalsePositiveRate, 0) ||
		opts.FalsePositiveRate <= 0 || opts.FalsePositiveRate >= 1 {
		return nil, fmt.Errorf("%w: FalsePositiveRate must be a finite value in (0, 1), got %g",
			ErrInvalidOptions, opts.FalsePositiveRate)
	}

	m, k := calcParams(opts.ExpectedItems, opts.FalsePositiveRate)
	if m > maxBits {
		return nil, fmt.Errorf("%w: required bit count %d exceeds cap %d",
			ErrFilterTooLarge, m, maxBits)
	}

	words := (m + 63) / 64
	return &Filter{
		words:         make([]uint64, words),
		bitCount:      m,
		hashCount:     k,
		expectedItems: opts.ExpectedItems,
		fpr:           opts.FalsePositiveRate,
	}, nil
}

// ── Add ───────────────────────────────────────────────────────────────────────

// Add inserts key into the filter. It is idempotent with respect to
// MightContain: adding a key that is already "present" does not change the
// observable membership result.
//
// InsertedItems is incremented on every successful call (not deduplicated).
// Returns ErrInvalidKey for nil or empty keys.
func (f *Filter) Add(key []byte) error {
	if len(key) == 0 {
		return fmt.Errorf("%w: key must be non-empty", ErrInvalidKey)
	}

	h1, h2 := hashKey(key)

	f.mu.Lock()
	defer f.mu.Unlock()

	for i := uint64(0); i < uint64(f.hashCount); i++ {
		bit := (h1 + i*h2) % f.bitCount
		f.words[bit/64] |= 1 << (bit % 64)
	}
	f.insertedItems++
	return nil
}

// ── MightContain ──────────────────────────────────────────────────────────────

// MightContain returns true if key was possibly inserted (may false-positive),
// and false if key was definitely not inserted (no false negatives).
//
// Nil and empty keys always return false.
func (f *Filter) MightContain(key []byte) bool {
	if len(key) == 0 {
		return false
	}

	h1, h2 := hashKey(key)

	f.mu.RLock()
	defer f.mu.RUnlock()

	for i := uint64(0); i < uint64(f.hashCount); i++ {
		bit := (h1 + i*h2) % f.bitCount
		if f.words[bit/64]&(1<<(bit%64)) == 0 {
			return false
		}
	}
	return true
}

// ── Metadata ──────────────────────────────────────────────────────────────────

// Metadata returns a copy of the filter's parameters and state.
func (f *Filter) Metadata() Metadata {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return Metadata{
		BitCount:          f.bitCount,
		HashCount:         f.hashCount,
		ExpectedItems:     f.expectedItems,
		FalsePositiveRate: f.fpr,
		InsertedItems:     f.insertedItems,
	}
}

// ── MarshalBinary ─────────────────────────────────────────────────────────────

// MarshalBinary serializes the filter to a self-describing binary blob.
// The format includes magic bytes, version, all parameters, the bit array,
// a CRC-32 checksum, and a trailing magic sentinel.
//
// The returned slice is a fresh allocation; the caller may freely mutate it
// without affecting the filter.
func (f *Filter) MarshalBinary() ([]byte, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	wordCount := uint64(len(f.words))
	// Total: header + words*8 + trailer
	totalSize := serialHeaderSize + int(wordCount)*8 + serialTrailerSize
	buf := make([]byte, totalSize)

	pos := 0

	// Leading magic
	binary.LittleEndian.PutUint64(buf[pos:], filterMagic)
	pos += 8
	// Version
	binary.LittleEndian.PutUint16(buf[pos:], filterVersion)
	pos += 2
	// bitCount
	binary.LittleEndian.PutUint64(buf[pos:], f.bitCount)
	pos += 8
	// hashCount
	binary.LittleEndian.PutUint32(buf[pos:], f.hashCount)
	pos += 4
	// expectedItems
	binary.LittleEndian.PutUint64(buf[pos:], f.expectedItems)
	pos += 8
	// insertedItems
	binary.LittleEndian.PutUint64(buf[pos:], f.insertedItems)
	pos += 8
	// fprBits
	binary.LittleEndian.PutUint64(buf[pos:], math.Float64bits(f.fpr))
	pos += 8
	// wordCount
	binary.LittleEndian.PutUint64(buf[pos:], wordCount)
	pos += 8

	// words
	for _, w := range f.words {
		binary.LittleEndian.PutUint64(buf[pos:], w)
		pos += 8
	}

	// CRC-32 over [leading magic … last word]
	checksum := crc32.Checksum(buf[:pos], crcTable)
	binary.LittleEndian.PutUint32(buf[pos:], checksum)
	pos += 4

	// Trailing magic
	binary.LittleEndian.PutUint64(buf[pos:], filterMagic)

	return buf, nil
}

// ── UnmarshalBinary ───────────────────────────────────────────────────────────

// UnmarshalBinary deserializes a Filter from data produced by MarshalBinary.
//
// Rejected conditions (all return ErrCorruptFilter):
//   - data shorter than the minimum serialized size
//   - bad leading magic
//   - unsupported version
//   - zero bitCount or hashCount
//   - wordCount inconsistent with bitCount
//   - wordCount exceeding the safety cap (maxWords)
//   - bad trailing magic
//   - CRC-32 checksum mismatch
//   - impossible metadata (fpr outside (0,1))
//
// The returned filter does not share memory with data; mutating data after the
// call does not affect the filter.
func UnmarshalBinary(data []byte) (*Filter, error) {
	if len(data) < minSerialSize {
		return nil, fmt.Errorf("%w: data too short (%d bytes, need at least %d)",
			ErrCorruptFilter, len(data), minSerialSize)
	}

	// Leading magic
	gotMagic := binary.LittleEndian.Uint64(data[0:8])
	if gotMagic != filterMagic {
		return nil, fmt.Errorf("%w: bad leading magic (got %016x)", ErrCorruptFilter, gotMagic)
	}

	// Version
	version := binary.LittleEndian.Uint16(data[8:10])
	if version != filterVersion {
		return nil, fmt.Errorf("%w: unsupported version %d (want %d)",
			ErrCorruptFilter, version, filterVersion)
	}

	bitCount := binary.LittleEndian.Uint64(data[10:18])
	hashCount := binary.LittleEndian.Uint32(data[18:22])
	expectedItems := binary.LittleEndian.Uint64(data[22:30])
	insertedItems := binary.LittleEndian.Uint64(data[30:38])
	fprBits := binary.LittleEndian.Uint64(data[38:46])
	fpr := math.Float64frombits(fprBits)
	wordCount := binary.LittleEndian.Uint64(data[46:54])

	// Validate scalar fields before allocating.
	if bitCount == 0 {
		return nil, fmt.Errorf("%w: bitCount is zero", ErrCorruptFilter)
	}
	if hashCount == 0 {
		return nil, fmt.Errorf("%w: hashCount is zero", ErrCorruptFilter)
	}
	if fpr <= 0 || fpr >= 1 || math.IsNaN(fpr) || math.IsInf(fpr, 0) {
		return nil, fmt.Errorf("%w: impossible fpr %g", ErrCorruptFilter, fpr)
	}
	if expectedItems == 0 {
		return nil, fmt.Errorf("%w: expectedItems is zero", ErrCorruptFilter)
	}

	// Validate wordCount against bitCount to detect internal inconsistency.
	expectedWords := (bitCount + 63) / 64
	if wordCount != expectedWords {
		return nil, fmt.Errorf("%w: wordCount %d inconsistent with bitCount %d (expected %d words)",
			ErrCorruptFilter, wordCount, bitCount, expectedWords)
	}
	// Safety cap: reject oversized before allocation.
	if wordCount > maxWords {
		return nil, fmt.Errorf("%w: wordCount %d exceeds cap %d",
			ErrCorruptFilter, wordCount, maxWords)
	}

	// Check total length matches wordCount.
	expectedLen := serialHeaderSize + int(wordCount)*8 + serialTrailerSize
	if len(data) != expectedLen {
		return nil, fmt.Errorf("%w: data length %d inconsistent with wordCount %d (expected %d)",
			ErrCorruptFilter, len(data), wordCount, expectedLen)
	}

	// Compute the range covered by the CRC (everything before the trailer).
	crcEnd := serialHeaderSize + int(wordCount)*8
	storedCRC := binary.LittleEndian.Uint32(data[crcEnd : crcEnd+4])
	computedCRC := crc32.Checksum(data[:crcEnd], crcTable)
	if storedCRC != computedCRC {
		return nil, fmt.Errorf("%w: checksum mismatch (stored %08x computed %08x)",
			ErrCorruptFilter, storedCRC, computedCRC)
	}

	// Trailing magic (last 8 bytes).
	trailingMagic := binary.LittleEndian.Uint64(data[len(data)-8:])
	if trailingMagic != filterMagic {
		return nil, fmt.Errorf("%w: bad trailing magic (got %016x)", ErrCorruptFilter, trailingMagic)
	}

	// Decode words — defensive copy so caller may mutate data.
	words := make([]uint64, wordCount)
	wordsStart := serialHeaderSize
	for i := range words {
		words[i] = binary.LittleEndian.Uint64(data[wordsStart+i*8:])
	}

	return &Filter{
		words:         words,
		bitCount:      bitCount,
		hashCount:     hashCount,
		expectedItems: expectedItems,
		insertedItems: insertedItems,
		fpr:           fpr,
	}, nil
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// calcParams applies the standard Bloom filter formulas to compute the bit
// count m and hash count k:
//
//	m = ceil( -n * ln(p) / (ln(2)^2) )
//	k = round( (m / n) * ln(2) ), minimum 1
func calcParams(n uint64, p float64) (m uint64, k uint32) {
	ln2 := math.Log(2)
	ln2sq := ln2 * ln2

	mFloat := -float64(n) * math.Log(p) / ln2sq
	m = uint64(math.Ceil(mFloat))
	if m == 0 {
		m = 1
	}

	kFloat := float64(m) / float64(n) * ln2
	kRounded := math.Round(kFloat)
	if kRounded < 1 {
		kRounded = 1
	}
	// Cap k at 64 to prevent degenerate computation on very small p values.
	// At k=64 the false-positive rate is already astronomically low.
	if kRounded > 64 {
		kRounded = 64
	}
	k = uint32(kRounded)
	return
}

// hashKey computes two deterministic 64-bit hashes of key using FNV-1a 64-bit.
//
//	h1 = FNV-1a-64(key)
//	h2 = FNV-1a-64(key XOR-fed with h2Salt bytes)
//
// To decorrelate h2 from h1 without requiring a full second pass over key, we
// run a second FNV-1a-64 hasher seeded with the four bytes of h2Salt and then
// feed key into it. This is deterministic and salt-controlled.
//
// If h2 is zero, h2Fallback is substituted to prevent degenerate double-hashing
// where all positions collapse to h1 * (constant).
func hashKey(key []byte) (h1, h2 uint64) {
	// h1: standard FNV-1a 64-bit.
	hasher1 := fnv.New64a()
	hasher1.Write(key)
	h1 = hasher1.Sum64()

	// h2: FNV-1a 64-bit with a salt prefix.
	hasher2 := fnv.New64a()
	var salt [8]byte
	binary.LittleEndian.PutUint64(salt[:], h2Salt)
	hasher2.Write(salt[:])
	hasher2.Write(key)
	h2 = hasher2.Sum64()

	if h2 == 0 {
		h2 = h2Fallback
	}
	return
}
