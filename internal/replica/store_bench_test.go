package replica

import (
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// benchStore opens a replica store in a temp directory and returns it with a
// cleanup function.
func benchStore(b *testing.B, count int) (*Store, func()) {
	b.Helper()
	dir, err := os.MkdirTemp("", "replica-bench-*")
	if err != nil {
		b.Fatalf("TempDir: %v", err)
	}
	s, err := Open(Options{Dir: dir, ReplicaCount: count})
	if err != nil {
		os.RemoveAll(dir)
		b.Fatalf("Open: %v", err)
	}
	return s, func() {
		s.Close()
		os.RemoveAll(dir)
	}
}

// seedOps writes n deterministic key-value pairs to s and replicates them all.
func seedOps(b *testing.B, s *Store, n int) {
	b.Helper()
	for i := 0; i < n; i++ {
		if _, err := s.Put(
			[]byte(fmt.Sprintf("bench-key-%07d", i)),
			[]byte(fmt.Sprintf("bench-val-%07d", i)),
		); err != nil {
			b.Fatalf("seed Put %d: %v", i, err)
		}
	}
	if err := s.ReplicateAll(); err != nil {
		b.Fatalf("seed ReplicateAll: %v", err)
	}
}

// BenchmarkPut_10k_LeaderOnly measures Put throughput writing to leader only
// (no replication). Local in-process simulation.
func BenchmarkPut_10k_LeaderOnly(b *testing.B) {
	s, cleanup := benchStore(b, 3)
	defer cleanup()

	const n = 10_000
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("bench-key-%07d", i%n)
		val := fmt.Sprintf("bench-val-%07d", i%n)
		if _, err := s.Put([]byte(key), []byte(val)); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkReplicateAll_10k_2Followers measures ReplicateAll throughput
// propagating 10k committed operations to 2 followers.
func BenchmarkReplicateAll_10k_2Followers(b *testing.B) {
	dir, err := os.MkdirTemp("", "replica-bench-repall-*")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(dir)

	const n = 10_000
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		s, err := Open(Options{Dir: dir, ReplicaCount: 3})
		if err != nil {
			// Re-open after close from previous iteration.
			os.RemoveAll(dir)
			os.MkdirAll(dir, 0o755)
			s, err = Open(Options{Dir: dir, ReplicaCount: 3})
			if err != nil {
				b.Fatalf("Open: %v", err)
			}
		}
		for j := 0; j < n; j++ {
			s.Put(
				[]byte(fmt.Sprintf("bench-key-%07d", j)),
				[]byte(fmt.Sprintf("bench-val-%07d", j)),
			)
		}
		b.StartTimer()
		if err := s.ReplicateAll(); err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		s.Close()
		// Clean up for next iteration.
		os.RemoveAll(dir)
		os.MkdirAll(dir, 0o755)
		b.StartTimer()
	}
}

// BenchmarkGet_Leader_10k_Existing measures Get throughput from the leader for
// pre-seeded keys.
func BenchmarkGet_Leader_10k_Existing(b *testing.B) {
	s, cleanup := benchStore(b, 3)
	defer cleanup()
	const n = 10_000
	seedOps(b, s, n)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("bench-key-%07d", i%n)
		if _, _, err := s.Get([]byte(key), ReadLeader, -1); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGet_Follower_10k_Existing measures Get throughput from a follower
// after all operations have been replicated.
func BenchmarkGet_Follower_10k_Existing(b *testing.B) {
	s, cleanup := benchStore(b, 3)
	defer cleanup()
	const n = 10_000
	seedOps(b, s, n)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("bench-key-%07d", i%n)
		if _, _, err := s.Get([]byte(key), ReadFollower, 1); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkScan_Leader_10k measures fan-out Scan from the leader with 10k keys.
func BenchmarkScan_Leader_10k(b *testing.B) {
	s, cleanup := benchStore(b, 3)
	defer cleanup()
	const n = 10_000
	seedOps(b, s, n)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.Scan(nil, nil, ReadLeader, -1); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkReopen_10k measures open-then-close latency with 10k flushed keys.
func BenchmarkReopen_10k(b *testing.B) {
	baseDir, err := os.MkdirTemp("", "replica-bench-reopen-*")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(baseDir)

	init, err := Open(Options{Dir: baseDir, ReplicaCount: 3})
	if err != nil {
		b.Fatal(err)
	}
	const n = 10_000
	seedOps(b, init, n)
	init.Flush()
	init.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s, err := Open(Options{Dir: baseDir, LeaderID: -1})
		if err != nil {
			b.Fatal(err)
		}
		s.Close()
	}
}

// BenchmarkReplicateOnce_SmallBatch measures one ReplicateOnce call with a
// small pending batch.
func BenchmarkReplicateOnce_SmallBatch(b *testing.B) {
	s, cleanup := benchStore(b, 3)
	defer cleanup()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		// Write a small batch and reset follower state.
		for j := 0; j < 10; j++ {
			s.Put([]byte(fmt.Sprintf("so-%07d", i*10+j)), []byte("v"))
		}
		// Reset followers to lag behind.
		for _, h := range s.replicas {
			if h.info.Role == RoleFollower {
				h.appliedIndex = 0
			}
		}
		b.StartTimer()
		s.ReplicateOnce()
	}
}

// BenchmarkConcurrentPut measures concurrent Put throughput (leader only).
func BenchmarkConcurrentPut(b *testing.B) {
	s, cleanup := benchStore(b, 3)
	defer cleanup()

	var counter atomic.Int64
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			n := counter.Add(1)
			s.Put([]byte(fmt.Sprintf("cp-%010d", n)), []byte("v"))
		}
	})
}

// BenchmarkConcurrentReplicateAllWithReads measures concurrent ReplicateAll
// calls interleaved with leader reads.
func BenchmarkConcurrentReplicateAllWithReads(b *testing.B) {
	s, cleanup := benchStore(b, 3)
	defer cleanup()
	const n = 1_000
	seedOps(b, s, n)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%2 == 0 {
				s.Get([]byte(fmt.Sprintf("bench-key-%07d", i%n)), ReadLeader, -1)
			} else {
				s.ReplicateAll()
			}
			i++
		}
	})
}

// BenchmarkLogAppendReplay measures log append and replay throughput.
func BenchmarkLogAppendReplay(b *testing.B) {
	dir, err := os.MkdirTemp("", "replica-bench-log-*")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(dir)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		path := filepath.Join(dir, fmt.Sprintf("log-%d.dat", i))
		ol, err := openLog(path)
		if err != nil {
			b.Fatal(err)
		}
		for j := 0; j < 1000; j++ {
			ol.append(Operation{
				Type:  OpPut,
				Key:   []byte(fmt.Sprintf("k-%07d", j)),
				Value: []byte(fmt.Sprintf("v-%07d", j)),
			})
		}
		ol.close()
		b.StartTimer()

		ol2, err := openLog(path)
		if err != nil {
			b.Fatal(err)
		}
		ol2.close()
	}
}
