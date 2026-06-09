# ShardForgeDB — High-Level Architecture Design

> **Status:** WAL (`internal/wal`), MemTable (`internal/memtable`), and SSTable (`internal/sstable`) are implemented as of Phase 4.
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

The distinction between a partial tail and corruption is based on which bytes are present, not on position:

- **Partial header** — `io.ReadFull` returns `io.EOF` or `io.ErrUnexpectedEOF` while reading the 8-byte frame header → clean stop. The process crashed before it could write a complete frame header.
- **Partial body** — `io.ReadFull` returns `io.EOF` or `io.ErrUnexpectedEOF` while reading the declared body → clean stop. The process crashed after writing the header but before finishing the body.
- **Complete body, checksum mismatch** → `ErrCorruptRecord`, always, regardless of whether the record is the last one in the file. A crashed write leaves bytes *absent*, not bytes *present-but-wrong*. A complete record with the wrong CRC is data corruption, not a partial tail.
- **Frame header claims `bodyLen > MaxRecordSize`** → `ErrCorruptRecord` immediately, before any allocation. Prevents OOM from a corrupt length field.
- **Frame header claims `bodyLen < bodyFixedSize`** → `ErrCorruptRecord` immediately. No valid `Append` can produce a body smaller than the fixed overhead.

#### Known Limitations (Phase 2)

- Single WAL file only — no segment rotation.
- No WAL compaction / GC.
- No group commit (each `Append` is an independent write).
- No compression or encryption.
- WAL is not yet wired to the MemTable or Engine (future phases).

### MemTable

**Phase 3 — Implemented** in `internal/memtable`.

The MemTable is an ordered in-memory write buffer that holds the most recent version of each key. It sits directly above the WAL in the storage stack: writes are first recorded in the WAL for durability, then applied to the MemTable for fast in-memory reads.

#### Data Structure

Two complementary structures are maintained under a single `sync.RWMutex`:

- `map[string]Entry` — O(1) lookup by string key.
- `[]string` — sorted key slice for ordered range scans (lexicographic order).

Insertions maintain the sorted slice via binary search (`sort.SearchStrings`, O(log n)) to find the insert position, followed by a slice shift (O(n)). This gives O(n) per insert and O(n²) for a random bulk load of n keys. A skip list will be evaluated in a later phase if profiling shows this as a bottleneck.

#### Ordering and Tombstones

- All keys are stored in lexicographic (byte) order; `Scan(start, end)` returns entries in that order.
- `Delete` does not remove a key — it records an `EntryDelete` tombstone. The engine (not yet implemented) uses tombstones to shadow older versions of a key in lower SSTable levels during reads and compaction.
- `Get` returns tombstones; callers must check `entry.Kind`.

#### Concurrency

- `Put`, `Delete`, `Get`, `Scan`, `Len`, `ApproxBytes`, `ShouldFlush` are all safe for concurrent use.
- `Get` and `Scan` hold only a read lock; multiple readers may proceed in parallel.
- `Put` and `Delete` hold an exclusive write lock.
- All returned entries are deep copies; caller mutation does not affect stored state.

#### Size Accounting

`ApproxBytes` tracks: `len(key) + len(value) + entryOverhead` (64 B) per entry. The overhead is a fixed estimate covering the map bucket, sorted slice element, and `Entry` struct fields. It is deterministic but not an exact runtime measurement.

`ShouldFlush` returns `true` when `ApproxBytes >= MaxBytes` (default: 64 MiB).

#### Known Limitations (Phase 3)

- O(n) per insert; O(n²) bulk load. Skip list deferred to profiling phase.
- Single `sync.RWMutex` — no per-key or per-shard locking.
- No immutable (flushing) MemTable handoff — flush path not yet wired.
- MemTable is not yet connected to the WAL or Engine; data is lost on process restart.

### SSTables (Sorted String Tables)

**Phase 4 — Implemented** in `internal/sstable`.

An SSTable is an immutable, sorted, on-disk file produced when a MemTable is flushed. It stores key-value entries (`EntryPut`) and deletion tombstones (`EntryDelete`) in lexicographic key order. Once written, the file is never modified.

#### File Layout

```
[header]
[data record 0]
[data record 1]
…
[data record N-1]
[index block]
[footer]
```

#### Header (10 bytes)

```
[magic uint64][version uint16]
```

- `magic` = `0x53534853_46445442` (`SHSFDBTB` in ASCII) — identifies the file type.
- `version` = `1`.

#### Data Record

```
[recordLen uint32][crc32 uint32][seq uint64][kind uint8][keyLen uint32][valueLen uint32][key…][value…]
```

- `recordLen` — byte count of the body (everything after `crc32`).
- `crc32` — IEEE CRC-32 computed over the body (`seq` through `value` bytes inclusive).
- `kind` — `1 = EntryPut`, `2 = EntryDelete`.
- All integers are little-endian.

#### Index Block

One entry per data record, written immediately after the last data record:

```
[keyLen uint32][key bytes][offset uint64][recordLen uint32]
```

- `offset` — file position of the data record's `recordLen` field.
- `recordLen` — body length (redundant with the record header; kept for direct reads without seeking back).

The index block is a dense index (one entry per key). On `Open`, the entire index is loaded into memory. The data region is never loaded.

#### Footer (36 bytes)

```
[indexOffset uint64][indexLen uint64][entryCount uint64][footerCRC uint32][magic uint64]
```

- `footerCRC` — IEEE CRC-32 over `[indexOffset][indexLen][entryCount]` (24 bytes).
- Trailing `magic` — same sentinel as header, confirms file is not truncated at the tail.

#### Checksum Strategy

- **Per-record CRC**: every data record body is checksummed. `Get` and `Scan` verify this on every read. A mismatch returns `ErrCorruptTable`.
- **Footer CRC**: `[indexOffset][indexLen][entryCount]` are checksummed. `Open` verifies before loading the index.
- **Header and footer magic**: validated by `Open` to catch non-SSTable files or truncation.

#### Read Path

**Open:**
1. Verify file size is at least `headerSize + footerSize`.
2. Read and verify header magic.
3. Seek to `fileSize - footerSize`, read footer, verify trailing magic and footer CRC.
4. Seek to `indexOffset`, read `indexLen` bytes, decode index into memory.

**Get(key):**
1. Binary-search in-memory index for `key`.
2. If not found, return `(Entry{}, false, nil)`.
3. Seek to `index[i].offset`, read `recordHeaderSize + recordLen` bytes.
4. Verify CRC; decode and return a deep copy.

**Scan(start, end):**
1. Binary-search index for `start` to find `startIdx`.
2. Iterate index from `startIdx` while `key < end`.
3. For each matching index entry, read record from disk, verify CRC, append deep copy to result.

#### Atomic Creation

`Create` writes to a temporary file (same directory as the target), syncs, then calls `os.Rename`. If any step fails, the temp file is removed and the target path is never created or modified.

Limitation: the parent directory is not fsynced after rename (a future improvement).

#### Known Limitations (Phase 4)

- No Bloom filter; every `Get` for a missing key does a binary index search (no disk I/O) but confirms absence only after scanning the in-memory index.
- No block cache; data reads go to the OS page cache only.
- Dense index (one entry per key) — no sparse index, so large tables consume proportional RAM.
- No compression or encryption.
- No block-level layout; the data region is a flat record stream.
- No compaction; multi-SSTable lookup logic is the Engine's responsibility.
- SSTable is not yet wired to the MemTable or Engine.
- Parent directory is not fsynced after rename.

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
