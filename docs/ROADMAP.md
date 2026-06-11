# ShardForgeDB — Roadmap

---

## Completed: Phases 1–20

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
| 20 | Final polish — architecture docs, claims audit, launch readiness |

---

## Possible future work

**None of the following are implemented. They are listed for reference only.**

### Storage engine

- **Background compaction** — automatic size-tiered or leveled compaction triggered by SSTable count or size
- **Block cache** — in-memory LRU cache for hot SSTable index blocks and data blocks
- **Prefix compression** — shared prefix encoding for keys within SSTable data blocks
- **Bloom filter per-SSTable tuning** — adaptive FPR based on level and key count
- **Write batch API** — atomic multi-key writes sharing a single WAL record

### Replication and consensus

- **Persistent replication log** — durable `replnet.Log` backed by WAL so primary restarts don't lose the log
- **Background follower sync** — automatic polling loop so followers stay up-to-date without manual `POST /replication/sync`
- **Raft consensus** — leader election, replicated log, membership changes, log compaction via snapshots
- **Quorum writes** — require acknowledgment from a majority before returning success
- **Multi-primary writes** — conflict resolution strategy (last-write-wins, CRDT, etc.)

### Cluster management

- **Dynamic membership** — node join/leave protocol without restart
- **Service discovery** — automatic node registration via DNS, etcd, or gossip
- **Gossip protocol** — epidemic membership and health propagation
- **Automatic failover** — promote a follower to primary when primary is unreachable

### Rebalancing and migration

- **Shard migration** — move key ranges between nodes when adding/removing nodes
- **Online resharding** — redistribute data without downtime
- **Automatic rebalancing** — trigger key migration when node load is uneven

### Vector search

- **HNSW index** — approximate nearest-neighbour with sublinear query time
- **IVF index** — inverted file index for large-scale ANN
- **Quantization** — scalar/product quantization to reduce memory footprint
- **Filtered vector search** — combined metadata filter + vector similarity

### Observability and operations

- **Persistent health history** — time-series node health data
- **OpenTelemetry integration** — distributed tracing and metrics
- **Prometheus exporter** — `/metrics` endpoint for node and engine stats
- **Structured audit log** — record all admin operations with timestamps and actors

### Security

- **TLS transport** — mutual TLS between nodes, proxy, and clients
- **Authentication** — API key or token-based auth for node HTTP API
- **Authorization** — per-key or per-namespace ACLs

---

## Design philosophy note

ShardForgeDB was designed for explainability first. Every phase was kept narrow and honest. Future work items are listed here not as planned features but as the natural extension of the foundation that has been built — an honest answer to "where would you go next?"

Building any of the above would require additional phases with their own tests, benchmarks, and scope documentation.
