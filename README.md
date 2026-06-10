# ShardForgeDB — Explainable Go Database Engine with Networked Node Runtime

An **explainable** Go database engine for key-value and vector search workloads, built layer-by-layer with strict documentation, tests, and benchmarks at every phase.

> **Phase 14 in review.** All thirteen prior phases are implemented and locked.
> Phase 14 adds a real networked node runtime: independent `shardforge-node` processes, HTTP/JSON transport, and a Docker Compose 3-node demo.
>
> This is NOT Raft, NOT consensus, NOT quorum replication, NOT distributed sharding.
> Each node is an independent networked key-value store — the foundation for future distributed features.
> No background compaction, no automatic leader election, no shard migration.

---

## Portfolio Pitch

ShardForgeDB is a ground-up Go database engine designed to be fully explainable at every layer — from the write-ahead log to the replication log. Every design decision, trade-off, and benchmark result is documented alongside the code.

Built to demonstrate:

- **LSM-tree key-value core** — WAL, MemTable, SSTable, Bloom filter, manual full compaction
- **Exact vector search** — cosine / L2 / dot product, brute-force, Engine-backed persistence
- **Local key-value sharding** — FNV-1a consistent-hash ring across multiple in-process Engine instances
- **Local replication simulation** — leader/follower operation log, pause/lag/catch-up controls
- **Local HTTP dashboard and chaos scenarios** — observable stats, HTML view, deterministic failure scenarios
- **Real networked node runtime** — independent `shardforge-node` processes, HTTP/JSON API, Docker Compose 3-node demo
- **Strict test/benchmark/docs discipline** — race-safe tests, reproducible benchmarks, honest scope docs

---

## Honesty / Scope

Sharding, replication, and the dashboard are **local in-process simulations** — not distributed systems:

| Feature | What is implemented | What is NOT implemented |
|---------|--------------------|-----------------------|
| Sharding | FNV-1a hash ring over local Engine instances, single process | Networked cluster, shard migration, distributed routing |
| Replication | Leader/follower log, pause/lag simulation, single process | Raft, consensus, automatic leader election, quorum, fault tolerance |
| Dashboard | Local HTTP server (`127.0.0.1:8080`), chaos scenarios via replica API | Real distributed monitoring, networked node discovery |
| Vector search | Exact brute-force k-NN, cosine/L2/dot | ANN, HNSW, IVF, approximate search |
| Compaction | Manual full compaction (`Compact()`) | Background compaction, automatic thresholds, leveled/size-tiered |
| Node runtime | Real independent `shardforge-node` processes, HTTP/JSON API, Docker Compose demo | Distributed sharding across nodes, networked replication, Raft, consensus |

---

## Architecture

```
HTTP Client / shardforge-node CLI
  │ HTTP/JSON (real network transport, Phase 14)
  ▼
Node Server (internal/node)
  │
  ▼
Engine (key-value + vector)
  ├── WAL      — durable, CRC-checksummed write-ahead log
  ├── MemTable — ordered, concurrent in-memory write buffer
  ├── SSTables — sorted, immutable on-disk segments
  ├── Bloom    — deterministic probabilistic membership filters
  └── Vector   — exact k-nearest-neighbour index (cosine / L2 / dot)

Simulation Layers (all single-process, no networking between database nodes)
  ├── Shard    — consistent-hash routing over multiple local Engines
  ├── Replica  — leader/follower operation log with pause/lag/catch-up
  └── Dashboard — local HTTP observability + deterministic chaos scenarios

Docker Compose Demo (Phase 14)
  ├── shardforge-node-1 (port 9101, /data/node-1)
  ├── shardforge-node-2 (port 9102, /data/node-2)
  └── shardforge-node-3 (port 9103, /data/node-3)
  — 3 independent nodes, each with its own Engine directory
  — NOT distributed sharding, NOT Raft, NOT consensus
```

Implemented components are tracked in the phase list below.

---

## Quickstart

```bash
# Build all four binaries (includes shardforge-node)
make build

# Run all tests (race detector on)
make test

# Run a benchmark report (small scale)
make bench-report

# Run the local dashboard demo
./bin/shardforge-dashboard --demo

# Run the local dashboard demo with chaos scenarios
./bin/shardforge-dashboard --demo --run-chaos

# Start a single networked node
./bin/shardforge-node --node-id node-1 --addr 127.0.0.1:9101 --data-dir /tmp/node-1

# Start 3-node Docker Compose demo
make node-demo

# Fast smoke validation
./scripts/smoke.sh

# Full release check
./scripts/release_check.sh
```

---

## Demo Commands

```bash
./bin/shardforge --help
./bin/shardforge version

./bin/shardforge-bench --scale small --out /tmp/shardforge-bench.md

./bin/shardforge-dashboard --demo
./bin/shardforge-dashboard --demo --run-chaos
./bin/shardforge-dashboard --addr 127.0.0.1:9090 --demo

# Node runtime demo (single node)
./bin/shardforge-node --help
./bin/shardforge-node --node-id node-1 --addr 127.0.0.1:9101 --data-dir /tmp/shardforge-node-1

# Node HTTP API (while node is running)
curl http://127.0.0.1:9101/healthz
curl http://127.0.0.1:9101/status
curl -X PUT http://127.0.0.1:9101/kv/user:1 -H "Content-Type: application/json" -d '{"value":"alice"}'
curl http://127.0.0.1:9101/kv/user:1
curl "http://127.0.0.1:9101/scan?start=user:&end=user:~"
curl -X POST http://127.0.0.1:9101/flush
curl -X POST http://127.0.0.1:9101/compact

# 3-node Docker Compose demo
make node-demo
curl http://localhost:9101/healthz
curl http://localhost:9102/healthz
curl http://localhost:9103/healthz
make node-demo-down
```

---

## Phase Implementation Status

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
- [x] **Single-node engine only** — no compaction, no distributed deployment, no vector search

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
- [x] Multiple local `Engine` instances as shards — **no database-node networking, no RPC, no cluster**
- [x] Static shard count; configuration stored in atomic `SHARDING.json` manifest
- [x] FNV-1a 64-bit consistent hash ring with configurable virtual nodes (default 128)
- [x] `Open`, `Put`, `Delete`, `Get`, `Scan`, `Flush`, `Compact`, `Stats`, `ShardForKey`, `Close` API
- [x] Single-key operations route to exactly one shard; empty key returns `ErrInvalidKey`
- [x] Fan-out `Scan`: all shards queried, results merged and sorted by key; duplicate keys resolved by highest Seq
- [x] Manifest atomicity: written via temp file + rename; validates version, hash, paths, duplicate IDs/names
- [x] Reopen safety: manifest values loaded on reopen; mismatched options return `ErrShardMismatch`
- [x] Concurrent-safe: `sync.RWMutex` guards closed flag; each engine handles its own synchronisation
- [x] Makefile target: `bench-shard`
- [x] 55 tests, 10 benchmarks in `internal/shard`
- [x] **Local single-process sharding only** — no replication, no database-node networking, no distributed cluster, no Raft, no consensus, no shard migration

**Phase 11 — Local In-process Leader/Follower Replication Simulation** ✓ locked

- [x] `internal/replica` — local in-process leader/follower replication simulation for key-value operations
- [x] Multiple local `Engine` instances as replicas — **no database-node networking, no RPC, no distributed deployment**
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
- [x] **Local in-process simulation only** — no database-node networking, no RPC, no Raft, no consensus, no automatic leader election, no quorum, no fault-tolerant distributed claims

**Phase 12 — Local Dashboard and Chaos/Failure Simulation** ✓ locked

- [x] `internal/dashboard` — local HTTP observability dashboard and chaos scenario runner
- [x] **Local HTTP server** (`127.0.0.1:8080`) — does NOT implement database-node networking or RPC
- [x] HTML dashboard (`GET /`), JSON status (`GET /status`), healthz (`GET /healthz`), events (`GET /events`)
- [x] GET-only endpoints; unknown paths → 404; non-GET methods → 405 with `Allow: GET` header
- [x] `EngineCollector`, `ShardCollector`, `ReplicaCollector`, `MultiCollector`, `ScenarioCollector`
- [x] Three deterministic local chaos scenarios: follower pause, follower lag, follower catch-up
- [x] `RunFollowerPauseScenario`, `RunFollowerLagScenario`, `RunFollowerCatchupScenario`
- [x] Timeline event recording in all scenarios; events exposed through dashboard
- [x] `cmd/shardforge-dashboard` CLI with `--demo`, `--run-chaos`, `--addr` flags
- [x] HTML rendered via Go standard library `html/template`; no external JS dependencies
- [x] Footer: "Local dashboard only — no networking, no Raft, no consensus, no distributed cluster."
- [x] Makefile targets: `dashboard`, `bench-dashboard`; build produces `bin/shardforge-dashboard`
- [x] 52 tests, 8 benchmarks in `internal/dashboard`
- [x] **Local only** — no database-node networking, no RPC, no Raft, no consensus, no distributed cluster

**Phase 13 — Final Polish + Release Hardening** ✓ locked

- [x] Docs consistency pass across all phases
- [x] README: full rewrite with portfolio pitch, quickstart, scope section, demo commands
- [x] Release scripts: `scripts/smoke.sh`, `scripts/demo.sh`, `scripts/release_check.sh`
- [x] Release checklist: `docs/RELEASE_CHECKLIST.md`
- [x] Project summary: `docs/PROJECT_SUMMARY.md`
- [x] Makefile targets: `smoke`, `demo`, `release-check`
- [x] **No new engine features** — polish, docs, scripts, and release hardening only

**Phase 14 — Real Networked Node Runtime + HTTP Transport Foundation** (branch: `phase-14-network-node-transport`, in review)

- [x] `internal/node` — real networked database node package
- [x] `cmd/shardforge-node` — CLI binary for independent node processes
- [x] `deploy/docker-compose.yml` + `deploy/Dockerfile` — 3-node Docker Compose demo
- [x] HTTP/JSON API: `GET /healthz`, `GET /status`, `PUT/GET/DELETE /kv/{key}`, `GET /scan`, `POST /flush`, `POST /compact`
- [x] `node.Client` — HTTP/JSON network client with timeout and error handling
- [x] `node.Server` — HTTP server wrapping a local Engine; each node has its own `DataDir`
- [x] 25+ tests: handler tests, client tests, multi-server isolation, restart persistence, race tests
- [x] 6 benchmarks: handler Put/Get/Status/Scan, Client Put/Get via httptest
- [x] Makefile targets: `node`, `node-demo`, `node-demo-down`, `bench-node`
- [x] `bin/shardforge-node` added to `make build`
- [x] **NOT Raft, NOT consensus, NOT quorum replication, NOT distributed sharding, NOT automatic leader election**
- [x] **Each node is independent** — no shared state, no cluster coordination, no shard migration

---

## Not Implemented

The following are **not** present in the current codebase and are not claimed:

| Category | Not implemented |
|----------|----------------|
| Compaction | Background compaction, automatic thresholds, leveled compaction, size-tiered compaction |
| Consensus | Raft, Paxos, full consensus, automatic leader election, fault-tolerant quorum |
| Distribution | Distributed sharding across nodes, shard migration, resharding, distributed transactions |
| Replication | Networked replication between nodes, quorum replication, network-based leader election |
| Vector search | ANN, HNSW, IVF, approximate nearest-neighbour |
| Monitoring | Production monitoring, real-time alerting, distributed tracing |
| Dashboard | Networked node discovery, multi-host monitoring, production deployment |
| Node routing | Automatic request routing to correct shard node, cluster-level load balancing |

---

## Build and Run

```bash
# Build all four binaries into bin/
make build

# Individual binaries
go build -o bin/shardforge ./cmd/shardforge
go build -o bin/shardforge-bench ./cmd/shardforge-bench
go build -o bin/shardforge-dashboard ./cmd/shardforge-dashboard
go build -o bin/shardforge-node ./cmd/shardforge-node
```

## Test and Benchmark

```bash
make test                    # go test -race -count=1 ./...
make vet                     # go vet ./...
make bench-report            # generate docs/BENCHMARKS.md (small scale)
make bench-engine            # engine Go benchmarks
make bench-replica           # replica Go benchmarks
make bench-vector            # vector Go benchmarks
make bench-shard             # shard Go benchmarks
make bench-dashboard         # dashboard Go benchmarks
```

## Scripts

```bash
./scripts/smoke.sh           # fast smoke validation
./scripts/demo.sh            # demo sequence
./scripts/release_check.sh   # full release gate
```

## All Makefile Targets

```bash
make all           # fmt + vet + build
make build         # compile bin/shardforge, bin/shardforge-bench, bin/shardforge-dashboard
make test          # go test -race -count=1 ./...
make fmt           # format source files
make vet           # static analysis
make lint          # run golangci-lint (skipped if not installed)
make bench         # run all Go benchmarks across all packages
make bench-engine
make bench-replica
make bench-vector
make bench-shard
make bench-dashboard
make bench-node        # node package Go benchmarks
make bench-report      # generate docs/BENCHMARKS.md (small scale)
make dashboard         # run shardforge-dashboard --demo
make node              # run shardforge-node (node-1, 127.0.0.1:9101)
make node-demo         # docker compose up --build (3-node demo)
make node-demo-down    # docker compose down -v
make smoke             # ./scripts/smoke.sh
make demo              # ./scripts/demo.sh
make release-check     # ./scripts/release_check.sh
make clean             # remove bin/
make help              # list all targets
```

---

## Requirements

- Go 1.21 or later (uses `log/slog`)

## License

MIT
