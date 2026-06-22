# ShardForgeDB — Distributed Consistency Model

**Phase 29 — Specification Only. No implementation in this phase.**

This document defines the exact consistency contracts for ShardForgeDB v1.0-distributed. Every contract is stated in terms of the behaviors visible to clients, not internal implementation details. This document is the authoritative reference for what clients can rely on.

---

## 1. Terminology

- **Proposed write:** a write sent by the client to the shard leader.
- **Acknowledged write:** a write for which the server has returned a success response to the client.
- **Committed write:** a write that has been appended to the Raft log and confirmed by a majority of replicas.
- **Applied write:** a committed write that has been executed by the state machine (engine) on a specific replica.
- **Linearizable:** a consistency level where every operation appears to take effect instantaneously at some point between its invocation and its response; the history is equivalent to some sequential history consistent with real-time order.
- **Stale follower read:** a read served by a follower whose applied index is behind the leader's committed index.
- **Read-your-writes:** a session guarantee where a client that writes a value and then reads it (in any subsequent operation) always sees its own write.
- **Monotonic reads:** a session guarantee where if a client reads a value at time T, it never sees an older value in any subsequent read.

---

## 2. Acknowledged Write Contract

**Contract:** Every acknowledged write is a committed write.

- The server returns a success response to the client only after the write has been committed by a Raft majority.
- An acknowledged write is never lost, even if the leader crashes immediately after acknowledging.
- An acknowledged write is visible to all subsequent linearizable reads.

**Deviation from this contract is never permitted.** There is no "fire-and-forget" write mode that claims acknowledgment before commit.

---

## 3. Committed Write Contract

**Contract:** A committed write is durable on a majority of replicas.

- Committed writes survive the failure of any minority of replicas (up to (N-1)/2 failures for N replicas).
- Committed writes are never rolled back by a subsequent leader election.
- Committed writes are eventually applied to all non-crashed replicas.

---

## 4. Applied Write Contract

**Contract:** An applied write is visible to reads on the replica where it was applied.

- Applied writes are ordered: if write W1 is committed before write W2, then on any replica, W1 is applied before W2.
- The applied index on a replica is monotonically increasing.
- Replicas may apply writes at different rates; the leader is never behind any replica.

---

## 5. Linearizable Read Contract

**Contract:** A linearizable read returns the value of the most recently committed write for the queried key, as of the time the read was issued.

- The leader executes the read-index protocol (or a lease-based protocol with verified clock accuracy) before serving the read.
- A linearizable read never returns a value that was committed before the read but not yet visible at the read time — it must be the most recent committed value.
- A linearizable read never returns a value that was not yet committed at the time the read was served.

**Default for all leader reads:** linearizable.

---

## 6. Stale Follower Read Contract

**Contract:** A follower read returns a value consistent with the follower's current applied index. It may be stale relative to the leader's committed index.

- Clients must explicitly request a follower read; follower reads are never the default.
- The staleness bound is the follower's replication lag (lag_entries * average entry apply time).
- A follower read never returns a value that was committed and then overwritten on the leader — it only returns values that were committed at some point in the past.
- A follower read never returns a value that was not yet committed (i.e., a follower never reads ahead of its own applied state).

**Explicit contract violation:** it is never acceptable to advertise follower reads as linearizable.

---

## 7. Read-Your-Writes Contract

**Contract:** A client that performs a write and then performs a read (in the same session) always sees its own write.

- Implemented by the client tracking the committed sequence number of its last write.
- On a subsequent read, the client sends the sequence number to the router.
- The router forwards the read only to a replica whose applied index is >= the client's sequence number.
- If no such replica is immediately available, the read blocks until one becomes available (or times out).

**Availability trade-off:** if all replicas for a shard are lagging (e.g., after a partition), a read-your-writes read may block. The client may choose to degrade to a stale read after a configurable timeout.

---

## 8. Monotonic Reads Contract

**Contract:** A client that reads a value V at sequence number S never sees a value that was committed before S in a subsequent read.

- Implemented by the client tracking the applied index of the last read it received.
- Subsequent reads are directed to a replica whose applied index is >= the client's last read index.
- This prevents a client from reading newer data and then reading older data (e.g., by being redirected to a more lagging replica).

---

## 9. Duplicate Request Contract

**Contract:** Submitting the same request (same idempotency key) multiple times produces the same result as submitting it once.

- Each write request includes a client-assigned idempotency key (client ID + request ID).
- The state machine tracks the last completed request per client.
- A duplicate request is detected by the state machine and the previous result is returned without re-applying the write.
- The idempotency window is configurable (default: last N requests per client, or a time-based TTL).
- Outside the idempotency window, duplicate detection is not guaranteed. Clients must not reuse request IDs across sessions without a new client ID.

---

## 10. Retry After Timeout Contract

**Contract:** A client that times out waiting for a write response may safely retry using the same idempotency key.

- If the write was committed before the timeout, the retry is detected as a duplicate and the previous result is returned.
- If the write was not committed before the timeout (e.g., the leader crashed before committing), the retry is treated as a new write and committed normally.
- The client is indistinguishable from the server's perspective; the idempotency key makes the retry safe in both cases.

---

## 11. Membership Change Contract

**Contract:** Membership changes (adding or removing nodes) do not cause committed data loss or split-brain.

- Joint consensus (Phase 36) ensures that during a membership change, both the old majority and the new majority must agree on new entries.
- No membership change may proceed if the Raft group is already in a degraded state (e.g., below quorum).
- At most one membership change may be pending at a time.

---

## 12. Shard Migration Cutover Contract

**Contract:** The cutover from the source shard to the destination shard is atomic from the perspective of the metadata group.

- Before cutover: all reads and writes go to the source shard.
- After cutover: all reads and writes go to the destination shard.
- During cutover: the metadata group commits a single atomic entry that changes the assignment. There is no window where both the source and destination serve the shard simultaneously without epoch fencing.
- Epoch fencing ensures that source replicas reject writes after the epoch is incremented.

---

## 13. Vector Query During Migration Contract

**Contract:** A vector search query during a shard migration returns correct results, possibly with a brief period of duplicate-candidate results that are deduplicated.

- During the Catchup stage: the coordinator queries both source and destination shards; deduplication by vector ID prevents spurious duplicate results.
- During Cutover: the coordinator follows the epoch and queries the new shard assignment immediately after the epoch is updated.
- No vector results are silently dropped during migration.

---

## 14. Partial Vector-Shard Failure Contract

**Contract:** If a subset of shards is unavailable during a vector search, the coordinator returns the results from the available shards along with an explicit indication that the results are partial.

- The coordinator does not return a partial result silently as if it were a complete result.
- The client receives an explicit `partial_result: true` field in the response along with the list of unavailable shards.
- Availability is sacrificed for honesty: a partial result is returned rather than blocking indefinitely.

---

## 15. Unavailable Quorum Contract

**Contract:** When a shard's quorum is unavailable, writes to that shard return an explicit error. Reads may be served from the surviving minority under the follower-read contract (with explicit staleness indication) if the client opts in.

- The system never silently drops writes when quorum is unavailable.
- The system never claims a write is committed when it is not.
- If the client does not opt into stale follower reads, the system returns an error when quorum is unavailable.

---

## 16. CAP and Partition Handling

ShardForgeDB v1.0-distributed is a **CP system** under network partitions (Consistency + Partition-tolerance, availability sacrificed):

- Under a network partition that prevents a shard from forming a majority, **writes are unavailable**.
- The minority partition does not serve writes and does not acknowledge writes.
- The majority partition continues to serve both reads and writes.
- Follower reads on the minority partition may be served (with stale data) if the client explicitly opts in.

**ShardForgeDB does not claim CA (Consistency + Availability) under partitions.** This is explicitly impossible per the CAP theorem when network partitions must be tolerated.

---

## 17. Consistency Level Summary Table

| Operation | Default Consistency | Opt-in Weaker Consistency | Safety Guarantee |
|---|---|---|---|
| Write (Put/Delete) | Majority-committed (linearizable) | None | No acknowledged write is ever lost |
| Leader Read (Get) | Linearizable | Stale follower read (explicit) | Linearizable: most recent committed value |
| Follower Read (Get) | Not available by default | Stale read (explicit opt-in) | Never returns uncommitted or rolled-back values |
| Vector Search (leader) | Linearizable per shard | Stale follower read (explicit) | Results consistent with committed vector state |
| Vector Search (partial shard failure) | Partial result with indication | None | Never silently drops shards |
| Read-your-writes | Session guarantee (opt-in) | Degrade to stale after timeout | Client always sees its own writes within session |
| Monotonic reads | Session guarantee (opt-in) | None | Applied index is monotonically increasing per client |

---

## 18. Implementation Obligations (per Phase)

| Contract | Phase that implements it |
|---|---|
| Majority-committed writes | Phase 33 |
| Linearizable leader reads (read-index) | Phase 34 |
| Client idempotency (duplicate request detection) | Phase 34 |
| Follower reads with applied-index tracking | Phase 34 |
| Read-your-writes and monotonic reads | Phase 34 |
| Membership change safety (joint consensus) | Phase 36 |
| Shard migration cutover atomicity | Phase 40 |
| Epoch fencing | Phase 38 + 40 |
| Partial vector-shard failure indication | Phase 43 |
