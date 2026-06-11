# ShardForgeDB — High-Level Architecture Design

> **Status (Phase 20 — Final):** All 20 phases are complete. WAL, MemTable, SSTable, Bloom filter, LSM-tree Engine, vector search, local sharding, local replication simulation, local dashboard, networked HTTP nodes, client-side routing gateway, stateless proxy, static cluster metadata, networked read replicas, and ops simulation are all implemented, tested, and benchmarked. See `docs/ARCHITECTURE.md` for the current system diagram and `docs/FINAL_REPORT.md` for the full engineering summary.
>
> This file (`DESIGN.md`) records the original design intent from early phases and is preserved for historical reference. For the authoritative current-state documentation, see `docs/ARCHITECTURE.md`.

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
- **Header version**: `Open` rejects any version other than `1` with `ErrCorruptTable`.

#### Index Validation on Open

After decoding the index, `Open` validates:
- Decoded entry count equals the footer `entryCount`; mismatch → `ErrCorruptTable`.
- Keys are strictly ascending; unsorted or duplicate keys → `ErrCorruptTable`.
- Each record offset falls in `[headerSize, indexOffset)` (the data region); out-of-range → `ErrCorruptTable`.
- Each index `recordLen` is in `[bodyFixedSize, MaxRecordSize]`; violation → `ErrCorruptTable`.

#### readRecord Safety

Before allocating the record body buffer, `readRecord` validates:
- `recLen >= bodyFixedSize` — rejects truncated body lengths.
- `recLen <= MaxRecordSize` — prevents OOM from a corrupt length field.
- `recLen == index entry recordLen` — cross-checks the on-disk record header against the in-memory index.
After CRC verification it additionally checks:
- `kind` is `EntryPut` or `EntryDelete` — rejects semantically invalid kind bytes even if CRC passes.
- Decoded key matches the index key — detects data corruption where CRC is recomputed but keys diverge.

#### Read Path

**Open:**
1. Verify file size is at least `headerSize + footerSize`.
2. Read header; verify magic and version.
3. Seek to `fileSize - footerSize`, read footer, verify trailing magic and footer CRC.
4. Seek to `indexOffset`, read `indexLen` bytes, decode index into memory.
5. Validate decoded index (count, sort order, offsets, record lengths).

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

**Phase 5 — Implemented** in `internal/bloom`.

Each SSTable will carry a per-key Bloom filter. Before reading an SSTable, the engine checks the filter to skip files that definitely do not contain the requested key. The filter package is complete; SSTable integration is deferred to the Engine phase.

#### Parameter Formulas

Given `n = ExpectedItems` and `p = FalsePositiveRate`:

```
m = ceil( -n * ln(p) / (ln(2)^2) )   // bit count
k = round( (m / n) * ln(2) )          // hash count, minimum 1, capped at 64
```

Example: n=1000, p=0.01 → m=9586 bits, k=7 hash functions.

#### Hash Strategy

Double hashing with two deterministic FNV-1a 64-bit variants:

```
h1(x) = FNV-1a-64(x)
h2(x) = FNV-1a-64(salt_bytes || x)     salt = 0x9e3779b97f4a7c15
```

The i-th bit position for key x:

```
pos_i = (h1(x) + i * h2(x)) mod m
```

If h2 computes to zero it is replaced by the salt constant (an odd, well-distributed value) to prevent degenerate double-hashing where all positions collapse to multiples of h1.

The salt is the fractional part of the golden ratio × 2⁶⁴ — a standard choice for hashing schemes that need a decorrelated secondary value with good bit distribution.

#### Bitset Layout

Bits are packed into a `[]uint64` word array. Bit `b` lives in word `b/64` at bit position `b%64`. All reads and writes are protected by a `sync.RWMutex`; readers hold only the read lock, so concurrent `MightContain` calls proceed in parallel.

#### Serialization Format

```
[magic         uint64  ]  0x544c464d4f4f4c42  ("BLOOMFLT" little-endian)
[version       uint16  ]  currently 1
[bitCount      uint64  ]
[hashCount     uint32  ]
[expectedItems uint64  ]
[insertedItems uint64  ]
[fprBits       uint64  ]  math.Float64bits(FalsePositiveRate)
[wordCount     uint64  ]
[words ...     uint64  ]  wordCount × 8 bytes
[crc32         uint32  ]  IEEE CRC-32 over all bytes from magic through last word
[magic         uint64  ]  trailing sentinel — same value as leading magic
```

All integers are little-endian. The wordCount safety cap is 2³⁰ words (8 GiB of bits) to prevent OOM from corrupt length fields.

#### Checksum and Corruption Handling

- **CRC-32 (IEEE)** over the entire blob except the trailing checksum and magic bytes.
- `UnmarshalBinary` rejects: too-short data, bad leading magic, unsupported version, zero bitCount, zero hashCount, impossible FPR (outside (0,1)), inconsistent wordCount, wordCount exceeding the safety cap, CRC mismatch, bad trailing magic.
- Checks run before any allocation of the word array to prevent OOM from corrupt metadata.

#### Known Limitations (Phase 5)

- Not wired into SSTable or Engine yet; integration is the Engine's responsibility.
- No scalable / partitioned Bloom filter; the entire bit array lives in RAM.
- No counting Bloom filter; Delete is not supported.
- No compression of the bit array.
- `InsertedItems` counts successful `Add` calls, not unique keys.
- False positives are possible by design; false negatives are impossible.

### Compaction

**Phase 7 — Implemented** in `internal/engine` via `(*Engine) Compact() error`.

Compaction is **manual** and **full**: it merges all currently flushed SSTables into at most one compacted SSTable. The active MemTable and WAL are not modified.

#### Preconditions

- `Compact()` returns `ErrClosed` if the engine is closed.
- `Compact()` returns `nil` immediately if there are 0 or 1 SSTables (no-op).
- `Compact()` holds the engine write lock for its entire duration to ensure correctness without complexity.

#### Merge Algorithm

1. Scan all SSTables oldest-first via `Reader.Scan`.
2. For each key, track the entry with the highest sequence number.
3. If the winning entry is PUT, include it in the output (preserving the original sequence number).
4. If the winning entry is DELETE (tombstone), **drop it**. This is safe because full compaction covers all SSTables — no older version of the key exists below the compacted level. After the manifest swap, the key is gone.
5. Sort surviving entries lexicographically before writing.

#### Output

| Live entries | Action |
|---|---|
| 0 | Update manifest to empty table list. No new SSTable is written. |
| ≥ 1 | Write one new compacted SSTable (new file ID). Write one new Bloom sidecar (live keys only). Update manifest to list only the new table. |

#### Manifest Swap

The manifest update is atomic (temp file + `fsync` + rename):

```json
{
  "version":      1,
  "next_file_id": N+1,
  "next_seq":     M,
  "tables": [
    { "id": N, "sstable_path": "sstables/table-NNNNNN.sst", ... }
  ]
}
```

Old table IDs are removed from the manifest in one atomic write. Paths are relative; MinKey/MaxKey are base64-encoded.

#### File Cleanup

After the manifest commit:
1. All old SSTable readers are closed.
2. `e.tables` is replaced with the new single handle (or empty slice).
3. Old SSTable and Bloom sidecar files are deleted best-effort. If deletion fails, the orphaned files are harmless because they are no longer in the manifest and are ignored on restart.

#### Crash Safety

| Crash point | Result |
|---|---|
| Before new manifest commit | Old manifest intact; any partially written compacted SSTable/Bloom is an orphan and ignored. |
| After new manifest commit, before old file deletion | New manifest lists only the compacted table; old files are orphans, ignored on restart. |
| After manifest clears all tables | Old files may remain as orphans; ignored on restart. |

#### Compaction Stats

`Stats()` reports:

| Field | Description |
|---|---|
| `CompactionCount` | Number of successful `Compact()` calls that actually ran (no-ops not counted). |
| `LastCompactionInputTables` | Number of SSTables merged in the most recent compaction. |
| `LastCompactionOutputEntries` | Number of live entries written to the compacted SSTable (0 if all-deleted). |

#### Known Limitations (Phase 7)

- Manual full compaction only — no background goroutine, no automatic threshold.
- No levels; all SSTables live at a single (L0-equivalent) level.
- No size-tiered or leveled compaction strategy.
- No range tombstones.
- No snapshots or MVCC.
- Old file deletion is best-effort; orphans are harmless but not cleaned up proactively.
- Parent directory not fsynced after manifest rename.

---

## Engine Layer

**Phase 6 — Implemented** in `internal/engine`.

The `engine` package composes WAL + MemTable + SSTable + Bloom Filter into a unified single-node key-value interface. This is a **single-node engine only** — no compaction, distribution, replication, or vector search.

### API

```go
Open(opts Options) (*Engine, error)
(*Engine) Put(key, value []byte) error
(*Engine) Delete(key []byte) error
(*Engine) Get(key []byte) ([]byte, bool, error)
(*Engine) Scan(start, end []byte) ([]Entry, error)
(*Engine) Flush() error
(*Engine) Stats() Stats
(*Engine) Close() error
```

### File Layout

```
<Dir>/
  wal.log               — append-only write-ahead log
  MANIFEST.json         — atomic JSON manifest (temp: MANIFEST.json.tmp)
  sstables/
    table-000001.sst    — immutable sorted SSTable
    table-000001.bloom  — serialized Bloom filter sidecar
    table-000002.sst
    table-000002.bloom
```

### Write Path

1. Validate key (non-empty).
2. Acquire write lock.
3. Assign next sequence number (engine-owned monotonic counter).
4. Append WAL record (PUT or DELETE) — durable before MemTable is updated.
5. Apply to MemTable.

On WAL failure the sequence counter is rolled back; the MemTable is not updated.

### Read Path (Get)

1. Acquire read lock.
2. Check MemTable — PUT → return copy; DELETE tombstone → return not-found.
3. For each SSTable, newest to oldest:
   a. Min/max key bounds check — skip if key is outside the SSTable's range.
   b. Bloom filter check — skip (and increment `BloomNegativeSkips`) if absent.
   c. `Reader.Get` — return on PUT or DELETE.
4. Key not in any source → return (nil, false, nil).

### Scan Path

All sources (MemTable + all SSTables) are scanned. Results are merged into a `map[string]*candidate` keyed by string(key), keeping the highest-Seq entry per key. Only PUT entries survive; DELETE tombstones suppress all lower-Seq versions. The final result is sorted lexicographically and returned as `[]Entry`.

### Flush

Manual-only in this phase. Sequence:

1. Snapshot MemTable via `Scan(nil, nil)` — all entries in sorted order.
2. Convert to SSTable entries; write SSTable (atomic temp+rename inside `sstable.Create`).
3. Build Bloom filter over all keys (including tombstones); serialize to `.bloom` sidecar file.
4. Reload manifest from disk, append new `tableEntry`, save manifest atomically (temp+rename).
5. Open `sstable.Reader` for the new SSTable; append `tableHandle` to `e.tables`.
6. Reset MemTable.
7. Rotate WAL: close, remove, reopen empty file.

### Manifest Format

`MANIFEST.json` is a JSON object tracking all flushed SSTables. Paths are relative to `Dir` so the directory is portable. `MinKey` and `MaxKey` are base64-encoded to support arbitrary binary keys.

```json
{
  "version":      1,
  "next_file_id": 3,
  "next_seq":     42,
  "tables": [
    {
      "id":           1,
      "sstable_path": "sstables/table-000001.sst",
      "bloom_path":   "sstables/table-000001.bloom",
      "count":        100,
      "min_key":      "<base64>",
      "max_key":      "<base64>"
    }
  ]
}
```

The manifest is written atomically via temp file + `fsync` + rename. The parent directory is not fsynced after rename (known limitation).

### Recovery Invariants

- **Crash before manifest update:** orphan SSTable and Bloom files may exist on disk but are absent from the manifest, so they are ignored on restart. No data loss.
- **Crash after manifest update, before WAL rotation:** replaying the WAL re-applies entries already in the SSTable. The MemTable holds duplicate (higher-Seq) entries that shadow the SSTable during reads. Correct; a subsequent Flush consolidates.
- **Sequence monotonicity:** `nextSeq` is initialized from `manifest.NextSeq` and bumped by WAL replay (`max(manifestSeq, maxWALSeq+1)`). This ensures sequence numbers never repeat across restarts.

### Bloom Sidecar Strategy

A fresh Bloom filter is built at flush time from all SSTable entries (including tombstones). The filter is serialized and written as a `.bloom` sidecar file alongside the `.sst` file via `writeFileAtomic` (temp file + `fsync` + rename) so a crash mid-write cannot leave a truncated sidecar. At `Open` time, both files are loaded together. The Bloom filter lives entirely in RAM; it is not embedded in the SSTable binary.

### Bloom Stats Concurrency

`BloomChecks` and `BloomNegativeSkips` are incremented inside `Get`, which holds only `e.mu.RLock()`. Multiple concurrent readers can therefore reach these increments simultaneously. Both counters are `sync/atomic.Uint64` values — no write lock is needed and no data race can occur. `Stats()` reads them with `Load()`.

### Manifest Initialization

On the first `Open` (no `MANIFEST.json` on disk), the engine writes an empty manifest to disk before proceeding. This makes the on-disk layout explicit from the very first call and ensures restart logic always finds a valid manifest.

### Manifest Table Entry Validation

Every table entry loaded from the manifest is validated by `validateTableEntries`:

| Rule | Error |
|------|-------|
| ID must be non-zero | `ErrCorruptManifest` |
| IDs must be unique | `ErrCorruptManifest` |
| SSTablePath and BloomPath must be non-empty | `ErrCorruptManifest` |
| Both paths must pass `filepath.IsLocal` (not absolute, no `..`) | `ErrCorruptManifest` |
| MinKey and MaxKey must be valid base64 | `ErrCorruptManifest` |
| If both decoded keys are non-empty, MinKey must be ≤ MaxKey | `ErrCorruptManifest` |
| Count must be non-zero | `ErrCorruptManifest` |

`filepath.IsLocal` (available since Go 1.20) checks that a path is not absolute, does not escape the current directory via `..`, and is not empty.

### Known Limitations (Phase 6)

- No compaction; read amplification grows linearly with flush count.
- No background flush; callers must call Flush explicitly.
- No WAL segment rotation; WAL is replaced in full after each flush.
- No background cleanup of orphan SSTable/Bloom files.
- No transactions, snapshots, or MVCC.
- No compression or block cache.
- No distributed/sharded/replicated mode.
- No vector search.
- Parent directory not fsynced after manifest rename.
- Bloom sidecars are not embedded in SSTables.

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

---

## Benchmarking (Phase 8)

### Goal

Phase 8 adds a workload evaluation layer that measures the single-node engine under realistic, deterministic workloads and produces repeatable results that can be documented alongside the code. No database features are added.

### Package: `internal/bench`

| File | Responsibility |
|------|----------------|
| `runner.go` | `Runner`, `Result`, `Recorder`, `Percentile`, `Scale` configs |
| `workload.go` | `GenKey`, `GenValue`, per-workload run functions |
| `report.go` | Markdown report generation (`WriteReport`, `WriteReportFile`) |
| `bench_test.go` | 31 tests covering all framework components |

### CLI: `cmd/shardforge-bench`

```bash
./bin/shardforge-bench --scale small --out docs/BENCHMARKS.md
./bin/shardforge-bench --scale medium --workload write-heavy
```

Flags: `--scale`, `--workload`, `--out`, `--seed`.

### Workload Definitions

| Workload | Operations Measured | Preload? | Notes |
|----------|---------------------|----------|-------|
| `write-heavy` | 100% Put | No | Periodic manual Flush every N Puts |
| `read-heavy` | 95% Get existing, 5% Get missing | Yes | Out-of-bounds missing keys hit bounds check |
| `mixed` | 50% Put / 30% Get / 20% Delete | Yes | Deterministic via `i%10` |
| `scan` | ScanCount range scans | Yes | Each scan covers RangeSize keys |
| `compaction` | Gets before and after Compact() | No | Records pre/post SSTable count and compact duration |
| `restart` | 1 engine reopen | Yes | WAL replay + manifest load |

### Key/Value Generation

Keys are generated as zero-padded decimal indices (`key-0000000042`) so lexicographic order matches insertion order, simplifying scan workloads without a PRNG.

Values are generated via a fast LCG seeded by `(seed XOR index*magic)`. Two runs with the same seed produce identical values; changing seed or index produces different output.

### Metrics

The `Result` struct captures:

- `Operations`, `Duration`, `OpsPerSec` — throughput
- `P50Latency`, `P95Latency`, `P99Latency` — from a sorted per-op duration slice
- `BytesWritten`, `BytesRead` — I/O accounting
- `FinalSSTableCount`, `FinalMemTableEntries` — engine state at workload end
- `FlushCount`, `CompactionCount`, `BloomChecks`, `BloomNegativeSkips` — from engine Stats
- `PreCompactSSTableCount`, `PostCompactSSTableCount`, `CompactDuration` — compaction workload detail

### Reproducibility

All workloads are fully deterministic given identical `(scale, seed)`:

- Fixed key generation (index-based, no PRNG)
- Fixed value generation (LCG seeded by caller-supplied seed + index)
- Fixed operation ratios (modular arithmetic, no PRNG)
- Fixed flush intervals (count-based)
- No wall-clock randomness in output

### Report Format

`WriteReport` produces a Markdown document with sections: Environment (placeholders for the developer to fill in), Configuration, Commands Used, Results tables, Compaction Detail, Interpretation, Known Limitations, How to Reproduce.

The report output is fully deterministic for identical `(scaleName, cfg, results)` — no timestamps or machine-specific values are written automatically.

### Scales

| Scale | KeyCount | ValueSize | FlushInterval | RangeSize | ScanCount |
|-------|----------|-----------|---------------|-----------|-----------|
| `small` | 1,000 | 128 B | 100 | 50 | 100 |
| `medium` | 50,000 | 256 B | 1,000 | 500 | 500 |

`small` completes in seconds and is suitable for CI. `medium` is for stronger local measurement (not run in CI).

### Limitations

- Preload phases are included in total Duration (documented per-workload in the interpretation).
- P99 latency is sensitive to OS scheduler jitter on the measurement machine.
- Missing keys in the read-heavy workload are out-of-bounds and hit the per-SSTable bounds check before Bloom is consulted. Bloom negative-skip rate is therefore zero for that workload — this is correct behavior, not a limitation of the Bloom filter.
- No wall-clock isolation; OS background tasks affect results.

---

---

## Vector Search (Phase 9)

### Overview

Phase 9 adds a single-node **exact** k-nearest-neighbour vector store in `internal/vector`. It is backed by the existing single-node Engine (Phase 6) for persistence and provides an in-memory exact index for fast search.

This is **not** approximate nearest-neighbour search. There is no HNSW, no IVF, no product quantization, and no graph index. Every search scores every stored vector against the query. This design prioritises correctness, explainability, and benchmarkability over raw search throughput at very large scales.

### Package Layout

```
internal/vector/
  store.go            — Store type, public API, lifecycle, in-memory index
  codec.go            — binary record encoding/decoding (magic, CRC, version)
  metric.go           — distance computations: cosine, L2, dot product
  store_test.go       — 44 unit tests
  store_bench_test.go — 10 benchmarks
```

### Store Architecture

```
Caller
  │
  ▼
vector.Store
  ├── sync.RWMutex — guards the in-memory index and closed flag
  ├── map[string]Record — in-memory exact index (ID → Record)
  └── engine.Engine — WAL + MemTable + SSTable stack for persistence
```

`Open` creates (or reopens) an engine at `opts.Dir` and scans the vector namespace prefix to rebuild the in-memory index. After that, all reads (`Get`, `Search`) operate entirely from memory. Writes (`Upsert`, `Delete`) go to the engine first, then update memory.

### Engine-Backed Persistence

Vectors are stored as opaque binary values in the engine under keys of the form:

```
__vector__/<namespace>/<id>
```

The prefix `__vector__/` is reserved. The namespace field allows multiple stores to share one engine directory without key collision. IDs are validated to exclude `/` and control characters. On `Open`, the store scans `__vector__/<ns>/` to `__vector__/<ns>/\xff` to load all live records.

### Binary Record Encoding

Each vector record is encoded as a self-describing binary blob:

```
[magic      8 bytes ] — 0x5348415244564543 ("SHARDVEC")
[version    2 bytes ] — uint16, currently 1
[dimension  4 bytes ] — uint32
[metaLen    4 bytes ] — uint32
[vector     dim×4 B ] — float32 values, little-endian IEEE 754
[metadata   metaLen ] — raw bytes (may be empty)
[crc32      4 bytes ] — CRC-32/IEEE over [version..metadata]
[magic      8 bytes ] — same footer sentinel
```

On decode, bad magic, unsupported version, dimension mismatch, CRC failure, or truncation returns `ErrCorruptRecord`.

### Distance Metrics

| Metric | Score (higher = better) | Distance |
|--------|------------------------|----------|
| `cosine` | cosine similarity ∈ [−1, 1] | 1 − score |
| `l2` | −(squared Euclidean distance) | squared L2 |
| `dot` | dot product | −(dot product) |

Tie-breaking is always by ID ascending (lexicographic) for determinism. Zero vectors are rejected for cosine. NaN and ±Inf are rejected for all metrics.

### Search Algorithm

```
for each stored record:
    score, dist = computeDistance(metric, query, record.Vector)
sort by score descending, then by ID ascending
return top-k
```

Time complexity: O(n·d). All stored vectors must fit in memory.

### Limitations

- **Exact only.** No ANN, HNSW, IVF, or approximate search.
- **Single node.** No sharding, replication, or distributed search.
- **Memory-bound.** All vectors reside in the process heap.
- **Manual flush/compact.** No background maintenance.

---

## Sharding Layer

### Phase 10 — Single-process Key-value Sharding

**Implemented** in `internal/shard`.

This phase adds a sharding layer that routes key-value operations across multiple local `Engine` instances using deterministic consistent hashing. It is **not** a distributed system: all shards live inside a single OS process, there is no networking, no RPC, no replication, and no consensus.

#### Goals

- Deterministic, stable key routing across process restarts.
- Shard-local persistence via independent `Engine` instances.
- Fan-out range scans that merge results from all shards.
- Shard-level flush and compaction.
- Clean restart and recovery via an atomic manifest.

#### Directory Layout

```
<Dir>/
  SHARDING.json        — sharding manifest (written atomically)
  shards/
    shard-0000/        — Engine directory for shard 0
    shard-0001/        — Engine directory for shard 1
    shard-0002/        — Engine directory for shard 2
    ...
```

Each shard directory is a fully independent `engine.Engine` instance with its own WAL, MemTable, SSTables, Bloom sidecars, and `MANIFEST.json`.

#### Manifest Format

`SHARDING.json` is written atomically (temp file + `rename`):

```json
{
  "version":       1,
  "shard_count":   4,
  "virtual_nodes": 128,
  "hash":          "fnv64a",
  "shard_prefix":  "shard",
  "shards": [
    {"id": 0, "name": "shard-0000", "path": "shards/shard-0000"},
    {"id": 1, "name": "shard-0001", "path": "shards/shard-0001"},
    {"id": 2, "name": "shard-0002", "path": "shards/shard-0002"},
    {"id": 3, "name": "shard-0003", "path": "shards/shard-0003"}
  ]
}
```

Manifest validation rejects:

- Unsupported version number.
- Unknown hash algorithm.
- Absolute shard paths.
- Path traversal (`../`).
- Duplicate shard IDs.
- Duplicate shard names.

On reopen, if the caller passes non-zero `ShardCount` or `VirtualNodes` that differ from the manifest, `Open` returns `ErrShardMismatch` immediately.

#### Hash Ring Algorithm

The ring uses **FNV-1a 64-bit** hashing. For each shard `i` and virtual node `v`:

```
token_label = "shard-XXXX#vnode-XXXX"    (zero-padded 4-digit decimal)
token_hash  = fnv1a64(token_label)
```

All `ShardCount × VirtualNodes` tokens are sorted ascending by hash value. Ties are broken deterministically by (shardID, vnodeIndex).

Key routing:

1. Compute `h = fnv1a64(key)`.
2. Binary-search the sorted token array for the first token with `hash >= h`.
3. If none found, wrap to token 0.
4. Return the shard ID of the found token.

This is standard consistent hashing. The same key always routes to the same shard given identical ring parameters, making routing stable across process restarts.

#### Operation Routing

| Operation    | Routing |
|-------------|---------|
| `Put`        | Single shard determined by key hash |
| `Delete`     | Single shard determined by key hash |
| `Get`        | Single shard determined by key hash |
| `ShardForKey`| Single shard determined by key hash |
| `Scan`       | Fan-out to all shards (see below) |
| `Flush`      | Applied to every shard sequentially |
| `Compact`    | Applied to every shard sequentially |

Empty keys are rejected immediately with `ErrInvalidKey` before ring lookup.

#### Scan Fan-out

Consistent hashing destroys key locality: adjacent keys may live on different shards. Therefore `Scan(start, end)` must fan out:

1. Call `engine.Scan(start, end)` on every shard.
2. Merge all results into a `map[string]*candidate`, keeping only the entry with the highest `Seq` per key.
3. Collect surviving entries and sort by key ascending.
4. Return deterministic, tombstone-free output.

The deduplication step handles the edge case where the same key is present in multiple shard engines (e.g., injected by tests or hypothetical migration scenarios).

#### Stats Aggregation

`Stats()` calls `engine.Stats()` on every shard and aggregates:

- `TotalMemTableEntries` — sum across all shards.
- `TotalMemTableApproxBytes` — sum across all shards.
- `TotalSSTableCount` — sum across all shards.
- `TotalFlushCount` — sum across all shards.
- `TotalCompactionCount` — sum across all shards.
- `Shards []ShardStats` — one entry per shard with per-shard counters.

`Stats()` is allowed after `Close` (documented; engines return their last known state).

#### Failure Behavior

- `Flush` and `Compact`: iterate all shards; return a wrapped error (shard ID + name) on first failure.
- `Close`: iterate all shards; return first close error. Remaining shards are still closed (best-effort).
- `Open` with failed engine: close all already-opened engines and return the error.

#### Concurrency

`Store` uses a single `sync.RWMutex`:

- Public write methods acquire `RLock`, check `closed`, get the target engine, release `RLock`, then call the engine method outside the lock.
- `Close` acquires the full `Lock`, sets `closed = true`, releases, then closes all engines.
- Each `Engine` already handles its own internal synchronisation.

This design avoids holding a global lock during engine I/O while still protecting against use-after-close races in the sequential (non-racing) case.

#### Limitations

- **Static shard count.** The number of shards is fixed at creation time. Resharding is not implemented.
- **No shard migration.** Keys cannot be moved between shards.
- **No rebalancing.** If ring coverage is uneven (FNV-1a with few virtual nodes is not perfectly uniform), load imbalance is possible.
- **No replication.** Each shard has a single local copy. No leader/follower, no Raft.
- **No networking.** All shards are in-process; no RPC or cluster membership.
- **No distributed transactions.** Operations are not atomic across shards.
- **No vector sharding.** The `internal/vector` package is not sharded.
- **No background compaction.** `Compact()` is manual only.

---

---

## Replication Simulation Layer

**Phase 11 — Implemented** in `internal/replica`.

### Goals

1. Prove leader/follower write routing, deterministic operation ordering, and follower catch-up — all within a single process.
2. Demonstrate read consistency trade-offs (stale follower reads vs. leader reads).
3. Provide a durable, restart-safe replication log with CRC integrity.
4. Simulate failure conditions (paused followers, bounded lag) for educational purposes.
5. **Not** distributed consensus, not Raft, not automatic leader election, not fault-tolerant quorum.

### Directory Layout

```
<Dir>/
  REPLICATION.json        — atomic manifest (version, replica_count, leader_id, paths)
  COMMIT                  — last committed LogIndex (atomic write; authoritative on restart)
  replog/
    log.dat               — append-only binary replication log
  replicas/
    replica-0000/         — leader Engine directory (Engine + WAL + SSTables)
    replica-0001/         — follower Engine directory
      APPLIED             — last applied log index (ASCII decimal, best-effort write)
    replica-0002/
      APPLIED
```

### Manifest Format

`REPLICATION.json` is written atomically (temp file + rename):

```json
{
  "version": 1,
  "replica_count": 3,
  "leader_id": 0,
  "replica_prefix": "replica",
  "replicas": [
    {"id": 0, "role": "leader",   "name": "replica-0000", "path": "replicas/replica-0000"},
    {"id": 1, "role": "follower", "name": "replica-0001", "path": "replicas/replica-0001"},
    {"id": 2, "role": "follower", "name": "replica-0002", "path": "replicas/replica-0002"}
  ]
}
```

Validation rules: version==1, replica_count>0, len(replicas)==replica_count, unique IDs in [0,n), unique names, unique relative clean paths (no absolute paths, no path traversal), exactly one leader role, leader_id matches a leader-role replica.

### Replication Log Format

`replog/log.dat` is an append-only binary file:

```
File header (10 bytes):
  [magic    8 bytes]  "SHARDREP"
  [version  uint16]  1 (little-endian)

Per record:
  [recordLen  uint32]  byte count of: crc32 + index + opType + keyLen + valLen + key + value
  [crc32      uint32]  CRC-32/IEEE over all bytes after itself in this record
  [index      uint64]  LogIndex (1-based, monotonically increasing)
  [opType     uint8]   1=Put, 2=Delete
  [keyLen     uint32]  byte length of key
  [valueLen   uint32]  byte length of value
  [key        bytes]
  [value      bytes]
```

Records are validated on replay: bad CRC and truncated records are rejected with distinct errors.

### Commit / Applied Index Semantics

- **CommitIndex:** advances immediately when a leader write succeeds. No follower acknowledgement is required. This is **not** quorum replication.
- **AppliedIndex (per follower):** advances as each follower processes operations via `ReplicateOnce`/`ReplicateAll`. Persisted to `APPLIED` after each successful application.
- On restart, the commit index is reconstructed from the log's last index. Per-follower applied indexes are loaded from `APPLIED` files. Followers only apply operations with index > their loaded applied index.

### Write Path

1. Validate key (non-empty).
2. Acquire store write lock (`s.mu.Lock`).
3. Append operation to `replog/log.dat` (durable; index = last+1).
4. Apply to leader Engine (WAL + MemTable write).
5. If leader write succeeds: advance leader's `appliedIndex` and `commitIndex`; persist leader's `APPLIED`.
6. If leader write fails: return error; `commitIndex` is not advanced; the log has an uncommitted tail record.

### Replication Path

`ReplicateOnce`:
1. Acquire store write lock.
2. For each non-paused follower with `appliedIndex < commitIndex`:
   - Fetch up to `maxApplyPerCall` operations from the in-memory log cache with index > follower's `appliedIndex`.
   - Apply each to the follower's Engine.
   - Update and persist follower's `appliedIndex` after each success.
3. Return total applications.

`ReplicateAll`: loops `ReplicateOnce` until no progress remains.

### Read Modes

| Mode | Behaviour | Staleness |
|------|-----------|-----------|
| `ReadLeader` | Reads from the leader Engine | Never stale |
| `ReadFollower` | Reads from the specified follower; replicaID must not be the leader | Stale until ReplicateAll |
| `ReadAny` | Reads from replicaID if ≥ 0, otherwise from leader | Depends on target |

Follower reads reflect only operations applied up to that follower's `appliedIndex`. They may miss keys or return outdated values. This is documented and expected behaviour, not a bug.

### Pause / Lag Simulation

- `SetFollowerPaused(id, true)` — follower is skipped during `ReplicateOnce`/`ReplicateAll`. Simulates a stopped node.
- `SetFollowerPaused(id, false)` — resume; next `ReplicateAll` catches up.
- `SetFollowerLag(id, n)` — follower applies at most `n` operations per `ReplicateOnce` call. Simulates a slow node.
- These controls are **in-memory only** and are not persisted. They reset on reopen.

### Restart Behavior

On `Open` with an existing `REPLICATION.json`:
1. Manifest is loaded and validated.
2. The replication log is opened and replayed into an in-memory operation cache.
3. Each replica Engine is opened (WAL replayed internally by the Engine layer).
4. Per-follower `APPLIED` files are read to restore applied indexes.
5. `commitIndex` is loaded from the durable `COMMIT` file. If `COMMIT` is missing, `commitIndex = 0`. The log may have records beyond `commitIndex`; they are treated as uncommitted and are never replicated to followers.
6. Followers do not automatically catch up on restart; callers must call `ReplicateAll`.

The `COMMIT` file is the authoritative source for `commitIndex`. It is written atomically (temp+rename) on every successful `Put`/`Delete`, after the leader Engine write succeeds. Using `log.lastIndex()` as a proxy for `commitIndex` is incorrect because the log may have an uncommitted tail record whose leader write failed.

### Failure Limitations

- If the process crashes after a log record is appended but before the leader Engine write succeeds, the log will have an uncommitted tail record. On restart, `commitIndex` is loaded from `COMMIT` (which was not updated), so the uncommitted tail record is invisible to followers. The leader Engine may or may not have the write (depending on whether its WAL was flushed before the crash).
- If `saveCommitIndex` fails (disk full, permission error), `Put`/`Delete` return an error and `commitIndex` is not advanced in memory. The log has an uncommitted tail record that will be permanently ignored after restart.
- `APPLIED` persistence is best-effort: if the write fails and the process crashes, the follower may re-apply an already-applied operation on restart. The Engine is idempotent for repeated same-key writes (higher Seq wins), so correctness is preserved at the cost of potentially different sequence numbers.
- If a follower `APPLIED` write fails (best-effort), the follower may re-apply an already-applied operation on restart. The Engine is idempotent for repeated same-key writes at higher sequence numbers, so correctness is preserved but Seq numbers may differ.
- Pause/lag simulation does not persist across restarts.

### Why This Is Not Raft / Distributed Consensus

| Property | This Phase | Raft |
|----------|-----------|------|
| Transport | None (in-process function calls) | Network RPC |
| Leader election | Manual configuration; fixed | Randomised timeout election |
| Commit rule | Single leader write (no quorum) | Majority acknowledgement |
| Log compaction | Not implemented | Snapshotting |
| Membership changes | Not implemented | Joint consensus / single-server |
| Fault tolerance | None (leader death = halt) | Majority alive = liveness |
| Linearisability | Leader reads only | All reads with lease or read index |

This phase is an **educational simulation** demonstrating the mechanics of operation propagation in a leader/follower model. It should not be used for production durability or availability.

### Concurrency

`Store` uses a single `sync.RWMutex`:

- `Put`/`Delete` acquire the full write lock (`Lock`) for the entire append-apply-commit sequence.
- `ReplicateOnce`/`ReplicateAll` acquire the full write lock to prevent races between `appliedIndex` updates in different goroutines.
- `Get`/`Scan`/`Stats`/`Replicas` acquire the read lock.
- `Close` acquires the write lock, sets `closed = true`, closes all engines and the log.

### Limitations

- **In-process only.** No networking, no RPC.
- **No automatic leader election.** Leader is fixed at `Open` time.
- **No Raft, no consensus.** Commit does not require follower acknowledgement.
- **No log compaction.** The replication log grows unboundedly.
- **No snapshotting.** Followers catch up by replaying the full log from index 0.
- **No vector replication.** `internal/vector` is not replicated.
- **No distributed transactions.** Operations are not atomic across replicas in a distributed sense.
- **Manual flush/compact only.** No background maintenance.

---

*This document will be updated as each phase is implemented and design decisions are validated.*

## Phase 12 — Local Dashboard and Chaos/Failure Simulation

### Goals

- Provide a local, in-process HTTP observability dashboard for ShardForgeDB components.
- Expose JSON status endpoints and an HTML view of engine, shard, and replica state.
- Run deterministic local failure scenarios using the existing replica API.
- Record event timelines for scenarios and surface them through the dashboard.

This phase is **local only**. It is NOT distributed chaos testing. It is NOT production monitoring. It is NOT networking between database nodes.

### Dashboard Architecture

The dashboard is implemented in `internal/dashboard` with six files:

| File | Role |
|------|------|
| `types.go` | All exported types, constants, sentinel values |
| `collector.go` | Engine/Shard/Replica/Multi/Scenario collectors |
| `templates.go` | HTML template (Go stdlib `html/template`) |
| `server.go` | HTTP server with route handler |
| `scenario.go` | Three deterministic chaos scenarios |
| `server_test.go` | Tests for server, collectors, rendering |
| `scenario_test.go` | Tests for all chaos scenarios |

### Collectors

Collectors implement the `Collector` interface:

```go
type Collector interface {
    Snapshot() Snapshot
}
```

Each collector reads stats from an existing local store — it never mutates the underlying store:

- **`NewEngineCollector`** — wraps `engine.Engine`, calls `Stats()`.
- **`NewShardCollector`** — wraps `shard.Store`, calls `Stats()`.
- **`NewReplicaCollector`** — wraps `replica.Store`, calls `Stats()`. Reports `HealthDegraded` when any follower's `AppliedIndex` < `CommitIndex`.
- **`NewMultiCollector`** — merges components and events from multiple collectors.
- **`NewScenarioCollector`** — exposes a `ScenarioResult`'s events through the dashboard.

### HTTP Endpoints

| Endpoint | Content-Type | Description |
|----------|-------------|-------------|
| `GET /` | `text/html` | HTML dashboard with component cards and timeline |
| `GET /status` | `application/json` | Full `Snapshot` as JSON |
| `GET /healthz` | `application/json` | `{"status":"ok"}` if server is running |
| `GET /events` | `application/json` | JSON array of `TimelineEvent` |
| Unknown path | — | 404 Not Found |

### HTML Rendering

- Template: Go standard library `html/template` (auto-escapes all user-controlled strings).
- No external JavaScript dependencies.
- No external CSS or font resources.
- All styles are inlined.
- Footer states: "Local dashboard only — no networking, no Raft, no consensus, no distributed cluster."

### Scenario Runner

Three deterministic chaos scenarios in `scenario.go`:

**`RunFollowerPauseScenario`**
1. Write a key to the leader.
2. Pause the target follower (`SetFollowerPaused(true)`).
3. `ReplicateAll` — follower is skipped.
4. Verify key absent on follower.
5. Unpause (`SetFollowerPaused(false)`).
6. `ReplicateAll` — follower catches up.
7. Verify key present on follower.

**`RunFollowerLagScenario`**
1. Set follower lag limit to 2 ops/call (`SetFollowerLag(2)`).
2. Write 6 keys to the leader.
3. `ReplicateOnce` — follower applies at most 2 ops.
4. Confirm lag (follower `AppliedIndex` < `CommitIndex`).
5. `ReplicateAll` — follower reaches full catch-up.
6. Verify all 6 keys visible on follower.
7. Clear lag limit (`SetFollowerLag(0)`).

**`RunFollowerCatchupScenario`**
1. Pause the follower.
2. Write 4 keys to the leader.
3. Verify all 4 keys absent on follower.
4. Unpause follower.
5. `ReplicateAll`.
6. Verify all 4 keys visible on follower.

Scenario rules:
- Deterministic key names (no randomness).
- No goroutine chaos, no sleeps.
- All failures reported in `ScenarioResult.Error`.
- Events are time-ordered.
- Invalid inputs (nil store, invalid follower ID, leader ID as follower) return `ScenarioFailed` without panicking.

### Failure Simulation Limits

Scenarios use the existing `SetFollowerPaused` and `SetFollowerLag` APIs from Phase 11. These are in-memory simulation controls only:

- **No process crash simulation.** Followers cannot actually die.
- **No network partition simulation.** All replicas share the same in-process call path.
- **No disk failure simulation.** Underlying engines always succeed.
- **No leader failure simulation.** Only follower behaviour is exercised.

### Why This Is Not Distributed Chaos Testing

Real distributed chaos testing (e.g. Jepsen) injects failures across physical nodes over a real network. This phase operates entirely within one OS process, using in-memory flags on local structs. It validates the correctness of the `pause`/`lag`/`catch-up` replica controls, not network-level fault tolerance.

### CLI Command

`cmd/shardforge-dashboard/main.go` provides:

```
shardforge-dashboard --help
shardforge-dashboard --demo
shardforge-dashboard --demo --run-chaos
shardforge-dashboard --addr 127.0.0.1:9090 --demo
```

In `--demo` mode the CLI:
1. Opens a temp directory with a 3-replica store.
2. Seeds 20 keys and replicates them.
3. Optionally runs all three chaos scenarios (separate temp stores per scenario).
4. Starts the HTTP server and blocks until Ctrl+C.

Uses only Go standard library `flag` package — no external dependencies.

---

## Release Scope

### What Is Stable in This Release

Every phase through Phase 12 is implemented, tested (race-safe), benchmarked, and documented:

| Component | Status | Notes |
|-----------|--------|-------|
| WAL | Stable | Binary format, CRC, replay, sequence numbers |
| MemTable | Stable | Sorted concurrent buffer, tombstones |
| SSTable | Stable | Immutable segments, index, CRC footer, atomic creation |
| Bloom Filter | Stable | Deterministic FNV-1a double hashing, serializable |
| Engine | Stable | Full LSM-tree read/write path, WAL replay, manifest |
| Compaction | Stable | Manual full compaction only |
| Vector Search | Stable | Exact brute-force k-NN, cosine/L2/dot |
| Sharding | Stable | FNV-1a consistent-hash, single-process |
| Replication | Stable | Binary op-log, leader-commit, COMMIT file, pause/lag simulation |
| Dashboard | Stable | Local HTTP server, HTML+JSON endpoints, chaos scenarios |
| Scripts | Stable | smoke.sh, demo.sh, release_check.sh |

### What Is Intentionally Not Included

The following are explicitly out of scope and make no claims of implementation:

- **Background/automatic/leveled/size-tiered compaction** — Compact() is manual only.
- **Database-node networking or RPC** — All components run in a single OS process.
- **Raft or any consensus protocol** — No distributed log, no leader election, no fault tolerance.
- **Fault-tolerant quorum replication** — Replication simulation does not survive process death.
- **Shard migration or resharding** — Shard topology is static at Open() time.
- **ANN/HNSW/IVF vector search** — Vector search is exact brute-force only.
- **Production monitoring** — Dashboard is a local simulation tool, not a monitoring system.
- **Distributed transactions** — No MVCC, no cross-node atomic operations.

### Why Local Simulation Phases Exist

Phases 10 (sharding), 11 (replication), and 12 (dashboard) demonstrate the *mechanics* of distributed database patterns without the complexity of network programming:

- **Sharding** shows consistent-hash routing and multi-engine fan-out correctly.
- **Replication** shows leader/follower operation propagation, stale read semantics, and commit durability correctly.
- **Dashboard** shows observable system state and deterministic failure scenario execution correctly.

Each could be extended to a real distributed system by adding an RPC transport layer. That is intentionally left as future work to keep the current scope honest and self-contained.

---

## Phase 13 — Final Polish and Release Hardening

### Goals

- Make the repository clean, accurate, demo-ready, and recruiter-readable.
- Ensure all documentation is internally consistent across phases 1–12.
- Add release scripts for fast smoke validation and full release gating.
- Add a release checklist and project summary for portfolio use.
- Fix any wording that could mislead about scope (e.g. blanket "no networking" when Phase 12 adds a local HTTP server).

### Changes

- **README.md**: Full rewrite — portfolio pitch, quickstart, demo commands, scope table, not-implemented table.
- **docs/DESIGN.md**: Release Scope section, Phase 13 section.
- **docs/PROOF.md**: Phase 13 section.
- **docs/BENCHMARKS.md**: Benchmark Reproducibility section.
- **docs/RELEASE_CHECKLIST.md**: Build, test, benchmark, demo, scope honesty, resume/LinkedIn checklists.
- **docs/PROJECT_SUMMARY.md**: One-paragraph overview, architecture, phase map, recruiter bullets, "what next" section.
- **scripts/smoke.sh**: Fast smoke validation.
- **scripts/demo.sh**: Recruiter-friendly demo sequence.
- **scripts/release_check.sh**: Full release gate with clean tree check.
- **Makefile**: `smoke`, `demo`, `release-check` targets.

### No Behavior Changes

Phase 13 does not change any Go package behavior. No new packages. No Engine, Shard, Replica, Vector, Dashboard, WAL, MemTable, SSTable, or Bloom changes.

---

## Phase 14 — Real Networked Node Runtime + HTTP Transport Foundation

### Goals

- Implement the first real networked distributed-system foundation: independent `shardforge-node` processes backed by their own Engine directories.
- Provide an HTTP/JSON transport API so nodes can be operated and tested over the network.
- Demonstrate multi-process node independence with a Docker Compose 3-node demo.
- Add integration tests proving cross-process/network behavior and data isolation.
- Maintain strict scope: this is NOT Raft, NOT consensus, NOT quorum replication, NOT distributed sharding.

### Node Architecture

```
shardforge-node process
  │
  ├── node.Server (internal/node/server.go)
  │     ├── Options: NodeID, Addr, DataDir, WALSyncOnWrite, MemTableMaxBytes
  │     ├── Engine (internal/engine) — local LSM-tree key-value store
  │     ├── http.ServeMux — routes HTTP/JSON requests to handlers
  │     └── net.Listener — binds to Addr for real TCP connections
  │
  ├── HTTP Handlers (internal/node/handlers.go)
  │     ├── GET  /healthz   → {"status":"ok","node_id":"..."}
  │     ├── GET  /status    → Status{NodeID, Addr, DataDir, StartedAt, Engine{...}}
  │     ├── PUT  /kv/{key}  → Engine.Put(key, value)
  │     ├── GET  /kv/{key}  → Engine.Get(key)
  │     ├── DELETE /kv/{key} → Engine.Delete(key)
  │     ├── GET  /scan      → Engine.Scan(start, end) — query params
  │     ├── POST /flush     → Engine.Flush()
  │     └── POST /compact   → Engine.Compact()
  │
  └── node.Client (internal/node/client.go)
        ├── HTTP/JSON only — never calls Engine methods directly
        ├── Timeout handling via http.Client.Timeout + context
        └── Clear errors: node unavailable, timeout, invalid JSON, server error
```

### HTTP Transport Design

All request/response bodies are JSON. The API is intentionally simple:

| Endpoint | Method | Request body | Response body |
|----------|--------|-------------|---------------|
| `/healthz` | GET | — | `{"status":"ok","node_id":"..."}` |
| `/status` | GET | — | `Status` struct |
| `/kv/{key}` | PUT | `{"value":"..."}` | `{"ok":true,"node_id":"..."}` |
| `/kv/{key}` | GET | — | `{"found":bool,"key":"...","value":"...","node_id":"..."}` |
| `/kv/{key}` | DELETE | — | `{"ok":true,"node_id":"..."}` |
| `/scan` | GET | query: `start`, `end` | `{"node_id":"...","entries":[...]}` |
| `/flush` | POST | — | `{"ok":true,"node_id":"..."}` |
| `/compact` | POST | — | `{"ok":true,"node_id":"..."}` |

Wrong HTTP methods return 405 with `Allow` header. Empty key returns 400.

### Data Independence

Each `shardforge-node` process has its own `DataDir`. The Engine, WAL, MemTable, SSTables, and Bloom filters are fully isolated to that directory. Writing to node-1 never affects node-2. Data survives node restart because the Engine WAL and SSTable files persist in `DataDir`.

### Error Handling

- Handler errors: serialized as `{"error":"...","node_id":"..."}` with appropriate HTTP status codes.
- Client errors: context cancellation → "node unavailable (context)"; connection refused → "node unavailable"; non-2xx status → "server error N: ..."; invalid JSON → "invalid JSON response".

### Docker Compose Demo

`deploy/docker-compose.yml` starts 3 independent `shardforge-node` processes as Docker containers, each with a unique port (9101, 9102, 9103) and a named volume for its data directory. The Dockerfile uses a two-stage build (Go builder + Alpine runtime). Health checks use `wget` to poll `/healthz`.

### CLI (shardforge-node)

```
shardforge-node --node-id node-1 --addr 127.0.0.1:9101 --data-dir ./data/node-1
```

Flags: `--node-id`, `--addr`, `--data-dir`, `--wal-sync`, `--memtable-max-bytes`.  
Prints a scope disclaimer on startup. Handles SIGTERM/SIGINT with clean shutdown.

### Limitations

- No distributed sharding: nodes do not know about each other; no routing layer.
- No networked replication: writing to node-1 does not propagate to node-2.
- No Raft, no consensus, no quorum, no automatic leader election.
- No shard migration or resharding.
- No distributed vector search.
- No background compaction (inherited Engine limitation).
- The Docker Compose demo is a foundation, not a production cluster.

### Future Work

The next natural steps after Phase 14:

1. **Shard router** — a routing layer that maps keys to the correct node via consistent hashing over real HTTP.
2. **Networked replication** — a leader node that replicates operations to follower nodes over the network.
3. **Raft consensus** — leader election, log replication, cluster membership over the network.
4. **Cluster membership** — node discovery, join/leave protocols, gossip.
5. **ANN vector index** — HNSW or IVF for approximate nearest-neighbour at scale.
6. **Background compaction** — size-tiered or leveled, triggered automatically.

---

## Phase 15 — Client-Side Routing Gateway

### Goals

- Implement a client-side routing library (`internal/gateway`) that routes Put/Get/Delete to the correct independent `shardforge-node` using a deterministic consistent-hash ring.
- Provide a `shardforge-gateway` CLI for direct key-routing, health checking, and admin operations.
- Remain strictly honest: this is client-side routing only, not a distributed database.

### Gateway Architecture

```
shardforge-gateway CLI
  │
  ▼
gateway.Gateway (internal/gateway/client.go)
  ├── hashRing (internal/gateway/ring.go)
  │     ├── FNV-1a 64-bit hashing of "nodeID:i" for ring points
  │     ├── Sorted ring points → O(log n) key lookup (binary search)
  │     ├── Virtual nodes (default 128) + weight scaling
  │     └── Deterministic: same config → same routing across restarts
  └── map[nodeID]*node.Client
        └── HTTP/JSON calls to shardforge-node processes (Phase 14)
```

### Consistent Hash Ring

- Each node gets `VirtualNodes * max(Weight, 1)` ring points.
- Ring points are keyed by `"nodeID:i"` and hashed with FNV-1a 64-bit.
- Points are sorted by hash and stored in a slice for O(log n) lookup.
- For a key: hash key → binary search for first point ≥ hash → wrap to 0 if past end.
- Ring is built once at `Open` time and is immutable (no resharding, no membership changes).

### Routing Behavior

| Operation | Behavior |
|-----------|----------|
| `Put(key, value)` | Hash key → one node → `node.Client.Put` |
| `Get(key)` | Hash key → same node → `node.Client.Get` |
| `Delete(key)` | Hash key → same node → `node.Client.Delete` |
| `ScanNode(nodeID, start, end)` | One named node only — no global scan |
| `FlushAll` | Fan out to all nodes (admin operation) |
| `CompactAll` | Fan out to all nodes (admin operation) |
| `HealthAll` | Check all nodes, return map[nodeID]error |

### No Retry to Another Node

When the routed node is unavailable, `Put/Get/Delete` return an error immediately. There is **no retry to another node**. Reason: without replication, a key written to node-A cannot be found on node-B. Retrying would silently miss data and give a false "not found" result. Callers must handle node failures explicitly.

### Scope Limitations

- No distributed sharding inside nodes (nodes don't know about routing).
- No networked replication (nodes don't propagate writes to each other).
- No automatic failover (no retry, no secondary node).
- No shard migration or resharding (ring is static).
- No cluster metadata service (gateway config is caller-provided).
- No distributed global scan (ScanNode is per-node only).
- No distributed transactions.
- No distributed vector search.
- No Raft, no consensus, no quorum.
- No automatic leader election.

### CLI Design

`shardforge-gateway` is a one-shot CLI (not a daemon). Each invocation creates a gateway,
executes one command, and exits. Node IDs are assigned deterministically as `node-1`, `node-2`, ...
from the `--nodes` URL list order.

### Error Handling

- Empty key → `ErrInvalidKey`.
- Unknown nodeID in ScanNode → `ErrUnknownNode`.
- Gateway closed → `ErrClosed` on all operations.
- Node unreachable → error from `node.Client` propagated directly (no retry).

---

## Phase 16 — Stateless Gateway Proxy Server (`internal/proxy`)

**Status:** Implemented. See `internal/proxy` and `cmd/shardforge-proxy`.

### Purpose

Phase 15 added the `internal/gateway` package as a library and a one-shot `shardforge-gateway`
CLI. Phase 16 wraps that library in a **long-running HTTP proxy server** so clients can send
HTTP/JSON requests to a single endpoint without embedding the gateway logic or running a
separate CLI process per request.

### Architecture

```
HTTP Client
  │  HTTP/JSON
  ▼
shardforge-proxy (internal/proxy.Server, port 9200)
  │  uses internal/gateway.Gateway
  │  consistent-hash ring, FNV-1a 64-bit
  ▼
shardforge-node-{1,2,3} (internal/node.Server, ports 9101–9103)
  │
  ▼
Engine (WAL + MemTable + SSTable + Bloom)
```

### Request Flow

1. HTTP client sends `PUT /kv/user:1` to the proxy (port 9200).
2. Proxy extracts key `user:1` from the URL path.
3. `internal/gateway.Gateway.Put` computes `FNV-1a(user:1)`, binary-searches the ring, selects `node-2`.
4. Proxy calls `node.Client.Put(ctx, key, value)` → HTTP PUT to `http://node-2:9101/kv/user:1`.
5. Node stores the key in its local Engine (WAL + MemTable).
6. Response propagates back: node → proxy → client.

The proxy is **stateless** — it holds no data and can be restarted at any time. It caches the ring configuration in memory but that is reconstructed from `--nodes` flags on startup.

### Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/healthz` | Proxy liveness (does not check backend nodes) |
| GET | `/status` | Proxy addr, started_at, gateway stats, scope flags |
| GET | `/route/{key}` | Ring lookup — returns node_id + base_url, no network call |
| PUT | `/kv/{key}` | Write key/value to routed node |
| GET | `/kv/{key}` | Read key from routed node |
| DELETE | `/kv/{key}` | Delete key from routed node |
| GET | `/scan-node/{nodeID}` | Per-node scan (start/end query params) |
| POST | `/flush-all` | Flush all configured nodes |
| POST | `/compact-all` | Compact all configured nodes |
| GET | `/nodes/health` | Health check all configured nodes |

### No Failover — Safety Property

The proxy does **not** retry to another node if the routed node is unavailable. This is a safety property, not an oversight:

- Without replication, `user:1` written to `node-2` does not exist on `node-1` or `node-3`.
- Retrying to `node-1` would silently return "not found" after a write that the client believes succeeded.
- The proxy returns 502/503 immediately, making the failure explicit and detectable.

### `/scan-node/{nodeID}` — Per-Node Scan

The scan endpoint scans exactly one named node. There is no global distributed scan because there
is no replication — keys are partitioned across nodes by the ring. A global scan would require
querying all nodes and merging, which is not implemented (and would be inconsistent without
coordinated snapshots).

### `/nodes/health` — Diagnostic Only

`GET /nodes/health` returns one health entry per configured node. A 200 HTTP response is always
returned, even if some nodes are down, because this endpoint is diagnostic. Callers check the
per-node `ok` field to determine individual node health.

### Scope Honesty

- Stateless client-side routing only.
- No data stored in the proxy.
- No Raft, no consensus, no quorum replication.
- No automatic failover or retry to another node.
- No distributed sharding inside nodes.
- No networked replication between nodes.
- No shard migration or resharding.
- No distributed vector search.
- If the routed node is unavailable, the operation fails clearly (502/503).
- Duplicate node IDs or URLs → `ErrInvalidOptions` at `Open` time.

---

## Phase 17 — Static Cluster Metadata (`internal/cluster`)

**Phase 17 — Implemented** in `internal/cluster`.

### Purpose

Instead of passing long `--nodes http://...` strings to every gateway/proxy invocation, Phase 17 adds a file-based, typed, validated cluster configuration format. A config file describes the nodes, routing settings, and proxy address once. CLI tools load it with `--config <path>`.

### Config Load Path

```
configs/local-3node.json
  │
  ▼  cluster.Load(path)
cluster.Config (normalised + validated)
  │
  ├─ cluster.GatewayOptions(cfg, timeout) → gateway.Options  → gateway.Open(opts)
  └─ cluster.ProxyOptions(cfg, timeout)   → proxy.Options    → proxy.Open(opts)
```

### Config Schema

```json
{
  "version": "v1",
  "name": "local-3node",
  "routing": {
    "algorithm": "fnv1a-consistent-hash",
    "virtual_nodes": 128
  },
  "proxy": { "enabled": true, "addr": "127.0.0.1:9200" },
  "nodes": [
    { "id": "node-1", "base_url": "http://127.0.0.1:9101", "weight": 1 }
  ],
  "scope": {
    "static_config_only": true,
    "no_dynamic_membership": true,
    "no_discovery": true,
    "no_consensus": true,
    "no_raft": true,
    "no_replication": true,
    "no_failover": true,
    "no_shard_migration": true,
    "no_distributed_txns": true
  }
}
```

The `scope` object is required to be all-true. This prevents false capability claims: a config that claims `no_raft: false` is invalid and will be rejected by `Validate`.

### Normalisation Before Validation

`Normalize(cfg)` fills in safe defaults before validation:
- `routing.virtual_nodes` → 128 if <= 0
- `proxy.addr` → `127.0.0.1:9200` if empty
- `node.weight` → 1 if <= 0
- `scope` → `DefaultScope()` if all flags are false (zero JSON value)

### Validation Rules

`Validate(cfg)` enforces:
- Version must be `"v1"`.
- Name non-empty.
- Algorithm must be `"fnv1a-consistent-hash"`.
- Virtual nodes positive.
- At least one node.
- Node ID and BaseURL non-empty, BaseURL starts with `http://` or `https://`.
- No duplicate node IDs or BaseURLs.
- Weight positive (after normalization).
- DataDir must not contain `..`.
- All scope flags must be true.

### No Dynamic Membership

The config is loaded once at startup. There is no:
- Node discovery or gossip
- Dynamic membership change
- Automatic ring rebalancing
- Raft or consensus
- Leader election
- Replication
- Failover

The config is static. If nodes change, the config file is updated and the process is restarted.

### Scope Honesty

- Static metadata only; no runtime membership updates.
- No distributed cluster management.
- No production cluster manager.
- Config-driven gateway/proxy startup is the only integration point.

---

## Phase 18 — Networked Read Replicas v1 (`internal/replnet`)

**Phase 18 — Implemented** in `internal/replnet`, `internal/node`, `internal/proxy`.

### Purpose

Phase 18 adds the first real networked replication: a primary node that maintains an in-memory mutation log, and follower nodes that explicitly pull entries from the primary and apply them locally. This is pull-based, explicit, non-automatic replication.

**Scope:**
- Primary manually configured via `--replication-role=primary`.
- Follower sync is explicit (manual): `POST /replication/sync` or via proxy `POST /replication/sync-node/{nodeID}`.
- No automatic background sync loop. No automatic failover. No Raft. No consensus. No quorum.
- Replication log is in-memory only (not persisted; cleared on restart). Engine data is durable via WAL.
- Followers reject client `PUT`/`DELETE` with HTTP 403.

### Replication Architecture

```
Primary Node (port 9111)
  ├── PUT/DELETE → replnet.Log.Append → in-memory entries [{Seq:1,Op:put,Key:k,Value:v}, ...]
  ├── GET /replication/log?after=<seq>&limit=<n>  → returns entries as JSON
  └── GET /replication/status  → {role:"primary", last_local_seq:N}

Follower Node (port 9112)
  ├── GET/SCAN allowed — serves local engine reads
  ├── PUT/DELETE → 403 Forbidden ("follower: writes are not accepted; this node is a read replica")
  ├── POST /replication/sync
  │     → calls primary GET /replication/log?after=lastAppliedSeq
  │     → applies entries to local engine
  │     → updates lastAppliedSeq
  └── GET /replication/status  → {role:"follower", last_applied_seq:N, primary_base_url:"..."}

Proxy (port 9210)
  ├── GET /replication/status  → fan-out to all nodes, aggregate results
  └── POST /replication/sync-node/{nodeID}  → forward to that node's POST /replication/sync
```

### Mutation Log (`replnet.Log`)

- Append-only, goroutine-safe in-memory log.
- Entries assigned monotonically increasing `uint64` sequence numbers starting at 1.
- `Append(op, key, value)` → assigns next seq, records timestamp (UTC), returns Entry.
- `EntriesAfter(after, limit)` → returns entries with Seq > after in ascending order, up to limit.
- `Stats()` → returns `{Count: N, LastSeq: N}`.
- Not persisted to disk. Cleared on restart. Engine WAL provides durability for the actual data.

### Sequence Number Protocol

- Follower tracks `lastAppliedSeq` (atomic uint64).
- Sync: pull entries with `Seq > lastAppliedSeq` from primary.
- Apply: entries must be in strict sequential order (`Seq == lastApplied+1`).
- Already-applied entries (`Seq <= lastApplied`) are safely skipped.
- Out-of-order gaps return `replnet.ErrInvalidEntry`.

### Scope Honesty

- No strong consistency. Replication lag is expected.
- No automatic failover. If the primary is down, followers cannot elect a new primary.
- No multi-primary. Exactly one node may have `replication.role = "primary"` per config.
- Replication log is in-memory. It is cleared on primary restart.
- Followers are read-only for client operations but can have their engine written via `/replication/apply`.
- The cluster config `scope.no_replication: true` is preserved — it refers to *automatic* background replication at the cluster layer, which this phase does not implement.

---

## Phase 19 — Failure Handling and Manual Rebalance Simulation

### Purpose

Phase 19 adds an **operations and simulation layer** (`internal/ops`) to help operators answer:

- Which configured nodes are healthy?
- What happens to routing if a node is considered down?
- Which sample keys would move if a node is removed or added?
- What manual steps would an operator take to recover or rebalance?

This is a **simulation and planning layer only**. No automatic failover. No automatic rebalancing. No data movement. No shard migration. Manual operator action required for all real cluster changes.

### Package: `internal/ops`

Pure, stateless functions. No background goroutines. No persistent state. No mutations of live configs.

**Key types:**
- `OpsScope` — 8 flags all true: `ManualOnly`, `SimulationOnly`, `NoAutomaticFailover`, `NoAutomaticRebalancing`, `NoShardMigration`, `NoDataMovement`, `NoConsensus`, `NoRaft`
- `NodeHealth` — per-node health result: state (healthy/unhealthy/unknown), HTTP status, latency, error string
- `ClusterHealth` — aggregated health result with summary and scope
- `FailureSimulationResult` — routing impact with affected/unaffected key lists
- `RebalancePlan` — key movement plan with operator steps and scope

**Key functions:**
- `DefaultOpsScope()` — all flags true
- `CheckClusterHealth(ctx, cfg, timeout)` — HTTP `/healthz` checks, sorted by node ID
- `RouteKey(cfg, key)` — pure consistent-hash routing (no network)
- `RouteKeyWithAvailableNodes(cfg, key, availableIDs)` — route on filtered node subset
- `SimulateFailure(cfg, req)` — routing impact of specified failures (no live calls)
- `PlanManualRebalance(cfg, removed, added, keys)` — key movement plan (no data movement)

### Routing Reuse

`RouteKey` uses `cluster.GatewayOptions` → `gateway.Open` → `gateway.NodeForKey`. No hashing logic is duplicated. The consistent-hash ring is the same FNV-1a ring used by the proxy and gateway.

`RouteKeyWithAvailableNodes` builds a filtered `gateway.Options` directly from the subset of nodes, bypassing `cluster.Validate` to allow partial node sets.

### Health Check Architecture

```
CheckClusterHealth(ctx, cfg, timeout)
  │ for each node in cfg.Nodes (parallel-capable, currently sequential)
  ├── GET {baseURL}/healthz
  ├── measure latency
  ├── check HTTP 2xx
  ├── decode JSON {"status":"ok"}
  └── mark healthy/unhealthy/unknown
  │
  Sort by node ID (deterministic output)
  Return ClusterHealth with summary + OpsScope
```

Never panics. Never retries. Never mutates config. Exits 0 even if all nodes unhealthy.

### Failure Simulation Flow

```
SimulateFailure(cfg, {downNodeIDs, sampleKeys})
  │
  ├── Validate: all downNodeIDs exist in cfg → ErrUnknownNode if not
  ├── Validate: sampleKeys non-empty → ErrInvalidSimulation if empty
  ├── Compute healthyIDs = cfg.Nodes \ downNodeIDs
  │
  for each key in sampleKeys:
  ├── origNode = RouteKey(cfg, key)
  ├── if allDown: mark unavailable
  ├── else: newNode = RouteKeyWithAvailableNodes(cfg, key, healthyIDs)
  ├── affected if origNode ∈ downSet or newNode ≠ origNode
  └── moved if !downSet[origNode] && newNode ≠ origNode
  │
  Return FailureSimulationResult with affected/unaffected lists + OpsScope
```

### Rebalance Plan Flow

```
PlanManualRebalance(cfg, removedNodeIDs, addedNodes, sampleKeys)
  │
  ├── Validate: all removedNodeIDs exist → ErrUnknownNode
  ├── Validate: sampleKeys non-empty → ErrInvalidSimulation
  ├── plannedNodes = (cfg.Nodes \ removed) + added
  ├── if len(plannedNodes) == 0 → ErrNoHealthyNodes
  │
  Build currentGW from cfg.Nodes
  Build plannedGW from plannedNodes
  │
  for each key in sampleKeys:
  ├── fromNode = currentGW.NodeForKey(key)
  ├── toNode = plannedGW.NodeForKey(key)
  └── moved = fromNode ≠ toNode
  │
  Return RebalancePlan with key movements + operator steps + OpsScope
  NOTE: No data movement. No file writes. Pure computation.
```

### Operator Steps (Hardcoded)

The rebalance plan always includes these honest steps:

1. Review node health
2. Stop sending writes to removed node manually
3. Start replacement node manually if needed
4. Update the static config file manually
5. Restart proxy/gateway with updated config
6. Manually sync read replicas if applicable
7. Verify key routing and application correctness
8. NOTE: No data movement is performed by this tool

### CLI Commands Added

```bash
shardforge-cluster health <config>
  # Diagnostic only. Exits 0 even if nodes unhealthy.

shardforge-cluster simulate-failure <config> --down <nodeID> --key <key>
  # Simulation only. No live calls. Exits non-zero for unknown node or no keys.

shardforge-cluster plan-rebalance <config> --remove <nodeID> --key <key>
  # Planning only. No data movement. No file writes. Exits non-zero for unknown node or no keys.
```

### Scope Confirmation (Phase 19)

- Simulation and planning only: correct
- Manual operator action required: correct
- No automatic failover: correct
- No automatic rebalancing: correct
- No automatic retry to another node: correct
- No data movement: correct
- No shard migration: correct
- No resharding: correct
- No dynamic membership: correct
- No service discovery: correct
- No gossip: correct
- No Raft: correct
- No consensus: correct
- No quorum: correct
- No leader election: correct
- No multi-primary writes: correct
- No conflict resolution: correct
- No distributed transactions: correct
- No distributed vector search: correct
- No production fault tolerance: correct
- No core Engine/WAL/MemTable/SSTable/Bloom/Vector/Shard/Replica/Dashboard changes: correct
