package vector

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"math"
)

// Binary record format (all little-endian):
//
//	[magic      8 bytes  ]  — 0x5348415244564543 ("SHARDVEC")
//	[version    2 bytes  ]  — uint16, currently 1
//	[dimension  4 bytes  ]  — uint32
//	[metaLen    4 bytes  ]  — uint32, length of metadata in bytes
//	[vector     dim×4 B  ]  — float32 values, little-endian
//	[metadata   metaLen B]  — raw bytes (may be empty)
//	[crc32      4 bytes  ]  — IEEE CRC-32 over [version..metadata]
//	[magic      8 bytes  ]  — same magic footer

const (
	recordMagic   uint64 = 0x5348415244564543 // "SHARDVEC"
	recordVersion uint16 = 1
	headerSize           = 8 + 2 + 4 + 4 // magic + version + dim + metaLen
	footerSize           = 4 + 8         // crc32 + magic
	minRecordSize        = headerSize + footerSize
)

var crcTable = crc32.MakeTable(crc32.IEEE)

// encodeRecord encodes a vector and its metadata into a binary blob.
// Returns a defensive copy; caller may free the inputs after this returns.
func encodeRecord(dim int, vector []float32, metadata []byte) ([]byte, error) {
	if len(vector) != dim {
		return nil, fmt.Errorf("%w: got %d, want %d", ErrInvalidVector, len(vector), dim)
	}

	metaLen := len(metadata)
	bodyLen := 2 + 4 + 4 + dim*4 + metaLen // version + dim + metaLen + vector + meta
	total := 8 + bodyLen + 4 + 8           // header-magic + body + crc + footer-magic

	buf := make([]byte, total)
	off := 0

	// Header magic.
	binary.LittleEndian.PutUint64(buf[off:], recordMagic)
	off += 8

	bodyStart := off

	// Version.
	binary.LittleEndian.PutUint16(buf[off:], recordVersion)
	off += 2

	// Dimension.
	binary.LittleEndian.PutUint32(buf[off:], uint32(dim))
	off += 4

	// Metadata length.
	binary.LittleEndian.PutUint32(buf[off:], uint32(metaLen))
	off += 4

	// Vector.
	for _, f := range vector {
		binary.LittleEndian.PutUint32(buf[off:], math.Float32bits(f))
		off += 4
	}

	// Metadata.
	copy(buf[off:], metadata)
	off += metaLen

	bodyEnd := off

	// CRC over body [version..metadata].
	checksum := crc32.Checksum(buf[bodyStart:bodyEnd], crcTable)
	binary.LittleEndian.PutUint32(buf[off:], checksum)
	off += 4

	// Footer magic.
	binary.LittleEndian.PutUint64(buf[off:], recordMagic)
	off += 8

	if off != total {
		panic("vector codec: size mismatch") // programming error
	}

	return buf, nil
}

// decodeRecord decodes a binary blob produced by encodeRecord.
// Returns defensive copies of the vector and metadata.
// Returns ErrCorruptRecord for any structural or checksum problem.
// Returns ErrCorruptRecord with dimension mismatch detail if wantDim != decoded dim.
func decodeRecord(data []byte, wantDim int) (vector []float32, metadata []byte, err error) {
	if len(data) < minRecordSize {
		return nil, nil, fmt.Errorf("%w: record too short (%d bytes)", ErrCorruptRecord, len(data))
	}

	off := 0

	// Header magic.
	magic := binary.LittleEndian.Uint64(data[off:])
	off += 8
	if magic != recordMagic {
		return nil, nil, fmt.Errorf("%w: bad header magic %016x", ErrCorruptRecord, magic)
	}

	bodyStart := off

	// Version.
	version := binary.LittleEndian.Uint16(data[off:])
	off += 2
	if version != recordVersion {
		return nil, nil, fmt.Errorf("%w: unsupported version %d", ErrCorruptRecord, version)
	}

	// Dimension.
	dim := int(binary.LittleEndian.Uint32(data[off:]))
	off += 4
	if dim != wantDim {
		return nil, nil, fmt.Errorf("%w: dimension mismatch: got %d, want %d", ErrCorruptRecord, dim, wantDim)
	}

	// Metadata length.
	metaLen := int(binary.LittleEndian.Uint32(data[off:]))
	off += 4

	// Check remaining length before reading variable-length fields.
	needed := off + dim*4 + metaLen + footerSize
	if len(data) < needed {
		return nil, nil, fmt.Errorf("%w: truncated (need %d, have %d)", ErrCorruptRecord, needed, len(data))
	}

	// Vector.
	vec := make([]float32, dim)
	for i := range vec {
		bits := binary.LittleEndian.Uint32(data[off:])
		vec[i] = math.Float32frombits(bits)
		off += 4
	}

	// Metadata (defensive copy).
	var meta []byte
	if metaLen > 0 {
		meta = make([]byte, metaLen)
		copy(meta, data[off:off+metaLen])
	}
	off += metaLen

	bodyEnd := off

	// CRC.
	stored := binary.LittleEndian.Uint32(data[off:])
	off += 4
	computed := crc32.Checksum(data[bodyStart:bodyEnd], crcTable)
	if stored != computed {
		return nil, nil, fmt.Errorf("%w: CRC mismatch (stored %08x, computed %08x)",
			ErrCorruptRecord, stored, computed)
	}

	// Footer magic.
	footerMagic := binary.LittleEndian.Uint64(data[off:])
	off += 8
	if footerMagic != recordMagic {
		return nil, nil, fmt.Errorf("%w: bad footer magic %016x", ErrCorruptRecord, footerMagic)
	}

	// Reject trailing bytes after the footer — appended data is treated as corrupt.
	if off != len(data) {
		return nil, nil, fmt.Errorf("%w: %d trailing bytes after footer", ErrCorruptRecord, len(data)-off)
	}

	return vec, meta, nil
}
