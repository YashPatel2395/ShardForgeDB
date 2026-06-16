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
active  ──[POST /replication/quiesce]──>  quiescing (transient)  ──>  quiesced
```

- **active**: Normal operation. Writes accepted.
- **quiescing**: Transient state while the write gate drains in-flight writes.
  Not externally observable (entire transition happens within a single HTTP
  request handler).
- **quiesced**: Terminal state. All writes rejected with HTTP 409. Reads still
  served. Replication log still available for followers to catch up.

The state machine is one-way: there is no un-quiesce operation. Once quiesced, the
node remains write-fenced forever (including across restarts).

## 6. Write-Handler Synchronization: RWMutex Write Gate

A `writeGate` type uses `sync.RWMutex` to synchronize writes and quiesce:

- Each write handler calls `writeGate.Enter()` which acquires `RLock`.
- `POST /replication/quiesce` calls `writeGate.Quiesce()` which acquires `Lock`.
- The exclusive Lock blocks until all shared RLock holders release, ensuring all
  in-flight writes complete before quiesce returns.
- After Quiesce(), an `atomic.Bool` flag causes all future `Enter()` calls to
  return `ErrNodeQuiesced` immediately (fast path, no lock contention).

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

## 12. Background-Worker Shutdown Ordering

Before the role switch, the follower must stop its background sync worker:
1. Call `bgWorker.stop()` — cancels context, waits for goroutine to exit.
2. Only after the worker is fully stopped, proceed with promotion.

This prevents the worker from attempting a sync against the (now-stopped) primary
during promotion.

## 13. Manual-Sync Concurrency Behavior

During promotion, `syncInProgress` is checked as a precondition (check 13). If a
sync is in flight, promotion is rejected with `ErrPromotionNotReady`. After
promotion completes, the follower-to-primary role switch means
`POST /replication/sync` returns an error ("not a follower").

## 14. Promotion Persistence Ordering

1. Stop background sync worker.
2. Persist journal baseline (`journal_baseline.json`): `base_seq = inherited_last_seq`.
3. Persist promotion record (`promotion_record.json`): contains quiesce_id,
   inherited_last_seq, new_role="primary".
4. Open a new DurableLog in the data dir (it will find the baseline and start
   seq at base_seq + 1).
5. Switch runtime role to "primary".
6. Initialize write gate.

Crash between steps 2 and 3: on restart, no promotion record exists; node remains
follower. Baseline file is harmless (ignored without promotion record).

Crash between steps 3 and 5: on restart, promotion record is found; node starts
as primary with the baseline. DurableLog opens, finds baseline, starts at the
correct seq.

## 15. Runtime-Role State

The `runtimeRole` field on Server (protected by `s.mu`) tracks the effective role:
- Initialized from config on startup.
- Overridden by promotion record if found on disk.
- Changed by successful `POST /replication/promote`.

All role checks in handlers use `runtimeRole` rather than `cfg.Role`.

## 16. Static-Config vs Durable-Role Precedence

On startup:
1. Load promotion record from data dir.
2. If found and valid: `runtimeRole = "primary"`, `localRoleSource = "promotion_record"`.
3. If not found: `runtimeRole = cfg.Role`, `localRoleSource = "config"`.

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

## 22. Controlled Split-Brain Assumptions

The operator asserts via `confirm_old_primary_stopped: true` that the old primary
is no longer accepting writes. ShardForgeDB does not verify this claim. If the
operator lies (old primary is still running), both nodes will accept writes and
data will diverge. This is a known limitation of manual failover without
distributed fencing.

## 23. Known Limitations

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

## 24. Safe and Unsafe Claims

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
