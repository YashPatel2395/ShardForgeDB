# Phase 28 — Manual Promotion and Controlled Failover Design

## Scope

Strictly operator-controlled, planned promotion workflow. NOT automatic failover.
NOT Raft. NOT consensus. NOT quorum. NOT distributed failure detection.

## 1. Existing Phase 27 Replication Architecture

ShardForgeDB uses pull-based replication between a single primary and one or more
followers. The primary maintains a durable binary journal (`replication.journal`)
with monotonically increasing sequence numbers starting at 1. Followers pull
entries via `GET /replication/log?after=<seq>` and apply them locally, persisting
their cursor in `replication_state.json`. Background sync (Phase 27) automates
polling on a configurable interval with exponential backoff on failure.

Key properties:
- Primary appends to journal on every PUT/DELETE (write-after-commit ordering).
- Follower cursor is identity-bound (follower_node_id + primary_url).
- No push from primary to follower; follower initiates all pulls.
- No distributed locking, fencing, or consensus.

## 2. Planned Failover Sequence (Step-by-Step Operator Procedure)

1. Operator ensures follower is caught up (lag = 0 via `/replication/status`).
2. Operator quiesces the primary: `POST /replication/quiesce` on the primary.
3. Primary drains in-flight writes, rejects all future writes, records final seq.
4. Primary persists a quiesce record (quiesce_record.json) with the final seq.
5. Primary returns QuiesceResponse containing the quiesce record.
6. Operator syncs the follower one final time (or waits for bg sync) to reach the
   primary's final seq.
7. Operator verifies follower cursor == primary's quiesced seq.
8. Operator stops the old primary process.
9. Operator sends `POST /replication/promote` to the follower with the quiesce
   record and a confirmation flag (`confirm_old_primary_stopped: true`).
10. Follower validates all preconditions (17 checks).
11. Follower stops its background sync worker.
12. Follower persists journal baseline (base_seq = inherited seq).
13. Follower persists promotion record.
14. Follower switches runtime role to primary.
15. Follower initializes write gate and durable log.
16. Follower returns PromoteResponse.
17. Operator updates client routing to point to the new primary.
18. New primary accepts writes, starting at seq N+1.

## 3. Why Promotion Is Manual (No Distributed Failure Detection)

ShardForgeDB has no distributed failure detector. There is no heartbeat protocol,
no gossip layer, no quorum, and no lease mechanism. A follower cannot distinguish
between "primary is down" and "network partition isolates me from primary." Without
reliable failure detection, automatic promotion would risk split-brain.

## 4. Why Automatic Failover Remains Unsafe (Split-Brain, No Fencing)

Automatic failover requires distributed fencing to prevent the old primary from
accepting writes after a new primary is elected. ShardForgeDB has no fencing
mechanism: the old primary has no way to learn that it has been superseded. If both
nodes accept writes simultaneously, data diverges irrecoverably.

Even with a fencing token approach, the lack of a consensus layer means there is no
authoritative source of truth about which node holds the primary lease.

## 5. Primary Write-Quiesce State Machine

```
active  ──[POST /replication/quiesce]──>  quiescing (transient)
                    │
                    ├─[entropy failure]──────────────────>  active (no-op, gate never closed)
                    │
                    ├─[gate closed, SaveQuiesceRecord fails]──>  quiesce_failed_fenced
                    │                                               (retry reuses same QuiesceID)
                    │
                    └─[gate closed, save succeeds]──────>  quiesced
```

- **active**: Normal operation. Writes accepted.
- **quiescing**: Transient state while the write gate drains in-flight writes.
  Not externally observable (entire transition happens within a single HTTP
  request handler, serialized by `quiesceMu`).
- **quiesce_failed_fenced**: Gate is closed (writes already rejected) but the
  quiesce record was not persisted. The `pendingQuiesceRecord` is kept. Retry
  uses the same QuiesceID to maintain idempotency, skips re-calling `quiesceIDFn`.
- **quiesced**: Terminal state. All writes rejected with HTTP 409. Reads still
  served. Replication log still available for followers to catch up.

The state machine is one-way: there is no un-quiesce operation. Once quiesced, the
node remains write-fenced forever (including across restarts).

The entire quiesce operation (check → gate → persist → state) is serialized by
`quiesceMu sync.Mutex`. Concurrent POST /replication/quiesce requests queue up;
only one QuiesceID is ever generated per primary.

## 6. Write-Handler Synchronization: RWMutex Write Gate

A `writeGate` type uses `sync.RWMutex` to synchronize writes and quiesce:

- Each write handler calls `writeGate.Enter()` which acquires `RLock`. This includes
  `handleKVPut`, `handleKVDelete`, `handleExplainPut`, and `handleExplainDelete`.
- `POST /replication/quiesce` calls `writeGate.Quiesce()` which acquires `Lock`.
- The exclusive Lock blocks until all shared RLock holders release, ensuring all
  in-flight writes complete before quiesce returns.
- After Quiesce(), an `atomic.Bool` flag causes all future `Enter()` calls to
  return `ErrNodeQuiesced` immediately (fast path, no lock contention).

**FSync durability note**: The write gate's `Quiesce()` returning guarantees that
all in-flight writes have committed to the WAL (per-write fsync) and appended to
the replication journal (per-Append fsync). No additional flush is needed before
recording the quiesce record.

## 7. In-Flight Write Handling

When `Quiesce()` is called, `mu.Lock()` blocks until every goroutine that holds
`mu.RLock()` calls `mu.RUnlock()`. This is the standard Go `sync.RWMutex`
behavior. Once `Lock()` returns, all in-flight writes have completed (committed to
engine and appended to journal). The quiesce handler then reads the journal's
latest seq as the authoritative final sequence number.

## 8. Quiesce Record Schema

```json
{
  "version": 1,
  "quiesce_id": "<16-hex-char random>",
  "primary_node_id": "primary-1",
  "primary_base_url": "http://127.0.0.1:9601",
  "primary_latest_seq": 42,
  "quiesced_at": "2026-06-15T12:00:00.000000000Z",
  "checksum": 3456789012
}
```

The checksum is IEEE CRC32 over JSON-marshaled record with checksum set to 0.

**QuiesceID generation**: `NewQuiesceID()` reads 8 bytes from `crypto/rand` and
hex-encodes them (16 characters). It returns `(string, error)` — entropy failure
aborts the quiesce request entirely before the write gate is closed. The
`quiesceIDFn func() (string, error)` field on Server is injectable for tests.

**primary_base_url**: Captured from `s.Addr()` (the server's bound listener
address), not the config option. Quiesce is rejected if the listener is not yet
bound (address ends in `:0`).

## 9. Quiesce Persistence Ordering

1. Create `quiesce_record.json.tmp` in data dir.
2. Write JSON content.
3. `fsync` the temp file.
4. Close the temp file.
5. `rename` temp to `quiesce_record.json` (atomic on POSIX).
6. `fsync` the data directory (best-effort; no-op on macOS).

This is the same temp-fsync-rename-dirsync pattern used by
`ReplicationStateStore.AdvanceTo`.

## 10. Old-Primary Restart Behavior

On startup, `resolveRuntimeRole()` checks for `quiesce_record.json`. If found and
the node's config role is "primary":
- Set `quiesceState = "quiesced"`.
- Call `writeGate.Quiesce()` to re-apply the write fence.
- The node serves reads and replication log but rejects all writes forever.

The operator must delete the quiesce record file manually to clear the fence (an
explicit, auditable action).

## 11. Follower Promotion Preconditions (17 Checks)

1. Node's configured role is "follower" (runtime role must be "follower").
2. Node is not already promoted (no existing promotion record with different ID).
3. Node is not currently closing.
4. `confirm_old_primary_stopped` is true.
5. Quiesce record version == 1.
6. Quiesce record checksum is valid.
7. Quiesce record primary_node_id matches follower's configured primary node ID
   (if available) or is non-empty.
8. Quiesce record primary_base_url matches follower's configured primary_base_url.
9. Quiesce record quiesce_id is non-empty.
10. Quiesce record primary_latest_seq != math.MaxUint64 (would overflow).
11. Follower's last_applied_seq == quiesce record primary_latest_seq (exact match).
12. Follower's last_applied_seq > 0 OR quiesce record primary_latest_seq == 0
    (follower must have applied at least one entry if primary had entries).
13. No sync is currently in progress (syncInProgress flag is false).
14. No promotion is currently in progress (promotionState != "promoting").
15. Background sync worker is stoppable (not in a state that would interfere).
16. Quiesce record quiesced_at is a valid RFC3339 timestamp.
17. Node ID is non-empty.

## 12. Background-Worker Shutdown and Promotion Barrier

Before the role switch, the follower must safely stop all replication activity:

1. Acquire `promoteMu` — serializes concurrent promotion attempts.
2. Set `promotionBarrier atomic.Bool` to `true` — signals `SyncFromPrimary` to abort.
3. Drain active `SyncFromPrimary` calls: spin until `syncInProgress` is false, with
   a 5-second timeout.
4. Stop the background sync worker (`bgWorker.stop()`).
5. Proceed with promotion commit.

**Double-check barrier**: `SyncFromPrimary` checks `promotionBarrier` before AND
after acquiring `syncInProgress`. This prevents a sync that starts concurrently
just before step 2 from slipping through:

```
if s.promotionBarrier.Load() { return error }   // pre-CAS check
if !s.syncInProgress.CompareAndSwap(false, true) { return ErrSyncInProgress }
defer s.syncInProgress.Store(false)
if s.promotionBarrier.Load() { return error }   // post-CAS double-check
```

## 13. Manual-Sync Concurrency Behavior

During promotion, `syncInProgress` is checked as a precondition (check 13). If a
sync is in flight, promotion is rejected with `ErrPromotionNotReady`. After
promotion completes, the follower-to-primary role switch means
`POST /replication/sync` returns an error ("not a follower").

## 14. Promotion Persistence Ordering

1. Stop background sync worker.
2. Persist journal baseline using `CreateJournalBaseline(dir, inheritedLastSeq)`:
   - Idempotent: same value → OK; different value → `ErrJournalBaselineConflict`.
   - `math.MaxUint64` → `ErrJournalBaselineMaxUint64` (would overflow nextSeq).
   - This is the phase-1 commit: orphan-safe on crash (no promotion record yet).
3. Persist promotion record (`promotion_record.json`): contains quiesce_id,
   inherited_last_seq, new_role="primary". This is the commit point.
4. Open a new DurableLog in the data dir (it will find the baseline and start
   seq at base_seq + 1).
5. Switch runtime role to "primary".
6. Initialize write gate.

**Pre-commit failure** (fail before step 3): revert `promotionBarrier` to false,
set `promotionState` back to `""`, return error. Node is still a follower.

**Post-commit failure** (fail at or after step 3): do NOT revert. On restart,
promotion record is found; node completes promotion. If the same error recurs,
operator intervention is required.

**`CreateJournalBaseline` idempotency**: If promotion is retried (e.g., client
timeout → operator retries), the second call to `CreateJournalBaseline` with the
same `baseSeq` returns nil. No conflict.

Crash between steps 2 and 3: on restart, no promotion record exists; node remains
follower. Baseline file is orphaned but harmless.

Crash between steps 3 and 5: on restart, promotion record is found; node starts
as primary with the baseline. DurableLog opens, finds baseline, starts at the
correct seq.

## 15. Runtime-Role State

The `runtimeRole` field on Server (protected by `s.mu`) tracks the effective role:
- Initialized from config on startup.
- Overridden by promotion record if found on disk.
- Changed by successful `POST /replication/promote`.

All role checks in handlers use `runtimeState().role` (a snapshot taken under
`s.mu`) rather than reading `s.runtimeRole` bare. The `runtimeSnapshot` struct
captures all mutable fields atomically under a single lock acquisition.

## 16. Static-Config vs Durable-Role Precedence

On startup (`resolveRuntimeRole()`):
1. Load promotion record from data dir.
2. If found, cross-validate all fields:
   - `new_role == "primary"` (must be explicit)
   - `source_primary_node_id`, `source_primary_base_url`, `quiesce_id` non-empty
   - `inherited_last_seq != math.MaxUint64` (would overflow nextSeq)
   - `journal_baseline.json` exists
   - `baseline.base_seq == rec.inherited_last_seq` (must match exactly)
   - Any failure → `resolveRuntimeRole` returns an error; node refuses to open
3. If found and valid: `runtimeRole = "primary"`, `localRoleSource = "promotion_record"`.
4. If not found: `runtimeRole = cfg.Role`, `localRoleSource = "config"`.

**Orphan baseline** (`journal_baseline.json` exists, no `promotion_record.json`):
Safe to ignore. Promotion never committed. Node starts as follower. The baseline
is never used unless a promotion record is also present.

The promotion record always wins because it represents a durable state change that
the operator explicitly requested.

## 17. Promoted-Primary Journal Baseline

After promotion, a new DurableLog is opened. The journal baseline file
(`journal_baseline.json`) tells the log that seq 1..N already exist in the old
primary's journal. The new log's first append will be seq N+1.

```json
{
  "version": 1,
  "base_seq": 42,
  "checksum": 1234567890
}
```

## 18. Sequence Continuity After Promotion

If the old primary's final seq was N, the promoted primary's first write will be
seq N+1. This maintains a globally monotonic sequence across the promotion
boundary. Followers connecting to the new primary (in a future phase) would see
a continuous sequence.

## 19. Crash Recovery During Promotion (7 Crash Boundary Points)

1. Crash before bgWorker.stop(): Worker not stopped. On restart, node is still
   follower (no promotion record). Normal follower behavior resumes.
2. Crash after bgWorker.stop() but before journal baseline write: On restart,
   node is follower. Worker restarts normally.
3. Crash after journal baseline write but before promotion record: On restart,
   no promotion record. Node remains follower. Baseline file is orphaned but
   harmless.
4. Crash after promotion record write but before DurableLog open: On restart,
   promotion record found. Node starts as primary. DurableLog opens with baseline.
5. Crash after DurableLog open but before role switch in memory: On restart,
   same as 4 — promotion record drives startup.
6. Crash after role switch but before HTTP response: Client gets timeout/error.
   On restart, promotion record found — idempotent. Client can retry.
7. Crash after HTTP response: Clean promotion. On restart, promotion record
   found — node starts as primary.

## 20. Idempotent Repeated Requests

**Quiesce**: If the primary is already quiesced and the stored quiesce record
exists, return 200 with `idempotent: true` and the existing record. Do not create
a new record.

**Promote**: If the follower is already promoted with the same quiesce_id, return
200 with `idempotent: true`. If promoted with a different quiesce_id, return 409.

## 21. Failure Rollback Semantics

If promotion fails at any step before the promotion record is persisted, the node
remains a follower. No rollback is needed because no durable state has been changed
(or only the orphaned baseline file exists, which is harmless).

If promotion fails after the promotion record is persisted (e.g., DurableLog open
fails), the node is in an indeterminate state. On restart, the promotion record
will be found and the node will attempt to complete promotion. If DurableLog open
fails again, the node will fail to start (operator intervention required).

## 22. Stable HTTP Error Codes

All error responses from Phase 28 handlers use the `writeJSONError` helper, which
writes a JSON body with a machine-readable `code` field alongside a human-readable
`message`:

```json
{"code": "node_quiesced", "message": "node is quiesced: writes not allowed"}
```

Key codes:
- `node_quiesced` — 409 on any write after quiesce
- `wrong_role` — 403 when handler requires a role the node does not have
- `node_closing` — 503 when server is shutting down
- `quiesce_persistence_failed` — 500 if SaveQuiesceRecord fails
- `promotion_sequence_mismatch` — 409 if follower seq != quiesced seq
- `sync_in_progress` — 409 on concurrent sync

These codes are stable across releases: clients may match on `code` without
parsing `message`.

## 23. Controlled Split-Brain Assumptions

The operator asserts via `confirm_old_primary_stopped: true` that the old primary
is no longer accepting writes. ShardForgeDB does not verify this claim. If the
operator lies (old primary is still running), both nodes will accept writes and
data will diverge. This is a known limitation of manual failover without
distributed fencing.

## 24. Known Limitations

1. **Phase 26 crash window**: The gap between engine commit and journal append on
   the primary means a crash can lose the journal entry while the engine has the
   data. This is unchanged from Phase 26.
2. **No distributed fencing**: The old primary has no way to learn it has been
   replaced. Write-fencing relies on the quiesce record, which only works if the
   operator actually stopped the old primary before promoting the follower.
3. **No automatic follower re-routing**: Followers of the old primary do not
   automatically discover the new primary. Client routing must be changed manually.
4. **Single follower promotion**: Only one follower can be promoted. Multi-follower
   topologies require the operator to reconfigure remaining followers manually.
5. **No un-quiesce**: Once quiesced, a primary stays write-fenced forever. To
   revert, the operator must delete the quiesce record file and restart.

## 25. Safe and Unsafe Claims

### Safe Claims

- A quiesced primary will never accept writes again (within the same or future
  process lifetime), assuming the quiesce record file is not deleted.
- A promoted follower will continue at the correct sequence number (N+1) after
  inheriting the old primary's final seq N.
- Promotion is idempotent: repeating the same request returns the same result.
- All state transitions are persisted before being acknowledged to the operator.
- Crash at any point leaves the system in a well-defined state (follower or
  promoted primary, never both).

### Unsafe Claims (Explicitly NOT Made)

- We do NOT claim the old primary is actually stopped. That is the operator's
  responsibility.
- We do NOT claim zero data loss across the promotion boundary. The Phase 26
  crash window applies.
- We do NOT claim automatic client re-routing.
- We do NOT claim protection against operator error (e.g., promoting the wrong
  follower, promoting while old primary is still running).
