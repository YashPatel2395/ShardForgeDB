package bench

import (
	"fmt"
	"time"

	"github.com/YashPatel2395/ShardForgeDB/internal/engine"
)

// ── Key / value generation ────────────────────────────────────────────────────

// GenKey returns a fixed-width, zero-padded, lexicographically ordered key for
// the given index. Keys are index-derived only (no PRNG), so lexicographic
// order matches insertion order.
//
// Example: GenKey(42) → "key-0000000042"
func GenKey(index int) []byte {
	return []byte(fmt.Sprintf("key-%010d", index))
}

// GenValue returns a deterministic byte slice of length size derived from seed
// and index via a fast linear congruential generator (LCG).
//
// Two calls with identical (seed, index, size) always return equal slices.
// Changing any argument produces a different result.
func GenValue(seed int64, index int, size int) []byte {
	v := make([]byte, size)
	// 0x9e3779b97f4a7c15 as signed int64 = -7046029254386353131
	h := seed ^ (int64(index) * -7046029254386353131)
	for i := range v {
		h = h*6364136223846793005 + 1442695040888963407
		v[i] = byte(h >> 56)
	}
	return v
}

// ── Engine helper ─────────────────────────────────────────────────────────────

func openEngine(dir string) (*engine.Engine, error) {
	return engine.Open(engine.Options{Dir: dir})
}

func captureStats(e *engine.Engine) engineSnapshot {
	s := e.Stats()
	return engineSnapshot{
		SSTableCount:       s.SSTableCount,
		MemTableEntries:    s.MemTableEntries,
		FlushCount:         s.FlushCount,
		CompactionCount:    s.CompactionCount,
		BloomChecks:        s.BloomChecks,
		BloomNegativeSkips: s.BloomNegativeSkips,
	}
}

// ── write-heavy ───────────────────────────────────────────────────────────────

// runWriteHeavy writes KeyCount entries with periodic manual Flush.
// Every operation is measured; preload time is included in Duration.
func runWriteHeavy(cfg Config, rec *Recorder) error {
	e, err := openEngine(cfg.EngineDir)
	if err != nil {
		return fmt.Errorf("open engine: %w", err)
	}
	defer e.Close()

	for i := 0; i < cfg.Scale.KeyCount; i++ {
		key := GenKey(i)
		val := GenValue(cfg.Seed, i, cfg.Scale.ValueSize)

		t0 := time.Now()
		if err := e.Put(key, val); err != nil {
			return fmt.Errorf("Put %d: %w", i, err)
		}
		rec.recordOp(time.Since(t0))
		rec.addBytesWritten(uint64(len(key) + len(val)))

		if (i+1)%cfg.Scale.FlushInterval == 0 {
			if err := e.Flush(); err != nil {
				return fmt.Errorf("Flush at %d: %w", i, err)
			}
		}
	}
	if err := e.Flush(); err != nil {
		return fmt.Errorf("final Flush: %w", err)
	}
	rec.finalStats = captureStats(e)
	return nil
}

// ── read-heavy ────────────────────────────────────────────────────────────────

// runReadHeavy preloads KeyCount entries, then executes KeyCount Get operations.
// Every 20th read targets a missing key; the rest hit existing keys.
// Only Get operations are recorded in per-op latencies; preload is not.
func runReadHeavy(cfg Config, rec *Recorder) error {
	e, err := openEngine(cfg.EngineDir)
	if err != nil {
		return fmt.Errorf("open engine: %w", err)
	}
	defer e.Close()

	// Preload (not measured per-op).
	for i := 0; i < cfg.Scale.KeyCount; i++ {
		if err := e.Put(GenKey(i), GenValue(cfg.Seed, i, cfg.Scale.ValueSize)); err != nil {
			return fmt.Errorf("preload Put %d: %w", i, err)
		}
		if (i+1)%cfg.Scale.FlushInterval == 0 {
			if err := e.Flush(); err != nil {
				return fmt.Errorf("preload Flush at %d: %w", i, err)
			}
		}
	}
	if err := e.Flush(); err != nil {
		return fmt.Errorf("preload final Flush: %w", err)
	}

	// Read phase (measured).
	// i%20 == 0 → missing key (key index beyond KeyCount).
	// otherwise  → existing key (i % KeyCount).
	for i := 0; i < cfg.Scale.KeyCount; i++ {
		var key []byte
		if i%20 == 0 {
			key = GenKey(cfg.Scale.KeyCount + i)
		} else {
			key = GenKey(i % cfg.Scale.KeyCount)
		}

		t0 := time.Now()
		val, found, err := e.Get(key)
		if err != nil {
			return fmt.Errorf("Get %d: %w", i, err)
		}
		rec.recordOp(time.Since(t0))
		if found {
			rec.addBytesRead(uint64(len(val)))
		}
	}
	rec.finalStats = captureStats(e)
	return nil
}

// ── mixed ─────────────────────────────────────────────────────────────────────

// runMixed executes a deterministic mix of Put (50%), Get (30%), Delete (20%).
// Half the key space is pre-loaded so Gets have something to find.
// All mixed-phase operations are measured.
func runMixed(cfg Config, rec *Recorder) error {
	e, err := openEngine(cfg.EngineDir)
	if err != nil {
		return fmt.Errorf("open engine: %w", err)
	}
	defer e.Close()

	// Preload half the key space (not measured per-op).
	half := cfg.Scale.KeyCount / 2
	for i := 0; i < half; i++ {
		if err := e.Put(GenKey(i), GenValue(cfg.Seed, i, cfg.Scale.ValueSize)); err != nil {
			return fmt.Errorf("preload Put %d: %w", i, err)
		}
		if (i+1)%cfg.Scale.FlushInterval == 0 {
			if err := e.Flush(); err != nil {
				return fmt.Errorf("preload Flush at %d: %w", i, err)
			}
		}
	}
	if err := e.Flush(); err != nil {
		return fmt.Errorf("preload final Flush: %w", err)
	}

	// Mixed phase (measured).
	// kind = i%10: 0-4 → Put (50%), 5-7 → Get (30%), 8-9 → Delete (20%).
	for i := 0; i < cfg.Scale.KeyCount; i++ {
		kind := i % 10
		key := GenKey(i % cfg.Scale.KeyCount)

		t0 := time.Now()
		var opErr error
		switch {
		case kind <= 4: // Put
			val := GenValue(cfg.Seed, i, cfg.Scale.ValueSize)
			opErr = e.Put(key, val)
			if opErr == nil {
				rec.addBytesWritten(uint64(len(key) + len(val)))
			}
		case kind <= 7: // Get
			val, _, err2 := e.Get(key)
			opErr = err2
			if opErr == nil && len(val) > 0 {
				rec.addBytesRead(uint64(len(val)))
			}
		default: // Delete
			opErr = e.Delete(key)
		}
		if opErr != nil {
			return fmt.Errorf("mixed op %d (kind=%d): %w", i, kind, opErr)
		}
		rec.recordOp(time.Since(t0))

		if (i+1)%cfg.Scale.FlushInterval == 0 {
			if err := e.Flush(); err != nil {
				return fmt.Errorf("Flush at %d: %w", i, err)
			}
		}
	}
	if err := e.Flush(); err != nil {
		return fmt.Errorf("final Flush: %w", err)
	}
	rec.finalStats = captureStats(e)
	return nil
}

// ── scan ──────────────────────────────────────────────────────────────────────

// runScan preloads KeyCount entries, then executes ScanCount range scans.
// Each scan covers RangeSize keys. Only Scan operations are measured.
func runScan(cfg Config, rec *Recorder) error {
	e, err := openEngine(cfg.EngineDir)
	if err != nil {
		return fmt.Errorf("open engine: %w", err)
	}
	defer e.Close()

	// Preload (not measured per-op).
	for i := 0; i < cfg.Scale.KeyCount; i++ {
		if err := e.Put(GenKey(i), GenValue(cfg.Seed, i, cfg.Scale.ValueSize)); err != nil {
			return fmt.Errorf("preload Put %d: %w", i, err)
		}
		if (i+1)%cfg.Scale.FlushInterval == 0 {
			if err := e.Flush(); err != nil {
				return fmt.Errorf("preload Flush at %d: %w", i, err)
			}
		}
	}
	if err := e.Flush(); err != nil {
		return fmt.Errorf("preload final Flush: %w", err)
	}

	// Scan phase (measured).
	// Distribute ScanCount start offsets evenly across the key space.
	step := cfg.Scale.KeyCount / cfg.Scale.ScanCount
	if step < 1 {
		step = 1
	}
	for i := 0; i < cfg.Scale.ScanCount; i++ {
		startIdx := (i * step) % cfg.Scale.KeyCount
		endIdx := startIdx + cfg.Scale.RangeSize
		if endIdx > cfg.Scale.KeyCount {
			endIdx = cfg.Scale.KeyCount
		}
		startKey := GenKey(startIdx)
		endKey := GenKey(endIdx)

		t0 := time.Now()
		entries, err := e.Scan(startKey, endKey)
		if err != nil {
			return fmt.Errorf("Scan %d: %w", i, err)
		}
		rec.recordOp(time.Since(t0))
		for _, en := range entries {
			rec.addBytesRead(uint64(len(en.Key) + len(en.Value)))
		}
	}
	rec.finalStats = captureStats(e)
	return nil
}

// ── compaction ────────────────────────────────────────────────────────────────

// runCompaction creates multiple SSTables, measures Gets and Scans before and
// after manual full compaction, and records the compaction duration separately.
//
// Measured operations:
//   - sampleOps Get operations before Compact
//   - scanSamples Scan operations before Compact
//   - sampleOps Get operations after Compact
//   - scanSamples Scan operations after Compact
//
// CompactDuration and Pre/PostCompactSSTableCount are stored in the Recorder.
func runCompaction(cfg Config, rec *Recorder) error {
	e, err := openEngine(cfg.EngineDir)
	if err != nil {
		return fmt.Errorf("open engine: %w", err)
	}
	defer e.Close()

	// Build multiple SSTables by flushing in equal-sized batches.
	const tableCount = 5
	keysPerTable := cfg.Scale.KeyCount / tableCount
	if keysPerTable < 1 {
		keysPerTable = 1
	}
	total := tableCount * keysPerTable
	for i := 0; i < total; i++ {
		if err := e.Put(GenKey(i), GenValue(cfg.Seed, i, cfg.Scale.ValueSize)); err != nil {
			return fmt.Errorf("setup Put %d: %w", i, err)
		}
		if (i+1)%keysPerTable == 0 {
			if err := e.Flush(); err != nil {
				return fmt.Errorf("setup Flush at %d: %w", i, err)
			}
		}
	}

	rec.preCompactSSTableCount = e.Stats().SSTableCount

	sampleOps := min(100, keysPerTable)
	scanSamples := min(20, cfg.Scale.ScanCount)

	// Sample Gets before compaction.
	for i := 0; i < sampleOps; i++ {
		t0 := time.Now()
		val, _, err := e.Get(GenKey(i))
		if err != nil {
			return fmt.Errorf("Get before compact %d: %w", i, err)
		}
		rec.recordOp(time.Since(t0))
		rec.addPreCompactGet()
		if len(val) > 0 {
			rec.addBytesRead(uint64(len(val)))
		}
	}

	// Sample Scans before compaction.
	// Distribute scanSamples start offsets evenly across the loaded key space.
	step := total / scanSamples
	if step < 1 {
		step = 1
	}
	for i := 0; i < scanSamples; i++ {
		startIdx := (i * step) % total
		endIdx := startIdx + cfg.Scale.RangeSize
		if endIdx > total {
			endIdx = total
		}
		t0 := time.Now()
		entries, err := e.Scan(GenKey(startIdx), GenKey(endIdx))
		if err != nil {
			return fmt.Errorf("Scan before compact %d: %w", i, err)
		}
		rec.recordOp(time.Since(t0))
		rec.addPreCompactScan()
		for _, en := range entries {
			rec.addBytesRead(uint64(len(en.Key) + len(en.Value)))
		}
	}

	// Compact.
	t0 := time.Now()
	if err := e.Compact(); err != nil {
		return fmt.Errorf("Compact: %w", err)
	}
	rec.compactDuration = time.Since(t0)
	rec.postCompactSSTableCount = e.Stats().SSTableCount

	// Sample Gets after compaction.
	for i := 0; i < sampleOps; i++ {
		t0 := time.Now()
		val, _, err := e.Get(GenKey(i))
		if err != nil {
			return fmt.Errorf("Get after compact %d: %w", i, err)
		}
		rec.recordOp(time.Since(t0))
		rec.addPostCompactGet()
		if len(val) > 0 {
			rec.addBytesRead(uint64(len(val)))
		}
	}

	// Sample Scans after compaction.
	for i := 0; i < scanSamples; i++ {
		startIdx := (i * step) % total
		endIdx := startIdx + cfg.Scale.RangeSize
		if endIdx > total {
			endIdx = total
		}
		t0 := time.Now()
		entries, err := e.Scan(GenKey(startIdx), GenKey(endIdx))
		if err != nil {
			return fmt.Errorf("Scan after compact %d: %w", i, err)
		}
		rec.recordOp(time.Since(t0))
		rec.addPostCompactScan()
		for _, en := range entries {
			rec.addBytesRead(uint64(len(en.Key) + len(en.Value)))
		}
	}

	rec.finalStats = captureStats(e)
	return nil
}

// ── restart ───────────────────────────────────────────────────────────────────

// runRestart measures the latency of reopening an engine that has both flushed
// SSTables (manifest load path) and unflushed WAL entries (WAL replay path).
//
// Setup (not measured):
//   - Write half the keys and flush them to SSTables.
//   - Write the other half to the WAL only (not flushed).
//   - Close the engine.
//
// Measurement (1 operation):
//   - Reopen the engine, which replays the WAL and loads the manifest.
func runRestart(cfg Config, rec *Recorder) error {
	// Setup phase: write then close without flushing all data.
	{
		e, err := openEngine(cfg.EngineDir)
		if err != nil {
			return fmt.Errorf("open engine (setup): %w", err)
		}

		half := cfg.Scale.KeyCount / 2
		for i := 0; i < half; i++ {
			if err := e.Put(GenKey(i), GenValue(cfg.Seed, i, cfg.Scale.ValueSize)); err != nil {
				e.Close()
				return fmt.Errorf("setup Put %d: %w", i, err)
			}
			if (i+1)%cfg.Scale.FlushInterval == 0 {
				if err := e.Flush(); err != nil {
					e.Close()
					return fmt.Errorf("setup Flush at %d: %w", i, err)
				}
			}
		}
		if err := e.Flush(); err != nil {
			e.Close()
			return fmt.Errorf("setup final Flush: %w", err)
		}
		// WAL-only writes (not flushed).
		for i := half; i < cfg.Scale.KeyCount; i++ {
			if err := e.Put(GenKey(i), GenValue(cfg.Seed, i, cfg.Scale.ValueSize)); err != nil {
				e.Close()
				return fmt.Errorf("WAL Put %d: %w", i, err)
			}
		}
		e.Close()
	}

	// Measure restart.
	t0 := time.Now()
	e, err := openEngine(cfg.EngineDir)
	if err != nil {
		return fmt.Errorf("reopen engine: %w", err)
	}
	rec.recordOp(time.Since(t0))
	rec.finalStats = captureStats(e)
	e.Close()
	return nil
}
