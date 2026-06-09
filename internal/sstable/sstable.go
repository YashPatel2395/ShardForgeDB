// Package sstable implements the SSTable (Sorted String Table) file format for
// ShardForgeDB.
//
// An SSTable is an immutable, sorted, on-disk file that stores key-value
// entries (PUT records) and deletion tombstones (DELETE records). It is the
// primary persistent storage unit produced when a MemTable is flushed to disk.
//
// # File layout
//
//	[header]
//	[data record 0]
//	[data record 1]
//	…
//	[data record N-1]
//	[index block]
//	[footer]
//
// # Header (10 bytes)
//
//	[magic uint64][version uint16]   — all little-endian
//
// magic   = 0x425442_44465348_53 (little-endian bytes: S H S F D B T B)
// version = 1
//
// # Data record
//
//	[recordLen uint32][crc32 uint32][seq uint64][kind uint8][keyLen uint32][valueLen uint32][key…][value…]
//
// recordLen — byte count of the body: seq+kind+keyLen+valueLen+key+value.
// crc32     — IEEE CRC-32 computed over that same body.
//
// # Index entry (per record)
//
//	[keyLen uint32][key bytes][offset uint64][recordLen uint32]
//
// offset is the file offset of the corresponding data record header (recordLen
// field). An index entry occupies 4+len(key)+8+4 bytes.
//
// # Footer (36 bytes)
//
//	[indexOffset uint64][indexLen uint64][entryCount uint64][footerCRC uint32][magic uint64]
//
// footerCRC is IEEE CRC-32 over [indexOffset][indexLen][entryCount] (24 bytes).
// The trailing magic is the same 8-byte sentinel as the header magic, used to
// confirm the file is not truncated at the tail.
//
// # Read path
//
// Open reads the last 36 bytes (footer), verifies the trailing magic and footer
// checksum, verifies header magic and version, then reads the index block into
// memory. Get binary-searches the in-memory index, seeks to the matching
// record, reads and verifies the record CRC, and decodes the entry. Scan
// iterates the relevant index range, reading records from disk in order.
//
// # Atomic creation
//
// Create writes to a temporary file in the same directory, syncs it, then
// renames it to the final path. If Create fails the final path is never
// written. Directory fsync after rename is a known limitation (see below).
//
// # Known limitations
//
//   - No Bloom filter; every Get for an existing key requires a disk seek.
//   - No block cache; every read goes to the OS page cache.
//   - One index entry per key (dense index); no sparse index.
//   - No compression or encryption.
//   - No block-level layout; the data region is a flat record stream.
//   - No compaction; callers must handle multi-SSTable lookup.
//   - SSTable is not wired to the MemTable or Engine yet.
//   - Parent directory is not fsynced after rename.
package sstable

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// ── Exported errors ───────────────────────────────────────────────────────────

// ErrClosed is returned by Get/Scan after the Reader has been closed.
var ErrClosed = errors.New("sstable: reader closed")

// ErrInvalidEntry is returned by Create when an entry is malformed (empty key,
// unknown kind).
var ErrInvalidEntry = errors.New("sstable: invalid entry")

// ErrCorruptTable is returned when a file fails a magic, version, or checksum
// check; when the file is truncated; when the index is internally inconsistent;
// or when a data record's decoded fields are semantically invalid.
var ErrCorruptTable = errors.New("sstable: corrupt table")

// ErrRecordTooLarge is returned when an entry's encoded body exceeds
// Options.MaxRecordSize.
var ErrRecordTooLarge = errors.New("sstable: record too large")

// ErrUnsortedEntries is returned by Create when entries are not in strictly
// ascending lexicographic key order.
var ErrUnsortedEntries = errors.New("sstable: entries not sorted")

// ErrDuplicateKey is returned by Create when two adjacent entries share the
// same key.
var ErrDuplicateKey = errors.New("sstable: duplicate key")

// ErrEmptyTable is returned by Create when entries is nil or empty. Empty
// SSTables are not supported in this phase.
var ErrEmptyTable = errors.New("sstable: empty table")

// ── Types ─────────────────────────────────────────────────────────────────────

// EntryKind distinguishes a live value from a deletion tombstone.
type EntryKind uint8

const (
	// EntryPut represents a live key/value pair.
	EntryPut EntryKind = 1
	// EntryDelete represents a deletion tombstone.
	EntryDelete EntryKind = 2
)

// Entry is a single record in an SSTable.
type Entry struct {
	Key   []byte
	Value []byte
	Kind  EntryKind
	Seq   uint64
}

// Options configures SSTable behaviour. Zero values apply sensible defaults.
type Options struct {
	// MaxRecordSize is the maximum encoded body size of a single record in
	// bytes. Default (0): 64 MiB.
	MaxRecordSize uint32
}

// Metadata describes an SSTable file without opening it for reads.
type Metadata struct {
	Path      string
	Count     uint64
	MinKey    []byte
	MaxKey    []byte
	SizeBytes uint64
}

// ── File format constants ──────────────────────────────────────────────────────

const (
	// magic is the 8-byte sentinel written at the start of the header and at
	// the end of the footer. "SHSFDBTB" — ShardForge DB Table Block.
	magic uint64 = 0x425442_44465348_53 // little-endian bytes: S H S F D B T B

	tableVersion uint16 = 1

	// headerSize = magic(8) + version(2)
	headerSize = 8 + 2

	// recordHeaderSize = recordLen(4) + crc32(4)
	recordHeaderSize = 4 + 4

	// bodyFixedSize = seq(8) + kind(1) + keyLen(4) + valueLen(4)
	bodyFixedSize = 8 + 1 + 4 + 4

	// indexEntryFixedWidth is the sum of the fixed-width fields in an index
	// entry: keyLen(4) + offset(8) + recordLen(4) = 16.
	// The actual on-disk size per entry is indexEntryFixedWidth + len(key).
	// Layout: [keyLen uint32][key bytes][offset uint64][recordLen uint32]
	indexEntryFixedWidth = 4 + 8 + 4

	// footerSize = indexOffset(8) + indexLen(8) + entryCount(8) + footerCRC(4) + magic(8)
	footerSize = 8 + 8 + 8 + 4 + 8

	defaultMaxRecordSize uint32 = 64 << 20 // 64 MiB
)

var crcTable = crc32.MakeTable(crc32.IEEE)

// ── indexEntry ────────────────────────────────────────────────────────────────

// indexEntry is the in-memory representation of one index record.
type indexEntry struct {
	key       []byte
	offset    uint64 // file offset of the data record header (recordLen field)
	recordLen uint32 // body length from the index (cross-checked against record header)
}

// ── Reader ────────────────────────────────────────────────────────────────────

// Reader provides read access to an immutable SSTable file.
//
// Concurrent calls to Get, Scan, Len, Metadata, and Close are safe.
type Reader struct {
	mu            sync.RWMutex
	file          *os.File
	index         []indexEntry
	meta          Metadata
	maxRecordSize uint32
	closed        bool
}

// ── Create ────────────────────────────────────────────────────────────────────

// Create writes a new SSTable to path containing the given entries.
//
// entries must be sorted in strictly ascending lexicographic key order with no
// duplicates. Create validates this. Each entry is defensively copied; caller
// mutation after the call does not affect the file.
//
// Create writes atomically: it first writes to a temporary file, syncs, then
// renames to path. If Create returns an error, path is not created or modified.
//
// Errors: ErrEmptyTable, ErrInvalidEntry, ErrDuplicateKey, ErrUnsortedEntries,
// ErrRecordTooLarge, or any OS error.
func Create(path string, entries []Entry, opts Options) (Metadata, error) {
	if path == "" {
		return Metadata{}, fmt.Errorf("%w: empty path", ErrInvalidEntry)
	}
	if len(entries) == 0 {
		return Metadata{}, ErrEmptyTable
	}
	if opts.MaxRecordSize == 0 {
		opts.MaxRecordSize = defaultMaxRecordSize
	}

	// Validate entries before touching the filesystem.
	if err := validateEntries(entries, opts.MaxRecordSize); err != nil {
		return Metadata{}, err
	}

	// Write to a temp file in the same directory so rename is atomic.
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".sstable-tmp-*")
	if err != nil {
		return Metadata{}, fmt.Errorf("sstable: create temp: %w", err)
	}
	tmpName := tmp.Name()

	meta, err := writeTable(tmp, path, entries)
	if err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return Metadata{}, err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return Metadata{}, fmt.Errorf("sstable: sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return Metadata{}, fmt.Errorf("sstable: close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return Metadata{}, fmt.Errorf("sstable: rename: %w", err)
	}
	return meta, nil
}

// ── Open ──────────────────────────────────────────────────────────────────────

// Open opens an existing SSTable for reading.
//
// Open verifies the header magic and version, footer magic and checksum, then
// loads the index block into memory. Data records are not read at open time.
// The caller must call Close when done.
func Open(path string, opts Options) (*Reader, error) {
	if opts.MaxRecordSize == 0 {
		opts.MaxRecordSize = defaultMaxRecordSize
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("sstable: open %q: %w", path, err)
	}

	r := &Reader{file: f, maxRecordSize: opts.MaxRecordSize}
	if err := r.load(path); err != nil {
		f.Close()
		return nil, err
	}
	return r, nil
}

// ── Reader methods ────────────────────────────────────────────────────────────

// Get returns the entry for key. Returns (Entry{}, false, nil) if the key is
// not present. Returns tombstones. The returned entry is a deep copy.
func (r *Reader) Get(key []byte) (Entry, bool, error) {
	if len(key) == 0 {
		return Entry{}, false, nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.closed {
		return Entry{}, false, ErrClosed
	}

	idx := sort.Search(len(r.index), func(i int) bool {
		return string(r.index[i].key) >= string(key)
	})
	if idx >= len(r.index) || string(r.index[idx].key) != string(key) {
		return Entry{}, false, nil
	}

	e, err := r.readRecord(r.index[idx])
	if err != nil {
		return Entry{}, false, err
	}
	return e, true, nil
}

// Scan returns all entries whose keys fall in [start, end) in ascending order.
//
// Boundary rules:
//   - start is inclusive; nil/empty means beginning.
//   - end is exclusive; nil/empty means end.
//   - Tombstones are included.
//   - All returned entries are deep copies.
func (r *Reader) Scan(start, end []byte) ([]Entry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.closed {
		return nil, ErrClosed
	}

	startIdx := 0
	if len(start) > 0 {
		startIdx = sort.Search(len(r.index), func(i int) bool {
			return string(r.index[i].key) >= string(start)
		})
	}

	hasEnd := len(end) > 0
	endStr := string(end)

	var result []Entry
	for i := startIdx; i < len(r.index); i++ {
		if hasEnd && string(r.index[i].key) >= endStr {
			break
		}
		e, err := r.readRecord(r.index[i])
		if err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, nil
}

// Len returns the number of entries in the SSTable (live + tombstones).
func (r *Reader) Len() uint64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.meta.Count
}

// Metadata returns a copy of the SSTable metadata. MinKey and MaxKey are deep
// copies.
func (r *Reader) Metadata() Metadata {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return copyMetadata(r.meta)
}

// Close closes the underlying file. Subsequent Get/Scan calls return ErrClosed.
// Calling Close on an already-closed Reader returns ErrClosed.
func (r *Reader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return ErrClosed
	}
	r.closed = true
	if err := r.file.Close(); err != nil {
		return fmt.Errorf("sstable: close: %w", err)
	}
	return nil
}

// ── Internal: write ───────────────────────────────────────────────────────────

// writeTable encodes the full SSTable into w and returns metadata for path.
func writeTable(w *os.File, path string, entries []Entry) (Metadata, error) {
	// ── Header ────────────────────────────────────────────────────────────────
	var hdr [headerSize]byte
	binary.LittleEndian.PutUint64(hdr[0:8], magic)
	binary.LittleEndian.PutUint16(hdr[8:10], tableVersion)
	if _, err := w.Write(hdr[:]); err != nil {
		return Metadata{}, fmt.Errorf("sstable: write header: %w", err)
	}

	// ── Data records ──────────────────────────────────────────────────────────
	offset := uint64(headerSize)
	type idxRecord struct {
		key    []byte
		offset uint64
		recLen uint32
	}
	idxRecords := make([]idxRecord, len(entries))

	for i, e := range entries {
		body := encodeBody(e)
		crc := crc32.Checksum(body, crcTable)
		recLen := uint32(len(body))

		var recHdr [recordHeaderSize]byte
		binary.LittleEndian.PutUint32(recHdr[0:4], recLen)
		binary.LittleEndian.PutUint32(recHdr[4:8], crc)

		if _, err := w.Write(recHdr[:]); err != nil {
			return Metadata{}, fmt.Errorf("sstable: write record header: %w", err)
		}
		if _, err := w.Write(body); err != nil {
			return Metadata{}, fmt.Errorf("sstable: write record body: %w", err)
		}

		keyCopy := make([]byte, len(e.Key))
		copy(keyCopy, e.Key)
		idxRecords[i] = idxRecord{
			key:    keyCopy,
			offset: offset,
			recLen: recLen,
		}
		offset += uint64(recordHeaderSize) + uint64(recLen)
	}

	// ── Index block ───────────────────────────────────────────────────────────
	// On-disk index entry layout: [keyLen uint32][key bytes][offset uint64][recordLen uint32]
	indexOffset := offset
	for _, ir := range idxRecords {
		kl := uint32(len(ir.key))

		// keyLen
		var klBuf [4]byte
		binary.LittleEndian.PutUint32(klBuf[:], kl)
		if _, err := w.Write(klBuf[:]); err != nil {
			return Metadata{}, fmt.Errorf("sstable: write index keyLen: %w", err)
		}
		// key
		if _, err := w.Write(ir.key); err != nil {
			return Metadata{}, fmt.Errorf("sstable: write index key: %w", err)
		}
		// offset + recordLen
		var trailBuf [12]byte
		binary.LittleEndian.PutUint64(trailBuf[0:8], ir.offset)
		binary.LittleEndian.PutUint32(trailBuf[8:12], ir.recLen)
		if _, err := w.Write(trailBuf[:]); err != nil {
			return Metadata{}, fmt.Errorf("sstable: write index offset+recLen: %w", err)
		}
	}

	afterIndex := indexOffset
	for _, ir := range idxRecords {
		afterIndex += uint64(indexEntryFixedWidth) + uint64(len(ir.key))
	}
	indexLen := afterIndex - indexOffset

	// ── Footer ────────────────────────────────────────────────────────────────
	entryCount := uint64(len(entries))
	var footerFields [24]byte // indexOffset(8)+indexLen(8)+entryCount(8)
	binary.LittleEndian.PutUint64(footerFields[0:8], indexOffset)
	binary.LittleEndian.PutUint64(footerFields[8:16], indexLen)
	binary.LittleEndian.PutUint64(footerFields[16:24], entryCount)
	footerCRC := crc32.Checksum(footerFields[:], crcTable)

	var footer [footerSize]byte
	copy(footer[0:24], footerFields[:])
	binary.LittleEndian.PutUint32(footer[24:28], footerCRC)
	binary.LittleEndian.PutUint64(footer[28:36], magic)
	if _, err := w.Write(footer[:]); err != nil {
		return Metadata{}, fmt.Errorf("sstable: write footer: %w", err)
	}

	totalSize := afterIndex + footerSize

	minKey := make([]byte, len(entries[0].Key))
	copy(minKey, entries[0].Key)
	maxKey := make([]byte, len(entries[len(entries)-1].Key))
	copy(maxKey, entries[len(entries)-1].Key)

	return Metadata{
		Path:      path,
		Count:     entryCount,
		MinKey:    minKey,
		MaxKey:    maxKey,
		SizeBytes: totalSize,
	}, nil
}

// encodeBody serialises the variable body of a record (everything after the
// frame header).
func encodeBody(e Entry) []byte {
	body := make([]byte, bodyFixedSize+len(e.Key)+len(e.Value))
	binary.LittleEndian.PutUint64(body[0:8], e.Seq)
	body[8] = uint8(e.Kind)
	binary.LittleEndian.PutUint32(body[9:13], uint32(len(e.Key)))
	binary.LittleEndian.PutUint32(body[13:17], uint32(len(e.Value)))
	copy(body[bodyFixedSize:], e.Key)
	copy(body[bodyFixedSize+len(e.Key):], e.Value)
	return body
}

// ── Internal: read ────────────────────────────────────────────────────────────

// load reads the header, footer, and index block from an already-open file.
// It validates magic bytes, header version, footer checksum, and index
// consistency. r.maxRecordSize must be set before calling load.
func (r *Reader) load(path string) error {
	fi, err := r.file.Stat()
	if err != nil {
		return fmt.Errorf("sstable: stat: %w", err)
	}
	fileSize := fi.Size()

	minSize := int64(headerSize + footerSize)
	if fileSize < minSize {
		return fmt.Errorf("%w: file too small (%d bytes)", ErrCorruptTable, fileSize)
	}

	// ── Verify header magic and version ──────────────────────────────────────
	var hdr [headerSize]byte
	if _, err := io.ReadFull(r.file, hdr[:]); err != nil {
		return fmt.Errorf("%w: cannot read header: %v", ErrCorruptTable, err)
	}
	gotMagic := binary.LittleEndian.Uint64(hdr[0:8])
	if gotMagic != magic {
		return fmt.Errorf("%w: bad header magic (got %016x)", ErrCorruptTable, gotMagic)
	}
	gotVersion := binary.LittleEndian.Uint16(hdr[8:10])
	if gotVersion != tableVersion {
		return fmt.Errorf("%w: unsupported version %d (want %d)", ErrCorruptTable, gotVersion, tableVersion)
	}

	// ── Read and verify footer ────────────────────────────────────────────────
	if _, err := r.file.Seek(-int64(footerSize), io.SeekEnd); err != nil {
		return fmt.Errorf("sstable: seek to footer: %w", err)
	}
	var footer [footerSize]byte
	if _, err := io.ReadFull(r.file, footer[:]); err != nil {
		return fmt.Errorf("%w: cannot read footer: %v", ErrCorruptTable, err)
	}

	// Verify trailing magic.
	trailingMagic := binary.LittleEndian.Uint64(footer[28:36])
	if trailingMagic != magic {
		return fmt.Errorf("%w: bad footer magic (got %016x)", ErrCorruptTable, trailingMagic)
	}

	// Verify footer CRC.
	storedFooterCRC := binary.LittleEndian.Uint32(footer[24:28])
	computedFooterCRC := crc32.Checksum(footer[0:24], crcTable)
	if storedFooterCRC != computedFooterCRC {
		return fmt.Errorf("%w: footer checksum mismatch (stored %08x computed %08x)",
			ErrCorruptTable, storedFooterCRC, computedFooterCRC)
	}

	indexOffset := binary.LittleEndian.Uint64(footer[0:8])
	indexLen := binary.LittleEndian.Uint64(footer[8:16])
	entryCount := binary.LittleEndian.Uint64(footer[16:24])

	// ── Validate index region bounds (using uint64 arithmetic) ───────────────
	// fileSize is positive; convert to uint64 once after the size check above.
	uFileSize := uint64(fileSize)
	uFooterSize := uint64(footerSize)
	uHeaderSize := uint64(headerSize)

	// The data+index region spans [headerSize, fileSize-footerSize).
	dataRegionEnd := uFileSize - uFooterSize // safe: fileSize >= headerSize+footerSize

	if indexOffset < uHeaderSize || indexOffset > dataRegionEnd {
		return fmt.Errorf("%w: indexOffset %d outside valid range [%d, %d)",
			ErrCorruptTable, indexOffset, uHeaderSize, dataRegionEnd)
	}
	if indexLen > dataRegionEnd-indexOffset {
		return fmt.Errorf("%w: index region [%d, %d+%d) extends into footer",
			ErrCorruptTable, indexOffset, indexOffset, indexLen)
	}

	// ── Read index block ──────────────────────────────────────────────────────
	if _, err := r.file.Seek(int64(indexOffset), io.SeekStart); err != nil {
		return fmt.Errorf("sstable: seek to index: %w", err)
	}
	indexData := make([]byte, indexLen)
	if _, err := io.ReadFull(r.file, indexData); err != nil {
		return fmt.Errorf("%w: cannot read index: %v", ErrCorruptTable, err)
	}

	// ── Decode index entries ──────────────────────────────────────────────────
	// On-disk layout per entry: [keyLen uint32][key bytes][offset uint64][recordLen uint32]
	index := make([]indexEntry, 0, entryCount)
	pos := 0
	for pos < len(indexData) {
		// Need at least 4 bytes for keyLen.
		if pos+4 > len(indexData) {
			return fmt.Errorf("%w: index entry truncated (keyLen)", ErrCorruptTable)
		}
		kl := binary.LittleEndian.Uint32(indexData[pos : pos+4])
		pos += 4

		// Need kl bytes for key + 12 bytes for offset+recordLen.
		// Guard against kl overflow: kl is uint32, pos+int(kl)+12 could overflow int.
		if uint64(pos)+uint64(kl)+12 > uint64(len(indexData)) {
			return fmt.Errorf("%w: index entry truncated (key+trailer)", ErrCorruptTable)
		}
		key := make([]byte, kl)
		copy(key, indexData[pos:pos+int(kl)])
		pos += int(kl)

		recOffset := binary.LittleEndian.Uint64(indexData[pos : pos+8])
		recLen := binary.LittleEndian.Uint32(indexData[pos+8 : pos+12])
		pos += 12

		index = append(index, indexEntry{key: key, offset: recOffset, recordLen: recLen})
	}

	// ── Validate decoded index ────────────────────────────────────────────────

	// Entry count must match footer.
	if uint64(len(index)) != entryCount {
		return fmt.Errorf("%w: index has %d entries but footer claims %d",
			ErrCorruptTable, len(index), entryCount)
	}

	for i, ie := range index {
		// Each record offset must point into the data region [headerSize, indexOffset).
		if ie.offset < uHeaderSize || ie.offset >= indexOffset {
			return fmt.Errorf("%w: index entry %d offset %d outside data region [%d, %d)",
				ErrCorruptTable, i, ie.offset, uHeaderSize, indexOffset)
		}
		// recordLen must be within [bodyFixedSize, maxRecordSize].
		if ie.recordLen < uint32(bodyFixedSize) {
			return fmt.Errorf("%w: index entry %d recordLen %d < bodyFixedSize %d",
				ErrCorruptTable, i, ie.recordLen, bodyFixedSize)
		}
		if ie.recordLen > r.maxRecordSize {
			return fmt.Errorf("%w: index entry %d recordLen %d > MaxRecordSize %d",
				ErrCorruptTable, i, ie.recordLen, r.maxRecordSize)
		}
		// Overflow-safe full-record boundary check: the complete record
		// [offset, offset+recordHeaderSize+recordLen) must fit inside the data region.
		recordEnd := ie.offset + uint64(recordHeaderSize) + uint64(ie.recordLen)
		if recordEnd < ie.offset || recordEnd > indexOffset {
			return fmt.Errorf("%w: record at index entry %d extends beyond data region (end %d > indexOffset %d)",
				ErrCorruptTable, i, recordEnd, indexOffset)
		}
		// Keys must be strictly sorted (no duplicates).
		if i > 0 && string(ie.key) <= string(index[i-1].key) {
			return fmt.Errorf("%w: index keys not strictly sorted at entry %d (%q <= %q)",
				ErrCorruptTable, i, ie.key, index[i-1].key)
		}
	}

	// ── Build metadata ────────────────────────────────────────────────────────
	var minKey, maxKey []byte
	if len(index) > 0 {
		minKey = make([]byte, len(index[0].key))
		copy(minKey, index[0].key)
		maxKey = make([]byte, len(index[len(index)-1].key))
		copy(maxKey, index[len(index)-1].key)
	}

	r.index = index
	r.meta = Metadata{
		Path:      path,
		Count:     entryCount,
		MinKey:    minKey,
		MaxKey:    maxKey,
		SizeBytes: uFileSize,
	}
	return nil
}

// readRecord reads and decodes one record from the file at ie's offset.
// Validates recLen bounds and consistency with the index before allocating.
// Validates the record CRC, entry kind, and that the decoded key matches the
// index key.
// Must be called with at least the read lock held.
func (r *Reader) readRecord(ie indexEntry) (Entry, error) {
	var recHdr [recordHeaderSize]byte
	if _, err := r.file.ReadAt(recHdr[:], int64(ie.offset)); err != nil {
		return Entry{}, fmt.Errorf("%w: cannot read record header: %v", ErrCorruptTable, err)
	}
	recLen := binary.LittleEndian.Uint32(recHdr[0:4])
	storedCRC := binary.LittleEndian.Uint32(recHdr[4:8])

	// Validate recLen before any allocation.
	if recLen < uint32(bodyFixedSize) {
		return Entry{}, fmt.Errorf("%w: record at offset %d has recLen %d < bodyFixedSize %d",
			ErrCorruptTable, ie.offset, recLen, bodyFixedSize)
	}
	if recLen > r.maxRecordSize {
		return Entry{}, fmt.Errorf("%w: record at offset %d has recLen %d > MaxRecordSize %d",
			ErrCorruptTable, ie.offset, recLen, r.maxRecordSize)
	}
	// Cross-check with index: the record header must agree with the index entry.
	if recLen != ie.recordLen {
		return Entry{}, fmt.Errorf("%w: record at offset %d has recLen %d but index says %d",
			ErrCorruptTable, ie.offset, recLen, ie.recordLen)
	}

	body := make([]byte, recLen)
	if _, err := r.file.ReadAt(body, int64(ie.offset)+recordHeaderSize); err != nil {
		return Entry{}, fmt.Errorf("%w: cannot read record body: %v", ErrCorruptTable, err)
	}

	// Verify CRC over the body.
	computed := crc32.Checksum(body, crcTable)
	if computed != storedCRC {
		return Entry{}, fmt.Errorf("%w: record checksum mismatch (stored %08x computed %08x)",
			ErrCorruptTable, storedCRC, computed)
	}

	// Decode body fields.
	seq := binary.LittleEndian.Uint64(body[0:8])
	kind := EntryKind(body[8])
	keyLen := binary.LittleEndian.Uint32(body[9:13])
	valueLen := binary.LittleEndian.Uint32(body[13:17])

	// Validate kind before decoding key/value.
	if kind != EntryPut && kind != EntryDelete {
		return Entry{}, fmt.Errorf("%w: record at offset %d has invalid kind %d",
			ErrCorruptTable, ie.offset, kind)
	}

	// Guard against keyLen+valueLen overflow and exceeding actual body.
	if uint64(keyLen)+uint64(valueLen) > uint64(recLen)-uint64(bodyFixedSize) {
		return Entry{}, fmt.Errorf("%w: key+value lengths exceed body", ErrCorruptTable)
	}

	key := make([]byte, keyLen)
	copy(key, body[bodyFixedSize:bodyFixedSize+keyLen])
	value := make([]byte, valueLen)
	copy(value, body[bodyFixedSize+keyLen:bodyFixedSize+keyLen+valueLen])

	// Cross-check decoded key against the index key.
	if string(key) != string(ie.key) {
		return Entry{}, fmt.Errorf("%w: record key %q does not match index key %q",
			ErrCorruptTable, key, ie.key)
	}

	return Entry{Key: key, Value: value, Kind: kind, Seq: seq}, nil
}

// ── Internal: validation ──────────────────────────────────────────────────────

// validateEntries checks all entries before any file I/O.
func validateEntries(entries []Entry, maxRecordSize uint32) error {
	for i, e := range entries {
		if len(e.Key) == 0 {
			return fmt.Errorf("%w: entry %d has empty key", ErrInvalidEntry, i)
		}
		if e.Kind != EntryPut && e.Kind != EntryDelete {
			return fmt.Errorf("%w: entry %d has unknown kind %d", ErrInvalidEntry, i, e.Kind)
		}
		// Size check using uint64 to avoid uint32 overflow.
		bodyLen := uint64(bodyFixedSize) + uint64(len(e.Key)) + uint64(len(e.Value))
		if bodyLen > uint64(maxRecordSize) {
			return fmt.Errorf("%w: entry %d body %d bytes exceeds MaxRecordSize %d",
				ErrRecordTooLarge, i, bodyLen, maxRecordSize)
		}
		// Order check.
		if i > 0 {
			cmp := compareBytes(e.Key, entries[i-1].Key)
			if cmp == 0 {
				return fmt.Errorf("%w: entries %d and %d share key %q",
					ErrDuplicateKey, i-1, i, e.Key)
			}
			if cmp < 0 {
				return fmt.Errorf("%w: entry %d key %q < entry %d key %q",
					ErrUnsortedEntries, i, e.Key, i-1, entries[i-1].Key)
			}
		}
	}
	return nil
}

func compareBytes(a, b []byte) int {
	if string(a) < string(b) {
		return -1
	}
	if string(a) > string(b) {
		return 1
	}
	return 0
}

// ── Internal: helpers ─────────────────────────────────────────────────────────

func copyMetadata(m Metadata) Metadata {
	minKey := make([]byte, len(m.MinKey))
	copy(minKey, m.MinKey)
	maxKey := make([]byte, len(m.MaxKey))
	copy(maxKey, m.MaxKey)
	return Metadata{
		Path:      m.Path,
		Count:     m.Count,
		MinKey:    minKey,
		MaxKey:    maxKey,
		SizeBytes: m.SizeBytes,
	}
}
