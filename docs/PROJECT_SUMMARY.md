# ShardForgeDB — Project Summary

## Overview

ShardForgeDB is a ground-up Go database engine built layer-by-layer to be fully explainable at every level of the stack. Every design decision, data structure, trade-off, and benchmark result is documented alongside the code. The project is structured as a sequence of thirteen numbered phases, each building on the previous and producing its own tests, benchmarks, and documentation.

The goal is not to compete with production databases. The goal is to demonstrate deep understanding of database internals — how LSM trees actually work, what makes replication hard, why compaction matters — through working code that is honest about what it is and what it is not.

---

## Architecture Summary

```
Client
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

---

## Scope Honesty

ShardForgeDB is honest about what it is and is not:

**What it is:**
- A working, tested, documented Go implementation of an LSM-tree key-value engine
- An exact vector search engine (brute-force cosine / L2 / dot product)
- A local simulation of sharding, replication, and a chaos-testing dashboard
- A portfolio project designed to demonstrate deep database internals knowledge

**What it is not:**
- A production database
- A distributed system
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
- 460+ tests across all packages, race-safe, with reproducible benchmark results documented at every phase
- Full documentation: DESIGN.md (architecture), PROOF.md (per-phase evidence), BENCHMARKS.md (reproducible numbers)

---

## What I Would Build Next

If this were a real production system, the logical next steps would be:

1. **RPC transport** — gRPC or custom binary protocol for cross-node communication
2. **Raft consensus** — leader election, log replication, cluster membership changes
3. **Networked shard cluster** — RPC-based routing, shard migration, rebalancing
4. **ANN vector index** — HNSW or IVF for approximate nearest-neighbour at scale
5. **Background compaction** — automatic size-tiered or leveled compaction triggered by SSTable count
6. **Block cache** — in-memory LRU cache for hot SSTable blocks
7. **Snapshot and log compaction** — Raft snapshot protocol to bound log growth
8. **Multi-version concurrency control (MVCC)** — snapshot-isolated reads
9. **Distributed tracing and metrics** — OpenTelemetry integration
10. **Horizontal read scaling** — read replicas served from follower nodes over real network
