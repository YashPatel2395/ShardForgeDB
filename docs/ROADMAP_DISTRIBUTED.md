# ShardForgeDB — Distributed Roadmap

## Historical Statement

Phases 1–28 produced `v0.5.0-portfolio`, released 2026-06-22 (git tag `v0.5.0-portfolio`, squash commit `4fca00819ec75e24ca2b3fa58741178540b2ae8d` on main). All 28 phases are merged, validated, and tagged. Phase 29 begins the formally gated distributed v1.0 program.

---

## Current State (v0.5.0-portfolio baseline)

| Component | Status |
|---|---|
| WAL, MemTable, SSTable, Bloom filter | Implemented |
| Single-node LSM-tree Engine | Implemented |
| Manual full compaction | Implemented |
| Exact vector search (cosine / L2 / dot) | Implemented |
| Local sharding simulation | Implemented (in-process only) |
| Local leader/follower replication simulation | Implemented (in-process only) |
| Local HTTP dashboard | Implemented (local only) |
| Real networked HTTP node runtime | Implemented (`internal/node`) |
| Client-side consistent-hash routing gateway | Implemented (`internal/gateway`) |
| Stateless HTTP routing proxy | Implemented (`internal/proxy`) |
| Static cluster metadata | Implemented (`internal/cluster`) |
| Explicit pull-based read replicas with durable state | Implemented (`internal/replnet`) |
| Automatic background pull replication with lag tracking | Implemented (`internal/node`) |
| Operator-controlled manual promotion and controlled failover | Implemented (`internal/node`) |
| Ops simulation (health, failure sim, rebalance plan) | Implemented (`internal/ops`) |
| 1292 race-safe tests | Implemented |

**Not yet implemented (required for distributed v1.0 claims):**
- Raft consensus, leader election, quorum writes, automatic failover
- Real distributed sharding with migration
- Background compaction, block cache
- HNSW / approximate nearest-neighbor vector search
- Distributed vector search with global top-k merge
- Prometheus metrics, OpenTelemetry traces, multi-host dashboard

---

## Phase 29 — Distributed Architecture Constitution

**Status: IN PROGRESS (this phase)**

**Goal:** Establish the complete architecture specification, failure model, consistency contract, formal-model skeleton, test strategy, and governance rules for the distributed v1.0 program. No production distributed code is implemented.

**Dependencies:** v0.5.0-portfolio (all 28 phases complete and locked).

**Implementation scope:** Documentation and formal specification files only. No production Go code changes.

**Forbidden claims:** All distributed capability claims remain forbidden (see `docs/CLAIMS.md` Section B and Phase 29 additions).

**Acceptance criteria:**
- `docs/DISTRIBUTED_V1_ARCHITECTURE.md` with 25 sections, claim-unlock matrix, and invariant list
- `docs/DISTRIBUTED_FAILURE_MODEL.md` covering all 32 failure categories
- `docs/CONSISTENCY_MODEL.md` defining all 15 consistency contracts
- `docs/DISTRIBUTED_TEST_STRATEGY.md` with 15 test layers and simulator interfaces
- `docs/PHASE_GOVERNANCE.md` with non-negotiable rules and ACCEPTED/HOLD states
- `formal/ShardForgeRaft.tla` — syntactically valid TLA+ skeleton with TypeInvariant and ElectionSafety
- `formal/ShardForgeRaft.cfg` — valid TLC configuration file
- `formal/README.md` — formal methods program definition
- `docs/CLAIMS.md` updated with Phase 29 section
- `README.md` updated with Distributed v1.0 Program section
- All 1292 existing tests still pass

**Required tests:** No new Go tests in Phase 29. All 1292 existing tests must pass.

**Required demos:** None in Phase 29.

**Required documentation:** All files listed in acceptance criteria.

**Claim unlocked:** "ShardForgeDB has an approved, documented distributed v1.0 architecture, failure model, consistency contract, formal-model skeleton, and gated implementation roadmap."

**Explicit stop condition:** Phase 29 is complete when all acceptance criteria are met and the PR is merged. Phase 30 does not start until Phase 29 is in ACCEPTED state per `docs/PHASE_GOVERNANCE.md`.

---

## Phase 30 — Deterministic Simulation and Fault Injection

**Goal:** Build the deterministic simulation infrastructure that all subsequent distributed phases depend on. Every distributed test must be reproducible by seed.

**Dependencies:** Phase 29 ACCEPTED.

**Implementation scope:**
- `internal/simulator` — VirtualClock, NodeScheduler, NetworkTransport, DiskAbstraction, FaultInjector, EventRecorder, InvariantChecker, Simulator interfaces (as specified in `docs/DISTRIBUTED_TEST_STRATEGY.md`)
- In-process node harness: a simulated node that uses the simulator's interfaces instead of real OS clock/network/disk
- Seed-based replay: given a seed, the exact same sequence of events is reproduced

**Forbidden claims:** No Raft claim. No consensus claim. No distributed data capability claim.

**Acceptance criteria:**
- 100 randomly seeded simulation runs of a trivial 3-node "ping" scenario produce identical event logs when replayed with the same seed
- FaultInjector can crash, pause, partition, and slow-disk any node
- InvariantChecker detects and reports invariant violations
- All existing 1292 tests still pass; simulator tests add >= 50 new tests

**Required tests:** Simulator unit tests (50+); seed-replay property test.

**Required demos:** `make sim-demo` runs a 3-node simulated cluster with injected partition and prints the event log.

**Required documentation:** `docs/SIMULATION_DESIGN.md` documenting the simulator architecture.

**Claim unlocked:** "ShardForgeDB has a deterministic fault-injection simulator with seed-based replay."

**Explicit stop condition:** 100 seeded replay tests pass; all existing tests pass; PR merged and ACCEPTED.

---

## Phase 31 — Raft Persistent State and Transport

**Goal:** Implement the Raft persistent state layer (term, votedFor, log entry WAL) and the inter-node transport layer. No leader election yet.

**Dependencies:** Phase 30 ACCEPTED.

**Correctness obligations:**
- currentTerm must be written to durable storage before any message is sent that uses the new term
- votedFor must be written to durable storage before any vote is granted
- Log entries must be written to durable storage before they are acknowledged to the leader
- Partial tail recovery: a node that crashes mid-write truncates to the last valid CRC-verified record

**Implementation scope:**
- `internal/raft/storage` — durable term store, votedFor store, log WAL
- `internal/raft/transport` — message types (RequestVote, AppendEntries, InstallSnapshot and their responses); in-process transport using simulator; real TCP transport
- Unit tests for all persistence scenarios

**Forbidden claims:** No "Raft" claim yet. No "leader election" claim. No "consensus" claim.

**Acceptance criteria:**
- Write term=5, crash, restart → read term=5 (100 crash-restart iterations)
- Write votedFor=node-2, crash, restart → read votedFor=node-2 (100 iterations)
- Write 1000 log entries, crash at random point, restart → all entries before crash point present; partial tail truncated
- All tests pass (existing 1292 + new >= 60)

**Required tests:** Layer 2 (persistent state); WAL crash recovery property tests (100 iterations).

**Required demos:** None (internal infrastructure phase).

**Required documentation:** `docs/RAFT_STORAGE_DESIGN.md`.

**Claim unlocked:** "ShardForgeDB has durable Raft persistent state (term, votedFor, log) with crash recovery."

**Explicit stop condition:** All persistence tests pass; all existing tests pass; PR merged and ACCEPTED.

---

## Phase 32 — Raft Leader Election

**Goal:** Implement Raft leader election: follower timeout, candidate vote request, vote granting, leader heartbeats.

**Dependencies:** Phase 31 ACCEPTED.

**Correctness obligations:**
- ElectionSafety: at most one leader per term (verified by InvariantChecker in simulator)
- A candidate only wins if its log is at least as up-to-date as the majority's logs
- A node grants at most one vote per term (enforced by durable votedFor)

**Implementation scope:**
- `internal/raft` — Follower, Candidate, Leader state machine; election timeout (using VirtualClock); RequestVote/RequestVoteResponse handling; heartbeat loop
- Simulator integration: 500 seeded runs of 3-node election from cold start with random timeouts and partitions

**Forbidden claims:** No "consensus" claim. No "quorum write" claim. No "automatic failover" claim.

**Acceptance criteria:**
- 500 randomly seeded simulator runs: ElectionSafety never violated
- Cold start from 3 followers → leader elected within 3 election timeout periods
- Kill leader → new leader elected within 3 election timeout periods
- All existing tests pass; new tests >= 80

**Required tests:** Layer 1 (election unit tests); Layer 4 (election integration tests in simulator).

**Required demos:** `make election-demo` — starts 3 simulated nodes, shows election transcript.

**Required documentation:** `docs/RAFT_ELECTION_DESIGN.md`.

**Claim unlocked:** "ShardForgeDB has Raft leader election: at-most-one leader per term, verified by 500 seeded simulator runs."

**Explicit stop condition:** 500 seeded runs pass ElectionSafety; all existing tests pass; PR merged and ACCEPTED.

---

## Phase 33 — Raft Log Replication and Quorum Commit

**Goal:** Implement AppendEntries log replication, majority-committed writes, and commitIndex advancement.

**Dependencies:** Phase 32 ACCEPTED.

**Correctness obligations:**
- LogMatching: all logs with same index+term are identical through that index
- A write is never acknowledged to the client before a majority confirms it in their log
- CommitIndex is never advanced beyond the majority-confirmed index
- Entries from a previous term are only committed via a current-term entry (Raft §5.4.2)

**Implementation scope:**
- `internal/raft` — AppendEntries/AppendEntriesResponse handling; nextIndex/matchIndex tracking; commitIndex advancement; client proposal channel; response channel per proposal
- Integration with shard state machine (basic: engine.Put/Delete)

**Forbidden claims:** No "linearizable reads" claim yet (Phase 34). No "automatic failover" claim (Phase 39).

**Acceptance criteria:**
- Write 1000 entries via leader; kill leader after 500; new leader elected; remaining 500 entries committed on new leader; all 1000 entries present on all replicas
- 500 seeded simulator runs: LogMatching and StateMachineSafety never violated
- Write latency P99 < 10ms in 3-node in-process simulator
- All existing tests pass; new tests >= 100

**Required tests:** Layer 1 (AppendEntries unit); Layer 4 (replication integration); 500 seeded invariant runs.

**Required demos:** `make replication-demo` — 3 simulated nodes, write 100 keys, kill leader, show all keys present on new leader.

**Required documentation:** `docs/RAFT_REPLICATION_DESIGN.md`.

**Claim unlocked:** "ShardForgeDB has majority-committed Raft log replication: writes acknowledged only after N/2+1 nodes confirm."

**Explicit stop condition:** 500 seeded runs pass LogMatching + StateMachineSafety; all existing tests pass; PR merged and ACCEPTED.

---

## Phase 34 — Linearizable Reads and Client Idempotency

**Goal:** Implement the read-index protocol for linearizable leader reads and the client idempotency layer for safe retries.

**Dependencies:** Phase 33 ACCEPTED.

**Correctness obligations:**
- A linearizable read never returns a value that was not committed as of the time the read was processed
- A stale leader cannot serve a linearizable read without first confirming it is still the leader
- A duplicate request (same client ID + request ID) never causes double-apply

**Implementation scope:**
- `internal/raft` — read-index protocol: leader broadcasts heartbeat and waits for majority response before serving read; applies read at the confirmed commitIndex
- `internal/raft/client` — client ID + request ID tracking; last-completed-result cache per client; idempotency window (configurable)
- Follower reads: client sends min-applied-index; follower checks its appliedIndex before responding

**Forbidden claims:** No "lease reads" claim unless clock accuracy is verified.

**Acceptance criteria:**
- 500 seeded runs: linearizable reads never return uncommitted values; never return rolled-back values
- Duplicate request test: send same request 100 times; state machine applies once; all 100 return identical result
- Retry after timeout test: write committed before timeout; retry returns same result without re-applying
- All existing tests pass; new tests >= 80

**Required tests:** Layer 3 (idempotency); Layer 4 (linearizable reads); read-your-writes session test.

**Required demos:** `make linearizable-read-demo` — 3 nodes, concurrent writes and reads, verify linearizability.

**Required documentation:** `docs/LINEARIZABLE_READ_DESIGN.md`.

**Claim unlocked:** "ShardForgeDB serves linearizable reads via read-index protocol; client idempotency prevents double-apply on retry."

**Explicit stop condition:** 500 seeded linearizable read runs pass; idempotency verified; all existing tests pass; PR merged and ACCEPTED.

---

## Phase 35 — Snapshots, InstallSnapshot, and Log Compaction

**Goal:** Implement periodic snapshots, the InstallSnapshot RPC for lagging followers, and log compaction after snapshots.

**Dependencies:** Phase 34 ACCEPTED.

**Correctness obligations:**
- A snapshot is never taken beyond the current applied index
- Log entries below the snapshot's last-included-index are only truncated after the snapshot is durably written
- A snapshot is installed atomically: the old state is replaced by the new state in a single operation
- Snapshot corruption is detected before installation (CRC check)

**Implementation scope:**
- `internal/raft` — snapshot creation, InstallSnapshot RPC, log truncation after snapshot
- `internal/shard_sm` — state machine snapshot: serialize all engine state to byte slice; restore from byte slice

**Forbidden claims:** No "zero-downtime snapshot" claim without measuring pause time.

**Acceptance criteria:**
- Write 10,000 entries → snapshot taken every 1,000 entries → WAL size stays bounded
- Lagging follower falls behind by 2,000 entries → receives InstallSnapshot → state matches leader
- 100 seeded crash-during-snapshot runs: no data loss; restart always recovers correctly
- All existing tests pass; new tests >= 70

**Required tests:** Layer 5 (snapshot + compaction).

**Required demos:** `make snapshot-demo` — write 10,000 keys, show WAL stays bounded, show slow follower recovery.

**Required documentation:** `docs/SNAPSHOT_DESIGN.md`.

**Claim unlocked:** "ShardForgeDB has periodic snapshots, InstallSnapshot for lagging followers, and bounded WAL via log compaction."

**Explicit stop condition:** 100 seeded snapshot runs pass; all existing tests pass; PR merged and ACCEPTED.

---

## Phase 36 — Joint Consensus and Membership Changes

**Goal:** Implement joint consensus for safe cluster membership changes (adding and removing nodes).

**Dependencies:** Phase 35 ACCEPTED.

**Correctness obligations:**
- During a membership change, both the old majority and the new majority must commit entries
- At most one membership change may be pending at a time
- A membership change that is interrupted (crash during joint consensus) leaves the cluster in a safe state (either all-old or all-new configuration)

**Implementation scope:**
- `internal/raft` — joint consensus: C_old,new configuration entry; majority computed from both sets; new-only configuration entry; validation rules

**Forbidden claims:** No "dynamic auto-scaling" claim. No "gossip" claim.

**Acceptance criteria:**
- Add node to 3-node cluster → writes continue during transition → 4-node cluster operational
- Remove node from 3-node cluster → writes continue → 2-node cluster operational
- Crash during joint consensus → restart → cluster recovers to consistent configuration
- 100 seeded membership change runs: no split-brain, no data loss
- All existing tests pass; new tests >= 60

**Required tests:** Layer 6 (membership changes).

**Required demos:** `make membership-demo` — show add-node and remove-node with concurrent writes.

**Required documentation:** `docs/MEMBERSHIP_DESIGN.md`.

**Claim unlocked:** "ShardForgeDB has safe cluster membership changes via joint consensus."

**Explicit stop condition:** 100 seeded membership runs pass; all existing tests pass; PR merged and ACCEPTED.

---

## Phase 37 — Replicated Shard State Machine

**Goal:** Connect the Raft log to the Engine-backed state machine for a real shard. Writes go through Raft; reads use the read-index protocol.

**Dependencies:** Phase 36 ACCEPTED.

**Correctness obligations:**
- The same sequence of committed log entries, applied to the same initial state machine, produces the same final state on every replica
- A deleted key is never returned by a subsequent read (tombstone is applied deterministically)
- Vector upserts and deletes are applied deterministically in log order

**Implementation scope:**
- `internal/shard_sm` — Apply(entry) for Put, Delete, VectorUpsert, VectorDelete; Snapshot() and RestoreSnapshot() for the full engine state
- Wire into `internal/raft` apply goroutine

**Forbidden claims:** No "distributed sharding" claim (that requires Phase 38–39). No "automatic failover at shard level" claim (Phase 39).

**Acceptance criteria:**
- 3-node shard Raft group: write 10,000 keys; read all back via linearizable reads; all values correct on all replicas
- Kill replica; restart; verify state matches other replicas (snapshot or log replay)
- All existing tests pass; new tests >= 80

**Required tests:** Layer 3 (state machine); Layer 4 (replicated shard integration).

**Required demos:** `make shard-sm-demo` — 3-node shard with real Engine, write/read/kill/restart.

**Required documentation:** `docs/SHARD_SM_DESIGN.md`.

**Claim unlocked:** "ShardForgeDB has a replicated shard state machine backed by the LSM-tree engine."

**Explicit stop condition:** Shard state machine passes all tests; all existing tests pass; PR merged and ACCEPTED.

---

## Phase 38 — Metadata Raft Group

**Goal:** Implement the metadata Raft group that manages shard assignment, cluster membership, and shard epoch table.

**Dependencies:** Phase 37 ACCEPTED.

**Correctness obligations:**
- Shard epoch is strictly monotonically increasing; no two assignments share the same epoch
- A stale epoch write is always rejected by the data shard
- The metadata group's committed assignment is the authoritative source of truth for all routers

**Implementation scope:**
- `internal/meta` — metadata Raft group; shard assignment state machine; epoch table; routing table version
- Epoch fencing in `internal/shard_sm` — reject entries with stale epoch

**Forbidden claims:** No "automatic shard rebalancing" claim. No "shard migration" claim (Phase 40).

**Acceptance criteria:**
- Shard assignment stored in metadata group → survives metadata group leader failure
- Epoch incremented on assignment change → stale-epoch writes rejected
- 50 seeded metadata group failover scenarios: no assignment loss
- All existing tests pass; new tests >= 60

**Required tests:** Layer 7 (metadata group).

**Required demos:** `make metadata-demo` — show shard assignment, epoch fencing, metadata leader failover.

**Required documentation:** `docs/METADATA_GROUP_DESIGN.md`.

**Claim unlocked:** "ShardForgeDB has a metadata Raft group managing shard assignments and epoch fencing."

**Explicit stop condition:** Metadata group tests pass; epoch fencing verified; all existing tests pass; PR merged and ACCEPTED.

---

## Phase 39 — Leader-Aware Routing and Automatic Failover

**Goal:** Implement the leader-aware router that routes writes to the current shard leader and automatically retries after leader changes.

**Dependencies:** Phase 38 ACCEPTED.

**Correctness obligations:**
- The router never silently drops a write; it either commits or returns an error
- A retry after NOT_LEADER uses the same idempotency key and produces the same result
- The router does not route to a stale leader indefinitely; it refreshes its cache on NOT_LEADER

**Implementation scope:**
- `internal/router` — leader-aware routing ring; cache of shard → leader; NOT_LEADER redirect handling; cache refresh from metadata group; idempotent retry

**Forbidden claims:** No "shard migration" claim (Phase 40). No "dynamic resharding" claim (Phase 40).

**Acceptance criteria:**
- Write → leader fails during commit → new leader elected → idempotent retry succeeds → value committed once
- 200 seeded router + failover runs: no silent write loss; no double-apply
- All existing tests pass; new tests >= 70

**Required tests:** Layer 8 (router + failover).

**Required demos:** `make router-failover-demo` — write under leader failure, show automatic retry and commit.

**Required documentation:** `docs/ROUTER_DESIGN.md`.

**Claim unlocked:** "ShardForgeDB has leader-aware routing with automatic failover: writes retry transparently on leader change."

**Explicit stop condition:** 200 seeded router runs pass; all existing tests pass; PR merged and ACCEPTED.

---

## Phase 40 — Live Shard Migration and Resharding

**Goal:** Implement live shard migration with the five-stage protocol (Plan, Prepare, Snapshot, Catchup, Cutover) and epoch-based fencing.

**Dependencies:** Phase 39 ACCEPTED.

**Correctness obligations:**
- At no point during migration are there zero live replicas capable of serving the shard
- The cutover is atomic in the metadata group; no window where both source and destination serve without fencing
- An interrupted migration at any stage leaves the cluster in a safe, rollback-able state

**Implementation scope:**
- `internal/migration` — five-stage migration coordinator; snapshot streaming; learner join protocol; cutover atomic metadata commit; rollback procedures

**Forbidden claims:** No "automatic rebalancing" claim (migration is always operator or metadata-group triggered, not fully automatic).

**Acceptance criteria:**
- Migration completes correctly under no faults
- Migration interrupted at each stage → rollback → cluster is operational → retry → migration completes
- Write during migration → committed on correct replica set
- 100 seeded migration + fault injection runs: no data loss; no stale-epoch commits
- All existing tests pass; new tests >= 100

**Required tests:** Layer 9 (migration with fault injection).

**Required demos:** `make migration-demo` — live migration of a shard with concurrent writes.

**Required documentation:** `docs/MIGRATION_DESIGN.md`.

**Claim unlocked:** "ShardForgeDB supports live shard migration with epoch fencing and fault-tolerant five-stage protocol."

**Explicit stop condition:** 100 seeded migration runs pass; all existing tests pass; PR merged and ACCEPTED.

---

## Phase 41 — Background Compaction and Block Cache

**Goal:** Implement automatic background compaction triggered by SSTable count and an in-memory LRU block cache per node.

**Dependencies:** Phase 40 ACCEPTED (or may run in parallel with Phase 40 if scoped independently).

**Correctness obligations:**
- Background compaction never drops committed data; tombstones are only dropped when no older version exists in a lower level
- Compaction does not corrupt the Raft log or shard state machine state
- Cache eviction never causes data loss; data is always available on disk

**Implementation scope:**
- `internal/compaction` — background goroutine with threshold-based trigger; level-aware or full compaction
- `internal/cache` — LRU block cache; configurable size; cache-key: (file path, block offset); hit/miss stats

**Forbidden claims:** No "leveled compaction" claim unless leveled is implemented.

**Acceptance criteria:**
- Write 50,000 keys → SSTable count never exceeds threshold + 1 (background compaction fires)
- Cache hit rate > 80% on repeated reads in benchmark
- Compaction correctness: all 50,000 keys readable after compaction
- All existing tests pass; new tests >= 60

**Required tests:** Layer 10 (compaction + cache).

**Required demos:** `make compaction-demo` — write many keys, show SSTable count staying bounded.

**Required documentation:** Update `docs/ARCHITECTURE.md` with compaction + cache components.

**Claim unlocked:** "ShardForgeDB has background compaction (auto-triggered by SSTable count) and an LRU block cache."

**Explicit stop condition:** Compaction + cache tests pass; all existing tests pass; PR merged and ACCEPTED.

---

## Phase 42 — Persistent HNSW ANN Index

**Goal:** Implement a persistent HNSW approximate nearest-neighbor index per shard, with checkpointing and log-replay recovery.

**Dependencies:** Phase 41 ACCEPTED.

**Correctness obligations:**
- Deleted vectors never appear in search results
- HNSW checkpoint + log-replay produces the same index state as continuous in-memory operation
- HNSW recall@10 >= 95% on standard benchmark datasets

**Implementation scope:**
- `internal/hnsw` — HNSW graph: node insertion, connection, search; graph serialization (checkpoint); log-replay recovery; concurrency: reads concurrent with writes via read-write lock

**Forbidden claims:** No "exact search" claim for HNSW results (HNSW is approximate). Recall must be stated explicitly.

**Acceptance criteria:**
- HNSW recall@10 >= 95% on 100,000-vector benchmark (cosine metric)
- Checkpoint + restart: search results within 1% of pre-restart results
- Deleted vector: never appears in search results after delete is applied
- All existing tests pass; new tests >= 80

**Required tests:** Layer 11 (HNSW unit tests); recall benchmark.

**Required demos:** `make hnsw-demo` — insert 10,000 vectors, search, checkpoint, restart, verify recall.

**Required documentation:** `docs/HNSW_DESIGN.md`.

**Claim unlocked:** "ShardForgeDB has a persistent HNSW approximate nearest-neighbor index per shard (recall@10 >= 95%)."

**Explicit stop condition:** HNSW recall benchmark passes; all existing tests pass; PR merged and ACCEPTED.

---

## Phase 43 — Distributed Vector Search

**Goal:** Implement distributed vector search: fan-out to all shards, per-shard HNSW (or exact) search, global top-k merge.

**Dependencies:** Phase 42 ACCEPTED.

**Correctness obligations:**
- The global top-k result is always a subset of the union of all shard top-k' results
- A partial result (some shards unavailable) is never returned as a complete result
- Vector deduplication during migration is always performed before re-ranking

**Implementation scope:**
- `internal/vector_coord` — query fan-out; per-shard k' = k * 2x; result merge and re-rank; partial result indication

**Forbidden claims:** No "exact distributed search" claim (HNSW is approximate). Recall must be stated explicitly.

**Acceptance criteria:**
- 3-shard cluster: insert 30,000 vectors (10,000 per shard); search top-10; distributed result matches brute-force top-10 with recall >= 95%
- Partial shard failure: partial result returned with explicit indication; never silent
- All existing tests pass; new tests >= 60

**Required tests:** Layer 11 (distributed vector search).

**Required demos:** `make vector-search-demo` — 3-shard vector search with recall verification.

**Required documentation:** `docs/DISTRIBUTED_VECTOR_DESIGN.md`.

**Claim unlocked:** "ShardForgeDB supports distributed ANN vector search: fan-out to all shards, global top-k merge, recall >= 95%."

**Explicit stop condition:** Distributed recall benchmark passes; partial failure indication verified; all existing tests pass; PR merged and ACCEPTED.

---

## Phase 44 — Prometheus, OpenTelemetry, and Dashboard

**Goal:** Expose Prometheus metrics on every node, instrument distributed traces via OpenTelemetry, and serve a multi-host dashboard from the proxy.

**Dependencies:** Phase 43 ACCEPTED.

**Implementation scope:**
- `internal/metrics` — Prometheus exposition format `/metrics` endpoint; all metrics listed in `docs/DISTRIBUTED_V1_ARCHITECTURE.md` §16.1
- `internal/telemetry` — OpenTelemetry OTLP export; trace spans for write path, read path, vector search
- Dashboard update: polls `/metrics` and `/healthz` on all configured nodes; renders node health, shard status, replication lag, compaction activity

**Forbidden claims:** No "real-time monitoring" claim without sub-second polling interval.

**Acceptance criteria:**
- `/metrics` returns valid Prometheus text format; all required metrics present
- Distributed trace spans visible in OTLP collector for a write request
- Dashboard shows all 3 nodes, their shard assignments, and lag metrics
- All existing tests pass; new tests >= 40

**Required tests:** Metrics endpoint tests; telemetry sampling tests.

**Required demos:** `make observability-demo` — start 3-node cluster, show Prometheus scrape, show dashboard.

**Required documentation:** Update `docs/ARCHITECTURE.md` with observability stack.

**Claim unlocked:** "ShardForgeDB exposes Prometheus metrics and OpenTelemetry distributed traces; multi-host dashboard available."

**Explicit stop condition:** Metrics + traces verified; all existing tests pass; PR merged and ACCEPTED.

---

## Phase 45 — Security and Multi-Host Operations

**Goal:** Add mTLS for all inter-node communication, authentication for client requests, and multi-host deployment documentation.

**Dependencies:** Phase 44 ACCEPTED.

**Implementation scope:**
- mTLS: certificate loading and validation for all inter-node RPCs and HTTP connections
- Client authentication: bearer token or certificate-based auth for client HTTP requests
- Multi-host deployment guide: `docs/MULTI_HOST_OPS.md`

**Forbidden claims:** No "production security audit" claim without third-party review.

**Acceptance criteria:**
- Inter-node communication without valid certificate rejected
- Client request without valid auth rejected
- 3-node cluster starts with mTLS enabled; all operations succeed
- All existing tests pass; new tests >= 30

**Required tests:** mTLS handshake test; auth rejection test.

**Required demos:** `make security-demo` — start 3-node cluster with mTLS, show rejection of unauthorized request.

**Required documentation:** `docs/MULTI_HOST_OPS.md`.

**Claim unlocked:** "ShardForgeDB supports mTLS for inter-node communication and token-based client authentication."

**Explicit stop condition:** Security tests pass; all existing tests pass; PR merged and ACCEPTED.

---

## Phase 46 — Chaos, Linearizability, and v1.0 Release Audit

**Goal:** Run the full chaos test suite, verify linearizability with a model checker, document all results, and release `v1.0.0-distributed`.

**Dependencies:** Phase 45 ACCEPTED.

**Implementation scope:**
- Chaos test harness (Layer 14): 10 runs of 10 minutes each with random failures every 30 seconds
- Linearizability checker (Layer 13): 500 seeded runs of the full fault injection suite fed to Porcupine (or equivalent)
- Performance benchmarks (Layer 15): write/read/vector throughput and latency documented
- `docs/FINAL_DISTRIBUTED_REPORT.md`: full audit of all 18 phases (29–46) with test counts and results
- GitHub Release `v1.0.0-distributed` with release notes

**Correctness obligations:**
- No linearizability violation in any of the 500 seeded runs
- No linearizability violation in any of the 10 chaos runs
- All forbidden claims from `docs/CLAIMS.md` Section B are absent from all documentation (verified by grep)

**Acceptance criteria:**
- 500 seeded linearizability checker runs: 0 violations
- 10 chaos runs of 10 minutes each: 0 linearizability violations, 0 committed data loss
- All benchmark results documented
- `docs/CLAIMS.md` Section A updated with all distributed claims
- `make release-check` passes
- All existing tests pass; total test count >= 2000

**Required tests:** Layer 13 (linearizability); Layer 14 (chaos); Layer 15 (benchmarks).

**Required demos:** Full 3-node cluster demo with chaos injection and recovery.

**Required documentation:** `docs/FINAL_DISTRIBUTED_REPORT.md`.

**Claim unlocked:** "ShardForgeDB v1.0-distributed: verified linearizable key-value and approximate vector search on a Raft-replicated distributed engine. Verified by 500 linearizability checker runs and 10 chaos runs."

**Explicit stop condition:** All linearizability and chaos tests pass; `v1.0.0-distributed` tag pushed; PR merged and ACCEPTED.

---

## Rules for All Phases (29–46)

1. No phase may claim a feature until it is tested, benchmarked, and reviewed (see `docs/PHASE_GOVERNANCE.md`).
2. Every phase must update `docs/CLAIMS.md` before its PR is merged.
3. `go test -race -count=1 ./...` must pass with at least the previous phase's test count.
4. No phase may remove or weaken any existing test.
5. The v0.5.0-portfolio tag is immutable.
6. If Raft is not implemented, Raft must never appear in any claim.
7. If HNSW is not implemented, ANN / HNSW / IVF must never appear in any claim.
8. Phase governance rules in `docs/PHASE_GOVERNANCE.md` supersede any other document.
