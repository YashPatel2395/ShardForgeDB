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
| write-heavy | 1000 | 130.5ms | 7662 | 1.7µs | 4.5µs | 18.6µs | 10 | 0 |
| read-heavy | 1000 | 130.8ms | 7648 | 1.1µs | 2.2µs | 3.4µs | 10 | 0 |
| mixed | 1000 | 177.3ms | 5639 | 2.4µs | 7.0µs | 26.5µs | 15 | 0 |
| scan | 100 | 144.1ms | 694 | 113.2µs | 161.0µs | 240.5µs | 10 | 0 |
| compaction | 240 | 98.2ms | 2445 | 2.2µs | 95.5µs | 97.5µs | 1 | 0 |
| restart | 1 | 71.9ms | 14 | 1.3ms | 1.3ms | 1.3ms | 5 | 0 |

| Workload | Bytes Written | Bytes Read | Flush Count | Compaction Count |
|----------|:-------------:|:----------:|:-----------:|:----------------:|
| write-heavy | 138.7 KiB | 0 B | 10 | 0 |
| read-heavy | 0 B | 118.8 KiB | 10 | 0 |
| mixed | 69.3 KiB | 18.8 KiB | 15 | 0 |
| scan | 0 B | 679.5 KiB | 10 | 0 |
| compaction | 0 B | 302.3 KiB | 5 | 1 |
| restart | 0 B | 0 B | 0 | 0 |

---

## Compaction Detail

> The compaction workload measures both point lookups (Get) and range scans (Scan)
> before and after manual full compaction.

| Metric | Value |
|--------|-------|
| SSTables before compact | 5 |
| SSTables after compact | 1 |
| Compact duration | 23.8ms |
| Gets before compact | 100 |
| Gets after compact | 100 |
| Scans before compact | 20 |
| Scans after compact | 20 |
| Total measured ops (Get + Scan, before + after) | 240 |

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

Measures both Get (point lookup) and Scan (range) latency before and after manual full compaction. After compaction the engine holds a single merged SSTable, reducing the number of files and Bloom filter checks per lookup. The compact duration and per-phase op counts are listed in the Compaction Detail table. Total measured ops = Gets before + Scans before + Gets after + Scans after.

### restart

Measures engine reopen latency including WAL replay and SSTable manifest load. Operations = 1 (the single reopen call). P50/P95/P99 all reflect the same single measurement.

### Bloom filter effectiveness (read-heavy)

- Bloom checks: 950
- Negative skips (definite misses): 0
- Skip rate: 0.0%

---

---

## Phase 10 — Shard Package Benchmarks (Local Single-process)

These benchmarks measure the sharding layer (`internal/shard`) routing key-value operations across multiple local Engine instances. All shards live inside a single OS process — **no networking, no replication**.

**Platform:** Apple M3 · darwin/arm64 · Go 1.21 · `go test -bench=. -benchmem -benchtime=3s`

| Benchmark | ns/op | B/op | allocs/op |
|-----------|------:|-----:|----------:|
| RingRoute1M | 78 | 32 | 1 |
| Put_10k_4shards | 1566 | 201 | 7 |
| Get_10k_existing_4shards | 149 | 103 | 4 |
| Get_10k_missing_4shards | 110 | 31 | 1 |
| Scan_10k_4shards | 5,708,292 | 9,651,265 | 100,309 |
| Flush_10k_4shards | 102,990,683 | 5,832,826 | 50,836 |
| Compact_10k_4shards | 126,712,509 | 16,978,758 | 218,841 |
| Reopen_10k_4shards | 578,179 | 1,065,529 | 10,793 |
| ConcurrentPut_4shards | 2,482 | 484 | 6 |
| ConcurrentGet_4shards | 127 | 103 | 4 |

**Notes:**
- Ring lookup is ~78 ns: FNV-1a hash + binary search over 512 ring tokens.
- Get (existing, in-memory) is ~149 ns: ring lookup + RWMutex RLock + engine MemTable check.
- Scan fans out to all 4 shards, merges 10k entries, and sorts — allocation-heavy by design.
- Flush and Compact include actual SSTable write and compaction I/O across all 4 shards.

---

## Known Limitations

- **Manual compaction only.** No background, automatic, leveled, or size-tiered compaction.
- **Manual flush only.** No automatic MemTable flush.
- **Local single-process only.** No networking, no cross-process shards, no replication, no distributed mode.
- **Static shard count.** Cannot be changed after first open; no resharding or rebalancing.
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
