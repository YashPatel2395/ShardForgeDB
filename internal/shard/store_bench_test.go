package shard

import (
	"fmt"
	"os"
	"sync/atomic"
	"testing"
)

// benchStore opens a store in a temp dir and returns it with a cleanup func.
func benchStore(b *testing.B, shards, vnodes int) (*Store, func()) {
	b.Helper()
	dir, err := os.MkdirTemp("", "shard-bench-*")
	if err != nil {
		b.Fatalf("TempDir: %v", err)
	}
	s, err := Open(Options{Dir: dir, ShardCount: shards, VirtualNodes: vnodes})
	if err != nil {
		os.RemoveAll(dir)
		b.Fatalf("Open: %v", err)
	}
	return s, func() {
		s.Close()
		os.RemoveAll(dir)
	}
}

// seedKeys inserts n deterministic key-value pairs into s.
func seedKeys(b *testing.B, s *Store, n int) {
	b.Helper()
	for i := 0; i < n; i++ {
		key := fmt.Sprintf("bench-key-%07d", i)
		val := fmt.Sprintf("bench-val-%07d", i)
		if err := s.Put([]byte(key), []byte(val)); err != nil {
			b.Fatalf("seed Put %d: %v", i, err)
		}
	}
}

// BenchmarkRing_Route1M measures the ring routing throughput for 1M keys.
func BenchmarkRing_Route1M(b *testing.B) {
	ring := newRing(4, 128)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("bench-key-%07d", i%1_000_000)
		ring.shardIndex([]byte(key))
	}
}

// BenchmarkPut_10k_4shards measures Put throughput for 10k unique keys.
func BenchmarkPut_10k_4shards(b *testing.B) {
	s, cleanup := benchStore(b, 4, 128)
	defer cleanup()

	const n = 10_000
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("bench-key-%07d", i%n)
		val := fmt.Sprintf("bench-val-%07d", i%n)
		if err := s.Put([]byte(key), []byte(val)); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGet_10k_existing_4shards measures Get throughput for existing keys.
func BenchmarkGet_10k_existing_4shards(b *testing.B) {
	s, cleanup := benchStore(b, 4, 128)
	defer cleanup()
	const n = 10_000
	seedKeys(b, s, n)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("bench-key-%07d", i%n)
		if _, _, err := s.Get([]byte(key)); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGet_10k_missing_4shards measures Get throughput for missing keys.
func BenchmarkGet_10k_missing_4shards(b *testing.B) {
	s, cleanup := benchStore(b, 4, 128)
	defer cleanup()
	const n = 10_000
	seedKeys(b, s, n)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("missing-key-%07d", i%n)
		if _, _, err := s.Get([]byte(key)); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkScan_10k_4shards measures fan-out Scan across 4 shards with 10k keys.
func BenchmarkScan_10k_4shards(b *testing.B) {
	s, cleanup := benchStore(b, 4, 128)
	defer cleanup()
	const n = 10_000
	seedKeys(b, s, n)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.Scan(nil, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkFlush_10k_4shards measures Flush after inserting 10k keys.
func BenchmarkFlush_10k_4shards(b *testing.B) {
	s, cleanup := benchStore(b, 4, 128)
	defer cleanup()
	const n = 10_000

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		seedKeys(b, s, n)
		b.StartTimer()
		if err := s.Flush(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCompact_10k_4shards measures Compact after two flush cycles.
func BenchmarkCompact_10k_4shards(b *testing.B) {
	s, cleanup := benchStore(b, 4, 128)
	defer cleanup()
	const n = 10_000

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		seedKeys(b, s, n)
		s.Flush()
		seedKeys(b, s, n) // overwrite same keys → second SSTable
		s.Flush()
		b.StartTimer()
		if err := s.Compact(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkReopen_10k_4shards measures open-then-close latency with 10k flushed keys.
func BenchmarkReopen_10k_4shards(b *testing.B) {
	// Pre-populate a persistent store then repeatedly reopen it.
	baseDir, err := os.MkdirTemp("", "shard-bench-reopen-*")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(baseDir)

	init, err := Open(Options{Dir: baseDir, ShardCount: 4})
	if err != nil {
		b.Fatal(err)
	}
	const n = 10_000
	seedKeys(b, init, n)
	if err := init.Flush(); err != nil {
		b.Fatal(err)
	}
	init.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s, err := Open(Options{Dir: baseDir})
		if err != nil {
			b.Fatal(err)
		}
		s.Close()
	}
}

// BenchmarkConcurrentPut_4shards measures concurrent Put throughput.
func BenchmarkConcurrentPut_4shards(b *testing.B) {
	s, cleanup := benchStore(b, 4, 128)
	defer cleanup()

	var counter atomic.Int64
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			n := counter.Add(1)
			key := fmt.Sprintf("bench-key-%010d", n)
			s.Put([]byte(key), []byte("v"))
		}
	})
}

// BenchmarkConcurrentGet_4shards measures concurrent Get throughput.
func BenchmarkConcurrentGet_4shards(b *testing.B) {
	s, cleanup := benchStore(b, 4, 128)
	defer cleanup()
	const n = 10_000
	seedKeys(b, s, n)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("bench-key-%07d", i%n)
			i++
			s.Get([]byte(key))
		}
	})
}
