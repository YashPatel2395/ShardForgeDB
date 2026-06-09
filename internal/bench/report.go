package bench

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// WriteReportFile writes a Markdown benchmark report to path, creating or
// truncating the file. scaleName is used in the configuration table header.
func WriteReportFile(path, scaleName string, cfg Config, results []Result) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("bench: create report file %s: %w", path, err)
	}
	defer f.Close()
	return WriteReport(f, scaleName, cfg, results)
}

// WriteReport writes a Markdown benchmark report to w.
//
// The output is deterministic for identical (scaleName, cfg, results) inputs —
// no wall-clock timestamps or machine-specific values are written automatically.
// The Environment section contains only placeholder text that the developer
// should fill in after running benchmarks on their hardware.
func WriteReport(w io.Writer, scaleName string, cfg Config, results []Result) error {
	if _, err := fmt.Fprint(w, header(scaleName, cfg)); err != nil {
		return err
	}
	if _, err := fmt.Fprint(w, resultsTable(results)); err != nil {
		return err
	}
	if _, err := fmt.Fprint(w, compactionDetail(results)); err != nil {
		return err
	}
	if _, err := fmt.Fprint(w, interpretation(results)); err != nil {
		return err
	}
	if _, err := fmt.Fprint(w, footer(scaleName)); err != nil {
		return err
	}
	return nil
}

// ── Sections ─────────────────────────────────────────────────────────────────

func header(scaleName string, cfg Config) string {
	sc := cfg.Scale
	var sb strings.Builder
	sb.WriteString("# ShardForgeDB Benchmark Report\n\n")
	sb.WriteString("> **Important:** These results were collected on a single developer machine.\n")
	sb.WriteString("> They are **not** universal performance claims. Results vary by hardware,\n")
	sb.WriteString("> OS scheduler, disk subsystem, and Go version. Do not use these numbers\n")
	sb.WriteString("> to claim superiority over other databases.\n\n")
	sb.WriteString("---\n\n")

	sb.WriteString("## Environment\n\n")
	sb.WriteString("Fill in the fields below after running benchmarks on your hardware.\n\n")
	sb.WriteString("| Field | Value |\n")
	sb.WriteString("|-------|-------|\n")
	sb.WriteString("| Go version | (fill in: `go version`) |\n")
	sb.WriteString("| OS | (fill in: e.g., darwin arm64) |\n")
	sb.WriteString("| CPU | (fill in: e.g., Apple M3) |\n")
	sb.WriteString("| Memory | (fill in: e.g., 16 GiB) |\n")
	sb.WriteString("| Storage | (fill in: e.g., NVMe SSD) |\n")
	sb.WriteString("| Date | (fill in: YYYY-MM-DD) |\n\n")

	sb.WriteString("---\n\n")
	sb.WriteString("## Configuration\n\n")
	sb.WriteString(fmt.Sprintf("| Parameter | Value |\n"))
	sb.WriteString("|-----------|-------|\n")
	sb.WriteString(fmt.Sprintf("| Scale | `%s` |\n", scaleName))
	sb.WriteString(fmt.Sprintf("| Key count | %d |\n", sc.KeyCount))
	sb.WriteString(fmt.Sprintf("| Value size | %d B |\n", sc.ValueSize))
	sb.WriteString(fmt.Sprintf("| Flush interval | every %d ops |\n", sc.FlushInterval))
	sb.WriteString(fmt.Sprintf("| Range size | %d keys |\n", sc.RangeSize))
	sb.WriteString(fmt.Sprintf("| Scan count | %d |\n", sc.ScanCount))
	sb.WriteString(fmt.Sprintf("| Seed | %d |\n", cfg.Seed))
	sb.WriteString("\n---\n\n")

	sb.WriteString("## Commands Used\n\n")
	sb.WriteString("```bash\n")
	sb.WriteString(fmt.Sprintf("go run ./cmd/shardforge-bench --scale %s --out docs/BENCHMARKS.md\n", scaleName))
	sb.WriteString("# or\n")
	sb.WriteString("make bench-report\n")
	sb.WriteString("```\n\n")
	sb.WriteString("---\n\n")

	return sb.String()
}

func resultsTable(results []Result) string {
	var sb strings.Builder
	sb.WriteString("## Results\n\n")
	sb.WriteString("Durations include preload time where applicable (see Interpretation).\n\n")

	// Main results table.
	sb.WriteString("| Workload | Ops | Duration | Ops/sec | P50 | P95 | P99 | SSTables | Bloom Skips |\n")
	sb.WriteString("|----------|----:|---------:|--------:|----:|----:|----:|:--------:|------------:|\n")
	for _, r := range results {
		sb.WriteString(fmt.Sprintf("| %s | %d | %s | %.0f | %s | %s | %s | %d | %d |\n",
			r.Name,
			r.Operations,
			fmtDuration(r.Duration),
			r.OpsPerSec,
			fmtDuration(r.P50Latency),
			fmtDuration(r.P95Latency),
			fmtDuration(r.P99Latency),
			r.FinalSSTableCount,
			r.BloomNegativeSkips,
		))
	}
	sb.WriteString("\n")

	// Bytes table.
	sb.WriteString("| Workload | Bytes Written | Bytes Read | Flush Count | Compaction Count |\n")
	sb.WriteString("|----------|:-------------:|:----------:|:-----------:|:----------------:|\n")
	for _, r := range results {
		sb.WriteString(fmt.Sprintf("| %s | %s | %s | %d | %d |\n",
			r.Name,
			fmtBytes(r.BytesWritten),
			fmtBytes(r.BytesRead),
			r.FlushCount,
			r.CompactionCount,
		))
	}
	sb.WriteString("\n---\n\n")

	return sb.String()
}

func compactionDetail(results []Result) string {
	// Only emit this section if a compaction workload ran.
	for _, r := range results {
		if r.Name == "compaction" && (r.PreCompactSSTableCount > 0 || r.PostCompactSSTableCount > 0) {
			var sb strings.Builder
			sb.WriteString("## Compaction Detail\n\n")
			sb.WriteString("| Metric | Value |\n")
			sb.WriteString("|--------|-------|\n")
			sb.WriteString(fmt.Sprintf("| SSTables before compact | %d |\n", r.PreCompactSSTableCount))
			sb.WriteString(fmt.Sprintf("| SSTables after compact | %d |\n", r.PostCompactSSTableCount))
			sb.WriteString(fmt.Sprintf("| Compact duration | %s |\n", fmtDuration(r.CompactDuration)))
			sb.WriteString(fmt.Sprintf("| Gets measured (before + after) | %d |\n", r.Operations))
			sb.WriteString("\n---\n\n")
			return sb.String()
		}
	}
	return ""
}

func interpretation(results []Result) string {
	var sb strings.Builder
	sb.WriteString("## Interpretation\n\n")

	sb.WriteString("### write-heavy\n\n")
	sb.WriteString("Measures raw Put throughput including periodic manual Flush. " +
		"The ops/sec figure reflects sustained write throughput with SSTable creation overhead. " +
		"P99 latency spikes are expected at flush boundaries.\n\n")

	sb.WriteString("### read-heavy\n\n")
	sb.WriteString("Measures Get throughput after a warm-up preload. " +
		"5% of reads target missing keys with indices beyond KeyCount; those keys fall outside " +
		"the SSTable min/max bounds so the bounds check short-circuits before Bloom is consulted. " +
		"This demonstrates the per-SSTable bounds optimization that avoids Bloom checks entirely " +
		"for clearly out-of-range keys. " +
		"The Duration includes preload time, so ops/sec under-reports the true read rate.\n\n")

	sb.WriteString("### mixed\n\n")
	sb.WriteString("Measures a 50%/30%/20% Put/Get/Delete mix. " +
		"Tombstone creation (Delete) is included. " +
		"Periodic Flush causes P99 spikes similar to the write-heavy workload.\n\n")

	sb.WriteString("### scan\n\n")
	sb.WriteString("Measures range Scan throughput over an ordered SSTable layout. " +
		"Each scan reads RangeSize entries. " +
		"Ops/sec reflects full scan completions per second, not individual key reads.\n\n")

	sb.WriteString("### compaction\n\n")
	sb.WriteString("Compares Get latency before and after manual full compaction. " +
		"After compaction the engine holds a single merged SSTable, reducing the " +
		"number of files and Bloom filter checks per lookup. " +
		"The compact duration is listed separately in the Compaction Detail table.\n\n")

	sb.WriteString("### restart\n\n")
	sb.WriteString("Measures engine reopen latency including WAL replay and SSTable manifest load. " +
		"Operations = 1 (the single reopen call). " +
		"P50/P95/P99 all reflect the same single measurement.\n\n")

	// Emit bloom stats summary if available.
	for _, r := range results {
		if r.BloomChecks > 0 {
			sb.WriteString(fmt.Sprintf("### Bloom filter effectiveness (%s)\n\n", r.Name))
			sb.WriteString(fmt.Sprintf("- Bloom checks: %d\n", r.BloomChecks))
			sb.WriteString(fmt.Sprintf("- Negative skips (definite misses): %d\n", r.BloomNegativeSkips))
			hitRate := 0.0
			if r.BloomChecks > 0 {
				hitRate = float64(r.BloomNegativeSkips) / float64(r.BloomChecks) * 100
			}
			sb.WriteString(fmt.Sprintf("- Skip rate: %.1f%%\n\n", hitRate))
			break
		}
	}

	sb.WriteString("---\n\n")
	return sb.String()
}

func footer(scaleName string) string {
	var sb strings.Builder
	sb.WriteString("## Known Limitations\n\n")
	sb.WriteString("- **Manual compaction only.** No background, automatic, leveled, or size-tiered compaction.\n")
	sb.WriteString("- **Manual flush only.** No automatic MemTable flush.\n")
	sb.WriteString("- **Single node.** No sharding, replication, or distributed mode.\n")
	sb.WriteString("- **No block cache.** Every SSTable read goes to disk.\n")
	sb.WriteString("- **No compression.** On-disk size reflects raw key+value data.\n")
	sb.WriteString("- **No async writes.** WAL appends are synchronous (fsync off by default).\n")
	sb.WriteString("- **Preload included in Duration.** Ops/sec for read-heavy, scan, " +
		"compaction, and restart workloads is lower than the measured-phase throughput alone.\n")
	sb.WriteString("- **OS scheduling jitter.** P99 latency is sensitive to scheduler decisions " +
		"and disk cache state on the measurement machine.\n")
	sb.WriteString("\n---\n\n")

	sb.WriteString("## How to Reproduce\n\n")
	sb.WriteString("Run the benchmark suite on your machine:\n\n")
	sb.WriteString("```bash\n")
	sb.WriteString("# Quick run (small scale, suitable for CI)\n")
	sb.WriteString(fmt.Sprintf("go run ./cmd/shardforge-bench --scale %s --out docs/BENCHMARKS.md\n", scaleName))
	sb.WriteString("\n# Or via Makefile\n")
	sb.WriteString("make bench-report\n")
	sb.WriteString("\n# Stronger local run (medium scale)\n")
	sb.WriteString("go run ./cmd/shardforge-bench --scale medium --out /tmp/bench-medium.md\n")
	sb.WriteString("\n# Run a single workload\n")
	sb.WriteString("go run ./cmd/shardforge-bench --scale small --workload write-heavy\n")
	sb.WriteString("\n# Run existing Go benchmarks\n")
	sb.WriteString("make bench-engine\n")
	sb.WriteString("```\n\n")
	sb.WriteString("Results will vary by hardware. " +
		"Fill in the Environment section with your machine details for reproducibility.\n")
	return sb.String()
}

// ── Formatting helpers ────────────────────────────────────────────────────────

func fmtDuration(d interface{ Nanoseconds() int64 }) string {
	ns := d.Nanoseconds()
	switch {
	case ns < 1_000:
		return fmt.Sprintf("%dns", ns)
	case ns < 1_000_000:
		return fmt.Sprintf("%.1fµs", float64(ns)/1_000)
	case ns < 1_000_000_000:
		return fmt.Sprintf("%.1fms", float64(ns)/1_000_000)
	default:
		return fmt.Sprintf("%.2fs", float64(ns)/1_000_000_000)
	}
}

func fmtBytes(n uint64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KiB", float64(n)/1024)
	case n < 1024*1024*1024:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1024*1024))
	default:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1024*1024*1024))
	}
}
