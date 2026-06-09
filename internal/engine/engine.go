// Package engine implements a single-node key-value storage engine for
// ShardForgeDB by composing the WAL, MemTable, SSTable, and Bloom Filter
// packages into a unified read/write interface.
//
// # Architecture
//
// The engine follows a standard LSM-tree write path:
//
//	Write → WAL (durable) → MemTable (in-memory)
//	                              ↓ Flush (manual)
//	                         SSTable + Bloom sidecar
//	                              ↓ saved to MANIFEST.json
//
// Reads check the MemTable first, then SSTables from newest to oldest.
// Each SSTable lookup is preceded by a Bloom filter check to skip files that
// definitely do not contain the requested key.
//
// # File layout
//
//	<Dir>/
//	  wal.log               — append-only write-ahead log
//	  MANIFEST.json         — atomic JSON manifest (temp: MANIFEST.json.tmp)
//	  sstables/
//	    table-000001.sst    — immutable sorted SSTable
//	    table-000001.bloom  — serialized Bloom filter sidecar
//	    table-000002.sst
//	    table-000002.bloom
//
// # Manifest format
//
// MANIFEST.json is a JSON object:
//
//	{
//	  "version":      1,
//	  "next_file_id": 3,
//	  "next_seq":     42,
//	  "tables": [
//	    {
//	      "id":           1,
//	      "sstable_path": "sstables/table-000001.sst",
//	      "bloom_path":   "sstables/table-000001.bloom",
//	      "count":        100,
//	      "min_key":      "<base64>",
//	      "max_key":      "<base64>"
//	    }
//	  ]
//	}
//
// Paths are relative to Dir so the engine directory is portable.
// MinKey/MaxKey are base64-encoded to support arbitrary binary keys.
//
// # Write path
//
//  1. Validate key (non-empty).
//  2. Acquire write lock.
//  3. Assign next sequence number (engine-owned monotonic counter).
//  4. Append WAL record (PUT or DELETE).
//  5. Apply to MemTable.
//
// # Read path (Get)
//
//  1. Acquire read lock.
//  2. Check MemTable.
//     — PUT  → return value copy.
//     — DELETE tombstone → return (nil, false, nil).
//  3. For each SSTable, newest to oldest:
//     a. Check min/max key bounds; skip if out of range.
//     b. Check Bloom filter; skip and increment BloomNegativeSkips if absent.
//     c. Read SSTable.Get; return on PUT or DELETE.
//  4. Not found → (nil, false, nil).
//
// # Flush
//
// Flush is manual-only in this phase. It:
//  1. Returns nil immediately if the MemTable is empty.
//  2. Snapshots MemTable entries, converts them to SSTable entries.
//  3. Writes SSTable to a temp path, syncs, renames atomically (handled by
//     sstable.Create).
//  4. Builds and serializes a Bloom filter; writes sidecar atomically via
//     writeFileAtomic (temp file + fsync + rename in the same directory).
//  5. Updates MANIFEST.json atomically (temp + rename).
//  6. Resets MemTable and rotates WAL.
//
// # Compact
//
// Compact performs a manual full compaction of all currently flushed SSTables.
// The active MemTable and WAL are not touched.
//
// Preconditions:
//   - If the engine is closed, returns ErrClosed.
//   - If there are 0 or 1 SSTables, returns nil immediately (no-op).
//
// Algorithm:
//  1. Scan all SSTables oldest-first; build the latest candidate per key by
//     highest sequence number.
//  2. If the latest candidate is PUT, keep it; if DELETE, drop it. Tombstone
//     dropping is safe here because full compaction covers all SSTables — no
//     older version of the key can exist below the compacted level.
//  3. Sort surviving entries lexicographically by key.
//
// Output:
//   - Zero live entries: update manifest to empty table list; delete old files.
//   - ≥1 live entries: write one new compacted SSTable + Bloom sidecar with a
//     new file ID; update manifest to list only the new table; delete old files.
//
// Manifest update is atomic (temp + fsync + rename). Old files are removed
// after manifest commit (best-effort; orphaned files are ignored on restart).
//
// Crash recovery for Compact:
//   - Crash before new manifest commit: old manifest still lists old SSTables;
//     any partially written compacted SSTable/Bloom is an orphan and ignored.
//   - Crash after new manifest commit, before old file deletion: new manifest
//     lists only the compacted table; old files are orphans and ignored.
//   - Crash after manifest clears all tables: old files may remain as orphans
//     and are ignored because they are not in the manifest.
//
// # Bloom stats concurrency
//
// BloomChecks and BloomNegativeSkips are updated inside Get, which holds only
// a read lock (e.mu.RLock). Multiple concurrent readers therefore update these
// counters simultaneously. They are stored as [sync/atomic.Uint64] values so
// that concurrent increments are safe without a write lock. Stats.BloomChecks
// and Stats.BloomNegativeSkips are read with Load().
//
// # Recovery invariants
//
// If a crash occurs before the manifest is updated, orphan SSTable/Bloom files
// may exist on disk but are not listed in the manifest, so they are ignored on
// restart. Recovery is correct.
//
// If a crash occurs after the manifest is updated but before the WAL is reset,
// replaying the WAL will re-apply entries already captured in the new SSTable.
// The MemTable holds duplicate (higher-Seq) entries that shadow the SSTable
// entries during reads. This is correct; a subsequent Flush consolidates.
//
// # Known limitations
//
//   - Manual full compaction only; no background or automatic compaction.
//   - No levels, no size-tiered or leveled compaction strategy.
//   - No background flush; callers must call Flush explicitly.
//   - No WAL rotation beyond full reset after a successful flush.
//   - No background cleanup of orphan SSTable/Bloom files.
//   - No transactions, snapshots, or MVCC.
//   - No range tombstones.
//   - No compression or block cache.
//   - No distributed/sharded/replicated mode.
//   - No vector search.
//   - Bloom filters are sidecar files; they are not embedded in SSTables yet.
//   - Parent directory is not fsynced after manifest rename.
//   - Old file deletion after compaction is best-effort; orphans are harmless.
package engine

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/YashPatel2395/ShardForgeDB/internal/bloom"
	"github.com/YashPatel2395/ShardForgeDB/internal/memtable"
	"github.com/YashPatel2395/ShardForgeDB/internal/sstable"
	"github.com/YashPatel2395/ShardForgeDB/internal/wal"
)

// ── Test hooks ────────────────────────────────────────────────────────────────

// openSSTableReader is the function used to open an SSTable reader. It is a
// package-level variable so that tests can replace it to inject failures.
// Production code must not reassign this variable. Tests must restore the
// original value with defer (e.g. defer func() { openSSTableReader = sstable.Open }()).
var openSSTableReader = sstable.Open

// ── Exported errors ───────────────────────────────────────────────────────────

// ErrClosed is returned by any engine operation after Close has been called.
var ErrClosed = errors.New("engine: closed")

// ErrInvalidOptions is returned when Options fields are invalid.
var ErrInvalidOptions = errors.New("engine: invalid options")

// ErrInvalidKey is returned when an empty or nil key is passed to Put/Delete.
var ErrInvalidKey = errors.New("engine: invalid key")

// ErrCorruptManifest is returned when MANIFEST.json cannot be parsed or fails
// validation.
var ErrCorruptManifest = errors.New("engine: corrupt manifest")

// ErrManifestVersion is returned when the manifest schema version is
// unsupported.
var ErrManifestVersion = errors.New("engine: unsupported manifest version")

// ErrFlushFailed is returned when Flush cannot atomically commit the new
// SSTable and manifest.
var ErrFlushFailed = errors.New("engine: flush failed")

// ErrCompactionFailed is returned when Compact cannot read input SSTables,
// write the compacted output, or atomically commit the new manifest.
var ErrCompactionFailed = errors.New("engine: compaction failed")

// ── Constants ─────────────────────────────────────────────────────────────────

const (
	walFilename     = "wal.log"
	sstablesDir     = "sstables"
	defaultMemBytes = 64 << 20 // 64 MiB
	defaultBloomFPR = 0.01     // 1%
)

// ── Public types ──────────────────────────────────────────────────────────────

// Options configures an Engine.
type Options struct {
	// Dir is the directory where all engine files are stored. Required.
	Dir string

	// MemTableMaxBytes is the size threshold above which ShouldFlush returns
	// true. Default (0): 64 MiB.
	MemTableMaxBytes uint64

	// WALSyncOnWrite calls fsync after every WAL append when true.
	WALSyncOnWrite bool

	// WALMaxRecordSize is the maximum WAL record body size in bytes.
	// Default (0): 64 MiB.
	WALMaxRecordSize uint32

	// SSTableMaxRecordSize is the maximum SSTable record body size in bytes.
	// Default (0): 64 MiB.
	SSTableMaxRecordSize uint32

	// BloomFalsePositiveRate is the target FPR for per-SSTable Bloom filters.
	// Must be in (0, 1). Default (0): 0.01 (1%).
	BloomFalsePositiveRate float64
}

// Entry is a live key-value pair returned by Scan.
// Tombstones are never returned to callers.
type Entry struct {
	Key   []byte
	Value []byte
	Seq   uint64
}

// Stats exposes observable engine counters.
type Stats struct {
	MemTableEntries     int
	MemTableApproxBytes uint64
	SSTableCount        int
	NextSeq             uint64
	FlushCount          uint64
	BloomChecks         uint64
	BloomNegativeSkips  uint64

	// Compaction counters. All fields are zero until at least one compaction
	// completes.
	CompactionCount             uint64 // number of successful Compact calls
	LastCompactionInputTables   int    // SSTable count fed into the last compaction
	LastCompactionOutputEntries uint64 // live entries written by the last compaction
}

// ── internal types ────────────────────────────────────────────────────────────

// tableHandle holds a live SSTable reader and its companion Bloom filter.
// Stored oldest-first in Engine.tables; reads traverse newest-first.
type tableHandle struct {
	id        uint64
	reader    *sstable.Reader
	bloom     *bloom.Filter
	minKey    []byte
	maxKey    []byte
	sstPath   string // relative path from engine Dir, used for cleanup
	bloomPath string // relative path from engine Dir, used for cleanup
}

// ── Engine ────────────────────────────────────────────────────────────────────

// Engine is a single-node key-value storage engine.
//
// Concurrent calls to Put, Delete, Get, Scan, Flush, Stats, and Close are
// safe.
type Engine struct {
	mu   sync.RWMutex
	opts Options

	walLog *wal.WAL
	mem    *memtable.MemTable
	tables []*tableHandle // oldest first; reads iterate newest-first

	nextSeq    uint64 // next sequence number to assign
	nextFileID uint64 // next SSTable file ID to assign

	flushCount uint64 // guarded by mu (write lock on every write path)

	// Compaction counters — all updated inside compact() under the write lock.
	compactionCount             uint64
	lastCompactionInputTables   int
	lastCompactionOutputEntries uint64

	// bloomChecks and bloomNegSkips are incremented inside Get which holds only
	// e.mu.RLock(), so multiple goroutines can update them concurrently.
	// They are stored as atomic values to avoid a data race.
	bloomChecks   atomic.Uint64
	bloomNegSkips atomic.Uint64

	closed bool
}

// ── Open ──────────────────────────────────────────────────────────────────────

// Open opens or creates an engine rooted at opts.Dir.
//
// On the first call it creates the directory layout and writes an empty
// manifest. On subsequent calls it loads the manifest, opens all listed
// SSTables and Bloom sidecars, opens the WAL, and replays any WAL records not
// yet captured in an SSTable.
func Open(opts Options) (*Engine, error) {
	if opts.Dir == "" {
		return nil, fmt.Errorf("%w: Dir is required", ErrInvalidOptions)
	}
	if opts.MemTableMaxBytes == 0 {
		opts.MemTableMaxBytes = defaultMemBytes
	}
	if opts.BloomFalsePositiveRate == 0 {
		opts.BloomFalsePositiveRate = defaultBloomFPR
	}
	if math.IsNaN(opts.BloomFalsePositiveRate) || math.IsInf(opts.BloomFalsePositiveRate, 0) ||
		opts.BloomFalsePositiveRate <= 0 || opts.BloomFalsePositiveRate >= 1 {
		return nil, fmt.Errorf("%w: BloomFalsePositiveRate must be finite in (0,1), got %g",
			ErrInvalidOptions, opts.BloomFalsePositiveRate)
	}

	// Create directory structure.
	if err := os.MkdirAll(filepath.Join(opts.Dir, sstablesDir), 0o700); err != nil {
		return nil, fmt.Errorf("engine: mkdir: %w", err)
	}

	// Load or create manifest. On first Open (no file on disk) persist an empty
	// manifest so that the on-disk layout is explicit and matches documentation.
	m, existed, err := loadManifest(opts.Dir)
	if err != nil {
		return nil, err
	}
	if !existed {
		if err := saveManifest(opts.Dir, m); err != nil {
			return nil, fmt.Errorf("engine: init manifest: %w", err)
		}
	}

	e := &Engine{
		opts:       opts,
		nextSeq:    m.NextSeq,
		nextFileID: m.NextFileID,
	}

	// Open SSTables and Bloom filters listed in the manifest (oldest first).
	for _, te := range m.Tables {
		sstPath := filepath.Join(opts.Dir, te.SSTablePath)
		bloomPath := filepath.Join(opts.Dir, te.BloomPath)

		r, err := sstable.Open(sstPath, sstable.Options{MaxRecordSize: opts.SSTableMaxRecordSize})
		if err != nil {
			e.closeHandles()
			return nil, fmt.Errorf("engine: open SSTable %s: %w", sstPath, err)
		}

		bf, err := loadBloom(bloomPath)
		if err != nil {
			r.Close()
			e.closeHandles()
			return nil, fmt.Errorf("engine: load Bloom %s: %w", bloomPath, err)
		}

		minKey, err := decodeKey(te.MinKey)
		if err != nil {
			r.Close()
			e.closeHandles()
			return nil, fmt.Errorf("engine: decode MinKey for table %d: %w", te.ID, err)
		}
		maxKey, err := decodeKey(te.MaxKey)
		if err != nil {
			r.Close()
			e.closeHandles()
			return nil, fmt.Errorf("engine: decode MaxKey for table %d: %w", te.ID, err)
		}

		e.tables = append(e.tables, &tableHandle{
			id:        te.ID,
			reader:    r,
			bloom:     bf,
			minKey:    minKey,
			maxKey:    maxKey,
			sstPath:   te.SSTablePath,
			bloomPath: te.BloomPath,
		})
	}

	// Open WAL. Must replay before starting writers.
	walPath := filepath.Join(opts.Dir, walFilename)
	w, err := wal.Open(walPath, wal.Options{
		SyncOnWrite:   opts.WALSyncOnWrite,
		MaxRecordSize: opts.WALMaxRecordSize,
	})
	if err != nil {
		e.closeHandles()
		return nil, fmt.Errorf("engine: open WAL: %w", err)
	}
	e.walLog = w

	// Build MemTable.
	e.mem = memtable.New(memtable.Options{MaxBytes: opts.MemTableMaxBytes})

	// Replay WAL into MemTable.
	var maxWALSeq uint64
	if err := w.Replay(func(r wal.Record) error {
		if r.Seq > maxWALSeq {
			maxWALSeq = r.Seq
		}
		switch r.Type {
		case wal.RecordPut:
			return e.mem.Put(r.Key, r.Value, r.Seq)
		case wal.RecordDelete:
			return e.mem.Delete(r.Key, r.Seq)
		default:
			return fmt.Errorf("engine: unknown WAL record type %d", r.Type)
		}
	}); err != nil {
		e.walLog.Close()
		e.closeHandles()
		return nil, fmt.Errorf("engine: WAL replay: %w", err)
	}

	// next Seq must be strictly greater than both the manifest value and any
	// seq seen in the WAL.
	if maxWALSeq >= e.nextSeq {
		e.nextSeq = maxWALSeq + 1
	}

	return e, nil
}

// ── Put ───────────────────────────────────────────────────────────────────────

// Put inserts or updates key with value.
//
// The WAL record is written first for durability; the MemTable is updated
// after. Returns ErrInvalidKey for empty keys. Returns ErrClosed after Close.
func (e *Engine) Put(key, value []byte) error {
	if len(key) == 0 {
		return fmt.Errorf("%w: key must be non-empty", ErrInvalidKey)
	}

	keyCopy := make([]byte, len(key))
	copy(keyCopy, key)
	valCopy := make([]byte, len(value))
	copy(valCopy, value)

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return ErrClosed
	}

	seq := e.nextSeq
	e.nextSeq++

	if _, err := e.walLog.Append(wal.Record{
		Type:  wal.RecordPut,
		Key:   keyCopy,
		Value: valCopy,
		Seq:   seq,
	}); err != nil {
		e.nextSeq--
		return fmt.Errorf("engine: WAL append: %w", err)
	}

	if err := e.mem.Put(keyCopy, valCopy, seq); err != nil {
		return fmt.Errorf("engine: memtable put: %w", err)
	}
	return nil
}

// ── Delete ────────────────────────────────────────────────────────────────────

// Delete records a deletion tombstone for key.
//
// Tombstones shadow any older value in the MemTable or SSTables. Returns
// ErrInvalidKey for empty keys. Returns ErrClosed after Close.
func (e *Engine) Delete(key []byte) error {
	if len(key) == 0 {
		return fmt.Errorf("%w: key must be non-empty", ErrInvalidKey)
	}

	keyCopy := make([]byte, len(key))
	copy(keyCopy, key)

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return ErrClosed
	}

	seq := e.nextSeq
	e.nextSeq++

	if _, err := e.walLog.Append(wal.Record{
		Type: wal.RecordDelete,
		Key:  keyCopy,
		Seq:  seq,
	}); err != nil {
		e.nextSeq--
		return fmt.Errorf("engine: WAL append: %w", err)
	}

	if err := e.mem.Delete(keyCopy, seq); err != nil {
		return fmt.Errorf("engine: memtable delete: %w", err)
	}
	return nil
}

// ── Get ───────────────────────────────────────────────────────────────────────

// Get returns the value for key. Returns (nil, false, nil) if the key is not
// present or was deleted. The returned value is a defensive copy.
//
// Lookup order: MemTable → newest SSTable → oldest SSTable.
// Each SSTable is guarded by a Bloom filter check.
func (e *Engine) Get(key []byte) ([]byte, bool, error) {
	if len(key) == 0 {
		return nil, false, nil
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.closed {
		return nil, false, ErrClosed
	}

	// 1. MemTable.
	if entry, ok := e.mem.Get(key); ok {
		if entry.Kind == memtable.EntryPut {
			val := make([]byte, len(entry.Value))
			copy(val, entry.Value)
			return val, true, nil
		}
		return nil, false, nil // tombstone
	}

	// 2. SSTables, newest to oldest.
	for i := len(e.tables) - 1; i >= 0; i-- {
		th := e.tables[i]

		// Bounds skip.
		if len(th.minKey) > 0 && string(key) < string(th.minKey) {
			continue
		}
		if len(th.maxKey) > 0 && string(key) > string(th.maxKey) {
			continue
		}

		// Bloom check. Use atomic increments because Get holds only RLock and
		// multiple concurrent readers may reach this branch simultaneously.
		e.bloomChecks.Add(1)
		if !th.bloom.MightContain(key) {
			e.bloomNegSkips.Add(1)
			continue
		}

		entry, found, err := th.reader.Get(key)
		if err != nil {
			return nil, false, fmt.Errorf("engine: SSTable get: %w", err)
		}
		if !found {
			continue
		}
		if entry.Kind == sstable.EntryPut {
			val := make([]byte, len(entry.Value))
			copy(val, entry.Value)
			return val, true, nil
		}
		// DELETE tombstone in SSTable.
		return nil, false, nil
	}

	return nil, false, nil
}

// ── Scan ──────────────────────────────────────────────────────────────────────

// Scan returns all live entries in [start, end) in ascending key order.
//
// Tombstones suppress older values. Duplicate keys across sources resolve to
// the entry with the highest Seq. Only live (PUT) entries are returned.
// All returned keys and values are defensive copies.
func (e *Engine) Scan(start, end []byte) ([]Entry, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.closed {
		return nil, ErrClosed
	}

	type candidate struct {
		seq   uint64
		kind  uint8 // 1=put, 2=delete
		value []byte
	}
	best := make(map[string]*candidate)

	merge := func(k []byte, seq uint64, kind uint8, value []byte) {
		ks := string(k)
		if c, ok := best[ks]; ok && seq <= c.seq {
			return
		}
		val := make([]byte, len(value))
		copy(val, value)
		best[ks] = &candidate{seq: seq, kind: kind, value: val}
	}

	// MemTable.
	for _, me := range e.mem.Scan(start, end) {
		kind := uint8(1)
		if me.Kind == memtable.EntryDelete {
			kind = 2
		}
		merge(me.Key, me.Seq, kind, me.Value)
	}

	// All SSTables.
	for _, th := range e.tables {
		sstEntries, err := th.reader.Scan(start, end)
		if err != nil {
			return nil, fmt.Errorf("engine: SSTable scan: %w", err)
		}
		for _, se := range sstEntries {
			kind := uint8(1)
			if se.Kind == sstable.EntryDelete {
				kind = 2
			}
			merge(se.Key, se.Seq, kind, se.Value)
		}
	}

	var result []Entry
	for ks, c := range best {
		if c.kind == 1 {
			key := make([]byte, len(ks))
			copy(key, ks)
			result = append(result, Entry{Key: key, Value: c.value, Seq: c.seq})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return string(result[i].Key) < string(result[j].Key)
	})
	return result, nil
}

// ── Flush ─────────────────────────────────────────────────────────────────────

// Flush converts the current MemTable to an immutable SSTable, writes a Bloom
// sidecar, updates the manifest atomically, then resets the MemTable and WAL.
//
// If the MemTable is empty, Flush is a no-op and returns nil.
func (e *Engine) Flush() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return ErrClosed
	}
	return e.flush()
}

// flush is the internal implementation of Flush; must be called with mu held.
func (e *Engine) flush() error {
	if e.mem.Len() == 0 {
		return nil
	}

	// Snapshot MemTable in sorted order.
	memEntries := e.mem.Scan(nil, nil)

	// Convert to SSTable entries.
	sstEntries := make([]sstable.Entry, len(memEntries))
	for i, me := range memEntries {
		kind := sstable.EntryPut
		if me.Kind == memtable.EntryDelete {
			kind = sstable.EntryDelete
		}
		sstEntries[i] = sstable.Entry{
			Key:   me.Key,
			Value: me.Value,
			Kind:  kind,
			Seq:   me.Seq,
		}
	}

	fileID := e.nextFileID
	e.nextFileID++

	sstRelPath := filepath.Join(sstablesDir, fmt.Sprintf("table-%06d.sst", fileID))
	bloomRelPath := filepath.Join(sstablesDir, fmt.Sprintf("table-%06d.bloom", fileID))
	sstAbsPath := filepath.Join(e.opts.Dir, sstRelPath)
	bloomAbsPath := filepath.Join(e.opts.Dir, bloomRelPath)

	// Write SSTable (sstable.Create handles temp+rename internally).
	sstMeta, err := sstable.Create(sstAbsPath, sstEntries, sstable.Options{
		MaxRecordSize: e.opts.SSTableMaxRecordSize,
	})
	if err != nil {
		e.nextFileID--
		return fmt.Errorf("%w: create SSTable: %v", ErrFlushFailed, err)
	}

	// Build Bloom filter covering all keys (including tombstones).
	bf, err := bloom.New(bloom.Options{
		ExpectedItems:     uint64(len(sstEntries)),
		FalsePositiveRate: e.opts.BloomFalsePositiveRate,
	})
	if err != nil {
		os.Remove(sstAbsPath)
		e.nextFileID--
		return fmt.Errorf("%w: create Bloom: %v", ErrFlushFailed, err)
	}
	for _, se := range sstEntries {
		bf.Add(se.Key)
	}
	bloomData, err := bf.MarshalBinary()
	if err != nil {
		os.Remove(sstAbsPath)
		e.nextFileID--
		return fmt.Errorf("%w: marshal Bloom: %v", ErrFlushFailed, err)
	}
	// Write Bloom sidecar atomically (temp file + fsync + rename) so that a
	// crash between the SSTable write and the manifest update leaves only a
	// harmless orphan temp file rather than a truncated sidecar.
	if err := writeFileAtomic(bloomAbsPath, bloomData, 0o600); err != nil {
		os.Remove(sstAbsPath)
		e.nextFileID--
		return fmt.Errorf("%w: write Bloom sidecar: %v", ErrFlushFailed, err)
	}

	// Open the new SSTable reader BEFORE committing the manifest. If opening
	// fails we can safely undo: remove the new files, decrement the file ID,
	// and return an error with the old tables and manifest unchanged.
	r, err := openSSTableReader(sstAbsPath, sstable.Options{MaxRecordSize: e.opts.SSTableMaxRecordSize})
	if err != nil {
		os.Remove(sstAbsPath)
		os.Remove(bloomAbsPath)
		e.nextFileID--
		return fmt.Errorf("%w: open new SSTable reader: %v", ErrFlushFailed, err)
	}

	// Load the current manifest, append the new table entry, and save
	// atomically. We reload it from disk so concurrent restarts see a
	// consistent view (within this single-node engine that is the same object,
	// but loading keeps the logic clean).
	m, _, err := loadManifest(e.opts.Dir)
	if err != nil {
		r.Close()
		os.Remove(sstAbsPath)
		os.Remove(bloomAbsPath)
		e.nextFileID--
		return fmt.Errorf("%w: reload manifest: %v", ErrFlushFailed, err)
	}
	m.NextFileID = e.nextFileID
	m.NextSeq = e.nextSeq
	m.Tables = append(m.Tables, tableEntry{
		ID:          fileID,
		SSTablePath: sstRelPath,
		BloomPath:   bloomRelPath,
		Count:       sstMeta.Count,
		MinKey:      encodeKey(sstMeta.MinKey),
		MaxKey:      encodeKey(sstMeta.MaxKey),
	})
	if err := saveManifest(e.opts.Dir, m); err != nil {
		r.Close()
		os.Remove(sstAbsPath)
		os.Remove(bloomAbsPath)
		e.nextFileID--
		return fmt.Errorf("%w: save manifest: %v", ErrFlushFailed, err)
	}

	// Manifest committed. Append the new table handle (reader already open).
	e.tables = append(e.tables, &tableHandle{
		id:        fileID,
		reader:    r,
		bloom:     bf,
		minKey:    sstMeta.MinKey,
		maxKey:    sstMeta.MaxKey,
		sstPath:   sstRelPath,
		bloomPath: bloomRelPath,
	})

	// Reset MemTable.
	e.mem = memtable.New(memtable.Options{MaxBytes: e.opts.MemTableMaxBytes})

	// Rotate WAL: close old, truncate, reopen.
	oldWAL := e.walLog
	oldWAL.Close()
	walPath := filepath.Join(e.opts.Dir, walFilename)
	os.Remove(walPath)
	newWAL, err := wal.Open(walPath, wal.Options{
		SyncOnWrite:   e.opts.WALSyncOnWrite,
		MaxRecordSize: e.opts.WALMaxRecordSize,
	})
	if err != nil {
		e.closed = true
		return fmt.Errorf("engine: reopen WAL after flush: %w", err)
	}
	e.walLog = newWAL
	e.flushCount++
	return nil
}

// ── Compact ───────────────────────────────────────────────────────────────────

// Compact performs a manual full compaction of all currently flushed SSTables.
//
// All existing SSTables are merged into at most one new compacted SSTable.
// The active MemTable and WAL are not modified.
//
// Preconditions:
//   - Returns ErrClosed if the engine has been closed.
//   - Returns nil immediately if there are 0 or 1 SSTables (no-op).
//
// After a successful compaction:
//   - All old SSTable and Bloom sidecar files are removed (best-effort).
//   - MANIFEST.json lists at most one compacted SSTable.
//   - Stats().SSTableCount is 0 (all-tombstone case) or 1 (live-entries case).
//   - Read correctness is preserved: Get and Scan return identical live results.
func (e *Engine) Compact() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return ErrClosed
	}
	return e.compact()
}

// compact is the internal implementation. Must be called with e.mu held (write lock).
//
// Merge rules:
//  1. Scan all SSTables oldest-first via Reader.Scan.
//  2. For each key, keep the entry with the highest sequence number.
//  3. If the winning entry is PUT, include it in the output.
//  4. If the winning entry is DELETE (tombstone), drop it. This is safe because
//     full compaction covers all SSTables; no older version of the key can
//     exist below this level after the manifest swap.
//  5. Sort surviving entries lexicographically by key before writing.
//
// Ordering invariant (live-output path):
//
//  1. Write compacted SSTable.
//  2. Write compacted Bloom sidecar atomically.
//  3. Open the new SSTable reader.
//     — On failure: remove new files, decrement nextFileID, return error.
//     — Old tables and manifest are unchanged; old reads continue working.
//  4. Commit the new manifest.
//     — On failure: close new reader, remove new files, decrement nextFileID,
//     return error. Old tables and manifest are unchanged.
//  5. Close old readers, replace e.tables, remove old files best-effort.
//  6. Update compaction stats.
//
// Crash safety:
//   - Crash before manifest commit: old manifest intact; compacted orphan ignored.
//   - Crash after manifest commit, before file deletion: old files are orphans,
//     ignored on restart because they are absent from the new manifest.
//   - Crash after manifest empties all tables: orphaned old files are ignored.
func (e *Engine) compact() error {
	if len(e.tables) <= 1 {
		return nil
	}

	inputCount := len(e.tables)

	// ── Step 1: merge all SSTable entries ────────────────────────────────────

	type candidate struct {
		seq   uint64
		kind  sstable.EntryKind
		value []byte
	}
	best := make(map[string]*candidate)

	for _, th := range e.tables {
		entries, err := th.reader.Scan(nil, nil)
		if err != nil {
			return fmt.Errorf("%w: scan SSTable %d: %v", ErrCompactionFailed, th.id, err)
		}
		for _, se := range entries {
			ks := string(se.Key)
			if c, ok := best[ks]; ok && se.Seq <= c.seq {
				continue
			}
			val := make([]byte, len(se.Value))
			copy(val, se.Value)
			best[ks] = &candidate{seq: se.Seq, kind: se.Kind, value: val}
		}
	}

	// ── Step 2: build sorted live entry slice (tombstones dropped) ────────────

	liveEntries := make([]sstable.Entry, 0, len(best))
	for ks, c := range best {
		if c.kind != sstable.EntryPut {
			continue // full compaction: drop tombstones
		}
		key := make([]byte, len(ks))
		copy(key, ks)
		liveEntries = append(liveEntries, sstable.Entry{
			Key:   key,
			Value: c.value,
			Kind:  sstable.EntryPut,
			Seq:   c.seq,
		})
	}
	sort.Slice(liveEntries, func(i, j int) bool {
		return string(liveEntries[i].Key) < string(liveEntries[j].Key)
	})

	// Collect old handles for cleanup after manifest commit.
	oldHandles := make([]*tableHandle, len(e.tables))
	copy(oldHandles, e.tables)

	// ── Step 3a: no live entries — empty all SSTables ─────────────────────────

	if len(liveEntries) == 0 {
		m, _, err := loadManifest(e.opts.Dir)
		if err != nil {
			return fmt.Errorf("%w: load manifest: %v", ErrCompactionFailed, err)
		}
		m.NextSeq = e.nextSeq
		m.Tables = []tableEntry{}
		if err := saveManifest(e.opts.Dir, m); err != nil {
			return fmt.Errorf("%w: save manifest: %v", ErrCompactionFailed, err)
		}

		// Manifest committed. Replace in-memory state first.
		for _, th := range oldHandles {
			th.reader.Close()
		}
		e.tables = nil

		// Remove old files best-effort.
		var firstCleanupErr error
		for _, th := range oldHandles {
			if err := os.Remove(filepath.Join(e.opts.Dir, th.sstPath)); err != nil &&
				!errors.Is(err, os.ErrNotExist) && firstCleanupErr == nil {
				firstCleanupErr = err
			}
			if err := os.Remove(filepath.Join(e.opts.Dir, th.bloomPath)); err != nil &&
				!errors.Is(err, os.ErrNotExist) && firstCleanupErr == nil {
				firstCleanupErr = err
			}
		}

		e.compactionCount++
		e.lastCompactionInputTables = inputCount
		e.lastCompactionOutputEntries = 0
		return firstCleanupErr
	}

	// ── Step 3b: live entries exist — write one compacted SSTable ─────────────

	fileID := e.nextFileID
	e.nextFileID++

	sstRelPath := filepath.Join(sstablesDir, fmt.Sprintf("table-%06d.sst", fileID))
	bloomRelPath := filepath.Join(sstablesDir, fmt.Sprintf("table-%06d.bloom", fileID))
	sstAbsPath := filepath.Join(e.opts.Dir, sstRelPath)
	bloomAbsPath := filepath.Join(e.opts.Dir, bloomRelPath)

	sstMeta, err := sstable.Create(sstAbsPath, liveEntries, sstable.Options{
		MaxRecordSize: e.opts.SSTableMaxRecordSize,
	})
	if err != nil {
		e.nextFileID--
		return fmt.Errorf("%w: create compacted SSTable: %v", ErrCompactionFailed, err)
	}

	// Build Bloom filter only over live keys.
	bf, err := bloom.New(bloom.Options{
		ExpectedItems:     uint64(len(liveEntries)),
		FalsePositiveRate: e.opts.BloomFalsePositiveRate,
	})
	if err != nil {
		os.Remove(sstAbsPath)
		e.nextFileID--
		return fmt.Errorf("%w: create Bloom: %v", ErrCompactionFailed, err)
	}
	for _, se := range liveEntries {
		bf.Add(se.Key)
	}
	bloomData, err := bf.MarshalBinary()
	if err != nil {
		os.Remove(sstAbsPath)
		e.nextFileID--
		return fmt.Errorf("%w: marshal Bloom: %v", ErrCompactionFailed, err)
	}
	if err := writeFileAtomic(bloomAbsPath, bloomData, 0o600); err != nil {
		os.Remove(sstAbsPath)
		e.nextFileID--
		return fmt.Errorf("%w: write Bloom sidecar: %v", ErrCompactionFailed, err)
	}

	// Open the new SSTable reader BEFORE committing the manifest. If opening
	// fails, remove the new files, decrement the file ID, and return an error
	// with the old tables and manifest unchanged — old reads continue working.
	r, err := openSSTableReader(sstAbsPath, sstable.Options{MaxRecordSize: e.opts.SSTableMaxRecordSize})
	if err != nil {
		os.Remove(sstAbsPath)
		os.Remove(bloomAbsPath)
		e.nextFileID--
		return fmt.Errorf("%w: open compacted SSTable reader: %v", ErrCompactionFailed, err)
	}

	// Atomic manifest update: replace all old table entries with one new entry.
	m, _, err := loadManifest(e.opts.Dir)
	if err != nil {
		r.Close()
		os.Remove(sstAbsPath)
		os.Remove(bloomAbsPath)
		e.nextFileID--
		return fmt.Errorf("%w: load manifest: %v", ErrCompactionFailed, err)
	}
	m.NextFileID = e.nextFileID
	m.NextSeq = e.nextSeq
	m.Tables = []tableEntry{
		{
			ID:          fileID,
			SSTablePath: sstRelPath,
			BloomPath:   bloomRelPath,
			Count:       sstMeta.Count,
			MinKey:      encodeKey(sstMeta.MinKey),
			MaxKey:      encodeKey(sstMeta.MaxKey),
		},
	}
	if err := saveManifest(e.opts.Dir, m); err != nil {
		r.Close()
		os.Remove(sstAbsPath)
		os.Remove(bloomAbsPath)
		e.nextFileID--
		return fmt.Errorf("%w: save manifest: %v", ErrCompactionFailed, err)
	}

	// Manifest committed. Replace in-memory table list (reader already open).
	for _, th := range oldHandles {
		th.reader.Close()
	}
	e.tables = []*tableHandle{
		{
			id:        fileID,
			reader:    r,
			bloom:     bf,
			minKey:    sstMeta.MinKey,
			maxKey:    sstMeta.MaxKey,
			sstPath:   sstRelPath,
			bloomPath: bloomRelPath,
		},
	}

	// Remove old files best-effort (after in-memory state is correct).
	var firstCleanupErr error
	for _, th := range oldHandles {
		if err := os.Remove(filepath.Join(e.opts.Dir, th.sstPath)); err != nil &&
			!errors.Is(err, os.ErrNotExist) && firstCleanupErr == nil {
			firstCleanupErr = err
		}
		if err := os.Remove(filepath.Join(e.opts.Dir, th.bloomPath)); err != nil &&
			!errors.Is(err, os.ErrNotExist) && firstCleanupErr == nil {
			firstCleanupErr = err
		}
	}

	e.compactionCount++
	e.lastCompactionInputTables = inputCount
	e.lastCompactionOutputEntries = uint64(len(liveEntries))
	return firstCleanupErr
}

// ── Stats ─────────────────────────────────────────────────────────────────────

// Stats returns a point-in-time snapshot of engine counters.
//
// MemTableEntries, MemTableApproxBytes, SSTableCount, NextSeq, and FlushCount
// are read under the read lock. BloomChecks and BloomNegativeSkips are atomic
// counters read without the lock; they may reflect operations that completed
// after the lock was released, but are always consistent in isolation.
func (e *Engine) Stats() Stats {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return Stats{
		MemTableEntries:             e.mem.Len(),
		MemTableApproxBytes:         e.mem.ApproxBytes(),
		SSTableCount:                len(e.tables),
		NextSeq:                     e.nextSeq,
		FlushCount:                  e.flushCount,
		BloomChecks:                 e.bloomChecks.Load(),
		BloomNegativeSkips:          e.bloomNegSkips.Load(),
		CompactionCount:             e.compactionCount,
		LastCompactionInputTables:   e.lastCompactionInputTables,
		LastCompactionOutputEntries: e.lastCompactionOutputEntries,
	}
}

// ── Close ─────────────────────────────────────────────────────────────────────

// Close closes the WAL and all SSTable readers.
//
// Close is idempotent: calling it twice returns nil. Any subsequent operation
// on the engine returns ErrClosed.
func (e *Engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return nil
	}
	e.closed = true

	var firstErr error
	if e.walLog != nil {
		if err := e.walLog.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("engine: close WAL: %w", err)
		}
	}
	for _, th := range e.tables {
		if err := th.reader.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("engine: close SSTable %d: %w", th.id, err)
		}
	}
	return firstErr
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// closeHandles closes all open SSTable readers accumulated so far. Used during
// a failed Open to clean up partial state.
func (e *Engine) closeHandles() {
	for _, th := range e.tables {
		th.reader.Close()
	}
}

// loadBloom reads and deserializes a Bloom filter from path.
func loadBloom(path string) (*bloom.Filter, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	f, err := bloom.UnmarshalBinary(data)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	return f, nil
}
