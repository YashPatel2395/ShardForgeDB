// Package vector implements single-node exact vector search for ShardForgeDB.
//
// # Architecture
//
// A Store wraps the existing single-node Engine for durable persistence and
// keeps an in-memory map (ID → Record) for fast exact k-nearest-neighbour
// search. All persistence is Engine-backed; no separate storage format is
// introduced.
//
// # Search
//
// Search is exact brute-force — every stored vector is scored against the
// query. This is O(n·d) in the number of stored vectors n and dimension d.
// It is correct, deterministic, and suitable for small-to-medium workloads.
// It is NOT approximate nearest-neighbour (no HNSW, no IVF).
//
// # Key format
//
// Vectors are stored in the engine under keys of the form:
//
//	__vector__/<namespace>/<id>
//
// The prefix is chosen to be unlikely to collide with user keys and to sort
// predictably so Scan can reload the entire namespace on Open.
//
// # Metrics
//
//   - cosine: cosine similarity (higher = better); zero vectors rejected.
//   - l2:     squared Euclidean distance (lower = better).
//   - dot:    dot product (higher = better).
//
// # Limitations
//
//   - Single node only.  No sharding, replication, or distribution.
//   - Exact search only.  No approximate/ANN.
//   - Manual flush/compact.  No background maintenance.
//   - All stored vectors must fit in memory.
package vector

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/YashPatel2395/ShardForgeDB/internal/engine"
)

// ── Sentinel errors ───────────────────────────────────────────────────────────

var (
	ErrClosed         = errors.New("vector: closed")
	ErrInvalidOptions = errors.New("vector: invalid options")
	ErrInvalidID      = errors.New("vector: invalid id")
	ErrInvalidVector  = errors.New("vector: invalid vector")
	ErrInvalidK       = errors.New("vector: invalid k")
	ErrCorruptRecord  = errors.New("vector: corrupt record")
)

// ── Metric ────────────────────────────────────────────────────────────────────

// Metric selects the distance function used for Search.
type Metric string

const (
	MetricCosine Metric = "cosine"
	MetricL2     Metric = "l2"
	MetricDot    Metric = "dot"
)

// ── Options ───────────────────────────────────────────────────────────────────

// Options configure a Store.
type Options struct {
	// Dir is the path to the engine data directory. Required.
	Dir string
	// Dimension is the fixed vector dimension. Must be > 0.
	Dimension int
	// Metric selects the distance function. Defaults to MetricCosine.
	Metric Metric
	// Namespace scopes the engine keys. Defaults to "vector".
	// Different namespaces within the same Dir do not collide.
	Namespace string
}

func (o *Options) applyDefaults() {
	if o.Metric == "" {
		o.Metric = MetricCosine
	}
	if o.Namespace == "" {
		o.Namespace = "vector"
	}
}

func (o Options) validate() error {
	if o.Dir == "" {
		return fmt.Errorf("%w: Dir is required", ErrInvalidOptions)
	}
	if o.Dimension <= 0 {
		return fmt.Errorf("%w: Dimension must be > 0", ErrInvalidOptions)
	}
	switch o.Metric {
	case MetricCosine, MetricL2, MetricDot:
	default:
		return fmt.Errorf("%w: unknown metric %q (want cosine, l2, dot)", ErrInvalidOptions, o.Metric)
	}
	if strings.ContainsAny(o.Namespace, "/__\x00") {
		return fmt.Errorf("%w: Namespace contains reserved characters", ErrInvalidOptions)
	}
	return nil
}

// ── Record / SearchResult / Stats ─────────────────────────────────────────────

// Record is a stored vector with its ID and optional metadata.
type Record struct {
	ID       string
	Vector   []float32
	Metadata []byte
}

// SearchResult is one item in a ranked Search response.
type SearchResult struct {
	ID       string
	Score    float64
	Distance float64
	Metadata []byte
}

// Stats reports current Store state.
type Stats struct {
	Count      int
	Dimension  int
	Metric     Metric
	EnginePath string

	// Derived from the underlying Engine.
	SSTableCount    int
	FlushCount      uint64
	CompactionCount uint64
}

// ── Store ─────────────────────────────────────────────────────────────────────

// Store is a persistent, exact k-nearest-neighbour vector store.
//
// Concurrent calls to Upsert, Delete, Get, Search, Count, Stats, Flush,
// Compact, and Close are safe.
type Store struct {
	mu     sync.RWMutex
	opts   Options
	eng    *engine.Engine
	index  map[string]Record // in-memory exact index
	closed bool
}

// Open opens (or creates) a vector Store at opts.Dir.
//
// On first open, the directory and an Engine are created.
// On subsequent opens, existing vectors are reloaded from the Engine by
// scanning the vector namespace prefix.
func Open(opts Options) (*Store, error) {
	opts.applyDefaults()
	if err := opts.validate(); err != nil {
		return nil, err
	}

	eng, err := engine.Open(engine.Options{Dir: opts.Dir})
	if err != nil {
		return nil, fmt.Errorf("vector: open engine: %w", err)
	}

	s := &Store{
		opts:  opts,
		eng:   eng,
		index: make(map[string]Record),
	}
	if err := s.loadIndex(); err != nil {
		eng.Close()
		return nil, err
	}
	return s, nil
}

// loadIndex scans the engine for all records in this store's namespace and
// populates the in-memory index.  Called once during Open.
func (s *Store) loadIndex() error {
	prefix := s.keyPrefix()
	// Scan [prefix, prefix+"\xff") to load all namespace records.
	// The end key uses the fact that '\xff' > any printable character.
	startKey := []byte(prefix)
	endKey := []byte(prefix + "\xff")

	entries, err := s.eng.Scan(startKey, endKey)
	if err != nil {
		return fmt.Errorf("vector: scan namespace: %w", err)
	}

	for _, e := range entries {
		id := s.idFromKey(string(e.Key))
		if id == "" {
			continue
		}
		vec, meta, err := decodeRecord(e.Value, s.opts.Dimension)
		if err != nil {
			return fmt.Errorf("vector: corrupt record for id %q: %w", id, err)
		}
		s.index[id] = Record{ID: id, Vector: vec, Metadata: meta}
	}
	return nil
}

// ── Public API ────────────────────────────────────────────────────────────────

// Upsert inserts or replaces the vector for id.
//
// id must be non-empty and must not contain '/' or control characters.
// vector must have exactly opts.Dimension elements and no NaN/Inf values.
// For cosine metric, the zero vector is rejected.
// metadata may be nil or empty.
//
// Defensively copies vector and metadata before storing.
func (s *Store) Upsert(id string, vector []float32, metadata []byte) error {
	if err := s.checkClosed(); err != nil {
		return err
	}
	if err := validateID(id); err != nil {
		return err
	}
	if err := s.validateVector(vector); err != nil {
		return err
	}

	// Defensive copies.
	vec := cloneFloat32(vector)
	meta := cloneBytes(metadata)

	encoded, err := encodeRecord(s.opts.Dimension, vec, meta)
	if err != nil {
		return fmt.Errorf("vector: encode: %w", err)
	}

	key := []byte(s.engineKey(id))

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.eng.Put(key, encoded); err != nil {
		return fmt.Errorf("vector: put: %w", err)
	}
	s.index[id] = Record{ID: id, Vector: vec, Metadata: meta}
	return nil
}

// Delete removes the vector with the given id.
// If the id does not exist, Delete is a safe no-op.
func (s *Store) Delete(id string) error {
	if err := s.checkClosed(); err != nil {
		return err
	}
	if err := validateID(id); err != nil {
		return err
	}

	key := []byte(s.engineKey(id))

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.eng.Delete(key); err != nil {
		return fmt.Errorf("vector: delete: %w", err)
	}
	delete(s.index, id)
	return nil
}

// Get returns the Record for id.
// Returns (Record{}, false, nil) if id is not found.
// The returned Record's Vector and Metadata are defensive copies.
func (s *Store) Get(id string) (Record, bool, error) {
	if err := s.checkClosed(); err != nil {
		return Record{}, false, err
	}

	s.mu.RLock()
	r, ok := s.index[id]
	s.mu.RUnlock()

	if !ok {
		return Record{}, false, nil
	}
	return Record{
		ID:       r.ID,
		Vector:   cloneFloat32(r.Vector),
		Metadata: cloneBytes(r.Metadata),
	}, true, nil
}

// Search returns the top-k most similar records to query.
//
// Results are ordered best-first (highest cosine/dot, lowest L2).
// Ties in score are broken by ID ascending.
//
// k must be > 0.  If k > Count(), all records are returned.
// The returned SearchResult.Metadata fields are defensive copies.
// query is not mutated.
func (s *Store) Search(query []float32, k int) ([]SearchResult, error) {
	if err := s.checkClosed(); err != nil {
		return nil, err
	}
	if k <= 0 {
		return nil, fmt.Errorf("%w: k must be > 0, got %d", ErrInvalidK, k)
	}
	if len(query) != s.opts.Dimension {
		return nil, fmt.Errorf("%w: query dimension %d != store dimension %d",
			ErrInvalidVector, len(query), s.opts.Dimension)
	}
	if hasNonFinite(query) {
		return nil, fmt.Errorf("%w: query contains NaN or Inf", ErrInvalidVector)
	}
	if s.opts.Metric == MetricCosine && isZeroVector(query) {
		return nil, fmt.Errorf("%w: cosine search requires non-zero query vector", ErrInvalidVector)
	}

	// Defensive copy of query so we cannot be affected by caller mutation.
	q := cloneFloat32(query)

	s.mu.RLock()
	// Collect all candidates into a slice while holding the read lock.
	type candidate struct {
		id    string
		score float64
		dist  float64
		meta  []byte
	}
	candidates := make([]candidate, 0, len(s.index))
	for id, rec := range s.index {
		score, dist := computeDistance(s.opts.Metric, q, rec.Vector)
		candidates = append(candidates, candidate{
			id:    id,
			score: score,
			dist:  dist,
			meta:  cloneBytes(rec.Metadata),
		})
	}
	s.mu.RUnlock()

	// Sort: higher score first; tie-break by ID ascending.
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].id < candidates[j].id
	})

	if k > len(candidates) {
		k = len(candidates)
	}
	results := make([]SearchResult, k)
	for i := range results {
		c := candidates[i]
		results[i] = SearchResult{
			ID:       c.id,
			Score:    c.score,
			Distance: c.dist,
			Metadata: c.meta,
		}
	}
	return results, nil
}

// Count returns the number of stored vectors.
func (s *Store) Count() int {
	s.mu.RLock()
	n := len(s.index)
	s.mu.RUnlock()
	return n
}

// Stats returns a snapshot of the Store's current state.
func (s *Store) Stats() Stats {
	s.mu.RLock()
	count := len(s.index)
	s.mu.RUnlock()

	st := Stats{
		Count:      count,
		Dimension:  s.opts.Dimension,
		Metric:     s.opts.Metric,
		EnginePath: s.opts.Dir,
	}

	es := s.eng.Stats()
	st.SSTableCount = es.SSTableCount
	st.FlushCount = es.FlushCount
	st.CompactionCount = es.CompactionCount

	return st
}

// Flush delegates to the underlying Engine's Flush.
// It persists the current MemTable to an SSTable on disk.
func (s *Store) Flush() error {
	if err := s.checkClosed(); err != nil {
		return err
	}
	return s.eng.Flush()
}

// Compact delegates to the underlying Engine's Compact.
// It merges all SSTables into at most one compacted SSTable.
func (s *Store) Compact() error {
	if err := s.checkClosed(); err != nil {
		return err
	}
	return s.eng.Compact()
}

// Close closes the Store and the underlying Engine.
// After Close, all operations return ErrClosed.
// Close is idempotent.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true
	return s.eng.Close()
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func (s *Store) checkClosed() error {
	s.mu.RLock()
	closed := s.closed
	s.mu.RUnlock()
	if closed {
		return ErrClosed
	}
	return nil
}

// keyPrefix returns the namespace prefix for engine keys.
// Format: __vector__/<namespace>/
func (s *Store) keyPrefix() string {
	return "__vector__/" + s.opts.Namespace + "/"
}

// engineKey returns the full engine key for a given ID.
func (s *Store) engineKey(id string) string {
	return s.keyPrefix() + id
}

// idFromKey extracts the user-visible ID from a full engine key.
// Returns "" if the key does not belong to this namespace.
func (s *Store) idFromKey(key string) string {
	prefix := s.keyPrefix()
	if !strings.HasPrefix(key, prefix) {
		return ""
	}
	return key[len(prefix):]
}

// validateID returns an error if id is not a valid vector ID.
func validateID(id string) error {
	if id == "" {
		return fmt.Errorf("%w: id must not be empty", ErrInvalidID)
	}
	if strings.ContainsAny(id, "/\x00") {
		return fmt.Errorf("%w: id must not contain '/' or null bytes", ErrInvalidID)
	}
	for _, r := range id {
		if r < 0x20 {
			return fmt.Errorf("%w: id must not contain control characters", ErrInvalidID)
		}
	}
	return nil
}

// validateVector checks dimension, finiteness, and (for cosine) non-zero.
func (s *Store) validateVector(v []float32) error {
	if len(v) != s.opts.Dimension {
		return fmt.Errorf("%w: got %d elements, want %d", ErrInvalidVector, len(v), s.opts.Dimension)
	}
	if hasNonFinite(v) {
		return fmt.Errorf("%w: vector contains NaN or Inf", ErrInvalidVector)
	}
	if s.opts.Metric == MetricCosine && isZeroVector(v) {
		return fmt.Errorf("%w: cosine metric requires non-zero vector", ErrInvalidVector)
	}
	return nil
}

// cloneFloat32 returns a copy of v.
func cloneFloat32(v []float32) []float32 {
	if v == nil {
		return nil
	}
	out := make([]float32, len(v))
	copy(out, v)
	return out
}

// cloneBytes returns a copy of b, or nil if b is nil/empty.
func cloneBytes(b []byte) []byte {
	if len(b) == 0 {
		return nil
	}
	out := make([]byte, len(b))
	copy(out, b)
	return out
}
