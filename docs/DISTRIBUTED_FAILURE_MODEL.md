# ShardForgeDB — Distributed Failure Model

**Phase 29 — Specification Only. No implementation in this phase.**

This document reasons about every failure mode the distributed v1 system must tolerate. For each failure: the safety property that must hold, the availability that may be lost, how recovery occurs, whether operator action is required, and the metric or alert emitted.

---

## Definitions

- **Safety:** A property that is never violated even in the presence of the failure (data is never lost or corrupted, split-brain never occurs).
- **Availability:** The ability to serve reads and writes during or after the failure.
- **Recovery:** The automatic or manual steps required to restore normal operation.
- **Operator action required:** Whether a human must intervene to restore safety or availability.
- **Metric / alert:** The observable signal that indicates the failure has occurred.

---

## Failure Mode Table

| # | Failure | Safety Property | Availability Lost | Recovery | Operator Required | Metric / Alert |
|---|---|---|---|---|---|---|
| 1 | Process crash | No committed data lost; no write acknowledged that is not committed | Writes to crashed shard leader unavailable until new leader elected; minority crash: zero availability impact | Raft elects new leader automatically; crashed node restarts and replays log | No (automatic for minority crash; No for majority crash with surviving majority) | `raft_leader_changes_total++`; alert: `raft_role{role="leader"}` drops to 0 for shard |
| 2 | Process pause (GC, OS scheduling, swap) | No split-brain; paused leader steps down when it detects its lease has expired or loses heartbeat responses | Writes stall for election timeout duration (150–300ms); reads from paused leader may be stale | Election fires; new leader serves writes; paused node re-joins as follower when it recovers | No | `raft_election_total++`; `raft_stale_leader_stepdown_total++` |
| 3 | Machine crash (power loss, kernel panic) | No committed data lost; WAL fsync ensures durability | Shard is unavailable until replacement node is provisioned (if no surviving majority) | Surviving majority elects new leader; crashed machine restarts and replays WAL + log | No for minority failure; operator provisions replacement node if majority lost | `node_health{node="X"}=0`; alert: shard majority unavailable |
| 4 | Machine restart | No data loss; WAL + log survive restart | Brief unavailability during restart (process startup time) | Node restarts, loads term+votedFor, replays log or installs snapshot, rejoins Raft group | No | `node_restarts_total++` |
| 5 | Network delay | No safety violation; delayed messages cause heartbeat timeouts | Possible spurious election if delay exceeds election timeout; transient write stall | Election resolves; delayed messages are processed after election if term matches | No | `raft_election_total++`; `raft_append_entries_latency_p99` |
| 6 | Dropped messages | No safety violation; Raft retries AppendEntries | Reduced throughput; possible election if heartbeats dropped repeatedly | Raft retries; election on persistent drops | No | `raft_append_entries_rejected_total++`; `network_dropped_packets_total` |
| 7 | Duplicate messages | No safety violation; Raft is idempotent for duplicate AppendEntries (same term, same index) | None | Duplicate entries are detected and discarded by the receiver's log check | No | `raft_duplicate_message_total++` |
| 8 | Reordered messages | No safety violation; Raft validates prevLogIndex and prevLogTerm before accepting entries | None; reordered entries are rejected and retried in correct order | Raft rejects out-of-order entries; leader retries from the correct nextIndex | No | `raft_append_entries_rejected_total++` |
| 9 | Asymmetric partition (A can send to B, B cannot send to A) | No split-brain; leader requires majority acknowledgment; one-way messages alone cannot form a quorum | Possible reduced throughput on partitioned side | Election resolves when one side achieves majority | No | `raft_election_total++`; network partition alert |
| 10 | Full partition (network split into two groups) | No split-brain; minority partition cannot commit writes (cannot achieve quorum); majority partition elects a new leader and continues | Minority partition cannot serve writes; reads on minority may be stale | Majority elects new leader; minority re-joins when partition heals | No | Alert: `raft_commit_index` stopped advancing on minority; partition alert |
| 11 | Stale leader (old leader that missed an election still receives client requests) | No safety violation; stale leader cannot commit new entries (it cannot achieve quorum in the new term) | Stale leader returns errors to clients; clients retry against new leader | Stale leader detects newer term in any message and steps down | No | `raft_stale_leader_stepdown_total++`; client sees `LEADER_CHANGED` error |
| 12 | Disk full | No corruption if WAL write fails before fsync; Append returns error; leader cannot accept new writes | Affected node cannot accept writes; shard may lose its leader | Remove old SSTable files (compaction); extend disk; node recovers after disk space is freed | Yes (operator must free disk space) | `disk_free_bytes{node="X"}` below threshold; alert: disk full |
| 13 | Short write (partial record written to disk) | No corruption; WAL detects partial tail on replay (CRC mismatch), truncates to last valid record | Brief unavailability; WAL replay may lose the partial record (which was never fsync'd and hence not committed) | Node restarts, WAL truncates partial tail, replays to last valid entry | No | `wal_partial_write_truncated_total++` |
| 14 | fsync failure | No committed data claimed; Append returns error if fsync fails; leader does not advance commitIndex | Write rejected; client receives error and may retry | Investigate disk hardware; restart node; WAL truncates to last valid fsync'd record | Yes (investigate disk hardware) | `wal_fsync_failure_total++`; alert: fsync error rate |
| 15 | Corrupt log (bit flip or corrupted file) | CRC-32 detects corruption; corrupted entry is not applied | Node cannot restart if corruption is in a committed region that is not covered by a prior snapshot | Restore from snapshot (InstallSnapshot from leader); or restore from backup | Yes (restore from snapshot or backup) | `wal_corruption_detected_total++`; alert: log corruption |
| 16 | Corrupt snapshot | CRC check on snapshot detects corruption before installation | Node cannot install snapshot; falls back to log replay if log is available | Retry InstallSnapshot from leader; if leader's snapshot is also corrupt, restore from backup | Yes (if all copies corrupt) | `snapshot_install_failure_total++`; alert: snapshot corruption |
| 17 | Clock skew (clocks differ across nodes) | No safety violation from clock skew alone; Raft uses logical time (terms), not wall-clock time | Lease-based reads may incorrectly serve stale data if clock skew exceeds lease duration; read-index is safe regardless of clock skew | Use NTP; use read-index protocol (not lease) for linearizable reads when clock accuracy is not guaranteed | No (but operator should monitor clock skew) | `clock_skew_ms` metric; alert: skew > 200ms |
| 18 | Clock rollback (wall clock jumps backward) | No safety violation if Raft uses terms; timeout logic using wall clock may behave incorrectly | Election timeout may fire prematurely or not at all | Use monotonic clock for all timeouts; wall clock only for human-readable timestamps | No | `clock_rollback_detected_total++` |
| 19 | Slow follower (follower lag > threshold) | No safety violation; slow follower does not affect commit (majority does not require it) | Read traffic on slow follower returns stale data | Leader sends InstallSnapshot if follower is too far behind | No | `raft_lag_entries{node="X"}` above threshold; alert: follower lag |
| 20 | Lagging replica (replica behind but not disconnected) | No safety violation; writes commit on the non-lagging majority | Lagging replica serves stale reads if follower reads are enabled | Replica catches up via AppendEntries batches | No | `raft_lag_entries`; `raft_applied_index` delta alert |
| 21 | Unavailable majority (more than half of a shard's replicas fail) | Safety: no new writes committed; no split-brain | Full write unavailability for the affected shard; reads may also be unavailable depending on read mode | Operator provisions replacement nodes; surviving minority applies new entries once quorum is restored | Yes (provision replacement nodes) | Alert: shard majority unavailable; `raft_commit_index` stopped |
| 22 | Concurrent membership change (two changes in flight) | Joint consensus prevents two independent majorities; at most one pending membership change | Possible delay in membership convergence | Second change is rejected until first is committed | No | `raft_membership_change_rejected_total++` |
| 23 | Migration interrupted at Plan stage | No state change; no safety risk | None | Retry migration from Plan | No | `migration_interrupted{stage="plan"}++` |
| 24 | Migration interrupted at Prepare stage | Partial destination engines exist but are empty | None (source shard still fully operational) | Rollback: delete destination engines; retry migration | No | `migration_interrupted{stage="prepare"}++` |
| 25 | Migration interrupted at Snapshot transfer | Partial snapshot on destination | None (source shard still fully operational) | Rollback: delete partial snapshot; retry from Snapshot stage | No | `migration_interrupted{stage="snapshot"}++` |
| 26 | Migration interrupted at Catchup stage | Destination learners partially caught up but not in Raft group | None (source shard still operational) | Rollback: remove destination learners; retry migration | No | `migration_interrupted{stage="catchup"}++` |
| 27 | Migration interrupted at Cutover stage | Epoch not yet incremented (atomic metadata commit) | Brief stall during cutover | Retry cutover; idempotent by epoch check | No | `migration_interrupted{stage="cutover"}++` |
| 28 | Router with stale metadata | Safety: write goes to wrong (stale) leader which returns NOT_LEADER | Slight latency increase for redirect | Router receives NOT_LEADER, refreshes cache, retries against new leader | No | `router_redirect_total++`; `router_cache_stale_total++` |
| 29 | Duplicate client requests (client retries) | Safety: duplicate request ID detected by state machine; previous result returned | None | State machine deduplication using request ID | No | `state_machine_duplicate_request_total++` |
| 30 | Client timeout after successful commit | Safety: write was committed; client may not know | Client may retry; duplicate detected and previous result returned | Client uses idempotency key on retry | No | `client_timeout_after_commit_total++` (instrumented at client layer) |
| 31 | Node identity reuse (new node gets same ID as crashed node) | Safety risk: old state from crashed node may be replayed with wrong term/log | Depends on recovery | Operator must ensure new node has a fresh data directory when reusing a node ID; startup validation checks for stale state | Yes (operator ensures clean data directory) | Alert: node ID reuse detected; `node_identity_reuse_detected_total++` |
| 32 | Version mismatch during rolling upgrade | Safety: on-wire protocol version must be negotiated; newer node must reject messages from older nodes that use incompatible formats | Rolling upgrade may cause transient leader instability | Use protocol version negotiation in all RPCs; define upgrade ordering; reject incompatible version messages | Yes (operator manages upgrade ordering) | `version_mismatch_rejected_total++`; alert: mixed-version cluster |

---

## Key Safety Guarantees (Summary)

1. **No committed data loss.** An entry acknowledged to the client is durable on a majority of nodes. Even if the leader crashes immediately after acknowledging, the entry will be present in the new leader's log.
2. **No split-brain.** At most one leader per term, enforced by the vote-granting rule (one vote per term, durable) and the log-up-to-date check.
3. **No stale epoch writes.** Shard migration fencing ensures that writes to a migrated shard with a stale epoch are rejected, preventing writes from reaching the wrong replica set.
4. **No rolled-back follower reads.** Follower reads are bounded by the follower's applied index. A follower never returns a value that was committed and then overwritten — it may only return a value that was not yet applied.

## Availability Trade-offs (Summary)

- **Minority failure:** zero impact on availability.
- **Majority failure:** writes unavailable; reads may be unavailable. Operator must restore majority.
- **Full partition:** minority partition cannot serve writes; majority continues normally.
- **Leader election:** writes stall for election timeout duration (typically < 500ms).

---

## Operator Runbook (High-Level)

| Situation | Required Action |
|---|---|
| Disk full on a shard node | Free disk space; node recovers automatically |
| Majority of a shard unavailable | Provision replacement nodes with clean data dirs; they rejoin as learners |
| Log corruption detected | Install snapshot from leader; or restore from backup |
| Node identity reuse | Provide clean data directory; do not reuse stale WAL/log files |
| Rolling upgrade version mismatch | Follow documented upgrade ordering; upgrade nodes one at a time |
| Clock skew > 200ms | Investigate NTP; do not use lease reads until clock is corrected |
