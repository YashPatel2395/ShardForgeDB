package replica

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// ── Binary format ─────────────────────────────────────────────────────────────
//
// File header (written once at offset 0):
//
//	[magic  8 bytes]  "SHARDREP"
//	[version uint16]  1 (little-endian)
//
// Each record immediately follows the previous (or the header):
//
//	[recordLen uint32]  byte count of: crc32 + index + opType + keyLen + valLen + key + value
//	[crc32     uint32]  CRC-32/IEEE of the bytes covered by recordLen
//	[index     uint64]  LogIndex (1-based)
//	[opType    uint8]   1=Put, 2=Delete
//	[keyLen    uint32]  byte length of key
//	[valueLen  uint32]  byte length of value
//	[key       bytes]
//	[value     bytes]

const (
	logMagic        = "SHARDREP"
	logVersion      = uint16(1)
	logHeaderSize   = 10 // 8 (magic) + 2 (version)
	logRecordPrefix = 4  // recordLen uint32

	// Fixed inner record size before key/value: crc32(4) + index(8) + opType(1) + keyLen(4) + valueLen(4)
	logRecordFixed = 4 + 8 + 1 + 4 + 4 // = 21
)

var (
	errLogCorrupt   = errors.New("replica: corrupt log record")
	errLogVersion   = errors.New("replica: unsupported log version")
	errLogTruncated = errors.New("replica: truncated log record")
)

// ── opLog ─────────────────────────────────────────────────────────────────────

// opLog is an append-only, durable replication log. It is not safe for
// concurrent use by itself; callers must serialise access.
type opLog struct {
	mu   sync.Mutex
	f    *os.File
	last LogIndex
	ops  []Operation // in-memory cache of all replayed operations
}

// openLog opens (or creates) the replication log at path. The containing
// directory must already exist. It replays all existing records to rebuild the
// in-memory operation list and last index.
func openLog(path string) (*opLog, error) {
	// Ensure directory exists.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("replica: log mkdir: %w", err)
	}

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("replica: open log: %w", err)
	}

	ol := &opLog{f: f}

	// Read existing content (if any).
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("replica: stat log: %w", err)
	}

	if fi.Size() == 0 {
		// New file — write header.
		if err := ol.writeHeader(); err != nil {
			f.Close()
			return nil, err
		}
		return ol, nil
	}

	// Existing file — validate header then replay records.
	if err := ol.validateHeader(); err != nil {
		f.Close()
		return nil, err
	}

	if err := ol.replayInto(func(op Operation) error {
		dup := Operation{
			Index: op.Index,
			Type:  op.Type,
			Key:   copyBytes(op.Key),
			Value: copyBytes(op.Value),
		}
		ol.ops = append(ol.ops, dup)
		ol.last = op.Index
		return nil
	}); err != nil {
		f.Close()
		return nil, err
	}

	// Seek to end for appends.
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		f.Close()
		return nil, fmt.Errorf("replica: log seek end: %w", err)
	}

	return ol, nil
}

// writeHeader writes the 10-byte file header. Called only on new files.
func (ol *opLog) writeHeader() error {
	buf := make([]byte, logHeaderSize)
	copy(buf[:8], logMagic)
	binary.LittleEndian.PutUint16(buf[8:], logVersion)
	if _, err := ol.f.Write(buf); err != nil {
		return fmt.Errorf("replica: write log header: %w", err)
	}
	return nil
}

// validateHeader reads and checks the file header.
func (ol *opLog) validateHeader() error {
	buf := make([]byte, logHeaderSize)
	if _, err := io.ReadFull(ol.f, buf); err != nil {
		return fmt.Errorf("%w: header read: %v", errLogCorrupt, err)
	}
	if string(buf[:8]) != logMagic {
		return fmt.Errorf("%w: bad magic %q", errLogCorrupt, string(buf[:8]))
	}
	v := binary.LittleEndian.Uint16(buf[8:])
	if v != logVersion {
		return fmt.Errorf("%w: got version %d", errLogVersion, v)
	}
	return nil
}

// replayInto reads every record from the current file position (after header)
// and calls fn for each. Stops at EOF. Returns error on corruption.
func (ol *opLog) replayInto(fn func(Operation) error) error {
	for {
		// Read recordLen prefix.
		var lenBuf [4]byte
		_, err := io.ReadFull(ol.f, lenBuf[:])
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			if err == io.ErrUnexpectedEOF {
				return fmt.Errorf("%w: partial recordLen field", errLogTruncated)
			}
			return nil // clean EOF
		}
		if err != nil {
			return fmt.Errorf("%w: read recordLen: %v", errLogCorrupt, err)
		}
		recordLen := binary.LittleEndian.Uint32(lenBuf[:])
		if recordLen < uint32(logRecordFixed) {
			return fmt.Errorf("%w: recordLen %d too small", errLogCorrupt, recordLen)
		}

		body := make([]byte, recordLen)
		if _, err := io.ReadFull(ol.f, body); err != nil {
			return fmt.Errorf("%w: record body truncated (need %d bytes): %v", errLogTruncated, recordLen, err)
		}

		// CRC check: covers body[4:] (everything after the stored crc itself).
		storedCRC := binary.LittleEndian.Uint32(body[:4])
		computedCRC := crc32.ChecksumIEEE(body[4:])
		if storedCRC != computedCRC {
			return fmt.Errorf("%w: crc mismatch stored=%08x computed=%08x", errLogCorrupt, storedCRC, computedCRC)
		}

		// Decode fields.
		off := 4
		index := LogIndex(binary.LittleEndian.Uint64(body[off:]))
		off += 8
		opType := OperationType(body[off])
		off++
		keyLen := binary.LittleEndian.Uint32(body[off:])
		off += 4
		valLen := binary.LittleEndian.Uint32(body[off:])
		off += 4

		expectedTotal := uint32(logRecordFixed) + keyLen + valLen
		if recordLen != expectedTotal {
			return fmt.Errorf("%w: recordLen %d != expected %d", errLogCorrupt, recordLen, expectedTotal)
		}

		key := make([]byte, keyLen)
		copy(key, body[off:off+int(keyLen)])
		off += int(keyLen)
		val := make([]byte, valLen)
		copy(val, body[off:off+int(valLen)])

		if err := fn(Operation{Index: index, Type: opType, Key: key, Value: val}); err != nil {
			return err
		}
	}
}

// append writes one operation to the log and updates the in-memory cache.
// The next index is last+1.
func (ol *opLog) append(op Operation) (LogIndex, error) {
	ol.mu.Lock()
	defer ol.mu.Unlock()

	idx := ol.last + 1
	op.Index = idx

	keyLen := uint32(len(op.Key))
	valLen := uint32(len(op.Value))
	recordLen := uint32(logRecordFixed) + keyLen + valLen

	// Build the body (everything that the CRC covers):
	//   index(8) + opType(1) + keyLen(4) + valueLen(4) + key + value
	inner := make([]byte, int(recordLen)-4) // recordLen - crc32
	off := 0
	binary.LittleEndian.PutUint64(inner[off:], uint64(idx))
	off += 8
	inner[off] = byte(op.Type)
	off++
	binary.LittleEndian.PutUint32(inner[off:], keyLen)
	off += 4
	binary.LittleEndian.PutUint32(inner[off:], valLen)
	off += 4
	copy(inner[off:], op.Key)
	off += int(keyLen)
	copy(inner[off:], op.Value)

	crc := crc32.ChecksumIEEE(inner)

	// Full record: recordLen(4) + crc(4) + inner
	rec := make([]byte, 4+4+len(inner))
	binary.LittleEndian.PutUint32(rec[0:], recordLen)
	binary.LittleEndian.PutUint32(rec[4:], crc)
	copy(rec[8:], inner)

	if _, err := ol.f.Write(rec); err != nil {
		return 0, fmt.Errorf("replica: log append: %w", err)
	}

	// Update in-memory state.
	ol.last = idx
	dup := Operation{
		Index: idx,
		Type:  op.Type,
		Key:   copyBytes(op.Key),
		Value: copyBytes(op.Value),
	}
	ol.ops = append(ol.ops, dup)
	return idx, nil
}

// replay calls fn for every stored operation in order. Used on Open.
func (ol *opLog) replay(fn func(Operation) error) error {
	ol.mu.Lock()
	defer ol.mu.Unlock()
	for _, op := range ol.ops {
		if err := fn(op); err != nil {
			return err
		}
	}
	return nil
}

// operationsAfter returns up to limit operations with Index > after.
// If limit <= 0, all matching operations are returned.
func (ol *opLog) operationsAfter(after LogIndex, limit int) []Operation {
	ol.mu.Lock()
	defer ol.mu.Unlock()

	var result []Operation
	for _, op := range ol.ops {
		if op.Index <= after {
			continue
		}
		cp := Operation{
			Index: op.Index,
			Type:  op.Type,
			Key:   copyBytes(op.Key),
			Value: copyBytes(op.Value),
		}
		result = append(result, cp)
		if limit > 0 && len(result) >= limit {
			break
		}
	}
	return result
}

// lastIndex returns the index of the last appended operation.
func (ol *opLog) lastIndex() LogIndex {
	ol.mu.Lock()
	defer ol.mu.Unlock()
	return ol.last
}

// close closes the underlying file.
func (ol *opLog) close() error {
	ol.mu.Lock()
	defer ol.mu.Unlock()
	return ol.f.Close()
}

// ── helpers ───────────────────────────────────────────────────────────────────

func copyBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	c := make([]byte, len(b))
	copy(c, b)
	return c
}
