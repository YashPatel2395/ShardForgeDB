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
║  internal/replnet (durable journal, state store, replicator) ║
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
Networked replication primitives. `DurableLog` is an append-only binary journal (`replication.journal`) with CRC-verified records, an in-memory index for fast seeks, and partial-tail crash recovery. `ReplicationStateStore` persists the follower cursor (`replication_state.json`) using atomic temp→fsync→rename writes. `Replicator` is an HTTP pull client that calls `GET /replication/log` on the primary and decodes HTTP 409 gap errors into `*ReplicationGapError`. The in-memory `Log` is retained for standalone and test use. Both the journal and cursor survive process restarts; explicit pull via `POST /replication/sync` is still operator-triggered only.

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
| Replication state durable (Phase 26) | Primary journal (`replication.journal`) and follower cursor (`replication_state.json`) survive restart |
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
scripts/demo_cluster_smoke.sh  ← 25-check smoke: health, routing, put/get, isolation, explain
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

---

## Phase 25 — Networked Pull-Based Replication Demo

Phase 25 adds a reproducible leader+follower HTTP replication demo on top of the existing `internal/replnet` and `internal/node` infrastructure. No new runtime packages were added.

```
configs/replication/demo-leader-follower.json   ← Phase 25 replication demo config
  │ 2 nodes: leader/9301 (primary) + follower/9302 (follower)
  │ scope: no_raft=true, no_consensus=true, no_quorum_replication=true, no_failover=true
  ▼
scripts/repl_demo_up.sh     ← start leader + follower as local HTTP processes
scripts/repl_demo_smoke.sh  ← 16-check smoke: health, put, isolation, pull, delete, idempotency
scripts/repl_demo_down.sh   ← stop processes, remove data dirs

Replication endpoints (already present in internal/node since Phase 18):
  GET  /replication/log?after=<seq>&limit=<n>   ← leader: serves mutation log
  POST /replication/sync                         ← follower: explicit pull from primary
  POST /replication/apply                        ← follower: apply entries from body
  GET  /replication/status                       ← any node: replication state

Phase 25 additions to internal/node:
  SyncResult type          ← fetched, applied, last_applied_seq, source_node, follower_node
  SyncFromPrimary returns  ← SyncResult (was replnet.ReplicaStatus)
  Client.SyncReplication() ← calls POST /replication/sync, returns *SyncResult

Explicit pull proof:
  1. PUT key to leader → leader log has seq=1
  2. GET key from follower → {"found":false} (no auto-sync)
  3. POST /replication/sync → {"fetched":1,"applied":1,"last_applied_seq":1}
  4. GET key from follower → value present
  5. Second POST /replication/sync → {"fetched":0,"applied":0} (idempotent)
  6. DELETE key on leader → leader log has seq=2
  7. POST /replication/sync → {"fetched":1,"applied":1}
  8. GET key from follower → {"found":false} (tombstone applied)
```

**Scope:** Explicit, operator-triggered pull replication. Not automatic. Not distributed consensus.
- No Raft, no consensus, no quorum replication, no automatic failover.
- Replication cursor (`lastApplied`) was in-memory only in Phase 25 — made durable in Phase 26.
- Follower rejects client PUT/DELETE with 403.
- See `docs/DEMO.md` for the full demo guide.

## Phase 26 — Durable Replication State and Restart Recovery

Phase 26 makes the Phase 25 replication infrastructure durable. No new HTTP endpoints. No new packages. Changes to `internal/replnet` and `internal/node` only.

```
New files in internal/replnet:
  durable_log.go      ← DurableLog: binary journal (replication.journal)
  state_store.go      ← ReplicationStateStore: follower cursor (replication_state.json)

New types in internal/replnet/types.go:
  DurableLogStats     ← count, last_seq, first_available_seq, journal_bytes, durable=true
  ReplicationGapError ← requested_after, first_available_seq, latest_seq; HTTP 409

New errors in internal/replnet/errors.go:
  ErrReplicationGap, ErrCorruptedJournal, ErrCorruptedState, ErrInvalidSeqRegression,
  ErrUnsupportedStateVersion, ErrFollowerIdentityMismatch, ErrPrimaryIdentityMismatch,
  ErrPoisonedLog

Changes to internal/node/server.go:
  replLog *replnet.Log  →  durableLog *replnet.DurableLog   (primary)
  + stateStore *replnet.ReplicationStateStore               (follower)
  + syncInProgress atomic.Bool                              (concurrent-sync guard)
  Open() loads cursor from stateStore.LastAppliedSeq() on startup
  ApplyReplicationEntries() calls stateStore.AdvanceTo() after each batch
  ReplicationEntries() returns *ReplicationGapError when after+1 < firstAvailableSeq
  SyncFromPrimary() returns ErrSyncInProgress if a sync is already in-flight

Changes to internal/node/handlers.go:
  GET  /replication/log   ← returns HTTP 409 + gap struct when cursor behind journal
  POST /replication/sync  ← returns HTTP 409 + gap struct when primary reports gap
  PUT/DELETE /kv/*        ← calls durableLog.Append() instead of replLog.Append()

Journal binary format (replication.journal):
  [4] length uint32 LE  — bytes after this field
  [4] crc32  uint32 LE  — IEEE CRC32 of payload
  [8] seq    uint64 LE  — monotonically increasing, starts at 1; 0 and MaxUint64 rejected on replay
  [1] op     uint8      — 1=put, 2=delete
  [4] keyLen uint32 LE
  [4] valLen uint32 LE
  [8] tsNano uint64 LE
  [keyLen] key bytes
  [valLen] val bytes

Durability: each Append does write → fsync → index update. On write/sync failure, the file is
truncated back to the pre-write offset and the truncation is synced. If rollback fails (truncate,
seek, or sync), the log is marked poisoned and all future Appends return ErrPoisonedLog.

Cursor persistence (replication_state.json) — versioned, identity-bound:
  {
    "version": 1,
    "follower_node_id": "follower-1",
    "primary_url": "http://primary:8080",
    "last_applied_seq": 42,
    "updated_at": "2026-06-13T12:00:00.000000000Z",
    "checksum": 1234567890
  }
  checksum = CRC32 over all fields except itself (version, follower_node_id, primary_url,
             last_applied_seq, updated_at)
  atomic write: write temp → fsync → rename
  directory fsync: best-effort after rename (errors silently ignored; see design doc Q5)
  identity binding: FollowerNodeID and PrimaryURL mismatch → ErrFollowerIdentityMismatch /
                    ErrPrimaryIdentityMismatch; never silently reset to zero
```

**Scope:** Durable state only. Replication is still pull-based and operator-triggered.
- No Raft, no consensus, no quorum, no automatic failover, no background sync loop.
- Primary journal is never truncated in Phase 26 (no compaction of journal).
- Gap detection is implemented but only triggers if the journal file is externally deleted/truncated.
- Crash window: engine.Put before journal.Append — a mutation visible in engine but not in the
  replication log if the process crashes between the two. Journal entries ⊆ engine state.
- `docs/REPLICATION_DURABILITY_DESIGN.md` documents all 12 design decisions.
