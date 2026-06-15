# Background Replication Design — ShardForgeDB Phase 27

**Scope:** Configurable automatic background pull replication and operation-count lag tracking.
This document captures the design decisions for Phase 27 before any implementation.

**Non-goals (unchanged from Phase 26):** No Raft, no consensus, no quorum, no leader election,
no automatic failover, no synchronous replication, no write forwarding, no multi-primary,
no conflict resolution, no snapshot transfer, no automatic gap repair, no shard migration,
no dynamic membership.

The Phase 26 crash window remains:

```
engine commit → replication journal append
```

A process crash between these operations can leave a local mutation absent from replication
history. This limitation is unchanged in Phase 27.

---

## 1. Existing Phase 26 Explicit Synchronization Path

Phase 25/26 provides explicit pull-based replication via `POST /replication/sync`.

The path:

```
POST /replication/sync
  → handleReplicationSync
  → s.SyncFromPrimary(ctx)
      → syncInProgress.CompareAndSwap(false, true)   // concurrency gate
      → s.replicator.PullEntries(ctx, after, limit)   // HTTP GET /replication/log
      → s.ApplyReplicationEntries(entries)            // engine puts/deletes
          → s.stateStore.AdvanceTo(lastSeq)           // persist cursor
      → syncInProgress.Store(false)
```

This path is durable (Phase 26+): the primary journal persists across restarts; the follower
cursor persists to `replication_state.json`; gap detection returns HTTP 409.

---

## 2. Why the Background Worker Reuses SyncFromPrimary

**Rule:** `SyncFromPrimary` is the single, validated synchronization entry point.

Reusing it ensures the background worker inherits:

- The `syncInProgress` concurrency guard (prevents double-application)
- Durable cursor persistence via `stateStore.AdvanceTo`
- Idempotent reapplication (entries with `Seq <= lastApplied` are skipped)
- Replication-gap detection and `*ReplicationGapError` propagation
- Correct sequence validation in `ApplyReplicationEntries`
- Typed `*RollbackError` from the primary journal
- PUT and DELETE application via the local engine
- Context cancellation reaching in-flight HTTP requests

Duplicating the fetch-apply-persist pipeline would create two diverging code paths that could
independently introduce ordering bugs, double-application, or cursor corruption.

---

## 3. Worker Ownership and Lifecycle

The background sync worker (`backgroundSyncWorker`) is owned by the `Server`. It is:

- Created in `Open()` when `BackgroundSync.Enabled` is true and role is follower.
- Started in `Start()` and `StartBackground()` after the HTTP listener is bound.
- Stopped in `Close()` before resources are released.

The worker holds:

- A `context.Context` / `cancel` pair for shutdown signaling.
- A `sync.WaitGroup` (one goroutine) for deterministic join on stop.
- A mutex-protected `BackgroundSyncStatus` snapshot.
- Injectable seams for `syncFn`, `nowFn`, `jitterFn`, and `afterFn` (timer factory) to enable deterministic tests.

---

## 4. Startup Behavior

On `StartBackground()` or `Start()`:

1. The HTTP listener is bound first (existing behavior).
2. `bgWorker.start()` is called, which:
   - Sets state to `starting`.
   - Spawns a single goroutine.
   - Goroutine transitions to `running`.
   - Performs an **initial sync immediately** (no interval wait on first run).
   - The initial sync is a full `SyncFromPrimary` call, pulling all entries since the
     persisted durable cursor (which may be non-zero after a restart).

---

## 5. Shutdown and Cancellation Behavior

On `Close()`:

1. `bgWorker.stop()` is called **before** closing dependent resources.
2. `stop()` cancels the worker's context and calls `wg.Wait()`.
3. The goroutine receives the cancellation via:
   - The context passed to `SyncFromPrimary(ctx)` reaching the in-flight HTTP request.
   - The `select { case <-ctx.Done() }` in the wait loop.
4. After the goroutine exits, state transitions to `stopped`.
5. Then `durableLog.Close()` and `eng.Close()` are called.

**Guarantee:** the goroutine exits before any dependent resource is closed. No goroutine leaks.

---

## 6. Polling Interval Semantics

`BackgroundSyncConfig.Interval` controls the wait between **successful** sync completions and
the **next** attempt.

- The interval timer starts **after** `SyncFromPrimary` returns, not before.
- The interval applies to successful syncs and to `ErrSyncInProgress` skips.
- During exponential backoff, the interval is replaced by the computed backoff duration.
- After a successful sync following a backoff period, the next wait reverts to the normal
  interval (backoff resets to zero).

---

## 7. Retry and Backoff Policy

Temporary failures (connection refused, timeout, HTTP 5xx, any non-terminal error) use
bounded exponential backoff:

| Event | Backoff |
|---|---|
| First failure | `initial_backoff` |
| Second consecutive failure | `initial_backoff × 2` |
| Nth consecutive failure | `min(initial_backoff × 2^(N-1), max_backoff)` |
| Successful sync | reset to 0 (next wait is normal `interval`) |
| `ErrSyncInProgress` skip | no change to backoff |

Backoff state is visible in `BackgroundSyncStatus.CurrentBackoffMs` and `NextRetryAt`.

`ErrSyncInProgress` is explicitly excluded from backoff: the background worker just waits
for the normal interval and tries again.

---

## 8. Jitter Policy

`BackgroundSyncConfig.JitterFraction` adds a random component to backoff to prevent
thundering-herd behavior when multiple followers restart simultaneously.

Jitter range: `[0, current_backoff × jitter_fraction]` (uniformly distributed).

Applied backoff: `current_backoff + jitter`.

Rules:
- `jitter_fraction = 0.0`: no jitter (deterministic; good for tests).
- `jitter_fraction = 0.10`: up to 10% additional wait.
- The jitter function is injectable for deterministic tests.
- Jitter is applied only to backoff waits, not to normal interval waits.
- Jitter must be validated: must be in `[0.0, 1.0]`.

---

## 9. Concurrent Manual/Background Sync Behavior

Phase 26 enforces: only one `SyncFromPrimary` call can be in flight at a time (via
`syncInProgress atomic.Bool`).

**Background encounters manual sync in progress:**
- `SyncFromPrimary` returns `ErrSyncInProgress`.
- Worker: records as `TotalSkippedBusy++`. Does **not** increment `TotalFailures` or
  `ConsecutiveFailures`. Does not modify the current backoff.
- Worker waits the normal `Interval` before the next attempt.

**Manual sync encounters background sync in progress:**
- `SyncFromPrimary` returns `ErrSyncInProgress` to the HTTP handler.
- `handleReplicationSync` returns HTTP 409 with a structured error.
- The caller is informed that a sync is already running; no mutation state is changed.

This ensures at most one apply is in flight at any time, preventing double-application.

---

## 10. Lag Definition

```
lag_entries = primary_latest_seq − follower_last_applied_seq
```

`lag_entries` is clamped to 0 defensively if the subtraction would underflow (which should
not occur in correct operation but is guarded against).

`primary_latest_seq` is obtained from the `/replication/log` HTTP response, which now
includes a `primary_latest_seq` field alongside the entries. This avoids an extra round-trip.

---

## 11. Unknown or Stale Lag Behavior

Lag is **known** (`lag_known = true`) only when the follower has successfully contacted
the primary and received a `primary_latest_seq` value. Lag is **unknown** when:

- The worker has never successfully synced.
- The last sync attempt failed (any non-skip error).
- The worker is in `blocked` state due to a replication gap.

**Rule:** never report unknown lag as zero. When `lag_known = false`, consumers must not
interpret `lag_entries` as meaning "the follower is caught up." The field is omitted (0)
in the JSON response when lag is unknown.

The `lag_observed_at` timestamp records when `primary_latest_seq` was last observed.
Consumers can compare this against the current time to judge staleness.

---

## 12. Temporary Versus Terminal Errors

| Error | Classification | Worker Behavior |
|---|---|---|
| `ErrSyncInProgress` | Skip (not failure) | `TotalSkippedBusy++`; wait normal interval |
| Connection refused | Temporary | Exponential backoff |
| Request timeout | Temporary | Exponential backoff |
| HTTP 5xx | Temporary | Exponential backoff |
| Network reset | Temporary | Exponential backoff |
| `*ReplicationGapError` | **Terminal** | Enter `blocked` state; stop retrying |
| Context cancelled (shutdown) | External | Exit cleanly |

---

## 13. Replication-Gap Behavior

When `SyncFromPrimary` returns a `*ReplicationGapError`:

1. Worker transitions to `WorkerStateBlocked`.
2. The `blocked_reason` is set to `"replication_gap"`.
3. The gap struct (`RequestedAfter`, `FirstAvailableSeq`, `LatestSeq`) is stored.
4. Worker stops calling `SyncFromPrimary`. It waits for context cancellation.
5. `lag_known` is set to `false` (gap implies we cannot compute valid lag).
6. Manual `POST /replication/sync` still works (it calls `SyncFromPrimary` directly)
   and will return the same `*ReplicationGapError` to the operator.
7. The cursor is **not** automatically reset. Operator must reseed or reconfigure.

Recovery: restart the follower with a clean data directory (or after reseed from primary).

---

## 14. Restart Behavior

**Follower restart with background sync enabled:**

1. `Open()` restores `lastApplied` from `replication_state.json` (Phase 26 cursor).
2. `Start()` starts the worker.
3. Worker performs initial sync: `SyncFromPrimary(ctx)` with `after = lastApplied`.
4. Primary returns entries from `lastApplied + 1` to `primaryLatestSeq`.
5. Follower applies only new entries; lag converges to zero automatically.
6. No manual intervention required.

**Primary restart:**

1. Primary replays `replication.journal` (Phase 26) and restores its in-memory index.
2. Follower's background worker may be in backoff state (primary was unreachable).
3. After primary is healthy, the next background sync attempt succeeds.
4. Backoff resets; normal interval resumes.

**Both nodes restart:**

1. Primary restores its journal.
2. Follower restores its cursor from `replication_state.json`.
3. First background sync fetches only entries since the cursor.
4. No semantic duplicates: entries with `Seq <= lastApplied` are skipped.

---

## 15. Observability Fields

`BackgroundSyncStatus` fields (returned by `GET /replication/status`):

| Field | Type | Description |
|---|---|---|
| `background_sync_enabled` | bool | true when feature is configured on |
| `background_sync_running` | bool | true while goroutine is active |
| `background_sync_state` | string | `disabled`, `starting`, `running`, `backing_off`, `blocked`, `stopping`, `stopped` |
| `last_sync_attempt_at` | RFC3339 | when the last sync attempt started |
| `last_sync_success_at` | RFC3339 | when the last successful sync completed |
| `last_sync_failure_at` | RFC3339 | when the last failed sync completed |
| `last_sync_fetched` | int | entries returned by primary in last successful sync |
| `last_sync_applied` | int | entries newly applied in last successful sync |
| `total_sync_attempts` | int64 | total non-skipped attempts since startup |
| `total_sync_successes` | int64 | total successful syncs since startup |
| `total_sync_failures` | int64 | total failed syncs since startup |
| `total_sync_skipped_busy` | int64 | times skipped because a manual sync was in progress |
| `consecutive_failures` | int | failures since last success (resets on success) |
| `current_backoff_ms` | int64 | current backoff duration in ms (0 when not backing off) |
| `next_retry_at` | RFC3339 | when the next retry is scheduled (during backoff only) |
| `last_sync_error` | string | error message from last failed sync |
| `follower_last_applied_seq` | uint64 | follower's replication cursor |
| `primary_latest_seq` | uint64 | last observed primary seq (0 = unknown) |
| `lag_entries` | int64 | `primary_latest_seq − follower_last_applied_seq` (clamped ≥ 0) |
| `lag_known` | bool | whether lag is currently valid |
| `lag_observed_at` | RFC3339 | when `primary_latest_seq` was last observed |
| `blocked_reason` | string | `"replication_gap"` when blocked |
| `replication_gap` | object | gap struct when blocked by a gap |

---

## 16. Known Limitations

- Lag is measured in operations (entries), not wall-clock seconds or bytes.
- The Phase 26 crash window remains: a mutation applied to the engine but not appended to
  the primary journal before a crash is invisible to replication.
- Background sync is pull-based and requires the follower to initiate. The primary has no
  push mechanism.
- `primary_latest_seq` is obtained from the `/replication/log` response at the time of the
  pull. It is not continuously updated; it reflects the primary's state at the pull moment.
- After a follower restart, there is a window between startup and the first background sync
  completion during which lag is unknown (`lag_known = false`).
- Follower reads may lag by an arbitrary number of operations between sync intervals.
- Replication gap recovery is not automatic: it requires operator intervention.

---

## 17. Claims That Remain Unsafe

The following must NOT be claimed for Phase 27:

- **Real-time replication**: lag is non-zero between sync intervals.
- **Zero-lag replication**: the follower is always behind by at least one interval.
- **Zero-data-loss replication**: the Phase 26 crash window remains documented.
- **High availability**: no automatic failover; primary outage blocks writes.
- **Fault tolerance**: if the primary is permanently lost, the follower cannot recover.
- **Automatic failover**: primary outage requires operator action.
- **Guaranteed replication latency**: network issues can delay syncs arbitrarily.
- **Automatic gap repair**: gaps require manual reseed.
- **Strong consistency**: follower may read stale data.
- **Synchronous replication**: all replication is asynchronous pull.

---

## Summary of New Files

| File | Purpose |
|---|---|
| `internal/node/background_sync.go` | Background worker lifecycle, retry, backoff, status |
| `internal/node/background_sync_test.go` | Unit tests for the background worker |
| `internal/node/replication_phase27_test.go` | Integration tests for Phase 27 |
| `configs/replication/demo-background-sync.json` | Demo configuration |
| `scripts/repl_auto_demo_up.sh` | Demo startup script |
| `scripts/repl_auto_demo_smoke.sh` | Demo smoke test (24 checks) |
| `scripts/repl_auto_demo_down.sh` | Demo teardown script |
