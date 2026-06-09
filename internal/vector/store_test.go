package vector

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sync"
	"testing"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func openStore(t *testing.T, metric Metric) *Store {
	t.Helper()
	s, err := Open(Options{
		Dir:       t.TempDir(),
		Dimension: 4,
		Metric:    metric,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func vec(vals ...float32) []float32 { return vals }

func mustUpsert(t *testing.T, s *Store, id string, v []float32, meta []byte) {
	t.Helper()
	if err := s.Upsert(id, v, meta); err != nil {
		t.Fatalf("Upsert(%q): %v", id, err)
	}
}

// ── 1. Open: invalid options ──────────────────────────────────────────────────

func TestOpen_RejectsEmptyDir(t *testing.T) {
	_, err := Open(Options{Dimension: 4, Metric: MetricCosine})
	if !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("want ErrInvalidOptions, got %v", err)
	}
}

func TestOpen_RejectsZeroDimension(t *testing.T) {
	_, err := Open(Options{Dir: t.TempDir(), Dimension: 0, Metric: MetricCosine})
	if !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("want ErrInvalidOptions, got %v", err)
	}
}

func TestOpen_DefaultsMetricToCosine(t *testing.T) {
	s, err := Open(Options{Dir: t.TempDir(), Dimension: 4})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if s.opts.Metric != MetricCosine {
		t.Errorf("default metric = %q, want cosine", s.opts.Metric)
	}
}

func TestOpen_RejectsUnknownMetric(t *testing.T) {
	_, err := Open(Options{Dir: t.TempDir(), Dimension: 4, Metric: "hamming"})
	if !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("want ErrInvalidOptions, got %v", err)
	}
}

// ── 5–10. Upsert validation ───────────────────────────────────────────────────

func TestUpsert_RejectsEmptyID(t *testing.T) {
	s := openStore(t, MetricCosine)
	err := s.Upsert("", vec(1, 0, 0, 0), nil)
	if !errors.Is(err, ErrInvalidID) {
		t.Fatalf("want ErrInvalidID, got %v", err)
	}
}

func TestUpsert_RejectsIDWithSlash(t *testing.T) {
	s := openStore(t, MetricCosine)
	err := s.Upsert("a/b", vec(1, 0, 0, 0), nil)
	if !errors.Is(err, ErrInvalidID) {
		t.Fatalf("want ErrInvalidID, got %v", err)
	}
}

func TestUpsert_RejectsWrongDimension(t *testing.T) {
	s := openStore(t, MetricCosine)
	err := s.Upsert("id", vec(1, 0), nil) // dim=2, want 4
	if !errors.Is(err, ErrInvalidVector) {
		t.Fatalf("want ErrInvalidVector, got %v", err)
	}
}

func TestUpsert_RejectsNaN(t *testing.T) {
	s := openStore(t, MetricL2)
	err := s.Upsert("id", vec(float32(math.NaN()), 0, 0, 0), nil)
	if !errors.Is(err, ErrInvalidVector) {
		t.Fatalf("want ErrInvalidVector, got %v", err)
	}
}

func TestUpsert_RejectsInf(t *testing.T) {
	s := openStore(t, MetricL2)
	err := s.Upsert("id", vec(float32(math.Inf(1)), 0, 0, 0), nil)
	if !errors.Is(err, ErrInvalidVector) {
		t.Fatalf("want ErrInvalidVector, got %v", err)
	}
}

func TestUpsert_CosineRejectsZeroVector(t *testing.T) {
	s := openStore(t, MetricCosine)
	err := s.Upsert("id", vec(0, 0, 0, 0), nil)
	if !errors.Is(err, ErrInvalidVector) {
		t.Fatalf("want ErrInvalidVector for cosine+zero, got %v", err)
	}
}

// ── 11. Cosine rejects zero query ─────────────────────────────────────────────

func TestSearch_CosineRejectsZeroQuery(t *testing.T) {
	s := openStore(t, MetricCosine)
	mustUpsert(t, s, "a", vec(1, 0, 0, 0), nil)
	_, err := s.Search(vec(0, 0, 0, 0), 1)
	if !errors.Is(err, ErrInvalidVector) {
		t.Fatalf("want ErrInvalidVector, got %v", err)
	}
}

// ── 12–14. Get / Upsert / overwrite ──────────────────────────────────────────

func TestUpsertGet_RoundTrip(t *testing.T) {
	s := openStore(t, MetricCosine)
	v := vec(1, 2, 3, 4)
	meta := []byte("hello")
	mustUpsert(t, s, "abc", v, meta)

	r, ok, err := s.Get("abc")
	if err != nil || !ok {
		t.Fatalf("Get: ok=%v err=%v", ok, err)
	}
	if len(r.Vector) != 4 {
		t.Fatalf("Vector length %d, want 4", len(r.Vector))
	}
	for i, x := range v {
		if r.Vector[i] != x {
			t.Errorf("Vector[%d] = %v, want %v", i, r.Vector[i], x)
		}
	}
	if string(r.Metadata) != "hello" {
		t.Errorf("Metadata = %q, want %q", r.Metadata, "hello")
	}
}

func TestGet_MissingReturnsFalse(t *testing.T) {
	s := openStore(t, MetricCosine)
	_, ok, err := s.Get("nonexistent")
	if err != nil {
		t.Fatalf("Get: unexpected error: %v", err)
	}
	if ok {
		t.Error("Get missing: expected ok=false")
	}
}

func TestUpsert_OverwritesExisting(t *testing.T) {
	s := openStore(t, MetricL2)
	mustUpsert(t, s, "id", vec(1, 0, 0, 0), []byte("v1"))
	mustUpsert(t, s, "id", vec(0, 1, 0, 0), []byte("v2"))

	r, ok, err := s.Get("id")
	if err != nil || !ok {
		t.Fatalf("Get: ok=%v err=%v", ok, err)
	}
	if r.Vector[1] != 1 {
		t.Errorf("expected overwritten vector, got %v", r.Vector)
	}
	if string(r.Metadata) != "v2" {
		t.Errorf("expected overwritten metadata, got %q", r.Metadata)
	}
}

// ── 15–16. Delete ─────────────────────────────────────────────────────────────

func TestDelete_RemovesVector(t *testing.T) {
	s := openStore(t, MetricCosine)
	mustUpsert(t, s, "x", vec(1, 0, 0, 0), nil)
	if err := s.Delete("x"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, ok, _ := s.Get("x")
	if ok {
		t.Error("expected vector to be deleted")
	}
}

func TestDelete_MissingIsSafe(t *testing.T) {
	s := openStore(t, MetricCosine)
	if err := s.Delete("no-such-id"); err != nil {
		t.Fatalf("Delete missing: %v", err)
	}
}

// ── 17–18. Search: k and dimension validation ─────────────────────────────────

func TestSearch_RejectsInvalidK(t *testing.T) {
	s := openStore(t, MetricCosine)
	_, err := s.Search(vec(1, 0, 0, 0), 0)
	if !errors.Is(err, ErrInvalidK) {
		t.Fatalf("want ErrInvalidK, got %v", err)
	}
}

func TestSearch_RejectsWrongQueryDimension(t *testing.T) {
	s := openStore(t, MetricCosine)
	_, err := s.Search(vec(1, 0), 1) // dim=2, want 4
	if !errors.Is(err, ErrInvalidVector) {
		t.Fatalf("want ErrInvalidVector, got %v", err)
	}
}

// ── 19–21. Search ranking ─────────────────────────────────────────────────────

func TestSearch_CosineRanking(t *testing.T) {
	s := openStore(t, MetricCosine)
	// a: aligned with query → cosine=1
	// b: orthogonal         → cosine=0
	// c: opposite           → cosine=-1
	mustUpsert(t, s, "a", vec(1, 0, 0, 0), nil)
	mustUpsert(t, s, "b", vec(0, 1, 0, 0), nil)
	mustUpsert(t, s, "c", vec(-1, 0, 0, 0), nil)

	results, err := s.Search(vec(1, 0, 0, 0), 3)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	order := []string{results[0].ID, results[1].ID, results[2].ID}
	want := []string{"a", "b", "c"}
	for i, id := range want {
		if order[i] != id {
			t.Errorf("rank %d: got %q, want %q (all ranks: %v)", i, order[i], id, order)
		}
	}
}

func TestSearch_L2Ranking(t *testing.T) {
	s := openStore(t, MetricL2)
	// Closest to query [1,0,0,0] should be [1,0,0,0] (dist=0), then [1,1,0,0], then [0,0,0,0].
	mustUpsert(t, s, "exact", vec(1, 0, 0, 0), nil)
	mustUpsert(t, s, "close", vec(1, 1, 0, 0), nil)
	mustUpsert(t, s, "far", vec(0, 0, 0, 0), nil)

	results, err := s.Search(vec(1, 0, 0, 0), 3)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if results[0].ID != "exact" {
		t.Errorf("rank 0 = %q, want exact", results[0].ID)
	}
	if results[1].ID != "close" {
		t.Errorf("rank 1 = %q, want close", results[1].ID)
	}
	if results[2].ID != "far" {
		t.Errorf("rank 2 = %q, want far", results[2].ID)
	}
}

func TestSearch_DotRanking(t *testing.T) {
	s := openStore(t, MetricDot)
	// Dot product with [1,0,0,0]: "high"=5, "mid"=2, "low"=1.
	mustUpsert(t, s, "high", vec(5, 0, 0, 0), nil)
	mustUpsert(t, s, "mid", vec(2, 0, 0, 0), nil)
	mustUpsert(t, s, "low", vec(1, 0, 0, 0), nil)

	results, err := s.Search(vec(1, 0, 0, 0), 3)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	order := []string{results[0].ID, results[1].ID, results[2].ID}
	want := []string{"high", "mid", "low"}
	for i, id := range want {
		if order[i] != id {
			t.Errorf("rank %d: got %q, want %q", i, order[i], id)
		}
	}
}

// ── 22. Tie-breaking by ID ────────────────────────────────────────────────────

func TestSearch_TieBreakByID(t *testing.T) {
	s := openStore(t, MetricCosine)
	// All vectors identical magnitude in [1,0,0,0] direction → same cosine score.
	mustUpsert(t, s, "z", vec(1, 0, 0, 0), nil)
	mustUpsert(t, s, "a", vec(1, 0, 0, 0), nil)
	mustUpsert(t, s, "m", vec(1, 0, 0, 0), nil)

	results, err := s.Search(vec(1, 0, 0, 0), 3)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if results[0].ID != "a" || results[1].ID != "m" || results[2].ID != "z" {
		ids := []string{results[0].ID, results[1].ID, results[2].ID}
		t.Errorf("tie-breaking order = %v, want [a m z]", ids)
	}
}

// ── 23. k > count returns all ─────────────────────────────────────────────────

func TestSearch_KGreaterThanCount(t *testing.T) {
	s := openStore(t, MetricCosine)
	mustUpsert(t, s, "a", vec(1, 0, 0, 0), nil)
	mustUpsert(t, s, "b", vec(0, 1, 0, 0), nil)

	results, err := s.Search(vec(1, 0, 0, 0), 100)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results (all), got %d", len(results))
	}
}

// ── 24–27. Defensive copies ───────────────────────────────────────────────────

func TestSearch_ReturnsMetadataCopies(t *testing.T) {
	s := openStore(t, MetricCosine)
	mustUpsert(t, s, "a", vec(1, 0, 0, 0), []byte("orig"))

	results, _ := s.Search(vec(1, 0, 0, 0), 1)
	results[0].Metadata[0] = 'X' // mutate returned metadata

	r, _, _ := s.Get("a")
	if string(r.Metadata) != "orig" {
		t.Errorf("metadata was mutated through Search result: %q", r.Metadata)
	}
}

func TestGet_ReturnsDefensiveCopies(t *testing.T) {
	s := openStore(t, MetricCosine)
	mustUpsert(t, s, "a", vec(1, 0, 0, 0), []byte("meta"))

	r1, _, _ := s.Get("a")
	r1.Vector[0] = 99
	r1.Metadata[0] = 'Z'

	r2, _, _ := s.Get("a")
	if r2.Vector[0] != 1 {
		t.Error("Get: vector was mutated through returned copy")
	}
	if string(r2.Metadata) != "meta" {
		t.Errorf("Get: metadata was mutated through returned copy: %q", r2.Metadata)
	}
}

func TestUpsert_DefensivelyCopiesCaller(t *testing.T) {
	s := openStore(t, MetricCosine)
	v := []float32{1, 0, 0, 0}
	meta := []byte("before")
	mustUpsert(t, s, "a", v, meta)

	// Mutate caller's slices after Upsert.
	v[0] = 99
	meta[0] = 'X'

	r, _, _ := s.Get("a")
	if r.Vector[0] != 1 {
		t.Error("Upsert did not defensively copy vector")
	}
	if string(r.Metadata) != "before" {
		t.Errorf("Upsert did not defensively copy metadata: %q", r.Metadata)
	}
}

func TestSearch_DoesNotMutateQuery(t *testing.T) {
	s := openStore(t, MetricCosine)
	mustUpsert(t, s, "a", vec(1, 0, 0, 0), nil)

	q := []float32{1, 0, 0, 0}
	orig := make([]float32, len(q))
	copy(orig, q)

	_, _ = s.Search(q, 1)

	for i, x := range orig {
		if q[i] != x {
			t.Errorf("Search mutated query at index %d: %v → %v", i, x, q[i])
		}
	}
}

// ── 28–29. Close behaviour ────────────────────────────────────────────────────

func TestClose_MakesOptsReturnErrClosed(t *testing.T) {
	s, _ := Open(Options{Dir: t.TempDir(), Dimension: 4, Metric: MetricCosine})
	s.Close()

	if err := s.Upsert("x", vec(1, 0, 0, 0), nil); !errors.Is(err, ErrClosed) {
		t.Errorf("Upsert after Close: want ErrClosed, got %v", err)
	}
	if err := s.Delete("x"); !errors.Is(err, ErrClosed) {
		t.Errorf("Delete after Close: want ErrClosed, got %v", err)
	}
	if _, _, err := s.Get("x"); !errors.Is(err, ErrClosed) {
		t.Errorf("Get after Close: want ErrClosed, got %v", err)
	}
	if _, err := s.Search(vec(1, 0, 0, 0), 1); !errors.Is(err, ErrClosed) {
		t.Errorf("Search after Close: want ErrClosed, got %v", err)
	}
	if err := s.Flush(); !errors.Is(err, ErrClosed) {
		t.Errorf("Flush after Close: want ErrClosed, got %v", err)
	}
	if err := s.Compact(); !errors.Is(err, ErrClosed) {
		t.Errorf("Compact after Close: want ErrClosed, got %v", err)
	}
}

func TestClose_IsIdempotent(t *testing.T) {
	s, _ := Open(Options{Dir: t.TempDir(), Dimension: 4, Metric: MetricCosine})
	if err := s.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// ── 30–32. Persistence / reopen ───────────────────────────────────────────────

func reopenStore(t *testing.T, dir string, metric Metric) *Store {
	t.Helper()
	s, err := Open(Options{Dir: dir, Dimension: 4, Metric: metric})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestReopen_RestoresVectors(t *testing.T) {
	dir := t.TempDir()
	s1, _ := Open(Options{Dir: dir, Dimension: 4, Metric: MetricCosine})
	mustUpsert(t, s1, "a", vec(1, 0, 0, 0), nil)
	s1.Flush()
	s1.Close()

	s2 := reopenStore(t, dir, MetricCosine)
	r, ok, err := s2.Get("a")
	if err != nil || !ok {
		t.Fatalf("Get after reopen: ok=%v err=%v", ok, err)
	}
	if r.Vector[0] != 1 {
		t.Errorf("Vector[0] = %v, want 1", r.Vector[0])
	}
}

func TestReopen_PreservesMetadata(t *testing.T) {
	dir := t.TempDir()
	s1, _ := Open(Options{Dir: dir, Dimension: 4, Metric: MetricCosine})
	mustUpsert(t, s1, "a", vec(1, 0, 0, 0), []byte("my-meta"))
	s1.Flush()
	s1.Close()

	s2 := reopenStore(t, dir, MetricCosine)
	r, ok, _ := s2.Get("a")
	if !ok {
		t.Fatal("vector not found after reopen")
	}
	if string(r.Metadata) != "my-meta" {
		t.Errorf("Metadata = %q, want my-meta", r.Metadata)
	}
}

func TestReopen_AfterDelete_DoesNotRestore(t *testing.T) {
	dir := t.TempDir()
	s1, _ := Open(Options{Dir: dir, Dimension: 4, Metric: MetricCosine})
	mustUpsert(t, s1, "a", vec(1, 0, 0, 0), nil)
	s1.Flush()
	s1.Delete("a")
	s1.Flush()
	s1.Close()

	s2 := reopenStore(t, dir, MetricCosine)
	_, ok, _ := s2.Get("a")
	if ok {
		t.Error("deleted vector resurrected after reopen")
	}
}

// ── 33. Namespace isolation ───────────────────────────────────────────────────

func TestNamespace_Isolation(t *testing.T) {
	dir := t.TempDir()

	sa, _ := Open(Options{Dir: dir, Dimension: 4, Metric: MetricCosine, Namespace: "ns-a"})
	defer sa.Close()
	sb, _ := Open(Options{Dir: dir, Dimension: 4, Metric: MetricCosine, Namespace: "ns-b"})
	defer sb.Close()

	mustUpsert(t, sa, "id1", vec(1, 0, 0, 0), []byte("from-a"))

	// sb should not see sa's vector.
	_, ok, _ := sb.Get("id1")
	if ok {
		t.Error("namespace leak: ns-b sees ns-a's vector")
	}
	if sb.Count() != 0 {
		t.Errorf("ns-b Count = %d, want 0", sb.Count())
	}
}

// ── 34–35. Flush and Compact ──────────────────────────────────────────────────

func TestFlush_PersistsThroughReopen(t *testing.T) {
	dir := t.TempDir()
	s1, _ := Open(Options{Dir: dir, Dimension: 4, Metric: MetricL2})
	mustUpsert(t, s1, "a", vec(1, 0, 0, 0), nil)
	if err := s1.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	s1.Close()

	s2 := reopenStore(t, dir, MetricL2)
	if s2.Count() != 1 {
		t.Errorf("Count after flush+reopen = %d, want 1", s2.Count())
	}
}

func TestCompact_PreservesVectors(t *testing.T) {
	dir := t.TempDir()
	s1, _ := Open(Options{Dir: dir, Dimension: 4, Metric: MetricL2})
	for i := 0; i < 5; i++ {
		id := string(rune('a' + i))
		mustUpsert(t, s1, id, vec(float32(i), 0, 0, 0), nil)
		s1.Flush()
	}
	if err := s1.Compact(); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	s1.Close()

	s2 := reopenStore(t, dir, MetricL2)
	if s2.Count() != 5 {
		t.Errorf("Count after compact+reopen = %d, want 5", s2.Count())
	}
}

// ── 36. Corrupt record ───────────────────────────────────────────────────────

func TestOpen_CorruptRecordReturnsError(t *testing.T) {
	dir := t.TempDir()
	s1, _ := Open(Options{Dir: dir, Dimension: 4, Metric: MetricCosine})

	// Inject a corrupt record directly into the engine using the internal key
	// format.
	key := []byte(s1.engineKey("bad"))
	corrupt := []byte("this is not a valid encoded record")
	s1.eng.Put(key, corrupt)
	s1.eng.Flush()
	s1.Close()

	_, err := Open(Options{Dir: dir, Dimension: 4, Metric: MetricCosine})
	if err == nil {
		t.Fatal("expected error opening store with corrupt record, got nil")
	}
}

// ── 37. Concurrent safety ─────────────────────────────────────────────────────

func TestConcurrent_UpsertGetSearch(t *testing.T) {
	s := openStore(t, MetricL2)

	// Seed some vectors before concurrent phase.
	for i := 0; i < 20; i++ {
		id := string(rune('a' + i%26))
		_ = s.Upsert(id, vec(float32(i), 0, 0, 0), nil)
	}

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := string(rune('A' + n%26))
			_ = s.Upsert(id, vec(float32(n), 0, 0, 0), nil)
			_, _, _ = s.Get(id)
			_, _ = s.Search(vec(1, 0, 0, 0), 3)
		}(g)
	}
	wg.Wait()
}

// ── 38. Large deterministic workload ─────────────────────────────────────────

func TestLargeWorkload_InsertSearchReopen(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large workload in short mode")
	}
	dir := t.TempDir()

	const n = 1000
	const dim = 16

	s1, err := Open(Options{Dir: dir, Dimension: dim, Metric: MetricDot})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	rng := rand.New(rand.NewSource(42))
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		ids[i] = fmt.Sprintf("id-%04d", i)
		v := make([]float32, dim)
		for j := range v {
			v[j] = rng.Float32()*2 - 1
		}
		if err := s1.Upsert(ids[i], v, nil); err != nil {
			t.Fatalf("Upsert %d: %v", i, err)
		}
		if i%100 == 99 {
			s1.Flush()
		}
	}
	s1.Flush()

	// Search before reopen.
	query := make([]float32, dim)
	for j := range query {
		query[j] = rng.Float32()*2 - 1
	}
	results1, err := s1.Search(query, 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	s1.Close()

	// Reopen and search again — must get same top-5.
	s2, err := Open(Options{Dir: dir, Dimension: dim, Metric: MetricDot})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()

	if s2.Count() != n {
		t.Errorf("Count after reopen = %d, want %d", s2.Count(), n)
	}

	results2, err := s2.Search(query, 5)
	if err != nil {
		t.Fatalf("Search after reopen: %v", err)
	}

	for i := range results1 {
		if results1[i].ID != results2[i].ID {
			t.Errorf("rank %d: before=%q after=%q (IDs differ after reopen)", i, results1[i].ID, results2[i].ID)
		}
	}
}

// ── 39. Codec round-trip ──────────────────────────────────────────────────────

func TestCodec_RoundTrip(t *testing.T) {
	v := []float32{1.1, -2.2, 3.3, 0}
	meta := []byte("metadata-bytes")

	encoded, err := encodeRecord(4, v, meta)
	if err != nil {
		t.Fatalf("encodeRecord: %v", err)
	}

	decoded, decodedMeta, err := decodeRecord(encoded, 4)
	if err != nil {
		t.Fatalf("decodeRecord: %v", err)
	}
	for i, x := range v {
		if decoded[i] != x {
			t.Errorf("Vector[%d]: got %v, want %v", i, decoded[i], x)
		}
	}
	if string(decodedMeta) != string(meta) {
		t.Errorf("Metadata: got %q, want %q", decodedMeta, meta)
	}
}

// ── 40. Codec rejects bad records ────────────────────────────────────────────

func TestCodec_RejectsBadMagic(t *testing.T) {
	encoded, _ := encodeRecord(4, []float32{1, 0, 0, 0}, nil)
	encoded[0] ^= 0xFF // corrupt header magic
	_, _, err := decodeRecord(encoded, 4)
	if !errors.Is(err, ErrCorruptRecord) {
		t.Fatalf("want ErrCorruptRecord, got %v", err)
	}
}

func TestCodec_RejectsBadFooterMagic(t *testing.T) {
	encoded, _ := encodeRecord(4, []float32{1, 0, 0, 0}, nil)
	encoded[len(encoded)-1] ^= 0xFF // corrupt footer magic
	_, _, err := decodeRecord(encoded, 4)
	if !errors.Is(err, ErrCorruptRecord) {
		t.Fatalf("want ErrCorruptRecord, got %v", err)
	}
}

func TestCodec_RejectsBadCRC(t *testing.T) {
	encoded, _ := encodeRecord(4, []float32{1, 0, 0, 0}, nil)
	// The CRC is 4 bytes before the 8-byte footer magic.
	crcOffset := len(encoded) - 12
	encoded[crcOffset] ^= 0xFF
	_, _, err := decodeRecord(encoded, 4)
	if !errors.Is(err, ErrCorruptRecord) {
		t.Fatalf("want ErrCorruptRecord, got %v", err)
	}
}

func TestCodec_RejectsTruncated(t *testing.T) {
	encoded, _ := encodeRecord(4, []float32{1, 0, 0, 0}, nil)
	_, _, err := decodeRecord(encoded[:len(encoded)/2], 4)
	if !errors.Is(err, ErrCorruptRecord) {
		t.Fatalf("want ErrCorruptRecord for truncated, got %v", err)
	}
}

func TestCodec_RejectsDimensionMismatch(t *testing.T) {
	encoded, _ := encodeRecord(4, []float32{1, 0, 0, 0}, nil)
	_, _, err := decodeRecord(encoded, 8) // decode with wrong dim
	if !errors.Is(err, ErrCorruptRecord) {
		t.Fatalf("want ErrCorruptRecord for dim mismatch, got %v", err)
	}
}
