# ShardForgeDB — Project Summary

## Overview

ShardForgeDB is a ground-up Go database engine built layer-by-layer to be fully explainable at every level of the stack. Every design decision, data structure, trade-off, and benchmark result is documented alongside the code. The project is structured as a sequence of fourteen numbered phases, each building on the previous and producing its own tests, benchmarks, and documentation.

The goal is not to compete with production databases. The goal is to demonstrate deep understanding of database internals — how LSM trees actually work, what makes replication hard, why compaction matters, how to design a real networked node transport — through working code that is honest about what it is and what it is not.

---

## Architecture Summary

```
HTTP Client / shardforge-node CLI (Phase 14)
  │ real HTTP/JSON over TCP
  ▼
Node Server (internal/node)
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

Docker Compose Demo (Phase 14)
  ├── shardforge-node-1 — independent node, port 9101, /data/node-1
  ├── shardforge-node-2 — independent node, port 9102, /data/node-2
  └── shardforge-node-3 — independent node, port 9103, /data/node-3
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

---

## Scope Honesty

ShardForgeDB is honest about what it is and is not:

**What it is:**
- A working, tested, documented Go implementation of an LSM-tree key-value engine
- An exact vector search engine (brute-force cosine / L2 / dot product)
- A local simulation of sharding, replication, and a chaos-testing dashboard
- A real networked node runtime with HTTP/JSON API and Docker Compose multi-node demo
- A portfolio project designed to demonstrate deep database internals knowledge

**What it is not:**
- A production database
- A distributed system (Phase 14 nodes are independent, not coordinated)
- A Raft or Paxos implementation
- A fault-tolerant replicated cluster
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
- 500+ tests across all packages, race-safe, with reproducible benchmark results documented at every phase
- Full documentation: DESIGN.md (architecture), PROOF.md (per-phase evidence), BENCHMARKS.md (reproducible numbers)

---

## What I Would Build Next

If this were a real production system, the logical next steps would be:

1. **Shard router** — a routing layer that maps keys to the correct node via consistent hashing over real HTTP
2. **Networked replication** — a leader node that replicates writes to follower nodes over the network
3. **Raft consensus** — leader election, log replication, cluster membership changes
4. **Cluster membership** — node discovery, join/leave protocols, gossip
5. **ANN vector index** — HNSW or IVF for approximate nearest-neighbour at scale
6. **Background compaction** — automatic size-tiered or leveled compaction triggered by SSTable count
7. **Block cache** — in-memory LRU cache for hot SSTable blocks
8. **Snapshot and log compaction** — Raft snapshot protocol to bound log growth
9. **Multi-version concurrency control (MVCC)** — snapshot-isolated reads
10. **Distributed tracing and metrics** — OpenTelemetry integration
