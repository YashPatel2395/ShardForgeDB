package sstable

import (
	"fmt"
	"path/filepath"
	"testing"
)

func BenchmarkCreate_1k(b *testing.B) {
	entries := makeEntries(1_000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		path := filepath.Join(b.TempDir(), fmt.Sprintf("bench-%d.sst", i))
		if _, err := Create(path, entries, Options{}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCreate_100k measures bulk SSTable creation for 100k entries.
// Each iteration writes to a fresh temp file.
func BenchmarkCreate_100k(b *testing.B) {
	entries := makeEntries(100_000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		path := filepath.Join(b.TempDir(), fmt.Sprintf("bench-%d.sst", i))
		if _, err := Create(path, entries, Options{}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkOpen_100k measures the cost of opening a 100k-entry SSTable —
// specifically footer + index load time.
func BenchmarkOpen_100k(b *testing.B) {
	path := filepath.Join(b.TempDir(), "open_bench.sst")
	if _, err := Create(path, makeEntries(100_000), Options{}); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r, err := Open(path, Options{})
		if err != nil {
			b.Fatal(err)
		}
		r.Close()
	}
}

func BenchmarkGet_Existing(b *testing.B) {
	path := filepath.Join(b.TempDir(), "get_bench.sst")
	if _, err := Create(path, makeEntries(10_000), Options{}); err != nil {
		b.Fatal(err)
	}
	r, err := Open(path, Options{})
	if err != nil {
		b.Fatal(err)
	}
	defer r.Close()
	target := []byte("key-00005000")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Get(target)
	}
}

func BenchmarkGet_Missing(b *testing.B) {
	path := filepath.Join(b.TempDir(), "get_missing_bench.sst")
	if _, err := Create(path, makeEntries(10_000), Options{}); err != nil {
		b.Fatal(err)
	}
	r, err := Open(path, Options{})
	if err != nil {
		b.Fatal(err)
	}
	defer r.Close()
	target := []byte("key-99999999")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Get(target)
	}
}

func BenchmarkScan_1k(b *testing.B) {
	path := filepath.Join(b.TempDir(), "scan1k_bench.sst")
	if _, err := Create(path, makeEntries(10_000), Options{}); err != nil {
		b.Fatal(err)
	}
	r, err := Open(path, Options{})
	if err != nil {
		b.Fatal(err)
	}
	defer r.Close()
	start := []byte("key-00001000")
	end := []byte("key-00002000")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Scan(start, end)
	}
}

// BenchmarkScan_100k measures full-table scan throughput for a 100k-entry SSTable.
func BenchmarkScan_100k(b *testing.B) {
	path := filepath.Join(b.TempDir(), "scan100k_bench.sst")
	if _, err := Create(path, makeEntries(100_000), Options{}); err != nil {
		b.Fatal(err)
	}
	r, err := Open(path, Options{})
	if err != nil {
		b.Fatal(err)
	}
	defer r.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Scan(nil, nil)
	}
}
