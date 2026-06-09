package vector

import (
	"fmt"
	"math/rand"
	"sync"
	"testing"
)

// randVec returns a deterministic pseudo-random float32 slice of length dim.
func randVec(rng *rand.Rand, dim int) []float32 {
	v := make([]float32, dim)
	for i := range v {
		v[i] = rng.Float32()*2 - 1
	}
	return v
}

// seedStore inserts n vectors of the given dimension into a fresh store and
// flushes them. Returns the store and a random query vector.
func seedStore(b *testing.B, dir string, n, dim int, metric Metric) (*Store, []float32) {
	b.Helper()
	s, err := Open(Options{Dir: dir, Dimension: dim, Metric: metric})
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < n; i++ {
		v := randVec(rng, dim)
		// Ensure non-zero for cosine.
		v[0] += 0.1
		if err := s.Upsert(fmt.Sprintf("id-%06d", i), v, nil); err != nil {
			b.Fatalf("Upsert: %v", err)
		}
	}
	if err := s.Flush(); err != nil {
		b.Fatalf("Flush: %v", err)
	}
	query := randVec(rng, dim)
	query[0] += 0.1
	return s, query
}

// BenchmarkUpsert_1k_dim128 measures Put throughput for 1,000 vectors of dimension 128.
func BenchmarkUpsert_1k_dim128(b *testing.B) {
	const n, dim = 1_000, 128
	rng := rand.New(rand.NewSource(0))
	vecs := make([][]float32, n)
	for i := range vecs {
		vecs[i] = randVec(rng, dim)
		vecs[i][0] += 0.1
	}

	b.ResetTimer()
	for iter := 0; iter < b.N; iter++ {
		b.StopTimer()
		s, _ := Open(Options{Dir: b.TempDir(), Dimension: dim, Metric: MetricCosine})
		b.StartTimer()
		for i, v := range vecs {
			_ = s.Upsert(fmt.Sprintf("id-%06d", i), v, nil)
		}
		b.StopTimer()
		s.Close()
		b.StartTimer()
	}
}

// BenchmarkSearch_1k_dim128_Cosine measures exact Search over 1,000 dim-128 vectors.
func BenchmarkSearch_1k_dim128_Cosine(b *testing.B) {
	s, q := seedStore(b, b.TempDir(), 1_000, 128, MetricCosine)
	defer s.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = s.Search(q, 10)
	}
}

// BenchmarkSearch_10k_dim128_Cosine measures exact Search over 10,000 dim-128 vectors.
func BenchmarkSearch_10k_dim128_Cosine(b *testing.B) {
	s, q := seedStore(b, b.TempDir(), 10_000, 128, MetricCosine)
	defer s.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = s.Search(q, 10)
	}
}

// BenchmarkSearch_1k_dim128_L2 measures exact Search over 1,000 dim-128 vectors (L2).
func BenchmarkSearch_1k_dim128_L2(b *testing.B) {
	s, q := seedStore(b, b.TempDir(), 1_000, 128, MetricL2)
	defer s.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = s.Search(q, 10)
	}
}

// BenchmarkSearch_1k_dim128_Dot measures exact Search over 1,000 dim-128 vectors (dot).
func BenchmarkSearch_1k_dim128_Dot(b *testing.B) {
	s, q := seedStore(b, b.TempDir(), 1_000, 128, MetricDot)
	defer s.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = s.Search(q, 10)
	}
}

// BenchmarkReopen_1k measures engine reopen (WAL replay + namespace scan) for 1,000 vectors.
func BenchmarkReopen_1k(b *testing.B) {
	dir := b.TempDir()
	s, _ := seedStore(b, dir, 1_000, 128, MetricCosine)
	s.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s2, err := Open(Options{Dir: dir, Dimension: 128, Metric: MetricCosine})
		if err != nil {
			b.Fatalf("reopen: %v", err)
		}
		s2.Close()
	}
}

// BenchmarkCodec_Encode_dim128 measures binary encoding of a dim-128 vector.
func BenchmarkCodec_Encode_dim128(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	v := randVec(rng, 128)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = encodeRecord(128, v, nil)
	}
}

// BenchmarkCodec_Decode_dim128 measures binary decoding of a pre-encoded dim-128 record.
func BenchmarkCodec_Decode_dim128(b *testing.B) {
	rng := rand.New(rand.NewSource(2))
	v := randVec(rng, 128)
	encoded, _ := encodeRecord(128, v, nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = decodeRecord(encoded, 128)
	}
}

// BenchmarkConcurrentSearch measures Search under concurrent load.
func BenchmarkConcurrentSearch(b *testing.B) {
	s, q := seedStore(b, b.TempDir(), 1_000, 128, MetricCosine)
	defer s.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = s.Search(q, 10)
		}
	})
}

// BenchmarkConcurrentUpsert measures Upsert under concurrent load.
func BenchmarkConcurrentUpsert(b *testing.B) {
	s, _ := seedStore(b, b.TempDir(), 100, 128, MetricCosine)
	defer s.Close()

	rng := rand.New(rand.NewSource(99))
	vecs := make([][]float32, b.N)
	for i := range vecs {
		vecs[i] = randVec(rng, 128)
		vecs[i][0] += 0.1
	}

	var counter int64
	var mu sync.Mutex

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			mu.Lock()
			i := counter
			counter++
			mu.Unlock()
			v := vecs[int(i)%len(vecs)]
			_ = s.Upsert(fmt.Sprintf("bench-%d", i), v, nil)
		}
	})
}
