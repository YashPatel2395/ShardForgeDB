// Package wal implements the Write-Ahead Log (WAL) for ShardForgeDB.
//
// Every mutation destined for the storage engine is first durably recorded
// here before being applied to the in-memory MemTable. In the event of a
// crash the WAL is replayed to reconstruct any writes that were not yet
// flushed to an SSTable.
//
// # Binary record format
//
// Each record is written as a fixed-size frame header followed by a
// variable-length body. All integers are little-endian.
//
//	┌─────────┬─────────┬─────────┬──────┬────────┬──────────┬────────────────┐
//	│ length  │  crc32  │   seq   │ type │ keyLen │ valueLen │  key … value … │
//	│ uint32  │ uint32  │ uint64  │uint8 │ uint32 │  uint32  │   (raw bytes)  │
//	│  4 B    │  4 B    │  8 B    │ 1 B  │  4 B   │   4 B    │   variable     │
//	└─────────┴─────────┴─────────┴──────┴────────┴──────────┴────────────────┘
//
// "length" is the byte count of everything after the crc32 field
// (i.e. seq + type + keyLen + valueLen + key bytes + value bytes).
//
// "crc32" is IEEE CRC-32 computed over those same bytes.
//
// # Partial-tail vs. corruption
//
// A partial tail (crash mid-write) produces an incomplete header or body:
// io.ReadFull returns io.EOF or io.ErrUnexpectedEOF. That is a clean stop.
//
// A complete body whose checksum does not match the stored CRC is always
// ErrCorruptRecord, regardless of whether the record is the last one in the
// file. The distinction is intentional: a crashed write leaves bytes missing,
// not bytes present-but-wrong.
package wal

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"sync"
)

// ── Public errors ─────────────────────────────────────────────────────────────

// ErrClosed is returned by any operation on a WAL that has been closed.
var ErrClosed = errors.New("wal: closed")

// ErrInvalidRecord is returned when an Append argument is invalid (empty key,
// unknown type, …).
var ErrInvalidRecord = errors.New("wal: invalid record")

// ErrCorruptRecord is returned by Replay when a complete record's checksum
// does not match, or when a frame header contains an impossible body length.
var ErrCorruptRecord = errors.New("wal: corrupt record")

// ErrRecordTooLarge is returned when the encoded size of a record exceeds
// Options.MaxRecordSize.
var ErrRecordTooLarge = errors.New("wal: record too large")

// ── Types ─────────────────────────────────────────────────────────────────────

// RecordType identifies the operation captured by a WAL record.
type RecordType uint8

const (
	// RecordPut represents an insert or update of a key/value pair.
	RecordPut RecordType = 1
	// RecordDelete represents the deletion (tombstone) of a key.
	RecordDelete RecordType = 2
)

// Record is the unit of data written to and read from the WAL.
type Record struct {
	Type  RecordType
	Key   []byte
	Value []byte
	// Seq is the monotonically increasing sequence number assigned by Append.
	// Callers may set it before calling Append; Append overwrites it with the
	// next internal sequence number so the WAL always owns the counter.
	Seq uint64
}

// Options configures WAL behaviour. Zero values apply sensible defaults.
type Options struct {
	// SyncOnWrite calls fsync after every Append when true.
	// This guarantees durability at the cost of throughput.
	// Default: false (suitable for tests; set true in production).
	SyncOnWrite bool

	// MaxRecordSize is the maximum allowed body size in bytes.
	// Append returns ErrRecordTooLarge when the limit is exceeded.
	// Replay returns ErrCorruptRecord when a frame claims a larger body.
	// Default (0): 64 MiB.
	MaxRecordSize uint32
}

// WAL is a single-file, append-only write-ahead log.
//
// Concurrent calls to Append are safe. Replay must not be called concurrently
// with Append (the caller is expected to replay only before starting writers).
type WAL struct {
	mu     sync.Mutex
	file   *os.File
	opts   Options
	seq    uint64 // next sequence number to assign
	closed bool
}

// ── Frame layout constants ────────────────────────────────────────────────────

const (
	// frameHeaderSize is the size of [length uint32][crc32 uint32] = 8 bytes.
	frameHeaderSize = 4 + 4

	// bodyFixedSize is the fixed part of the body:
	// [seq uint64][type uint8][keyLen uint32][valueLen uint32] = 17 bytes.
	bodyFixedSize = 8 + 1 + 4 + 4

	defaultMaxRecordSize uint32 = 64 << 20 // 64 MiB
)

var crcTable = crc32.MakeTable(crc32.IEEE)

// ── Open ──────────────────────────────────────────────────────────────────────

// Open opens the WAL at path, creating it if necessary.
// Existing data is preserved; new records are appended after any existing ones.
// Call Replay before Append to recover previously written records.
func Open(path string, opts Options) (*WAL, error) {
	if opts.MaxRecordSize == 0 {
		opts.MaxRecordSize = defaultMaxRecordSize
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("wal: open %q: %w", path, err)
	}

	// Advance to EOF so new records are appended after existing ones.
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		f.Close()
		return nil, fmt.Errorf("wal: seek %q: %w", path, err)
	}

	return &WAL{file: f, opts: opts}, nil
}

// ── Append ────────────────────────────────────────────────────────────────────

// Append durably records r in the WAL and returns the sequence number assigned
// to the record.
//
// Append owns the sequence counter. The Seq field of r is ignored on input and
// set to the assigned value before encoding. This keeps the counter
// authoritative inside the WAL.
//
// Append defensively copies Key and Value so that caller mutation after the
// call does not affect replayed data.
func (w *WAL) Append(r Record) (uint64, error) {
	if err := validateRecord(r); err != nil {
		return 0, err
	}

	// Pre-check body size before any allocation to prevent OOM from huge
	// records. Use uint64 arithmetic to avoid uint32 overflow when summing
	// bodyFixedSize + len(key) + len(value).
	rawBodyLen := uint64(bodyFixedSize) + uint64(len(r.Key)) + uint64(len(r.Value))
	if rawBodyLen > uint64(w.opts.MaxRecordSize) {
		return 0, fmt.Errorf("%w: body would be %d bytes, limit is %d",
			ErrRecordTooLarge, rawBodyLen, w.opts.MaxRecordSize)
	}

	// Defensive copies — protect against caller mutation after Append returns.
	key := make([]byte, len(r.Key))
	copy(key, r.Key)
	value := make([]byte, len(r.Value))
	copy(value, r.Value)

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return 0, ErrClosed
	}

	w.seq++
	seq := w.seq

	buf := encodeRecord(seq, r.Type, key, value)

	if _, err := w.file.Write(buf); err != nil {
		return 0, fmt.Errorf("wal: write: %w", err)
	}

	if w.opts.SyncOnWrite {
		if err := w.file.Sync(); err != nil {
			return 0, fmt.Errorf("wal: sync: %w", err)
		}
	}

	return seq, nil
}

// ── Replay ────────────────────────────────────────────────────────────────────

// Replay reads records from the beginning of the WAL in append order, calling
// fn for each valid record. It stops and returns the first error returned by
// fn, or any read/corruption error.
//
// Partial-tail handling: if the file ends in the middle of a frame header or
// frame body (io.EOF / io.ErrUnexpectedEOF from io.ReadFull), Replay treats
// it as a clean stop. This is the expected outcome when a process crashes
// mid-write.
//
// Corruption: a complete body whose checksum does not match its stored CRC is
// always ErrCorruptRecord — even when it is the last record in the file. A
// crashed write leaves bytes absent, not bytes present-but-wrong.
//
// Replay must not be called concurrently with Append.
func (w *WAL) Replay(fn func(Record) error) error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return ErrClosed
	}
	w.mu.Unlock()

	// Seek to the beginning for replay.
	if _, err := w.file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("wal: replay seek: %w", err)
	}

	var maxSeq uint64

	for {
		// ── Read frame header [length uint32][crc32 uint32] ──────────────
		var header [frameHeaderSize]byte
		_, err := io.ReadFull(w.file, header[:])
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				// Partial or missing header at the tail — clean stop.
				break
			}
			return fmt.Errorf("wal: replay read header: %w", err)
		}

		bodyLen := binary.LittleEndian.Uint32(header[0:4])
		storedCRC := binary.LittleEndian.Uint32(header[4:8])

		// ── Validate body length before allocation ────────────────────────
		// A valid record always has at least bodyFixedSize bytes.
		// Anything larger than MaxRecordSize is impossible from a valid Append.
		// Both conditions indicate a corrupt frame header, not a partial tail
		// (we have a complete 8-byte header so the tail scenario does not apply).
		if bodyLen > w.opts.MaxRecordSize {
			return fmt.Errorf("%w: frame claims body of %d bytes, MaxRecordSize is %d",
				ErrCorruptRecord, bodyLen, w.opts.MaxRecordSize)
		}
		if bodyLen < bodyFixedSize {
			return fmt.Errorf("%w: frame claims body of %d bytes, minimum is %d",
				ErrCorruptRecord, bodyLen, bodyFixedSize)
		}

		// ── Read body ────────────────────────────────────────────────────
		body := make([]byte, bodyLen)
		_, err = io.ReadFull(w.file, body)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				// Partial body at the tail — clean stop.
				break
			}
			return fmt.Errorf("wal: replay read body: %w", err)
		}

		// ── Verify checksum ───────────────────────────────────────────────
		// The body is complete. A checksum mismatch is always ErrCorruptRecord.
		// We do NOT treat tail-position CRC failures as a clean stop: a crashed
		// write leaves a truncated record (caught above), not a full record with
		// wrong bytes.
		computed := crc32.Checksum(body, crcTable)
		if computed != storedCRC {
			return fmt.Errorf("%w: checksum mismatch (stored %08x, computed %08x)",
				ErrCorruptRecord, storedCRC, computed)
		}

		// ── Decode body ───────────────────────────────────────────────────
		seq := binary.LittleEndian.Uint64(body[0:8])
		rtype := RecordType(body[8])
		keyLen := binary.LittleEndian.Uint32(body[9:13])
		valueLen := binary.LittleEndian.Uint32(body[13:17])

		// Guard against keyLen+valueLen overflow and against a corrupt body
		// where the declared lengths exceed the actual body bytes.
		if uint64(keyLen)+uint64(valueLen) > uint64(bodyLen)-uint64(bodyFixedSize) {
			return fmt.Errorf("%w: key+value lengths exceed body size", ErrCorruptRecord)
		}

		key := make([]byte, keyLen)
		copy(key, body[bodyFixedSize:bodyFixedSize+keyLen])
		value := make([]byte, valueLen)
		copy(value, body[bodyFixedSize+keyLen:bodyFixedSize+keyLen+valueLen])

		if seq > maxSeq {
			maxSeq = seq
		}

		if err := fn(Record{Type: rtype, Key: key, Value: value, Seq: seq}); err != nil {
			// Re-seek to EOF so subsequent Appends land at the right offset.
			w.file.Seek(0, io.SeekEnd) //nolint:errcheck
			return err
		}
	}

	// Restore maxSeq so further Appends assign numbers higher than any replayed.
	w.mu.Lock()
	if maxSeq > w.seq {
		w.seq = maxSeq
	}
	w.mu.Unlock()

	// Re-seek to EOF for subsequent Appends.
	if _, err := w.file.Seek(0, io.SeekEnd); err != nil {
		return fmt.Errorf("wal: replay seek-to-end: %w", err)
	}

	return nil
}

// ── Close ─────────────────────────────────────────────────────────────────────

// Close flushes any OS-level buffers and closes the underlying file.
// Subsequent calls to Close return ErrClosed but are otherwise harmless.
// Append after Close returns ErrClosed.
func (w *WAL) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return ErrClosed
	}

	w.closed = true
	if err := w.file.Close(); err != nil {
		return fmt.Errorf("wal: close: %w", err)
	}

	return nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// encodeRecord serialises one record into the wire format and returns the
// complete frame (header + body). The caller must validate body size before
// calling encodeRecord.
func encodeRecord(seq uint64, rtype RecordType, key, value []byte) []byte {
	bodyLen := bodyFixedSize + len(key) + len(value)
	buf := make([]byte, frameHeaderSize+bodyLen)

	// Build body in-place starting at offset 8 (after the frame header).
	body := buf[frameHeaderSize:]
	binary.LittleEndian.PutUint64(body[0:8], seq)
	body[8] = uint8(rtype)
	binary.LittleEndian.PutUint32(body[9:13], uint32(len(key)))
	binary.LittleEndian.PutUint32(body[13:17], uint32(len(value)))
	copy(body[bodyFixedSize:], key)
	copy(body[bodyFixedSize+len(key):], value)

	checksum := crc32.Checksum(body, crcTable)

	binary.LittleEndian.PutUint32(buf[0:4], uint32(bodyLen))
	binary.LittleEndian.PutUint32(buf[4:8], checksum)

	return buf
}

// validateRecord checks the caller-supplied record before encoding.
func validateRecord(r Record) error {
	switch r.Type {
	case RecordPut, RecordDelete:
	default:
		return fmt.Errorf("%w: unknown type %d", ErrInvalidRecord, r.Type)
	}
	if len(r.Key) == 0 {
		return fmt.Errorf("%w: empty key", ErrInvalidRecord)
	}
	return nil
}
