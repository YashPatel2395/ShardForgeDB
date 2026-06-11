// Package vector — ExplainUpsert, ExplainSearch, ExplainDelete
//
// These methods mirror the execution paths of their non-Explain counterparts
// exactly. Each trace step is recorded by the real code that performs the
// operation; no steps are fabricated or pre-scripted.
//
// Raw vectors and metadata values are never written into trace fields.
// Safe metadata only: dimension, metric, k, candidate counts, result counts,
// index size, encoded length.
package vector

import (
	"fmt"
	"sort"
	"time"

	"github.com/YashPatel2395/ShardForgeDB/internal/trace"
)

// ExplainUpsert performs an Upsert and records a step-by-step trace.
//
// Trace steps:
//  1. ID + vector validation
//  2. Vector encoding
//  3. Engine write (WAL + MemTable via engine.Put)
//  4. In-memory index update
//
// The non-Explain Upsert behaviour is preserved exactly.
func (s *Store) ExplainUpsert(id string, vector []float32, metadata []byte) (*trace.Trace, error) {
	tr := trace.New(trace.OpVectorInsert, id)

	// Validate ID before acquiring the lock (s.opts is immutable).
	t0 := time.Now()
	if err := validateID(id); err != nil {
		tr.Step(trace.ComponentVector, trace.StepTypeVectorValidate, trace.StatusError, time.Since(t0),
			"id_valid=false")
		tr.Finish(err)
		return tr, err
	}
	if err := s.validateVector(vector); err != nil {
		tr.Step(trace.ComponentVector, trace.StepTypeVectorValidate, trace.StatusError, time.Since(t0),
			fmt.Sprintf("vector_valid=false dimension=%d metric=%s", s.opts.Dimension, s.opts.Metric))
		tr.Finish(err)
		return tr, err
	}
	valDur := time.Since(t0)
	tr.Step(trace.ComponentVector, trace.StepTypeVectorValidate, trace.StatusOK, valDur,
		fmt.Sprintf("id_len=%d dimension=%d metric=%s", len(id), s.opts.Dimension, s.opts.Metric))

	// Defensive copies and encoding can happen before the lock.
	t1 := time.Now()
	vec := cloneFloat32(vector)
	meta := cloneBytes(metadata)
	encoded, err := encodeRecord(s.opts.Dimension, vec, meta)
	encodeDur := time.Since(t1)
	if err != nil {
		tr.Step(trace.ComponentVector, trace.StepTypeVectorEncode, trace.StatusError, encodeDur,
			"encode_failed=true")
		wrapped := fmt.Errorf("vector: encode: %w", err)
		tr.Finish(wrapped)
		return tr, wrapped
	}
	tr.Step(trace.ComponentVector, trace.StepTypeVectorEncode, trace.StatusOK, encodeDur,
		fmt.Sprintf("dimension=%d encoded_len=%d", s.opts.Dimension, len(encoded)))

	key := []byte(s.engineKey(id))

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		tr.Finish(ErrClosed)
		return tr, ErrClosed
	}

	// Engine write (WAL + MemTable).
	t2 := time.Now()
	if err := s.eng.Put(key, encoded); err != nil {
		engDur := time.Since(t2)
		tr.Step(trace.ComponentVector, trace.StepTypeVectorEngineWrite, trace.StatusError, engDur,
			fmt.Sprintf("key_len=%d", len(key)))
		wrapped := fmt.Errorf("vector: put: %w", err)
		tr.Finish(wrapped)
		return tr, wrapped
	}
	engDur := time.Since(t2)
	tr.Step(trace.ComponentVector, trace.StepTypeVectorEngineWrite, trace.StatusOK, engDur,
		fmt.Sprintf("key_len=%d value_len=%d", len(key), len(encoded)))

	// In-memory index update.
	t3 := time.Now()
	s.index[id] = Record{ID: id, Vector: vec, Metadata: meta}
	indexDur := time.Since(t3)
	tr.Step(trace.ComponentVector, trace.StepTypeVectorIndexUpdate, trace.StatusOK, indexDur,
		fmt.Sprintf("index_size=%d", len(s.index)))

	tr.Finish(nil)
	return tr, nil
}

// ExplainSearch performs a Search and records a step-by-step trace.
//
// Trace steps:
//  1. Query validation (k, dimension, finiteness, cosine zero-check)
//  2. Candidate load (index size / namespace)
//  3. Distance computation for all candidates (metric + pair count)
//  4. Top-k sort and trim (k, result count)
//
// Raw query vectors and stored vectors are never written into trace fields.
// The non-Explain Search behaviour is preserved exactly.
func (s *Store) ExplainSearch(query []float32, k int) (*trace.Trace, []SearchResult, error) {
	tr := trace.New(trace.OpVectorSearch, "")

	// Validate k and query using immutable s.opts — no lock needed.
	if k <= 0 {
		err := fmt.Errorf("%w: k must be > 0, got %d", ErrInvalidK, k)
		tr.Step(trace.ComponentVector, trace.StepTypeVectorValidate, trace.StatusError, 0,
			fmt.Sprintf("k=%d", k))
		tr.Finish(err)
		return tr, nil, err
	}
	if len(query) != s.opts.Dimension {
		err := fmt.Errorf("%w: query dimension %d != store dimension %d",
			ErrInvalidVector, len(query), s.opts.Dimension)
		tr.Step(trace.ComponentVector, trace.StepTypeVectorValidate, trace.StatusError, 0,
			fmt.Sprintf("query_dim=%d store_dim=%d", len(query), s.opts.Dimension))
		tr.Finish(err)
		return tr, nil, err
	}
	if hasNonFinite(query) {
		err := fmt.Errorf("%w: query contains NaN or Inf", ErrInvalidVector)
		tr.Step(trace.ComponentVector, trace.StepTypeVectorValidate, trace.StatusError, 0,
			"non_finite=true")
		tr.Finish(err)
		return tr, nil, err
	}
	if s.opts.Metric == MetricCosine && isZeroVector(query) {
		err := fmt.Errorf("%w: cosine search requires non-zero query vector", ErrInvalidVector)
		tr.Step(trace.ComponentVector, trace.StepTypeVectorValidate, trace.StatusError, 0,
			"metric=cosine zero_vector=true")
		tr.Finish(err)
		return tr, nil, err
	}
	tr.Step(trace.ComponentVector, trace.StepTypeVectorValidate, trace.StatusOK, 0,
		fmt.Sprintf("dimension=%d metric=%s k=%d", s.opts.Dimension, s.opts.Metric, k))

	// Defensive copy of query before acquiring the lock.
	q := cloneFloat32(query)

	s.mu.RLock()

	if s.closed {
		s.mu.RUnlock()
		tr.Finish(ErrClosed)
		return tr, nil, ErrClosed
	}

	candidateCount := len(s.index)
	tr.Step(trace.ComponentVector, trace.StepTypeVectorLoad, trace.StatusOK, 0,
		fmt.Sprintf("namespace=%s candidate_count=%d", s.opts.Namespace, candidateCount))

	// Collect all candidates while holding the read lock.
	type candidate struct {
		id    string
		score float64
		dist  float64
		meta  []byte
	}
	candidates := make([]candidate, 0, len(s.index))

	t0 := time.Now()
	for id, rec := range s.index {
		score, dist := computeDistance(s.opts.Metric, q, rec.Vector)
		candidates = append(candidates, candidate{
			id:    id,
			score: score,
			dist:  dist,
			meta:  cloneBytes(rec.Metadata),
		})
	}
	computeDur := time.Since(t0)
	s.mu.RUnlock()

	tr.Step(trace.ComponentVector, trace.StepTypeVectorCompute, trace.StatusOK, computeDur,
		fmt.Sprintf("metric=%s pairs=%d", s.opts.Metric, len(candidates)))

	// Sort and trim outside the lock.
	t1 := time.Now()
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
	topkDur := time.Since(t1)

	tr.Step(trace.ComponentVector, trace.StepTypeVectorTopK, trace.StatusOK, topkDur,
		fmt.Sprintf("k=%d result_count=%d", k, len(results)))

	tr.Finish(nil)
	return tr, results, nil
}

// ExplainDelete performs a Delete and records a step-by-step trace.
//
// Trace steps:
//  1. ID validation
//  2. Engine delete (WAL tombstone)
//  3. In-memory index removal
//
// The non-Explain Delete behaviour is preserved exactly.
func (s *Store) ExplainDelete(id string) (*trace.Trace, error) {
	tr := trace.New(trace.OpDelete, id)

	t0 := time.Now()
	if err := validateID(id); err != nil {
		tr.Step(trace.ComponentVector, trace.StepTypeVectorValidate, trace.StatusError, time.Since(t0),
			"id_valid=false")
		tr.Finish(err)
		return tr, err
	}
	valDur := time.Since(t0)
	tr.Step(trace.ComponentVector, trace.StepTypeVectorValidate, trace.StatusOK, valDur,
		fmt.Sprintf("id_len=%d", len(id)))

	key := []byte(s.engineKey(id))

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		tr.Finish(ErrClosed)
		return tr, ErrClosed
	}

	// Engine delete (WAL tombstone + MemTable).
	t1 := time.Now()
	if err := s.eng.Delete(key); err != nil {
		engDur := time.Since(t1)
		tr.Step(trace.ComponentVector, trace.StepTypeVectorEngineWrite, trace.StatusError, engDur,
			fmt.Sprintf("key_len=%d tombstone=true", len(key)))
		wrapped := fmt.Errorf("vector: delete: %w", err)
		tr.Finish(wrapped)
		return tr, wrapped
	}
	engDur := time.Since(t1)
	tr.Step(trace.ComponentVector, trace.StepTypeVectorEngineWrite, trace.StatusOK, engDur,
		fmt.Sprintf("key_len=%d tombstone=true", len(key)))

	// In-memory index removal.
	t2 := time.Now()
	delete(s.index, id)
	indexDur := time.Since(t2)
	tr.Step(trace.ComponentVector, trace.StepTypeVectorIndexDelete, trace.StatusOK, indexDur,
		fmt.Sprintf("index_size=%d", len(s.index)))

	tr.Finish(nil)
	return tr, nil
}
