# ShardForgeDB — Explainable Go Database Engine

An **explainable** Go database engine for key-value and vector search workloads, built layer-by-layer toward a real distributed system. Every phase is strictly documented, tested, and benchmarked. Every claim is audited.

> **Phase 28 — Manual Promotion and Controlled Failover (Hardening Pass 4).** Phases 1–28 complete and locked. Squash-merged to main at `b6965e839baf1ddaeeb6e64a69c781775d5e1396` (2026-06-18). No additional phase is currently authorized.
> Phase 28 adds operator-controlled planned failover: `POST /replication/quiesce` write-fences a primary; `POST /replication/promote` promotes a follower. Hardening pass 4 adds: idempotent bgWorker restart (never replaces live worker), deterministic Close-vs-restart race test, `BackgroundSyncStatus` read under mutex, `SyncFromPrimary` replicator capture under mutex, `ApplyReplicationEntries` follower-only enforcement (ErrNotFollower for primary/standalone/promoted-primary), injectable `removeQuiesceIntentFn` seam with truthful `quiesceIntentActive` tracking (`cleanup_pending` vs `active` intent state). 1292 total tests.
> Phase 27 adds automatic background pull replication: configurable goroutine polls primary every 500ms, exponential backoff, bounded jitter, lag tracking (`lag_entries`, `lag_known`), terminal gap detection. No Raft, no automatic failover.
> Phase 26 makes replication state durable: primary binary journal (`replication.journal`) and follower cursor (`replication_state.json`) survive process restarts. Replication gap detection added (HTTP 409).
> Phase 25 adds a reproducible leader+follower HTTP replication demo: explicit pull via `POST /replication/sync`, PUT+DELETE replication proven, idempotent pull proven, follower write-rejection proven.
>
> **1292 race-safe tests. 120+ reproducible benchmarks. All 28 approved phases complete and locked. Phase 29 has not started.**

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
- **Client-side routing gateway** — deterministic consistent-hash routing to independent nodes via `shardforge-gateway`
- **Stateless HTTP gateway proxy** — `shardforge-proxy` wraps the gateway ring in a long-running HTTP/JSON server (10 endpoints, no failover, no retry)
- **Static cluster metadata** — `internal/cluster` provides a typed, validated JSON config format; gateway and proxy CLIs accept `--config <path>` instead of `--nodes`
- **Runtime operation traces from real execution paths** — `ExplainGet/Put/Delete/Scan` and vector `ExplainUpsert/Search/Delete` produce step-by-step traces (WAL_APPEND, MEMTABLE_HIT, BLOOM_CHECK, SSTABLE_HIT, …) with real wall-clock durations; no fabricated steps
- **Networked `/explain/*` trace API and CLI** — HTTP explain endpoints on every node; `shardforge explain-node` calls any live node and prints the real execution trace
- **Reproducible three-node HTTP cluster demo** — scripts start 3 independent nodes + proxy, prove data isolation and deterministic key routing, 25 checks
- **Explicit pull-based HTTP replication** — leader keeps durable binary journal; follower pulls via `POST /replication/sync`; PUT+DELETE replication and idempotent pull proven
- **Durable primary journal and identity-bound follower cursor** — `replication.journal` (per-append fsync, CRC, crash rollback); `replication_state.json` (CRC-verified, versioned, identity-bound); both survive process restarts
- **Automatic background pull replication with lag tracking** — configurable background goroutine polls primary every 500 ms; exponential backoff with bounded jitter; `lag_entries`/`lag_known` always current after any successful sync
- **Operator-controlled planned failover** — `POST /replication/quiesce` write-fences primary (durable intent record); `POST /replication/promote` promotes follower via crash-consistent two-phase commit; NOT automatic failover; NOT Raft
- **Strict test/benchmark/docs discipline** — 1292 race-safe tests, 120+ reproducible benchmarks, per-claim honest scope audit

---

## Honesty / Scope

ShardForgeDB includes both **local simulations** and **real networked components**. The table below distinguishes what is implemented from what remains intentionally out of scope:

| Feature | What is implemented | What is NOT implemented |
|---------|--------------------|-----------------------|
| Sharding | FNV-1a hash ring over local Engine instances, single process | Networked cluster, shard migration, distributed routing |
| Replication | **Local simulation:** leader/follower op-log, pause/lag/catch-up controls, single process. **Networked:** explicit pull-based read replication between real HTTP nodes; durable binary journal (per-append fsync, CRC, crash rollback); identity-bound follower cursor (CRC-verified versioned JSON); automatic background pull (500 ms interval, exponential backoff, lag tracking); operator-controlled quiesce + manual promotion (durable intent record, two-phase commit, cross-validated startup) | Raft, consensus, quorum replication, automatic leader election, automatic failover, distributed fencing, synchronous replication, strong-consistency guarantee, automatic client rerouting, automatic old-primary reintegration, zero-data-loss guarantee |
| Dashboard | Local HTTP server (`127.0.0.1:8080`), chaos scenarios via replica API | Real distributed monitoring, networked node discovery |
| Vector search | Exact brute-force k-NN, cosine/L2/dot | ANN, HNSW, IVF, approximate search |
| Compaction | Manual full compaction (`Compact()`) | Background compaction, automatic thresholds, leveled/size-tiered |
| Node runtime | Real independent `shardforge-node` processes, HTTP/JSON API, Docker Compose demo, replication journal and follower cursor, explain trace endpoints | Distributed sharding across nodes, Raft, consensus, automatic coordination |
| Gateway | Client-side consistent-hash routing (`shardforge-gateway`), deterministic key→node mapping | Server-side routing, cluster metadata service, automatic failover, resharding |
| Proxy | Stateless HTTP proxy (`shardforge-proxy`), routes requests to nodes via gateway ring, 10 endpoints; stateless and replication-unaware; 502 on node failure — no retry, no failover | Fault-tolerant proxy, retry on failure, replication awareness, distributed state |
| Cluster config | Static JSON config (`configs/*.json`), typed/validated, loaded at startup by gateway/proxy via `--config` | Dynamic membership, node discovery, gossip, Raft, leader election, production cluster manager |

---

## Architecture

```
HTTP Client
  │ HTTP/JSON
  ▼
shardforge-proxy (internal/proxy, port 9200)   ← Phase 16 — stateless routing proxy
  │ consistent-hash routing via internal/gateway
  ▼
shardforge-node-{1,2,3} (internal/node, ports 9101–9103)  ← Phase 14
  │ HTTP/JSON (real network transport)
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

Docker Compose Demo (Phase 16)
  ├── shardforge-node-1 (port 9101, /data/node-1)
  ├── shardforge-node-2 (port 9102, /data/node-2)
  ├── shardforge-node-3 (port 9103, /data/node-3)
  └── shardforge-proxy  (port 9200)
  — 3 independent nodes + 1 stateless proxy
  — NOT distributed sharding, NOT Raft, NOT consensus, NOT automatic failover
```

Implemented components are tracked in the phase list below.

---

## Quickstart

```bash
# Build all seven binaries (includes shardforge-node, shardforge-gateway, shardforge-proxy, shardforge-cluster)
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

# Route a key with the gateway (show which node wins)
./bin/shardforge-gateway --nodes http://127.0.0.1:9101,http://127.0.0.1:9102,http://127.0.0.1:9103 route user:1

# Start proxy (routes to nodes already running on 9101-9103)
./bin/shardforge-proxy --nodes http://127.0.0.1:9101,http://127.0.0.1:9102,http://127.0.0.1:9103

# Start proxy using cluster config file (Phase 17)
./bin/shardforge-proxy --config configs/local-3node-with-proxy.json

# Route a key using cluster config (no network call — pure ring computation)
./bin/shardforge-gateway --config configs/local-3node.json route user:1

# Validate a cluster config file
./bin/shardforge-cluster validate configs/local-3node.json

# Proxy quickstart: curl to proxy for Put/Get
curl -X PUT http://127.0.0.1:9200/kv/user:1 -H "Content-Type: application/json" -d '{"value":"alice"}'
curl http://127.0.0.1:9200/kv/user:1

# Start 3-node + proxy Docker Compose demo
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

# Gateway routing demo (requires node-demo running)
./bin/shardforge-gateway --nodes http://127.0.0.1:9101,http://127.0.0.1:9102,http://127.0.0.1:9103 route user:1
./bin/shardforge-gateway --nodes http://127.0.0.1:9101,http://127.0.0.1:9102,http://127.0.0.1:9103 put user:1 alice
./bin/shardforge-gateway --nodes http://127.0.0.1:9101,http://127.0.0.1:9102,http://127.0.0.1:9103 get user:1
./bin/shardforge-gateway --nodes http://127.0.0.1:9101,http://127.0.0.1:9102,http://127.0.0.1:9103 delete user:1
./bin/shardforge-gateway --nodes http://127.0.0.1:9101,http://127.0.0.1:9102,http://127.0.0.1:9103 health
./bin/shardforge-gateway --nodes http://127.0.0.1:9101,http://127.0.0.1:9102,http://127.0.0.1:9103 flush-all
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

**Phase 14 — Real Networked Node Runtime + HTTP Transport Foundation** ✓ locked

- [x] `internal/node` — real networked database node package
- [x] `cmd/shardforge-node` — CLI binary for independent node processes
- [x] `deploy/docker-compose.yml` + `deploy/Dockerfile` — 3-node Docker Compose demo
- [x] HTTP/JSON API: `GET /healthz`, `GET /status`, `PUT/GET/DELETE /kv/{key}`, `GET /scan`, `POST /flush`, `POST /compact`
- [x] `node.Client` — HTTP/JSON network client with timeout and error handling
- [x] `node.Server` — HTTP server wrapping a local Engine; each node has its own `DataDir`
- [x] 36 tests, 6 benchmarks
- [x] Makefile targets: `node`, `node-demo`, `node-demo-down`, `bench-node`
- [x] **NOT Raft, NOT consensus, NOT quorum replication, NOT distributed sharding, NOT automatic leader election**
- [x] **Each node is independent** — no shared state, no cluster coordination, no shard migration

**Phase 15 — Client-Side Routing Gateway** ✓ locked

- [x] `internal/gateway` — deterministic consistent-hash routing gateway library
- [x] `cmd/shardforge-gateway` — CLI for routing-aware Put/Get/Delete/Health/Flush/Compact
- [x] FNV-1a 64-bit consistent-hash ring with configurable virtual nodes and weight support
- [x] `Gateway.Put/Get/Delete` — route by key to responsible node, no retry to other nodes
- [x] `Gateway.ScanNode` — per-node scan (no global distributed scan without replication)
- [x] `Gateway.FlushAll/CompactAll` — admin fanout to all configured nodes
- [x] `Gateway.HealthAll` — health check map across all configured nodes
- [x] 41 tests, 6 benchmarks
- [x] Makefile targets: `bench-gateway`, `gateway-help`, `gateway-demo`
- [x] `bin/shardforge-gateway` added to `make build` (now 5 binaries)
- [x] **Client-side routing only** — nodes do NOT coordinate, replicate, or share state
- [x] **No Raft, no consensus, no quorum, no automatic leader election, no failover, no shard migration**
- [x] **No retry to another node** — explicitly unsafe without replication

**Phase 16 — Stateless Gateway Proxy Server** ✓ locked


- [x] `internal/proxy` — stateless HTTP routing proxy wrapping `internal/gateway.Gateway`
- [x] `cmd/shardforge-proxy` — long-running proxy server CLI binary
- [x] 10 HTTP/JSON endpoints: `/healthz`, `/status`, `/route/{key}`, `/kv/{key}` (PUT/GET/DELETE), `/scan-node/{nodeID}`, `/flush-all`, `/compact-all`, `/nodes/health`
- [x] `proxy.Status` with `Scope` struct — self-documents no-Raft, no-consensus, no-replication, no-failover
- [x] If routed node is unavailable → 502/503 immediately; **no retry to another node**
- [x] 45 tests, 7 benchmarks in `internal/proxy` + `cmd/shardforge-proxy`
- [x] Makefile targets: `bench-proxy`, `proxy`, `proxy-help`, `proxy-route-demo`
- [x] `bin/shardforge-proxy` added to `make build` (now 6 binaries)
- [x] Docker Compose updated: `shardforge-proxy` on port 9200 alongside 3 independent nodes
- [x] **Stateless routing only** — proxy holds no data; can be restarted at any time
- [x] **No Raft, no consensus, no replication, no failover, no retry**

**Phase 17 — Static Cluster Metadata** (merged)

- [x] `internal/cluster` — typed, validated, file-based cluster configuration
- [x] `cmd/shardforge-cluster` — CLI utility: `validate`, `print`, `example-local-3node`
- [x] `configs/local-3node.json` — 3-node local example config
- [x] `configs/local-3node-with-proxy.json` — 3-node local config with proxy enabled
- [x] `configs/docker-3node-with-proxy.json` — Docker Compose config with service DNS names
- [x] `--config` support added to `shardforge-gateway` and `shardforge-proxy` CLIs
- [x] Reject `--config` + `--nodes` together (ambiguity error)
- [x] Docker Compose updated to load proxy config from `configs/docker-3node-with-proxy.json`
- [x] 47 tests, 4 benchmarks in `internal/cluster` + `cmd/shardforge-cluster`
- [x] Makefile targets: `bench-cluster`, `cluster-validate`, `cluster-help`, `cluster-example`, `gateway-config-demo`
- [x] `bin/shardforge-cluster` added to `make build` (now 7 binaries)
- [x] **Static metadata only** — no dynamic membership, no discovery, no gossip
- [x] **No Raft, no consensus, no leader election, no replication, no failover**
- [x] **Config loaded once at startup** — no runtime cluster updates

**Phase 18 — Networked Read Replicas v1** ✓ locked

- [x] `internal/replnet` — new package: `Role`, `Operation`, `Entry`, `Log` (in-memory mutation log), `Replicator` (HTTP pull client)
- [x] `internal/node` — primary/follower roles, `replLog` mutation log, `--replication-role`, `--primary-url` CLI flags
- [x] Follower rejects `PUT`/`DELETE` with 403 ("follower: writes are not accepted; this node is a read replica")
- [x] Primary appends to mutation log on every successful `PUT`/`DELETE`
- [x] 4 new node endpoints: `GET /replication/status`, `GET /replication/log`, `POST /replication/apply`, `POST /replication/sync`
- [x] `internal/proxy` — 2 new admin endpoints: `GET /replication/status` (fan-out), `POST /replication/sync-node/{nodeID}` (forward)
- [x] `internal/gateway` — `ForwardToAll`, `ForwardToNode` methods; `ForwardResult` type
- [x] `internal/cluster` — `Replication{Enabled, Role, Primary}` per-node config, replication validation in `Validate`
- [x] `cmd/shardforge-cluster` — `example-read-replica-3node` command
- [x] `configs/local-read-replica-3node.json`, `configs/docker-read-replica-3node.json`
- [x] `deploy/docker-compose-replica.yml` — primary (9111) + replica-1 (9112) + replica-2 (9113) + proxy (9210)
- [x] 55+ new tests; `bench-replnet` Makefile target
- [x] **Explicit pull-based only** — no automatic background sync, no background goroutine
- [x] **In-memory mutation log** — not persisted; engine WAL provides data durability
- [x] **Explicit pull-based only** — no automatic background sync, no background goroutine
- [x] **In-memory mutation log** — not persisted; engine WAL provides data durability
- [x] **No Raft, no consensus, no quorum, no automatic failover, no strong consistency**

**Phase 19 — Failure Handling and Manual Rebalance Simulation** (branch: `phase-19-failure-handling-manual-rebalance`, in review)

- [x] `internal/ops` — new package: health visibility, failure simulation, rebalance planning
- [x] `CheckClusterHealth(ctx, cfg, timeout)` — calls `GET /healthz` on all configured nodes, sorts by ID, returns latency + error strings. Diagnostic only.
- [x] `RouteKey(cfg, key)` — pure consistent-hash routing using the cluster ring. No network call.
- [x] `RouteKeyWithAvailableNodes(cfg, key, availableIDs)` — routes on a filtered node subset.
- [x] `SimulateFailure(cfg, req)` — shows routing impact of specified down nodes on sample keys. No live node calls.
- [x] `PlanManualRebalance(cfg, removed, added, keys)` — shows key movement when nodes change. No data movement. No file writes.
- [x] `DefaultOpsScope()` — all 8 scope flags true (manual only, simulation only, no automatic failover, etc.)
- [x] 3 new `shardforge-cluster` commands: `health`, `simulate-failure`, `plan-rebalance`
- [x] `configs/local-failure-sim-3node.json` — 3-node config for ops demos
- [x] 40 tests in `internal/ops`, 15 new tests in `cmd/shardforge-cluster`
- [x] 4 benchmarks: `BenchmarkOps_RouteKey`, `BenchmarkOps_SimulateFailure_100Keys`, `BenchmarkOps_PlanManualRebalance_100Keys`, `BenchmarkOps_CheckClusterHealth_HealthyNodes`
- [x] Makefile targets: `bench-ops`, `ops-health-demo`, `ops-simulate-failure-demo`, `ops-rebalance-plan-demo`
- [x] **Simulation and planning only** — no automatic failover, no automatic rebalancing, no data movement
- [x] **No Raft, no consensus, no quorum, no shard migration, no dynamic membership**
- [x] **Manual operator action required** for all real cluster changes

```bash
# Health check (nodes can be down — command exits 0 with unhealthy report)
./bin/shardforge-cluster health configs/local-failure-sim-3node.json

# Simulate node-2 failure impact on sample keys (no live calls)
./bin/shardforge-cluster simulate-failure configs/local-failure-sim-3node.json --down node-2 --key user:1 --key user:2

# Plan manual rebalance after removing node-2 (no data movement)
./bin/shardforge-cluster plan-rebalance configs/local-failure-sim-3node.json --remove node-2 --key user:1 --key user:2
```

**Phase 21 — Truth Lock + Trace Foundation** ✓ locked

- [x] `docs/CLAIMS.md` — three-section claims audit: Safe (proven by code), Unsafe (must never claim), Future (requires future phases)
- [x] `docs/ROADMAP_DISTRIBUTED.md` — Phases 15–27 defining the path to real distributed features
- [x] `internal/trace` — trace types: `Trace`, `TraceStep`, `OperationType`, `Component`, `StepType`, `Status`
- [x] `docs/TRACE_DESIGN.md` — trace philosophy, types, integration plan
- [x] Fixed stale statements in `docs/DESIGN.md`
- [x] Added summary table to `docs/PROOF.md`
- [x] 22 tests in `internal/trace`

**Phase 22 — Runtime Operation Traces** ✓ locked

- [x] `engine.ExplainGet` / `ExplainPut` / `ExplainDelete` / `ExplainScan` — real per-op traces (WAL_APPEND, MEMTABLE_HIT, BLOOM_CHECK, SSTABLE_HIT, etc.)
- [x] `vector.ExplainUpsert` / `ExplainSearch` / `ExplainDelete` — real per-op traces (VECTOR_UPSERT, VECTOR_SEARCH_EXACT, etc.)
- [x] `shardforge explain` CLI — local explain mode (`put`, `get`, `delete`, `scan`, `vector-upsert`, `vector-search`)
- [x] 40 new tests in `internal/engine` + `internal/vector`

**Phase 23 — Networked Node Trace API** ✓ locked

- [x] `POST /explain/put`, `GET /explain/get`, `DELETE /explain/delete`, `GET /explain/scan` — HTTP explain endpoints on every node
- [x] `node.Client.ExplainPut` / `ExplainGet` / `ExplainDelete` / `ExplainScan` — typed client methods
- [x] `shardforge explain-node` CLI — calls any live node over HTTP, prints real execution trace
- [x] 24 new tests in `internal/node` + `cmd/shardforge`
- [x] **929 total tests across 27 packages, race detector on every run**

**Phase 24 — Reproducible Multi-Node Local Cluster Demo** ✓ locked

- [x] `configs/cluster/demo-3node.json` — dedicated 3-node cluster config (node-1/9101, node-2/9102, node-3/9103, proxy/9200)
- [x] `scripts/demo_cluster_up.sh` — start 3 nodes + proxy as local processes, wait for health
- [x] `scripts/demo_cluster_smoke.sh` — 25-check smoke: health, key placement proof, put/get, data isolation proof, explain-node, config validation
- [x] `scripts/demo_cluster_down.sh` — stop all processes, clean data directories
- [x] `docs/DEMO.md` — full demo documentation with scope table, limitations, examples
- [x] `make cluster-demo-{up,smoke,down}` — Makefile targets
- [x] 13 new tests in `internal/cluster/demo_test.go` — config validity, unique IDs/addrs/dirs, scope flags, deterministic routing, known key routes, invalid config rejection
- [x] **942 total tests (929 + 13)**

**Phase 25 — Networked Pull-Based Replication Demo** ✓ locked

- [x] `configs/replication/demo-leader-follower.json` — leader+follower replication config (leader/9301, follower/9302); `no_raft=true`, `no_quorum_replication=true`
- [x] `scripts/repl_demo_up.sh` — start leader (primary) + follower as local HTTP processes, wait for health
- [x] `scripts/repl_demo_smoke.sh` — 16-check smoke: health, PUT to leader, isolation before pull, explicit pull, value on follower, idempotent pull, DELETE replication, follower role enforcement, scope flags
- [x] `scripts/repl_demo_down.sh` — stop all processes, clean data directories
- [x] `make repl-demo-{up,smoke,down}` — Makefile targets
- [x] Richer `POST /replication/sync` response: `fetched`, `applied`, `last_applied_seq`, `source_node`, `follower_node`
- [x] `SyncResult` type in `internal/node` — returned by `SyncFromPrimary` and HTTP handler
- [x] `Client.SyncReplication()` method in `node.Client`
- [x] 20 new tests — 8 in `internal/node` (DELETE replication, idempotency, unavailable primary, cursor advancement, SyncResult counts, distinct data dirs, in-memory cursor) + 12 in `internal/cluster` (config validity, scope flags, leader/follower roles, deterministic routing, invalid config rejection)
- [x] **962 total tests (942 + 20)**

**Phase 26 — Durable Replication State and Restart Recovery** ✓ locked

- [x] `internal/replnet/durable_log.go` — `DurableLog`: binary journal (`replication.journal`), per-Append fsync (write → sync → index update), rollback on write/sync failure (truncate + sync), `ErrPoisonedLog` when rollback itself fails, replay boundary checks (seq 0, first≠1, gap, duplicate, regression, MaxUint64)
- [x] `internal/replnet/state_store.go` — `ReplicationStateStore`: identity-bound versioned JSON cursor (`{version, follower_node_id, primary_url, last_applied_seq, updated_at, checksum}`), CRC32 covers all fields, atomic tmp→fsync→rename, directory fsync best-effort, `ErrInvalidSeqRegression` on backward advance
- [x] `internal/replnet/errors.go` — `ErrReplicationGap`, `ErrCorruptedJournal`, `ErrCorruptedState`, `ErrInvalidSeqRegression`, `ErrUnsupportedStateVersion`, `ErrFollowerIdentityMismatch`, `ErrPrimaryIdentityMismatch`, `ErrPoisonedLog`
- [x] `internal/replnet/types.go` — `ReplicationGapError` (HTTP 409 struct), `DurableLogStats`, `Durable`/`StatePersistent` fields in `ReplicaStatus`
- [x] `internal/replnet/replicator.go` — gap detection: decodes 409 from primary, returns `*ReplicationGapError`
- [x] `internal/node/server.go` — replaces in-memory `Log` with `DurableLog`; adds `ReplicationStateStore` for followers; `atomic.Bool syncInProgress` guard (`ErrSyncInProgress` on concurrent calls); cursor loaded from disk on `Open`; `ApplyReplicationEntries` persists cursor after batch; gap detection in `ReplicationEntries`
- [x] `internal/node/handlers.go` — `GET /replication/log` returns HTTP 409 on gap; `POST /replication/sync` returns HTTP 409 with `gap` struct on gap
- [x] `scripts/repl_restart_demo_{up,smoke,down}.sh` — 18-check smoke: writes before restart, journal file present, follower state file present, restart leader, journal survives, write after restart, restart follower, cursor restored, pull fetches only new entries, idempotent second pull
- [x] `make repl-restart-demo-{up,smoke,down}` — Makefile targets
- [x] `docs/REPLICATION_DURABILITY_DESIGN.md` — 12 design decisions documented before code was written
- [x] 61 new tests — 31 in `internal/replnet/durable_log_test.go` + 12 in `internal/replnet/state_store_test.go` + 18 in `internal/node/replication_phase26_test.go`
- [x] **1023 total tests (962 + 61)**

**Phase 27 — Automatic Background Pull Replication and Lag Tracking** ✓ locked

- [x] `docs/BACKGROUND_REPLICATION_DESIGN.md` — 17-section design doc: worker ownership, polling semantics, backoff/jitter policy, concurrent sync behavior, lag definition, terminal error classification, restart behavior, observability fields
- [x] `internal/replnet/replicator.go` — `PullResult` struct (`Entries []Entry`, `PrimaryLatestSeq uint64`); `PullEntries` returns `PullResult`; `/replication/log` response extended with `primary_latest_seq`
- [x] `internal/node/types.go` — `Duration` type (JSON "500ms" strings), `BackgroundSyncConfig`, `WorkerState` constants, `BackgroundSyncStatus`, extended `SyncResult` with `PrimaryLatestSeq`, `LagEntriesAfterSync`, `LagKnown`, `BackgroundSyncEnabled`
- [x] `internal/node/config.go` — `BackgroundSyncConfig.validate()`: positive durations, max≥initial, role check
- [x] `internal/node/background_sync.go` — `backgroundSyncWorker`: injected seams (`syncFn`, `nowFn`, `afterFn`, `jitterFn`), bounded exponential backoff, `ErrSyncInProgress`→skip, `*ReplicationGapError`→terminal blocked state, lag always known after any successful sync
- [x] `internal/node/server.go` — `bgWorker` field; lifecycle tied to `Start`/`Close`; `Close` stops worker BEFORE closing journal/engine; `SyncFromPrimary` returns `PrimaryLatestSeq` and sets `lagKnown=true` unconditionally on success
- [x] `internal/node/handlers.go` — `/replication/status` includes `background_sync` block; `/replication/log` includes `primary_latest_seq`; `/replication/sync` response includes lag fields
- [x] `cmd/shardforge-node/main.go` — `--bg-sync`, `--bg-sync-interval`, `--bg-sync-request-timeout`, `--bg-sync-initial-backoff`, `--bg-sync-max-backoff`, `--bg-sync-jitter-fraction` flags
- [x] `configs/replication/demo-background-sync.json` — Phase 27 demo config (ports 9501/9502, 500ms interval)
- [x] `scripts/repl_auto_demo_{up,smoke,down}.sh` — 24-check smoke: health, bg enabled/running, auto PUT, lag zero, multiple keys, auto DELETE, primary stop→lag unknown, primary restart→recovery, follower restart survival
- [x] `make repl-auto-demo-{up,smoke,down}` — Makefile targets
- [x] `internal/node/background_sync_test.go` — 48 unit tests: config validation, lifecycle (start/stop idempotency, ErrAlreadyStarted, stop-before-start, start-after-stop, StopWaitsForConcurrentStart via preLaunchHook seam, StopBeforeStart_StartStillSucceeds proving CAS inside lifecycleMu), propagation, backoff doubling/capping, success-resets-backoff, jitter bounds (NaN/Inf guard), `ErrSyncInProgress`-as-skip (resets backoff), lag known/unknown, gap terminal (clears Running/CurrentBackoffMs/NextRetryAt), UTC timestamps, shutdown-not-failure, Duration JSON
- [x] `internal/node/replication_phase27_test.go` — 26 integration tests: auto PUT/DELETE, multiple ordered mutations, empty sync, durable cursor (cursor verified before worker fires), lag tracking, PrimaryLatestSeq absent→LagKnown=false, status API typed client, manual sync coexistence (HTTP 409 ErrSyncInProgress), blocked state+manual sync (real Server), restart scenarios, UTC timestamps, `SyncResult` lag fields
- [x] `internal/node/server_lifecycle_test.go` — 9 lifecycle+client tests: double-StartBackground (standalone/primary/follower), StartBackground after Close, Close without Start then StartBackground, concurrent StartBackground+Close (50-iteration race loop), follower bg worker started exactly once, `Client.SyncReplication` HTTP 409 sync_in_progress→wrapped ErrSyncInProgress, 502 does not match ErrSyncInProgress
- [x] `internal/replnet/replicator_test.go` — 1 Phase-27 test: PrimaryLatestSeq_Reported (verifies *uint64 pointer propagation)
- [x] **1112 total tests (1023 + 89 Phase 27 net-new: 48 background_sync + 26 phase27_integration + 14 server_lifecycle + 1 replnet = 89)**
- [x] **NOT Raft, NOT consensus, NOT quorum, NOT automatic failover, NOT leader election**
- [x] **Background sync is follower-only** — primary is unaffected; follower rejects writes
- [x] **Phase 26 crash window acknowledged** — engine commit → journal append window remains

**Phase 28 — Manual Promotion and Controlled Failover (Hardening Pass 4)** — merged and validation-locked (squash commit `b6965e8`, 2026-06-18)

- [x] `internal/replnet/quiesce_intent.go` — NEW: `QuiesceIntentRecord` (crash-safe fence written before write-gate close); `SaveQuiesceIntent`/`LoadQuiesceIntent`/`RemoveQuiesceIntent`; startup rules: intent-only → `quiesce_failed_fenced`; intent+final matching → `quiesced` + intent cleanup; mismatch → startup error
- [x] `internal/replnet/quiesce_store.go` — `NewQuiesceID() (string, error)`: crypto/rand, returns error on entropy failure; `NewQuiesceRecord` returns `(*QuiesceRecord, error)`; `quiesceIDFn` injectable seam on Server
- [x] `internal/replnet/journal_baseline.go` — `CreateJournalBaseline`: hardened with `journalCompatibilityCheck` (rejects existing journals whose first entry seq ≠ baseSeq+1); `ErrJournalIncompatible` sentinel
- [x] `internal/node/server.go` — `replicationMutationMu sync.RWMutex`: SyncFromPrimary/handleReplicationApply hold RLock; handlePromote holds WLock (replaces 5s drain poll); triple barrier check (before CAS, after CAS, inside RLock)
- [x] `internal/node/server.go` — `ReplicationStatus()` data race fixed: reads `s.runtimeRole` via `s.runtimeState()` instead of directly
- [x] `internal/node/server.go` — `resolveRuntimeRole()`: quiesce intent startup rules; cross-validate promotion record against `replication_state.json` cursor; intent-only pending record seq updated after durable log opens
- [x] `internal/node/handlers.go` — `handleKVPut`/`handleKVDelete`: consistent `writeJSONError` with stable `node_quiesced` code (replaces inline `map[string]any`); `handleQuiesce`: intent written before gate close, removed after final commit; `handleReplicationApply`: barrier check + RLock; pre-commit promotion failure restarts bgWorker
- [x] `internal/node/types.go` — `HTTPStatusError` with `Is()` mapping 9 stable codes to sentinels; `Code string` added to `errorResponse`; `ReplicationStatusResponse` extended with 8 new fields (`PromotionSourceNodeID`, `PromotionSourceBaseURL`, `InheritedLastSeq`, `PromotedAt`, `PromotionDurableCommitted`, `PendingQuiesceID`, `PendingQuiesceSeq`, `QuiesceIntentState`)
- [x] `internal/node/client.go` — `doJSON` returns typed `*HTTPStatusError` on non-2xx (parses `code` field); `SyncReplication` uses same typed error
- [x] `internal/node/phase28_hardening_test.go` — 31 hardening tests (added tests 21–25: quiesce intent normal flow, intent-only startup fence, intent+final cleanup, `HTTPStatusError.Is()`, promoted-primary detail fields); test 14 rewritten for true concurrent-first-commit race
- [x] **1292 total tests — Phase 28 net-new: 98 (phase28_hardening_test.go: 31, phase28_safety_test.go: 28, journal_baseline_test.go: 22, quiesce_store_test.go: 17)**
- [x] **NOT automatic failover** — operator must stop old primary before calling promote
- [x] **No distributed fencing** — quiesce write-fence is local to the primary process

---

## Not Implemented

The following are **not** present in the current codebase and must not be claimed. See `docs/CLAIMS.md` for the full audit and `docs/ROADMAP_DISTRIBUTED.md` for the phases that will implement them.

| Category | Not implemented |
|----------|----------------|
| Compaction | Background compaction, automatic thresholds, leveled compaction, size-tiered compaction |
| Consensus | Raft, Paxos, full consensus, automatic leader election, fault-tolerant quorum |
| Distribution | Distributed sharding across nodes, shard migration, resharding, distributed transactions |
| Replication | Quorum replication, strong consistency guarantee, automatic failover |
| Vector search | ANN, HNSW, IVF, approximate nearest-neighbour |
| Monitoring | Production monitoring, real-time alerting, distributed operation traces |
| Dashboard | Networked node discovery, multi-host monitoring |
| Node routing | Automatic failover, retry on node failure |

---

## Build and Run

```bash
# Build all seven binaries into bin/
make build

# Individual binaries
go build -o bin/shardforge ./cmd/shardforge
go build -o bin/shardforge-bench ./cmd/shardforge-bench
go build -o bin/shardforge-dashboard ./cmd/shardforge-dashboard
go build -o bin/shardforge-node ./cmd/shardforge-node
go build -o bin/shardforge-gateway ./cmd/shardforge-gateway
go build -o bin/shardforge-proxy ./cmd/shardforge-proxy
go build -o bin/shardforge-cluster ./cmd/shardforge-cluster
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
make bench-node              # node package Go benchmarks
make bench-gateway           # gateway package Go benchmarks
make bench-proxy             # proxy package Go benchmarks
make bench-cluster           # cluster package Go benchmarks
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
make build         # compile all 7 binaries into bin/
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
make bench-gateway     # gateway package Go benchmarks
make bench-report      # generate docs/BENCHMARKS.md (small scale)
make dashboard         # run shardforge-dashboard --demo
make node              # run shardforge-node (node-1, 127.0.0.1:9101)
make node-demo         # docker compose up --build (3-node demo)
make node-demo-down    # docker compose down -v
make gateway-help      # print shardforge-gateway --help
make gateway-demo      # show route demo (requires node-demo running)
make proxy             # run shardforge-proxy (requires nodes running)
make proxy-help        # print shardforge-proxy --help
make proxy-route-demo  # show proxy route endpoint demo
make bench-proxy       # proxy package Go benchmarks
make bench-cluster     # cluster package Go benchmarks
make cluster-validate  # run cluster package tests
make cluster-help      # print shardforge-cluster help
make cluster-example   # print example cluster config
make gateway-config-demo  # route using config file (no network call)
make smoke             # ./scripts/smoke.sh
make demo              # ./scripts/demo.sh
make release-check     # ./scripts/release_check.sh
make clean             # remove bin/
make help              # list all targets
```

---

## Distributed v1.0 Program

v0.5.0-portfolio is the completed released baseline for ShardForgeDB — all 28 phases merged, validated, and tagged.

Phase 29 begins a formally gated distributed-systems program. This phase contains architecture and specification only.

**Not yet implemented:**
- Raft consensus
- Automatic failover
- Distributed sharding
- Persistent HNSW approximate nearest-neighbor index
- Distributed vector search

No new distributed implementation claim is unlocked by Phase 29. All distributed capabilities remain in the roadmap (docs/ROADMAP_DISTRIBUTED.md) and are gated by the Phase Governance rules (docs/PHASE_GOVERNANCE.md).

---

## Requirements

- Go 1.21 or later (uses `log/slog`)

## License

MIT
