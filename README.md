# ShardForgeDB

An **explainable** distributed database engine for key-value and vector search workloads, written in Go.

> **Phase 1 — Project Foundation only.**
> No database internals are implemented yet.

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

WAL (`internal/wal`) and MemTable (`internal/memtable`) are implemented as of Phase 3. All other components are intended design only — not yet implemented.

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

**Phase 3 — MemTable** (branch: `phase-3-memtable`, in review)

- [x] `internal/memtable` — ordered, concurrent in-memory write buffer
- [x] `Put`, `Delete`, `Get`, `Scan`, `Len`, `ApproxBytes`, `ShouldFlush` API
- [x] Lexicographically sorted key slice for ordered range scans
- [x] Deletion tombstones (consistent with WAL `RecordDelete`)
- [x] Defensive copies on all reads and writes; `sync.RWMutex` concurrency
- [x] Size accounting (`len(key) + len(value) + 64 B overhead` per entry)
- [x] 30 tests, 6 benchmarks

> The MemTable is not yet connected to the WAL or Engine. Data is lost on
> process restart — it is not durable by itself.

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

- MemTable (partial — no WAL/Engine integration)
- SSTables
- Bloom filters
- Compaction
- Key-value read / write / delete
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
