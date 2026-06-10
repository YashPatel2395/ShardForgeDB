package shard

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/YashPatel2395/ShardForgeDB/internal/engine"
)

// Sentinel errors.
var (
	// ErrClosed is returned when an operation is attempted on a closed Store.
	ErrClosed = errors.New("shard: closed")

	// ErrInvalidOptions is returned when Open is called with invalid options.
	ErrInvalidOptions = errors.New("shard: invalid options")

	// ErrInvalidKey is returned when an empty key is provided.
	ErrInvalidKey = errors.New("shard: invalid key")

	// ErrCorruptManifest is returned when the manifest file is missing, corrupt,
	// or contains invalid data.
	ErrCorruptManifest = errors.New("shard: corrupt manifest")

	// ErrShardMismatch is returned on reopen when the caller-supplied ShardCount
	// or VirtualNodes does not match the manifest.
	ErrShardMismatch = errors.New("shard: manifest/options mismatch")
)

// Options configures the Store.
type Options struct {
	// Dir is the root directory for the store. Required.
	Dir string

	// ShardCount is the number of local shards.
	// Required and must be > 0 on first Open.
	// On reopen, 0 means "use manifest value".
	ShardCount int

	// VirtualNodes is the number of virtual ring entries per shard.
	// Default: 128. On reopen, 0 means "use manifest value".
	VirtualNodes int

	// ShardPrefix is the directory name prefix for shard subdirectories.
	// Default: "shard".
	ShardPrefix string
}

// Entry is a live key-value pair returned by Scan.
// Tombstones are never returned to callers.
type Entry struct {
	Key   []byte
	Value []byte
	Seq   uint64
}

// ShardInfo describes one shard.
type ShardInfo struct {
	ID   int
	Name string
	Path string // relative path from store Dir
}

// Stats aggregates store-wide and per-shard statistics.
type Stats struct {
	ShardCount   int
	VirtualNodes int

	TotalMemTableEntries     int
	TotalMemTableApproxBytes uint64
	TotalSSTableCount        int

	TotalFlushCount      uint64
	TotalCompactionCount uint64

	Shards []ShardStats
}

// ShardStats holds per-shard statistics.
type ShardStats struct {
	ID   int
	Name string
	Path string

	MemTableEntries     int
	MemTableApproxBytes uint64
	SSTableCount        int
	FlushCount          uint64
	CompactionCount     uint64
}

// shardHandle holds a single local shard's metadata and engine.
type shardHandle struct {
	info   ShardInfo
	engine *engine.Engine
}

// Store is a sharded key-value store backed by multiple local Engine instances.
//
// All public methods are safe for concurrent use. The sharding layer holds
// a read lock only to check the closed flag and access the immutable shard
// slice; each Engine handles its own internal synchronisation.
//
// Directory layout:
//
//	<Dir>/
//	  SHARDING.json        — sharding manifest (written atomically)
//	  shards/
//	    shard-0000/        — engine directory for shard 0
//	    shard-0001/        — engine directory for shard 1
//	    ...
type Store struct {
	mu     sync.RWMutex
	opts   Options
	ring   *Ring
	shards []*shardHandle
	closed bool
}

// Open opens or creates a sharded store rooted at opts.Dir.
//
// On first open, opts.ShardCount must be > 0. opts.VirtualNodes defaults to
// 128 and opts.ShardPrefix defaults to "shard" if not set.
//
// On reopen (SHARDING.json already exists), zero values for ShardCount and
// VirtualNodes are replaced by the manifest values. Non-zero values that
// differ from the manifest return ErrShardMismatch.
func Open(opts Options) (*Store, error) {
	if opts.Dir == "" {
		return nil, fmt.Errorf("%w: Dir is required", ErrInvalidOptions)
	}
	if opts.ShardPrefix == "" {
		opts.ShardPrefix = defaultShardPrefix
	}

	manifestPath := filepath.Join(opts.Dir, manifestFileName)
	manifestExists := pathExists(manifestPath)

	var m *shardingManifest

	if manifestExists {
		loaded, err := readManifest(opts.Dir)
		if err != nil {
			return nil, err
		}
		m = loaded

		// Validate caller options against manifest on reopen.
		if opts.ShardCount != 0 && opts.ShardCount != m.ShardCount {
			return nil, fmt.Errorf("%w: ShardCount %d != manifest %d",
				ErrShardMismatch, opts.ShardCount, m.ShardCount)
		}
		if opts.VirtualNodes != 0 && opts.VirtualNodes != m.VirtualNodes {
			return nil, fmt.Errorf("%w: VirtualNodes %d != manifest %d",
				ErrShardMismatch, opts.VirtualNodes, m.VirtualNodes)
		}
		// Override options with manifest values so the ring is consistent.
		opts.ShardCount = m.ShardCount
		opts.VirtualNodes = m.VirtualNodes
		opts.ShardPrefix = m.ShardPrefix
	} else {
		// First open — build and write manifest.
		if opts.ShardCount <= 0 {
			return nil, fmt.Errorf("%w: ShardCount must be > 0 on first open", ErrInvalidOptions)
		}
		if opts.VirtualNodes <= 0 {
			opts.VirtualNodes = defaultVirtualNodes
		}

		entries := make([]shardEntry, opts.ShardCount)
		for i := 0; i < opts.ShardCount; i++ {
			name := fmt.Sprintf("%s-%04d", opts.ShardPrefix, i)
			relPath := filepath.Join("shards", name)
			entries[i] = shardEntry{ID: i, Name: name, Path: relPath}
		}
		m = &shardingManifest{
			Version:      manifestVersion,
			ShardCount:   opts.ShardCount,
			VirtualNodes: opts.VirtualNodes,
			Hash:         hashAlgorithm,
			ShardPrefix:  opts.ShardPrefix,
			Shards:       entries,
		}
		if err := os.MkdirAll(opts.Dir, 0o755); err != nil {
			return nil, fmt.Errorf("shard: create dir: %w", err)
		}
		if err := writeManifest(opts.Dir, m); err != nil {
			return nil, err
		}
	}

	// Build the ring from manifest configuration.
	ring := newRing(opts.ShardCount, opts.VirtualNodes)

	// Open each shard engine in manifest order.
	handles := make([]*shardHandle, 0, len(m.Shards))
	for _, entry := range m.Shards {
		absPath := filepath.Join(opts.Dir, entry.Path)
		if err := os.MkdirAll(absPath, 0o755); err != nil {
			closeAllHandles(handles)
			return nil, fmt.Errorf("shard: create shard dir %q: %w", entry.Name, err)
		}
		eng, err := engine.Open(engine.Options{Dir: absPath})
		if err != nil {
			closeAllHandles(handles)
			return nil, fmt.Errorf("shard: open engine for shard %q: %w", entry.Name, err)
		}
		handles = append(handles, &shardHandle{
			info:   ShardInfo{ID: entry.ID, Name: entry.Name, Path: entry.Path},
			engine: eng,
		})
	}

	return &Store{
		opts:   opts,
		ring:   ring,
		shards: handles,
	}, nil
}

// Put writes key→value to the shard responsible for key.
// Empty key returns ErrInvalidKey.
func (s *Store) Put(key, value []byte) error {
	if len(key) == 0 {
		return ErrInvalidKey
	}
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return ErrClosed
	}
	eng := s.shards[s.ring.shardIndex(key)].engine
	s.mu.RUnlock()

	// Defensive copy so the caller may reuse its buffer after Put returns.
	val := make([]byte, len(value))
	copy(val, value)
	return eng.Put(key, val)
}

// Delete removes key from the shard responsible for it.
// Deleting a missing key is a no-op and returns nil.
// Empty key returns ErrInvalidKey.
func (s *Store) Delete(key []byte) error {
	if len(key) == 0 {
		return ErrInvalidKey
	}
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return ErrClosed
	}
	eng := s.shards[s.ring.shardIndex(key)].engine
	s.mu.RUnlock()
	return eng.Delete(key)
}

// Get retrieves the value for key from the shard responsible for it.
// Returns (nil, false, nil) if the key does not exist.
// Empty key returns ErrInvalidKey.
func (s *Store) Get(key []byte) ([]byte, bool, error) {
	if len(key) == 0 {
		return nil, false, ErrInvalidKey
	}
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return nil, false, ErrClosed
	}
	eng := s.shards[s.ring.shardIndex(key)].engine
	s.mu.RUnlock()
	return eng.Get(key)
}

// Scan fans out to all shards and returns all live entries where
// start <= key < end, merged and sorted by key ascending.
// If start is nil, scanning begins at the first key.
// If end is nil, scanning continues to the last key.
// Duplicate keys (e.g. injected by tests) are resolved by highest Seq.
func (s *Store) Scan(start, end []byte) ([]Entry, error) {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return nil, ErrClosed
	}
	shards := s.shards // header copy; elements are stable pointers
	s.mu.RUnlock()

	// Merge results across shards; resolve duplicates by highest Seq.
	type candidate struct {
		seq   uint64
		value []byte
		key   []byte
	}
	best := make(map[string]*candidate)

	for _, sh := range shards {
		entries, err := sh.engine.Scan(start, end)
		if err != nil {
			return nil, fmt.Errorf("shard: scan shard %q: %w", sh.info.Name, err)
		}
		for _, e := range entries {
			ks := string(e.Key)
			if c, ok := best[ks]; ok && e.Seq <= c.seq {
				continue
			}
			val := make([]byte, len(e.Value))
			copy(val, e.Value)
			k := make([]byte, len(e.Key))
			copy(k, e.Key)
			best[ks] = &candidate{seq: e.Seq, value: val, key: k}
		}
	}

	result := make([]Entry, 0, len(best))
	for _, c := range best {
		result = append(result, Entry{Key: c.key, Value: c.value, Seq: c.seq})
	}
	sort.Slice(result, func(i, j int) bool {
		return string(result[i].Key) < string(result[j].Key)
	})
	return result, nil
}

// ShardForKey returns the ShardInfo for the shard responsible for key.
// Empty key returns ErrInvalidKey.
func (s *Store) ShardForKey(key []byte) (ShardInfo, error) {
	if len(key) == 0 {
		return ShardInfo{}, ErrInvalidKey
	}
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return ShardInfo{}, ErrClosed
	}
	info := s.shards[s.ring.shardIndex(key)].info
	s.mu.RUnlock()
	return info, nil
}

// Flush flushes the MemTable of every shard to disk.
// Returns a wrapped error on first shard failure (other shards continue).
func (s *Store) Flush() error {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return ErrClosed
	}
	shards := s.shards
	s.mu.RUnlock()

	for _, sh := range shards {
		if err := sh.engine.Flush(); err != nil {
			return fmt.Errorf("shard: flush shard %d (%s): %w",
				sh.info.ID, sh.info.Name, err)
		}
	}
	return nil
}

// Compact runs full compaction on every shard.
// Returns a wrapped error on first shard failure (other shards continue).
func (s *Store) Compact() error {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return ErrClosed
	}
	shards := s.shards
	s.mu.RUnlock()

	for _, sh := range shards {
		if err := sh.engine.Compact(); err != nil {
			return fmt.Errorf("shard: compact shard %d (%s): %w",
				sh.info.ID, sh.info.Name, err)
		}
	}
	return nil
}

// Stats aggregates statistics across all shards.
// Stats may be called after Close.
func (s *Store) Stats() Stats {
	s.mu.RLock()
	shards := s.shards
	vnodes := s.opts.VirtualNodes
	s.mu.RUnlock()

	st := Stats{
		ShardCount:   len(shards),
		VirtualNodes: vnodes,
		Shards:       make([]ShardStats, 0, len(shards)),
	}
	for _, sh := range shards {
		es := sh.engine.Stats()
		ss := ShardStats{
			ID:                  sh.info.ID,
			Name:                sh.info.Name,
			Path:                sh.info.Path,
			MemTableEntries:     es.MemTableEntries,
			MemTableApproxBytes: es.MemTableApproxBytes,
			SSTableCount:        es.SSTableCount,
			FlushCount:          es.FlushCount,
			CompactionCount:     es.CompactionCount,
		}
		st.Shards = append(st.Shards, ss)
		st.TotalMemTableEntries += es.MemTableEntries
		st.TotalMemTableApproxBytes += es.MemTableApproxBytes
		st.TotalSSTableCount += es.SSTableCount
		st.TotalFlushCount += es.FlushCount
		st.TotalCompactionCount += es.CompactionCount
	}
	return st
}

// Close closes all shard engines. Close is idempotent; calling it twice
// returns nil. After Close, operations return ErrClosed.
func (s *Store) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	shards := s.shards
	s.mu.Unlock()

	var firstErr error
	for _, sh := range shards {
		if err := sh.engine.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("shard: close shard %d (%s): %w",
				sh.info.ID, sh.info.Name, err)
		}
	}
	return firstErr
}

// ── internal helpers ──────────────────────────────────────────────────────────

// closeAllHandles closes all engine handles in a slice. Used during failed Open.
func closeAllHandles(handles []*shardHandle) {
	for _, h := range handles {
		h.engine.Close()
	}
}

// pathExists reports whether the given path exists.
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
