# ShardForgeDB

An **explainable** distributed database engine for key-value and vector search workloads, written in Go.

> **Phase 6 in review.** WAL, MemTable, SSTable, Bloom Filter, and single-node Engine are implemented.
> No compaction, distributed mode, or vector search exists yet.

---

## Project Overview

ShardForgeDB is a ground-up database engine built to be transparent about how it works at every layer — from the WAL to the cluster coordinator. The goal is a system where every design decision, trade-off, and benchmark result is documented alongside the code.

## Architecture Goal

```
Client
  │
  ▼
Engine (key-value + vector)
  ├── WAL      — durable write-ahead log
  ├── MemTable — in-memory write buffer (skip list)
  ├── SSTables — sorted, immutable on-disk segments
  ├── Bloom    — probabilistic membership filters
  └── Vector   — approximate nearest-neighbour index

Cluster
  ├── Sharding     — consistent-hash partitioning
  └── Replication  — leader/follower replication
```

WAL (`internal/wal`), MemTable (`internal/memtable`), SSTable (`internal/sstable`), Bloom Filter (`internal/bloom`), and the single-node Engine (`internal/engine`) are implemented as of Phase 6. All other components are intended design only — not yet implemented.

## Current Phase

**Phase 1 — Project Foundation** ✓ locked

- [x] Go module initialised
- [x] CLI skeleton (`shardforge --help`, `shardforge version`)
- [x] Config loading from YAML with validation
- [x] Structured logging (JSON / text) via `log/slog`
- [x] Makefile (`build`, `test`, `fmt`, `vet`, `lint`)
- [x] Placeholder packages for all future components
- [x] Design and proof documentation
- [x] GitHub Actions CI

**Phase 2 — WAL** ✓ locked

- [x] `internal/wal` — append-only, CRC-checksummed write-ahead log
- [x] `Open`, `Append`, `Replay`, `Close` API
- [x] Little-endian binary record format with sequence numbers
- [x] Corruption detection and partial-tail tolerance
- [x] Concurrent-safe appends
- [x] 24 tests, 4 benchmarks

**Phase 3 — MemTable** ✓ locked

- [x] `internal/memtable` — ordered, concurrent in-memory write buffer
- [x] `Put`, `Delete`, `Get`, `Scan`, `Len`, `ApproxBytes`, `ShouldFlush` API
- [x] Lexicographically sorted key slice for ordered range scans
- [x] Deletion tombstones (consistent with WAL `RecordDelete`)
- [x] Defensive copies on all reads and writes; `sync.RWMutex` concurrency
- [x] Size accounting (`len(key) + len(value) + 64 B overhead` per entry)
- [x] 30 tests, 7 benchmarks

**Phase 4 — SSTable** ✓ locked

- [x] `internal/sstable` — immutable, sorted, on-disk SSTable file format
- [x] `Create`, `Open`, `Get`, `Scan`, `Len`, `Metadata`, `Close` API
- [x] Binary file format: header, data records, index block, footer
- [x] CRC-32 checksums on every data record and on the footer
- [x] Dense in-memory index for O(log n) Get (binary search + single disk seek)
- [x] Atomic creation via temp-file + rename; partial-write safe
- [x] Deletion tombstones, sequence numbers, binary key/value support
- [x] Concurrent-safe reads; `sync.RWMutex` around file access
- [x] 46 tests, 7 benchmarks

**Phase 5 — Bloom Filter** ✓ locked

- [x] `internal/bloom` — deterministic, serializable Bloom filter
- [x] `New`, `Add`, `MightContain`, `Metadata`, `MarshalBinary`, `UnmarshalBinary` API
- [x] Standard Bloom formulas: m = ceil(-n·ln(p)/ln(2)²), k = round((m/n)·ln(2))
- [x] Deterministic double hashing with FNV-1a 64-bit (h1) and salted FNV-1a 64-bit (h2)
- [x] Compact bit array (packed `[]uint64`); no false negatives by design
- [x] Self-describing binary serialization with magic, version, CRC-32, and trailing sentinel
- [x] Concurrent-safe Add and MightContain via `sync.RWMutex`
- [x] 35 tests, 9 benchmarks

**Phase 6 — Single-node Engine** (branch: `phase-6-engine`, in review)

- [x] `internal/engine` — single-node LSM-tree key-value engine
- [x] `Open`, `Put`, `Delete`, `Get`, `Scan`, `Flush`, `Stats`, `Close` API
- [x] WAL + MemTable + SSTable + Bloom Filter wired end-to-end
- [x] Atomic JSON manifest (`MANIFEST.json`) tracking SSTable and Bloom sidecar paths
- [x] WAL replay on restart; monotonic sequence numbers across restarts
- [x] Bloom filter sidecar per SSTable; negative-key skips tracked in `Stats`
- [x] Min/max key bounds check before Bloom check per SSTable on Get
- [x] Full range Scan merging MemTable and all SSTables; tombstone suppression
- [x] Manual Flush: MemTable → SSTable → Bloom sidecar → manifest → WAL rotation
- [x] Crash-safe invariants: orphan files pre-manifest-commit are ignored on restart
- [x] Concurrent-safe via `sync.RWMutex`; idempotent `Close`
- [x] 45 tests, 10 benchmarks
- [x] This is a **single-node** engine only — no compaction, no distribution, no vector search

> No compaction; read amplification grows with flush count.
> No automatic flush; callers must call Flush explicitly.
> No distributed/sharded/replicated mode planned for this phase.

## Planned Phases

| Phase | Focus |
|-------|-------|
| 2 | WAL — durable, append-only write log |
| 3 | MemTable — concurrent in-memory write buffer |
| 4 | SSTable — sorted, immutable on-disk segments |
| 5 | Bloom filters — fast negative-key lookups |
| 6 | Engine — key-value read/write/delete, compaction |
| 7 | Vector search — ANN index (HNSW or IVF) |
| 8 | Sharding — consistent-hash partitioning |
| 9 | Replication — leader/follower; Raft-compatible consensus only after full implementation |
| 10 | Benchmarks, dashboard, chaos / failure simulation |

## Features NOT Yet Implemented

The following are **not** present in the current codebase:

- Compaction (read amplification grows with flush count)
- Vector search / ANN index
- Sharding
- Replication / consensus
- Failure simulation
- Dashboard / monitoring

## How to Build

```bash
make build          # produces bin/shardforge
```

Or directly:

```bash
go build -o bin/shardforge ./cmd/shardforge
```

## How to Run

```bash
./bin/shardforge --help
./bin/shardforge version
```

## How to Run Tests

```bash
make test           # go test -race ./...
```

Or directly:

```bash
go test -race -count=1 ./...
```

## Other Makefile Targets

```bash
make fmt    # format source files
make vet    # static analysis
make lint   # run golangci-lint (skipped if not installed)
make clean  # remove bin/
make help   # list all targets
```

## Requirements

- Go 1.21 or later (uses `log/slog`)

## License

MIT
