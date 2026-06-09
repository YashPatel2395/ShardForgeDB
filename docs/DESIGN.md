# ShardForgeDB — High-Level Architecture Design

> **Status: Intended design only. Nothing below this line is implemented.**
> This document describes where the project is going, not where it is.

---

## Goals

1. **Correctness first** — data must never be lost or corrupted.
2. **Explainability** — every design choice is documented with trade-offs.
3. **Incrementally testable** — each layer is independently testable before the next begins.
4. **Production-realistic** — no demo shortcuts; design decisions mirror real systems (RocksDB, TiKV, Milvus).

---

## Storage Layer

### Write-Ahead Log (WAL)

All mutations are first written to an append-only WAL before being acknowledged. This guarantees durability in the event of a process crash.

- Sequential writes for maximum throughput.
- Entries include a CRC checksum for integrity verification.
- Recovery scans the WAL on startup and replays any uncommitted entries.

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
