# ShardForgeDB — Final Engineering Report

**Version:** v0.5.0-portfolio
**Status:** Phases 1–27 locked; Phase 28 implemented and awaiting final validation and merge. 1292 tests passing.

---

## Executive summary

ShardForgeDB is a ground-up Go database engine built as a 28-phase educational and portfolio project. It implements a WAL-backed LSM-tree key-value store, exact vector search, a real networked HTTP node runtime, a stateless routing proxy, explicit pull-based read replicas with durable journal and cursor, automatic background pull replication with configurable interval/backoff/jitter and lag tracking, an operations simulation layer, a full explainability system that produces real execution traces for every key database operation, and an operator-controlled manual promotion workflow for planned failover. All phases are tested, benchmarked, and documented. Every design decision and limitation is explicitly stated.

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
| 28 | `internal/node/handlers.go` (handleQuiesce/handlePromote rewrite, writeJSONError, handleExplainPut/Delete fencing), `internal/node/server.go` (quiesceMu, promoteMu, promotionBarrier, quiesce_failed_fenced, quiesceIDFn seam, runtimeSnapshot/runtimeState(), replicationMutationMu, restartBackgroundWorkerAfterPromotionFailure idempotent, removeQuiesceIntentFn seam, ApplyReplicationEntries follower-only), `internal/node/types.go` (ReplicationStatusResponse Phase 28 fields, HTTPStatusError), `internal/node/config.go` (ErrQuiesceInProgress, ErrQuiesceFailedFenced, ErrPromotionInProgress, ErrNotFollower), `internal/replnet/quiesce_store.go` (NewQuiesceID→(string,error), NewQuiesceRecord→(*QuiesceRecord,error)), `internal/replnet/journal_baseline.go` (CreateJournalBaseline idempotent, JournalBaselineExists, ErrJournalBaselineConflict, ErrJournalBaselineMaxUint64, journalCompatibilityCheck), `internal/replnet/quiesce_intent.go` (SaveQuiesceIntent, LoadQuiesceIntent, RemoveQuiesceIntent), `internal/node/phase28_hardening_test.go` (31 tests), `internal/node/phase28_safety_test.go` (28 tests), `internal/node/phase28_unit_test.go`, `internal/node/phase28_test_helpers_test.go`, `internal/replnet/journal_baseline_test.go` (22 tests), `internal/replnet/quiesce_store_test.go` (17 tests), `docs/MANUAL_PROMOTION_DESIGN.md` | Phase 28 hardening (all 4 passes): quiesceMu serializes all concurrent quiesce; quiesce_failed_fenced with pendingQuiesceRecord retry; entropy failure aborts before gate closes; quiesceIDFn injectable seam; s.Addr() (not :0); handleExplainPut/Delete fenced; runtimeSnapshot atomic snapshot; promoteMu serializes promotion; promotionBarrier+double-check drain; CreateJournalBaseline idempotent; cross-validation: new_role, fields, MaxUint64, baseline, BaseSeq; pre-commit revert/post-commit preserve; writeJSONError stable code; typed ReplicationStatusResponse; quiesce-intent durable record; replicationMutationMu RWMutex (triple barrier); HTTPStatusError; cursor re-validated UNDER WLock; ApplyReplicationEntries split+follower-only; startup fails on state-store error; journalCompatibilityCheck stat-first; idempotent bgWorker restart; ErrPromotionInProgress from all 3 barriers (HTTP 409); quiesceIntentActive truthful (cleanup_pending vs active); BackgroundSyncStatus+SyncFromPrimary replicator under mutex. Test breakdown (all Phase 28 net-new, none of these files existed on main): phase28_hardening_test.go=31, phase28_safety_test.go=28, journal_baseline_test.go=22, quiesce_store_test.go=17 → 98 Phase 28 net-new tests | 98 (Phase 28) | — |

**Total tests:** 1292
**Total benchmarks:** 120+
**Packages with tests:** 23 of 27

---

## Architecture summary

The engine is a classic LSM-tree: writes go to WAL (for durability) and MemTable (for fast reads), then flush to SSTables on disk. Bloom filters skip unnecessary SSTable reads. Manual compaction merges all SSTables and drops tombstones.

The vector store uses the engine as its persistence layer and maintains an in-memory exact index rebuilt on open.

The networked layer adds real HTTP/JSON nodes, a client-side consistent-hash routing gateway, and a stateless proxy. Each node is independent — no coordination, no shared state.

Read replicas add explicit pull-based sync: the primary keeps a durable binary journal (`replication.journal`); followers persist their cursor to `replication_state.json` and pull entries on demand. Both survive process restarts. Gap detection returns HTTP 409 when the follower falls too far behind.

Phase 27 adds automatic background pull replication: when `--bg-sync` is set, a goroutine polls the primary every 500ms (configurable). Exponential backoff on failure, bounded jitter, `ErrSyncInProgress`→skip, `*ReplicationGapError`→terminal blocked state. Lag tracking (`lag_entries`, `lag_known`) is always accurate after any successful sync. The manual sync path is still available alongside the background worker. NOT Raft, NOT automatic failover.

Phase 28 adds operator-controlled manual promotion and controlled failover: a primary can be quiesced (write-fenced) via `POST /replication/quiesce`; a follower can be promoted via `POST /replication/promote` with the quiesce record. The Phase 28 hardening pass fixes all concurrency and crash-consistency issues found after initial implementation.

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

Phases 1–27 locked; Phase 28 implemented and awaiting final validation and merge. `go test -race -count=1 ./...` → 1292 tests pass across 23 packages. Smoke demos: cluster (25/25), repl (16/16), repl-restart (16/16), repl-auto (24/24), repl-failover (32/32).

**Phase 26 fix pass hardening** (not a new phase — correctness fixes to Phase 26 before PR acceptance):
- Journal fsync before index update; rollback on failure (injectable `syncFn` for deterministic tests)
- Identity-bound follower state file (`version`, `follower_node_id`, `primary_url`, `updated_at`; CRC32 covers all fields)
- Directory fsync after state file rename (no-op on macOS; durable on Linux)
- `AdvanceTo` returns `ErrInvalidSeqRegression` for backward cursor moves (not silent)
- Sequence monotonicity enforced in `replay()` (`seq == prevSeq+1`)
- `ErrSyncInProgress` guard prevents concurrent `SyncFromPrimary` calls
- Crash window documented explicitly: engine write before journal append; mutation visible in engine but absent from journal if crash occurs between the two

**Phase 28 concurrency and crash-consistency hardening** (correctness fixes before PR acceptance):
- `quiesceMu sync.Mutex` serializes the entire quiesce operation (check → gate → persist → state); concurrent quiesce requests queue and the second caller sees the already-quiesced state idempotently
- `quiesce_failed_fenced` state: when the write gate closes but `SaveQuiesceRecord` fails, `pendingQuiesceRecord` is preserved and retry reuses the same QuiesceID
- `NewQuiesceID() (string, error)`: entropy failure aborts quiesce before gate closes (gate is never poisoned by a zero/predictable ID); `quiesceIDFn func() (string, error)` injectable seam for tests
- `s.Addr()` used for `primary_base_url` capture; quiesce rejected if listener not yet bound (`:0`)
- `handleExplainPut` and `handleExplainDelete` now check `writeGate.Enter()` (previously unfenced despite real engine writes)
- `runtimeSnapshot` struct + `runtimeState()` method: all mutable fields read under `s.mu` in one snapshot; eliminates data races in all handlers that previously read `s.runtimeRole` bare
- `promoteMu sync.Mutex` serializes the entire promotion operation; concurrent promote attempts queue
- `promotionBarrier atomic.Bool` + double-check pattern: set before stopping bgWorker; `SyncFromPrimary` checks barrier before AND after claiming `syncInProgress` CAS slot, preventing slip-through
- `CreateJournalBaseline(dir, baseSeq)` idempotent helper: same value → nil, different value → `ErrJournalBaselineConflict`, `MaxUint64` → `ErrJournalBaselineMaxUint64`; used as phase-1 commit in promotion (orphan-safe)
- Cross-validation in `resolveRuntimeRole()`: verifies `new_role=="primary"`, non-empty fields, `InheritedLastSeq != MaxUint64`, baseline exists, `baseline.BaseSeq == rec.InheritedLastSeq`; any mismatch → node refuses to open
- Pre-commit failure: revert `promotionBarrier` + `promotionState`; post-commit failure: preserve state for restart recovery
- `writeJSONError` helper: stable machine-readable `code` field in all error responses (`node_quiesced`, `wrong_role`, `node_closing`, `promotion_sequence_mismatch`, `quiesce_persistence_failed`, `sync_in_progress`)
- Fully typed `ReplicationStatusResponse` with Phase 28 fields: `runtime_role`, `local_role_source`, `write_state`, `quiesced`, `quiesce_state`, `quiesce_id`, `quiesced_at`, `quiesced_latest_seq`, `promotion_state`
- 31 tests in `phase28_hardening_test.go` (all Phase 28 net-new); 22 tests in `journal_baseline_test.go` (all Phase 28 net-new); 17 tests in `quiesce_store_test.go` (all Phase 28 net-new); total 1264 passing (after passes 1+2)

**Phase 28 hardening pass 3** (9 safety contracts added; commit `381b11b`):
- `validatePromotionPreconditionsLocked`: re-reads `lastApplied` AND durable cursor UNDER exclusive `replicationMutationMu` WLock; any divergence rejects promotion with `ErrPromotionSequenceMismatch`
- `ApplyReplicationEntries` split: public guarded wrapper (barrier check + RLock) + `applyReplicationEntriesLocked` private helper; `SyncFromPrimary` calls locked path directly (already holds RLock)
- `resolveRuntimeRole()`: `NewReplicationStateStore` errors at promoted-primary startup now fail `Open()` — no silent ignore
- `journalCompatibilityCheck`: `os.Stat` first (nil if file absent); propagates `OpenDurableLog` errors
- `restartBackgroundWorkerAfterPromotionFailure`: atomic restart under `s.mu` with `closed`/`role`/`barrier` guards; pre-commit failure path uses this helper
- All 3 `SyncFromPrimary` barrier returns wrap `ErrPromotionInProgress`; `handleReplicationSync` returns HTTP 409 with `promotion_in_progress`; `HTTPStatusError.Is()` maps code → `ErrPromotionInProgress`
- Quiesce retry success path calls `RemoveQuiesceIntent`; `quiesceIntentActive` cleared only if removal succeeds
- `bgEnabled` read moved under `s.mu` in `SyncFromPrimary` (data race fix)
- 18 new tests in `phase28_safety_test.go`; total 1282 passing

**Phase 28 hardening pass 4** (6 safety contracts added; awaiting final validation and merge):
- `restartBackgroundWorkerAfterPromotionFailure` idempotent: reads `s.bgWorker.Status().State` under `s.mu`; only replaces nil/stopped/disabled workers; live worker → no-op (prevents duplicate active worker on repeated failure retry)
- `BackgroundSyncStatus()` race-free: reads `s.bgWorker` under `s.mu.Lock/Unlock`; calls `worker.Status()` outside the mutex
- `SyncFromPrimary` replicator capture: captures `closed`, `role`, `replicator`, `bgEnabled` under `s.mu` in first critical section; uses captured `replicator` pointer for `PullEntries`; fast path barrier check remains before lock
- `ApplyReplicationEntries` follower-only: after barrier+RLock, reads role under `s.mu`; returns `ErrNotFollower` for primary/standalone/promoted-primary (promoted-primary hits barrier first → `ErrPromotionInProgress`)
- `removeQuiesceIntentFn` injectable seam: truthful `quiesceIntentActive` — set to false only when removal succeeds; `handleReplicationStatus` reports `cleanup_pending` (quiesced + intent active) vs `active` (in-flight + intent active)
- Deterministic Close-vs-restart race test: 50 iterations with `sync.WaitGroup` barrier; after Close, no worker in running/starting/backing-off state
- 10 new tests added in `phase28_safety_test.go` (pass 4); 28 total in that file; total 1292 passing. Phase 28 net-new grand total: 31 + 28 + 22 + 17 = 98 tests across 4 files (all entirely Phase 28 new — none existed on main)

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
