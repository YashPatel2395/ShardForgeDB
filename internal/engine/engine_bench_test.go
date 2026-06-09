package engine

import (
	"fmt"
	"testing"
)

// benchEngine opens a fresh engine in a temp directory and registers cleanup.
func benchEngine(b *testing.B) *Engine {
	b.Helper()
	dir := b.TempDir()
	e, err := Open(Options{Dir: dir})
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	b.Cleanup(func() { e.Close() })
	return e
}

// BenchmarkPut measures Put throughput against an empty MemTable.
func BenchmarkPut(b *testing.B) {
	e := benchEngine(b)
	key := []byte("bench-put-key")
	val := []byte("bench-put-value")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := e.Put(key, val); err != nil {
			b.Fatalf("Put: %v", err)
		}
	}
}

// BenchmarkGet_MemTable_Existing measures Get for a key present in the MemTable.
func BenchmarkGet_MemTable_Existing(b *testing.B) {
	e := benchEngine(b)
	key := []byte("existing-key")
	val := []byte("existing-value")
	if err := e.Put(key, val); err != nil {
		b.Fatalf("Put: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err := e.Get(key)
		if err != nil {
			b.Fatalf("Get: %v", err)
		}
	}
}

// BenchmarkGet_MemTable_Missing measures Get for a key not in the MemTable (no SSTables).
func BenchmarkGet_MemTable_Missing(b *testing.B) {
	e := benchEngine(b)
	// Seed one key so the engine is non-empty but the queried key is missing.
	if err := e.Put([]byte("seed"), []byte("v")); err != nil {
		b.Fatalf("Put: %v", err)
	}
	missing := []byte("definitely-not-there")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err := e.Get(missing)
		if err != nil {
			b.Fatalf("Get: %v", err)
		}
	}
}

// BenchmarkFlush_1k measures a single Flush of 1,000 entries.
func BenchmarkFlush_1k(b *testing.B) {
	const n = 1_000
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		e := benchEngine(b)
		for j := 0; j < n; j++ {
			key := []byte(fmt.Sprintf("key-%08d", j))
			val := []byte(fmt.Sprintf("val-%08d", j))
			if err := e.Put(key, val); err != nil {
				b.Fatalf("Put: %v", err)
			}
		}
		b.StartTimer()
		if err := e.Flush(); err != nil {
			b.Fatalf("Flush: %v", err)
		}
	}
}

// BenchmarkFlush_100k measures a single Flush of 100,000 entries.
func BenchmarkFlush_100k(b *testing.B) {
	const n = 100_000
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		e := benchEngine(b)
		for j := 0; j < n; j++ {
			key := []byte(fmt.Sprintf("key-%09d", j))
			val := []byte(fmt.Sprintf("val-%09d", j))
			if err := e.Put(key, val); err != nil {
				b.Fatalf("Put: %v", err)
			}
		}
		b.StartTimer()
		if err := e.Flush(); err != nil {
			b.Fatalf("Flush: %v", err)
		}
	}
}

// BenchmarkGet_SSTable_Existing measures Get for a key present in a flushed SSTable.
func BenchmarkGet_SSTable_Existing(b *testing.B) {
	e := benchEngine(b)
	const n = 1_000
	for i := 0; i < n; i++ {
		key := []byte(fmt.Sprintf("key-%08d", i))
		val := []byte(fmt.Sprintf("val-%08d", i))
		if err := e.Put(key, val); err != nil {
			b.Fatalf("Put: %v", err)
		}
	}
	if err := e.Flush(); err != nil {
		b.Fatalf("Flush: %v", err)
	}
	// Queried key is in the SSTable, not the (now empty) MemTable.
	target := []byte("key-00000500")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err := e.Get(target)
		if err != nil {
			b.Fatalf("Get: %v", err)
		}
	}
}

// BenchmarkGet_SSTable_Missing_BloomSkip measures Get for a key absent from all
// SSTables; the Bloom filter should skip the SSTable without a disk read.
// The queried key falls within the SSTable's [minKey, maxKey] range so the
// bounds check passes and only the Bloom filter can eliminate it.
func BenchmarkGet_SSTable_Missing_BloomSkip(b *testing.B) {
	e := benchEngine(b)
	// Insert keys "key-00000000" .. "key-00000999". The missing key "key-00000500x"
	// falls inside [key-00000000, key-00000999] so bounds check passes.
	const n = 1_000
	for i := 0; i < n; i++ {
		key := []byte(fmt.Sprintf("key-%08d", i))
		val := []byte(fmt.Sprintf("val-%08d", i))
		if err := e.Put(key, val); err != nil {
			b.Fatalf("Put: %v", err)
		}
	}
	if err := e.Flush(); err != nil {
		b.Fatalf("Flush: %v", err)
	}
	// "key-00000500x" is within [key-00000000, key-00000999] but was never inserted.
	missing := []byte("key-00000500x")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err := e.Get(missing)
		if err != nil {
			b.Fatalf("Get: %v", err)
		}
	}
}

// BenchmarkScan_1k measures Scan over 1,000 live keys spread across MemTable
// and one SSTable.
func BenchmarkScan_1k(b *testing.B) {
	e := benchEngine(b)
	const n = 1_000
	// Write half to SSTable, half in MemTable.
	for i := 0; i < n/2; i++ {
		key := []byte(fmt.Sprintf("a-key-%08d", i))
		val := []byte(fmt.Sprintf("val-%08d", i))
		if err := e.Put(key, val); err != nil {
			b.Fatalf("Put: %v", err)
		}
	}
	if err := e.Flush(); err != nil {
		b.Fatalf("Flush: %v", err)
	}
	for i := n / 2; i < n; i++ {
		key := []byte(fmt.Sprintf("b-key-%08d", i))
		val := []byte(fmt.Sprintf("val-%08d", i))
		if err := e.Put(key, val); err != nil {
			b.Fatalf("Put: %v", err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := e.Scan(nil, nil)
		if err != nil {
			b.Fatalf("Scan: %v", err)
		}
	}
}

// BenchmarkRestart_WALReplay measures engine Open when all data is in the WAL
// (no SSTables). Simulates recovery from a write-heavy session without a flush.
func BenchmarkRestart_WALReplay(b *testing.B) {
	const n = 500
	dir := b.TempDir()

	// Seed data once.
	{
		e, err := Open(Options{Dir: dir})
		if err != nil {
			b.Fatalf("Open seed: %v", err)
		}
		for i := 0; i < n; i++ {
			key := []byte(fmt.Sprintf("wal-key-%08d", i))
			val := []byte(fmt.Sprintf("wal-val-%08d", i))
			if err := e.Put(key, val); err != nil {
				b.Fatalf("Put: %v", err)
			}
		}
		e.Close() // WAL has n records; no flush.
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e, err := Open(Options{Dir: dir})
		if err != nil {
			b.Fatalf("Open replay: %v", err)
		}
		e.Close()
	}
}

// BenchmarkRestart_ManifestLoad measures engine Open when all data is in
// SSTables (no WAL records to replay). Simulates a clean post-flush restart.
func BenchmarkRestart_ManifestLoad(b *testing.B) {
	const n = 500
	dir := b.TempDir()

	// Seed data and flush so everything lands in the manifest.
	{
		e, err := Open(Options{Dir: dir})
		if err != nil {
			b.Fatalf("Open seed: %v", err)
		}
		for i := 0; i < n; i++ {
			key := []byte(fmt.Sprintf("sst-key-%08d", i))
			val := []byte(fmt.Sprintf("sst-val-%08d", i))
			if err := e.Put(key, val); err != nil {
				b.Fatalf("Put: %v", err)
			}
		}
		if err := e.Flush(); err != nil {
			b.Fatalf("Flush: %v", err)
		}
		e.Close()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e, err := Open(Options{Dir: dir})
		if err != nil {
			b.Fatalf("Open manifest: %v", err)
		}
		e.Close()
	}
}
