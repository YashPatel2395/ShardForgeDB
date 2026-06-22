# ShardForgeDB — Distributed v1 Architecture

**Phase 29 — Architecture and Specification Only. No implementation in this phase.**

This document defines the complete target architecture for ShardForgeDB v1.0-distributed. Every section describes intent and invariants. No production distributed-system code is implemented in Phase 29. Implementation begins in Phase 30 and is gated by the Phase Governance rules (`docs/PHASE_GOVERNANCE.md`).

---

## 1. Overview and Design Principles

ShardForgeDB v1.0-distributed is a real distributed key-value and vector-search database engine. It is built on three core principles:

1. **Correctness before availability.** Safety invariants (no committed data loss, no split-brain) are never sacrificed for availability. When safety and availability conflict, availability is sacrificed and an explicit error is returned.
2. **Explainability at every layer.** Every design decision, trade-off, and invariant is documented alongside the code that implements it. Every claim is audited before it is published.
3. **Deterministic testability.** The distributed engine must be fully testable via a deterministic simulator before any real networking is wired. A random seed must be sufficient to reproduce any failure sequence.

---

## 2. Topology

### 2.1 Metadata Raft Group

A dedicated metadata Raft group manages the cluster membership, shard-to-node assignment, and shard epoch table. It runs on a fixed set of nodes (default: 3 or 5) and is independent of any data shard group.

- **Owner component:** `internal/meta` (Phase 38)
- **Persisted state:** current term, votedFor, Raft log (membership entries, shard assignments, epoch table)
- **Network messages:** RequestVote, RequestVoteResponse, AppendEntries, AppendEntriesResponse, InstallSnapshot, InstallSnapshotResponse
- **Concurrency boundary:** single goroutine per Raft state machine; all external calls go through a channel; snapshot installation pauses the state machine
- **Crash behavior:** surviving majority continues; crashed node recovers by replaying its log or installing a snapshot
- **Restart behavior:** term and votedFor reloaded from durable storage before any message is processed
- **Invariants:** at most one leader per term (ElectionSafety); committed entries never rolled back (LogMatching, LeaderCompleteness, StateMachineSafety)
- **Observability:** `raft_term`, `raft_role`, `raft_commit_index`, `raft_applied_index`, `raft_leader_id` metrics; leader-change trace events
- **Testing strategy:** deterministic simulator (Phase 30), unit tests per Raft action (Phases 31–36)

### 2.2 Per-Shard Raft Groups

Each data shard is backed by a dedicated Raft group with a default of three replicas. Replicas are placed on distinct physical nodes to tolerate single-node failures.

- **Owner component:** `internal/raft` + `internal/shard_sm` (Phases 31–37)
- **Persisted state:** currentTerm, votedFor, Raft log (Put/Delete/VectorUpsert entries), commitIndex, applied state machine snapshot
- **Network messages:** same Raft RPCs as the metadata group
- **Concurrency boundary:** one Raft goroutine per shard per node; apply goroutine reads from a committed-entries channel
- **Crash behavior:** surviving majority continues serving; crashed replica recovers via log replay or InstallSnapshot from leader
- **Restart behavior:** term and votedFor loaded before any election; commitIndex and appliedIndex recovered from snapshot + log replay
- **Invariants:** all committed entries eventually applied to every live replica in order; applied state is identical across replicas for the same sequence of committed entries
- **Observability:** per-shard `raft_commit_index`, `raft_applied_index`, `raft_lag_entries`, snapshot size, snapshot duration
- **Testing strategy:** deterministic simulator per shard group; Jepsen-style linearizability checker (Phase 46)

### 2.3 Replica Count and Placement

- Default: 3 replicas per shard (tolerates 1 failure)
- Optional: 5 replicas (tolerates 2 failures)
- Placement: metadata group assigns replicas to nodes; no two replicas of the same shard on the same physical host (enforced by placement policy in the metadata state machine)
- Failure of minority: cluster continues serving reads and writes
- Failure of majority: cluster stops accepting writes; reads from surviving minority may serve stale data under explicit follower-read mode

---

## 3. Write Path

### 3.1 Acknowledged vs Committed Writes

- A write is **proposed** when the client sends it to the shard leader.
- A write is **committed** when a Raft majority has durably appended the entry to their logs.
- A write is **applied** when the leader's state machine has executed the write against the engine.
- The leader responds to the client only after the write is **committed** (majority-committed writes).

### 3.2 Write Flow

```
Client PUT /kv/{key}
  → Router: resolve shard leader via metadata cache
  → Leader node: propose entry to Raft
      → Append to local log (WAL)
      → Broadcast AppendEntries to followers
      → Wait for majority acknowledgment
      → Advance commitIndex
      → Apply to state machine (engine.Put)
  → Respond 200 OK with committed sequence number
```

### 3.3 Concurrency Boundary

The leader's Raft goroutine is the only writer to the Raft log. Parallel client requests are serialized by appending to a proposal channel. Each proposal carries a response channel. The apply goroutine reads committed entries and writes results back to the response channel. No proposal is acknowledged before commit.

### 3.4 Crash Behavior

If the leader crashes after committing but before responding to the client: the client may retry (with an idempotency key). The idempotent retry is safely rejected by the state machine because the entry is already applied. The client receives a success response on retry.

If the leader crashes before committing: the entry is never committed. A new leader is elected. The client's retry is accepted by the new leader and committed.

---

## 4. Read Path

### 4.1 Linearizable Leader Reads

The leader must confirm it is still the leader before serving a read. It does this by broadcasting a heartbeat (or using a lease mechanism) before returning the value. This prevents a stale leader from serving reads after it has been superseded.

- **Owner component:** `internal/raft` read-index protocol (Phase 34)
- **Invariant:** a linearizable read never returns a value that has not yet been committed
- **Observability:** `raft_read_index_latency_ms`, `raft_stale_read_rejected_total`

### 4.2 Follower Reads

Follower reads are permitted under an explicitly weaker consistency contract: a follower may serve a read for any key whose applied index is >= the client's last-known committed index (read-your-writes consistency). Clients must opt into follower reads explicitly. Follower reads never claim linearizability.

- **Invariant:** follower reads never return a value rolled back by a committed overwrite
- **Contract:** follower reads may return stale values; the staleness bound is the replication lag

### 4.3 Vector Reads

Vector search is a read-only fan-out operation. The coordinator node sends the query to all shard leaders (or all replicas under follower-read mode). Each shard returns its local top-k results. The coordinator merges and re-ranks to produce the global top-k. The consistency level of the merge is determined by the per-shard read mode.

---

## 5. Leader Election

### 5.1 Election Protocol

Standard Raft leader election. When a follower's election timeout fires (150–300ms randomized), it increments its term, transitions to Candidate, votes for itself, and broadcasts RequestVote RPCs. A candidate wins if it receives votes from a majority of nodes in the same term, and its log is at least as up-to-date as the voter's log.

- **Owner component:** `internal/raft` (Phase 32)
- **Invariant:** ElectionSafety — at most one leader per term
- **Persisted state:** currentTerm and votedFor written to durable storage before any vote is granted
- **Observability:** `raft_election_total`, `raft_election_won_total`, `raft_term`

### 5.2 Automatic Failover

When the leader fails, the surviving majority elects a new leader without operator intervention. The new leader immediately serves writes. The old leader, if it recovers, discovers it is in a stale term and steps down to Follower.

- **Invariant:** automatic failover never loses committed data; the new leader's log contains all committed entries from the previous term (LeaderCompleteness)
- **Availability:** during election (typically < 500ms under normal conditions), writes are unavailable

---

## 6. Log Replication

### 6.1 AppendEntries Protocol

The leader sends AppendEntries RPCs to all followers in parallel. Each RPC includes a batch of log entries and the leader's current commitIndex. Followers validate the prevLogIndex and prevLogTerm fields; on mismatch, they reject the RPC and the leader backtracks its nextIndex for that follower.

- **Owner component:** `internal/raft` (Phase 33)
- **Invariant:** LogMatching — if two logs contain an entry with the same index and term, then the logs are identical in all entries up through that index
- **Persisted state:** log entries written to WAL before acknowledgment
- **Observability:** `raft_append_entries_sent_total`, `raft_append_entries_rejected_total`, `raft_log_lag_entries`

### 6.2 Slow Follower

A follower that falls significantly behind the leader's log is sent a snapshot (InstallSnapshot) instead of individual log entries. The snapshot contains the full state machine state at a specific log index. After installing the snapshot, the follower resumes normal log replication.

---

## 7. Snapshots and Log Compaction

### 7.1 Snapshot Creation

Periodic snapshots are created by the apply goroutine when the applied log exceeds a configurable threshold (default: 100,000 entries). The snapshot captures the state machine state (all engine data) at the current applied index, plus the current term and applied index.

- **Owner component:** `internal/raft` + `internal/shard_sm` (Phase 35)
- **Persisted state:** snapshot file + snapshot metadata (last included index, last included term)
- **Crash behavior:** on restart, load the latest valid snapshot, then replay log entries from the snapshot's last-included-index+1
- **Invariant:** a snapshot never truncates entries that are not yet committed

### 7.2 Log Truncation

After a snapshot is durably written, log entries up to and including the snapshot's last-included-index are truncated from the WAL. The truncation is atomic: the snapshot metadata file is written before the WAL truncation.

---

## 8. Membership Changes

### 8.1 Joint Consensus

Membership changes (adding or removing nodes from a Raft group) use the joint consensus protocol to avoid split-brain during the transition. The transition proceeds in two phases: a joint configuration phase (where both the old and new configuration must achieve majority) and a new configuration phase.

- **Owner component:** `internal/raft` (Phase 36)
- **Invariant:** during joint consensus, both the old majority and the new majority must commit entries — no configuration change can create two independent majorities simultaneously
- **Observability:** `raft_membership_change_total`, `raft_membership_change_phase`

### 8.2 Forbidden During Phase 29

Joint consensus implementation is deferred to Phase 36. No membership changes may be claimed before Phase 36 acceptance criteria are met.

---

## 9. Shard State Machine

### 9.1 State Machine Interface

Each shard Raft group applies committed entries to an engine-backed state machine. The state machine interface is:

```
Apply(entry Entry) ApplyResult
Snapshot() ([]byte, error)
RestoreSnapshot([]byte) error
```

- **Owner component:** `internal/shard_sm` (Phase 37)
- **Persisted state:** the engine data directory (WAL + MemTable + SSTables)
- **Concurrency boundary:** Apply is called sequentially by the apply goroutine; Snapshot may be called concurrently with Apply (engine must support concurrent snapshot reads)
- **Invariant:** identical sequences of applied entries produce identical state machine state on all replicas

### 9.2 Idempotency

Each state machine entry carries a client request ID. The state machine tracks the last applied request per client. Duplicate requests (retries) are detected and the previous result is returned without re-applying the write. This is the client idempotency layer (Phase 34).

---

## 10. Metadata Raft Group

### 10.1 Responsibilities

The metadata group maintains:
- Cluster membership: set of live nodes and their addresses
- Shard assignment: mapping of shard ID → replica node set
- Shard epoch table: monotonically increasing epoch per shard (used for fencing)
- Routing table version: cached by routers to detect staleness

### 10.2 Shard Epoch and Fencing

Every shard assignment has a monotonically increasing epoch number. When a shard is migrated or reassigned, the epoch is incremented. All writes and reads to a shard include the epoch number. Nodes reject requests with a stale epoch (< current epoch) or a future epoch (> current epoch + 1 tolerance).

- **Owner component:** `internal/meta` (Phase 38)
- **Invariant:** a stale epoch write is always rejected; a committed write on a new epoch is never lost

---

## 11. Leader-Aware Routing

### 11.1 Router Architecture

The router is a stateless component that maintains a local cache of the routing table (shard → leader node). On a cache hit, the router forwards the request directly to the known shard leader. On a leader-not-found or NOT_LEADER response from a node, the router refreshes its cache from the metadata group and retries.

- **Owner component:** `internal/router` (Phase 39)
- **Network messages:** GET /routing-table (to metadata group); PUT/GET/DELETE /kv/{key} (to shard leader)
- **Observability:** `router_cache_hit_total`, `router_cache_miss_total`, `router_redirect_total`

### 11.2 Automatic Failover at the Routing Layer

When a write fails because the contacted node is not the current leader (it returns NOT_LEADER with an optional leader hint), the router updates its cache and retries against the new leader. This is transparent to the client and does not require operator intervention.

- **Invariant:** the router never silently drops a write; it either commits the write or returns an error to the client
- **Crash behavior:** if the leader crashes between the router's request and the majority commit, the router retries and the new leader eventually commits or rejects the write (with idempotency)

---

## 12. Live Shard Migration

### 12.1 Migration Stages

Live shard migration proceeds in five stages:

1. **Plan:** operator (or metadata group) selects source replica set and destination replica set.
2. **Prepare:** destination nodes create empty engines; metadata group records the migration in progress.
3. **Snapshot transfer:** source leader takes a snapshot and streams it to destination nodes.
4. **Catchup:** destination nodes join the shard Raft group as learners; they apply log entries from the snapshot point until they are caught up (lag < threshold).
5. **Cutover:** metadata group atomically updates the shard assignment, increments the shard epoch, and removes source nodes from the Raft group.

- **Owner component:** `internal/migration` (Phase 40)
- **Invariant:** at no point during migration are there zero live replicas capable of serving the shard
- **Fencing:** the shard epoch is incremented at cutover; source nodes reject future writes with a stale epoch error
- **Crash behavior at each stage:** if migration is interrupted at any stage, the metadata group records the interrupted migration; on recovery, the migration coordinator resumes from the last durable stage or rolls back to the pre-migration state

### 12.2 Migration Interrupted at Every Stage

- **Interrupted at Plan:** no state change occurred; retry is safe.
- **Interrupted at Prepare:** destination engines may exist but are empty; rollback deletes them.
- **Interrupted at Snapshot transfer:** incomplete snapshot on destination; rollback deletes partial snapshot.
- **Interrupted at Catchup:** destination learners are removed from the group; rollback returns to source-only.
- **Interrupted at Cutover:** epoch is not yet incremented; rollback is safe. If epoch was incremented but assignment not updated: impossible by atomic metadata commit.

### 12.3 Router During Migration

During migration, the router may receive responses from either source or destination nodes. The router always follows the shard epoch: it accepts responses from nodes advertising the current epoch and rejects responses from nodes advertising a stale epoch. After cutover, the router updates its cache to the new assignment within one cache TTL (or on the first NOT_LEADER redirect).

---

## 13. Background Compaction and Block Cache

### 13.1 Background Compaction

Automatic compaction is triggered by a background goroutine when the SSTable count for a shard exceeds a configurable threshold (default: 8 SSTables). Compaction is per-shard and runs on the replica that hosts the shard (each replica compacts its own local engine independently, as the state machine is deterministic).

- **Owner component:** `internal/compaction` (Phase 41)
- **Invariant:** compaction never drops committed data; it only merges and removes tombstones for entries that have no older version in a lower level
- **Concurrency boundary:** compaction runs in a dedicated goroutine; it acquires a read lock on the engine during compaction to prevent concurrent flushes from racing with the merge
- **Observability:** `compaction_total`, `compaction_input_tables`, `compaction_output_tables`, `compaction_duration_ms`

### 13.2 Block Cache

An in-memory LRU block cache is maintained per node (shared across all shard replicas on the same node). Hot SSTable data blocks are cached to avoid repeated disk reads. Cache entries are keyed by (SSTable file path, block offset).

- **Owner component:** `internal/cache` (Phase 41)
- **Persisted state:** none (cache is volatile; it is rebuilt from disk on restart)
- **Observability:** `cache_hit_total`, `cache_miss_total`, `cache_eviction_total`, `cache_bytes_used`

---

## 14. Persistent HNSW Approximate Nearest-Neighbor Index

### 14.1 Per-Shard HNSW

Each shard maintains a persistent HNSW index for its vector namespace. The index is built incrementally as vectors are upserted and is checkpointed to disk periodically. On restart, the index is loaded from the checkpoint and updated by replaying log entries from the checkpoint point.

- **Owner component:** `internal/hnsw` (Phase 42)
- **Persisted state:** HNSW graph file (nodes, edges, metadata), checkpoint sequence number
- **Invariant:** the in-memory HNSW graph is always consistent with the applied log up to the latest applied index; a query against the HNSW index never returns a vector that has been deleted from the state machine

### 14.2 HNSW Parameters

- M (number of connections per node): configurable, default 16
- efConstruction (search width during construction): configurable, default 200
- efSearch (search width during query): configurable per-query, default 64
- Distance metrics: cosine, L2, dot product (same as exact search)

---

## 15. Distributed Vector Search

### 15.1 Query Fan-Out

A vector search query is processed as follows:

1. The coordinator node receives the query and the desired k.
2. It determines which shards hold the relevant vector namespace.
3. It fans out the query to each shard's leader (or all replicas under follower-read mode) with a per-shard k' = k * fan-out factor (default 2x) to ensure sufficient candidates for the global merge.
4. Each shard executes its local HNSW search (or exact brute-force if HNSW is not yet built) and returns its top-k' results.
5. The coordinator merges all results, re-ranks by the true distance metric, and returns the global top-k to the client.

- **Owner component:** `internal/vector_coord` (Phase 43)
- **Observability:** `vector_search_fanout_shards`, `vector_search_latency_p99`, `vector_search_merge_latency_ms`

### 15.2 Vector Search During Migration

During a shard migration, the coordinator queries both the source and destination shards if the migration is in the Catchup stage. It deduplicates results by vector ID before re-ranking. During Cutover, the coordinator follows the epoch and queries the new shard assignment.

---

## 16. Prometheus Metrics

### 16.1 Metrics Endpoint

Each node exposes a `/metrics` endpoint in Prometheus exposition format. Metrics are categorized:

- **Raft metrics:** `raft_term`, `raft_role`, `raft_commit_index`, `raft_applied_index`, `raft_leader_changes_total`, `raft_election_total`, `raft_append_entries_total`, `raft_snapshot_total`
- **Storage metrics:** `engine_put_total`, `engine_get_total`, `engine_sstable_count`, `engine_wal_bytes`, `engine_memtable_bytes`
- **Replication metrics:** `raft_lag_entries`, `raft_lag_known`
- **Router metrics:** `router_cache_hit_total`, `router_redirect_total`
- **Compaction metrics:** `compaction_total`, `compaction_duration_ms`
- **Cache metrics:** `cache_hit_total`, `cache_miss_total`
- **Vector metrics:** `vector_search_total`, `vector_search_latency_p99`

- **Owner component:** `internal/metrics` (Phase 44)

### 16.2 OpenTelemetry Traces

Distributed traces span the entire request path: client → router → shard leader → apply goroutine → state machine. Traces are exported in OpenTelemetry format (OTLP) to a configurable collector.

- **Owner component:** `internal/telemetry` (Phase 44)

### 16.3 Multi-Host Dashboard

A dashboard is served by the proxy at `GET /dashboard`. It polls `/metrics` and `/healthz` on all configured nodes and renders:
- Node health table
- Per-shard leader/follower status
- Replication lag per shard
- Compaction activity
- Vector search throughput

---

## 17. Observability Summary

| Layer | Key Metrics | Key Traces |
|---|---|---|
| Raft | term, role, commit index, election count | LeaderElected, EntryCommitted |
| Storage | put/get latency, SSTable count, WAL bytes | WAL_APPEND, SSTABLE_HIT |
| Replication | lag entries, lag known | ReplicationPull, SnapshotInstall |
| Router | cache hit rate, redirect count | RouteResolved, LeaderRedirect |
| Compaction | compaction count, duration | CompactionStarted, CompactionDone |
| Cache | hit rate, eviction count | CacheHit, CacheMiss |
| Vector | search latency, fanout shards | VectorFanout, VectorMerge |
| Migration | stage, epoch | MigrationStageChange, CutoverComplete |

---

## 18. Failure Categories Summary

See `docs/DISTRIBUTED_FAILURE_MODEL.md` for the full failure analysis.

---

## 19. Consistency Model Summary

See `docs/CONSISTENCY_MODEL.md` for the full consistency contract.

---

## 20. Testing Strategy

See `docs/DISTRIBUTED_TEST_STRATEGY.md` for all 15 test layers.

---

## 21. Claim-Unlock Matrix

The following table defines which Phase unlocks each distributed capability claim:

| Capability | Phase that unlocks claim |
|---|---|
| Deterministic simulator and fault injection infrastructure | Phase 30 |
| Raft persistent state (term, votedFor, log durability) | Phase 31 |
| Leader election (automatic, term-based) | Phase 32 |
| Log replication and quorum commit (majority-committed writes) | Phase 33 |
| Linearizable reads; client idempotency | Phase 34 |
| Snapshots, InstallSnapshot, and log compaction | Phase 35 |
| Joint consensus and safe membership changes | Phase 36 |
| Replicated shard state machine | Phase 37 |
| Metadata Raft group (shard assignment, epoch table) | Phase 38 |
| Leader-aware routing and automatic failover at router | Phase 39 |
| Live shard migration and resharding | Phase 40 |
| Background compaction and block cache | Phase 41 |
| Persistent HNSW approximate nearest-neighbor index | Phase 42 |
| Distributed vector search with global top-k merge | Phase 43 |
| Prometheus metrics endpoint; OpenTelemetry distributed traces | Phase 44 |
| Multi-host dashboard | Phase 44 |
| Security (mTLS, auth) and multi-host operations | Phase 45 |
| Chaos audit, linearizability proof, v1.0 release | Phase 46 |

No capability in this table may be claimed before the specified phase's acceptance criteria are met.

---

## 22. Forbidden Claims in Phase 29

Phase 29 establishes architecture and specification only. The following claims are **explicitly forbidden** until the specified phases are completed:

- "Raft" or "Raft consensus" — Phase 31–33
- "Majority-committed writes" — Phase 33
- "Linearizable reads" — Phase 34
- "Automatic leader election" — Phase 32
- "Automatic failover" — Phase 39
- "Dynamic membership changes" — Phase 36
- "Live shard migration" — Phase 40
- "Background compaction" — Phase 41
- "Block cache" — Phase 41
- "HNSW" or "approximate nearest-neighbor" — Phase 42
- "Distributed vector search" — Phase 43
- "Prometheus metrics endpoint" — Phase 44
- "OpenTelemetry distributed tracing" — Phase 44
- "Multi-host dashboard" — Phase 44

---

## 23. Invariants (Full List)

| Invariant | Category |
|---|---|
| ElectionSafety: at most one leader per term | Raft |
| LogMatching: identical index+term implies identical prefix | Raft |
| LeaderCompleteness: leader has all committed entries | Raft |
| StateMachineSafety: all replicas apply the same entry at the same log index | Raft |
| EpochMonotonicity: shard epoch is strictly increasing | Migration |
| EpochFencing: stale epoch writes are rejected | Migration |
| CommittedDataDurability: no committed entry is ever lost | Raft + Storage |
| MinorityCannotCommit: a write requires majority agreement | Raft |
| ClientIdempotency: duplicate request IDs produce the same result | Client |
| FollowerReadSafety: follower reads never return rolled-back values | Read |
| MigrationAtomicity: cutover is atomic in the metadata state machine | Migration |
| NoSplitBrain: never two simultaneous leaders for the same shard in the same term | Raft |

---

## 24. Architecture Decisions Not Yet Made

The following architecture decisions are deferred to their respective implementation phases:

1. WAL format for the Raft log (Phase 31) — separate file vs. reuse existing WAL
2. Snapshot compression algorithm (Phase 35) — snappy vs. zstd vs. none
3. Leader lease vs. read-index for linearizable reads (Phase 34) — trade-off between latency and correctness under clock skew
4. HNSW on-disk format (Phase 42) — custom binary vs. HNSWlib-compatible format
5. Block cache partitioning policy (Phase 41) — per-shard vs. shared node-wide
6. mTLS certificate management strategy (Phase 45) — self-signed vs. PKI

---

## 25. Document History

| Version | Date | Phase | Changes |
|---|---|---|---|
| 1.0 | 2026-06-22 | Phase 29 | Initial architecture specification. Architecture and specification only; no implementation. |
