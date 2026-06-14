package replnet

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"math"
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
// file on disk. Every Append is durably synced to disk before returning.
//
// Durability guarantee:
//   - Each Append writes the complete record, calls syncFn, then updates the in-memory
//     index and advances nextSeq. If write or sync fails, rollback() truncates the file
//     back to the pre-write offset (via truncateFn) and syncs the truncation so no partial
//     record is left on disk. If rollback itself fails (truncateFn, seekFn, or syncFn returns
//     an error), the log is marked poisoned and all future Append calls return ErrPoisonedLog
//     until the log is closed and reopened. Rollback errors are returned as *RollbackError so
//     callers can inspect ErrPoisonedLog, the original error, and the rollback error in chain.
//
// Injectable file operations:
//   - syncFn defaults to f.Sync (used in Append and rollback)
//   - truncateFn defaults to f.Truncate (used in rollback only)
//   - seekFn defaults to f.Seek (used in rollback only)
//
// Scope:
//   - Primary nodes only. Followers do not hold a DurableLog.
//   - No automatic log truncation in Phase 26.
//   - Goroutine-safe.
type DurableLog struct {
	mu         sync.RWMutex
	f          *os.File
	index      []indexEntry // in-memory index built from replay on open
	nextSeq    uint64       // next seq to assign; starts at 1
	closed     bool
	poisoned   bool                            // true if a rollback failure has made this log unusable
	path       string                          // full path to journal file, for Stats()
	syncFn     func() error                    // injectable; defaults to f.Sync
	truncateFn func(int64) error               // injectable; defaults to f.Truncate (rollback only)
	seekFn     func(int64, int) (int64, error) // injectable; defaults to f.Seek (rollback only)
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
	dl.syncFn = f.Sync         // default: real fsync; override in tests to inject failures
	dl.truncateFn = f.Truncate // default: real truncate; override in tests for rollback path
	dl.seekFn = f.Seek         // default: real seek; override in tests for rollback path

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

// truncateTailAndSync removes a partial-tail record at offset by truncating the file,
// seeking to the new end, and syncing to make the corrected length durable.
//
// This is used during replay to recover from a crash that left a partial record at the
// end of the journal. It uses the real file methods (not the injectable seams) because
// it runs during OpenDurableLog before tests have a chance to override them — and because
// partial-tail recovery must always be durable.
//
// Returns an error if truncate, seek, or sync fails; OpenDurableLog will then fail rather
// than returning a log whose tail correction could not be persisted.
func (dl *DurableLog) truncateTailAndSync(offset int64) error {
	if err := dl.f.Truncate(offset); err != nil {
		return fmt.Errorf("replnet: durable log: truncate tail at %d: %w", offset, err)
	}
	if _, err := dl.f.Seek(offset, io.SeekStart); err != nil {
		return fmt.Errorf("replnet: durable log: seek after tail truncate: %w", err)
	}
	if err := dl.f.Sync(); err != nil {
		return fmt.Errorf("replnet: durable log: sync after tail truncate: %w", err)
	}
	return nil
}

// replay reads all records from the journal and populates the in-memory index.
//
// Sequence invariants enforced during replay:
//   - seq 0 is never valid (sequences start at 1).
//   - The first record in the journal must have seq == 1.
//   - Each subsequent seq must equal prevSeq + 1 (no gaps, no duplicates, no regressions).
//   - seq == math.MaxUint64 is rejected: nextSeq = seq+1 would wrap to 0.
//
// Partial-tail handling:
//   - EOF at record boundary → clean stop.
//   - io.ErrUnexpectedEOF reading length or body → partial tail; truncateTailAndSync removes
//     the partial record and syncs the corrected length. OpenDurableLog fails if this sync fails.
//   - CRC mismatch on a complete record → ErrCorruptedJournal (hard error, not truncated).
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
				// Partial length field: crash mid-write. Truncate to last good record
				// and sync so the corrected file length is durable.
				return dl.truncateTailAndSync(offset)
			}
			return err
		}
		recLen := binary.LittleEndian.Uint32(lenBuf[:])

		// Read the remainder of the record (crc + payload).
		body := make([]byte, recLen)
		if _, err := io.ReadFull(dl.f, body); err != nil {
			if err == io.ErrUnexpectedEOF {
				// Partial record body: crash mid-write. Truncate and sync.
				return dl.truncateTailAndSync(offset)
			}
			return err
		}

		// Verify CRC (covers bytes[4:] of body, i.e. everything after the crc field).
		if recLen < 4 {
			return fmt.Errorf("%w: record too short (%d bytes) at offset %d",
				ErrCorruptedJournal, recLen, offset)
		}
		storedCRC := binary.LittleEndian.Uint32(body[:4])
		computed := crc32.ChecksumIEEE(body[4:])
		if storedCRC != computed {
			return fmt.Errorf("%w: crc mismatch at offset %d (stored=%d computed=%d)",
				ErrCorruptedJournal, offset, storedCRC, computed)
		}

		// Parse seq from payload (first 8 bytes after crc).
		payload := body[4:]
		if len(payload) < 25 {
			return fmt.Errorf("%w: payload too short at offset %d", ErrCorruptedJournal, offset)
		}
		seq := binary.LittleEndian.Uint64(payload[0:8])

		// Sequence boundary checks.
		if seq == 0 {
			return fmt.Errorf("%w: sequence 0 is invalid (sequences start at 1) at offset %d",
				ErrCorruptedJournal, offset)
		}
		if len(dl.index) == 0 {
			// First record in the journal must have seq == 1.
			if seq != 1 {
				return fmt.Errorf("%w: first journal record has seq %d, expected 1 at offset %d",
					ErrCorruptedJournal, seq, offset)
			}
		} else {
			// Subsequent records: seq must equal prevSeq + 1.
			prevSeq := dl.index[len(dl.index)-1].seq
			if seq <= prevSeq {
				return fmt.Errorf("%w: non-monotonic sequence at offset %d (got %d, previous was %d)",
					ErrCorruptedJournal, offset, seq, prevSeq)
			}
			if seq != prevSeq+1 {
				return fmt.Errorf("%w: sequence gap at offset %d (got %d, expected %d)",
					ErrCorruptedJournal, offset, seq, prevSeq+1)
			}
		}

		// Reject seq == MaxUint64: nextSeq = seq+1 would overflow to 0.
		if seq == math.MaxUint64 {
			return fmt.Errorf("%w: sequence %d at offset %d would cause nextSeq overflow to 0",
				ErrCorruptedJournal, seq, offset)
		}

		dl.index = append(dl.index, indexEntry{seq: seq, offset: offset})
		dl.nextSeq = seq + 1 // safe: seq < MaxUint64, so seq+1 > 0
	}
}

// rollback undoes a partial or unsynced Append by truncating the file back to offset
// (via truncateFn), resetting the file position (via seekFn), and syncing the truncation
// (via syncFn) to make the corrected length durable.
//
// If any step fails, the log is marked poisoned (dl.poisoned = true) and a *RollbackError
// is returned. *RollbackError.Unwrap() returns [ErrPoisonedLog, originalErr, rollbackErr],
// so callers can use errors.Is to inspect all three. Future Append calls on a poisoned log
// return ErrPoisonedLog. Closing and reopening the log clears the poisoned state.
//
// On success (all three steps pass), rollback returns nil and the caller returns originalErr
// to its own caller.
//
// Caller must hold dl.mu.Lock.
func (dl *DurableLog) rollback(offset int64, originalErr error) error {
	if err := dl.truncateFn(offset); err != nil {
		dl.poisoned = true
		return &RollbackError{Original: originalErr, Rollback: fmt.Errorf("truncate: %w", err)}
	}
	if _, err := dl.seekFn(offset, io.SeekStart); err != nil {
		dl.poisoned = true
		return &RollbackError{Original: originalErr, Rollback: fmt.Errorf("seek: %w", err)}
	}
	// Sync the truncation: if the original write landed in the page cache, the truncation
	// must also be synced to ensure the file length update is durable before we return.
	if err := dl.syncFn(); err != nil {
		dl.poisoned = true
		return &RollbackError{Original: originalErr, Rollback: fmt.Errorf("sync: %w", err)}
	}
	return nil
}

// Append records a new mutation in the journal and returns the assigned Entry.
// op must be OpPut or OpDelete; key must be non-empty.
//
// Durability ordering:
//  1. Encode the record.
//  2. Capture the current file offset (for rollback on failure).
//  3. Write the complete record (short write → rollback + error).
//  4. Call syncFn (fsync) → on failure, rollback (truncate + sync) + error.
//  5. Only after both write and sync succeed: update in-memory index and nextSeq.
//  6. Return the committed Entry.
//
// Returns ErrClosed if the log is closed.
// Returns ErrPoisonedLog if a previous rollback failure rendered the log unusable.
// Returns ErrInvalidEntry if op or key is invalid.
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
	if dl.poisoned {
		return Entry{}, ErrPoisonedLog
	}

	// Guard against uint64 overflow. nextSeq is always < MaxUint64 because:
	//   - replay() rejects seq == MaxUint64 (would make nextSeq = 0)
	//   - Append checks here before using nextSeq
	if dl.nextSeq == math.MaxUint64 {
		return Entry{}, fmt.Errorf("replnet: durable log: sequence number overflow: nextSeq=%d", dl.nextSeq)
	}

	seq := dl.nextSeq
	tsNano := time.Now().UnixNano()

	rec, err := encodeJournalRecord(seq, op, key, value, tsNano)
	if err != nil {
		return Entry{}, fmt.Errorf("replnet: durable log: encode record: %w", err)
	}

	// Capture offset before write (used for rollback on write or sync failure).
	offset, err := dl.f.Seek(0, io.SeekCurrent)
	if err != nil {
		return Entry{}, fmt.Errorf("replnet: durable log: seek: %w", err)
	}

	// Step 1: write complete record.
	n, writeErr := dl.f.Write(rec)
	if writeErr != nil || n != len(rec) {
		var origErr error
		if writeErr != nil {
			origErr = fmt.Errorf("replnet: durable log: write record: %w", writeErr)
		} else {
			origErr = fmt.Errorf("replnet: durable log: short write: wrote %d of %d bytes", n, len(rec))
		}
		if rbErr := dl.rollback(offset, origErr); rbErr != nil {
			return Entry{}, rbErr // rollback failed → log is poisoned
		}
		return Entry{}, origErr // rollback succeeded → return original write error
	}

	// Step 2: sync to disk. On failure, rollback (truncate + sync the truncation).
	if syncErr := dl.syncFn(); syncErr != nil {
		origErr := fmt.Errorf("replnet: durable log: sync: %w", syncErr)
		if rbErr := dl.rollback(offset, origErr); rbErr != nil {
			return Entry{}, rbErr // rollback failed → log is poisoned
		}
		return Entry{}, origErr // rollback succeeded → return original sync error
	}

	// Both write and sync succeeded — now commit to in-memory state.
	dl.index = append(dl.index, indexEntry{seq: seq, offset: offset})
	dl.nextSeq++ // safe: seq < MaxUint64, so nextSeq = seq+1 > 0

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
// Durable is true only when the DurableLog implementation is active (always true here).
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
