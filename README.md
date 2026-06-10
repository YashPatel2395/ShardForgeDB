# ShardForgeDB

An **explainable** distributed database engine for key-value and vector search workloads, written in Go.

> **Phase 12 in review.** WAL, MemTable, SSTable, Bloom Filter, single-node Engine, manual full compaction, exact vector search, local key-value sharding, local in-process leader/follower replication simulation, and local observability dashboard with chaos/failure simulation are implemented.
> No background compaction, no database-node networking, no RPC, no distributed cluster, no Raft, no consensus. The Phase 12 dashboard is a local HTTP server only — it does not implement networking between database nodes.

---

## Project Overview

ShardForgeDB is a ground-up database engine built to be transparent about how it works at every layer — from the WAL to the cluster coordinator. The goal is a system where every design decision, trade-off, and benchmark result is documented alongside the code.

## Architecture Goal

```
Client
  │
  ▼
Engine (key-value + vector)
  ├── WAL      — durable write-ahead log
  ├── MemTable — in-memory write buffer (skip list)
  ├── SSTables — sorted, immutable on-disk segments
  ├── Bloom    — probabilistic membership filters
  └── Vector   — exact k-nearest-neighbour index (cosine / L2 / dot)

Cluster
  ├── Sharding     — consistent-hash partitioning
  └── Replication  — leader/follower replication
```

Implemented components are tracked in the phase list below. Later distributed-system features remain intentionally scoped as local simulations unless explicitly marked otherwise.

## Current Phase

**Phase 1 — Project Foundation** ✓ locked

- [x] Go module initialised
- [x] CLI skeleton (`shardforge --help`, `shardforge version`)
- [x] Config loading from YAML with validation
- [x] Structured logging (JSON / text) via `log/slog`
- [x] Makefile (`build`, `test`, `fmt`, `vet`, `lint`)
- [x] Placeholder packages for all future components
- [x] Design and proof documentation
- [x] GitHub Actions CI

**Phase 2 — WAL** ✓ locked

- [x] `internal/wal` — append-only, CRC-checksummed write-ahead log
- [x] `Open`, `Append`, `Replay`, `Close` API
- [x] Little-endian binary record format with sequence numbers
- [x] Corruption detection and partial-tail tolerance
- [x] Concurrent-safe appends
- [x] 24 tests, 4 benchmarks

**Phase 3 — MemTable** ✓ locked

- [x] `internal/memtable` — ordered, concurrent in-memory write buffer
- [x] `Put`, `Delete`, `Get`, `Scan`, `Len`, `ApproxBytes`, `ShouldFlush` API
- [x] Lexicographically sorted key slice for ordered range scans
- [x] Deletion tombstones (consistent with WAL `RecordDelete`)
- [x] Defensive copies on all reads and writes; `sync.RWMutex` concurrency
- [x] Size accounting (`len(key) + len(value) + 64 B overhead` per entry)
- [x] 30 tests, 7 benchmarks

**Phase 4 — SSTable** ✓ locked

- [x] `internal/sstable` — immutable, sorted, on-disk SSTable file format
- [x] `Create`, `Open`, `Get`, `Scan`, `Len`, `Metadata`, `Close` API
- [x] Binary file format: header, data records, index block, footer
- [x] CRC-32 checksums on every data record and on the footer
- [x] Dense in-memory index for O(log n) Get (binary search + single disk seek)
- [x] Atomic creation via temp-file + rename; partial-write safe
- [x] Deletion tombstones, sequence numbers, binary key/value support
- [x] Concurrent-safe reads; `sync.RWMutex` around file access
- [x] 46 tests, 7 benchmarks

**Phase 5 — Bloom Filter** ✓ locked

- [x] `internal/bloom` — deterministic, serializable Bloom filter
- [x] `New`, `Add`, `MightContain`, `Metadata`, `MarshalBinary`, `UnmarshalBinary` API
- [x] Standard Bloom formulas: m = ceil(-n·ln(p)/ln(2)²), k = round((m/n)·ln(2))
- [x] Deterministic double hashing with FNV-1a 64-bit (h1) and salted FNV-1a 64-bit (h2)
- [x] Compact bit array (packed `[]uint64`); no false negatives by design
- [x] Self-describing binary serialization with magic, version, CRC-32, and trailing sentinel
- [x] Concurrent-safe Add and MightContain via `sync.RWMutex`
- [x] 35 tests, 9 benchmarks

**Phase 6 — Single-node Engine** ✓ locked

- [x] `internal/engine` — single-node LSM-tree key-value engine
- [x] `Open`, `Put`, `Delete`, `Get`, `Scan`, `Flush`, `Stats`, `Close` API
- [x] WAL + MemTable + SSTable + Bloom Filter wired end-to-end
- [x] Atomic JSON manifest (`MANIFEST.json`) tracking SSTable and Bloom sidecar paths
- [x] WAL replay on restart; monotonic sequence numbers across restarts
- [x] Bloom filter sidecar per SSTable; negative-key skips tracked in `Stats`
- [x] Min/max key bounds check before Bloom check per SSTable on Get
- [x] Full range Scan merging MemTable and all SSTables; tombstone suppression
- [x] Manual Flush: MemTable → SSTable → Bloom sidecar → manifest → WAL rotation
- [x] Crash-safe invariants: orphan files pre-manifest-commit are ignored on restart
- [x] Concurrent-safe via `sync.RWMutex`; idempotent `Close`
- [x] 45 tests, 10 benchmarks
- [x] This is a **single-node** engine only — no compaction, no distribution, no vector search

**Phase 7 — Manual Full Compaction** ✓ locked

- [x] `(*Engine) Compact() error` — manual full compaction of all flushed SSTables
- [x] Merges all SSTables into at most one compacted SSTable + Bloom sidecar
- [x] Tombstones dropped in full compaction (safe: no older level exists below)
- [x] Overwrites resolved by highest sequence number; original seqs preserved
- [x] Atomic manifest swap: old table list → one new entry (or empty if all-deleted)
- [x] SSTable reader opened before manifest commit; failure leaves old state usable
- [x] Old SSTable and Bloom sidecar files removed after manifest commit (best-effort)
- [x] Crash-safe: orphan files pre-commit ignored; orphan old files post-commit ignored
- [x] MemTable and WAL untouched by compaction
- [x] Compaction stats: `CompactionCount`, `LastCompactionInputTables`, `LastCompactionOutputEntries`
- [x] 34 tests, 8 benchmarks
- [x] **Manual full compaction only** — no background, no automatic thresholds, no levels

**Phase 8 — Benchmarking and Workload Evaluation** ✓ locked

- [x] `internal/bench` — deterministic workload benchmark framework
- [x] Six workloads: write-heavy, read-heavy, mixed, scan, compaction, restart
- [x] Per-operation latency collection with P50/P95/P99 percentiles
- [x] Markdown report generation (`docs/BENCHMARKS.md`)
- [x] CLI: `bin/shardforge-bench --scale small|medium --workload NAME --out PATH`
- [x] Makefile targets: `bench`, `bench-engine`, `bench-report`
- [x] 34 tests in `internal/bench/bench_test.go`
- [x] **No new database feature logic** — measurement and documentation only

**Phase 9 — Single-node Exact Vector Search** ✓ locked

- [x] `internal/vector` — persistent exact k-nearest-neighbour vector store
- [x] Engine-backed persistence (reuses single-node LSM engine from Phase 6)
- [x] In-memory exact index rebuilt on `Open` by scanning the vector namespace
- [x] `Upsert`, `Delete`, `Get`, `Search`, `Flush`, `Compact`, `Count`, `Stats` API
- [x] Three distance metrics: **cosine** (default), **L2** (squared), **dot product**
- [x] Exact brute-force search — **not ANN, not HNSW, not IVF**
- [x] Deterministic binary encoding with magic, version, CRC-32, dimension check
- [x] Namespace isolation: multiple stores can coexist in the same engine directory
- [x] Concurrent-safe via `sync.RWMutex`
- [x] Makefile target: `bench-vector`
- [x] 49 tests, 10 benchmarks in `internal/vector`
- [x] **Single-node only** — no distributed vector search, no sharding, no replication

**Phase 10 — Single-process Key-value Sharding** ✓ locked

- [x] `internal/shard` — deterministic consistent-hash key-value sharding over multiple local engines
- [x] Multiple local `Engine` instances as shards — **no networking, no RPC, no cluster**
- [x] Static shard count; configuration stored in atomic `SHARDING.json` manifest
- [x] FNV-1a 64-bit consistent hash ring with configurable virtual nodes (default 128)
- [x] `Open`, `Put`, `Delete`, `Get`, `Scan`, `Flush`, `Compact`, `Stats`, `ShardForKey`, `Close` API
- [x] Single-key operations route to exactly one shard; empty key returns `ErrInvalidKey`
- [x] Fan-out `Scan`: all shards queried, results merged and sorted by key; duplicate keys resolved by highest Seq
- [x] `Flush` and `Compact` applied to every shard; first failure returns wrapped shard error
- [x] Manifest atomicity: written via temp file + rename; validates version, hash, paths, duplicate IDs/names
- [x] Reopen safety: manifest values loaded on reopen; mismatched options return `ErrShardMismatch`
- [x] Concurrent-safe: `sync.RWMutex` guards closed flag; each engine handles its own synchronisation
- [x] Makefile target: `bench-shard`
- [x] 55 tests, 10 benchmarks in `internal/shard`
- [x] **Local single-process sharding only** — no replication, no networking, no distributed cluster, no Raft, no consensus, no shard migration

> No ANN, no HNSW, no IVF, no approximate search. Phase 9 vector search is **exact brute-force only**.
> No background compaction, no size-tiered compaction, no leveled compaction.
> No automatic flush. No networking. No distributed deployment. No Raft. No consensus.

**Phase 11 — Local In-process Leader/Follower Replication Simulation** ✓ locked

- [x] `internal/replica` — local in-process leader/follower replication simulation for key-value operations
- [x] Multiple local `Engine` instances as replicas — **no networking, no RPC, no distributed deployment**
- [x] Configured leader; followers receive operations via deterministic replication log
- [x] Append-only binary replication log with CRC-32 per record; durable restart/recovery
- [x] `Open`, `Put`, `Delete`, `Get`, `Scan`, `ReplicateOnce`, `ReplicateAll`, `Stats`, `Close` API
- [x] Leader-commit semantics: Put/Delete write to leader, append to log, advance commit index
- [x] Followers apply via `ReplicateOnce`/`ReplicateAll`; applied index persisted per replica
- [x] Stale follower reads documented; `ReadLeader`/`ReadFollower`/`ReadAny` modes
- [x] Pause/lag simulation: `SetFollowerPaused`, `SetFollowerLag` for failure testing
- [x] `REPLICATION.json` manifest written atomically with full validation
- [x] Concurrent-safe: `sync.RWMutex` guards closed flag and shared state
- [x] Makefile target: `bench-replica`
- [x] 66 tests, 10 benchmarks in `internal/replica`
- [x] **Local in-process simulation only** — no networking, no RPC, no Raft, no consensus, no automatic leader election, no quorum, no fault-tolerant distributed claims

**Phase 12 — Local Dashboard and Chaos/Failure Simulation** (branch: `phase-12-dashboard-chaos`, in review)

- [x] `internal/dashboard` — local HTTP observability dashboard and chaos scenario runner
- [x] Local HTTP server serving HTML dashboard, JSON `/status`, `/healthz`, `/events` endpoints
- [x] `EngineCollector`, `ShardCollector`, `ReplicaCollector`, `MultiCollector`, `ScenarioCollector`
- [x] Three deterministic local chaos scenarios: follower pause, follower lag, follower catch-up
- [x] `RunFollowerPauseScenario`, `RunFollowerLagScenario`, `RunFollowerCatchupScenario`
- [x] Timeline event recording in all scenarios; events exposed through dashboard
- [x] `cmd/shardforge-dashboard` CLI with `--demo` and `--run-chaos` flags
- [x] HTML rendered via Go standard library `html/template`; no external JS dependencies
- [x] Footer states: "Local dashboard only — no networking, no Raft, no consensus, no distributed cluster."
- [x] Makefile targets: `dashboard`, `bench-dashboard`; build target updated to include `bin/shardforge-dashboard`
- [x] 46 tests, 8 benchmarks in `internal/dashboard`
- [x] **Local only** — no networking between database nodes, no RPC, no Raft, no consensus, no distributed cluster, no shard migration, no resharding, no vector replication, no ANN/HNSW/IVF

## Planned Phases

| Phase | Focus |
|-------|-------|
| 2 | WAL — durable, append-only write log |
| 3 | MemTable — concurrent in-memory write buffer |
| 4 | SSTable — sorted, immutable on-disk segments |
| 5 | Bloom filters — fast negative-key lookups |
| 6 | Engine — key-value read/write/delete |
| 7 | Manual full compaction |
| 8 | Benchmarking and workload evaluation |
| 9 | Vector search — exact k-NN (cosine / L2 / dot) |
| 10 | Sharding — consistent-hash partitioning |
| 11 | Replication — local leader/follower simulation (no Raft, no networking) |
| 12 | Dashboard, chaos / failure simulation |

## Features NOT Yet Implemented

The following are **not** present in the current codebase:

- Background compaction (Compact() is manual only)
- Automatic compaction thresholds
- Leveled or size-tiered compaction
- ANN / HNSW / IVF vector search — Phase 9 is **exact brute-force only**, not approximate
- Real networking or RPC
- Distributed deployment
- Raft or full consensus
- Automatic leader election
- Fault-tolerant quorum replication
- Shard migration or resharding
- Dashboard / monitoring

## How to Build

```bash
make build          # produces bin/shardforge and bin/shardforge-bench
```

Or directly:

```bash
go build -o bin/shardforge ./cmd/shardforge
go build -o bin/shardforge-bench ./cmd/shardforge-bench
```

## How to Run

```bash
./bin/shardforge --help
./bin/shardforge version
```

## How to Run Benchmarks

```bash
# Generate Markdown report (small scale, fast)
make bench-report

# Or directly:
go run ./cmd/shardforge-bench --scale small --out docs/BENCHMARKS.md

# Run a single workload
go run ./cmd/shardforge-bench --workload write-heavy

# Medium scale (stronger local run)
go run ./cmd/shardforge-bench --scale medium --out /tmp/bench-medium.md

# Run existing Go package benchmarks
make bench-engine
```

## How to Run Tests

```bash
make test           # go test -race ./...
```

Or directly:

```bash
go test -race -count=1 ./...
```

## Other Makefile Targets

```bash
make fmt          # format source files
make vet          # static analysis
make lint         # run golangci-lint (skipped if not installed)
make bench        # run all Go benchmarks
make bench-engine   # run engine Go benchmarks
make bench-replica  # run replica Go benchmarks
make bench-vector # run vector Go benchmarks
make bench-shard  # run shard Go benchmarks
make bench-report # generate docs/BENCHMARKS.md (small scale)
make clean        # remove bin/
make help         # list all targets
```

## Requirements

- Go 1.21 or later (uses `log/slog`)

## License

MIT
