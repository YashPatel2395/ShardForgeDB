# ShardForgeDB — Architecture

This document describes the system architecture, component responsibilities, data flows, and known limitations.

---

## Layered diagram

```
╔══════════════════════════════════════════════════════════════╗
║  CLI / Admin Tooling                                         ║
║  shardforge         (explain, explain-node, version)         ║
║  shardforge-cluster (validate, print, health, simulate,      ║
║                      plan-rebalance, example-*)              ║
╚══════════════════════════════════╦═══════════════════════════╝
                                   │
╔══════════════════════════════════▼═══════════════════════════╗
║  Network Layer                                               ║
║  shardforge-proxy  (stateless HTTP routing proxy, port 9200) ║
║  shardforge-gateway (client-side ring routing library)        ║
║  internal/cluster  (static JSON config, typed, validated)    ║
╚══════════════════════════════════╦═══════════════════════════╝
                                   │ HTTP/JSON (real TCP)
╔══════════════════════════════════▼═══════════════════════════╗
║  Networked Node Runtime                                      ║
║  shardforge-node  (independent HTTP node processes)          ║
║  internal/node    (server, handlers, client, types)          ║
║  internal/node    (explain endpoints: POST/GET/DELETE/SCAN)  ║
║  internal/replnet (in-memory mutation log, replicator)       ║
╚══════════════════════════════════╦═══════════════════════════╝
                                   │
╔══════════════════════════════════▼═══════════════════════════╗
║  Explainability Layer (Phase 21–23)                          ║
║  internal/trace  (Trace, TraceStep, StepType, Component)     ║
║  internal/engine (ExplainGet/Put/Delete/Scan)                ║
║  internal/vector (ExplainUpsert/Search/Delete)               ║
╚══════════════════════════════════╦═══════════════════════════╝
                                   │
╔══════════════════════════════════▼═══════════════════════════╗
║  Storage Engine                                              ║
║  internal/engine   (LSM-tree: WAL + MemTable + SSTables)    ║
║  internal/wal      (append-only CRC log)                     ║
║  internal/memtable (ordered concurrent write buffer)         ║
║  internal/sstable  (sorted immutable file format)            ║
║  internal/bloom    (probabilistic membership filter)         ║
║  internal/vector   (exact k-NN vector index)                 ║
╚══════════════════════════════════════════════════════════════╝
╔══════════════════════════════════════════════════════════════╗
║  Single-process Simulation (no cross-node networking)        ║
║  internal/shard     (FNV-1a ring over multiple engines)      ║
║  internal/replica   (leader/follower op-log, pause/lag)      ║
║  internal/dashboard (local HTTP observability + chaos)       ║
║  internal/ops       (health visibility, sim, planning)       ║
╚══════════════════════════════════════════════════════════════╝
```

---

## Component descriptions

### `internal/wal`
Append-only, CRC-32-checksummed binary write-ahead log. Little-endian record format: length (4B) + CRC (4B) + sequence (8B) + op (1B) + key + value. Supports `Append`, `Replay`, and WAL rotation. Detects partial tail writes and corruption; never silently drops records.

### `internal/memtable`
Ordered, concurrent in-memory write buffer. Keys are stored in a sorted `[]Entry` slice with binary search for Get and Scan. Uses `sync.RWMutex`. Tracks approximate byte size for flush threshold. Deletion tombstones are explicit entries.

### `internal/sstable`
Sorted, immutable, on-disk SSTable file format. Structure: header, data records (key/value with CRC + seq), index block (all key offsets), footer (index offset + record count + CRC). Atomic creation via temp-file + rename. Supports O(log n) Get via binary search over in-memory index, full range Scan.

### `internal/bloom`
Deterministic Bloom filter using FNV-1a double hashing. Parameters derived from target FPR and expected item count using standard formulas. Packed `[]uint64` bit array. Binary serialization with magic number, version, CRC-32, and trailing sentinel. Concurrent-safe via `sync.RWMutex`.

### `internal/engine`
Single-node LSM-tree engine wiring WAL + MemTable + SSTables + Bloom. Atomic `MANIFEST.json` tracks all SSTable and Bloom sidecar paths. WAL replay on restart reconstructs MemTable. `Flush` writes MemTable to SSTable + Bloom sidecar, updates manifest, rotates WAL. `Compact` merges all SSTables, drops tombstones, atomic manifest swap. `Scan` merges MemTable and all SSTables in key order.

Also provides Explain variants (`ExplainGet`, `ExplainPut`, `ExplainDelete`, `ExplainScan`) that mirror the exact same execution paths and return a `*trace.Trace` of every real step taken — no fabricated steps. See `docs/TRACE_DESIGN.md`.

### `internal/vector`
Exact k-nearest-neighbour vector store backed by the Engine. Vectors stored in a namespace-prefixed key. In-memory exact index rebuilt on `Open` by scanning the namespace. Supports cosine, L2, dot product. `Search` is brute-force O(n) — not ANN, not HNSW.

Also provides `ExplainUpsert`, `ExplainSearch`, `ExplainDelete` returning real execution traces.

### `internal/trace`
Type package for operation tracing. Defines `Trace`, `TraceStep`, `OperationType`, `Component`, `StepType`, `Status`. All step types are constants; fabricated steps are explicitly forbidden. Traces are per-operation, not process-level logging. See `docs/TRACE_DESIGN.md`.

### `internal/shard`
Local consistent-hash sharding over multiple in-process `Engine` instances. FNV-1a 64-bit ring with configurable virtual nodes and weight. `SHARDING.json` manifest for reopen safety. Single-key operations route to exactly one shard. Fan-out `Scan` merges all shards. No cross-process networking.

### `internal/replica`
Local in-process leader/follower replication simulation. Binary operation log with CRC-32 per record. Leader-commit semantics: Put/Delete write to leader, append to log. Followers apply via `ReplicateOnce`/`ReplicateAll`. Supports follower pause and lag simulation. Applied index persisted per follower. No networking.

### `internal/dashboard`
Local HTTP observability dashboard. HTML dashboard, JSON status, `/healthz`, `/events`. Three deterministic chaos scenarios: follower pause, follower lag, follower catch-up. Rendered via Go `html/template`. Local only — no networked node discovery.

### `internal/node`
Real networked node runtime. Each node is an independent HTTP/JSON server backed by a local Engine. API: `GET /healthz`, `GET /status`, `PUT/GET/DELETE /kv/{key}`, `GET /scan`, `POST /flush`, `POST /compact`, plus 4 replication endpoints, plus 4 explain endpoints (`POST /explain/put`, `GET /explain/get`, `DELETE /explain/delete`, `GET /explain/scan`). `node.Client` includes typed explain methods.

### `internal/replnet`
Networked replication primitives. `Log` is an in-memory append-only mutation log with monotonic `uint64` sequence numbers. `Replicator` is an HTTP pull client that calls `GET /replication/log` on the primary. Follower nodes sync on demand via `POST /replication/sync`. Not persisted — cleared on restart.

### `internal/gateway`
Client-side routing gateway. FNV-1a 64-bit consistent-hash ring. `NodeForKey` does clockwise ring lookup. `Put/Get/Delete` route to the responsible node — no retry to another node. `FlushAll/CompactAll/HealthAll` fan out to all nodes.

### `internal/proxy`
Stateless HTTP routing proxy wrapping the gateway. 10 endpoints: `/healthz`, `/status`, `/route/{key}`, `/kv/{key}` (PUT/GET/DELETE), `/scan-node/{nodeID}`, `/flush-all`, `/compact-all`, `/nodes/health`. No failover, no retry, no distributed state.

### `internal/cluster`
Static, file-based cluster configuration. Typed, validated JSON config: nodes, routing (algorithm + virtual nodes), proxy settings, and scope flags. `cluster.GatewayOptions` and `cluster.ProxyOptions` convert config to gateway/proxy inputs.

### `internal/ops`
Operations and simulation layer. `CheckClusterHealth` polls `/healthz` on all nodes and returns sorted results with latency. `SimulateFailure` shows routing impact of specified failures on sample keys — no live calls. `PlanManualRebalance` shows key movement when nodes change — no data movement. All results include `OpsScope` with all 8 flags true.

---

## Data flows

### Single-node write (Put)

```
PUT /kv/{key}
  → node.handleKVPut
  → engine.Put(key, value)
      → wal.Append(OpPut, key, value)    [fsync if WAL sync enabled]
      → memtable.Put(key, value)
  → if primary: replnet.Log.Append(OpPut, key, value)
  → 200 OK
```

### Explain write (ExplainPut via HTTP)

```
POST /explain/put  {"key": "k", "value": "v"}
  → node.handleExplainPut
  → engine.ExplainPut(key, value)        [real write path + trace]
      → tr.Step(ENGINE, KEY_VALIDATED)
      → wal.Append(...)                  → tr.Step(WAL, WAL_APPEND, duration)
      → memtable.Put(...)                → tr.Step(MEMTABLE, MEMTABLE_PUT, duration)
  → 200 {"node_id":..., "trace": {...}}  [no fabricated steps]
```

### Single-node recovery (restart)

```
engine.Open(dir)
  → read MANIFEST.json                   [lists SSTable + Bloom paths]
  → open existing SSTables + Bloom sidecars
  → wal.Replay()                         [reconstruct MemTable from WAL]
  → ready to serve
```

### SSTable Get (key not in MemTable)

```
engine.Get(key)
  → memtable.Get(key) → not found
  → for each SSTable (newest first):
      → bloom.MightContain(key) → false → skip
      → bloom.MightContain(key) → true
          → sstable.Get(key)             [binary search on in-memory index]
          → if found: return value
  → return (nil, false)
```

### Exact vector search

```
vector.Search(query, k, metric)
  → load all vectors from in-memory index
  → compute distance(query, v) for each vector
  → return top-k by distance
  [O(n·d) brute force — no ANN approximation]
```

### Proxy-routed request (Put via proxy)

```
PUT http://proxy:9200/kv/user:1
  → proxy.handleKVPut
  → gateway.Put(key, value)
      → ring.nodeForKey(key)             [FNV-1a clockwise lookup]
      → node.Client.Put(key, value)      [HTTP to selected node]
  → 200 OK (or 502 if node unreachable — no retry)
```

### Read-replica sync

```
POST /replication/sync  (on follower)
  → replicator.PullEntries(ctx, lastAppliedSeq, limit)
      → GET primary:9111/replication/log?after=N&limit=M
      → primary returns JSON entries from replnet.Log
  → apply entries: engine.Put/Delete for each entry
  → update lastAppliedSeq
```

### Ops failure simulation

```
SimulateFailure(cfg, {downNodeIDs, sampleKeys})
  → build ring from cfg (all nodes)
  → compute origNode = ring.nodeForKey(key)  for each key
  → build ring from cfg minus downNodes
  → compute newNode = ring.nodeForKey(key)   for each key
  → affected if origNode ∈ downSet or newNode ≠ origNode
  [No HTTP calls. No data movement. Pure ring computation.]
```

---

## Known limitations

| Limitation | Detail |
|---|---|
| No Raft / consensus | Nodes are independent. No leader election. No quorum writes. |
| No automatic failover | Proxy returns 502 on node failure; no rerouting to another node |
| No shard migration | Keys stay on their original node. No data movement between nodes. |
| No dynamic membership | Cluster config is static JSON loaded at startup; no join/leave |
| No background compaction | `Compact()` must be called explicitly; no automatic thresholds |
| No replication log persistence | `replnet.Log` is in-memory; cleared on primary restart |
| No strong consistency | Follower reads may lag behind primary by an arbitrary number of ops |
| Exact vector search only | O(n·d) brute-force; no HNSW, no IVF, no approximation |
| Proxy no-retry policy | Without replication, retrying to another node would silently miss data |
| Local simulations only | `internal/shard`, `internal/replica`, `internal/dashboard` use in-process engines; no cross-node networking |
| No distributed tracing | Explain traces cover a single node's execution path only; no cross-node trace propagation |

---

## Phase 24 — Local Cluster Demo Infrastructure

Phase 24 adds reproducible local cluster demo scripts and tests on top of the existing networked node runtime and proxy. No new runtime packages were added.

```
configs/cluster/demo-3node.json   ← Phase 24 demo cluster config
  │ 3 nodes (node-1/9101, node-2/9102, node-3/9103) + proxy/9200
  │ scope: no_raft, no_consensus, no_failover, no_replication, no_shard_migration
  ▼
scripts/demo_cluster_up.sh     ← start nodes + proxy as local processes
scripts/demo_cluster_smoke.sh  ← 18-check smoke: health, routing, put/get, isolation, explain
scripts/demo_cluster_down.sh   ← stop processes, remove data dirs

Routing (pure ring, no network call):
  ./bin/shardforge-gateway --config configs/cluster/demo-3node.json route user:1
  → key="user:1" → node_id=node-2  base_url=http://127.0.0.1:9102

Data isolation (each node independent, no replication):
  Write to node-1 → readable on node-1, NOT on node-2 or node-3 (404)

explain-node across the cluster:
  ./bin/shardforge explain-node --addr http://127.0.0.1:9101 put mykey myval
  → WAL_APPEND, MEMTABLE_PUT steps from real engine code on node-1
```

**Scope:** This is a local demo with static routing. Not a real distributed cluster.
- No Raft, no consensus, no quorum replication.
- No automatic failover, no shard migration, no dynamic membership.
- See `docs/DEMO.md` for the full demo guide and honest limitations.
