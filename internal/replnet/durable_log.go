package replnet

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// journalFileName is the name of the binary journal file written by the primary.
const journalFileName = "replication.journal"

// journalMagicOp constants encode the operation type in the binary record.
const (
	journalOpPut    uint8 = 1
	journalOpDelete uint8 = 2
)

// journalHeaderSize is the fixed number of bytes preceding key/val in each record:
//
//	4 (length) + 4 (crc) + 8 (seq) + 1 (op) + 4 (keyLen) + 4 (valLen) + 8 (tsNano) = 33
const journalHeaderSize = 33

// indexEntry stores the file offset of a journal record for fast random access.
type indexEntry struct {
	seq    uint64
	offset int64 // byte offset of the length field for this record
}

// DurableLog is an append-only mutation log for a primary node backed by a binary journal
// file on disk. It survives process restarts.
//
// Scope:
//   - Primary nodes only. Followers do not hold a DurableLog.
//   - No automatic log truncation in Phase 26.
//   - Goroutine-safe.
type DurableLog struct {
	mu      sync.RWMutex
	f       *os.File
	index   []indexEntry // in-memory index built from replay on open
	nextSeq uint64       // next seq to assign; starts at 1
	closed  bool
	path    string // full path to journal file, for Stats()
}

// OpenDurableLog opens (or creates) the binary journal file in dataDir and replays
// all existing records to rebuild the in-memory index. Returns a ready-to-use DurableLog.
func OpenDurableLog(dataDir string) (*DurableLog, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("replnet: durable log: mkdir %s: %w", dataDir, err)
	}
	path := filepath.Join(dataDir, journalFileName)

	// Open for reading and appending; create if absent.
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("replnet: durable log: open %s: %w", path, err)
	}

	dl := &DurableLog{
		f:       f,
		nextSeq: 1,
		path:    path,
	}
	if err := dl.replay(); err != nil {
		f.Close()
		return nil, fmt.Errorf("replnet: durable log: replay %s: %w", path, err)
	}
	// Seek to end for appending.
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		f.Close()
		return nil, fmt.Errorf("replnet: durable log: seek end %s: %w", path, err)
	}
	return dl, nil
}

// replay reads all records from the journal and populates the in-memory index.
// A partial record at the tail (caused by a crash mid-write) is silently truncated.
func (dl *DurableLog) replay() error {
	if _, err := dl.f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	for {
		offset, err := dl.f.Seek(0, io.SeekCurrent)
		if err != nil {
			return err
		}

		// Read the 4-byte length field.
		var lenBuf [4]byte
		if _, err := io.ReadFull(dl.f, lenBuf[:]); err != nil {
			if err == io.EOF {
				return nil // clean end of file
			}
			if err == io.ErrUnexpectedEOF {
				// Partial length field: crash mid-write. Truncate to last good record.
				return dl.f.Truncate(offset)
			}
			return err
		}
		recLen := binary.LittleEndian.Uint32(lenBuf[:])

		// Read the remainder of the record (crc + payload).
		body := make([]byte, recLen)
		if _, err := io.ReadFull(dl.f, body); err != nil {
			if err == io.ErrUnexpectedEOF {
				// Partial record body: crash mid-write. Truncate.
				return dl.f.Truncate(offset)
			}
			return err
		}

		// Verify CRC (covers bytes[4:] of body, i.e. everything after the crc field).
		if recLen < 4 {
			return fmt.Errorf("%w: record too short (%d bytes)", ErrCorruptedJournal, recLen)
		}
		storedCRC := binary.LittleEndian.Uint32(body[:4])
		computed := crc32.ChecksumIEEE(body[4:])
		if storedCRC != computed {
			return fmt.Errorf("%w: crc mismatch at offset %d (stored=%d computed=%d)",
				ErrCorruptedJournal, offset, storedCRC, computed)
		}

		// Parse payload: seq(8) op(1) keyLen(4) valLen(4) tsNano(8) key val
		payload := body[4:]
		if len(payload) < 25 {
			return fmt.Errorf("%w: payload too short at offset %d", ErrCorruptedJournal, offset)
		}
		seq := binary.LittleEndian.Uint64(payload[0:8])

		dl.index = append(dl.index, indexEntry{seq: seq, offset: offset})
		dl.nextSeq = seq + 1
	}
}

// Append records a new mutation in the journal and returns the assigned Entry.
// op must be OpPut or OpDelete; key must be non-empty.
// Returns ErrClosed if the log is closed.
func (dl *DurableLog) Append(op Operation, key, value string) (Entry, error) {
	if op != OpPut && op != OpDelete {
		return Entry{}, fmt.Errorf("%w: op must be %q or %q", ErrInvalidEntry, OpPut, OpDelete)
	}
	if key == "" {
		return Entry{}, fmt.Errorf("%w: key must not be empty", ErrInvalidEntry)
	}

	dl.mu.Lock()
	defer dl.mu.Unlock()

	if dl.closed {
		return Entry{}, ErrClosed
	}

	seq := dl.nextSeq
	tsNano := time.Now().UnixNano()

	rec, err := encodeJournalRecord(seq, op, key, value, tsNano)
	if err != nil {
		return Entry{}, fmt.Errorf("replnet: durable log: encode record: %w", err)
	}

	// Capture offset before write.
	offset, err := dl.f.Seek(0, io.SeekCurrent)
	if err != nil {
		return Entry{}, fmt.Errorf("replnet: durable log: seek: %w", err)
	}
	if _, err := dl.f.Write(rec); err != nil {
		return Entry{}, fmt.Errorf("replnet: durable log: write record: %w", err)
	}

	dl.index = append(dl.index, indexEntry{seq: seq, offset: offset})
	dl.nextSeq++

	ts := time.Unix(0, tsNano).UTC()
	return Entry{Seq: seq, Op: op, Key: key, Value: value, Timestamp: ts}, nil
}

// encodeJournalRecord serialises a mutation into the binary journal format.
//
// Format:
//
//	[4] length uint32 LE  — bytes after this field
//	[4] crc32  uint32 LE  — IEEE CRC32 of payload
//	[8] seq    uint64 LE
//	[1] op     uint8      — journalOpPut or journalOpDelete
//	[4] keyLen uint32 LE
//	[4] valLen uint32 LE
//	[8] tsNano uint64 LE
//	[keyLen] key bytes
//	[valLen] val bytes
func encodeJournalRecord(seq uint64, op Operation, key, value string, tsNano int64) ([]byte, error) {
	keyB := []byte(key)
	valB := []byte(value)
	keyLen := uint32(len(keyB))
	valLen := uint32(len(valB))

	// payload = crc(4) + seq(8) + op(1) + keyLen(4) + valLen(4) + tsNano(8) + key + val
	payloadLen := uint32(4 + 8 + 1 + 4 + 4 + 8 + keyLen + valLen)
	buf := make([]byte, 4+payloadLen) // 4 for the length field itself

	// Write length field.
	binary.LittleEndian.PutUint32(buf[0:4], payloadLen)

	// Build payload (everything after crc field).
	payload := buf[8:] // buf[4:8] is crc, buf[8:] is payload
	binary.LittleEndian.PutUint64(payload[0:8], seq)
	var opByte uint8
	if op == OpPut {
		opByte = journalOpPut
	} else {
		opByte = journalOpDelete
	}
	payload[8] = opByte
	binary.LittleEndian.PutUint32(payload[9:13], keyLen)
	binary.LittleEndian.PutUint32(payload[13:17], valLen)
	binary.LittleEndian.PutUint64(payload[17:25], uint64(tsNano))
	copy(payload[25:25+keyLen], keyB)
	copy(payload[25+keyLen:], valB)

	// Compute and write CRC.
	checksum := crc32.ChecksumIEEE(buf[8:])
	binary.LittleEndian.PutUint32(buf[4:8], checksum)

	return buf, nil
}

// decodeJournalPayload parses the payload portion of a journal record (everything after the crc field).
func decodeJournalPayload(payload []byte) (Entry, error) {
	if len(payload) < 25 {
		return Entry{}, fmt.Errorf("%w: payload too short (%d bytes)", ErrCorruptedJournal, len(payload))
	}
	seq := binary.LittleEndian.Uint64(payload[0:8])
	opByte := payload[8]
	keyLen := binary.LittleEndian.Uint32(payload[9:13])
	valLen := binary.LittleEndian.Uint32(payload[13:17])
	tsNano := binary.LittleEndian.Uint64(payload[17:25])

	expected := 25 + int(keyLen) + int(valLen)
	if len(payload) < expected {
		return Entry{}, fmt.Errorf("%w: payload truncated: need %d, have %d",
			ErrCorruptedJournal, expected, len(payload))
	}

	key := string(payload[25 : 25+keyLen])
	val := string(payload[25+keyLen : 25+keyLen+valLen])

	var op Operation
	switch opByte {
	case journalOpPut:
		op = OpPut
	case journalOpDelete:
		op = OpDelete
	default:
		return Entry{}, fmt.Errorf("%w: unknown op byte %d at seq %d", ErrCorruptedJournal, opByte, seq)
	}

	ts := time.Unix(0, int64(tsNano)).UTC()
	return Entry{Seq: seq, Op: op, Key: key, Value: val, Timestamp: ts}, nil
}

// EntriesAfter returns up to limit entries whose Seq > after, in ascending order.
// If limit <= 0, DefaultEntriesLimit is used.
// Returns ErrClosed if the log is closed.
func (dl *DurableLog) EntriesAfter(after uint64, limit int) ([]Entry, error) {
	if limit <= 0 {
		limit = DefaultEntriesLimit
	}

	dl.mu.RLock()
	defer dl.mu.RUnlock()

	if dl.closed {
		return nil, ErrClosed
	}

	// Find the first index entry with seq > after using binary search.
	lo, hi := 0, len(dl.index)
	for lo < hi {
		mid := (lo + hi) / 2
		if dl.index[mid].seq <= after {
			lo = mid + 1
		} else {
			hi = mid
		}
	}

	result := make([]Entry, 0, limit)
	for i := lo; i < len(dl.index) && len(result) < limit; i++ {
		entry, err := dl.readRecordAt(dl.index[i].offset)
		if err != nil {
			return nil, err
		}
		result = append(result, entry)
	}
	return result, nil
}

// readRecordAt reads and decodes the journal record whose length field starts at offset.
// Caller must hold at least dl.mu.RLock.
func (dl *DurableLog) readRecordAt(offset int64) (Entry, error) {
	var lenBuf [4]byte
	if _, err := dl.f.ReadAt(lenBuf[:], offset); err != nil {
		return Entry{}, fmt.Errorf("replnet: read length at %d: %w", offset, err)
	}
	recLen := binary.LittleEndian.Uint32(lenBuf[:])

	body := make([]byte, recLen)
	if _, err := dl.f.ReadAt(body, offset+4); err != nil {
		return Entry{}, fmt.Errorf("replnet: read body at %d: %w", offset+4, err)
	}

	if recLen < 4 {
		return Entry{}, fmt.Errorf("%w: record too short at offset %d", ErrCorruptedJournal, offset)
	}
	storedCRC := binary.LittleEndian.Uint32(body[:4])
	computed := crc32.ChecksumIEEE(body[4:])
	if storedCRC != computed {
		return Entry{}, fmt.Errorf("%w: crc mismatch at offset %d", ErrCorruptedJournal, offset)
	}

	return decodeJournalPayload(body[4:])
}

// FirstAvailableSeq returns the lowest sequence number present in the journal.
// Returns 0 if the journal is empty. Used for gap detection.
func (dl *DurableLog) FirstAvailableSeq() uint64 {
	dl.mu.RLock()
	defer dl.mu.RUnlock()
	if len(dl.index) == 0 {
		return 0
	}
	return dl.index[0].seq
}

// Stats returns a point-in-time snapshot of the journal's state.
// Returns ErrClosed if the log is closed.
func (dl *DurableLog) Stats() (DurableLogStats, error) {
	dl.mu.RLock()
	defer dl.mu.RUnlock()

	if dl.closed {
		return DurableLogStats{}, ErrClosed
	}

	var lastSeq uint64
	var firstSeq uint64
	n := len(dl.index)
	if n > 0 {
		firstSeq = dl.index[0].seq
		lastSeq = dl.index[n-1].seq
	}

	fi, err := dl.f.Stat()
	var journalBytes int64
	if err == nil {
		journalBytes = fi.Size()
	}

	return DurableLogStats{
		Count:             n,
		LastSeq:           lastSeq,
		FirstAvailableSeq: firstSeq,
		JournalBytes:      journalBytes,
		Durable:           true,
	}, nil
}

// Close flushes and closes the underlying journal file.
// Safe to call multiple times.
func (dl *DurableLog) Close() error {
	dl.mu.Lock()
	defer dl.mu.Unlock()
	if dl.closed {
		return nil
	}
	dl.closed = true
	return dl.f.Close()
}
