# ShardForgeDB — Roadmap

---

## Completed: Phases 1–23

| Phase | Feature |
|---|---|
| 1 | CLI skeleton, config loading, structured logging, GitHub Actions CI |
| 2 | WAL — append-only CRC-checksummed write-ahead log |
| 3 | MemTable — ordered concurrent in-memory write buffer |
| 4 | SSTable — sorted immutable on-disk segment format |
| 5 | Bloom filter — FNV-1a double hashing, binary serialization |
| 6 | Single-node LSM-tree Engine |
| 7 | Manual full compaction with atomic manifest swap |
| 8 | Benchmark CLI — six workloads, P50/P95/P99, Markdown output |
| 9 | Exact vector search — cosine / L2 / dot, engine-backed |
| 10 | Local consistent-hash sharding |
| 11 | Local leader/follower replication simulation |
| 12 | Local HTTP dashboard and chaos/failure scenarios |
| 13 | Polish, release scripts, docs hardening |
| 14 | Real networked HTTP node runtime + Docker Compose 3-node |
| 15 | Client-side consistent-hash routing gateway |
| 16 | Stateless HTTP routing proxy |
| 17 | Static cluster metadata (JSON config, `shardforge-cluster` CLI) |
| 18 | Networked read replicas v1 (explicit pull-based, in-memory log) |
| 19 | Ops simulation — health checks, failure sim, rebalance planning |
| 20 | Portfolio polish — architecture docs, claims audit, launch readiness |
| 21 | Trace foundation — `internal/trace` type package (Trace, TraceStep, StepType, Component) |
| 22 | Runtime operation trace mode — `ExplainGet/Put/Delete/Scan` + `ExplainUpsert/Search/Delete` + `shardforge explain` CLI |
| 23 | Networked node trace API — HTTP `/explain/*` endpoints, `node.Client` explain methods, `shardforge explain-node` CLI |

---

## Possible future work

**None of the following are implemented. They are listed for reference only.**

### Storage engine

- **Background compaction** — automatic size-tiered or leveled compaction triggered by SSTable count or size
- **Block cache** — in-memory LRU cache for hot SSTable index blocks and data blocks
- **Prefix compression** — shared prefix encoding for keys within SSTable data blocks

### Vector search

- **ANN / HNSW** — approximate nearest-neighbour index (HNSW, IVF, PQ)
- **Distributed vector search** — query executed across multiple nodes

### Distributed systems

- **Raft** — full leader election, term handling, replicated log, commit index, voting
- **Quorum replication** — write acknowledged only after N/2+1 nodes confirm
- **Automatic leader election** — dynamic primary promotion on failure
- **Shard migration** — actual data movement between nodes when membership changes
- **Dynamic membership** — join/leave protocol with gossip or Raft membership changes
- **Distributed transactions** — cross-shard atomic operations

### Observability

- **Distributed tracing** — cross-node trace propagation with trace context headers
- **Metrics endpoint** — Prometheus-compatible `/metrics` on each node
- **Real cluster dashboard** — polls live distributed nodes

### Operations

- **Automatic failover** — follower automatically promoted when primary fails
- **Automatic rebalancing** — keys migrated when nodes are added/removed
