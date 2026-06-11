# ShardForgeDB — Project Summary

## Overview

ShardForgeDB is a ground-up Go database engine built layer-by-layer to be fully explainable at every level of the stack. Every design decision, data structure, trade-off, and benchmark result is documented alongside the code. The project is structured as a sequence of twenty numbered phases, each building on the previous and producing its own tests, benchmarks, and documentation.

The goal is not to compete with production databases. The goal is to demonstrate deep understanding of database internals — how LSM trees actually work, what makes replication hard, why compaction matters, how to design a real networked node transport — through working code that is honest about what it is and what it is not.

---

## Architecture Summary

```
Cluster Config (configs/*.json)   ← Phase 17 — static metadata
  │ cluster.Load / cluster.GatewayOptions / cluster.ProxyOptions
  ▼
HTTP Client
  │ HTTP/JSON
  ▼
shardforge-proxy (internal/proxy, port 9200/9210)   ← Phase 16/18
  │ client-side consistent-hash routing
  │ uses internal/gateway
  │ GET /replication/status  (fan-out to all nodes)
  │ POST /replication/sync-node/{id}  (forward to follower)
  ▼
shardforge-node-{primary,replica-1,replica-2} (internal/node)  ← Phase 14/18
  │ primary: GET /replication/log  (pull-based replication source)
  │ follower: POST /replication/sync  (explicit pull from primary)
  │           POST /replication/apply  (manual entry application)
  ▼
replnet.Log (internal/replnet)   ← Phase 18 — in-memory mutation log
  primary appends: PUT/DELETE → replnet.Log.Append
  follower pulls:  GET /replication/log?after=<seq>
  │
  ▼
Engine (key-value + vector)
  ├── WAL      — durable CRC-checksummed write-ahead log (binary, little-endian)
  ├── MemTable — ordered concurrent in-memory write buffer (sorted key slice, sync.RWMutex)
  ├── SSTables — sorted immutable on-disk segments (binary format, index block, Bloom sidecar)
  ├── Bloom    — deterministic Bloom filter (FNV-1a double hashing, packed uint64 bits)
  └── Vector   — exact k-NN index (cosine / L2 / dot, engine-backed persistence)

Simulation Layers (single-process, no networking between database nodes)
  ├── Shard     — FNV-1a consistent-hash routing over multiple local Engine instances
  ├── Replica   — binary operation log, leader-commit semantics, follower pause/lag controls
  └── Dashboard — local HTTP observability, HTML + JSON endpoints, chaos scenario runner

Docker Compose Demo (Phase 16/17 — 3-node sharded)
  ├── shardforge-node-1 — independent node, port 9101, /data/node-1
  ├── shardforge-node-2 — independent node, port 9102, /data/node-2
  ├── shardforge-node-3 — independent node, port 9103, /data/node-3
  └── shardforge-proxy  — stateless routing proxy (config from docker-3node-with-proxy.json), port 9200

Docker Compose Demo (Phase 18 — 1-primary + 2-replica read replicas)
  ├── shardforge-primary    — primary node, port 9111, explicit pull-based replication
  ├── shardforge-replica-1  — follower, port 9112, pulls from primary on demand
  ├── shardforge-replica-2  — follower, port 9113, pulls from primary on demand
  └── shardforge-proxy      — proxy with /replication/status and /replication/sync-node, port 9210
```

---

## Phase-by-Phase Feature Map

| Phase | Package | Key features | Tests | Benchmarks |
|-------|---------|-------------|-------|-----------|
| 1 | `cmd/shardforge` | CLI skeleton, config, logging, GitHub Actions CI | — | — |
| 2 | `internal/wal` | Append-only CRC log, replay, sequence numbers | 24 | 4 |
| 3 | `internal/memtable` | Sorted concurrent write buffer, tombstones | 30 | 7 |
| 4 | `internal/sstable` | Immutable on-disk segments, index, CRC footer | 46 | 7 |
| 5 | `internal/bloom` | Deterministic Bloom filter, binary serialization | 35 | 9 |
| 6 | `internal/engine` | Single-node LSM engine, WAL+MemTable+SSTable+Bloom | 45 | 10 |
| 7 | `internal/engine` | Manual full compaction, manifest swap | 34 | 8 |
| 8 | `internal/bench` + `cmd/shardforge-bench` | Six workloads, latency percentiles, Markdown report | 34 | 5 |
| 9 | `internal/vector` | Exact k-NN (cosine/L2/dot), engine-backed persistence | 49 | 10 |
| 10 | `internal/shard` | FNV-1a consistent-hash sharding, SHARDING.json manifest | 55 | 10 |
| 11 | `internal/replica` | Leader/follower op-log, pause/lag/catch-up, COMMIT file | 66 | 10 |
| 12 | `internal/dashboard` + `cmd/shardforge-dashboard` | Local HTTP dashboard, chaos scenarios, collectors | 52 | 8 |
| 13 | scripts, docs | Polish, release hardening, scripts, release checklist | — | — |
| 14 | `internal/node` + `cmd/shardforge-node` | Real networked node runtime, HTTP/JSON API, Docker Compose 3-node demo | 36 | 6 |
| 15 | `internal/gateway` + `cmd/shardforge-gateway` | Client-side consistent-hash routing gateway, FNV-1a ring, weight support | 41 | 6 |
| 16 | `internal/proxy` + `cmd/shardforge-proxy` | Stateless HTTP routing proxy, 10 endpoints, scope flags, Docker Compose integration | 45 | 7 |
| 17 | `internal/cluster` + `cmd/shardforge-cluster` | Static cluster metadata, JSON config format, --config for gateway/proxy CLIs, 3 example configs | 47 | 4 |
| 18 | `internal/replnet` + `internal/node` + `internal/proxy` | Networked read replicas v1: in-memory mutation log, explicit pull-based sync, 4 node replication endpoints, 2 proxy replication admin endpoints, Docker Compose replica demo | 55+ | 5 |
| 19 | `internal/ops` + `cmd/shardforge-cluster` | Health visibility, failure impact simulation, manual rebalance planning — all pure computation, 3 new CLI commands, `OpsScope` in every result | 40 | 4 |
| 20 | docs | Architecture doc, claims audit, roadmap, demo script, final report, resume content, final smoke script, release polish | — | — |

---

## Test and Benchmark Proof Summary

- **All packages** pass `go test -race -count=1 ./...`
- **Race detector** enabled across every test run
- **Benchmarks** use `benchmem` for allocation tracking
- Benchmark results are local development-machine numbers (Apple M3, darwin/arm64, Go 1.26)
- All benchmark commands are in the Makefile and reproducible on any machine

Representative benchmark results (Apple M3):

| Operation | Throughput |
|-----------|-----------|
| Engine Put (WAL + MemTable) | ~1.7µs/op |
| Engine Get (MemTable hit) | ~140ns/op |
| Shard Put (4 shards) | ~1.5µs/op |
| Shard Ring route 1M | ~78ns/op |
| Replica Put (leader + COMMIT write) | ~310µs/op |
| Replica Get (leader) | ~135ns/op |
| Dashboard Snapshot (engine collector) | ~235ns/op |
| Dashboard Render HTML | ~13µs/op |
| Dashboard Chaos scenario | ~14–19ms/op (includes store open/close) |
| Node Handler Put (HTTP, in-process) | ~20µs/op |
| Node Handler Get (HTTP, in-process) | ~1.5µs/op |
| Node Client Put (real HTTP loopback) | ~41µs/op |
| Node Client Get (real HTTP loopback) | ~33µs/op |
| Proxy Route (ring only, no backend call) | ~29µs/op |
| Proxy Put (proxy→node TCP loopback) | ~74µs/op |
| Proxy Get (proxy→node TCP loopback) | ~65µs/op |

---

## Scope Honesty

ShardForgeDB is honest about what it is and is not:

**What it is:**
- A working, tested, documented Go implementation of an LSM-tree key-value engine
- An exact vector search engine (brute-force cosine / L2 / dot product)
- A local simulation of sharding, replication, and a chaos-testing dashboard
- A real networked node runtime with HTTP/JSON API and Docker Compose multi-node demo
- A stateless HTTP routing proxy that routes requests to independent nodes via consistent hashing
- Networked read replicas: primary/follower roles, explicit pull-based sync, in-memory mutation log
- A portfolio project designed to demonstrate deep database internals knowledge

**What it is not:**
- A production database
- A distributed system (nodes are independent, not coordinated)
- A Raft or Paxos implementation
- A fault-tolerant replicated cluster (proxy has no failover)
- An approximate nearest-neighbour (ANN) vector database
- A system with automatic background compaction
- A production monitoring platform

The design documents explicitly state these boundaries at every phase.

---

## Recruiter-Facing Bullets

- Built a Go database engine from scratch: WAL → MemTable → SSTable → Bloom filter → LSM-tree Engine
- Implemented manual full compaction with atomic manifest swap and crash-safe invariants
- Implemented exact vector search (cosine, L2, dot product) with engine-backed persistence
- Designed and implemented a deterministic consistent-hash sharding layer over multiple in-process engines
- Built a local leader/follower replication simulation with binary operation log, COMMIT durability file, and follower pause/lag/catch-up controls
- Built a local HTTP observability dashboard with HTML rendering, JSON status endpoints, and deterministic chaos/failure scenarios
- Built a real networked node runtime: independent `shardforge-node` processes, HTTP/JSON API, Docker Compose 3-node demo
- Implemented a client-side routing gateway (`shardforge-gateway`) with deterministic consistent-hash routing, virtual nodes, weight support, and per-node health/flush/compact fanout
- Implemented a stateless HTTP routing proxy (`shardforge-proxy`) that exposes one HTTP/JSON API and routes requests to independent nodes; includes scope flags, no-failover proof, and Docker Compose integration
- Implemented networked read replicas (`internal/replnet`): in-memory append-only mutation log with monotonic seq numbers, explicit pull-based follower sync, follower write rejection (403), 4 node endpoints + 2 proxy admin endpoints, Docker Compose 1-primary+2-replica demo
- 700+ tests across all packages, race-safe, with reproducible benchmark results documented at every phase
- Full documentation: DESIGN.md (architecture), PROOF.md (per-phase evidence), BENCHMARKS.md (reproducible numbers)

---

## What I Would Build Next

If this were a real production system, the logical next steps would be:

1. **Automatic background replication** — background sync loop so followers stay up-to-date continuously
2. **Raft consensus** — leader election, log replication, cluster membership changes
3. **Cluster membership** — node discovery, join/leave protocols, gossip
4. **ANN vector index** — HNSW or IVF for approximate nearest-neighbour at scale
5. **Background compaction** — automatic size-tiered or leveled compaction triggered by SSTable count
6. **Block cache** — in-memory LRU cache for hot SSTable blocks
7. **Snapshot and log compaction** — Raft snapshot protocol to bound log growth
8. **Multi-version concurrency control (MVCC)** — snapshot-isolated reads
9. **Distributed tracing and metrics** — OpenTelemetry integration
10. **Proxy horizontal scaling** — multiple proxy instances with shared ring configuration

---

## Phase 19 Addition

Added operations simulation layer (`internal/ops`):

- `CheckClusterHealth` — HTTP `/healthz` polling with latency tracking, sorted results, clear error strings
- `RouteKey` / `RouteKeyWithAvailableNodes` — pure ring-based routing for simulation (no network)
- `SimulateFailure` — routing impact analysis for specified node failures (simulation only)
- `PlanManualRebalance` — key movement plan when nodes are added/removed (no data movement)
- `DefaultOpsScope` — all 8 flags true: manual only, simulation only, no automatic failover, no data movement, etc.
- 3 new `shardforge-cluster` commands: `health`, `simulate-failure`, `plan-rebalance`
- `configs/local-failure-sim-3node.json` — ops demo config

**Architecture map addition:**
```
internal/ops — operations simulation layer
  ├── health.go     — CheckClusterHealth: HTTP /healthz polling
  ├── simulate.go   — RouteKey, RouteKeyWithAvailableNodes, SimulateFailure
  └── rebalance.go  — PlanManualRebalance, buildGateway
```

Honest claim: "Implemented failure visibility and manual rebalance simulation tools that show node health, routing impact, and key movement plans for static cluster configs."
