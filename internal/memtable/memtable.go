// Package memtable implements an ordered in-memory write buffer for ShardForgeDB.
//
// The MemTable holds the latest entry per key in lexicographic order. Its role
// in the storage stack is to sit above the WAL (Write-Ahead Log) and below
// the SSTable layer:
//
//  1. A write is first recorded in the WAL for durability.
//  2. It is then applied to the MemTable for fast in-memory reads.
//  3. When the MemTable grows beyond its size threshold, it is flushed to an
//     immutable SSTable on disk (not yet implemented).
//
// # This package is not yet connected to the WAL or the Engine.
// Data stored here is lost on process restart — it is not durable by itself.
//
// # Data structure
//
// Two complementary structures are maintained under a single sync.RWMutex:
//
//   - map[string]Entry — O(1) lookup by string key.
//   - []string         — sorted key slice for ordered scans.
//
// Insertions maintain the sorted slice via binary search (O(log n)) to locate
// the insert position, followed by a slice shift (O(n)). This gives O(n)
// amortized per insert and O(n²) for a random bulk load of n keys. A skip
// list will be evaluated in a later phase if profiling shows this as a
// bottleneck.
//
// # Size accounting
//
// ApproxBytes tracks: len(key) + len(value) + entryOverhead per entry.
// entryOverhead (64 B) is a fixed estimate covering the map bucket, the sorted
// key slice element, and the Entry struct fields. It is not an exact runtime
// measurement, but it is deterministic and consistent.
package memtable

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

// ── Errors ────────────────────────────────────────────────────────────────────

// ErrEmptyKey is returned by Put and Delete when the key has zero length.
var ErrEmptyKey = errors.New("memtable: empty key")

// ── Types ─────────────────────────────────────────────────────────────────────

// EntryKind distinguishes a live value from a deletion tombstone.
type EntryKind uint8

const (
	// EntryPut represents a live key/value pair.
	EntryPut EntryKind = 1
	// EntryDelete represents a deletion tombstone. The engine uses tombstones
	// to shadow older versions of a key in SSTables below.
	EntryDelete EntryKind = 2
)

// Entry is a single record held by the MemTable. It is returned by Get and
// Scan as a deep copy; callers may mutate returned Entries freely.
type Entry struct {
	Key   []byte
	Value []byte
	Kind  EntryKind
	// Seq is the sequence number inherited from the WAL record that produced
	// this entry. It is used for ordering during compaction and merge.
	Seq uint64
}

// Options configures MemTable behaviour. Zero values use sensible defaults.
type Options struct {
	// MaxBytes is the approximate memory threshold above which ShouldFlush
	// returns true. Default (0): 64 MiB.
	MaxBytes uint64
}

// ── Constants ─────────────────────────────────────────────────────────────────

const (
	defaultMaxBytes uint64 = 64 << 20 // 64 MiB

	// entryOverhead is the fixed per-entry cost added to ApproxBytes on top of
	// len(key)+len(value). It estimates:
	//   map bucket pointer + string header  ≈ 24 B
	//   sorted-keys slice string element    ≈  8 B (string header in slice)
	//   Entry struct (Kind 1 + Seq 8 + two slice headers 32) ≈ 41 B
	// Rounded up to 64 for alignment and GC metadata.
	entryOverhead uint64 = 64
)

// ── MemTable ──────────────────────────────────────────────────────────────────

// MemTable is an ordered in-memory write buffer.
//
// Concurrent calls to Put, Delete, Get, Scan, Len, ApproxBytes, and
// ShouldFlush are safe. Scan and Get return defensive copies; callers may
// mutate returned values freely.
type MemTable struct {
	mu      sync.RWMutex
	entries map[string]Entry
	keys    []string // sorted lexicographically; parallel to entries
	bytes   uint64   // ApproxBytes value
	opts    Options
}

// New returns a new, empty MemTable with the given options.
func New(opts Options) *MemTable {
	if opts.MaxBytes == 0 {
		opts.MaxBytes = defaultMaxBytes
	}
	return &MemTable{
		entries: make(map[string]Entry),
		opts:    opts,
	}
}

// ── Write operations ──────────────────────────────────────────────────────────

// Put stores a live key/value entry with sequence number seq.
//
// If the key already exists, its entry is replaced and size accounting is
// updated. The provided key and value are defensively copied; caller mutation
// after the call does not affect stored data.
func (m *MemTable) Put(key, value []byte, seq uint64) error {
	if len(key) == 0 {
		return fmt.Errorf("%w", ErrEmptyKey)
	}

	// Defensive copies before acquiring the lock so the lock is held briefly.
	k := string(key)
	keyCopy := make([]byte, len(key))
	copy(keyCopy, key)
	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)

	e := Entry{Key: keyCopy, Value: valueCopy, Kind: EntryPut, Seq: seq}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.upsert(k, e)
	return nil
}

// Delete stores a deletion tombstone for key with sequence number seq.
//
// Delete does not remove the key from the MemTable; it records an EntryDelete
// marker that signals the engine to shadow older versions of this key in lower
// SSTable levels. If the key does not yet exist, a tombstone is still created.
func (m *MemTable) Delete(key []byte, seq uint64) error {
	if len(key) == 0 {
		return fmt.Errorf("%w", ErrEmptyKey)
	}

	k := string(key)
	keyCopy := make([]byte, len(key))
	copy(keyCopy, key)

	e := Entry{Key: keyCopy, Kind: EntryDelete, Seq: seq}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.upsert(k, e)
	return nil
}

// ── Read operations ───────────────────────────────────────────────────────────

// Get returns the latest entry for key, including tombstones.
// Returns (Entry{}, false) when the key is not present.
// The returned Entry is a deep copy.
func (m *MemTable) Get(key []byte) (Entry, bool) {
	if len(key) == 0 {
		return Entry{}, false
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	e, ok := m.entries[string(key)]
	if !ok {
		return Entry{}, false
	}
	return copyEntry(e), true
}

// Scan returns all entries whose keys fall in [start, end) in ascending order.
//
// Boundary rules:
//   - start is inclusive. Nil or empty means scan from the first key.
//   - end is exclusive. Nil or empty means scan to the last key.
//   - If start >= end (and both are non-empty), the result is empty.
//
// Tombstones are included. All returned entries are deep copies.
func (m *MemTable) Scan(start, end []byte) []Entry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	startIdx := 0
	if len(start) > 0 {
		startIdx = sort.SearchStrings(m.keys, string(start))
	}

	endStr := string(end)
	hasEnd := len(end) > 0

	var result []Entry
	for i := startIdx; i < len(m.keys); i++ {
		k := m.keys[i]
		if hasEnd && k >= endStr {
			break
		}
		result = append(result, copyEntry(m.entries[k]))
	}
	return result
}

// ── Metrics ───────────────────────────────────────────────────────────────────

// Len returns the number of unique keys currently in the MemTable.
// Both live entries and tombstones count toward Len.
func (m *MemTable) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.keys)
}

// ApproxBytes returns the approximate memory used by all entries.
// The value is deterministic but not an exact runtime measurement.
// See the package-level documentation for the accounting formula.
func (m *MemTable) ApproxBytes() uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.bytes
}

// ShouldFlush reports whether the MemTable has reached or exceeded its
// configured MaxBytes threshold and should be flushed to an SSTable.
func (m *MemTable) ShouldFlush() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.bytes >= m.opts.MaxBytes
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// upsert inserts or replaces the entry for key.
// Must be called with m.mu held for writing.
func (m *MemTable) upsert(key string, e Entry) {
	if old, exists := m.entries[key]; exists {
		// Subtract the old entry's contribution before overwriting.
		m.bytes -= uint64(len(old.Key)) + uint64(len(old.Value)) + entryOverhead
	} else {
		// New key: insert into the sorted slice at the correct position.
		i := sort.SearchStrings(m.keys, key)
		// Grow slice by one and shift elements right to make room.
		m.keys = append(m.keys, "")
		copy(m.keys[i+1:], m.keys[i:])
		m.keys[i] = key
	}
	m.bytes += uint64(len(e.Key)) + uint64(len(e.Value)) + entryOverhead
	m.entries[key] = e
}

// copyEntry returns a deep copy of e so callers cannot mutate stored state.
func copyEntry(e Entry) Entry {
	key := make([]byte, len(e.Key))
	copy(key, e.Key)
	val := make([]byte, len(e.Value))
	copy(val, e.Value)
	return Entry{Key: key, Value: val, Kind: e.Kind, Seq: e.Seq}
}
