package memtable

import (
	"fmt"
	"testing"
)

// seedTable returns a MemTable pre-loaded with n sequential key/value pairs.
func seedTable(b *testing.B, n int) *MemTable {
	b.Helper()
	m := New(Options{})
	for i := 0; i < n; i++ {
		key := fmt.Sprintf("key-%08d", i)
		val := fmt.Sprintf("value-%08d", i)
		if err := m.Put([]byte(key), []byte(val), uint64(i+1)); err != nil {
			b.Fatal(err)
		}
	}
	return m
}

func BenchmarkPut_1k(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		m := New(Options{})
		b.StartTimer()
		for j := 0; j < 1_000; j++ {
			key := fmt.Sprintf("key-%08d", j)
			_ = m.Put([]byte(key), []byte("value"), uint64(j+1))
		}
	}
}

// BenchmarkPut_100k exercises bulk-load of 100k keys. O(n²) insertion cost is
// expected with the current slice-shift implementation; this benchmark exists to
// track regressions and guide future skip-list evaluation.
func BenchmarkPut_100k(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		m := New(Options{})
		b.StartTimer()
		for j := 0; j < 100_000; j++ {
			key := fmt.Sprintf("key-%08d", j)
			_ = m.Put([]byte(key), []byte("value"), uint64(j+1))
		}
	}
}

func BenchmarkGet_Existing(b *testing.B) {
	m := seedTable(b, 1_000)
	target := []byte("key-00000500")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = m.Get(target)
	}
}

func BenchmarkGet_Missing(b *testing.B) {
	m := seedTable(b, 1_000)
	target := []byte("key-99999999")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = m.Get(target)
	}
}

func BenchmarkScan_1k(b *testing.B) {
	m := seedTable(b, 1_000)
	start := []byte("key-00000000")
	end := []byte("key-00001000")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.Scan(start, end)
	}
}

// BenchmarkScan_100k measures full-table scan throughput at 100k entries.
func BenchmarkScan_100k(b *testing.B) {
	m := seedTable(b, 100_000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.Scan(nil, nil)
	}
}
