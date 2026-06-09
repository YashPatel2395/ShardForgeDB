# ShardForgeDB Benchmark Report

> **Important:** These results were collected on a single developer machine.
> They are **not** universal performance claims. Results vary by hardware,
> OS scheduler, disk subsystem, and Go version. Do not use these numbers
> to claim superiority over other databases.

---

## Environment

Fill in the fields below after running benchmarks on your hardware.

| Field | Value |
|-------|-------|
| Go version | (fill in: `go version`) |
| OS | (fill in: e.g., darwin arm64) |
| CPU | (fill in: e.g., Apple M3) |
| Memory | (fill in: e.g., 16 GiB) |
| Storage | (fill in: e.g., NVMe SSD) |
| Date | (fill in: YYYY-MM-DD) |

---

## Configuration

| Parameter | Value |
|-----------|-------|
| Scale | `small` |
| Key count | 1000 |
| Value size | 128 B |
| Flush interval | every 100 ops |
| Range size | 50 keys |
| Scan count | 100 |
| Seed | 42 |

---

## Commands Used

```bash
go run ./cmd/shardforge-bench --scale small --out docs/BENCHMARKS.md
# or
make bench-report
```

---

## Results

Durations include preload time where applicable (see Interpretation).

| Workload | Ops | Duration | Ops/sec | P50 | P95 | P99 | SSTables | Bloom Skips |
|----------|----:|---------:|--------:|----:|----:|----:|:--------:|------------:|
| write-heavy | 1000 | 154.8ms | 6458 | 5.0µs | 14.0µs | 62.6µs | 10 | 0 |
| read-heavy | 1000 | 161.0ms | 6213 | 2.5µs | 4.1µs | 27.7µs | 10 | 0 |
| mixed | 1000 | 232.2ms | 4307 | 5.2µs | 15.7µs | 89.6µs | 15 | 0 |
| scan | 100 | 153.3ms | 652 | 114.9µs | 173.5µs | 244.9µs | 10 | 0 |
| compaction | 200 | 118.3ms | 1691 | 2.7µs | 3.6µs | 5.4µs | 1 | 0 |
| restart | 1 | 67.5ms | 15 | 1.0ms | 1.0ms | 1.0ms | 5 | 0 |

| Workload | Bytes Written | Bytes Read | Flush Count | Compaction Count |
|----------|:-------------:|:----------:|:-----------:|:----------------:|
| write-heavy | 138.7 KiB | 0 B | 10 | 0 |
| read-heavy | 0 B | 118.8 KiB | 10 | 0 |
| mixed | 69.3 KiB | 18.8 KiB | 15 | 0 |
| scan | 0 B | 679.5 KiB | 10 | 0 |
| compaction | 0 B | 25.0 KiB | 5 | 1 |
| restart | 0 B | 0 B | 0 | 0 |

---

## Compaction Detail

| Metric | Value |
|--------|-------|
| SSTables before compact | 5 |
| SSTables after compact | 1 |
| Compact duration | 27.5ms |
| Gets measured (before + after) | 200 |

---

## Interpretation

### write-heavy

Measures raw Put throughput including periodic manual Flush. The ops/sec figure reflects sustained write throughput with SSTable creation overhead. P99 latency spikes are expected at flush boundaries.

### read-heavy

Measures Get throughput after a warm-up preload. 5% of reads target missing keys with indices beyond KeyCount; those keys fall outside the SSTable min/max bounds so the bounds check short-circuits before Bloom is consulted. This demonstrates the per-SSTable bounds optimization that avoids Bloom checks entirely for clearly out-of-range keys. The Duration includes preload time, so ops/sec under-reports the true read rate.

### mixed

Measures a 50%/30%/20% Put/Get/Delete mix. Tombstone creation (Delete) is included. Periodic Flush causes P99 spikes similar to the write-heavy workload.

### scan

Measures range Scan throughput over an ordered SSTable layout. Each scan reads RangeSize entries. Ops/sec reflects full scan completions per second, not individual key reads.

### compaction

Compares Get latency before and after manual full compaction. After compaction the engine holds a single merged SSTable, reducing the number of files and Bloom filter checks per lookup. The compact duration is listed separately in the Compaction Detail table.

### restart

Measures engine reopen latency including WAL replay and SSTable manifest load. Operations = 1 (the single reopen call). P50/P95/P99 all reflect the same single measurement.

### Bloom filter effectiveness (read-heavy)

- Bloom checks: 950
- Negative skips (definite misses): 0
- Skip rate: 0.0%

---

## Known Limitations

- **Manual compaction only.** No background, automatic, leveled, or size-tiered compaction.
- **Manual flush only.** No automatic MemTable flush.
- **Single node.** No sharding, replication, or distributed mode.
- **No block cache.** Every SSTable read goes to disk.
- **No compression.** On-disk size reflects raw key+value data.
- **No async writes.** WAL appends are synchronous (fsync off by default).
- **Preload included in Duration.** Ops/sec for read-heavy, scan, compaction, and restart workloads is lower than the measured-phase throughput alone.
- **OS scheduling jitter.** P99 latency is sensitive to scheduler decisions and disk cache state on the measurement machine.

---

## How to Reproduce

Run the benchmark suite on your machine:

```bash
# Quick run (small scale, suitable for CI)
go run ./cmd/shardforge-bench --scale small --out docs/BENCHMARKS.md

# Or via Makefile
make bench-report

# Stronger local run (medium scale)
go run ./cmd/shardforge-bench --scale medium --out /tmp/bench-medium.md

# Run a single workload
go run ./cmd/shardforge-bench --scale small --workload write-heavy

# Run existing Go benchmarks
make bench-engine
```

Results will vary by hardware. Fill in the Environment section with your machine details for reproducibility.
