# Replication Durability Design — ShardForgeDB Phase 26

**Scope:** Durable replication state and restart recovery for the explicit pull-based replication
introduced in Phase 25. This document captures the twelve design decisions made before any code
was written.

**Non-goals (unchanged from Phase 25):** No Raft, no consensus, no quorum, no leader election,
no automatic failover, no automatic background replication, no distributed transactions, no shard
migration. Replication is still operator-triggered via `POST /replication/sync`.

---

## Q1. Why can't we just use the engine WAL for replication history?

The engine WAL is not suitable for replication history because it is **rotated and deleted on
MemTable flush**. After a flush the old WAL segments are removed; a follower that was offline
during that flush would find the WAL entries it needs gone. The replication log must outlive WAL
rotation and is an independent concern from crash recovery of the local engine.

**Decision:** maintain a dedicated binary journal file (`{dataDir}/replication.journal`) that is
never rotated by normal engine operations.

---

## Q2. What is the binary journal record format?

Each record is written as a self-describing, CRC-verified binary frame:

```
offset  0 : length  uint32 LE  — total bytes after the length field itself
offset  4 : crc32   uint32 LE  — IEEE CRC32 of bytes [8 .. 4+length)
offset  8 : seq     uint64 LE  — monotonically increasing sequence number (starts at 1)
offset 16 : op      uint8      — 1 = put, 2 = delete
offset 17 : keyLen  uint32 LE
offset 21 : valLen  uint32 LE
offset 25 : tsNano  uint64 LE  — Unix nanoseconds UTC when the primary wrote the entry
offset 33 : key     [keyLen]byte
offset 33+keyLen : val [valLen]byte
```

`length = 25 + keyLen + valLen` (the 4 CRC bytes + 8 seq + 1 op + 4 keyLen + 4 valLen + 8 tsNano
+ key + val). The length field itself (4 bytes) is excluded from the length value.

CRC covers all bytes from offset 8 onward (seq, op, keyLen, valLen, tsNano, key, val).

---

## Q3. How do we handle a partial/corrupt record at the end of the journal?

During replay (on open):

| Condition | Interpretation | Action |
|---|---|---|
| EOF at start of record | clean stop | stop, last complete record is the tail |
| `io.ErrUnexpectedEOF` reading length or body | process crashed mid-write | stop at this record, warn, truncate |
| CRC mismatch | corrupt record | return `ErrCorruptedJournal`; do not use the record |

The process that wrote the partial record crashed before finishing; we stop at that boundary and
the journal is effectively truncated to the last fully written record. We do **not** attempt to
repair or skip corrupt mid-sequence records; a corrupt interior record is a hard error.

---

## Q4. What is the write ordering between engine write and journal append?

```
engine.Put(key, val)    ← durable (WAL + MemTable)
journal.Append(op, key) ← durable (journal fsync or OS flush)
```

There is a documented **crash window** between the two:
- Crash after `engine.Put` but before `journal.Append`: engine has the data; journal does not.
  On restart the primary serves the data from the engine but the entry is invisible to followers
  via the replication log. The mutation is effectively "invisible to replication" — it happened
  locally but was never replicated and was never committed to the journal.
- Crash after `journal.Append` but before `engine.Put`: impossible because `engine.Put` is first.

This ordering guarantees **journal entries ⊆ engine state**: the engine always has at least what
the journal claims. Journal entries never refer to data that the engine has not written.

**Decision:** write-after-commit ordering (engine first, then journal). Document the crash window
explicitly. Do not attempt write-ahead semantics for the journal in Phase 26.

---

## Q5. How does the follower persist its replication cursor?

The follower writes a versioned, identity-bound JSON file `{dataDir}/replication_state.json`:

```json
{
  "version": 1,
  "follower_node_id": "follower-1",
  "primary_url": "http://primary:8080",
  "last_applied_seq": 42,
  "updated_at": "2026-06-13T12:00:00.000000000Z",
  "checksum": 1234567890
}
```

`checksum` is the IEEE CRC32 over all fields except itself:
`checksumState(version, follower_node_id, primary_url, last_applied_seq, updated_at)`.
This lets us detect a half-written or tampered state file without external dependencies.

**Identity binding:** the state file is bound to a specific follower node ID and primary URL.
On load, both are validated against the current node's configuration. Mismatches return
`ErrFollowerIdentityMismatch` or `ErrPrimaryIdentityMismatch` — never silently reset to zero.
This prevents a follower accidentally loading state from a different node or pointing at the
wrong primary after an operator error.

**Atomic write protocol:**
1. Write to `{dataDir}/replication_state.json.tmp`
2. `fsync` the temp file (durable on all platforms)
3. `os.Rename` temp → final (atomic on POSIX)
4. `fsync` the parent directory (durable rename on Linux; no-op on macOS)

On load: if the state file does not exist, `lastAppliedSeq = 0` (fresh follower, pull everything).
A corrupt checksum returns `ErrCorruptedState`. An unrecognised version field returns
`ErrUnsupportedStateVersion`.

**Sequence monotonicity:** `AdvanceTo(newSeq)` returns `ErrInvalidSeqRegression` if `newSeq` is
strictly less than the current cursor. Equal is idempotent (safe on replay). This prevents
silent cursor rollbacks from logic errors in the caller.

---

## Q6. When is the cursor persisted on the follower?

After every **complete batch** returned by a single `PullEntries` call, once all entries in the
batch have been applied to the local engine. We do **not** persist per-entry (too expensive) and
we do **not** hold it until the next sync (would lose progress on crash).

Crash between two entries in a batch: the state file still holds the cursor from the previous
sync. On restart the follower re-requests from the stored cursor; it may re-fetch and re-apply
some entries already in the engine. Because `ApplyReplicationEntries` is **idempotent** (entries
with `Seq <= lastApplied` are skipped), this is safe.

---

## Q7. What is a replication gap and when does it occur?

A **replication gap** occurs when the follower's cursor (`after`) points to a sequence position
that is no longer in the primary's journal (entry was never written due to a crash, or has been
trimmed in a future phase).

Gap condition: `after + 1 < firstAvailableSeq` on the primary.

In Phase 26 the primary journal is never trimmed, so the first available sequence is always 1
once the first write occurs. A gap can only arise if the journal file was manually deleted or
corrupted. The gap detection is implemented now so the code path exists and is tested, even
though it is rarely triggered in Phase 26.

---

## Q8. How is a replication gap reported?

The primary's `GET /replication/log` handler returns **HTTP 409 Conflict** with a structured body:

```json
{
  "error": "replication gap: requested after 5 but first available is 10 (latest=20)",
  "gap": {
    "requested_after": 5,
    "first_available_seq": 10,
    "latest_seq": 20
  },
  "node_id": "leader"
}
```

The follower's `Replicator.PullEntries` decodes the 409 and returns `*ReplicationGapError`.
`SyncFromPrimary` surfaces this error to `handleReplicationSync`, which returns **HTTP 409** to
the operator with the same `gap` struct. The operator must then take manual recovery action (e.g.
snapshot restore, wipe follower state, or re-seed from the primary).

---

## Q9. How does the primary survive a restart?

On `OpenDurableLog(dataDir)`:
1. Open (or create) `replication.journal` in append+read mode.
2. Replay all records to rebuild the in-memory index `[(seq, fileOffset)]`.
3. Set `nextSeq = lastReplayedSeq + 1` (or 1 if empty).

The in-memory index allows `EntriesAfter` to seek directly to the correct file offset rather than
scanning from the beginning.

After restart the primary's `GET /replication/log` serves the full history back to seq 1.
Followers that missed entries while the primary was down can sync them after restart.

---

## Q10. How does the follower survive a restart?

On `NewReplicationStateStore(dataDir)`:
1. Load `replication_state.json` → `lastAppliedSeq`.
2. If file missing → `lastAppliedSeq = 0`.
3. Store `lastAppliedSeq` as the initial `s.lastApplied` in the Server.

The follower's next `POST /replication/sync` will request `after=lastAppliedSeq`, which correctly
re-requests only entries it has not yet applied.

---

## Q11. What is the `DurableLog` interface and how does it relate to the existing `Log`?

`DurableLog` is a new type in `internal/replnet`. It has the same public API surface as `Log`
(`Append`, `EntriesAfter`, `Stats`, `Close`) plus two Phase 26 additions:
- `FirstAvailableSeq() uint64` — the lowest seq in the journal (for gap detection)
- `Stats()` returns `DurableLogStats` (extends `LogStats` with `FirstAvailableSeq` and `JournalBytes`)

`internal/node/Server` replaces `replLog *replnet.Log` with `durableLog *replnet.DurableLog`.
The existing `Log` type is retained (used in tests and potentially other contexts).

---

## Q12. What changes to the HTTP API surface does Phase 26 introduce?

| Endpoint | Change |
|---|---|
| `GET /replication/log` | Can now return 409 with `gap` struct when follower is behind |
| `POST /replication/sync` | `sync_result.replication.durable=true` in response |
| `GET /replication/status` | Now includes `durable_log_stats` for primary; `state_persistent=true` for follower |

All existing Phase 25 fields are preserved. Phase 26 adds fields; it does not remove or rename any.

---

## Summary of new files

| File | Purpose |
|---|---|
| `internal/replnet/durable_log.go` | Binary journal writer/reader for primary |
| `internal/replnet/durable_log_test.go` | Unit tests for DurableLog |
| `internal/replnet/state_store.go` | Follower cursor persistence (atomic JSON file) |
| `internal/replnet/state_store_test.go` | Unit tests for ReplicationStateStore |
| `internal/replnet/errors.go` | New: ErrReplicationGap, ErrCorruptedJournal, ErrCorruptedState, ErrInvalidSeqRegression |
| `internal/replnet/types.go` | New: ReplicationGapError, DurableLogStats |
| `internal/node/server.go` | Use DurableLog + StateStore instead of in-memory Log |
| `internal/node/handlers.go` | 409 gap response in handleReplicationLog |
| `internal/node/replication_phase26_test.go` | Node-level Phase 26 tests |
| `scripts/repl_restart_demo_{up,smoke,down}.sh` | Live demo proving restart recovery |
