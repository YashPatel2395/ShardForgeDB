# ShardForgeDB — Final Engineering Report

**Version:** v0.4.0-portfolio
**Status:** 27-phase build complete, all tests passing, all benchmarks reproducible.

---

## Executive summary

ShardForgeDB is a ground-up Go database engine built as a 27-phase educational and portfolio project. It implements a WAL-backed LSM-tree key-value store, exact vector search, a real networked HTTP node runtime, a stateless routing proxy, explicit pull-based read replicas with durable journal and cursor, automatic background pull replication with configurable interval/backoff/jitter and lag tracking, an operations simulation layer, and a full explainability system that produces real execution traces for every key database operation. All phases are tested, benchmarked, and documented. Every design decision and limitation is explicitly stated.

The project is not a production database. It is an explainable, deeply documented implementation that demonstrates real engineering depth across storage, networking, routing, replication, observability, and operations tooling.

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
| 21 | `internal/trace` | Trace type package: `Trace`, `TraceStep`, `OperationType`, `Component`, `StepType`, `Status` | 22 | — |
| 22 | `internal/engine`, `internal/vector`, `cmd/shardforge` | Runtime operation traces: `ExplainGet/Put/Delete/Scan`, vector `ExplainUpsert/Search/Delete`, `shardforge explain` CLI | 40 | — |
| 23 | `internal/node`, `cmd/shardforge` | HTTP explain endpoints, `node.Client` explain methods, `shardforge explain-node` CLI | 24 | — |
| 24 | `configs/cluster/`, `scripts/demo_cluster_*.sh`, `docs/DEMO.md`, `internal/cluster/demo_test.go` | Reproducible local 3-node cluster demo: up/smoke/down scripts, key placement proof, data isolation proof, 13 new cluster tests | 13 | — |
| 25 | `configs/replication/`, `scripts/repl_demo_*.sh`, `internal/node` (SyncResult, Client.SyncReplication), `internal/node/replication_phase25_test.go`, `internal/cluster/replication_demo_test.go` | Networked pull-based replication demo: leader+follower HTTP nodes, explicit pull via POST /replication/sync, PUT+DELETE replication, idempotent pull, role enforcement, 20 new tests | 20 | — |
| 26 | `internal/replnet/durable_log.go`, `internal/replnet/state_store.go`, `internal/node/replication_phase26_test.go`, `scripts/repl_restart_demo_*.sh`, `docs/REPLICATION_DURABILITY_DESIGN.md` | Durable replication state: binary journal for primary (`replication.journal` — per-Append fsync, rollback-on-failure, ErrPoisonedLog on rollback failure, replay boundary checks); identity-bound versioned JSON cursor for follower (`replication_state.json` — version+follower_node_id+primary_url+last_applied_seq+updated_at+checksum, CRC32 over all fields, directory fsync best-effort); gap detection (HTTP 409); sequence monotonicity enforcement; concurrent sync guard (`ErrSyncInProgress`); crash window documented; restart recovery demo (18 checks). Test breakdown: 31 DurableLog + 12 StateStore + 18 Phase26_node = 61 Phase 26 tests | 61 | — |
| 27 | `internal/node/background_sync.go`, `internal/node/background_sync_test.go`, `internal/node/replication_phase27_test.go`, `internal/node/server_lifecycle_test.go`, `internal/node/types.go` (Duration, BackgroundSyncConfig, WorkerState, BackgroundSyncStatus, ReplicationStatusResponse), `internal/node/config.go` (ErrAlreadyStarted, NaN/Inf jitter guard), `internal/node/server.go` (single-use lifecycle, race-safe start under mutex), `internal/node/handlers.go` (HTTP 409 ErrSyncInProgress + code field), `internal/node/client.go` (ReplicationStatus typed method; SyncReplication typed ErrSyncInProgress), `internal/replnet/replicator.go` (PullResult.PrimaryLatestSeqKnown, *uint64 logResponse), `cmd/shardforge-node/main.go` (bg-sync flags), `scripts/repl_auto_demo_*.sh` (24 checks), `configs/replication/demo-background-sync.json`, `docs/BACKGROUND_REPLICATION_DESIGN.md` | Automatic background pull replication with lag tracking: configurable goroutine polls primary every 500ms; exponential backoff (initial→max) with bounded jitter (fraction); ErrSyncInProgress→skip (resets backoff to 0, no failure counter); *ReplicationGapError→WorkerStateBlocked (clears Running/CurrentBackoffMs/NextRetryAt); lag_entries/lag_known propagated from PrimaryLatestSeqKnown (*uint64 pointer in logResponse); UTC timestamps (nowFn); server single-use lifecycle (ErrAlreadyStarted sentinel, started+closed bool under mu); worker CAS linearized inside lifecycleMu (CAS inside lifecycleMu.Lock() so stop() winning the mutex leaves CAS slot unconsumed; preLaunchHook test seam verifies stop() blocks until start() completes wg.Add+launch); shutdown cancellation not counted as failure (w.ctx.Err() guard); HTTP 409 with code="sync_in_progress" for manual sync conflict; Client.SyncReplication returns wrapped ErrSyncInProgress on 409 (502 does not match); typed ReplicationStatusResponse client; 24-check smoke demo; NOT Raft; NOT automatic failover. Test breakdown: 48 unit (background_sync_test.go: +StopWaitsForConcurrentStart +StopBeforeStart_StartStillSucceeds −ConcurrentStartStop) + 26 integration (replication_phase27_test.go) + 14 lifecycle+client (server_lifecycle_test.go: 9 original + 5 blocking-Start path) + 1 replnet (replicator_test.go#PrimaryLatestSeq_Reported) = 89 Phase 27 tests | 89 | — |

**Total tests:** 1112
**Total benchmarks:** 120+
**Packages with tests:** 23 of 27

---

## Architecture summary

The engine is a classic LSM-tree: writes go to WAL (for durability) and MemTable (for fast reads), then flush to SSTables on disk. Bloom filters skip unnecessary SSTable reads. Manual compaction merges all SSTables and drops tombstones.

The vector store uses the engine as its persistence layer and maintains an in-memory exact index rebuilt on open.

The networked layer adds real HTTP/JSON nodes, a client-side consistent-hash routing gateway, and a stateless proxy. Each node is independent — no coordination, no shared state.

Read replicas add explicit pull-based sync: the primary keeps a durable binary journal (`replication.journal`); followers persist their cursor to `replication_state.json` and pull entries on demand. Both survive process restarts. Gap detection returns HTTP 409 when the follower falls too far behind.

Phase 27 adds automatic background pull replication: when `--bg-sync` is set, a goroutine polls the primary every 500ms (configurable). Exponential backoff on failure, bounded jitter, `ErrSyncInProgress`→skip, `*ReplicationGapError`→terminal blocked state. Lag tracking (`lag_entries`, `lag_known`) is always accurate after any successful sync. The manual sync path is still available alongside the background worker. NOT Raft, NOT automatic failover.

The ops layer adds health visibility, failure simulation (routing impact without live calls), and manual rebalance planning (key movement without data movement).

The explainability layer (Phases 21–23) makes every operation traceable: `ExplainGet` walks the real MemTable→Bloom→SSTable path and records each step with real wall-clock timing. `ExplainPut` records WAL_APPEND and MEMTABLE_PUT. `ExplainScan` records SCAN_SOURCE from each layer and SCAN_MERGE. These traces are exposed both locally via `shardforge explain` and over the network via HTTP `/explain/*` endpoints on each node, callable via `shardforge explain-node`.

---

## Explainability system (Phases 21–23)

Phase 23 completed the explainability system. Phase 24 (the current final phase) adds the reproducible multi-node local cluster demo. The explainability system is the most important feature for understanding how the database internals actually work:

**Example GET trace (key found in SSTable after flush):**

```json
{
  "operation": "GET",
  "key": "user:1",
  "steps": [
    {"component":"ENGINE","step_type":"KEY_VALIDATED","status":"OK"},
    {"component":"MEMTABLE","step_type":"MEMTABLE_MISS","status":"OK","duration_ns":2041},
    {"component":"BLOOM","step_type":"BLOOM_CHECK","status":"OK","duration_ns":875,"detail":"sstable=000001.sst"},
    {"component":"SSTABLE","step_type":"SSTABLE_HIT","status":"OK","duration_ns":189042,"detail":"sstable=000001.sst"}
  ]
}
```

This trace shows exactly why Bloom filters matter (they skip SSTables where the key is definitely absent), and exactly which SSTable had to be read. Every step is produced by the real code — no fabricated output.

---

## Engineering learnings

### Binary formats are harder than they look
The WAL and SSTable both use hand-written binary formats with CRC checksums. Getting the exact byte offsets right, handling partial tail writes gracefully, and ensuring atomic creation via temp-file + rename required significant care.

### Bloom filters are O(1) but not magic
The Bloom filter implementation made it clear that false positive rate depends heavily on the number of items and hash functions. The real implementation includes a formula-derived bit array size and hash count, and the tests verify that false positive rate stays within bounds.

### LSM-tree compaction is a correctness problem, not just performance
Full compaction needs to correctly handle tombstones — a deleted key must not reappear after compaction. The implementation uses a two-pass merge with explicit tombstone tracking, verified with tests for overlapping key ranges and multiple generations of overwrites.

### HTTP nodes are easier than networked consensus
Making each node an independent HTTP/JSON process is tractable. The hard part of distributed databases is coordination — and ShardForgeDB explicitly does not implement it. The proxy's 502-on-failure behavior is honest: without Raft, there is no safe way to retry to another node.

### Explainability requires real execution paths
The trace system works only because each `Explain*` method mirrors the exact execution of its non-Explain counterpart. If the Explain version diverged from the real path, the trace would be educational fiction rather than educational fact. The tests verify step types (WAL_APPEND, MEMTABLE_HIT, BLOOM_SKIP, etc.) against real data paths.

---

## Honest limitations

- No Raft, no consensus, no quorum replication
- No automatic failover (proxy returns 502; no rerouting)
- No shard migration (data stays where it was written)
- No dynamic membership (static JSON config only)
- No background compaction (manual `Compact()` only)
- No block cache (full SSTable reads on every access)
- No distributed tracing (traces cover one node only; no cross-node propagation)
- Exact vector search only (O(n·d) brute-force; no HNSW, no IVF)
- Primary journal is durable per-Append (fsync before index update; crash window between engine.Put and journal.Append is documented — a mutation applied to the engine but lost before journal.Append is invisible to replication)
- Follower reads may lag by arbitrary number of ops

---

## Release status

All 27 phases complete. `go test -race -count=1 ./...` → 1112 tests pass across 23 packages. Smoke demos: cluster (25/25), repl (16/16), repl-restart (16/16), repl-auto (24/24).

**Phase 26 fix pass hardening** (not a new phase — correctness fixes to Phase 26 before PR acceptance):
- Journal fsync before index update; rollback on failure (injectable `syncFn` for deterministic tests)
- Identity-bound follower state file (`version`, `follower_node_id`, `primary_url`, `updated_at`; CRC32 covers all fields)
- Directory fsync after state file rename (no-op on macOS; durable on Linux)
- `AdvanceTo` returns `ErrInvalidSeqRegression` for backward cursor moves (not silent)
- Sequence monotonicity enforced in `replay()` (`seq == prevSeq+1`)
- `ErrSyncInProgress` guard prevents concurrent `SyncFromPrimary` calls
- Crash window documented explicitly: engine write before journal append; mutation visible in engine but absent from journal if crash occurs between the two

**Phase 27 production-contract hardening** (correctness and lifecycle fixes before PR acceptance):
- `ErrAlreadyStarted` sentinel: server single-use lifecycle (`started bool` under mutex); double-Start or Start-after-Close returns typed error
- Race-safe worker start: `bgWorker.start()` called while holding `s.mu` so `Close()` cannot race between lock release and worker launch
- Worker `lifecycleMu`: CAS (`started.CompareAndSwap`) moved inside `lifecycleMu.Lock()` so `start()` is linearizable — either `stop()` wins the mutex first (observes `cancel==nil`, returns no-op, CAS slot unconsumed so a subsequent `start()` still succeeds) or `start()` wins and completes the entire CAS + cancel-set + `wg.Add(1)` + goroutine-launch sequence before `stop()` can proceed. `preLaunchHook` test seam (called inside `lifecycleMu` after cancel set, before `wg.Add`) makes the invariant deterministically verifiable.
- Worker CAS idempotency: `started atomic.Bool` with `CompareAndSwap` (now inside `lifecycleMu`); `stop()` is also idempotent (nil-cancel safe, stop-before-start safe, stop-twice safe)
- Shutdown not counted as failure: `w.ctx.Err()` guard in `doSync` prevents cancelled-context errors from incrementing `TotalFailures`
- `ErrSyncInProgress` resets backoff: `*currentBackoff = 0` so next wait uses normal interval
- `PrimaryLatestSeqKnown`: `*uint64` pointer in `logResponse` distinguishes "primary empty (field present, value 0)" from "older primary (field absent)"; propagates through `PullResult.PrimaryLatestSeqKnown` → `SyncResult.LagKnown`
- HTTP 409 with `"code": "sync_in_progress"` for manual sync conflict: `handleReplicationSync` returns 409 when `errors.Is(err, ErrSyncInProgress)`; `Client.SyncReplication` detects the code field and returns `fmt.Errorf("...: %w", ErrSyncInProgress)` so `errors.Is(err, ErrSyncInProgress) == true`; 502 primary-unavailable does NOT match (proven by `TestServer_Client_SyncReplication_502PrimaryDown_DoesNotMatchErrSyncInProgress`)
- Server lifecycle tests (14 total in `server_lifecycle_test.go`): double-StartBackground (standalone/primary/follower-with-bg), StartBackground-after-Close (2 variants), concurrent-StartBackground+Close (50-iteration race loop), follower-bg-started-once; plus 5 blocking-Start path: Start×2 (ErrAlreadyStarted, proves no second listener), Start+StartBackground cross (ErrAlreadyStarted), StartBackground+Start cross (ErrAlreadyStarted), Start-after-Close (ErrClosed, proven non-blocking), concurrent-Start+Close (50-iteration race loop, no panic/deadlock)
- Typed client: `ReplicationStatusResponse` + `Client.ReplicationStatus()`
- UTC timestamps: `nowFn: func() time.Time { return time.Now().UTC() }` in worker
- Blocked state cleanup: `WorkerStateBlocked` clears `Running=false`, `CurrentBackoffMs=0`, `NextRetryAt=nil`
- NaN/Inf jitter guard: `math.IsNaN || math.IsInf` before range check in `ValidateBackgroundSyncConfig`
- Smoke script: 18→24 checks; `current_backoff_ms` omitempty handled correctly; manual sync accepts 200 or 409
- Test race fix: `TestPhase27_FollowerRestart_CursorRestoredBeforeWorkerStart` polls `FollowerLastAppliedSeq >= 3` (not stale `waitForBgZeroLag`) before reading cursor
- Phase 27 net-new test accounting: 48 (`background_sync_test.go`: −ConcurrentStartStop +StopWaitsForConcurrentStart +StopBeforeStart_StartStillSucceeds) + 26 (`replication_phase27_test.go`) + 14 (`server_lifecycle_test.go`) + 1 (`replicator_test.go#PrimaryLatestSeq_Reported`) = 89 (not 88; +1 for linearization proof tests)

The project is suitable for portfolio presentation, technical interviews, and as a reference implementation for database internals education. It is not suitable for production use.
