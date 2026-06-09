// Package bench provides a deterministic benchmark and workload evaluation
// framework for ShardForgeDB. It measures the single-node storage engine under
// realistic workloads and produces repeatable results.
//
// # Workloads
//
// Six workloads are defined:
//
//   - write-heavy: 100 % Put with periodic manual Flush.
//   - read-heavy:  preloads data, then reads existing and missing keys.
//   - mixed:       50% Put / 30% Get / 20% Delete with periodic Flush.
//   - scan:        range scans over the sorted key space.
//   - compaction:  Get/Scan before and after manual full compaction.
//   - restart:     WAL replay and manifest load on engine reopen.
//
// # Determinism
//
// Key generation is index-based (no PRNG); value generation uses a simple LCG
// seeded by the caller-supplied seed and the key index. Operation ratios are
// computed via modular arithmetic on a running counter — no PRNG is used for
// scheduling. Given the same seed and scale, two runs produce identical results
// (excluding OS scheduling jitter).
//
// # Metrics
//
// The [Result] struct captures per-workload metrics: operation count, wall
// duration, ops/sec, P50/P95/P99 latency, bytes written/read, final engine
// stats (SSTable count, flush count, compaction count, Bloom stats).
package bench

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"time"
)

// ── Scales ────────────────────────────────────────────────────────────────────

// Scale defines size parameters for a benchmark run.
type Scale struct {
	// KeyCount is the number of distinct keys in write workloads.
	KeyCount int
	// ValueSize is the byte length of generated values.
	ValueSize int
	// FlushInterval is the number of Puts between manual Flush calls.
	FlushInterval int
	// RangeSize is the number of keys covered by each Scan operation.
	RangeSize int
	// ScanCount is the number of Scan operations in the scan workload.
	ScanCount int
}

// Scales maps scale names to their configurations.
//
// "small" is designed for CI and quick local runs (completes in seconds).
// "medium" is designed for more meaningful local measurement.
var Scales = map[string]Scale{
	"small": {
		KeyCount:      1_000,
		ValueSize:     128,
		FlushInterval: 100,
		RangeSize:     50,
		ScanCount:     100,
	},
	"medium": {
		KeyCount:      50_000,
		ValueSize:     256,
		FlushInterval: 1_000,
		RangeSize:     500,
		ScanCount:     500,
	},
}

// WorkloadNames lists all available workload names in canonical order.
var WorkloadNames = []string{
	"write-heavy",
	"read-heavy",
	"mixed",
	"scan",
	"compaction",
	"restart",
}

// ── Config ────────────────────────────────────────────────────────────────────

// Config holds the configuration for a single workload execution.
type Config struct {
	// Scale holds size parameters for the workload.
	Scale Scale
	// Seed is the deterministic random seed for value generation.
	Seed int64
	// EngineDir is the engine's data directory. Set by the Runner; workloads
	// must not modify it.
	EngineDir string
}

// Validate returns an error if cfg contains invalid field values.
func (cfg Config) Validate() error {
	if cfg.Scale.KeyCount <= 0 {
		return errors.New("bench: Config.Scale.KeyCount must be > 0")
	}
	if cfg.Scale.ValueSize <= 0 {
		return errors.New("bench: Config.Scale.ValueSize must be > 0")
	}
	if cfg.Scale.FlushInterval <= 0 {
		return errors.New("bench: Config.Scale.FlushInterval must be > 0")
	}
	if cfg.Scale.RangeSize <= 0 {
		return errors.New("bench: Config.Scale.RangeSize must be > 0")
	}
	if cfg.Scale.ScanCount <= 0 {
		return errors.New("bench: Config.Scale.ScanCount must be > 0")
	}
	return nil
}

// ── Result ────────────────────────────────────────────────────────────────────

// Result holds the metrics collected from a single workload run.
type Result struct {
	// Name is the workload name (e.g. "write-heavy").
	Name string

	// Operations is the number of measured operations (excludes preload).
	Operations int
	// Duration is the total wall-clock time for the entire workload, including
	// any preload phase.
	Duration  time.Duration
	OpsPerSec float64

	// Latency percentiles across all measured operations.
	P50Latency time.Duration
	P95Latency time.Duration
	P99Latency time.Duration

	BytesWritten uint64
	BytesRead    uint64

	// Engine state at workload completion.
	FinalSSTableCount    int
	FinalMemTableEntries int

	FlushCount         uint64
	CompactionCount    uint64
	BloomChecks        uint64
	BloomNegativeSkips uint64

	// Compaction-workload extras (zero for other workloads).
	PreCompactSSTableCount  int
	PostCompactSSTableCount int
	CompactDuration         time.Duration

	// Compaction-workload per-phase op counts (zero for other workloads).
	PreCompactGetOps   int
	PostCompactGetOps  int
	PreCompactScanOps  int
	PostCompactScanOps int
}

// ── Recorder ──────────────────────────────────────────────────────────────────

// engineSnapshot mirrors the engine.Stats fields used by the bench package.
// Defined here to avoid importing engine from runner.go.
type engineSnapshot struct {
	SSTableCount       int
	MemTableEntries    int
	FlushCount         uint64
	CompactionCount    uint64
	BloomChecks        uint64
	BloomNegativeSkips uint64
}

// Recorder collects per-operation latencies and cumulative byte counts
// during a workload run.
type Recorder struct {
	latencies               []time.Duration
	bytesWritten            uint64
	bytesRead               uint64
	preCompactSSTableCount  int
	postCompactSSTableCount int
	compactDuration         time.Duration
	finalStats              engineSnapshot

	// Per-phase op counts for the compaction workload.
	preCompactGetOps   int
	postCompactGetOps  int
	preCompactScanOps  int
	postCompactScanOps int
}

func (r *Recorder) recordOp(d time.Duration) {
	r.latencies = append(r.latencies, d)
}

func (r *Recorder) addBytesWritten(n uint64) { r.bytesWritten += n }
func (r *Recorder) addBytesRead(n uint64)    { r.bytesRead += n }

func (r *Recorder) addPreCompactGet()   { r.preCompactGetOps++ }
func (r *Recorder) addPostCompactGet()  { r.postCompactGetOps++ }
func (r *Recorder) addPreCompactScan()  { r.preCompactScanOps++ }
func (r *Recorder) addPostCompactScan() { r.postCompactScanOps++ }

// result builds a Result from the recorder data and total workload wall time.
func (r *Recorder) result(name string, total time.Duration) Result {
	ops := len(r.latencies)
	var opsPerSec float64
	if total > 0 {
		opsPerSec = float64(ops) / total.Seconds()
	}
	return Result{
		Name:                    name,
		Operations:              ops,
		Duration:                total,
		OpsPerSec:               opsPerSec,
		P50Latency:              Percentile(r.latencies, 50),
		P95Latency:              Percentile(r.latencies, 95),
		P99Latency:              Percentile(r.latencies, 99),
		BytesWritten:            r.bytesWritten,
		BytesRead:               r.bytesRead,
		FinalSSTableCount:       r.finalStats.SSTableCount,
		FinalMemTableEntries:    r.finalStats.MemTableEntries,
		FlushCount:              r.finalStats.FlushCount,
		CompactionCount:         r.finalStats.CompactionCount,
		BloomChecks:             r.finalStats.BloomChecks,
		BloomNegativeSkips:      r.finalStats.BloomNegativeSkips,
		PreCompactSSTableCount:  r.preCompactSSTableCount,
		PostCompactSSTableCount: r.postCompactSSTableCount,
		CompactDuration:         r.compactDuration,
		PreCompactGetOps:        r.preCompactGetOps,
		PostCompactGetOps:       r.postCompactGetOps,
		PreCompactScanOps:       r.preCompactScanOps,
		PostCompactScanOps:      r.postCompactScanOps,
	}
}

// ── Percentile ────────────────────────────────────────────────────────────────

// Percentile returns the p-th percentile of durations (p in [0,100]).
//
// The input slice is not modified. For large workloads this copies and sorts
// the full slice; callers may want to pre-sort or sample for very high op counts.
func Percentile(durations []time.Duration, p int) time.Duration {
	if len(durations) == 0 {
		return 0
	}
	sorted := make([]time.Duration, len(durations))
	copy(sorted, durations)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	idx := (len(sorted) - 1) * p / 100
	return sorted[idx]
}

// ── Runner ────────────────────────────────────────────────────────────────────

// Runner executes one or more named workloads and returns their results.
//
// Each workload gets its own isolated temporary engine directory which is
// removed after the workload completes (best-effort).
type Runner struct {
	// ScaleName selects the benchmark scale: "small" or "medium". Required.
	ScaleName string
	// WorkloadNames is the ordered list of workloads to run.
	// If empty, all workloads in [WorkloadNames] are run in canonical order.
	WorkloadNames []string
	// Seed is the deterministic seed for value generation. Default (0) is valid.
	Seed int64
	// BaseDir is the parent for per-workload temp directories.
	// If empty, os.TempDir() is used.
	BaseDir string
}

// ErrUnknownScale is returned when an unrecognised scale name is given.
var ErrUnknownScale = errors.New("bench: unknown scale")

// ErrUnknownWorkload is returned when an unrecognised workload name is given.
var ErrUnknownWorkload = errors.New("bench: unknown workload")

// Run executes all configured workloads and returns their results in order.
//
// Returns an error if the scale or any workload name is unrecognised, or if a
// workload returns an error. Results collected before the error are included.
func (r *Runner) Run() ([]Result, error) {
	sc, ok := Scales[r.ScaleName]
	if !ok {
		return nil, fmt.Errorf("%w: %q; valid: small, medium", ErrUnknownScale, r.ScaleName)
	}

	names := r.WorkloadNames
	if len(names) == 0 {
		names = WorkloadNames
	}
	for _, n := range names {
		if !isKnownWorkload(n) {
			return nil, fmt.Errorf("%w: %q", ErrUnknownWorkload, n)
		}
	}

	base := r.BaseDir
	if base == "" {
		base = os.TempDir()
	}

	var results []Result
	for _, name := range names {
		res, err := r.runOne(name, sc, base)
		if err != nil {
			return results, fmt.Errorf("bench: workload %q: %w", name, err)
		}
		results = append(results, res)
	}
	return results, nil
}

func (r *Runner) runOne(name string, sc Scale, base string) (Result, error) {
	dir, err := os.MkdirTemp(base, "shardforge-bench-*")
	if err != nil {
		return Result{}, fmt.Errorf("mkdirtemp: %w", err)
	}
	defer os.RemoveAll(dir)

	cfg := Config{
		Scale:     sc,
		Seed:      r.Seed,
		EngineDir: dir,
	}
	if err := cfg.Validate(); err != nil {
		return Result{}, err
	}

	rec := &Recorder{}
	start := time.Now()
	var runErr error
	switch name {
	case "write-heavy":
		runErr = runWriteHeavy(cfg, rec)
	case "read-heavy":
		runErr = runReadHeavy(cfg, rec)
	case "mixed":
		runErr = runMixed(cfg, rec)
	case "scan":
		runErr = runScan(cfg, rec)
	case "compaction":
		runErr = runCompaction(cfg, rec)
	case "restart":
		runErr = runRestart(cfg, rec)
	default:
		return Result{}, fmt.Errorf("%w: %q", ErrUnknownWorkload, name)
	}
	if runErr != nil {
		return Result{}, runErr
	}
	return rec.result(name, time.Since(start)), nil
}

func isKnownWorkload(name string) bool {
	for _, n := range WorkloadNames {
		if n == name {
			return true
		}
	}
	return false
}
