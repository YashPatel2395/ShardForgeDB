# ShardForgeDB — High-Level Architecture Design

> **Status:** WAL (`internal/wal`) is implemented as of Phase 2.
> All other components described here are intended design only — not yet implemented.

---

## Goals

1. **Correctness first** — data must never be lost or corrupted.
2. **Explainability** — every design choice is documented with trade-offs.
3. **Incrementally testable** — each layer is independently testable before the next begins.
4. **Production-realistic** — no demo shortcuts; design decisions mirror real systems (RocksDB, TiKV, Milvus).

---

## Storage Layer

### Write-Ahead Log (WAL)

**Phase 2 — Implemented** in `internal/wal`.

All mutations are first written to an append-only WAL before being acknowledged. This guarantees durability in the event of a process crash. The engine (not yet implemented) will replay the WAL on startup to recover any writes not yet flushed to an SSTable.

#### Record Format

Each record is encoded as a fixed-size frame header followed by a variable-length body. All integers are little-endian.

```
┌─────────┬─────────┬─────────┬──────┬────────┬──────────┬─────────────────┐
│ length  │  crc32  │   seq   │ type │ keyLen │ valueLen │  key … value …  │
│ uint32  │ uint32  │ uint64  │uint8 │ uint32 │  uint32  │   (raw bytes)   │
│  4 B    │  4 B    │  8 B    │ 1 B  │  4 B   │   4 B    │   variable      │
└─────────┴─────────┴─────────┴──────┴────────┴──────────┴─────────────────┘
```

- `length` — byte count of everything after the `crc32` field (body only).
- `crc32` — IEEE CRC-32 of the body (`seq` through `value` bytes inclusive).
- `seq` — monotonically increasing sequence number assigned by `Append`.
- `type` — `1 = RecordPut`, `2 = RecordDelete`.
- `keyLen` / `valueLen` — byte lengths of the key and value payloads.

#### Durability Behaviour

- `SyncOnWrite: true` — calls `fsync` after every `Append`. Guarantees the record is on stable storage before the call returns.
- `SyncOnWrite: false` (default for tests) — the OS may buffer writes. Use `true` in production.

#### Corruption Handling

- **Checksum mismatch at a non-tail record** → `ErrCorruptRecord` (replay aborted).
- **Truncated final record** (partial header or body at EOF) → treated as a clean stop. This is normal after a crash-during-write scenario.
- **Checksum mismatch at the tail** → treated as a partial tail write (clean stop), because there is no following record to confirm mid-file corruption.

#### Known Limitations (Phase 2)

- Single WAL file only — no segment rotation.
- No WAL compaction / GC.
- No group commit (each `Append` is an independent write).
- No compression or encryption.
- WAL is not yet wired to the MemTable or Engine (future phases).

### MemTable

The MemTable is an in-memory, sorted write buffer backed by a skip list (or red-black tree). Reads check the MemTable before going to disk.

- Bounded by a configurable size limit.
- When full, the MemTable is frozen and flushed to disk as a new SSTable.
- Concurrent reads and writes are supported through fine-grained locking or lock-free structures.

### SSTables (Sorted String Tables)

SSTables are immutable, sorted, on-disk files produced by MemTable flushes.

- Each SSTable has a data block, index block, and metadata block.
- SSTables are organised into levels (L0–LN) following an LSM-tree design.
- L0 allows overlapping key ranges; deeper levels are non-overlapping.

### Bloom Filters

Each SSTable carries a per-key Bloom filter. Before reading an SSTable, the engine checks the filter to skip files that definitely do not contain the requested key.

- Reduces read amplification for missing keys.
- Target false-positive rate: ≤ 1%.

### Compaction

Compaction merges SSTables to reclaim space and bound read amplification.

- Levelled compaction strategy (similar to LevelDB / RocksDB).
- Compaction runs in background goroutines, rate-limited to avoid write stalls.
- Tombstone entries are purged during compaction.

---

## Engine Layer

The `engine` package composes WAL + MemTable + SSTables into a unified key-value interface:

```
Put(key, value) error
Get(key) (value, error)
Delete(key) error
Scan(start, end) (Iterator, error)
```

Reads follow this path:
1. Check active MemTable.
2. Check immutable (flushing) MemTable, if any.
3. Consult each SSTable level, newest first, using Bloom filters to skip.

---

## Vector Search Layer

The `vector` package will implement an Approximate Nearest Neighbour (ANN) index.

- Candidate algorithm: HNSW (Hierarchical Navigable Small World).
- Interface: `Insert(id, vector)`, `Search(query, topK) ([]Result, error)`.
- Vectors are stored separately from key-value data but use the same WAL for durability.

---

## Cluster Layer

### Sharding

Key space is partitioned across nodes using consistent hashing.

- Virtual nodes reduce hotspot risk during node additions/removals.
- The shard map is stored in a distributed metadata service.

### Replication

Each shard is replicated across N nodes using a leader/follower model.

- Leader handles all writes; followers serve reads (with optional staleness).
- Automatic failover on leader failure is planned for a later sub-phase.
- Log entries correspond to WAL entries, giving a natural replication unit.

> **Note on Raft:** Full Raft-compatible consensus (leader election, term
> handling, replicated log, commit index, voting, and failover) is a future
> candidate algorithm, not the committed implementation. It will not be claimed
> as implemented until leader election, term handling, replicated log, commit
> index, voting, failover, and tests are all in place.

---

## Data Flow Diagram

```
Write path:
  Client → WAL (fsync) → MemTable → [flush] → SSTable L0 → [compact] → SSTable L1+

Read path:
  Client → MemTable → SSTable L0 (Bloom) → SSTable L1+ (Bloom) → not found
```

---

## Trade-offs Acknowledged

| Decision | Trade-off |
|----------|-----------|
| LSM-tree over B-tree | Write-optimised; read amplification requires Bloom + cache |
| Levelled compaction | Predictable read performance; higher write amplification than tiered |
| Leader/follower replication | Simple to implement and reason about; Raft-compatible consensus is a future candidate if strong consistency is required |
| HNSW for vector search | High recall; memory-intensive; no native disk-resident variant |

---

*This document will be updated as each phase is implemented and design decisions are validated.*
