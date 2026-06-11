# ShardForgeDB — Final Engineering Report

**Version:** v0.2.0-portfolio
**Status:** 20-phase build complete, all tests passing, all benchmarks reproducible.

---

## Executive summary

ShardForgeDB is a ground-up Go database engine built as a 20-phase educational and portfolio project. It implements a WAL-backed LSM-tree key-value store, exact vector search, a real networked HTTP node runtime, a stateless routing proxy, explicit pull-based read replicas, and an operations simulation layer. All phases are tested, benchmarked, and documented. Every design decision and limitation is explicitly stated.

The project is not a production database. It is an explainable, deeply documented implementation that demonstrates real engineering depth across storage, networking, routing, replication, and operations tooling.

---

## Phase-by-phase summary

| Phase | Package(s) | What was built | Tests | Benchmarks |
|---|---|---|---|---|
| 1 | `cmd/shardforge` | CLI skeleton, YAML config, structured logging, GitHub Actions CI | — | — |
| 2 | `internal/wal` | Append-only CRC-checksummed WAL, `Append`/`Replay`, corruption detection | 24 | 4 |
| 3 | `internal/memtable` | Sorted concurrent write buffer, tombstones, size accounting | 30 | 7 |
| 4 | `internal/sstable` | Immutable on-disk segment: data records, index block, CRC footer, atomic creation | 46 | 7 |
| 5 | `internal/bloom` | FNV-1a double-hashing Bloom filter, configurable FPR, binary serialization | 35 | 9 |
| 6 | `internal/engine` | LSM-tree Engine: WAL + MemTable + SSTables + Bloom, `MANIFEST.json`, WAL replay | 45 | 10 |
| 7 | `internal/engine` | Manual full compaction, tombstone suppression, atomic manifest swap | 34 | 8 |
| 8 | `internal/bench` + CLI | Six workloads, P50/P95/P99 latency collection, Markdown report generator | 34 | 5 |
| 9 | `internal/vector` | Exact k-NN (cosine/L2/dot), engine-backed persistence, namespace isolation | 49 | 10 |
| 10 | `internal/shard` | FNV-1a consistent-hash ring over multiple local engines, `SHARDING.json` manifest | 55 | 10 |
| 11 | `internal/replica` | Binary op-log, leader-commit semantics, follower pause/lag/catch-up, `COMMIT` file | 66 | 10 |
| 12 | `internal/dashboard` | Local HTTP dashboard (HTML + JSON), chaos scenarios, timeline events | 52 | 8 |
| 13 | scripts, docs | Release hardening, smoke script, release checklist, docs polish | — | — |
| 14 | `internal/node` | Real networked HTTP node: full API, `node.Client`, Docker Compose 3-node | 36 | 6 |
| 15 | `internal/gateway` | Client-side FNV-1a ring, `NodeForKey`, `Put/Get/Delete/HealthAll/FlushAll` | 41 | 6 |
| 16 | `internal/proxy` | Stateless proxy: 10 endpoints, no failover, scope flags, Docker Compose | 45 | 7 |
| 17 | `internal/cluster` | Typed JSON config, `Validate`/`Normalize`/`Load`, `shardforge-cluster` CLI | 47 | 4 |
| 18 | `internal/replnet` | In-memory mutation log, `Replicator`, 4 node replication endpoints, Docker Compose replica | 55+ | 5 |
| 19 | `internal/ops` | `CheckClusterHealth`, `SimulateFailure`, `PlanManualRebalance`, 3 CLI commands | 40 | 4 |
| 20 | docs | Architecture doc, claims audit, roadmap, demo script, resume content, final polish | — | — |

**Total tests:** 700+
**Total benchmarks:** 120+
**Packages with tests:** 25 of 25

---

## Architecture summary

The engine is a classic LSM-tree: writes go to WAL (for durability) and MemTable (for fast reads), then flush to SSTables on disk. Bloom filters skip unnecessary SSTable reads. Manual compaction merges all SSTables and drops tombstones.

The vector store uses the engine as its persistence layer and maintains an in-memory exact index rebuilt on open.

The networked layer adds real HTTP/JSON nodes, a client-side consistent-hash routing gateway, and a stateless proxy. Each node is independent — no coordination, no shared state.

Read replicas add explicit pull-based sync: the primary keeps an in-memory mutation log; followers pull entries on demand. No automatic background sync, no Raft.

The ops layer adds health visibility, failure simulation (routing impact without live calls), and manual rebalance planning (key movement without data movement).

---

## Testing summary

| Metric | Value |
|---|---|
| Total test functions | 700+ |
| Packages tested | 25 of 25 |
| Race detector | Enabled on all runs |
| Test count per CI run | `-count=1` (no caching) |
| Concurrency tests | Yes (WAL, MemTable, Engine, Log, Gateway) |
| Integration tests | Yes (node, proxy, replnet, ops) |
| CLI tests | Yes (shardforge-cluster, shardforge-gateway, shardforge-proxy) |

All tests pass on Apple M3 darwin/arm64 with Go 1.26.

---

## Benchmark summary

Benchmarks are run with `-benchmem` for allocation tracking. All results from local development machine (Apple M3, darwin/arm64).

| Category | Key result |
|---|---|
| Engine Put (WAL + MemTable) | ~1.7 µs/op |
| Engine Get (MemTable hit) | ~140 ns/op |
| SSTable Get (disk) | ~2 µs/op |
| Bloom MightContain | ~110 ns/op |
| Shard Ring route | ~78 ns/op |
| Vector Search (100-vector, cosine) | ~11 µs/op |
| Node Handler Put (HTTP in-process) | ~20 µs/op |
| Gateway NodeForKey (ring lookup) | ~24 ns/op |
| Proxy Put (proxy→node TCP loopback) | ~74 µs/op |
| Cluster Validate | ~110 ns/op (zero allocations) |
| Ops RouteKey | ~44 µs/op |
| Ops PlanManualRebalance (100 keys) | ~81 µs/op |
| Ops CheckClusterHealth (3 local nodes) | ~93 µs/op |
| Log Append (replnet) | ~113 ns/op |
| Log Stats (replnet) | ~3.9 ns/op |

---

## Safety and limitations

ShardForgeDB makes no production database claims. All limitations are explicitly documented:

- No Raft, no consensus, no quorum, no automatic failover
- No shard migration, no data movement, no automatic rebalancing
- No dynamic membership, no service discovery, no gossip
- No strong consistency guarantee (follower reads may lag)
- No distributed transactions
- Vector search is exact brute-force (O(n·d)); no ANN approximation
- Compaction is manual only; no background automatic thresholds
- Replication log is in-memory; cleared on primary restart
- Proxy has a no-retry policy (correct without replication)

---

## What I learned / engineering value

**Storage internals:** Building WAL + MemTable + SSTable + Bloom from scratch forced a real understanding of LSM-tree mechanics — not just the theory, but why tombstones need special handling during compaction, why Bloom filters are per-SSTable, why WAL rotation matters, and why atomic manifest swaps are necessary.

**Binary format design:** Every file format (WAL records, SSTable, Bloom) was designed with CRC checksums, magic numbers, and crash-safe creation patterns. Thinking through partial-write scenarios and replay correctness was the most educational part of Phases 2–7.

**HTTP systems design:** Building a real node runtime with an HTTP/JSON API, then building a client-side routing library, then a stateless proxy over it gave direct experience with the full HTTP stack — not just handler code, but routing semantics, timeout propagation, and error handling at each layer.

**Replication semantics:** Designing the replnet layer required thinking carefully about sequence numbers, monotonicity, follower state, and what "applied" means. Understanding why you can't retry to another node without replication was the most important design insight.

**Honest documentation discipline:** The scope flags pattern — explicitly documenting what a component does *not* do — forced precision in every phase. Writing a forbidden claims list is a discipline that transfers to any engineering communication.

---

## Release status

- All 25 packages pass `go test -race -count=1 ./...`
- All 7 binaries build via `make build`
- All Docker Compose configs validate
- All cluster configs validate
- All docs complete
- No forbidden claims in any documentation
- Suggested release tag: `v0.2.0-portfolio`
