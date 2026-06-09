package engine

import (
	"fmt"
	"testing"
)

// benchEngineWithSSTables creates an engine with the given number of SSTables,
// each holding keysPerTable unique keys. All keys are distinct across tables.
func benchEngineWithSSTables(b *testing.B, tableCount, keysPerTable int) *Engine {
	b.Helper()
	e := benchEngine(b)
	for t := 0; t < tableCount; t++ {
		for k := 0; k < keysPerTable; k++ {
			key := []byte(fmt.Sprintf("t%03d-key-%06d", t, k))
			val := []byte(fmt.Sprintf("t%03d-val-%06d", t, k))
			if err := e.Put(key, val); err != nil {
				b.Fatalf("Put: %v", err)
			}
		}
		if err := e.Flush(); err != nil {
			b.Fatalf("Flush: %v", err)
		}
	}
	return e
}

// BenchmarkCompact_2SSTable_1kKeys measures compaction of 2 SSTables with
// 500 keys each (1,000 total unique keys).
func BenchmarkCompact_2SSTable_1kKeys(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		e := benchEngineWithSSTables(b, 2, 500)
		b.StartTimer()
		if err := e.Compact(); err != nil {
			b.Fatalf("Compact: %v", err)
		}
	}
}

// BenchmarkCompact_10SSTable_10kKeys measures compaction of 10 SSTables with
// 1,000 keys each (10,000 total unique keys).
func BenchmarkCompact_10SSTable_10kKeys(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		e := benchEngineWithSSTables(b, 10, 1_000)
		b.StartTimer()
		if err := e.Compact(); err != nil {
			b.Fatalf("Compact: %v", err)
		}
	}
}

// BenchmarkCompact_WithOverwrites measures compaction of 3 SSTables where each
// successive table overwrites all keys from the previous ones. Only the newest
// values survive.
func BenchmarkCompact_WithOverwrites(b *testing.B) {
	const keysPerTable = 500
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		e := benchEngine(b)
		for t := 0; t < 3; t++ {
			for k := 0; k < keysPerTable; k++ {
				key := []byte(fmt.Sprintf("key-%06d", k))
				val := []byte(fmt.Sprintf("t%d-val-%06d", t, k))
				if err := e.Put(key, val); err != nil {
					b.Fatalf("Put: %v", err)
				}
			}
			if err := e.Flush(); err != nil {
				b.Fatalf("Flush: %v", err)
			}
		}
		b.StartTimer()
		if err := e.Compact(); err != nil {
			b.Fatalf("Compact: %v", err)
		}
	}
}

// BenchmarkCompact_WithTombstones measures compaction of 2 SSTables where the
// second table deletes all keys from the first. Output should be zero entries.
func BenchmarkCompact_WithTombstones(b *testing.B) {
	const keysPerTable = 500
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		e := benchEngine(b)
		// SSTable 1: live keys.
		for k := 0; k < keysPerTable; k++ {
			if err := e.Put([]byte(fmt.Sprintf("key-%06d", k)), []byte("v")); err != nil {
				b.Fatalf("Put: %v", err)
			}
		}
		if err := e.Flush(); err != nil {
			b.Fatalf("Flush: %v", err)
		}
		// SSTable 2: tombstones for all keys.
		for k := 0; k < keysPerTable; k++ {
			if err := e.Delete([]byte(fmt.Sprintf("key-%06d", k))); err != nil {
				b.Fatalf("Delete: %v", err)
			}
		}
		if err := e.Flush(); err != nil {
			b.Fatalf("Flush: %v", err)
		}
		b.StartTimer()
		if err := e.Compact(); err != nil {
			b.Fatalf("Compact: %v", err)
		}
	}
}

// BenchmarkGet_MissingKey_BeforeCompaction measures Get for a missing key that
// passes bounds and reaches the Bloom filter, before compaction (2 SSTables).
func BenchmarkGet_MissingKey_BeforeCompaction(b *testing.B) {
	e := benchEngineWithSSTables(b, 2, 500)
	// "alpha" < "missing-key" < "zzz" so bounds check passes on both tables.
	missing := []byte("missing-key-zzz")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err := e.Get(missing)
		if err != nil {
			b.Fatalf("Get: %v", err)
		}
	}
}

// BenchmarkGet_MissingKey_AfterCompaction measures Get for the same missing key
// after compaction (1 SSTable). Expects fewer Bloom checks vs before.
func BenchmarkGet_MissingKey_AfterCompaction(b *testing.B) {
	e := benchEngineWithSSTables(b, 2, 500)
	if err := e.Compact(); err != nil {
		b.Fatalf("Compact: %v", err)
	}
	missing := []byte("missing-key-zzz")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err := e.Get(missing)
		if err != nil {
			b.Fatalf("Get: %v", err)
		}
	}
}

// BenchmarkScan_BeforeCompaction measures Scan over all keys before compaction
// (2 SSTables with 500 keys each = 1,000 keys).
func BenchmarkScan_BeforeCompaction(b *testing.B) {
	e := benchEngineWithSSTables(b, 2, 500)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := e.Scan(nil, nil)
		if err != nil {
			b.Fatalf("Scan: %v", err)
		}
	}
}

// BenchmarkScan_AfterCompaction measures Scan over the same key set after
// compaction (1 compacted SSTable with 1,000 keys).
func BenchmarkScan_AfterCompaction(b *testing.B) {
	e := benchEngineWithSSTables(b, 2, 500)
	if err := e.Compact(); err != nil {
		b.Fatalf("Compact: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := e.Scan(nil, nil)
		if err != nil {
			b.Fatalf("Scan: %v", err)
		}
	}
}
