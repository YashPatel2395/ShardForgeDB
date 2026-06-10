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
| write-heavy | 1000 | 128.0ms | 7814 | 1.7µs | 4.3µs | 12.5µs | 10 | 0 |
| read-heavy | 1000 | 127.4ms | 7848 | 1.6µs | 1.8µs | 2.7µs | 10 | 0 |
| mixed | 1000 | 197.3ms | 5068 | 2.7µs | 5.4µs | 15.8µs | 15 | 0 |
| scan | 100 | 150.1ms | 666 | 76.8µs | 92.2µs | 140.6µs | 10 | 0 |
| compaction | 240 | 96.7ms | 2482 | 1.5µs | 75.4µs | 77.5µs | 1 | 0 |
| restart | 1 | 69.7ms | 14 | 1.3ms | 1.3ms | 1.3ms | 5 | 0 |

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
| Compact duration | 20.7ms |
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

## Phase 10 — Shard Package Benchmarks (Local Single-process)

These benchmarks measure the sharding layer (`internal/shard`) routing key-value operations across multiple local Engine instances. All shards live inside a single OS process — **no networking, no replication**.

**Platform:** Apple M3 · darwin/arm64 · Go 1.26 · `go test -bench=. -benchmem -benchtime=3s`

| Benchmark | ns/op | B/op | allocs/op |
|-----------|------:|-----:|----------:|
| RingRoute1M | 83 | 32 | 1 |
| Put_10k_4shards | 5,672 | 205 | 7 |
| Get_10k_existing_4shards | 170 | 103 | 4 |
| Get_10k_missing_4shards | 120 | 31 | 1 |
| Scan_10k_4shards | 5,849,880 | 10,099,122 | 80,267 |
| Flush_10k_4shards | 225,437,084 | 5,790,347 | 50,629 |
| Compact_10k_4shards | 211,506,715 | 16,886,024 | 217,442 |
| Reopen_10k_4shards | 574,882 | 1,065,460 | 10,793 |
| ConcurrentPut_4shards | 2,438 | 525 | 6 |
| ConcurrentGet_4shards | 133 | 103 | 4 |

---

## Phase 11 — Replica Package Benchmarks (Local In-process Replication Simulation)

These benchmarks measure the replication simulation layer (`internal/replica`). Multiple local Engine instances act as leader and followers inside a single OS process. **No networking, no RPC, no Raft, no consensus.**

**Platform:** Apple M3 · darwin/arm64 · Go 1.26 · `go test -bench=. -benchmem -benchtime=3s`

| Benchmark | ns/op | B/op | allocs/op | Notes |
|-----------|------:|-----:|----------:|-------|
| Put_10k_LeaderOnly | 296,302 | 2,923 | 32 | WAL + MemTable + log append + COMMIT fsync |
| Get_Leader_10k_Existing | 135 | 103 | 4 | MemTable hit |
| Get_Follower_10k_Existing | 138 | 103 | 4 | MemTable hit after catch-up |
| Scan_Leader_10k | 3,451,186 | 8,638,429 | 60,121 | Full scan 10k entries |
| Reopen_10k | 10,240,000 | 7,533,945 | 90,268 | Open all 3 replicas + replay log |
| ReplicateOnce_SmallBatch | 312,313 | 2,481 | 34 | 1 op × 2 followers |
| ConcurrentPut | 294,805 | 2,792 | 31 | Serialised by write lock |
| ConcurrentReplicateAllWithReads | 119 | 51 | 2 | Reads interleaved with ReplicateAll |
| LogAppendReplay | 962,533 | 258,834 | 6,022 | 1000 ops append then replay |

**Note (review fix):** `Put_10k_LeaderOnly` is ~296µs/op after the commit-index fix (was ~147µs). The additional cost is the atomic COMMIT file write (temp+rename) that now happens on every successful leader commit to guarantee recovery correctness. Get and Scan throughput are unchanged.

---

## Known Limitations

- **Manual compaction only.** No background, automatic, leveled, or size-tiered compaction.
- **Manual flush only.** No automatic MemTable flush.
- **Single process.** No distributed deployment, no real networking.
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
