# ShardForgeDB — Distributed Roadmap

**Phase 28 — Manual Promotion and Controlled Failover edition (final completed phase)**

This document defines the phases required to evolve ShardForgeDB from its current explainable single-node + HTTP-node foundation into a real distributed database. Each phase is narrow, honest, and fully testable before the next begins.

The final target: **ShardForgeDB — a real explainable distributed database engine for key-value and vector search workloads.**

---

## Current state (Phases 1–28 complete and locked — no additional phase currently authorized)

All 28 approved phases are merged to main at `b6965e839baf1ddaeeb6e64a69c781775d5e1396` (2026-06-18). Phase 29 has not started.

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
| Explicit pull-based read replicas with durable state | Implemented (`internal/replnet` — DurableLog, ReplicationStateStore, gap detection) |
| Automatic background pull replication with lag tracking | Implemented (`internal/node` — backgroundSyncWorker, configurable interval/backoff/jitter, lag_entries/lag_known) |
| Operator-controlled manual promotion and controlled failover | Implemented (`internal/node` — quiesce/promote endpoints, promotionBarrier, replicationMutationMu, crash-consistent 2-phase commit, quiesce-intent record, 98 net-new tests, 32-check demo) |
| Ops simulation (health, failure sim, rebalance plan) | Implemented (`internal/ops`) |

**Not yet implemented (required for real distributed claims):**
- Consensus, Raft, quorum, automatic failover
- Real distributed sharding (data actually split across nodes)
- Distributed transactions
- ANN / HNSW / IVF vector search
- Background compaction
- Real cluster dashboard

---

## Phase 15 — Runtime Operation Trace Mode

**Goal:** Make every key storage operation internally traceable. Produce real step-by-step execution traces from actual code paths, not fabricated display text.

### What must be implemented

- Wire `internal/trace` types (created in Phase 21/14) into Engine, MemTable, SSTable, WAL, Bloom, and Vector
- `ExplainGet(key)` → returns a `Trace` with steps: WAL-not-searched, MemTable-check, Bloom-check per SSTable, SSTable-read
- `ExplainPut(key, value)` → returns a `Trace` with steps: WAL-append, MemTable-put
- `ExplainVectorSearch(query, k, metric)` → returns a `Trace` with steps: namespace-scan, distance-computation, top-k selection
- All trace steps must reflect real execution; latency fields must be real measurements
- CLI command `shardforge trace get <key>` and `trace put <key> <value>`

### What must NOT be claimed yet

- Distributed tracing, distributed execution, cross-node traces
- OpenTelemetry integration

### Acceptance criteria

- `engine.ExplainGet` produces a `Trace` matching actual execution order (verified by test)
- All trace step durations are real measured values (not fixed zero)
- `go test -race -count=1 ./...` passes
- CLI `trace` command produces human-readable + JSON output

### Required tests

- ExplainGet: MemTable hit, MemTable miss + SSTable hit, MemTable miss + Bloom skip, full miss
- ExplainPut: WAL step appears before MemTable step
- ExplainVectorSearch: step count matches index size
- Duration: all steps have non-negative duration; total >= sum of steps

### Required docs updates

- `docs/PROOF.md` — Phase 15 proof section
- `docs/CLAIMS.md` — move "Operation traces" from Future to Safe

---

## Phase 16 — Single Networked Node (foundation hardening)

**Goal:** Harden the existing `internal/node` runtime. Add real health and status reporting, structured request logging, configurable timeouts, and a richer node API before multi-node work begins.

### What must be implemented

- Request logging middleware with latency and status
- Configurable read/write/idle timeouts
- `/metrics` endpoint returning JSON engine stats (key count, wal size, memtable size, sstable count)
- `/trace/{key}` endpoint — runs ExplainGet and returns trace JSON
- Node graceful shutdown (SIGTERM → drain → close engine)
- Node restart preserves data (existing, confirmed by updated test)

### What must NOT be claimed yet

- Multi-node coordination, consensus, distributed metrics
- Prometheus or OpenTelemetry format `/metrics`

### Acceptance criteria

- All new endpoints return correct JSON
- Graceful shutdown drains in-flight requests before closing engine
- Timeout config is respected (tested with slow client)
- `go test -race -count=1 ./...` passes

### Required tests

- Middleware attaches latency to every response
- `/metrics` returns correct key count after known writes
- `/trace/{key}` returns a valid Trace JSON matching ExplainGet
- Graceful shutdown: pending request completes before shutdown returns

### Required docs updates

- `docs/PROOF.md` — Phase 16 proof section
- `docs/DESIGN.md` — updated node runtime section

---

## Phase 17 — Network Client + Router

**Goal:** Replace the current client-side gateway's HTTP/JSON routing with a proper binary or structured RPC client. Lay the groundwork for real multi-node data routing.

### What must be implemented

- A typed `node.Client` that wraps all node endpoints with structured error types
- Retry-on-timeout (not retry-on-error, to avoid stale-data masking)
- Router: given a key and a cluster config, determine the responsible node and execute the operation through the node client
- Router must use the same FNV-1a ring as the gateway but be a separate, directly-callable Go package
- Integration tests: write key via router, read back via router

### What must NOT be claimed yet

- Multi-node coordination, consensus, automatic failover, distributed sharding
- The router is still client-side (not a separate process required)

### Acceptance criteria

- Router correctly routes 1000 random keys deterministically (same key always same node)
- Retry-on-timeout works: request times out, is retried once, succeeds
- `go test -race -count=1 ./...` passes

### Required tests

- Determinism: same key always maps to same node ID across 1000 iterations
- Error propagation: 502 from node becomes a typed error at Router level
- Retry: verify retry happens only on timeout, not on 4xx

### Required docs updates

- `docs/PROOF.md` — Phase 17 proof section
- `docs/CLAIMS.md` — update if any safe claims are unlocked

---

## Phase 18 — Real Multi-Node Sharding

**Goal:** Keys are actually stored on the responsible node, not just routed to it by a client-side ring. A write to node A for key K should NOT appear on node B even if node B is queried directly.

### What must be implemented

- Router enforces: data is only stored on the hash-responsible node
- Proxy enforces this end-to-end: proxy → router → responsible node
- Integration tests: write 100 keys via proxy, read each back from the correct node directly (not via proxy), confirm other nodes return not-found
- `docker compose` 3-node demo proves sharding is data-level, not just routing-level
- Shard membership stored in cluster config; no dynamic redistribution yet

### What must NOT be claimed yet

- Shard migration, automatic rebalancing, dynamic membership
- Data replication across shards
- Consensus or quorum

### Acceptance criteria

- Writing key K via proxy: only responsible node holds K (other nodes return 404)
- Reading K directly from responsible node matches value written via proxy
- Test proves data isolation per shard
- `docker compose -f deploy/docker-compose.yml up --build` works

### Required tests

- 100 keys written via proxy, each found on exactly 1 node directly
- 100 keys: reading from wrong node returns 404
- Node restart: data persists on the responsible node

### Required docs updates

- `docs/PROOF.md` — Phase 18 proof section with data isolation evidence
- `docs/CLAIMS.md` — unlock "Real distributed sharding" when this phase passes

---

## Phase 19 — Docker Compose Cluster Demo

**Goal:** Build a production-style Docker Compose demo that is self-contained, reproducible, and demoed in a CI-like environment. Includes health checks, realistic startup ordering, named volumes.

### What must be implemented

- `docker-compose.yml` with health checks, depends-on, named volumes
- Start script: `make cluster-demo` → starts 3 nodes + proxy, runs smoke test, tears down
- Smoke test script that writes 10 keys, reads all back via proxy, checks node health endpoints
- README section: step-by-step Docker demo with expected output

### What must NOT be claimed yet

- Production orchestration, Kubernetes, automatic scaling
- Fault tolerance from Docker health checks

### Acceptance criteria

- `make cluster-demo` runs start-to-end without errors
- Smoke test writes and reads 10 keys correctly
- Docker Compose passes `docker compose config --quiet`

### Required tests

- CI: `docker compose config --quiet` validates in GitHub Actions
- Smoke test script exits 0 on clean run

### Required docs updates

- `docs/PROOF.md` — Phase 19 proof with `docker compose` output

---

## Phase 20 — Networked Replication v1, Non-Raft

**Goal:** Replace the current explicit pull-only replication with automatic background replication. Primary continuously pushes new mutations to follower nodes via HTTP. No Raft, no quorum — still explicit leader designation.

### What must be implemented

- Primary node: background goroutine that pushes new log entries to registered followers
- Follower node: applies received entries to its engine; rejects direct writes (403)
- Replication lag tracking: follower reports how many entries behind primary
- Failover: manual only — operator explicitly promotes a follower to primary via CLI command
- Persistent replication log: backed by WAL so primary restarts don't lose the log
- `docker compose` replica demo with automatic sync (no manual POST required)

### What must NOT be claimed yet

- Raft, consensus, automatic leader election, quorum, automatic failover
- Strong consistency (follower reads may still lag)

### Acceptance criteria

- Write to primary → follower receives entry within 500ms (no manual sync)
- Primary restart: followers re-sync from WAL-backed log
- Follower write rejected with 403
- `go test -race -count=1 ./...` passes

### Required tests

- Auto-sync: write 10 keys to primary, wait 100ms, read from follower — all present
- WAL persistence: primary restarts, followers recover from where they left off
- Follower write rejection: 403 on direct PUT

### Required docs updates

- `docs/PROOF.md` — Phase 20 proof
- `docs/CLAIMS.md` — unlock "Networked replication" when phase passes

---

## Phase 21 — Process-Level Failure Testing

**Goal:** Test real process-level failures: kill a node process, observe proxy behavior, observe follower behavior. No fake simulation — real OS-level kills.

### What must be implemented

- Failure test harness: `internal/failtest` — starts real node processes, kills them via `os.Process.Kill`, verifies proxy behavior
- Proxy: on node failure, return 502 with structured error (current behavior confirmed)
- Follower: on primary failure, stop accepting sync and report "primary unreachable" status
- Chaos test: kill primary, write to proxy → 502, promote follower manually → writes succeed
- Ops CLI: `shardforge-cluster promote <node-id>` — sends HTTP request to designated follower to assume primary role

### What must NOT be claimed yet

- Automatic failover (promotion is still manual via CLI)
- Raft or consensus

### Acceptance criteria

- Kill primary → proxy returns 502 for write
- Kill primary → promote follower → write succeeds
- Test harness runs entirely in `go test` with real processes (no mocks)
- `go test -race -count=1 ./...` passes

### Required tests

- Kill node, verify proxy 502, verify promotion via CLI, verify write succeeds
- Kill follower, verify primary continues accepting writes unaffected
- Restart killed node, verify it rejoins as follower

---

## Phase 22 — Quorum / Consistency Modes

**Goal:** Add quorum write acknowledgment. A write is only accepted when N/2+1 nodes confirm receipt. Read consistency modes: eventual (any node) and session (read-your-writes).

### What must be implemented

- Quorum write: primary waits for majority acknowledgment before returning success
- Client configurable: quorum vs fire-and-forget
- Session consistency: client tracks a sequence number; reads are routed to nodes at or above that sequence
- `go test` integration tests: quorum write survives 1 follower being killed mid-write

### What must NOT be claimed yet

- Raft, leader election, automatic failover
- Linearizability or serializability

### Acceptance criteria

- Quorum write: kill 1 follower of 3-node cluster → write still succeeds (2/3 is quorum)
- Quorum write: kill 2 followers → write returns error (less than quorum)
- Session read: after quorum write, read on any node returns the written value

---

## Phase 23 — Raft or Honest Manual Failover

**Goal:** Implement either full Raft consensus OR an honest, explicitly-documented manual failover mechanism. If Raft: all of leader election, term handling, replicated log, commit index, voting, and failover must be implemented and tested. If manual failover: it must be explicitly documented as NOT automatic, with clear operator procedures.

### What must be implemented

**Option A (Raft):**
- Leader election with term numbers and voting
- Replicated log: entries not applied until committed by majority
- Leader failover: new election triggered when heartbeat times out
- Membership change: static (no dynamic join/leave yet)

**Option B (Manual failover, honest):**
- Operator-initiated promotion: CLI command triggers promotion
- Primary detects its own isolation and steps down (voluntary, not automatic)
- Clear documentation that this is NOT automatic and NOT Raft
- Phase claims: "honest manual failover" not "automatic failover"

### What must NOT be claimed (until Raft is implemented)

- If Option B chosen: must not claim "Raft", "consensus", "automatic leader election"

### Acceptance criteria

For Raft: full Raft test suite including split-brain prevention, leadership convergence, log consistency.
For manual: documentation is accurate, promotion works reliably.

---

## Phase 24 — Background Compaction + Block Cache

**Goal:** Add automatic background compaction triggered by SSTable count threshold. Add an in-memory LRU block cache for hot SSTable data blocks.

### What must be implemented

- Background goroutine: triggers `Compact()` when SSTable count exceeds threshold
- Configurable threshold (default: 8 SSTables)
- Block cache: LRU eviction, configurable size, per-SSTable block caching
- Cache hit/miss stats on `/metrics` endpoint

### Acceptance criteria

- Write 10,000 keys → SSTable count stays bounded (background compaction fires)
- Cache hit rate on repeated reads > 90% in benchmark
- `go test -race -count=1 ./...` passes

---

## Phase 25 — Distributed Vector Search

**Goal:** Vector search queries are executed across multiple nodes. Each node holds a partition of the vector index. Partial results are merged at the gateway.

### What must be implemented

- Vector namespace partitioning: vectors assigned to nodes by namespace hash
- Gateway: fan-out search to all nodes, merge top-k results
- Exact distributed k-NN: all nodes return their local top-k, gateway merges and re-ranks
- (Optional) ANN: HNSW index per node for approximate search at scale

### What must NOT be claimed (if ANN not implemented)

- If HNSW not implemented: must not claim "ANN", "approximate nearest neighbour", "sublinear search"

### Acceptance criteria

- 3-node cluster: insert 1000 vectors evenly distributed, search from gateway returns correct top-k
- Distributed results match single-node exact results on same dataset

---

## Phase 26 — Real Cluster Dashboard

**Goal:** Replace the local simulation dashboard with a real dashboard that polls live distributed nodes, shows replication lag, shows shard distribution, and is served by the proxy.

### What must be implemented

- Dashboard polls `/healthz`, `/metrics`, `/replication/status` on all configured nodes
- HTML: node health table, replication lag graph (or table), shard distribution chart
- Dashboard served by proxy at `GET /dashboard`
- Auto-refresh: polls every 5 seconds

### What must NOT be claimed

- No claim of "real-time" if polling interval is > 1 second

### Acceptance criteria

- Start 3-node cluster → open dashboard → all nodes show healthy with real metrics
- Kill 1 node → dashboard shows it unhealthy within 10 seconds
- Replication lag shows actual sequence number delta

---

## Phase 27 — Final Release Proof

**Goal:** Produce the final documented proof that ShardForgeDB is a real, explainable distributed database engine. All claims in `docs/CLAIMS.md` Section A are confirmed. All claims in Section B are confirmed absent.

### What must be implemented

- Full end-to-end integration test: start cluster, write 1000 keys + 100 vectors, kill 1 node, verify data integrity on remaining nodes, restart killed node, verify it catches up
- Release smoke script passes on clean machine
- `docs/FINAL_REPORT.md` documents every phase with test counts, benchmark results, and explicit limitation statements
- GitHub Release tag `v1.0.0-distributed` with release notes

### Acceptance criteria

- `make release-check` passes on CI
- All forbidden claims are absent from all documentation (verified by grep)
- 1000+ tests across all packages

---

## Rules for all future phases

1. No phase may claim a feature until it is tested, benchmarked, and reviewed.
2. Every phase produces its own `docs/PROOF.md` section with commands and output.
3. Every phase updates `docs/CLAIMS.md` to move items from Future to Safe or to add new Unsafe items discovered.
4. No phase may remove the scope flags from ops results.
5. If Raft is not implemented, Raft must never appear in any claim.
6. If ANN is not implemented, ANN / HNSW / IVF must never appear in any claim.
