# ShardForgeDB — Claims Audit

This document defines which claims are safe to make about ShardForgeDB and which are forbidden.
All documentation, README copy, and recruiter materials must comply with this list.

---

## Safe claims

The following are accurate, honest descriptions of what exists in the codebase:

| Claim | Evidence |
|---|---|
| WAL-backed single-node key-value engine | `internal/wal`, `internal/engine` — CRC-checksummed binary WAL, full replay on restart |
| SSTable-backed persistent storage | `internal/sstable` — sorted immutable files with index block, CRC footer, atomic creation |
| Bloom filter acceleration | `internal/bloom` — FNV-1a double hashing, configurable FPR, binary serialization |
| Manual full compaction | `internal/engine.Compact()` — merges all SSTables, drops tombstones, atomic manifest swap |
| Exact vector search (cosine / L2 / dot) | `internal/vector` — brute-force k-NN, engine-backed persistence, namespace isolation |
| Local consistent-hash sharding | `internal/shard` — FNV-1a ring over multiple in-process engines, SHARDING.json manifest |
| Local replication simulation | `internal/replica` — leader/follower op-log, pause/lag/catch-up, COMMIT file |
| Local chaos and failure scenarios | `internal/dashboard` — follower pause/lag/catch-up scenarios, timeline events |
| Real networked HTTP node runtime | `internal/node` + `cmd/shardforge-node` — independent processes, full HTTP/JSON API |
| Client-side consistent-hash routing | `internal/gateway` + `cmd/shardforge-gateway` — FNV-1a ring, virtual nodes, weight support |
| Stateless proxy over independent HTTP nodes | `internal/proxy` + `cmd/shardforge-proxy` — 10 endpoints, no failover, scope flags |
| Static cluster metadata | `internal/cluster` + `cmd/shardforge-cluster` — typed JSON config, validate/print/example |
| Explicit pull-based read-replica sync | `internal/replnet` — in-memory mutation log, follower pulls on demand, 403 for follower writes |
| Failure simulation (no live calls) | `internal/ops.SimulateFailure` — routing impact on sample keys, pure computation |
| Manual rebalance planning (no data movement) | `internal/ops.PlanManualRebalance` — key movement plan, operator steps, pure computation |
| Health check visibility | `internal/ops.CheckClusterHealth` — HTTP /healthz polling, latency, sorted results |
| 700+ race-safe tests | `go test -race -count=1 ./...` passes across all 25 packages |
| Reproducible benchmarks | `make bench-*` targets, results in `docs/BENCHMARKS.md` |

---

## Unsafe / forbidden claims

The following must never appear in documentation, README, or recruiter materials:

| Forbidden claim | Why it is wrong |
|---|---|
| "production distributed database" | Nodes are independent, not coordinated; no fault tolerance |
| "fault-tolerant cluster" | No quorum, no automatic failover, no leader election |
| "Raft implementation" | Raft is not present at any layer |
| "Paxos implementation" | Paxos is not present |
| "consensus" | No consensus protocol exists |
| "quorum replication" | Replication is explicit pull-based, not quorum |
| "automatic failover" | All failover is manual operator action |
| "automatic rebalancing" | Rebalancing is planning-only, no data movement |
| "shard migration" | No data movement between nodes exists |
| "strong consistency" | Replication lag is expected; no consistency guarantee |
| "distributed transactions" | No cross-node transactions |
| "ANN / HNSW / IVF" | Vector search is exact brute-force only |
| "background compaction" | Compaction is manual only (explicit `Compact()` call) |
| "automatic background sync" | Follower sync is explicit pull-on-demand only |
| "self-healing cluster" | No automatic recovery exists |
| "production fault tolerance" | This is an educational/portfolio project |

---

## Correct wording examples

### Resume / LinkedIn (safe):

```
Built an explainable Go database engine with WAL-backed storage, SSTables, Bloom filters,
exact vector search, HTTP node runtime, deterministic client-side routing, static cluster
metadata, explicit read-replica sync, and ops simulation tools — 700+ race-safe tests,
reproducible benchmarks at every phase.
```

### Project description (safe):

```
ShardForgeDB is a 20-phase Go database engine built for learning and portfolio purposes.
It implements a WAL-backed LSM-tree key-value store, exact vector search, a real networked
HTTP node runtime, a stateless routing proxy, explicit pull-based read replicas, and
operations simulation tools. It does not implement Raft, consensus, automatic failover,
or production fault tolerance.
```

### What to say if asked "does it handle node failures?":

```
ShardForgeDB includes an operations simulation layer (internal/ops) that shows the routing
impact of specified node failures on sample keys and produces a manual rebalance plan with
operator steps. It does not perform automatic failover or data movement — those require
manual operator action.
```

---

## Scope flags enforced in code

Every ops result includes a `scope` object with all flags `true`:
- `manual_only`
- `simulation_only`
- `no_automatic_failover`
- `no_automatic_rebalancing`
- `no_shard_migration`
- `no_data_movement`
- `no_consensus`
- `no_raft`

Every cluster config includes a `scope` object with all limitations documented as `true`.
