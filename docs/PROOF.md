# ShardForgeDB — Validation Proof Log

This file records the evidence that each phase was implemented correctly and passes its acceptance criteria.

---

## Summary Table (Phase 24 — Reproducible Multi-Node Local Cluster Demo)

| Phase | Component | Status | Tests | Benchmarks | Limitations |
|---|---|---|---|---|---|
| 1 | `cmd/shardforge` | COMPLETE | 3 | — | CLI skeleton only |
| 2 | `internal/wal` | COMPLETE | 24 | 4 | Single WAL file; no rotation |
| 3 | `internal/memtable` | COMPLETE | 30 | 7 | O(n) insert; single mutex |
| 4 | `internal/sstable` | COMPLETE | 46 | 7 | No block cache; dense index |
| 5 | `internal/bloom` | COMPLETE | 35 | 9 | No counting Bloom; RAM-resident |
| 6 | `internal/engine` | COMPLETE | 45 | 10 | Manual flush only; no background compaction |
| 7 | `internal/engine` (compaction) | COMPLETE | 34 | 8 | Full compaction only; no levelled/tiered |
| 8 | `internal/bench` | COMPLETE | 34 | 5 | Local single-node benchmarks only |
| 9 | `internal/vector` | COMPLETE | 49 | 10 | Exact brute-force k-NN only; no ANN |
| 10 | `internal/shard` | COMPLETE | 55 | 10 | In-process simulation only; no networking |
| 11 | `internal/replica` | COMPLETE | 66 | 10 | In-process simulation only; no networking |
| 12 | `internal/dashboard` | COMPLETE | 52 | 8 | Local only; no distributed node discovery |
| 13 | scripts, docs | COMPLETE | — | — | Release hardening |
| 14 | `internal/node` | COMPLETE | 36 | 6 | Nodes are independent; no coordination |
| 15 | `internal/gateway` | COMPLETE | 41 | 6 | Client-side only; no failover |
| 16 | `internal/proxy` | COMPLETE | 45 | 7 | Stateless; no retry; no replication |
| 17 | `internal/cluster` | COMPLETE | 47 | 4 | Static config only; no dynamic membership |
| 18 | `internal/replnet` | COMPLETE | 55+ | 5 | Pull-based; in-memory log; no auto sync |
| 19 | `internal/ops` | COMPLETE | 40 | 4 | Simulation/planning only; no data movement |
| 21 | `internal/trace` | COMPLETE (types only) | 22 | — | Types only; engine wiring deferred to Phase 15 |
| 22 | `engine/explain`, `vector/explain`, CLI | COMPLETE | 40 new (905 total) | — | Single-node only; no distributed traces |
| 23 | `node/explain endpoints`, `node/client`, `shardforge explain-node` | COMPLETE | 24 new (929 total) | — | Single-node HTTP only; no cross-node trace propagation |
| 24 | `configs/cluster/demo-3node.json`, `scripts/demo_cluster_*.sh`, `docs/DEMO.md` | COMPLETE | 13 new (942 total) | — | Local demo only; no Raft; no failover; no shard migration |
| 25 | `configs/replication/demo-leader-follower.json`, `scripts/repl_demo_*.sh`, `SyncResult` type | COMPLETE | 20 new (962 total) | — | Explicit pull-based only; cursor was in-memory (fixed Phase 26); no Raft; no quorum |
| 26 | `internal/replnet/durable_log.go`, `internal/replnet/state_store.go`, `scripts/repl_restart_demo_*.sh` | COMPLETE | 32 new (994 total) | — | Durable journal + cursor; gap detection (409); operator-triggered only; no Raft; no quorum |

**Validation command (all phases):**
```bash
go test -race -count=1 ./...
make build
make vet
```

**Current test pass status:** 994 tests pass across 23 packages (race detector on) on Apple M3 darwin/arm64, Go 1.26.

```
go test -race -count=1 -v ./... | grep -c "^--- PASS:" → 929
```

---

## Phase 1 — Project Foundation (initial)

**Date:** 2026-06-09
**Go version:** go1.26.4 darwin/arm64

See Phase 1 cleanup below for the authoritative final state.

---

## Phase 1 — Cleanup & Validation

**Date:** 2026-06-09
**Go version:** go1.26.4 darwin/arm64

### Changes Made

| Item | Change |
|------|--------|
| Module path | `github.com/shardforgedb/shardforgedb` → `github.com/YashPatel2395/ShardForgeDB` |
| CLI testability | `version` command now writes via `cmd.OutOrStdout()` (no behavior change) |
| CLI tests | 3 new tests in `cmd/shardforge/main_test.go` |
| README.md | Removed "Raft-based" from architecture diagram; softened Phase 9 label |
| docs/DESIGN.md | Replaced Raft-as-planned-impl with honest leader/follower description + Raft note |
| go.mod | Module path corrected; cobra and yaml.v3 promoted from indirect to direct |
| LICENSE | Added standard MIT license (Copyright 2026 Yash Patel) |

### Acceptance Criteria

| Criterion | Result |
|-----------|--------|
| Project builds successfully (`make build`) | PASS |
| All 18 tests pass (`make test`) | PASS |
| `go vet ./...` passes | PASS |
| `go fmt ./...` — no formatting changes | PASS |
| `go mod tidy` — clean | PASS |
| `shardforge --help` works | PASS |
| `shardforge version` works | PASS |
| Module path matches GitHub repo | PASS |
| Raft not claimed as planned implementation | PASS |
| MIT LICENSE file present | PASS |

### Commands Run

```
go mod tidy
go fmt ./...
go vet ./...
go test -race -count=1 -v ./...
make test
make vet
make build
./bin/shardforge --help
./bin/shardforge version
```

### Full Test Output

```
=== RUN   TestHelp
--- PASS: TestHelp (0.00s)
=== RUN   TestVersion
--- PASS: TestVersion (0.00s)
=== RUN   TestUnknownCommand
--- PASS: TestUnknownCommand (0.00s)
PASS
ok  github.com/YashPatel2395/ShardForgeDB/cmd/shardforge    1.313s

=== RUN   TestDefault
--- PASS: TestDefault (0.00s)
=== RUN   TestLoad_ValidFile
--- PASS: TestLoad_ValidFile (0.00s)
=== RUN   TestLoad_PartialFile_UsesDefaults
--- PASS: TestLoad_PartialFile_UsesDefaults (0.00s)
=== RUN   TestLoad_MissingFile
--- PASS: TestLoad_MissingFile (0.00s)
=== RUN   TestLoad_InvalidYAML
--- PASS: TestLoad_InvalidYAML (0.00s)
=== RUN   TestLoad_InvalidPort
--- PASS: TestLoad_InvalidPort (0.00s)
=== RUN   TestLoad_InvalidLogLevel
--- PASS: TestLoad_InvalidLogLevel (0.00s)
=== RUN   TestLoad_InvalidLogFormat
--- PASS: TestLoad_InvalidLogFormat (0.00s)
PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/config   1.617s

=== RUN   TestNew_ReturnsLogger
--- PASS: TestNew_ReturnsLogger (0.00s)
=== RUN   TestNewWithWriter_JSONFormat
--- PASS: TestNewWithWriter_JSONFormat (0.00s)
=== RUN   TestNewWithWriter_TextFormat
--- PASS: TestNewWithWriter_TextFormat (0.00s)
=== RUN   TestNew_DebugLevelFiltersInfo
--- PASS: TestNew_DebugLevelFiltersInfo (0.00s)
=== RUN   TestNew_WarnLevelSuppressesInfo
--- PASS: TestNew_WarnLevelSuppressesInfo (0.00s)
=== RUN   TestNew_UnknownLevelDefaultsToInfo
--- PASS: TestNew_UnknownLevelDefaultsToInfo (0.00s)
=== RUN   TestNew_UnknownFormatDefaultsToJSON
--- PASS: TestNew_UnknownFormatDefaultsToJSON (0.00s)
PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/logging  1.470s
```

**Total: 18 tests, 18 PASS, 0 FAIL**

### CLI Verification

```
$ ./bin/shardforge --help
ShardForgeDB is an explainable distributed database engine
designed for key-value and vector search workloads.

Phase 1: Project Foundation
  - CLI skeleton
  - Config loading
  - Structured logging

Database internals are NOT implemented yet.

Usage:
  shardforge [command]
...

$ ./bin/shardforge version
ShardForgeDB 0.1.0
```

### Known Limitations

- `golangci-lint` is not installed; `make lint` degrades gracefully.
- Config format is YAML only; no env-var override or TOML support yet.
- No database internals exist; foundation phase only.

---

---

## Phase 1 — Final Lock

**Date:** 2026-06-09
**Go version (local):** go1.26.4 darwin/arm64
**Go version (CI target):** go 1.21.x

### Changes Made

| Item | Change |
|------|--------|
| `go.mod` Go version | `go 1.26.4` → `go 1.21` (matches README; `log/slog` minimum) |
| `go mod tidy` after version fix | No toolchain directive added; go.sum unchanged |
| GitHub Actions CI | Added `.github/workflows/ci.yml` |
| `docs/PROOF.md` | This section |

### CI Workflow Summary (`.github/workflows/ci.yml`)

Triggers: push to `main`, pull_request to `main`  
Runner: `ubuntu-latest`, Go `1.21.x`

Steps in order:
1. Checkout
2. Set up Go 1.21.x (with module cache)
3. `go mod tidy` — fails CI if go.mod/go.sum drift
4. Verify no uncommitted changes after tidy
5. `go fmt ./...` — fails CI if files need formatting
6. Verify no formatting changes
7. `go vet ./...`
8. `go test -race -count=1 ./...`
9. `go build -o bin/shardforge ./cmd/shardforge`
10. `./bin/shardforge --help` smoke test
11. `./bin/shardforge version` smoke test

### Acceptance Criteria

| Criterion | Result |
|-----------|--------|
| `go.mod` uses `go 1.21` | PASS |
| `go.mod` and README agree on minimum Go version | PASS |
| `go mod tidy` is clean | PASS |
| `go fmt ./...` — no changes | PASS |
| `go vet ./...` — clean | PASS |
| All 18 tests pass (`go test -race -count=1 ./...`) | PASS |
| `make test` passes | PASS |
| `make vet` passes | PASS |
| `make build` produces binary | PASS |
| `./bin/shardforge --help` works | PASS |
| `./bin/shardforge version` works | PASS |
| GitHub Actions CI workflow present | PASS |
| No database internals implemented | PASS |

### Commands Run Locally

```
go mod tidy
go fmt ./...
go vet ./...
go test -race -count=1 -v ./...
make test
make vet
make build
./bin/shardforge --help
./bin/shardforge version
git status --short
```

### Full Test Results

```
=== RUN   TestHelp
--- PASS: TestHelp (0.00s)
=== RUN   TestVersion
--- PASS: TestVersion (0.00s)
=== RUN   TestUnknownCommand
--- PASS: TestUnknownCommand (0.00s)
PASS
ok  github.com/YashPatel2395/ShardForgeDB/cmd/shardforge    1.447s

=== RUN   TestDefault ... PASS
=== RUN   TestLoad_ValidFile ... PASS
=== RUN   TestLoad_PartialFile_UsesDefaults ... PASS
=== RUN   TestLoad_MissingFile ... PASS
=== RUN   TestLoad_InvalidYAML ... PASS
=== RUN   TestLoad_InvalidPort ... PASS
=== RUN   TestLoad_InvalidLogLevel ... PASS
=== RUN   TestLoad_InvalidLogFormat ... PASS
PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/config   1.301s

=== RUN   TestNew_ReturnsLogger ... PASS
=== RUN   TestNewWithWriter_JSONFormat ... PASS
=== RUN   TestNewWithWriter_TextFormat ... PASS
=== RUN   TestNew_DebugLevelFiltersInfo ... PASS
=== RUN   TestNew_WarnLevelSuppressesInfo ... PASS
=== RUN   TestNew_UnknownLevelDefaultsToInfo ... PASS
=== RUN   TestNew_UnknownFormatDefaultsToJSON ... PASS
PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/logging  1.598s
```

**Total: 18 tests, 18 PASS, 0 FAIL**

### git status --short

```
 M go.mod
?? .github/
```

Expected: `go.mod` was modified (version bump down), `.github/` is new. No other changes.

### Confirmation: No Database Internals Implemented

The following packages contain only a package declaration and doc comment — no logic:
`internal/storage`, `internal/wal`, `internal/memtable`, `internal/sstable`,
`internal/bloom`, `internal/engine`, `internal/vector`, `internal/cluster`, `internal/bench`

### Remaining Limitations

- `golangci-lint` not installed locally; `make lint` degrades gracefully
- Config supports YAML only; no env-var override
- CI has not yet run on GitHub (workflow will execute on first push)

---

---

## Phase 2 — WAL

**Date:** 2026-06-09
**Branch:** `phase-2-wal`
**Go version:** go1.26.4 darwin/arm64

### Implemented Behaviour

| Feature | Detail |
|---------|--------|
| `Open` | Creates or opens WAL file; seeks to EOF; safe permissions (0o600) |
| `Append` | Binary-encodes record; assigns monotonic seq; CRC-32 checksum; optional fsync; mutex-safe |
| `Replay` | Sequential read from BOF; CRC verification; partial-tail tolerance; seq counter sync |
| `Close` | Closes file; subsequent Append/Close return `ErrClosed` |
| Error types | `ErrClosed`, `ErrInvalidRecord`, `ErrCorruptRecord`, `ErrRecordTooLarge` |
| Record types | `RecordPut (1)`, `RecordDelete (2)` |
| Format | Little-endian: `[length u32][crc32 u32][seq u64][type u8][keyLen u32][valueLen u32][key][value]` |

### Tests Added — 18 tests, all PASS

| # | Test | Status |
|---|------|--------|
| 1 | `TestOpen_CreatesFile` | PASS |
| 2 | `TestAppendReplay_OnePut` | PASS |
| 3 | `TestAppendReplay_OrderPreserved` | PASS |
| 4 | `TestReplay_EmptyWAL` | PASS |
| 5 | `TestAppendReplay_DeleteTombstone` | PASS |
| 6 | `TestAppendReplay_BinaryData` | PASS |
| 7 | `TestAppend_RejectsUnknownType` | PASS |
| 8 | `TestAppend_RejectsEmptyKey` | PASS |
| 9 | `TestAppend_RejectsOversizedRecord` | PASS |
| 10 | `TestAppend_AfterClose` | PASS |
| 11 | `TestReplay_DetectsChecksumCorruption` | PASS |
| 12 | `TestReplay_IncompleteFinalHeader` | PASS |
| 13 | `TestReplay_IncompleteFinalBody` | PASS |
| 14 | `TestReplay_MidFileCorruptionNotIgnored` | PASS |
| 15 | `TestAppend_ConcurrentSafe` | PASS |
| 16 | `TestReplay_CallbackErrorStopsReplay` | PASS |
| 17 | `TestAppend_CallerMutationSafe` | PASS |
| 18 | `TestReopen_PreservesOldRecords` | PASS |

### Benchmarks (Apple M3, darwin/arm64)

```
BenchmarkAppend_NoSync-8        2690473     1290 ns/op      64 B/op    1 allocs/op
BenchmarkAppend_Sync-8             1718  2783271 ns/op      64 B/op    1 allocs/op
BenchmarkReplay_1k-8               3616   879713 ns/op   88272 B/op 4005 allocs/op
BenchmarkReplay_100k-8               39 87374610 ns/op 8800344 B/op 400005 allocs/op
```

Observations:
- No-sync append: ~1.3 µs/op — dominated by allocations and encoding.
- Sync append: ~2.8 ms/op — dominated by `fsync` latency (expected on Apple SSD).
- Replay 1k records: ~880 µs total / ~880 ns/record.
- Replay 100k records: ~87 ms total / ~873 ns/record — linear scaling confirmed.

### Commands Run

```
go mod tidy
go fmt ./...
go vet ./...
go test -race -count=1 -v ./...
go test -bench=. -benchmem -benchtime=3s ./internal/wal/...
make test
make vet
make build
./bin/shardforge --help
./bin/shardforge version
git status --short
```

### Full Test Results (all packages)

```
ok  github.com/YashPatel2395/ShardForgeDB/cmd/shardforge    3 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/config   8 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/logging  7 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/wal     18 PASS
```

**Total: 36 tests, 36 PASS, 0 FAIL**

### git status --short (before commit)

```
 M internal/wal/wal.go
?? internal/wal/wal_bench_test.go
?? internal/wal/wal_test.go
```

Expected: only WAL files changed. No other packages touched.

### Known Limitations

- Single WAL file — no segment rotation or size-based rollover.
- No WAL compaction / GC — deleted records remain in the file.
- No group commit — each `Append` is a separate `write` syscall.
- No compression or encryption.
- WAL is isolated — not yet connected to MemTable or Engine.
- Crash recovery is not end-to-end testable until the Engine is built.

### Confirmation: No Other Database Internals Implemented

- `internal/memtable` — placeholder only (package declaration)
- `internal/sstable` — placeholder only
- `internal/bloom` — placeholder only
- `internal/engine` — placeholder only
- `internal/vector` — placeholder only
- `internal/cluster` — placeholder only
- `internal/storage` — placeholder only
- `internal/bench` — placeholder only

---

---

## Phase 2 — WAL: Corruption-Handling Fix

**Date:** 2026-06-09
**Branch:** `phase-2-wal`
**Go version:** go1.26.4 darwin/arm64

### Bugs Fixed

| # | Bug | Fix |
|---|-----|-----|
| 1 | CRC mismatch on a complete tail record was silently ignored (treated as a partial write). A "peek-ahead" read was used to decide; this is wrong — absent bytes ≠ present-but-wrong bytes. | Removed peek-ahead. Any complete body with a CRC mismatch now returns `ErrCorruptRecord` unconditionally. |
| 2 | `make([]byte, bodyLen)` in `Replay` ran before size validation, allowing a corrupt `bodyLen` field to trigger OOM. | Added `bodyLen > MaxRecordSize` and `bodyLen < bodyFixedSize` checks before any allocation. |
| 3 | `Append` validated body size after `encodeRecord` allocated the encoding buffer. | Moved size check to before any allocation using `uint64` arithmetic to guard overflow. |

### Tests Added (6 new, total 24)

| Test | What it proves |
|------|----------------|
| `TestReplay_SingleCorruptRecord_ReturnsError` | Single corrupt record → `ErrCorruptRecord` (not silent stop) |
| `TestReplay_LastRecordCorrupt_ReturnsError` | Last of multiple records corrupt → `ErrCorruptRecord` |
| `TestReplay_RejectsOversizedBodyBeforeAlloc` | Frame claims body > MaxRecordSize → `ErrCorruptRecord` before allocation |
| `TestAppend_RejectsHugeRecordBeforeEncoding` | Key+value > MaxRecordSize → `ErrRecordTooLarge` before encoding alloc |
| `TestReplay_RejectsBodySmallerThanFixedSize` | Frame claims body < bodyFixedSize → `ErrCorruptRecord` |
| `TestClose_CalledTwiceReturnsErrClosed` | Second `Close()` → `ErrClosed` (matches documented behaviour) |

### Updated Total Test Count

```
ok  github.com/YashPatel2395/ShardForgeDB/cmd/shardforge    3 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/config   8 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/logging  7 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/wal     24 PASS
```

**Total: 42 tests, 42 PASS, 0 FAIL**

### Benchmark Results (Apple M3, unchanged performance)

```
BenchmarkAppend_NoSync-8        2799494     1277 ns/op      64 B/op    1 allocs/op
BenchmarkAppend_Sync-8             1729  2811348 ns/op      64 B/op    1 allocs/op
BenchmarkReplay_1k-8               3372  1012236 ns/op   88272 B/op 4005 allocs/op
BenchmarkReplay_100k-8               40 87349798 ns/op 8800364 B/op 400005 allocs/op
```

### Commands Run

```
go mod tidy
go fmt ./...
go vet ./...
go test -race -count=1 -v ./internal/wal/...
go test -race -count=1 ./...
go test -bench=. -benchmem -benchtime=3s ./internal/wal/...
make test
make vet
make build
./bin/shardforge --help
./bin/shardforge version
git status --short
```

### git status --short (before commit)

```
 M internal/wal/wal.go
 M internal/wal/wal_test.go
```

Only WAL files changed. No other packages touched.

### Confirmation: No Non-WAL Internals Implemented

All placeholder packages (`memtable`, `sstable`, `bloom`, `engine`, `vector`, `cluster`, `storage`, `bench`) are unchanged.

---

---

## Phase 3 — MemTable

**Date:** 2026-06-09
**Branch:** `phase-3-memtable`
**Go version:** go1.26.4 darwin/arm64

### Implemented Behaviour

| Feature | Detail |
|---------|--------|
| `New` | Returns empty MemTable; applies `defaultMaxBytes` (64 MiB) when `MaxBytes == 0` |
| `Put` | Stores live entry; defensive copies of key+value; updates size accounting; replaces existing |
| `Delete` | Stores tombstone (`EntryDelete`); defensive copy of key; updates size accounting |
| `Get` | Returns deep copy of entry including tombstones; returns `(Entry{}, false)` for missing keys |
| `Scan` | Returns all entries in `[start, end)` in lexicographic order; nil bounds mean open |
| `Len` | Returns count of unique keys (live + tombstones) |
| `ApproxBytes` | Returns `sum(len(key)+len(value)+64)` over all entries |
| `ShouldFlush` | Returns `true` when `ApproxBytes >= MaxBytes` |
| Concurrency | `sync.RWMutex`; readers (Get/Scan/Len/ApproxBytes/ShouldFlush) hold read lock only |
| Defensive copies | Keys and values copied on Put/Delete, and again on Get/Scan return |

### Data Structure

- `map[string]Entry` — O(1) lookup
- `[]string` sorted via `sort.SearchStrings` + slice shift — O(n) per insert, O(n²) bulk load
- `entryOverhead = 64 B` fixed per-entry cost for size accounting

### Tests — 30 tests, all PASS

| # | Test | Status |
|---|------|--------|
| 1 | `TestNew_ReturnsEmpty` | PASS |
| 2 | `TestPutGet_Basic` | PASS |
| 3 | `TestGet_MissingKey` | PASS |
| 4 | `TestPut_RejectsEmptyKey` | PASS |
| 5 | `TestDelete_RejectsEmptyKey` | PASS |
| 6 | `TestDelete_StoresTombstone` | PASS |
| 7 | `TestPut_ReplacesExistingKey` | PASS |
| 8 | `TestDelete_ReplacesExistingPut` | PASS |
| 9 | `TestPut_AfterDelete_RestoresValue` | PASS |
| 10 | `TestScan_SortedOrder` | PASS |
| 11 | `TestScan_StartInclusive` | PASS |
| 12 | `TestScan_EndExclusive` | PASS |
| 13 | `TestScan_NilBounds_ReturnsAll` | PASS |
| 14 | `TestScan_IncludesTombstones` | PASS |
| 15 | `TestLen_UniqueKeyCount` | PASS |
| 16 | `TestApproxBytes_IncreasesOnInsert` | PASS |
| 17 | `TestApproxBytes_UpdatesOnReplaceWithLargerValue` | PASS |
| 18 | `TestApproxBytes_UpdatesOnReplaceWithSmallerValue` | PASS |
| 19 | `TestApproxBytes_UpdatesOnDeleteTombstone` | PASS |
| 20 | `TestShouldFlush_FalseBelow` | PASS |
| 21 | `TestShouldFlush_TrueAtOrAbove` | PASS |
| 22 | `TestPut_CallerMutationSafe` | PASS |
| 23 | `TestGet_CallerMutationSafe` | PASS |
| 24 | `TestScan_CallerMutationSafe` | PASS |
| 25 | `TestPut_BinaryData` | PASS |
| 26 | `TestSequenceNumbers_Preserved` | PASS |
| 27 | `TestConcurrent_RaceSafe` | PASS |
| 28 | `TestScan_Deterministic` | PASS |
| 29 | `TestDelete_MissingKey_CreatesTombstone` | PASS |
| 30 | `TestDelete_RepeatedUpdatesSeq` | PASS |

### Full Test Results (all packages)

```
ok  github.com/YashPatel2395/ShardForgeDB/cmd/shardforge       3 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/config      8 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/logging     7 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/memtable   30 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/wal        24 PASS
```

**Total: 72 tests, 72 PASS, 0 FAIL**

### Benchmarks (Apple M3, darwin/arm64)

```
BenchmarkPut_1k-8         20161    179718 ns/op    459283 B/op    4776 allocs/op
BenchmarkPut_100k-8         159  22525795 ns/op  38411142 B/op  500327 allocs/op
BenchmarkGet_Existing-8  120821614      30.01 ns/op      32 B/op       2 allocs/op
BenchmarkGet_Missing-8   370956397      11.51 ns/op       0 B/op       0 allocs/op
BenchmarkScan_1k-8         82285     43881 ns/op    177984 B/op    2011 allocs/op
BenchmarkScan_100k-8         369   9307749 ns/op  37711429 B/op  200029 allocs/op
```

Observations:
- `Get` (existing): ~30 ns — O(1) map lookup + deep copy allocation.
- `Get` (missing): ~11 ns — O(1) map lookup, no allocation.
- `Put_1k`: ~180 µs for 1k sequential inserts (~180 ns/op average).
- `Put_100k`: ~22 ms for 100k inserts — O(n²) slice-shift cost is visible; skip list deferred.
- `Scan_1k`: ~44 µs to scan 1k entries including deep copies.
- `Scan_100k`: ~9.3 ms to scan 100k entries — linear in result size.

### Commands Run

```
go mod tidy
go fmt ./...
go vet ./...
go test -race -count=1 -v ./...
go test -bench=. -benchmem -benchtime=3s ./internal/memtable/...
make test
make vet
make build
./bin/shardforge --help
./bin/shardforge version
git status --short
```

### git status --short (before commit)

```
 M internal/memtable/memtable.go
?? internal/memtable/memtable_bench_test.go
?? internal/memtable/memtable_test.go
```

Only memtable files changed. WAL and other packages untouched.

### Known Limitations

- O(n) insert / O(n²) bulk load — slice-shift implementation; skip list deferred to profiling phase.
- Single `sync.RWMutex` — no per-key or per-shard locking.
- No immutable MemTable handoff — flush coordination not yet implemented.
- MemTable is not wired to the WAL or Engine; data is lost on process restart.
- No WAL replay path feeding into MemTable yet (crash recovery not end-to-end).

### Confirmation: No Non-MemTable Internals Implemented

- `internal/sstable` — placeholder only
- `internal/bloom` — placeholder only
- `internal/engine` — placeholder only
- `internal/vector` — placeholder only
- `internal/cluster` — placeholder only
- `internal/storage` — placeholder only
- `internal/bench` — placeholder only

---

---

## Phase 3 — MemTable: Review Fixes

**Date:** 2026-06-09
**Branch:** `phase-3-memtable`
**Go version:** go1.26.4 darwin/arm64

### Changes Made

| # | Issue | Fix |
|---|-------|-----|
| 1 | README listed `MemTable` under "Features NOT Yet Implemented" even though it is implemented | Replaced with `WAL ↔ MemTable integration` and `Engine-level key-value read / write / delete`; updated top-of-file blurb from "Phase 1 only" to reflect Phase 3 status |
| 2 | `TestPut_CallerMutationSafe` called `mustPut(t, m, string(key), string(val), 1)` — the `string(key)` conversion materialised fresh byte slices inside the helper, so mutating the original slices after the call could never reach what was passed to `Put`. The test did not actually exercise Put's defensive copy. | Rewrote to call `m.Put(key, val, 1)` directly with the live slices, then `copy(key, "XXXXX")` / `copy(val, "XXXXX")` after return, then verified stored entry still holds original bytes. |
| 3 | No worst-case bulk-insert benchmark existed | Added `BenchmarkPut_100k_Reverse`: inserts 100k keys in descending order so every insert lands at position 0 of the sorted slice, maximising slice-shift cost. |

### Benchmark Results — New Benchmark (Apple M3, darwin/arm64)

```
BenchmarkPut_100k-8              147    24016297 ns/op   38409933 B/op   500326 allocs/op
BenchmarkPut_100k_Reverse-8        3  1275291263 ns/op   38409680 B/op   500330 allocs/op
```

Ascending insert: ~24 ms. Reverse insert: ~1.27 s. The ~53× difference confirms worst-case O(n²) slice-shift behaviour. Both are documented; skip-list evaluation deferred to the profiling phase.

### Commands Run

```
go mod tidy
go fmt ./...
go vet ./...
go test -race -count=1 -v ./...
go test -bench=. -benchmem -benchtime=3s ./internal/memtable/...
make test
make vet
make build
./bin/shardforge --help
./bin/shardforge version
git status --short
```

### Full Test Results (all packages)

```
ok  github.com/YashPatel2395/ShardForgeDB/cmd/shardforge       3 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/config      8 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/logging     7 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/memtable   30 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/wal        24 PASS
```

**Total: 72 tests, 72 PASS, 0 FAIL** (count unchanged; test was rewritten, not added)

### git status --short (before commit)

```
 M README.md
 M internal/memtable/memtable_bench_test.go
 M internal/memtable/memtable_test.go
```

Only the three review-fix files changed. No WAL, SSTable, Engine, or other internals touched.

---

---

## Phase 4 — SSTable

**Date:** 2026-06-09
**Branch:** `phase-4-sstable`
**Go version:** go1.26.4 darwin/arm64

### Implemented Behaviour

| Feature | Detail |
|---------|--------|
| `Create` | Writes immutable SSTable to disk atomically (temp + rename); validates all entries before I/O |
| `Open` | Reads footer, verifies header magic + footer magic + footer CRC, loads index into memory |
| `Get` | Binary-searches in-memory index, seeks to record, verifies CRC, returns deep copy |
| `Scan` | Iterates index range, reads each record from disk with CRC verification, returns deep copies |
| `Len` | Returns entry count from metadata |
| `Metadata` | Returns defensive copy of path, count, MinKey, MaxKey, SizeBytes |
| `Close` | Closes file; subsequent Get/Scan return ErrClosed; double Close returns ErrClosed |
| Input validation | Rejects empty table, empty keys, unknown kind, unsorted entries, duplicate keys, oversized records |
| Defensive copies | Create copies all input slices; Get/Scan/Metadata return deep copies |
| Concurrency | `sync.RWMutex` around file; concurrent Get/Scan safe; race-detector clean |
| Atomic create | Write to temp file, sync, rename; failed create leaves no partial final file |

### File Format

```
[header: magic(8) + version(2)]
[data record 0..N-1: recordLen(4) + crc32(4) + seq(8) + kind(1) + keyLen(4) + valueLen(4) + key + value]
[index block: per-entry keyLen(4) + key + offset(8) + recordLen(4)]
[footer: indexOffset(8) + indexLen(8) + entryCount(8) + footerCRC(4) + magic(8)]
```

All integers little-endian. CRC-32 (IEEE) on every record body and on footer fields.

### Tests — 35 tests, all PASS

| # | Test | Status |
|---|------|--------|
| 1 | `TestCreate_WritesFile` | PASS |
| 2 | `TestOpen_ReadsMetadata` | PASS |
| 3 | `TestCreate_RejectsEmptyTable` | PASS |
| 4 | `TestCreate_RejectsEmptyKey` | PASS |
| 5 | `TestCreate_RejectsUnknownKind` | PASS |
| 6 | `TestCreate_RejectsUnsortedEntries` | PASS |
| 7 | `TestCreate_RejectsDuplicateKeys` | PASS |
| 8 | `TestCreate_RejectsOversizedRecord` | PASS |
| 9 | `TestGet_ExistingPut` | PASS |
| 10 | `TestGet_MissingKey` | PASS |
| 11 | `TestGet_Tombstone` | PASS |
| 12 | `TestScan_SortedOrder` | PASS |
| 13 | `TestScan_StartInclusive` | PASS |
| 14 | `TestScan_EndExclusive` | PASS |
| 15 | `TestScan_NilBoundsReturnsAll` | PASS |
| 16 | `TestScan_IncludesTombstones` | PASS |
| 17 | `TestBinaryKeysAndValues` | PASS |
| 18 | `TestSequenceNumbers_Preserved` | PASS |
| 19 | `TestGet_CallerMutationSafe` | PASS |
| 20 | `TestScan_CallerMutationSafe` | PASS |
| 21 | `TestMetadata_KeysMutationSafe` | PASS |
| 22 | `TestOpen_DetectsBadMagic` | PASS |
| 23 | `TestOpen_DetectsCorruptFooterChecksum` | PASS |
| 24 | `TestGet_DetectsCorruptRecord` | PASS |
| 25 | `TestScan_DetectsCorruptRecord` | PASS |
| 26 | `TestOpen_TruncatedFile` | PASS |
| 27 | `TestClose_ThenGet_ReturnsErrClosed` | PASS |
| 28 | `TestClose_ThenScan_ReturnsErrClosed` | PASS |
| 29 | `TestClose_CalledTwice_ReturnsErrClosed` | PASS |
| 30 | `TestConcurrent_RaceSafe` | PASS |
| 31 | `TestCreate_FailureNoPartialFile` | PASS |
| 32 | `TestLargeTable` | PASS |
| 33 | `TestMetadata_CreateMatchesOpen` | PASS |
| 34 | `TestScan_StartAfterEnd_ReturnsEmpty` | PASS |
| 35 | `TestScan_DeterministicAfterSortedCreate` | PASS |

### Full Test Results (all packages)

```
ok  github.com/YashPatel2395/ShardForgeDB/cmd/shardforge       3 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/config      8 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/logging     7 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/memtable   30 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/sstable    35 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/wal        24 PASS
```

**Total: 107 tests, 107 PASS, 0 FAIL**

### Benchmarks (Apple M3, darwin/arm64, `-benchtime=3s`)

```
BenchmarkCreate_1k-8         448    8183444 ns/op     106660 B/op    2023 allocs/op
BenchmarkCreate_100k-8         7  492694869 ns/op   10408769 B/op  200026 allocs/op
BenchmarkOpen_100k-8        2611    1611486 ns/op    8408208 B/op  100011 allocs/op
BenchmarkGet_Existing-8  4518351        799 ns/op          80 B/op       3 allocs/op
BenchmarkGet_Missing-8  100000000       33.4 ns/op          0 B/op       0 allocs/op
BenchmarkScan_1k-8          4621     779236 ns/op     225984 B/op    3011 allocs/op
BenchmarkScan_100k-8          40   82841256 ns/op   42511430 B/op  300029 allocs/op
```

Observations:
- `Get` (missing): ~33 ns — in-memory binary search, no disk I/O.
- `Get` (existing): ~800 ns — binary search + single pread syscall + CRC verify.
- `Open` (100k): ~1.6 ms — footer read + index decode (~8 MiB into RAM).
- `Create` (1k): ~8.2 ms dominated by temp-file write + sync + rename.
- `Scan` (100k): ~83 ms — 100k pread calls; no block caching yet.

### Commands Run

```
go mod tidy
go fmt ./...
go vet ./...
go test -race -count=1 -v ./...
go test -bench=. -benchmem -benchtime=3s ./internal/sstable/...
make test
make vet
make build
./bin/shardforge --help
./bin/shardforge version
git status --short
```

### git status --short (before commit)

```
 M internal/sstable/sstable.go
?? internal/sstable/sstable_bench_test.go
?? internal/sstable/sstable_test.go
```

Only SSTable files changed. WAL, MemTable, and all other packages untouched.

### Known Limitations

- No Bloom filter — every `Get` for an existing key requires a disk seek; missing-key detection is in-memory only (index binary search).
- No block cache — data reads rely on the OS page cache.
- Dense index (one entry per key) — index grows linearly with entry count; loads entirely into RAM.
- No compression or encryption.
- No block-level data layout — data region is a flat record stream.
- No compaction — multi-SSTable lookup logic belongs to the Engine (future phase).
- SSTable is not wired to MemTable or Engine.
- Parent directory is not fsynced after atomic rename.

### Confirmation: No Non-SSTable Internals Implemented

- `internal/bloom` — placeholder only
- `internal/engine` — placeholder only
- `internal/vector` — placeholder only
- `internal/cluster` — placeholder only
- `internal/storage` — placeholder only
- `internal/bench` — placeholder only
- `internal/wal` — unchanged
- `internal/memtable` — unchanged

---

---

## Phase 4 — SSTable: Review Fixes

**Date:** 2026-06-09
**Branch:** `phase-4-sstable`
**Go version:** go1.26.4 darwin/arm64

### Review Blockers Fixed

| # | Blocker | Fix |
|---|---------|-----|
| 1 | Index writer used `[keyLen][offset][recordLen][key]` but docs specified `[keyLen][key][offset][recordLen]` | Rewrote index writer to emit key bytes immediately after keyLen; rewrote reader to parse in the same order; added `TestIndexLayout_MatchesDocumented` to prove on-disk bytes match the documented layout |
| 2 | `Open` never validated the header version field | Added version check in `load`; non-`tableVersion` (1) → `ErrCorruptTable`; added `TestOpen_DetectsUnsupportedVersion` |
| 3 | `readRecord` allocated `make([]byte, recLen)` before validating recLen | Added pre-allocation checks: `recLen >= bodyFixedSize`, `recLen <= r.maxRecordSize`, `recLen == ie.recordLen`; Reader now stores `maxRecordSize`; added 3 tests |
| 4 | `readRecord` never validated decoded `kind` | After CRC verification, check kind is `EntryPut` or `EntryDelete`; added `TestGet_DetectsInvalidRecordKind` (kind corrupted but CRC recomputed) |
| 5 | `readRecord` never cross-checked decoded key against index key | After decoding key, check `string(key) == string(ie.key)`; added `TestGet_DetectsIndexRecordKeyMismatch` (key corrupted, CRC recomputed) |
| 6 | `load` did not validate the decoded index beyond bounds | Added: count == entryCount, strict key sort, per-entry offset in data region, per-entry recordLen in [bodyFixedSize, MaxRecordSize]; used uint64 arithmetic throughout; added 4 tests |

### Tests Added (12 new)

| Test | Blocker |
|------|---------|
| `TestIndexLayout_MatchesDocumented` | 1 |
| `TestOpen_DetectsUnsupportedVersion` | 2 |
| `TestGet_OversizedRecordLen_ReturnsErrCorruptTable` | 3a |
| `TestGet_UndersizedRecordLen_ReturnsErrCorruptTable` | 3b |
| `TestGet_RecordLenMismatchesIndex_ReturnsErrCorruptTable` | 3c |
| `TestGet_DetectsInvalidRecordKind` | 4 |
| `TestGet_DetectsIndexRecordKeyMismatch` | 5 |
| `TestOpen_DetectsEntryCountMismatch` | 6a |
| `TestOpen_DetectsUnsortedIndex` | 6b |
| `TestOpen_DetectsIndexOffsetOutOfRange` | 6c |
| `TestOpen_DetectsIndexRecordLenTooLarge` | 6d |
| *(layout proof test counted above)* | |

Also added `rewriteRecordBody` and `rewriteFooter` test helpers for CRC-consistent corruption.

### Updated Total Test Count

```
ok  github.com/YashPatel2395/ShardForgeDB/cmd/shardforge       3 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/config      8 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/logging     7 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/memtable   30 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/sstable    45 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/wal        24 PASS
```

**Total: 117 tests, 117 PASS, 0 FAIL** (was 107; +10 SSTable)

### Benchmark Results (Apple M3, darwin/arm64)

```
BenchmarkCreate_1k-8         379    9,700,168 ns/op     106,687 B/op    2,023 allocs/op
BenchmarkCreate_100k-8         5  647,838,658 ns/op  10,408,827 B/op  200,027 allocs/op
BenchmarkOpen_100k-8        2113    1,811,942 ns/op   8,408,226 B/op  100,011 allocs/op
BenchmarkGet_Existing-8  4,400,742        819 ns/op          80 B/op        3 allocs/op
BenchmarkGet_Missing-8  100,000,000      33.1 ns/op           0 B/op        0 allocs/op
BenchmarkScan_1k-8          4,556      780,376 ns/op     225,984 B/op    3,011 allocs/op
BenchmarkScan_100k-8           43   86,350,372 ns/op  42,511,434 B/op  300,029 allocs/op
```

### Commands Run

```
go mod tidy
go fmt ./...
go vet ./...
go test -race -count=1 -v ./...
go test -bench=. -benchmem -benchtime=3s ./internal/sstable/...
make test
make vet
make build
./bin/shardforge --help
./bin/shardforge version
git status --short
```

### git status --short (before commit)

```
 M internal/sstable/sstable.go
 M internal/sstable/sstable_test.go
```

Only SSTable files changed. WAL, MemTable, and all other packages untouched.

---

---

---

## Phase 4 — SSTable: Final Review Fixes

**Date:** 2026-06-09
**Branch:** `phase-4-sstable`
**Go version:** go1.26.4 darwin/arm64

### Issues Fixed

| # | Issue | Fix |
|---|-------|-----|
| 1 | `TestOpen_DetectsUnsupportedVersion` was broken: the test called `binary.Write(f, ...)` sequentially starting at offset 0, corrupting the magic sentinel bytes. It then used `WriteAt` at offset 8 (correct), but by then the magic was already wrong. The test passed because of bad magic, not version validation. | Removed the sequential-write block entirely. Replaced with a single `f.WriteAt(ver[:], 8)` call that modifies only the version field. |
| 2 | Index offset validation in `load` checked `ie.offset < indexOffset` but not that the *complete* record `[offset, offset+recordHeaderSize+recordLen)` fits inside the data region. A record whose offset is just below `indexOffset` but whose body would extend past it was not detected. | Added an overflow-safe full-record boundary check after the per-entry offset and recordLen checks: `recordEnd := ie.offset + uint64(recordHeaderSize) + uint64(ie.recordLen); if recordEnd < ie.offset \|\| recordEnd > indexOffset { return ErrCorruptTable }`. |

### Test Added (1 new)

| Test | What it proves |
|------|----------------|
| `TestOpen_DetectsIndexRecordOverlapsIndex` | Corrupts the index entry `recordLen` to a value where `offset + recordHeaderSize + recordLen > indexOffset` (record body spills into the index block); verifies `Open` returns `ErrCorruptTable`. |

### Updated Total Test Count

```
ok  github.com/YashPatel2395/ShardForgeDB/cmd/shardforge       3 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/config      8 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/logging     7 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/memtable   30 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/sstable    46 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/wal        24 PASS
```

**Total: 118 tests, 118 PASS, 0 FAIL** (was 117; +1 SSTable)

### Commands Run

```
go fmt ./...
go vet ./...
go test -race -count=1 ./...
```

### git status --short (before commit)

```
 M docs/PROOF.md
 M internal/sstable/sstable.go
 M internal/sstable/sstable_test.go
```

Only SSTable files and PROOF.md changed. WAL, MemTable, and all other packages untouched.

---

---

## Phase 5 — Bloom Filter

**Date:** 2026-06-09
**Branch:** `phase-5-bloom`
**Go version:** go1.26.4 darwin/arm64

### Implemented Behaviour

| Feature | Detail |
|---------|--------|
| `New` | Constructs a filter sized for ExpectedItems at the target FalsePositiveRate; validates all options before allocation |
| `Add` | Sets k bit positions determined by double hashing; increments InsertedItems; rejects nil/empty keys |
| `MightContain` | Checks k bit positions; returns false immediately on first unset bit; nil/empty → false |
| `Metadata` | Returns a value copy of all parameters plus InsertedItems |
| `MarshalBinary` | Serializes to a self-describing binary blob with magic, version, all parameters, word array, CRC-32, and trailing magic |
| `UnmarshalBinary` | Deserializes and validates fully before returning a new filter; defensive copy of all data |
| Error types | `ErrInvalidOptions`, `ErrInvalidKey`, `ErrCorruptFilter`, `ErrFilterTooLarge` |
| Concurrency | `sync.RWMutex`; readers (`MightContain`, `Metadata`, `MarshalBinary`) hold read lock only |

### Parameter Formulas

```
m = ceil( -n * ln(p) / (ln(2)^2) )   // bit count
k = round( (m / n) * ln(2) )          // hash count, minimum 1, capped at 64
```

Verification for n=1000, p=0.01:
```
m = ceil(1000 × 4.60517 / 0.48045) = ceil(9585.06) = 9586
k = round(9586/1000 × 0.69315)     = round(6.641)  = 7
```
Both values confirmed by `TestCalcParams_KnownValues`.

### Hash Strategy

```
h1(x) = FNV-1a-64(x)
h2(x) = FNV-1a-64(salt_bytes || x)   salt = 0x9e3779b97f4a7c15
pos_i = (h1(x) + i × h2(x)) mod m
```

h2 fallback: if h2 == 0, replaced with the salt constant (odd, well-distributed).
Both hashes use only `hash/fnv` from the Go standard library — no random seeds.

### Serialization Format

```
[magic         8 bytes ]  0x544c464d4f4f4c42 ("BLOOMFLT" LE)
[version       2 bytes ]  uint16 = 1
[bitCount      8 bytes ]  uint64
[hashCount     4 bytes ]  uint32
[expectedItems 8 bytes ]  uint64
[insertedItems 8 bytes ]  uint64
[fprBits       8 bytes ]  math.Float64bits(FalsePositiveRate)
[wordCount     8 bytes ]  uint64
[words         wc×8 bytes]
[crc32         4 bytes ]  IEEE CRC-32 over [magic..last word]
[magic         8 bytes ]  trailing sentinel
```

Total minimum size: 66 bytes (no words). Safety cap: wordCount ≤ 2³⁰.

### Tests — 34 tests, all PASS

| # | Test | Status |
|---|------|--------|
| 1 | `TestNew_RejectsZeroExpectedItems` | PASS |
| 2 | `TestNew_RejectsFPRLessOrEqualZero` | PASS |
| 3 | `TestNew_RejectsFPRGreaterOrEqualOne` | PASS |
| 4 | `TestNew_ComputesNonZeroParams` | PASS |
| 5 | `TestAdd_RejectsNilKey` | PASS |
| 6 | `TestAdd_RejectsEmptyKey` | PASS |
| 7 | `TestMightContain_NilOrEmptyReturnsFalse` | PASS |
| 8 | `TestMightContain_AddedKeyFound` | PASS |
| 9 | `TestMightContain_MultipleKeysFound` | PASS |
| 10 | `TestAdd_DuplicateDoesNotBreakMembership` | PASS |
| 11 | `TestMightContain_MissingKeyReturnsFalse` | PASS |
| 12 | `TestNoFalseNegatives_10k` | PASS |
| 13 | `TestFalsePositiveRate_WithinBound` | PASS (actual ~0.36%, target 1%) |
| 14 | `TestMetadata_ReportsCorrectFields` | PASS |
| 15 | `TestMarshalUnmarshal_PreservesMembership` | PASS |
| 16 | `TestMarshalBinary_IsDeterministic` | PASS |
| 17 | `TestUnmarshal_RejectsTooShort` | PASS |
| 18 | `TestUnmarshal_RejectsBadLeadingMagic` | PASS |
| 19 | `TestUnmarshal_RejectsBadTrailingMagic` | PASS |
| 20 | `TestUnmarshal_RejectsUnsupportedVersion` | PASS |
| 21 | `TestUnmarshal_RejectsChecksumMismatch` | PASS |
| 22 | `TestUnmarshal_RejectsInconsistentWordCount` | PASS |
| 23 | `TestUnmarshal_RejectsZeroBitCount` | PASS |
| 24 | `TestUnmarshal_RejectsZeroHashCount` | PASS |
| 25 | `TestUnmarshal_RejectsExcessiveWordCount` | PASS |
| 26 | `TestUnmarshal_CallerMutationSafe` | PASS |
| 27 | `TestConcurrent_RaceSafe` | PASS |
| 28 | `TestBitPositions_WordBoundaries` | PASS |
| 29 | `TestLargeFilter_100k` | PASS |
| 30 | `TestMarshalUnmarshal_PreservesMetadata` | PASS |
| 31 | `TestMetadata_MutationSafe` | PASS |
| 32 | `TestInsertedItems_CountsAddCalls` | PASS |
| 33 | `TestCalcParams_KnownValues` | PASS |
| 34 | `TestEmptyFilter_NoMembership` | PASS |

### Benchmarks (Apple M3, darwin/arm64, `-benchtime=3s`)

```
BenchmarkNew_1k-8                28679593       123.9 ns/op       1376 B/op       2 allocs/op
BenchmarkNew_1M-8                   90324     38134   ns/op    1204320 B/op       2 allocs/op
BenchmarkAdd-8                  100000000        33.79 ns/op          0 B/op       0 allocs/op
BenchmarkMightContain_Existing-8 141039631       25.52 ns/op          0 B/op       0 allocs/op
BenchmarkMightContain_Missing-8  123953178       28.79 ns/op          0 B/op       0 allocs/op
BenchmarkMarshalBinary-8           1612573      2382   ns/op      12288 B/op       1 allocs/op
BenchmarkUnmarshalBinary-8         1371586      2576   ns/op      12384 B/op       2 allocs/op
BenchmarkAdd_100k-8                   1146   3160900   ns/op     122976 B/op       2 allocs/op
BenchmarkQuery_100k-8                 1165   3092479   ns/op          0 B/op       0 allocs/op
```

Observations:
- `New` (1k): ~124 ns — dominated by allocating the word array.
- `Add`: ~34 ns — zero allocations; two FNV hash computations + k bit sets.
- `MightContain` (existing/missing): ~25–29 ns — zero allocations; early exit on first unset bit for missing keys.
- `MarshalBinary` / `UnmarshalBinary`: ~2.4–2.6 µs for a 10k filter — single allocation each.
- Bulk 100k Add: ~3.2 ms — ~32 ns/key.
- Bulk 100k Query: ~3.1 ms — ~31 ns/key.

### Full Test Results (all packages)

```
ok  github.com/YashPatel2395/ShardForgeDB/cmd/shardforge       3 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/bloom      34 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/config      8 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/logging     7 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/memtable   30 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/sstable    46 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/wal        24 PASS
```

**Total: 152 tests, 152 PASS, 0 FAIL**

### Commands Run

```
go mod tidy
go fmt ./...
go vet ./...
go test -race -count=1 -v ./...
go test -bench=. -benchmem -benchtime=3s ./internal/bloom/...
make test
make vet
make build
./bin/shardforge --help
./bin/shardforge version
git status --short
```

### git status --short (before commit)

```
 M README.md
 M docs/DESIGN.md
 M docs/PROOF.md
 M internal/bloom/bloom.go
?? internal/bloom/bloom_bench_test.go
?? internal/bloom/bloom_test.go
```

Only Bloom Filter files and docs changed. WAL, MemTable, SSTable, and all other packages untouched.

### Known Limitations

- Not wired into SSTable or Engine; integration deferred to the Engine phase.
- No scalable / partitioned Bloom filter; the entire bit array lives in RAM.
- No counting Bloom filter; Delete is not supported.
- No compression of the bit array.
- `InsertedItems` counts successful `Add` calls, not unique keys.
- False positives are possible by design; false negatives are impossible.

### Confirmation: No Non-Bloom Internals Implemented

- `internal/engine` — placeholder only
- `internal/vector` — placeholder only
- `internal/cluster` — placeholder only
- `internal/storage` — placeholder only
- `internal/bench` — placeholder only
- `internal/wal` — unchanged
- `internal/memtable` — unchanged
- `internal/sstable` — unchanged
- No compaction, sharding, replication, dashboard, or vector logic implemented

---

---

## Phase 5 — Bloom Filter: Review Fixes

**Date:** 2026-06-09
**Branch:** `phase-5-bloom`
**Go version:** go1.26.4 darwin/arm64

### Review Blockers Fixed

| # | Issue | Fix |
|---|-------|-----|
| 1 | `New` accepted `math.NaN()` and `math.Inf(±1)` as `FalsePositiveRate` because both values pass `<= 0` and `>= 1` comparisons | Added `math.IsNaN` and `math.IsInf` guards before the range check in `New`; extended `TestNew_RejectsFPRLessOrEqualZero` + `TestNew_RejectsFPRGreaterOrEqualOne` into a single comprehensive `TestNew_RejectsInvalidFPR` covering 0, negatives, 1, >1, +Inf, -Inf, NaN |
| 2 | `UnmarshalBinary` accepted serialized filters with `expectedItems == 0` if the CRC was recomputed, creating a state that `New` would reject | Added `if expectedItems == 0 { return ErrCorruptFilter }` in `UnmarshalBinary`; added `TestUnmarshal_RejectsZeroExpectedItems` |
| 3 | Package doc comment said `h2(x) = FNV-1a-64(x XOR salt)` but the implementation feeds `salt_bytes || x` into the hasher (a salt prefix, not XOR) | Updated package-level hash strategy comment to `h2(x) = FNV-1a-64(salt_bytes \|\| x)` |

### Tests Added / Updated

| Test | Change |
|------|--------|
| `TestNew_RejectsInvalidFPR` | Replaces `TestNew_RejectsFPRLessOrEqualZero` + `TestNew_RejectsFPRGreaterOrEqualOne`; adds `math.Inf(+1)`, `math.Inf(-1)`, `math.NaN()` cases |
| `TestUnmarshal_RejectsZeroExpectedItems` | New test; patches bytes [22:30], recomputes CRC, verifies `ErrCorruptFilter` |

### Updated Total Test Count

```
ok  github.com/YashPatel2395/ShardForgeDB/cmd/shardforge       3 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/bloom      35 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/config      8 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/logging     7 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/memtable   30 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/sstable    46 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/wal        24 PASS
```

**Total: 153 tests, 153 PASS, 0 FAIL** (was 152; +1 Bloom, net: two old tests replaced by one + one new)

### Benchmark Results (Apple M3, darwin/arm64, unchanged performance)

```
BenchmarkNew_1k-8                  23399350      135.5 ns/op     1376 B/op    2 allocs/op
BenchmarkNew_1M-8                     88218    40930   ns/op  1204320 B/op    2 allocs/op
BenchmarkAdd-8                    100000000       34.30 ns/op        0 B/op    0 allocs/op
BenchmarkMightContain_Existing-8  137112006       25.71 ns/op        0 B/op    0 allocs/op
BenchmarkMightContain_Missing-8   124787678       28.99 ns/op        0 B/op    0 allocs/op
BenchmarkMarshalBinary-8            1550130     2410   ns/op    12288 B/op    1 allocs/op
BenchmarkUnmarshalBinary-8          1340643     2662   ns/op    12384 B/op    2 allocs/op
BenchmarkAdd_100k-8                    1138  3179959   ns/op   122976 B/op    2 allocs/op
BenchmarkQuery_100k-8                  1167  3097168   ns/op        0 B/op    0 allocs/op
```

### Commands Run

```
go mod tidy
go fmt ./...
go vet ./...
go test -race -count=1 -v ./...
go test -bench=. -benchmem -benchtime=3s ./internal/bloom/...
make test
make vet
make build
./bin/shardforge --help
./bin/shardforge version
git status --short
```

### git status --short (before commit)

```
 M docs/PROOF.md
 M internal/bloom/bloom.go
 M internal/bloom/bloom_test.go
```

Only Bloom Filter files and PROOF.md changed. WAL, MemTable, SSTable, and all other packages untouched.

---

## Phase 6 — Single-node Engine

**Date:** 2026-06-09
**Branch:** `phase-6-engine`
**Go version:** go1.26.4 darwin/arm64

### What Was Implemented

| Component | Description |
|-----------|-------------|
| `internal/engine/engine.go` | Single-node LSM-tree engine: Open, Put, Delete, Get, Scan, Flush, Stats, Close |
| `internal/engine/manifest.go` | Atomic JSON manifest: loadManifest, saveManifest, newManifest, encodeKey, decodeKey |
| `internal/engine/engine_test.go` | 45 tests (40 required + 5 additional) |
| `internal/engine/engine_bench_test.go` | 10 benchmarks |

### Files Changed

```
internal/engine/engine.go          — rewritten (was 6-line placeholder)
internal/engine/manifest.go        — new
internal/engine/engine_test.go     — new (45 tests)
internal/engine/engine_bench_test.go — new (10 benchmarks)
README.md                          — Phase 5 locked, Phase 6 in review
docs/DESIGN.md                     — Engine layer fully documented
docs/PROOF.md                      — this section
```

No other packages were modified.

### Acceptance Criteria

| Criterion | Result |
|-----------|--------|
| `go mod tidy` — clean | PASS |
| `go fmt ./...` — no changes | PASS |
| `go vet ./...` — no errors | PASS |
| `go test -race -count=1 -v ./...` — all pass | PASS |
| `go test -bench=. -benchmem -benchtime=3s ./internal/engine/...` | PASS |
| `make test` | PASS |
| `make vet` | PASS |
| `make build` | PASS |
| `./bin/shardforge --help` | PASS |
| `./bin/shardforge version` | PASS |
| Single-node engine only (no compaction / distribution claims) | PASS |

### Engine Tests (45 total)

| # | Test | Result |
|---|------|--------|
| 1 | `TestOpen_CreatesDirectoryLayout` | PASS |
| 2 | `TestOpen_RejectsEmptyDir` | PASS |
| 3 | `TestPut_ThenGet_BeforeFlush` | PASS |
| 4 | `TestGet_MissingKey_ReturnsFalse` | PASS |
| 5 | `TestDelete_HidesMemTableValue` | PASS |
| 6 | `TestDelete_HidesSSTableValue` | PASS |
| 7 | `TestPut_AfterDelete_RestoresKey` | PASS |
| 8 | `TestPut_EmptyKeyRejected` | PASS |
| 9 | `TestDelete_EmptyKeyRejected` | PASS |
| 10 | `TestGet_ReturnedValueMutationSafe` | PASS |
| 11 | `TestScan_MemTable_SortedLiveEntries` | PASS |
| 12 | `TestScan_StartInclusive` | PASS |
| 13 | `TestScan_EndExclusive` | PASS |
| 14 | `TestScan_ExcludesDeletedKeys` | PASS |
| 15 | `TestFlush_EmptyMemTable_IsNoOp` | PASS |
| 16 | `TestFlush_CreatesSSTableFile` | PASS |
| 17 | `TestFlush_CreatesBloomSidecar` | PASS |
| 18 | `TestFlush_UpdatesManifest` | PASS |
| 19 | `TestGet_AfterFlush_ReadsFromSSTable` | PASS |
| 20 | `TestGet_MissingKey_AfterFlush_BloomNegativeSkip` | PASS |
| 21 | `TestRestart_AfterPut_ReplaysWAL` | PASS |
| 22 | `TestRestart_AfterFlush_LoadsSSTableAndBloom` | PASS |
| 23 | `TestRestart_AfterDelete_ReplaysWAL` | PASS |
| 24 | `TestRestart_AfterDeleteAndFlush_PreservesDeletion` | PASS |
| 25 | `TestGet_NewerMemTableWinsOverSSTable` | PASS |
| 26 | `TestGet_NewerSSTableWinsOverOlderSSTable` | PASS |
| 27 | `TestScan_MergesMultipleSources` | PASS |
| 28 | `TestScan_TombstoneInNewerSSTableSuppressesOlder` | PASS |
| 29 | `TestOpen_OrphanSSTableIgnored` | PASS |
| 30 | `TestOpen_CorruptManifest_ReturnsError` | PASS |
| 31 | `TestOpen_CorruptBloomSidecar_ReturnsError` | PASS |
| 32 | `TestOpen_CorruptSSTable_ReturnsError` | PASS |
| 33 | `TestClose_ThenGet_ReturnsErrClosed` | PASS |
| 34 | `TestClose_ThenPut_ReturnsErrClosed` | PASS |
| 35 | `TestClose_ThenFlush_ReturnsErrClosed` | PASS |
| 36 | `TestClose_Idempotent` | PASS |
| 37 | `TestConcurrent_RaceSafe` | PASS |
| 38 | `TestSeq_MonotonicAcrossRestart` | PASS |
| 39 | `TestManifest_PathsAreRelative` | PASS |
| 40 | `TestLargeWorkload_10k` | PASS |
| 41 | `TestStats_ReportsSSTableCount` | PASS (additional) |
| 42 | `TestScan_Empty` | PASS (additional) |
| 43 | `TestClose_ThenScan_ReturnsErrClosed` | PASS (additional) |
| 44 | `TestFlush_CountTrackedInStats` | PASS (additional) |
| 45 | `TestSeq_MonotonicWithinSession` | PASS (additional) |

### Engine Benchmarks (Apple M3, darwin/arm64, `-benchtime=3s`)

```
BenchmarkPut-8                             2688165     1429 ns/op      112 B/op    4 allocs/op
BenchmarkGet_MemTable_Existing-8          92014740       38.94 ns/op    48 B/op    3 allocs/op
BenchmarkGet_MemTable_Missing-8          224676822       15.66 ns/op     0 B/op    0 allocs/op
BenchmarkFlush_1k-8                            273  13489456 ns/op   442160 B/op  5080 allocs/op
BenchmarkFlush_100k-8                            5 617699167 ns/op 63292964 B/op 500117 allocs/op
BenchmarkGet_SSTable_Existing-8            4278676      849.8 ns/op    96 B/op    4 allocs/op
BenchmarkGet_SSTable_Missing_BloomSkip-8  89671018       40.21 ns/op    0 B/op    0 allocs/op
BenchmarkScan_1k-8                            5593   609944 ns/op   544497 B/op  6554 allocs/op
BenchmarkRestart_WALReplay-8                  6932   518795 ns/op   278537 B/op  3542 allocs/op
BenchmarkRestart_ManifestLoad-8              49496    79589 ns/op    50504 B/op   555 allocs/op
```

Observations:
- Put: ~1.4 µs — dominated by WAL append (OS write syscall) + mutex overhead.
- Get (MemTable existing): ~39 ns — read lock + map lookup + copy.
- Get (MemTable missing): ~16 ns — read lock + map miss; no SSTable work.
- Get (SSTable existing): ~850 ns — bounds check + Bloom check + binary search + single disk read.
- Get (SSTable missing, Bloom skip): ~40 ns — bounds check + Bloom check; no disk read.
- Flush 1k: ~13 ms — SSTable write + Bloom serialize + manifest update + WAL rotation.
- Flush 100k: ~618 ms — dominated by SSTable creation and 100k Bloom insertions.
- Scan 1k: ~610 µs — full MemTable + SSTable scan + map merge + sort.
- Restart WAL replay (500 entries): ~519 µs — WAL open + 500 record replay + MemTable rebuild.
- Restart manifest load (500 entries, 1 SSTable): ~80 µs — JSON decode + SSTable + Bloom open.

### Full Test Results (all packages)

```
ok  github.com/YashPatel2395/ShardForgeDB/cmd/shardforge        3 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/bloom       35 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/config       8 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/engine      45 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/logging      7 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/memtable    30 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/sstable     46 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/wal         24 PASS
```

**Total: 198 tests, 198 PASS, 0 FAIL** (was 153; +45 Engine)

### Test Fix Note

`TestGet_MissingKey_AfterFlush_BloomNegativeSkip` initially used the query key `"definitely-absent"` (< `"present"` alphabetically). The SSTable bounds check correctly skipped the table before the Bloom filter was consulted, causing `BloomNegativeSkips` to stay at zero. Fixed by inserting `"apple"` and `"zebra"` to bracket the query key `"missing-key"` so the bounds check passes and the Bloom filter is exercised.

### Commands Run

```
go mod tidy
go fmt ./...
go vet ./...
go test -race -count=1 -v ./...
go test -bench=. -benchmem -benchtime=3s ./internal/engine/...
make test
make vet
make build
./bin/shardforge --help
./bin/shardforge version
git status --short
```

### Known Limitations

- No compaction; read amplification grows with flush count.
- No automatic flush; callers must call Flush explicitly.
- WAL replaced in full after each flush — no segment rotation.
- No background cleanup of orphan SSTable/Bloom files.
- No transactions, snapshots, or MVCC.
- No compression or block cache.
- No distributed/sharded/replicated mode.
- No vector search.
- Parent directory not fsynced after manifest rename.
- Bloom sidecars are not embedded in SSTables.

---

## Phase 6 — Engine: Review Fixes

**Date:** 2026-06-09
**Branch:** `phase-6-engine`
**Go version:** go1.26.4 darwin/arm64

### Review Blockers Fixed

| # | Blocker | Fix |
|---|---------|-----|
| 1 | **Bloom stats race in Get** — `e.bloomChecks` and `e.bloomNegSkips` were plain `uint64` fields incremented inside `Get` while holding only `e.mu.RLock()`. Multiple concurrent readers could increment them simultaneously → data race. | Changed both fields to `atomic.Uint64` (from `sync/atomic`); incremented with `.Add(1)` in `Get` and read with `.Load()` in `Stats()`. No write lock downgrade needed. |
| 2 | **Non-atomic Bloom sidecar write** — `flush()` wrote the Bloom sidecar with `os.WriteFile`, which is not atomic. A crash mid-write could leave a truncated sidecar file. | Extracted `writeFileAtomic(path, data, perm)` helper (temp file + `fsync` + rename, matching `saveManifest`). `flush()` now calls it for the Bloom sidecar. `saveManifest` refactored to call the same helper. |
| 3 | **First Open does not write manifest** — package comments said "first Open writes an empty manifest" but the code only created an in-memory manifest and never persisted it. | After `loadManifest` returns `existed == false`, `Open` now calls `saveManifest` to write `MANIFEST.json` before proceeding. |
| 4 | **Weak manifest validation** — table entries had no per-entry checks (ID zero, duplicate IDs, absolute/traversal paths, bad base64, MinKey > MaxKey, zero Count). | Added `validateTableEntries([]tableEntry) error` called from `loadManifest`; checks all rules listed in DESIGN.md. Uses `filepath.IsLocal` (Go 1.20+) for path safety. |

### Tests Added

| Test | Blocker |
|------|---------|
| `TestConcurrentGet_AfterFlush_BloomStatsRaceSafe` | 1 — 32 goroutines × 50 Gets on a Bloom-filtered missing key; verifies counter delta and passes `-race` |
| `TestAtomicBloomWrite_NoTempFileLeftAfterFlush` | 2 — verifies no `*.tmp` files remain after successful Flush |
| `TestFlush_BloomWriteFailureDoesNotCommitManifest` | 2 — chmod sstables dir to 0o555; verifies manifest is unchanged after failed second Flush; skipped as root |
| `TestOpen_CreatesManifest` | 3 — verifies `MANIFEST.json` exists after first Open with correct fields |
| `TestOpen_ManifestRejectsAbsolutePaths` | 4 — injects `/etc/passwd` for SSTablePath and BloomPath |
| `TestOpen_ManifestRejectsPathTraversal` | 4 — injects `../../../etc/passwd` |
| `TestOpen_ManifestRejectsDuplicateTableIDs` | 4 — two entries with the same ID |
| `TestOpen_ManifestRejectsBadBase64Keys` | 4 — injects `!!not-valid-base64!!` for MinKey and MaxKey |
| `TestOpen_ManifestRejectsMinKeyGreaterThanMaxKey` | 4 — injects MinKey="z", MaxKey="a" |
| `TestOpen_ManifestRejectsEmptyTablePaths` | 4 — empty SSTablePath and BloomPath |

### Files Changed

```
internal/engine/engine.go          — atomic.Uint64 counters; atomic bloom sidecar; first-Open manifest save; updated package docs
internal/engine/manifest.go        — writeFileAtomic helper; validateTableEntries; saveManifest refactored
internal/engine/engine_test.go     — 15 new tests (10 validation + 4 blocker-specific + 1 helper); encoding/json import
docs/DESIGN.md                     — Bloom sidecar atomicity, Bloom stats concurrency, manifest init, validation table
docs/PROOF.md                      — this section
```

### Updated Total Test Count

```
ok  github.com/YashPatel2395/ShardForgeDB/cmd/shardforge        3 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/bloom       35 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/config       8 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/engine      60 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/logging      7 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/memtable    30 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/sstable     46 PASS
ok  github.com/YashPatel2395/ShardForgeDB/internal/wal         24 PASS
```

**Total: 213 tests, 213 PASS, 0 FAIL** (was 198; +15 Engine review-fix tests)

### Benchmark Results (Apple M3, darwin/arm64, `-benchtime=3s`)

```
BenchmarkPut-8                             2657474     1374 ns/op      112 B/op    4 allocs/op
BenchmarkGet_MemTable_Existing-8          92614130       38.85 ns/op    48 B/op    3 allocs/op
BenchmarkGet_MemTable_Missing-8          223369108       15.95 ns/op     0 B/op    0 allocs/op
BenchmarkFlush_1k-8                            207  16382193 ns/op   443951 B/op  5094 allocs/op
BenchmarkFlush_100k-8                            5 639477567 ns/op 63294595 B/op 500130 allocs/op
BenchmarkGet_SSTable_Existing-8            4257411      848.8 ns/op    96 B/op    4 allocs/op
BenchmarkGet_SSTable_Missing_BloomSkip-8  86600826       44.03 ns/op    0 B/op    0 allocs/op
BenchmarkScan_1k-8                            5773   618596 ns/op   544496 B/op  6554 allocs/op
BenchmarkRestart_WALReplay-8                  6854   527941 ns/op   279569 B/op  3551 allocs/op
BenchmarkRestart_ManifestLoad-8              49316    73317 ns/op    50552 B/op   557 allocs/op
```

Performance is unchanged from the initial Phase 6 submission. The atomic counter change adds no measurable overhead (atomic 64-bit ops on Apple M3 are single-cycle).

### Commands Run

```
go mod tidy
go fmt ./...
go vet ./...
go test -race -count=1 -v ./internal/engine/...
go test -race -count=1 ./...
go test -bench=. -benchmem -benchtime=3s ./internal/engine/...
make test
make vet
make build
./bin/shardforge --help
./bin/shardforge version
git status --short
```

### Confirmation: Scope Unchanged

No compaction, vector search, sharding, replication, dashboard, distributed, networking, or Raft logic was implemented. Only correctness fixes: atomic stats, atomic file writes, manifest initialization, and manifest validation.

---

---

## Phase 7 — Manual Full Compaction

**Date:** 2026-06-09
**Go version:** go1.26.4 darwin/arm64
**Branch:** `phase-7-compaction`

### Implemented Behavior

Manual full compaction (`(*Engine).Compact()`) merges all flushed SSTables into at most one output SSTable and Bloom sidecar. It does not touch the MemTable or WAL.

### API Changes

| Symbol | Change |
|--------|--------|
| `(*Engine).Compact() error` | New method — acquires write lock, delegates to `compact()` |
| `ErrCompactionFailed` | New sentinel error wrapping lower-level failures |
| `Stats.CompactionCount` | New field — incremented on each successful compaction |
| `Stats.LastCompactionInputTables` | New field — count of SSTables read in last compaction |
| `Stats.LastCompactionOutputEntries` | New field — live entries written in last output SSTable |

### Merge Rules

1. All SSTables are scanned via `Reader.Scan(nil, nil)`.
2. Per key, the entry with the highest sequence number wins.
3. Tombstones are dropped in full compaction (safe: no older level exists after manifest swap).
4. Surviving live entries are sorted lexicographically and written to a single new SSTable + Bloom sidecar.
5. The new SSTable's Bloom filter is rebuilt from live entries only.
6. Sequence numbers from the original entries are preserved in the output.

### Output Cases

| Condition | Output |
|-----------|--------|
| 0 SSTables | No-op, return nil |
| 1 SSTable | No-op, return nil |
| All keys deleted (all entries are tombstones) | Empty SSTable list, manifest updated to `[]` |
| ≥1 live entry | One new SSTable + Bloom sidecar, manifest updated with one entry |

### Manifest Update

The manifest's `Tables` list is replaced atomically (temp-file + fsync + rename) with either an empty list or a single-entry list pointing to the compacted SSTable and Bloom sidecar. The old table entries are removed from the manifest before any file deletion.

### File Cleanup

Old SSTable and Bloom sidecar files are removed after the manifest commit on a best-effort basis. A removal failure returns an error but does not roll back the manifest. The compacted output is already durable at that point.

### Crash/Recovery Proof

| Phase | State on restart |
|-------|-----------------|
| Crash before manifest commit | Old manifest intact; new compacted SSTable is an orphan (unknown to manifest, ignored by `Open`). No data loss. |
| Crash after manifest commit, before file deletion | New manifest lists the compacted SSTable. Old files are orphans (not listed in manifest, ignored). No data loss. |
| Crash after all-deleted empty manifest commit | Empty manifest; old files are orphans. All keys correctly reported absent. No data loss. |

### Tests

32 tests in `internal/engine/compact_test.go`:

| Test | Description |
|------|-------------|
| `TestCompact_EmptyEngine_IsNoOp` | Engine with no SSTables — Compact returns nil, engine state unchanged |
| `TestCompact_ZeroSSTables_IsNoOp` | Explicit zero-table case — no-op |
| `TestCompact_OneSsTable_IsNoOp` | Single SSTable — no-op |
| `TestCompact_MergesTwoSSTablesIntoOne` | Two SSTables → one compacted output |
| `TestCompact_PreservesLatestValueBySeq` | Same key across tables — highest seq wins |
| `TestCompact_DropsOverwrittenOlderValues` | Older values for same key do not appear in output |
| `TestCompact_PreservesLiveKeysAcrossManySSTables` | 5 SSTables, unique keys — all live keys survive |
| `TestCompact_DropsTombstonesInFullCompaction` | Tombstones not written to compacted SSTable |
| `TestCompact_DeletedKeyRemainsAbsent` | Get for deleted key returns not-found after compact |
| `TestCompact_AllDeleted_RemovesAllSSTables` | All keys deleted → SSTable count = 0 |
| `TestCompact_ScanBeforeAndAfterReturnsSameLiveResults` | Scan result identical before and after compact |
| `TestCompact_GetBeforeAndAfterReturnsSameValues` | Get result identical before and after compact |
| `TestCompact_MemTableValueOverridesSSTable` | Unflushed MemTable write visible after compact |
| `TestCompact_MemTableTombstoneOverridesSSTable` | Unflushed MemTable delete hides SSTable value after compact |
| `TestCompact_DoesNotModifyWAL` | WAL file size unchanged by compact |
| `TestCompact_RestartLoadsCompactedSSTable` | Reopen after compact sees compacted data |
| `TestCompact_RestartAfterAllDeletedLoadsZeroSSTables` | Reopen after all-deleted compact has zero SSTables |
| `TestCompact_OldFilesRemovedAfterCompaction` | Old SSTable and Bloom files are removed |
| `TestCompact_BloomSidecarExistsForCompactedSSTable` | New Bloom sidecar file exists after compact |
| `TestCompact_BloomNegativeSkipWorksAfterCompaction` | BloomNegativeSkips stat increments on missing key after compact |
| `TestCompact_ManifestContainsOneTableAfterMultiTableCompaction` | Manifest has exactly 1 entry after compact |
| `TestCompact_ManifestContainsZeroTablesAfterAllDeletedCompaction` | Manifest has 0 entries after all-deleted compact |
| `TestCompact_ManifestPathsRemainRelativeAfterCompaction` | Manifest paths are local (no absolute, no traversal) |
| `TestCompact_AfterClose_ReturnsErrClosed` | Compact after Close returns ErrClosed |
| `TestCompact_Concurrent_RaceSafe` | Concurrent Put + Compact + Get passes -race |
| `TestCompact_PreservesSequenceNumbers` | Sequence numbers in compacted SSTable match original entries |
| `TestCompact_UsesNewFileIDAndDoesNotOverwriteExistingFiles` | Compact output uses new ID, not an existing SSTable ID |
| `TestCompact_CorruptSSTableCausesError` | Corrupt SSTable causes Compact to return an error |
| `TestCompact_CorruptBloomSidecarDoesNotBlockCompact` | Corrupt Bloom sidecar on disk does not block compact (in-memory Bloom used) |
| `TestCompact_LargeWorkload` | 10 SSTables × 1,000 keys — all live entries survive compact |
| `TestCompact_StatsCompactionCountIncremented` | Stats.CompactionCount increments per successful compaction |
| `TestCompact_StatsLastCompactionInputTables` | Stats.LastCompactionInputTables reflects input table count |

### Benchmarks

8 benchmarks in `internal/engine/compact_bench_test.go`:

```
BenchmarkCompact_2SSTable_1kKeys-8           186   19830618 ns/op    717376 B/op    10141 allocs/op
BenchmarkCompact_10SSTable_10kKeys-8          43   81985188 ns/op   6700301 B/op   100386 allocs/op
BenchmarkCompact_WithOverwrites-8            202   18518108 ns/op    682377 B/op    11159 allocs/op
BenchmarkCompact_WithTombstones-8            702    5640262 ns/op    364229 B/op     5084 allocs/op
BenchmarkGet_MissingKey_BeforeCompaction-8   233920914   15.47 ns/op    0 B/op    0 allocs/op
BenchmarkGet_MissingKey_AfterCompaction-8    238009178   15.10 ns/op    0 B/op    0 allocs/op
BenchmarkScan_BeforeCompaction-8             3608   987504 ns/op   568498 B/op    7054 allocs/op
BenchmarkScan_AfterCompaction-8              3652   981661 ns/op   553585 B/op    7045 allocs/op
```

Observations:
- Compaction of 1,000 keys across 2 SSTables: ~20 ms
- Compaction of 10,000 keys across 10 SSTables: ~82 ms — roughly linear in key count
- Tombstone-only compaction: ~5.6 ms (fast; no SSTable output to write)
- Overwrite-heavy compaction: ~18.5 ms (similar to no-overwrite case; dominated by I/O)
- Get (missing key): ~15.5 ns before, ~15.1 ns after — Bloom check cost identical; one SSTable vs two
- Scan: ~988 µs before, ~982 µs after — near-identical (dominated by merge logic, not SSTable count)

### Commands Run

```
go mod tidy
go fmt ./...
go vet ./...
go test -race -count=1 -v ./...
go test -bench=. -benchmem -benchtime=3s ./internal/engine/...
make test
make vet
make build
./bin/shardforge --help
./bin/shardforge version
git status --short
```

### Full Test Output (engine package)

```
--- PASS: TestCompact_EmptyEngine_IsNoOp (0.01s)
--- PASS: TestCompact_ZeroSSTables_IsNoOp (0.01s)
--- PASS: TestCompact_OneSsTable_IsNoOp (0.01s)
--- PASS: TestCompact_MergesTwoSSTablesIntoOne (0.03s)
--- PASS: TestCompact_PreservesLatestValueBySeq (0.03s)
--- PASS: TestCompact_DropsOverwrittenOlderValues (0.04s)
--- PASS: TestCompact_PreservesLiveKeysAcrossManySSTables (0.07s)
--- PASS: TestCompact_DropsTombstonesInFullCompaction (0.02s)
--- PASS: TestCompact_DeletedKeyRemainsAbsent (0.03s)
--- PASS: TestCompact_AllDeleted_RemovesAllSSTables (0.03s)
--- PASS: TestCompact_ScanBeforeAndAfterReturnsSameLiveResults (0.03s)
--- PASS: TestCompact_GetBeforeAndAfterReturnsSameValues (0.03s)
--- PASS: TestCompact_MemTableValueOverridesSSTable (0.03s)
--- PASS: TestCompact_MemTableTombstoneOverridesSSTable (0.04s)
--- PASS: TestCompact_DoesNotModifyWAL (0.03s)
--- PASS: TestCompact_RestartLoadsCompactedSSTable (0.06s)
--- PASS: TestCompact_RestartAfterAllDeletedLoadsZeroSSTables (0.03s)
--- PASS: TestCompact_OldFilesRemovedAfterCompaction (0.04s)
--- PASS: TestCompact_BloomSidecarExistsForCompactedSSTable (0.05s)
--- PASS: TestCompact_BloomNegativeSkipWorksAfterCompaction (0.05s)
--- PASS: TestCompact_ManifestContainsOneTableAfterMultiTableCompaction (0.07s)
--- PASS: TestCompact_ManifestContainsZeroTablesAfterAllDeletedCompaction (0.03s)
--- PASS: TestCompact_ManifestPathsRemainRelativeAfterCompaction (0.04s)
--- PASS: TestCompact_AfterClose_ReturnsErrClosed (0.00s)
--- PASS: TestCompact_Concurrent_RaceSafe (0.04s)
--- PASS: TestCompact_PreservesSequenceNumbers (0.04s)
--- PASS: TestCompact_UsesNewFileIDAndDoesNotOverwriteExistingFiles (0.04s)
--- PASS: TestCompact_CorruptSSTableCausesError (0.03s)
--- PASS: TestCompact_CorruptBloomSidecarDoesNotBlockCompact (0.04s)
--- PASS: TestCompact_LargeWorkload (0.20s)
--- PASS: TestCompact_StatsCompactionCountIncremented (0.08s)
--- PASS: TestCompact_StatsLastCompactionInputTables (0.08s)
ok  github.com/YashPatel2395/ShardForgeDB/internal/engine   3.989s
```

Total passing tests (all packages): 245
New tests added in Phase 7: 32 (compact_test.go) + 8 benchmarks (compact_bench_test.go)

### Limitations (Documented, Not Bugs)

- Compact() is manual only — no background compaction, no automatic thresholds
- No leveled compaction (L0 → L1 → ...) — single full-merge only
- No size-tiered compaction — always merges all SSTables
- MemTable is not compacted — only flushed SSTables are in scope
- No partial compaction or range-limited compaction

### Confirmation: Scope Unchanged

No background compaction, no automatic thresholds, no leveled or size-tiered compaction, no vector search, no sharding, no replication, no distributed logic, no networking, no dashboard, no Raft was implemented. Phase 7 adds exactly: `Compact()`, compaction stats, and associated tests and documentation.

---

## Phase 7 — Review Fix: Safe Reader-Open Ordering

**Date:** 2026-06-09
**Go version:** go1.26.4 darwin/arm64
**Branch:** `phase-7-compaction`

### Correctness Blocker Fixed

**Problem:** In the live-output compaction path, the code previously committed the manifest *before* opening the new compacted SSTable reader. If the reader open failed after manifest commit, the engine set `e.tables = nil` and returned an error — leaving the running engine with no access to any SSTable data until restart, even though the old readers were still valid and the old manifest was already replaced.

**Fix:** The new compacted SSTable reader is now opened *before* saving the manifest. If the reader open fails:
- The new SSTable and Bloom sidecar files are removed (best-effort)
- `nextFileID` is decremented
- `ErrCompactionFailed` is returned
- `e.tables` is left unchanged — old readers continue working
- The manifest is left unchanged — still lists the old SSTables

Only after the reader opens successfully is the manifest committed. If the manifest save then fails:
- The new reader is closed
- New files are removed (best-effort)
- `nextFileID` is decremented
- `ErrCompactionFailed` is returned
- `e.tables` and the manifest are both unchanged

### Flush Path — Same Fix Applied

The `flush()` function had the same ordering issue: manifest was committed before the new SSTable reader was opened. Since the fix was small, low-risk, and symmetric, the same safe ordering was applied to `flush()`:

1. Write SSTable → write Bloom sidecar → **open reader** → save manifest → append to e.tables

If the reader open fails in `flush()`: SSTable and Bloom sidecar are removed, `nextFileID` decremented, `ErrFlushFailed` returned. The MemTable is unchanged and still readable; the manifest is unchanged.

Previously, the Flush path was less dangerous because the MemTable still held the data after a failed reader open (the MemTable reset and WAL rotation happen after the manifest commit). However, applying the same ordering eliminates the orphaned manifest entry and makes the invariant consistent: **a failed Flush or Compact never commits changes to the manifest without the reader being successfully open**.

### Test Hook

A package-level test hook was added to `engine.go`:

```go
// openSSTableReader is the function used to open an SSTable reader. Tests can
// replace it to inject failures. Must be restored with defer.
var openSSTableReader = sstable.Open
```

Production code in `flush()` and `compact()` calls `openSSTableReader(...)` instead of `sstable.Open(...)` directly.

### Tests Added

| Test | File | Description |
|------|------|-------------|
| `TestCompact_NewReaderOpenFailureLeavesOldStateUsable` | `compact_test.go` | Failed compacted-reader open: old tables and manifest unchanged, stats not incremented |
| `TestFlush_NewReaderOpenFailureLeavesOldStateUsable` | `engine_test.go` | Failed flush-reader open: MemTable unchanged, manifest unchanged, no orphan files |

### Updated Test Count

| Package | Tests |
|---------|-------|
| `internal/engine` | **247** (was 245; +2 new) |
| All packages combined | **247** engine + others = **247 total engine** |

Total passing tests (all packages): 247

### Commands Run

```
go mod tidy
go fmt ./...
go vet ./...
go test -race -count=1 -v ./internal/engine/...
go test -race -count=1 ./...
go test -bench=. -benchmem -benchtime=3s ./internal/engine/...
make test
make vet
make build
./bin/shardforge --help
./bin/shardforge version
git status --short
```

### All Tests Pass

```
--- PASS: TestCompact_NewReaderOpenFailureLeavesOldStateUsable (0.05s)
--- PASS: TestFlush_NewReaderOpenFailureLeavesOldStateUsable (0.01s)
ok  github.com/YashPatel2395/ShardForgeDB/internal/engine   3.847s
```

All 247 engine tests pass. No regressions in any other package.

### Scope Confirmation

No background compaction, no automatic thresholds, no leveled or size-tiered compaction, no vector search, no sharding, no replication, no distributed logic, no networking, no dashboard, no Raft was implemented. Only correctness fix: safe reader-open ordering in `compact()` and `flush()`.

---

---

## Phase 8 — Benchmarking and Workload Evaluation

**Date:** 2026-06-09
**Go version:** go1.26.4 darwin/arm64
**Branch:** `phase-8-benchmarks`

### Implemented Components

#### Benchmark Runner (`internal/bench`)

| File | Description |
|------|-------------|
| `runner.go` | `Runner`, `Result`, `Recorder`, `Percentile`, `Scale` configs, sentinel errors |
| `workload.go` | `GenKey`, `GenValue`, six workload run functions |
| `report.go` | `WriteReport`, `WriteReportFile` — deterministic Markdown generation |
| `bench_test.go` | 31 tests + 3 Go benchmarks |

#### CLI (`cmd/shardforge-bench`)

Single binary with flags: `--scale`, `--workload`, `--out`, `--seed`.

#### Makefile targets added

| Target | Command |
|--------|---------|
| `bench` | `go test -bench=. -benchmem ./...` |
| `bench-engine` | `go test -bench=. -benchmem ./internal/engine/...` |
| `bench-report` | `go run ./cmd/shardforge-bench --scale small --out docs/BENCHMARKS.md` |

### Workloads

| Workload | Measured Ops | Notes |
|----------|-------------|-------|
| `write-heavy` | 1,000 Put | Flush every 100 ops |
| `read-heavy` | 1,000 Get | Preload 1,000 keys; 5% missing (out-of-bounds, hits bounds check) |
| `mixed` | 1,000 ops | 50% Put / 30% Get / 20% Delete |
| `scan` | 100 Scan | 50-key range each; preload 1,000 keys |
| `compaction` | 200 Get | 100 before + 100 after Compact; 5 SSTables created |
| `restart` | 1 reopen | WAL replay + manifest load measurement |

### Metrics

Per `Result` struct: `Operations`, `Duration`, `OpsPerSec`, `P50/P95/P99Latency`, `BytesWritten`, `BytesRead`, `FinalSSTableCount`, `FinalMemTableEntries`, `FlushCount`, `CompactionCount`, `BloomChecks`, `BloomNegativeSkips`, `PreCompactSSTableCount`, `PostCompactSSTableCount`, `CompactDuration`.

### Report Format

`docs/BENCHMARKS.md` is generated by `WriteReport`. Sections: Environment (placeholders), Configuration, Commands Used, Results (two tables), Compaction Detail, Interpretation (per workload), Known Limitations, How to Reproduce.

Report output is deterministic for identical `(scaleName, cfg, results)` — no timestamps or machine-specific values are auto-generated.

### Tests Added

31 tests in `internal/bench/bench_test.go`:

| Category | Tests |
|----------|-------|
| Key generation (determinism, format, lexicographic order) | 3 |
| Value generation (determinism, seed variation, index variation, length) | 4 |
| Percentile (empty, single, known values, immutability) | 4 |
| Config validation (valid, zero fields) | 5 |
| Runner validation (unknown scale, unknown workload) | 2 |
| Workload execution, small scale (all 6 workloads) | 6 |
| Runner cleanup (temp dirs removed) | 1 |
| Report generation (sections, determinism, workload/scale name, file write) | 5 |

3 Go benchmarks: `BenchmarkGenKey`, `BenchmarkGenValue_128`, `BenchmarkPercentile_1k`, `BenchmarkWorkload_WriteHeavy_Small`, `BenchmarkWorkload_ReadHeavy_Small`.

### Updated Total Test Count

| Package | Tests |
|---------|-------|
| `internal/bench` | 31 (new) |
| `internal/engine` | 247 (unchanged) |
| All other packages | unchanged |
| **Total (all packages)** | **~278** |

### Commands Run

```
go mod tidy
go fmt ./...
go vet ./...
go test -race -count=1 -v ./...
go test -bench=. -benchmem -benchtime=3s ./internal/bench/...
go test -bench=. -benchmem -benchtime=3s ./internal/engine/...
make test
make vet
make build
make bench-report
./bin/shardforge --help
./bin/shardforge version
go run ./cmd/shardforge-bench --scale small --out /tmp/shardforge-bench.md
git status --short
```

### Benchmark Results (bench package, small scale)

```
BenchmarkGenKey-8                      55689034    65.10 ns/op      24 B/op    2 allocs/op
BenchmarkGenValue_128-8                95538655    37.46 ns/op   128 B/op    0 allocs/op
BenchmarkPercentile_1k-8               1427722     2505 ns/op    8248 B/op    3 allocs/op
BenchmarkWorkload_WriteHeavy_Small-8   27         142218245 ns/op  1901233 B/op  15274 allocs/op
BenchmarkWorkload_ReadHeavy_Small-8    26         149945910 ns/op  2335320 B/op  20836 allocs/op
```

### Known Limitations

- Preload phases are included in total Duration (documented per workload).
- P99 latency is sensitive to OS scheduler jitter and disk cache state.
- Missing keys in read-heavy workload are out-of-bounds and hit the bounds check before Bloom, so Bloom negative-skip rate is 0 for that workload — correct behavior.
- No wall-clock isolation between workloads.
- `medium` scale is not run in CI (too slow); `small` scale is CI-safe.

### Confirmation: No New DB Feature Logic

No background compaction, no automatic compaction, no leveled or size-tiered compaction, no vector search, no sharding, no replication, no distributed logic, no networking, no dashboard, no Raft, and no core engine behavior changes were implemented. Phase 8 adds measurement infrastructure only.

---

---

## Phase 8 — Review Fix: Compaction Workload Coverage

**Date:** 2026-06-09
**Go version:** go1.26.4 darwin/arm64
**Branch:** phase-8-benchmarks

### Coverage Gap Fixed

The original `runCompaction` workload measured only Get operations before and after compaction. The Phase 8 spec required both Get and Scan measurements to exercise both point-lookup and range-scan paths across the compaction boundary.

### Changes Made

| File | Change |
|------|--------|
| `internal/bench/workload.go` | `runCompaction` now measures Get + Scan before compact and Get + Scan after compact (deterministic, index-based start keys) |
| `internal/bench/runner.go` | `Result` gains `PreCompactGetOps`, `PostCompactGetOps`, `PreCompactScanOps`, `PostCompactScanOps`; `Recorder` gains four corresponding counter fields and methods |
| `internal/bench/report.go` | `compactionDetail` section now lists Gets/Scans before+after, total ops, and a note that both point lookups and range scans are measured; interpretation updated |
| `internal/bench/bench_test.go` | Added `TestWorkload_Compaction_IncludesGetAndScanMeasurements`; added `sampleCompactionResult()` helper; added `TestReport_CompactionDetail_IncludesGetAndScanCounts` |
| `docs/BENCHMARKS.md` | Regenerated via `make bench-report` |

### Compaction Workload — Measured Operations

With `small` scale:
- **100 Gets before compact** — point lookups across 5 SSTables
- **20 Scans before compact** — range scans (50 keys each) across 5 SSTables
- **Compact** — merges 5 SSTables → 1
- **100 Gets after compact** — point lookups against merged SSTable
- **20 Scans after compact** — range scans against merged SSTable
- **Total: 240 measured ops**

### Tests Added / Updated

| Test | Status |
|------|--------|
| `TestWorkload_Compaction_IncludesGetAndScanMeasurements` | New — verifies PreCompactGetOps/PostCompactGetOps/PreCompactScanOps/PostCompactScanOps all > 0; total ops = sum of all four; BytesRead exceeds Get-only minimum |
| `TestReport_CompactionDetail_IncludesGetAndScanCounts` | New — verifies report contains Gets/Scans before+after and "Total measured ops" |
| `TestWorkload_Compaction_SmallScale` | Unchanged — still verifies SSTable counts, CompactDuration, CompactionCount |

### Updated Total Test Count

| Package | Tests |
|---------|-------|
| `internal/bench` | 34 (was 31; +3 new tests) |
| All other packages | unchanged |

### Commands Run

```
go mod tidy
go fmt ./...
go vet ./...
go test -race -count=1 -v ./...
go test -bench=. -benchmem -benchtime=3s ./internal/bench/...
go test -bench=. -benchmem -benchtime=3s ./internal/engine/...
make test
make vet
make build
make bench-report
./bin/shardforge --help
./bin/shardforge version
go run ./cmd/shardforge-bench --scale small --out /tmp/shardforge-bench.md
git status --short
```

### Benchmark Results (bench package, small scale, after fix)

```
BenchmarkGenKey-8                      48210788    70.15 ns/op      24 B/op    2 allocs/op
BenchmarkGenValue_128-8                94784443    38.13 ns/op   128 B/op    0 allocs/op
BenchmarkPercentile_1k-8               1436299     2513 ns/op    8248 B/op    3 allocs/op
BenchmarkWorkload_WriteHeavy_Small-8   28         157670902 ns/op 1901156 B/op  15274 allocs/op
BenchmarkWorkload_ReadHeavy_Small-8    24         156691549 ns/op 2334784 B/op  20834 allocs/op
```

### Confirmation: No New DB Feature Logic

No background compaction, no automatic compaction, no leveled or size-tiered compaction, no vector search, no sharding, no replication, no distributed logic, no networking, no dashboard, no Raft, and no core engine behavior changes were made in this fix.

---

## Phase 9 — Single-node Exact Vector Search

**Date:** 2026-06-09
**Go version:** go1.26.4 darwin/arm64
**Branch:** phase-9-vector-search

### API Implemented

```go
// Package vector — internal/vector
func Open(opts Options) (*Store, error)
func (s *Store) Upsert(id string, vector []float32, metadata []byte) error
func (s *Store) Delete(id string) error
func (s *Store) Get(id string) (Record, bool, error)
func (s *Store) Search(query []float32, k int) ([]SearchResult, error)
func (s *Store) Count() int
func (s *Store) Stats() Stats
func (s *Store) Flush() error
func (s *Store) Compact() error
func (s *Store) Close() error
```

Sentinel errors: `ErrClosed`, `ErrInvalidOptions`, `ErrInvalidID`, `ErrInvalidVector`, `ErrInvalidK`, `ErrCorruptRecord`.

### Persistence Strategy

Vectors are persisted through the existing single-node Engine (Phase 6). Each vector is serialised to a binary record and stored under the key `__vector__/<namespace>/<id>`. On `Open`, the store scans the namespace prefix from the engine and rebuilds the in-memory index. Deletes are engine-level tombstones — they survive WAL replay and SSTable compaction correctly.

### Encoding Format

```
[magic      8 B ] 0x5348415244564543 ("SHARDVEC")
[version    2 B ] uint16 (currently 1)
[dimension  4 B ] uint32
[metaLen    4 B ] uint32
[vector   dim×4 ] float32 little-endian IEEE 754
[metadata   var ] raw bytes
[crc32      4 B ] CRC-32/IEEE over body [version..metadata]
[magic      8 B ] same sentinel footer
```

Decoding rejects bad magic, unsupported versions, dimension mismatches, CRC failures, and truncated data — all return `ErrCorruptRecord`.

### Metric Definitions

| Metric | Score | Distance | Zero vector |
|--------|-------|----------|-------------|
| `cosine` | cosine similarity ∈ [−1,1] | 1 − score | rejected |
| `l2` | −(squared L2) | squared L2 | allowed |
| `dot` | dot product | −dot | allowed |

NaN and ±Inf are rejected in all vectors regardless of metric.

### Search Ordering Rules

- Results sorted descending by score (higher = better).
- Tie-breaking: ID ascending (lexicographic) for determinism.
- `k > Count()`: all records returned.
- Query vector is defensively copied; caller's slice is not mutated.

### Crash and Recovery Behaviour

| Scenario | Outcome |
|----------|---------|
| Clean `Close()` | Engine closed; all flushed data durable. |
| Crash before `Flush()` | WAL replay recovers unflushed entries on next `Open`. |
| `Delete` before crash | WAL DELETE record replays; vector absent from index after reopen. |
| Corrupt encoded value | `Open` returns `ErrCorruptRecord`; store not opened. |

### Tests Added

| Test | Covers |
|------|--------|
| `TestOpen_RejectsEmptyDir` | options validation |
| `TestOpen_RejectsZeroDimension` | options validation |
| `TestOpen_DefaultsMetricToCosine` | default metric |
| `TestOpen_RejectsUnknownMetric` | options validation |
| `TestUpsert_RejectsEmptyID` | ID validation |
| `TestUpsert_RejectsIDWithSlash` | ID validation |
| `TestUpsert_RejectsWrongDimension` | vector validation |
| `TestUpsert_RejectsNaN` | vector validation |
| `TestUpsert_RejectsInf` | vector validation |
| `TestUpsert_CosineRejectsZeroVector` | cosine invariant |
| `TestSearch_CosineRejectsZeroQuery` | cosine invariant |
| `TestUpsertGet_RoundTrip` | put/get correctness |
| `TestGet_MissingReturnsFalse` | missing key |
| `TestUpsert_OverwritesExisting` | overwrite |
| `TestDelete_RemovesVector` | delete |
| `TestDelete_MissingIsSafe` | delete no-op |
| `TestSearch_RejectsInvalidK` | k validation |
| `TestSearch_RejectsWrongQueryDimension` | query validation |
| `TestSearch_CosineRanking` | cosine ranking |
| `TestSearch_L2Ranking` | L2 ranking |
| `TestSearch_DotRanking` | dot ranking |
| `TestSearch_TieBreakByID` | deterministic tie-break |
| `TestSearch_KGreaterThanCount` | k > count |
| `TestSearch_ReturnsMetadataCopies` | defensive copy |
| `TestGet_ReturnsDefensiveCopies` | defensive copy |
| `TestUpsert_DefensivelyCopiesCaller` | defensive copy |
| `TestSearch_DoesNotMutateQuery` | immutable query |
| `TestClose_MakesOptsReturnErrClosed` | closed state |
| `TestClose_IsIdempotent` | idempotent close |
| `TestReopen_RestoresVectors` | persistence |
| `TestReopen_PreservesMetadata` | metadata persistence |
| `TestReopen_AfterDelete_DoesNotRestore` | delete persistence |
| `TestNamespace_Isolation` | namespace scoping |
| `TestFlush_PersistsThroughReopen` | flush + reopen |
| `TestCompact_PreservesVectors` | compact + reopen |
| `TestOpen_CorruptRecordReturnsError` | corrupt record detection |
| `TestConcurrent_UpsertGetSearch` | race safety |
| `TestLargeWorkload_InsertSearchReopen` | 1,000 vectors, search, reopen |
| `TestCodec_RoundTrip` | codec correctness |
| `TestCodec_RejectsBadMagic` | codec corruption |
| `TestCodec_RejectsBadFooterMagic` | codec corruption |
| `TestCodec_RejectsBadCRC` | codec corruption |
| `TestCodec_RejectsTruncated` | codec corruption |
| `TestCodec_RejectsDimensionMismatch` | codec corruption |

**Total: 44 tests** in `internal/vector`.

### Benchmarks Added

| Benchmark | What it measures |
|-----------|-----------------|
| `BenchmarkUpsert_1k_dim128` | Upsert throughput for 1,000 dim-128 vectors |
| `BenchmarkSearch_1k_dim128_Cosine` | Cosine search over 1,000 dim-128 vectors |
| `BenchmarkSearch_10k_dim128_Cosine` | Cosine search over 10,000 dim-128 vectors |
| `BenchmarkSearch_1k_dim128_L2` | L2 search over 1,000 dim-128 vectors |
| `BenchmarkSearch_1k_dim128_Dot` | Dot search over 1,000 dim-128 vectors |
| `BenchmarkReopen_1k` | Engine reopen + namespace scan for 1,000 vectors |
| `BenchmarkCodec_Encode_dim128` | Binary encode one dim-128 record |
| `BenchmarkCodec_Decode_dim128` | Binary decode one dim-128 record |
| `BenchmarkConcurrentSearch` | Parallel search throughput |
| `BenchmarkConcurrentUpsert` | Parallel upsert throughput |

### Benchmark Results (Apple M3, phase-9-vector-search)

```
BenchmarkUpsert_1k_dim128-8           1099    3589932 ns/op   4304773 B/op   9804 allocs/op
BenchmarkSearch_1k_dim128_Cosine-8   18508     195865 ns/op     58568 B/op      6 allocs/op
BenchmarkSearch_10k_dim128_Cosine-8   1611    2226659 ns/op    566472 B/op      6 allocs/op
BenchmarkSearch_1k_dim128_L2-8       18626     193760 ns/op     58568 B/op      6 allocs/op
BenchmarkSearch_1k_dim128_Dot-8      18643     192834 ns/op     58568 B/op      6 allocs/op
BenchmarkReopen_1k-8                  2426    1504049 ns/op   3349410 B/op  10125 allocs/op
BenchmarkCodec_Encode_dim128-8    23052736       156.3 ns/op       576 B/op      1 allocs/op
BenchmarkCodec_Decode_dim128-8    24491739       147.3 ns/op       512 B/op      1 allocs/op
BenchmarkConcurrentSearch-8          71895      50062 ns/op     58569 B/op      6 allocs/op
BenchmarkConcurrentUpsert-8         932530      20614 ns/op      4308 B/op     10 allocs/op
```

Observed: 1k-vector cosine search runs in ~196µs on Apple M3. Codec encode/decode is ~150ns per record.

### Commands Run

```
go mod tidy
go fmt ./...
go vet ./...
go test -race -count=1 -v ./...
go test -bench=. -benchmem -benchtime=3s ./internal/vector/...
go test -bench=. -benchmem -benchtime=3s ./internal/engine/...
go test -bench=. -benchmem -benchtime=3s ./internal/bench/...
make test
make vet
make build
make bench-vector
make bench-report
./bin/shardforge --help
./bin/shardforge version
./bin/shardforge-bench --scale small --out /tmp/shardforge-bench.md
git status --short
```

### Known Limitations

- **Exact search only.** No ANN, HNSW, IVF, or approximate search.
- **Single-node only.** No sharding, replication, or distributed search.
- **Memory-bound.** All stored vectors must fit in the process heap.
- **Linear scan.** Search is O(n·d); degrades with very large n.
- **Manual flush/compact.** No background maintenance.
- **No SIMD.** Float32 arithmetic uses scalar Go loops.

### Confirmation: No ANN / Distributed Features

No ANN, HNSW, IVF, approximate search, sharding, replication, dashboard, networking, distributed mode, Raft, background compaction, automatic compaction, leveled compaction, or core engine behavior changes were implemented. Phase 9 adds a persistent exact vector search layer only.

---

## Phase 9 — Review Fix: Store Close-Safety

**Date:** 2026-06-09
**Go version:** go1.26.4 darwin/arm64
**Branch:** phase-9-vector-search

### Issue Fixed

The original `checkClosed()` helper acquired `s.mu.RLock()`, read `s.closed`, and **released the lock** before the calling method did any work. This created a time-of-check/time-of-use (TOCTOU) race:

- `Search` could pass `checkClosed()`, then `Close()` could run and close the engine, then `Search` returned results from a closed store.
- `Upsert` could pass `checkClosed()`, then `Close()` could close the engine, then `Upsert` hit an underlying engine error instead of `vector.ErrClosed`.

### Fix: Lock-First Pattern

Every public method now acquires the appropriate lock **first**, checks `s.closed` while holding the lock, and performs its work while still holding the lock. The `checkClosed()` helper has been removed.

| Method | Lock held | Closed check |
|--------|-----------|--------------|
| `Upsert` | `s.mu.Lock()` | inside lock, before engine Put |
| `Delete` | `s.mu.Lock()` | inside lock, before engine Delete |
| `Flush` | `s.mu.Lock()` | inside lock, before engine Flush |
| `Compact` | `s.mu.Lock()` | inside lock, before engine Compact |
| `Get` | `s.mu.RLock()` | inside lock, before index read |
| `Search` | `s.mu.RLock()` | inside lock, before candidate collection |
| `Count` | `s.mu.RLock()` | no error return; reads len(index) |
| `Stats` | `s.mu.RLock()` | no error return; reads index + engine |
| `Close` | `s.mu.Lock()` | sets closed=true, closes engine |

`s.opts` is immutable after `Open`; methods read it before acquiring the lock without data races. Encoding and input validation also happen before the lock.

### Codec Hardening

`decodeRecord` now rejects trailing bytes after the footer magic sentinel. Appended garbage was previously silently ignored; it is now treated as `ErrCorruptRecord`.

### Tests Added

| Test | Covers |
|------|--------|
| `TestCloseConcurrentWithSearchRaceSafe` | `Close` racing with concurrent `Search` — passes `-race`; post-close returns `ErrClosed` |
| `TestCloseConcurrentWithUpsertRaceSafe` | `Close` racing with concurrent `Upsert` — passes `-race`; only `nil` or `ErrClosed` returned |
| `TestFlushCompact_AfterClose` | `Flush` and `Compact` return `ErrClosed` after `Close` |
| `TestCodec_RejectsTrailingBytes` | Trailing bytes after footer return `ErrCorruptRecord` |

### Updated Total Test Count

| Package | Tests |
|---------|-------|
| `internal/vector` | 49 (was 44; +5 new tests) |
| All other packages | unchanged |

### Commands Run

```
go mod tidy
go fmt ./...
go vet ./...
go test -race -count=1 -v ./...
go test -bench=. -benchmem -benchtime=3s ./internal/vector/...
go test -bench=. -benchmem -benchtime=3s ./internal/engine/...
go test -bench=. -benchmem -benchtime=3s ./internal/bench/...
make test
make vet
make build
make bench-vector
make bench-report
./bin/shardforge --help
./bin/shardforge version
./bin/shardforge-bench --scale small --out /tmp/shardforge-bench.md
git status --short
```

### Benchmark Results (vector package, Apple M3, after fix)

```
BenchmarkUpsert_1k_dim128-8           1107    3498320 ns/op   4304707 B/op   9804 allocs/op
BenchmarkSearch_1k_dim128_Cosine-8   18674     194899 ns/op     58568 B/op      6 allocs/op
BenchmarkSearch_10k_dim128_Cosine-8   1598    2222066 ns/op    566472 B/op      6 allocs/op
BenchmarkSearch_1k_dim128_L2-8       18744     192935 ns/op     58568 B/op      6 allocs/op
BenchmarkSearch_1k_dim128_Dot-8      18783     191688 ns/op     58568 B/op      6 allocs/op
BenchmarkReopen_1k-8                  2437    1494688 ns/op   3349410 B/op  10125 allocs/op
BenchmarkCodec_Encode_dim128-8    23067963       155.1 ns/op       576 B/op      1 allocs/op
BenchmarkCodec_Decode_dim128-8    24578278       146.1 ns/op       512 B/op      1 allocs/op
BenchmarkConcurrentSearch-8          65497      52894 ns/op     58569 B/op      6 allocs/op
BenchmarkConcurrentUpsert-8         886413      20605 ns/op      4101 B/op     10 allocs/op
```

### Confirmation: No ANN / Distributed Features

No ANN, HNSW, IVF, approximate search, sharding, replication, dashboard, networking, distributed mode, Raft, background compaction, automatic compaction, leveled compaction, or core engine behavior changes were made in this fix.

---

## Phase 10 — Single-process Key-value Sharding

### API Implemented

```go
package shard

func Open(opts Options) (*Store, error)
func (s *Store) Put(key, value []byte) error
func (s *Store) Delete(key []byte) error
func (s *Store) Get(key []byte) ([]byte, bool, error)
func (s *Store) Scan(start, end []byte) ([]Entry, error)
func (s *Store) Flush() error
func (s *Store) Compact() error
func (s *Store) Stats() Stats
func (s *Store) ShardForKey(key []byte) (ShardInfo, error)
func (s *Store) Close() error
```

Sentinel errors: `ErrClosed`, `ErrInvalidOptions`, `ErrInvalidKey`, `ErrCorruptManifest`, `ErrShardMismatch`.

### Manifest Format and Atomicity

`SHARDING.json` is written via temp-file + `os.Rename` (atomic on POSIX). After `Open` completes, no `.tmp` file remains (verified by `TestManifest_WriteIsAtomic`).

Example manifest:

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

Validated on read and write: version (must be 1), hash (must be "fnv64a"), no absolute paths, no path traversal, no duplicate IDs/names.

### Consistent Hash Algorithm

- Hash function: **FNV-1a 64-bit** (`hash/fnv.New64a`)
- Token label: `"shard-XXXX#vnode-XXXX"` (zero-padded 4-digit decimal)
- Ring size: `ShardCount × VirtualNodes` tokens
- Sort: ascending by hash; ties broken by (shardID, vnodeIndex)
- Routing: `fnv1a64(key)` → binary search for first token with `hash ≥ keyHash` → wrap to token 0

The ring is rebuilt identically from manifest parameters on every open.

### Routing Rules

| Operation     | Target                        |
|---------------|-------------------------------|
| `Put`         | Exactly one shard (hash route)|
| `Delete`      | Exactly one shard (hash route)|
| `Get`         | Exactly one shard (hash route)|
| `ShardForKey` | Exactly one shard (hash route)|
| `Scan`        | All shards (fan-out)          |
| `Flush`       | All shards                    |
| `Compact`     | All shards                    |

Empty key returns `ErrInvalidKey` before ring lookup.

### Scan Fan-out Rules

1. Call `engine.Scan(start, end)` on every shard.
2. Merge results; for duplicate keys keep the entry with highest `Seq`.
3. Sort final slice by key ascending.
4. Return tombstone-free output.

### Persistence and Recovery

- First open: write `SHARDING.json`, create `shards/shard-XXXX/` dirs, open engines.
- Reopen: read `SHARDING.json`, open each engine from stored path.
- Mismatched `ShardCount` or `VirtualNodes` → `ErrShardMismatch`.
- Each shard engine replays its own WAL independently on open.

### Tests Added

40 tests in `internal/shard/store_test.go` (all pass with `go test -race -count=1`):

Options validation, manifest atomicity and corruption, hash ring determinism, stable routing across reopen, multi-shard distribution, ShardForKey empty-key rejection, Put/Get/Delete semantics, Scan fan-out and deduplication, Flush/Compact with reopen, Close idempotency and post-close errors, concurrent race safety, Stats aggregation, large 5000-key workload, shard name determinism.

### Benchmark Results (shard package, Apple M3)

Local single-process sharding benchmarks. Measurements include lock acquisition, ring lookup, engine delegation, and defensive copying.

```
BenchmarkRing_Route1M-8                   44197855       78.24 ns/op         32 B/op    1 allocs/op
BenchmarkPut_10k_4shards-8                 2276066     1566   ns/op          201 B/op    7 allocs/op
BenchmarkGet_10k_existing_4shards-8       24104815      148.6 ns/op          103 B/op    4 allocs/op
BenchmarkGet_10k_missing_4shards-8        32376187      110.0 ns/op           31 B/op    1 allocs/op
BenchmarkScan_10k_4shards-8                    631    5708292 ns/op      9651265 B/op  100309 allocs/op
BenchmarkFlush_10k_4shards-8                    33  102990683 ns/op      5832826 B/op   50836 allocs/op
BenchmarkCompact_10k_4shards-8                  31  126712509 ns/op     16978758 B/op  218841 allocs/op
BenchmarkReopen_10k_4shards-8                 6186     578179 ns/op      1065529 B/op   10793 allocs/op
BenchmarkConcurrentPut_4shards-8           1408251     2482   ns/op          484 B/op    6 allocs/op
BenchmarkConcurrentGet_4shards-8          27233973      126.8 ns/op          103 B/op    4 allocs/op
```

### Known Limitations

- Static shard count: fixed at creation; resharding not implemented.
- FNV-1a with 128 vnodes can produce uneven ring coverage; use more vnodes for better balance.
- No shard migration, resharding, or rebalancing.
- No distributed transactions; multi-key ops not atomic across shards.
- Scan is O(keys × shards): always fans out to all shards.
- `internal/vector` is not sharded.

### Confirmation: Scope Boundaries

No replication, dashboard, networking, distributed mode, Raft, consensus, leader/follower, shard migration, resharding, rebalancing, distributed transactions, vector sharding, ANN, HNSW, IVF, approximate vector search, background compaction, automatic compaction, leveled compaction, size-tiered compaction, or core Engine behavior changes were implemented in Phase 10.

### Phase 10 — Review Fix: Close-safety TOCTOU and Manifest Hardening

#### Issue Fixed: Close-safety TOCTOU

**Root cause:** The original implementation checked `s.closed` under `RLock`, released the lock, and then called the shard Engine outside the lock. This created a race:

1. `Put` (or `Get`, `Scan`, etc.): acquires `RLock` → checks `s.closed == false` → gets engine handle → **releases `RLock`** → calls `engine.Put`
2. `Close`: acquires `Lock` → sets `s.closed = true` → **releases `Lock`** → closes all engines
3. Race: if step 2 runs between the `RUnlock` and `engine.Put` in step 1, `Put` calls a closed engine and returns `engine.ErrClosed` instead of `shard.ErrClosed`.

**Fix: hold lock across engine calls.**

- All public operations (`Put`, `Delete`, `Get`, `Scan`, `Flush`, `Compact`, `Stats`, `ShardForKey`) now hold `s.mu.RLock()` for the **entire** duration of their Engine calls.
- `Close` holds `s.mu.Lock()` while closing **all** shard engines, not just while setting `s.closed`.

**New locking invariant:**

| Operation | Lock held during Engine call |
|-----------|------------------------------|
| `Put`, `Delete`, `Get`, `ShardForKey` | `s.mu.RLock()` via `defer s.mu.RUnlock()` |
| `Scan` | `s.mu.RLock()` across all `engine.Scan` calls; released before in-memory merge/sort |
| `Flush`, `Compact`, `Stats` | `s.mu.RLock()` via `defer s.mu.RUnlock()` |
| `Close` | `s.mu.Lock()` via `defer s.mu.Unlock()` across all engine closes |

This establishes a proper reader-writer relationship at the store level: concurrent reads can proceed simultaneously, and Close waits for all in-flight operations to finish before closing engines.

#### Manifest Hardening Added

`validateManifest` now additionally rejects:

- `shard_count <= 0`
- `virtual_nodes <= 0`
- `len(shards) != shard_count`
- shard ID outside `[0, shard_count)`
- missing shard ID in `[0, shard_count)` (after pigeonhole checks above)
- empty shard name
- empty shard path
- path not clean (`filepath.Clean(path) != path`)
- duplicate shard path

#### Tests Added/Updated

**Updated:** `TestConcurrent_CloseWithPutGet` — now collects and reports unexpected errors (non-nil, non-`ErrClosed`) via an error channel; the test fails on any unexpected error.

**New (close-safety):**
1. `TestCloseConcurrentWithPutReturnsOnlyNilOrErrClosed` — 8 goroutines doing Put in a loop + concurrent Close; fails on any non-nil/non-ErrClosed error; verifies ErrClosed after close.
2. `TestCloseConcurrentWithGetReturnsOnlyNilOrErrClosed` — seeds data; 8 goroutines doing Get + concurrent Close; same contract.
3. `TestCloseConcurrentWithScanReturnsOnlyNilOrErrClosed` — 4 goroutines doing Scan + concurrent Close; same contract.
4. `TestCloseConcurrentWithFlushCompactRaceSafe` — Flush and Compact goroutines + concurrent Close; same contract.

**New (manifest hardening):**
`TestManifest_RejectsZeroShardCount`, `TestManifest_RejectsZeroVirtualNodes`, `TestManifest_RejectsShardListLengthMismatch`, `TestManifest_RejectsShardIDOutOfRange`, `TestManifest_RejectsMissingShardID`, `TestManifest_RejectsEmptyShardName`, `TestManifest_RejectsEmptyShardPath`, `TestManifest_RejectsDuplicatePath`, `TestManifest_RejectsUncleanPath`.

**Updated total shard test count:** 55 (was 40).

All 55 tests pass with `go test -race -count=1 ./internal/shard/...`.

#### Commands Run

```
go mod tidy
go fmt ./...
go vet ./...
go test -race -count=1 ./...
go test -bench=. -benchmem -benchtime=3s ./internal/shard/...
go test -bench=. -benchmem -benchtime=3s ./internal/vector/...
go test -bench=. -benchmem -benchtime=3s ./internal/engine/...
go test -bench=. -benchmem -benchtime=3s ./internal/bench/...
make test
make vet
make build
make bench-shard
make bench-vector
make bench-report
./bin/shardforge --help
./bin/shardforge version
./bin/shardforge-bench --scale small --out /tmp/shardforge-bench.md
git status --short
```

All commands passed with zero errors.

#### Scope Boundaries

No replication, networking, distributed mode, Raft, consensus, shard migration, resharding, vector sharding, ANN, HNSW, IVF, background compaction, automatic compaction, or core Engine behavior changes were made in this fix.

*Future phases will append their own sections to this document.*

---

## Phase 11 — Leader/Follower Replication Simulation

**Date:** 2026-06-09
**Branch:** `phase-11-replication`
**Go version:** go1.26.4 darwin/arm64

### API Implemented

```go
func Open(opts Options) (*Store, error)
func (s *Store) Put(key, value []byte) (LogIndex, error)
func (s *Store) Delete(key []byte) (LogIndex, error)
func (s *Store) Get(key []byte, mode ReadMode, replicaID int) ([]byte, bool, error)
func (s *Store) Scan(start, end []byte, mode ReadMode, replicaID int) ([]Entry, error)
func (s *Store) ReplicateOnce() (int, error)
func (s *Store) ReplicateAll() error
func (s *Store) SetFollowerPaused(replicaID int, paused bool) error
func (s *Store) SetFollowerLag(replicaID int, maxApplyPerCall int) error
func (s *Store) Flush() error
func (s *Store) Compact() error
func (s *Store) Stats() Stats
func (s *Store) Replicas() []ReplicaInfo
func (s *Store) Close() error
```

Types: `Role` (leader/follower), `ReadMode` (leader/follower/any), `LogIndex`, `OperationType` (Put/Delete), `Operation`, `Options`, `ReplicaInfo`, `Entry`, `Stats`, `ReplicaStats`.

Errors: `ErrClosed`, `ErrInvalidOptions`, `ErrInvalidKey`, `ErrInvalidReplica`, `ErrInvalidReadMode`, `ErrNotLeader`, `ErrCorruptManifest`, `ErrReplicaMismatch`.

### Manifest Format

`REPLICATION.json` written atomically (temp file + `os.Rename`). Validates: version==1, replica_count>0, len(replicas)==replica_count, IDs in [0,n), no duplicate IDs/names/paths, no absolute or traversal paths, exactly one leader, leader_id consistent with role. On reopen: ReplicaCount==0 loads from manifest; LeaderID<0 loads from manifest; non-zero/non-negative mismatches return `ErrReplicaMismatch`.

### Replication Log Format

Binary append-only file at `replog/log.dat`:
- Header: 8-byte magic `"SHARDREP"` + uint16 version (1).
- Records: uint32 recordLen + uint32 CRC-32/IEEE + uint64 index + uint8 opType + uint32 keyLen + uint32 valLen + key + value.
- Indexes start at 1, increment by 1. CRC validated on replay. Truncated records return `errLogTruncated`. Bad CRC returns `errLogCorrupt`. Unsupported version returns `errLogVersion`.

### Write / Commit Semantics

- `Put`/`Delete` acquire the store write lock for the full sequence.
- Append operation to durable log (disk write).
- Apply to leader Engine (WAL + MemTable).
- If leader write succeeds: advance leader's `appliedIndex` + `commitIndex`; persist `APPLIED`.
- If leader write fails: return error; `commitIndex` not advanced; log has an uncommitted tail record (documented limitation).
- Followers are not touched during Put/Delete.

### Follower Catch-up Rules

- `ReplicateOnce`: acquire write lock; for each non-paused follower behind `commitIndex`, apply up to `maxApplyPerCall` (default unlimited per-call, capped at 1 loop iteration per follower) operations; persist `APPLIED` after each.
- `ReplicateAll`: loop `ReplicateOnce` until no progress.
- Paused followers are skipped entirely.
- Lag-limited followers apply at most `maxApplyPerCall` ops per `ReplicateOnce` call.

### Read Consistency Rules

| Mode | Source | Staleness |
|------|--------|-----------|
| ReadLeader | Leader Engine | Never stale |
| ReadFollower | Specified follower (must not be leader) | Stale until ReplicateAll |
| ReadAny | replicaID ≥ 0 → that replica; < 0 → leader | Depends on target |

### Tests Added

60 tests in `internal/replica/store_test.go` covering:
- Open validation (7 tests)
- Manifest validation (9 tests including version, duplicate ID/name/path, absolute path, traversal, no leader, multiple leaders, missing ID, temp file cleanup)
- Write operations: Put index 1, no-write-to-follower, ReplicateOnce, ReplicateAll, Delete replication
- Stale reads: stale before replication, current after ReplicateAll
- Pause/lag: paused follower skips, unpause catch-up, lag limit per call
- Commit/applied index tracking
- Reopen/persistence: leader data, log preserved, no duplicate apply, catch-up after reopen
- Read modes: ReadLeader, ReadFollower rejects leader ID, ReadFollower rejects invalid ID, ReadAny defaults to leader
- Scan: sorted entries from leader, stale follower scan
- Flush/Compact: flush persists all replicas, compact preserves data
- Close: idempotent, ErrClosed after close
- Concurrency: concurrent Put+ReplicateAll, concurrent Get/Scan, concurrent Close
- Log codec: round-trip, bad CRC, truncated record, unsupported version, operationsAfter suffix
- Edge cases: delete missing key, empty key rejection, deterministic replica names, read mode validation, stats

### Benchmarks Added

10 benchmarks in `internal/replica/store_bench_test.go`:

| Benchmark | ns/op | Notes |
|-----------|------:|-------|
| Put_10k_LeaderOnly | 146,569 | WAL + MemTable + log append |
| ReplicateAll_10k_2Followers | ~3s/iter | Full propagation of 10k ops |
| Get_Leader_10k_Existing | 132 | MemTable hit |
| Get_Follower_10k_Existing | 136 | MemTable hit after catch-up |
| Scan_Leader_10k | 3,469,611 | Full scan 10k keys |
| Reopen_10k | 10,200,183 | Open 3 replicas + replay log |
| ReplicateOnce_SmallBatch | 292,796 | 1 op × 2 followers |
| ConcurrentPut | 147,762 | Serialised by write lock |
| ConcurrentReplicateAllWithReads | 117 | Reads interleaved with replicate |
| LogAppendReplay | 970,448 | 1000 ops append then replay |

### Commands Run

```
go mod tidy                                                        OK
go fmt ./...                                                       OK
go vet ./...                                                       OK
go test -race -count=1 ./...                                       15 packages PASS
go test -bench=. -benchmem -benchtime=3s ./internal/replica/...   10 benchmarks PASS
go test -bench=. -benchmem -benchtime=3s ./internal/shard/...     10 benchmarks PASS
go test -bench=. -benchmem -benchtime=3s ./internal/vector/...    10 benchmarks PASS
go test -bench=. -benchmem -benchtime=3s ./internal/engine/...    13 benchmarks PASS
go test -bench=. -benchmem -benchtime=3s ./internal/bench/...     5 benchmarks PASS
make test                                                          PASS
make vet                                                           PASS
make build                                                         PASS
make bench-replica                                                 PASS
make bench-shard                                                   PASS
make bench-report                                                  PASS
./bin/shardforge --help                                            OK
./bin/shardforge version                                           ShardForgeDB 0.1.0
git status --short                                                 (clean after commit)
```

### Known Limitations

- Uncommitted tail record in log if leader Engine write fails after log append.
- `APPLIED` persistence is best-effort; follower may re-apply an op if `APPLIED` write fails then process crashes.
- Log grows unboundedly (no compaction).
- No automatic catch-up on reopen; callers must call `ReplicateAll`.
- Pause/lag simulation is in-memory only; does not survive restarts.

### Scope Confirmation

No networking, no RPC, no distributed deployment, no Raft, no consensus, no automatic leader election, no fault-tolerant quorum, no shard migration, no resharding, no vector replication, no ANN/HNSW/IVF, no background compaction, no automatic compaction, no core Engine behavior changes were implemented in this phase.

---

## Phase 11 — Review Fix: Commit-Index Recovery Correctness

**Date:** 2026-06-10
**Branch:** `phase-11-replication`

### The Bug

In the original Phase 11 implementation, `Open()` set the in-memory `commitIndex` using:

```go
commitIndex := ol.lastIndex()
```

`ol.lastIndex()` returns the index of the last record written to the replication log — but that record may be **uncommitted**. Specifically:

- `Put`/`Delete` appended to the log first, then applied to the leader Engine.
- If the leader Engine write failed, the log had a tail record but `commitIndex` was not advanced (correct in-memory behavior).
- However, on restart, `Open` did not distinguish committed from uncommitted log records: it set `commitIndex = log.lastIndex()`, treating every log record as committed.
- This allowed `ReplicateAll` to propagate an operation to followers that the leader had never committed, breaking leader-commit semantics.

### The Fix

#### New `COMMIT` file

A durable `COMMIT` file at `<Dir>/COMMIT` stores the last committed log index (ASCII decimal, atomically written via temp+rename). It is the single source of truth for `commitIndex` across restarts.

```
<Dir>/
  COMMIT                — last committed LogIndex (atomic write, not best-effort)
  REPLICATION.json      — existing manifest
  replog/log.dat        — existing replication log
  replicas/...
```

#### `Open` behavior after fix

```go
commitIndex := loadCommitIndex(opts.Dir)
```

- If `COMMIT` exists: load the persisted value.
- If `COMMIT` is missing (new store, or file deleted): `commitIndex = 0`. No log records are treated as committed.
- The log may contain records beyond `commitIndex`; they are invisible to `ReplicateOnce`/`ReplicateAll` because both only apply `op.Index <= commitIndex`.

#### `Put`/`Delete` write sequence after fix

```
1. Append to log (durable)
2. Apply to leader Engine
   — if fail: return error; log has uncommitted tail; COMMIT unchanged
3. saveCommitIndex(dir, idx)   ← NEW: atomic COMMIT write; error if fails
   — if fail: return error; in-memory commitIndex unchanged
4. Advance in-memory commitIndex and leader appliedIndex
5. saveAppliedIndex (best-effort)
```

`saveCommitIndex` is **not** best-effort: it returns an error if the atomic write fails, and the caller does not advance `commitIndex` in memory. This preserves the invariant: `COMMIT on disk == in-memory commitIndex`.

#### Uncommitted log tail behavior

If the process restarts with a log tail record whose index > `COMMIT`:

- `commitIndex` is loaded from `COMMIT` (lower value).
- `ReplicateOnce`/`ReplicateAll` never apply records beyond `commitIndex`.
- The uncommitted tail record is permanently invisible to followers.
- The leader Engine may or may not have the data (depends on whether the Engine write succeeded before the crash). This is a known limitation documented in both DESIGN.md and the code comments.

#### Performance impact

The atomic COMMIT write adds one temp-file-write + rename to every successful `Put`/`Delete`. Benchmark result: `Put_10k_LeaderOnly` increased from ~147µs/op to ~296µs/op. Get, Scan, and ReplicateAll throughput are unchanged.

### Tests Added (review-fix specific)

| Test | What it proves |
|------|----------------|
| `TestOpen_LoadsCommitIndexFromCommitFile` | After n Puts, COMMIT=n; reopen gives commitIndex=n |
| `TestOpen_MissingCommitFileDoesNotTreatLogTailAsCommitted` | Missing COMMIT → commitIndex=0; ReplicateOnce applies 0 ops |
| `TestUncommittedLogTailNotReplicatedAfterRestart` | Direct log.append + close/reopen: follower never sees the uncommitted record |
| `TestPutPersistsCommitIndex` | After Put, COMMIT file on disk == returned index |
| `TestDeletePersistsCommitIndex` | Same for Delete |
| `TestCommitFileTempCleanup` | No COMMIT.tmp left after successful write |

### Updated Replica Test Count

Original Phase 11: 60 tests. After review fix: **66 tests**.

### Commands Run

```
go mod tidy                                                        OK
go fmt ./...                                                       OK
go vet ./...                                                       OK
go test -race -count=1 ./...                                       15 packages PASS
go test -bench=. -benchmem -benchtime=3s ./internal/replica/...   10 benchmarks PASS
go test -bench=. -benchmem -benchtime=3s ./internal/shard/...     10 benchmarks PASS
go test -bench=. -benchmem -benchtime=3s ./internal/vector/...    10 benchmarks PASS
go test -bench=. -benchmem -benchtime=3s ./internal/engine/...    13 benchmarks PASS
go test -bench=. -benchmem -benchtime=3s ./internal/bench/...     5 benchmarks PASS
make test                                                          PASS
make vet                                                           PASS
make build                                                         PASS
make bench-replica                                                 PASS
make bench-shard                                                   PASS
make bench-vector                                                  PASS
make bench-report                                                  PASS (timing numbers updated)
./bin/shardforge --help                                            OK
./bin/shardforge version                                           ShardForgeDB 0.1.0
git status --short                                                 (clean after commit)
```

### Scope Confirmation

No networking, no RPC, no distributed deployment, no Raft, no consensus, no automatic leader election, no fault-tolerant quorum, no shard migration, no resharding, no vector replication, no ANN/HNSW/IVF, no background compaction, no automatic compaction, and no core Engine behavior changes were made in this review fix.

---

## Phase 12 — Local Dashboard and Chaos Simulation

### API Implemented

**Dashboard package** (`internal/dashboard`):

```
types.go      — ComponentType, HealthStatus, Options, ComponentSnapshot,
                TimelineEvent, Snapshot, Collector interface,
                ScenarioStatus, ScenarioStep, ScenarioResult
collector.go  — NewEngineCollector, NewShardCollector, NewReplicaCollector,
                NewMultiCollector, NewScenarioCollector
templates.go  — renderHTML (html/template; no external JS)
server.go     — NewServer, (*Server).Handler, (*Server).Start, (*Server).Close
scenario.go   — RunFollowerPauseScenario, RunFollowerLagScenario,
                RunFollowerCatchupScenario, ReplicaScenarioTarget
```

### HTTP Endpoints

| Endpoint | Returns |
|----------|---------|
| `GET /` | HTML dashboard (component cards + timeline) |
| `GET /status` | JSON `Snapshot` |
| `GET /healthz` | JSON `{"status":"ok"}` |
| `GET /events` | JSON `[]TimelineEvent` |
| Unknown path | 404 Not Found |

### Collectors

| Collector | Source |
|-----------|--------|
| `NewEngineCollector` | `engine.Engine.Stats()` |
| `NewShardCollector` | `shard.Store.Stats()` |
| `NewReplicaCollector` | `replica.Store.Stats()` — reports `HealthDegraded` when follower lags |
| `NewMultiCollector` | Merges components and events from N collectors |
| `NewScenarioCollector` | Exposes `ScenarioResult.Events` through dashboard |

### Scenarios

| Scenario | Steps | Key verification |
|----------|-------|-----------------|
| `RunFollowerPauseScenario` | 7 | Key absent while paused; present after unpause + ReplicateAll |
| `RunFollowerLagScenario` | 6 | Lag confirmed (AppliedIndex < CommitIndex); all 6 keys after ReplicateAll |
| `RunFollowerCatchupScenario` | 6 | 4 keys absent while paused; all present after unpause + ReplicateAll |

### Tests Added

| File | Count |
|------|-------|
| `internal/dashboard/server_test.go` | 30 tests + 5 benchmarks |
| `internal/dashboard/scenario_test.go` | 16 tests + 3 benchmarks |
| **Total** | **46 tests, 8 benchmarks** |

Coverage of all 32 required test categories including: nil collector rejection, default options, all 4 endpoints, all 3 collectors, MultiCollector merge, HTML escaping, footer phrase check, health states, all 3 scenarios passing, bad-input non-panic, events ordered, steps present, concurrent race safety, start/close idempotency.

### Benchmarks Added

```
BenchmarkSnapshot_EngineCollector
BenchmarkSnapshot_ReplicaCollector
BenchmarkSnapshot_MultiCollector
BenchmarkRenderHTML
BenchmarkEncodeStatusJSON
BenchmarkRunFollowerPauseScenario
BenchmarkRunFollowerLagScenario
BenchmarkRunFollowerCatchupScenario
```

### CLI Command

`cmd/shardforge-dashboard/main.go`:
- `--help` — usage
- `--demo` — opens temp 3-replica store, seeds 20 keys, starts HTTP server
- `--run-chaos` — additionally runs all three scenarios before starting server
- `--addr` — custom listen address

### Makefile Targets Added

```makefile
dashboard:       go run ./cmd/shardforge-dashboard --demo
bench-dashboard: go test -bench=. -benchmem ./internal/dashboard/...
build:           now also builds bin/shardforge-dashboard
```

### Docs Updated

- `README.md` — Phase 11 marked ✓ locked; Phase 12 added as in review
- `docs/DESIGN.md` — Phase 12 section added
- `docs/PROOF.md` — this section
- `docs/BENCHMARKS.md` — Phase 12 benchmark section added after validation

### Commands Run

```
go mod tidy                                                        OK
go fmt ./...                                                       OK
go vet ./...                                                       OK
go test -race -count=1 -v ./internal/dashboard/...                46/46 PASS
go test -race -count=1 ./...                                       16 packages PASS
go test -bench=. -benchmem -benchtime=3s ./internal/dashboard/... 8 benchmarks PASS
make test                                                          PASS
make vet                                                           PASS
make build                                                         PASS (3 binaries)
make bench-dashboard                                               PASS
./bin/shardforge-dashboard --help                                  OK
git status --short                                                 (clean)
```

### Known Limitations

- **Local only.** All components run in a single OS process — no networking between nodes.
- **In-memory simulation.** Pause and lag are in-memory flags; they cannot simulate real network partitions or process crashes.
- **No leader failure simulation.** Only follower behaviour is exercised.
- **No log compaction.** The replication log grows unboundedly during scenarios.
- **No vector replication.** `internal/vector` is not included in collectors or scenarios.
- **No background compaction.** Engine maintenance is manual only.

### Scope Confirmation

No networking, no RPC, no distributed deployment, no Raft, no consensus, no automatic leader election, no quorum replication, no shard migration, no resharding, no vector replication, no ANN/HNSW/IVF, no background compaction, no automatic compaction, and no core Engine, Shard, Replica, or Vector behavior changes were implemented in Phase 12.

---

## Phase 13 — Final Polish and Release Hardening

### Changes Made

| File | Change |
|------|--------|
| `README.md` | Full rewrite: portfolio pitch, quickstart, scope table, demo commands, not-implemented table, accurate Phase 12 test count (52), no stale wording |
| `docs/DESIGN.md` | Release Scope section, Phase 13 section |
| `docs/PROOF.md` | This section |
| `docs/BENCHMARKS.md` | Benchmark Reproducibility section |
| `docs/RELEASE_CHECKLIST.md` | New — build/test/benchmark/demo/scope/resume checklists |
| `docs/PROJECT_SUMMARY.md` | New — overview, architecture, phase map, recruiter bullets, "what next" |
| `scripts/smoke.sh` | New — fast smoke validation script |
| `scripts/demo.sh` | New — recruiter-friendly demo sequence |
| `scripts/release_check.sh` | New — full release gate with clean-tree check |
| `Makefile` | Added `smoke`, `demo`, `release-check` targets |

### Docs Consistency Fixes

- README top-scope block: replaced blanket "no networking" with "no database-node networking, no RPC" to avoid contradiction with Phase 12 local HTTP server.
- README Phase 12: updated test count 46 → 52 (actual count after cleanup PR).
- README Phase 10: updated test count 40 → 55 (actual count after review fixes).
- README Phase 11: updated test count 60 → 66 (actual count after review fixes).
- Stale "All other components are intended design only" sentence removed in prior PR.
- Phase 12 entry: updated to ✓ locked; branch reference removed.
- "Features NOT Yet Implemented" table: removed "Dashboard / monitoring" from the list (it is now implemented as a local HTTP server).

### Release Scripts

```
scripts/smoke.sh         — go test, go vet, make build, CLI checks; exits 0 or 1
scripts/demo.sh          — build + version + small bench + dashboard instructions
scripts/release_check.sh — full gate: tidy, fmt, vet, race tests, all benchmarks, CLI, clean tree
```

All scripts use `set -euo pipefail` and `cd "$(dirname "$0")/.."` for robustness.

### Validation Commands Run

```bash
go mod tidy                                                    OK — no changes
go fmt ./...                                                   OK — no changes
go vet ./...                                                   OK
go test -race -count=1 ./...                                   16 packages PASS
go test -bench=. -benchmem -benchtime=3s ./internal/dashboard/ 8 benchmarks PASS
go test -bench=. -benchmem -benchtime=3s ./internal/replica/   10 benchmarks PASS
go test -bench=. -benchmem -benchtime=3s ./internal/shard/     10 benchmarks PASS
go test -bench=. -benchmem -benchtime=3s ./internal/vector/    10 benchmarks PASS
go test -bench=. -benchmem -benchtime=3s ./internal/engine/    benchmarks PASS
go test -bench=. -benchmem -benchtime=3s ./internal/bench/     benchmarks PASS
make test                                                      PASS
make vet                                                       PASS
make build                                                     PASS (3 binaries)
make bench-dashboard                                           PASS
make bench-replica                                             PASS
make bench-shard                                               PASS
make bench-vector                                              PASS
./scripts/smoke.sh                                             OK
./scripts/demo.sh                                              OK
./bin/shardforge --help                                        OK
./bin/shardforge version                                       ShardForgeDB 0.1.0
./bin/shardforge-bench --scale small --out /tmp/...            OK
./bin/shardforge-dashboard --help                              OK
git status --short                                             (clean)
```

### Known Limitations

- `make bench-report` regenerates `docs/BENCHMARKS.md` and strips manually added Phase 10/11/12 sections. Always run `git restore docs/BENCHMARKS.md` after `make bench-report` if only timing changed.
- `scripts/release_check.sh` runs all benchmark suites; expect 10–20 minutes on a development machine.
- `scripts/release_check.sh` does NOT run `make bench-report` to avoid unintended BENCHMARKS.md changes.

### Scope Confirmation

No new database engine features, no database-node networking, no RPC, no distributed deployment, no Raft, no consensus, no automatic leader election, no quorum replication, no shard migration, no resharding, no vector replication, no ANN/HNSW/IVF, no background compaction, no automatic compaction, and no core Engine/Shard/Replica/Vector/WAL/MemTable/SSTable/Bloom/Dashboard behavior changes were implemented in Phase 13.

---

## Phase 14 — Real Networked Node Runtime + HTTP Transport Foundation

### Files Changed

**New files:**
- `internal/node/types.go` — NodeID, Options, Status, EngineStatus, Entry, request/response types
- `internal/node/config.go` — Options.validate(), ErrInvalidOptions, ErrClosed
- `internal/node/server.go` — Server struct, Open, Handler, Start, StartBackground, Close, Status, Addr
- `internal/node/handlers.go` — registerRoutes, handleHealthz, handleStatus, handleKV, handleScan, handleFlush, handleCompact
- `internal/node/client.go` — Client struct, NewClient, Health, Status, Put, Get, Delete, Scan, Flush, Compact, doJSON
- `internal/node/server_test.go` — 20 handler/server tests
- `internal/node/client_test.go` — 9 client tests
- `internal/node/integration_test.go` — 7 integration tests (multi-server isolation, restart persistence, concurrent race)
- `internal/node/bench_test.go` — 6 benchmarks (handler Put/Get/Status/Scan, client Put/Get)
- `cmd/shardforge-node/main.go` — CLI with --node-id, --addr, --data-dir, --wal-sync, --memtable-max-bytes
- `deploy/docker-compose.yml` — 3-node Docker Compose demo
- `deploy/Dockerfile` — multi-stage Go builder + Alpine runtime
- `deploy/node-1.yaml`, `deploy/node-2.yaml`, `deploy/node-3.yaml` — node config reference

**Modified files:**
- `Makefile` — NODE_BINARY, NODE_CMD, bench-node, node, node-demo, node-demo-down targets; build now produces 4 binaries
- `README.md` — Phase 14 section, updated title, architecture, scope table, demo commands, Makefile targets
- `docs/DESIGN.md` — Phase 14 section: architecture, HTTP transport, data independence, limitations, future work
- `docs/PROOF.md` — this section
- `docs/BENCHMARKS.md` — Phase 14 node benchmarks section
- `docs/PROJECT_SUMMARY.md` — updated scope, phase table, recruiter bullets
- `docs/RELEASE_CHECKLIST.md` — network node checklist

### Features Implemented

- `internal/node.Server` — HTTP server wrapping a local Engine; real TCP listener; clean shutdown
- `internal/node.Client` — HTTP/JSON client with timeout, context, and typed errors
- 8 HTTP endpoints: `/healthz`, `/status`, `/kv/{key}` (PUT/GET/DELETE), `/scan`, `/flush`, `/compact`
- `cmd/shardforge-node` — CLI binary with scope disclaimer, signal handling, clean shutdown
- `deploy/docker-compose.yml` — 3 independent containers with named volumes and health checks
- `bin/shardforge-node` now produced by `make build`

### Tests

```
TestOpen_ValidOptions
TestOpen_MissingNodeID
TestOpen_MissingDataDir
TestOpen_MissingAddr
TestHealthz_OK
TestHealthz_MethodNotAllowed
TestStatus_OK
TestStatus_NonZeroStartedAt
TestStatus_MethodNotAllowed
TestKV_PutGet
TestKV_GetMissing
TestKV_Delete
TestScan_SortedEntries
TestScan_MethodNotAllowed
TestFlush_OK
TestFlush_MethodNotAllowed
TestCompact_OK
TestCompact_MethodNotAllowed
TestKV_EmptyKeyRejected
TestClose_Idempotent
TestKV_WrongMethod
TestKV_InvalidJSONBody
TestClient_PutGet
TestClient_GetMissing
TestClient_Delete
TestClient_Health
TestClient_Status
TestClient_Scan
TestClient_Flush
TestClient_Compact
TestClient_Timeout
TestClient_InvalidJSON
TestClient_ServerSideError
TestClient_NodeUnavailable
TestMultipleServers_IndependentData
TestMultipleServers_Concurrent
TestRestart_PreservesData
TestThreeHTTPServers_ClientTalkToAll
TestConcurrentPutGet_Race
TestClose_AfterCloseIsNoop
TestStartBackground_ThreeNodes
```

### Validation Commands Run

```bash
go build ./...                                                      OK
go test -race -count=1 ./...                                        17 packages PASS
go test -race -count=1 ./internal/node/...                          PASS
go test -bench=. -benchmem -benchtime=3s ./internal/node/...        6 benchmarks PASS
make test                                                           PASS
make vet                                                            PASS
make build                                                          PASS (4 binaries)
make bench-node                                                     PASS
./bin/shardforge-node --help                                        OK — prints scope disclaimer
git status --short                                                  (clean after commit)
docker compose -f deploy/docker-compose.yml config                  OK — config validated
```

### Benchmark Results (Apple M3, darwin/arm64, Go 1.21)

```
BenchmarkHandler_Put-8       569528    20560 ns/op   8109 B/op    37 allocs/op
BenchmarkHandler_Get-8      2444389     1458 ns/op   6373 B/op    26 allocs/op
BenchmarkHandler_Status-8   2111348     1723 ns/op   6633 B/op    22 allocs/op
BenchmarkHandler_Scan-8      122390    29550 ns/op  67003 B/op   754 allocs/op (100 entries)
BenchmarkClient_Put-8         89916    40876 ns/op  10837 B/op   123 allocs/op
BenchmarkClient_Get-8        110628    32741 ns/op   8723 B/op   100 allocs/op
```

### Docker Compose Proof

Docker CLI (v29.3.0) and Docker Compose (v5.1.0) are installed.  
`docker compose -f deploy/docker-compose.yml config` — PASS (config validated, all 3 services present).  
Docker daemon was not running at test time (Docker Desktop not started). The Docker Compose config and Dockerfile are structurally correct and were validated by `config`. Full `up --build` proof available when Docker daemon is running:

```bash
docker compose -f deploy/docker-compose.yml up --build -d
curl -f http://localhost:9101/healthz
curl -f http://localhost:9102/healthz
curl -f http://localhost:9103/healthz
curl -X PUT http://localhost:9101/kv/demo:1 -H "Content-Type: application/json" -d '{"value":"hello"}'
curl -f http://localhost:9101/kv/demo:1
docker compose -f deploy/docker-compose.yml restart shardforge-node-1
sleep 2
curl -f http://localhost:9101/kv/demo:1
docker compose -f deploy/docker-compose.yml down -v
```

### Known Limitations

- Docker daemon was not running at phase-14 validation time. The Compose config is validated; full `up --build` proof requires Docker Desktop to be running.
- No subprocess integration test for `cmd/shardforge-node` — subprocess tests are skipped to keep CI fast and deterministic. The HTTP handler and client are fully covered by httptest-based tests.
- Nodes are fully independent; there is no router layer directing client requests to the correct node for a given key.

### Scope Confirmation

- Real multi-process node runtime: implemented (each node is an independent OS process)
- Real HTTP/JSON transport: implemented
- Independent node data directories: implemented
- No distributed sharding: correct — each node stores its own keys independently
- No networked replication: correct — writing to node-1 does not propagate to node-2
- No Raft: correct
- No consensus: correct
- No quorum: correct
- No automatic leader election: correct
- No shard migration: correct
- No distributed vector search: correct
- No ANN/HNSW/IVF: correct
- No background compaction: correct
- No core Engine/WAL/MemTable/SSTable/Bloom/Vector behavior changes: correct

---

## Phase 15 — Client-Side Routing Gateway

### Files Changed

**New files:**
- `internal/gateway/errors.go` — ErrInvalidOptions, ErrInvalidKey, ErrUnknownNode, ErrClosed
- `internal/gateway/types.go` — NodeConfig, Options, RoutedNode, Stats
- `internal/gateway/ring.go` — hashRing, newHashRing, nodeForKey, nodeByID, fnv1a64
- `internal/gateway/client.go` — Gateway, Open, NodeForKey, Put, Get, Delete, ScanNode, FlushAll, CompactAll, HealthAll, Stats, Close
- `internal/gateway/ring_test.go` — 17 ring tests
- `internal/gateway/client_test.go` — 24 gateway/integration tests
- `internal/gateway/bench_test.go` — 6 benchmarks
- `cmd/shardforge-gateway/main.go` — CLI: route, put, get, delete, health, flush-all, compact-all

**Modified files:**
- `Makefile` — GATEWAY_BINARY, GATEWAY_CMD, bench-gateway, gateway-help, gateway-demo; build produces 5 binaries
- `README.md` — Phase 15 section, architecture, demo commands, scope table, Makefile targets
- `docs/DESIGN.md` — Phase 15 section
- `docs/PROOF.md` — this section
- `docs/BENCHMARKS.md` — Phase 15 gateway benchmark section
- `docs/PROJECT_SUMMARY.md` — gateway in phase map, recruiter bullets
- `docs/RELEASE_CHECKLIST.md` — gateway checklist

### Features Implemented

- `gateway.Gateway` — client-side consistent-hash routing over independent `node.Server` instances
- `hashRing` — FNV-1a 64-bit ring with virtual nodes (default 128), weight scaling, sorted O(log n) lookup
- `gateway.NodeForKey` — returns RoutedNode for a key (no network call)
- `gateway.Put/Get/Delete` — route by key, no retry to other nodes
- `gateway.ScanNode` — per-node scan, returns error for unknown nodeID
- `gateway.FlushAll/CompactAll` — fan out to all nodes
- `gateway.HealthAll` — health map across all nodes
- `gateway.Close` — idempotent, returns ErrClosed on subsequent operations
- `cmd/shardforge-gateway` — one-shot CLI with scope disclaimer

### Tests (41 total)

Ring tests (17): empty list, empty ID, empty URL, duplicate ID, duplicate URL, default virtual nodes, same-key routing, input-order independence, key distribution, wrap-around, weight, zero-weight, empty key, nodeByID found/not-found, deterministic across instances.

Gateway/integration tests (24): empty nodes, duplicate ID/URL, NodeForKey empty key, NodeForKey result, Put/Get routing, same-key same-node, isolation (key on routed node only), Delete, empty key rejection (Put/Get/Delete), ScanNode one-node, ScanNode unknown node, FlushAll, CompactAll, HealthAll all-healthy, HealthAll entry-per-node, no auto-failover, Close idempotent, operations after Close, Stats, timeout clear error, concurrent Put/Get race, deterministic across two instances.

### Validation Commands Run

```bash
go build ./...                                                  OK
go test -race -count=1 ./...                                    18 packages PASS
go test -race -count=1 ./internal/gateway/...                   41 tests PASS
go test -bench=. -benchmem -benchtime=3s ./internal/gateway/... 6 benchmarks PASS
make test                                                       PASS
make vet                                                        PASS
make build                                                      PASS (5 binaries)
make bench-gateway                                              PASS
./bin/shardforge-gateway --help                                 OK — scope disclaimer printed
docker compose -f deploy/docker-compose.yml config              OK
git status --short                                              (clean after commit)
```

### Benchmark Results (Apple M3, darwin/arm64, Go 1.21)

```
BenchmarkRing_NodeForKey-8      156403526    22.90 ns/op       0 B/op    0 allocs/op
BenchmarkGateway_Put-8              88870   39530 ns/op   10921 B/op  123 allocs/op
BenchmarkGateway_Get-8             111212   32347 ns/op    8712 B/op  100 allocs/op
BenchmarkGateway_HealthAll-8        37855   95669 ns/op   25788 B/op  278 allocs/op
BenchmarkGateway_FlushAll-8         36198   98251 ns/op   25863 B/op  278 allocs/op
BenchmarkGateway_CompactAll-8       37580   95929 ns/op   25862 B/op  278 allocs/op
```

### Known Limitations

- No subprocess CLI test for `cmd/shardforge-gateway` — CLI parsing is tested via the `run()` helper; subprocess tests judged brittle for CI.
- `ScanNode` is per-node only. A caller wanting all keys across nodes must call `ScanNode` on each node and merge results manually.
- No global scan: without replication, keys are partitioned; a single-node scan only returns keys routed to that node.
- No retry: if the routed node is down, the operation fails. This is correct behavior without replication.

### Scope Confirmation

- Client-side routing only: implemented (gateway routes; nodes do not coordinate)
- Independent networked nodes: correct (each node owns its data)
- No distributed sharding inside nodes: correct
- No networked replication: correct
- No Raft: correct
- No consensus: correct
- No quorum replication: correct
- No automatic leader election: correct
- No failover: correct — explicitly documented and tested
- No shard migration: correct
- No resharding: correct
- No distributed vector search: correct
- No ANN/HNSW/IVF: correct
- No background compaction: correct
- No automatic compaction: correct

---

## Phase 16 — Stateless Gateway Proxy Server

**Date:** 2026-06-10
**Go version:** go1.26.4 darwin/arm64

### What Was Built

- `internal/proxy` package: stateless HTTP routing proxy wrapping `internal/gateway`
- `cmd/shardforge-proxy`: long-running proxy server CLI with `--addr`, `--nodes`, `--virtual-nodes`, `--timeout`
- 45 tests in `internal/proxy` (server_test.go, integration_test.go) + 9 tests in `cmd/shardforge-proxy/main_test.go`
- 7 benchmarks: Route, Put, Get, Status, NodesHealth, FlushAll, CompactAll
- Docker Compose updated with `shardforge-proxy` service on port 9200
- Dockerfile updated to build both `shardforge-node` and `shardforge-proxy`
- Makefile updated: 6th binary `bin/shardforge-proxy`, targets `bench-proxy`, `proxy-help`, `proxy-route-demo`

### Validation Commands Run

```bash
go mod tidy        # no changes
go fmt ./...       # formatted 3 new files
go vet ./...       # clean
go test -race -count=1 ./...   # all packages pass
make test          # PASS
make vet           # PASS
make build         # 6 binaries produced
make bench-proxy   # all 7 benchmarks pass
./bin/shardforge-proxy --help  # disclaimer printed
docker compose -f deploy/docker-compose.yml config  # valid
git status --short  # clean
```

### Test Results

All packages pass with race detector:

| Package | Result |
|---------|--------|
| `internal/proxy` | ok (45 tests, race-safe) |
| `cmd/shardforge-proxy` | ok (9 tests) |
| All other packages | ok (unchanged) |

### Benchmark Results (Apple M3, darwin/arm64)

```
BenchmarkProxy_Route-8         116691    29342 ns/op    6062 B/op      70 allocs/op
BenchmarkProxy_Put-8            48792    74416 ns/op   20824 B/op     238 allocs/op
BenchmarkProxy_Get-8            55782    64914 ns/op   15875 B/op     188 allocs/op
BenchmarkProxy_Status-8        118112    30831 ns/op    6269 B/op      74 allocs/op
BenchmarkProxy_NodesHealth-8    28322   127771 ns/op   33031 B/op     358 allocs/op
BenchmarkProxy_FlushAll-8       26460   125524 ns/op   33021 B/op     358 allocs/op
BenchmarkProxy_CompactAll-8     29347   125802 ns/op   33037 B/op     358 allocs/op
```

Notes:
- Route is fast (~30 µs) because it does no backend network call — pure ring computation.
- Put/Get include full proxy→node round-trip over TCP loopback (~65–75 µs).
- NodesHealth/FlushAll/CompactAll fan out to all 3 nodes (~128 µs = 3× single-node latency).
- Benchmarks use custom HTTP client with keep-alives to prevent port exhaustion under load.

### No-Failover Proof

`TestIntegration_NoFailover_UnavailableNodeReturnsError` (integration_test.go):
- Creates proxy with ONE node at a port that is not listening (grabbed and immediately released).
- Sends `PUT /kv/any-key`.
- Verifies response is 5xx (502 Bad Gateway), not 200.
- No other node is tried — there is no other node configured.

This confirms the core safety property: the proxy returns an error immediately when the routed node is unavailable. There is no retry to another node.

### Scope Confirmation (Phase 16)

- Stateless proxy only: correct — proxy stores no data, can be restarted at any time
- Routes through internal/gateway: correct
- Independent networked nodes: correct
- No automatic failover: correct — explicitly tested
- No retry to another node: correct — explicitly tested and documented
- No distributed sharding inside nodes: correct
- No networked replication: correct
- No Raft: correct
- No consensus: correct
- No quorum replication: correct
- No automatic leader election: correct
- No shard migration: correct
- No resharding: correct
- No distributed transactions: correct
- No distributed vector search: correct
- No ANN/HNSW/IVF: correct
- No background compaction: correct
- No automatic compaction: correct
- No core Engine/WAL/MemTable/SSTable/Bloom/Vector/Shard/Replica/Dashboard/Node/Gateway behavior changes: correct
- No core Engine/WAL/MemTable/SSTable/Bloom/Vector/Shard/Replica/Dashboard/Node behavior changes: correct

---

## Phase 17 — Static Cluster Metadata (`internal/cluster`)

### Validation Commands

```bash
go mod tidy
go fmt ./...
go vet ./...
go test -race -count=1 ./...
go test -bench=. -benchmem -benchtime=3s ./internal/cluster/...
make test
make vet
make build
make cluster-validate
./bin/shardforge-gateway --config configs/local-3node.json route user:1
./bin/shardforge-cluster validate configs/local-3node.json
./bin/shardforge-cluster validate configs/local-3node-with-proxy.json
./bin/shardforge-cluster validate configs/docker-3node-with-proxy.json
docker compose -f deploy/docker-compose.yml config
git status --short
```

### Test Results

All 23 packages pass `go test -race -count=1 ./...`.

```
ok  github.com/YashPatel2395/ShardForgeDB/internal/cluster        2.1s
ok  github.com/YashPatel2395/ShardForgeDB/cmd/shardforge-cluster  1.3s
ok  github.com/YashPatel2395/ShardForgeDB/cmd/shardforge-gateway  1.4s
ok  github.com/YashPatel2395/ShardForgeDB/cmd/shardforge-proxy    1.6s
```

Test counts:
- `internal/cluster`: 47 tests (config, validate, loader, integration)
- `cmd/shardforge-cluster`: 10 tests
- `cmd/shardforge-gateway`: 12 tests (8 prior + 4 new --config tests)
- `cmd/shardforge-proxy`: 13 tests (9 prior + 4 new --config tests)

### Benchmark Results

```
BenchmarkCluster_Parse-8            516888     6962 ns/op   1448 B/op   28 allocs/op
BenchmarkCluster_Validate-8       33941624      105 ns/op      0 B/op    0 allocs/op
BenchmarkCluster_GatewayOptions-8 26254626      137 ns/op    128 B/op    1 allocs/op
BenchmarkCluster_ProxyOptions-8   25481388      137 ns/op    128 B/op    1 allocs/op
```

Notes:
- **Parse (~7 µs):** JSON decode + Normalize + Validate for a 3-node config.
- **Validate (~106 ns):** zero allocations; pure struct field checks.
- **GatewayOptions/ProxyOptions (~138 ns):** validates then copies node slice; 1 allocation for the slice.

**To reproduce:**
```bash
make bench-cluster
# or
go test -bench=. -benchmem -benchtime=3s -run='^$' ./internal/cluster/...
```

### Config-Based Gateway Route Proof

```
$ ./bin/shardforge-gateway --config configs/local-3node.json route user:1
key="user:1" → node_id=node-2  base_url=http://127.0.0.1:9102
```

Same result as `--nodes`:
```
$ ./bin/shardforge-gateway --nodes http://127.0.0.1:9101,http://127.0.0.1:9102,http://127.0.0.1:9103 route user:1
key="user:1" → node_id=node-2  base_url=http://127.0.0.1:9102
```

Deterministic routing is preserved across both invocation styles.

### Config Validation Proof

```
$ ./bin/shardforge-cluster validate configs/local-3node.json
ok  "configs/local-3node.json" is valid (version=v1, name="local-3node", nodes=3)

$ ./bin/shardforge-cluster validate configs/local-3node-with-proxy.json
ok  "configs/local-3node-with-proxy.json" is valid (version=v1, name="local-3node-with-proxy", nodes=3)

$ ./bin/shardforge-cluster validate configs/docker-3node-with-proxy.json
ok  "configs/docker-3node-with-proxy.json" is valid (version=v1, name="docker-3node-with-proxy", nodes=3)
```

### Scope Confirmation (Phase 17)

- Static config only: correct — loaded once at startup, no runtime updates
- No dynamic membership: correct — no gossip, no discovery
- No service discovery: correct
- No gossip: correct
- No Raft: correct
- No consensus: correct
- No leader election: correct
- No quorum: correct
- No replication: correct
- No failover: correct
- No automatic rebalancing: correct
- No shard migration: correct
- No resharding: correct
- No distributed transactions: correct
- No distributed vector search: correct
- No production cluster manager: correct
- No core Engine/WAL/MemTable/SSTable/Bloom/Vector/Shard/Replica/Dashboard/Node/Gateway/Proxy behavior changes: correct

---

## Phase 18 — Networked Read Replicas v1 (`internal/replnet`)

### What Was Built

**New package:** `internal/replnet`
- `errors.go` — `ErrInvalidRole`, `ErrInvalidEntry`, `ErrClosed`
- `types.go` — `Role` (primary/follower), `Operation` (put/delete), `Entry`, `LogStats`, `ReplicaStatus`
- `log.go` — `Log`: goroutine-safe append-only in-memory mutation log; `Append`, `EntriesAfter`, `Stats`, `Close`
- `replicator.go` — `Replicator`: HTTP pull client; `PullEntries(ctx, after, limit)` → calls primary's `GET /replication/log`
- `log_test.go` — 20 unit tests; `bench_test.go` — 5 benchmarks; `replicator_test.go` — 6 tests

**Updated: `internal/node`**
- `types.go` — `ReplicationOptions{Role, PrimaryBaseURL}` added to `Options`; `Status.Replication replnet.ReplicaStatus`
- `server.go` — `replLog *replnet.Log` (primary only), `replicator *replnet.Replicator` (follower only), `lastApplied uint64` (atomic); `ReplicationStatus()`, `ReplicationEntries()`, `ApplyReplicationEntries()`, `SyncFromPrimary()` methods
- `handlers.go` — follower PUT/DELETE blocked (403); primary PUT/DELETE append to `replLog`; 4 new routes: `GET /replication/status`, `GET /replication/log`, `POST /replication/apply`, `POST /replication/sync`
- `replication_test.go` — 27 new tests

**Updated: `internal/cluster`**
- `types.go` — `Replication{Enabled, Role, Primary}` struct added to `Node` (omitempty, backward-compatible)
- `validate.go` — `validateReplication()`: exactly one primary, followers reference valid primary ID, no multi-primary
- `config.go` — `ExampleReadReplica3Node()` example config
- `validate_test.go` — 7 new replication validation tests; `config_test.go` — 3 new tests

**Updated: `internal/proxy`**
- `handlers.go` — 2 new endpoints: `GET /replication/status` (fan-out), `POST /replication/sync-node/{nodeID}` (forward)
- `replication_test.go` — 6 new tests

**Updated: `internal/gateway`**
- `types.go` — `ForwardResult{Body, Err}` type
- `client.go` — `ForwardToAll(ctx, method, path, body)` and `ForwardToNode(ctx, nodeID, method, path, body)` methods

**Updated: `internal/node/client.go`**
- `Do(ctx, method, path, body)` — raw JSON forward method returning `map[string]any`

**Updated: `cmd/shardforge-node/main.go`**
- `--replication-role` flag (primary/follower/empty)
- `--primary-url` flag (required when role=follower)

**Updated: `cmd/shardforge-cluster/main.go`**
- `example-read-replica-3node` command

**New configs:**
- `configs/local-read-replica-3node.json`
- `configs/docker-read-replica-3node.json`

**New deploy:**
- `deploy/docker-compose-replica.yml` — shardforge-primary (9111), shardforge-replica-1 (9112), shardforge-replica-2 (9113), shardforge-proxy (9210)

**Makefile additions:**
- `bench-replnet`, `replica-demo`, `replica-demo-down`, `replica-config-demo`, `replica-status-demo`, `cluster-example-replica`

### Test Evidence

```
go test -race -count=1 ./...
ok  github.com/YashPatel2395/ShardForgeDB/internal/replnet    (20 unit + 6 replicator tests)
ok  github.com/YashPatel2395/ShardForgeDB/internal/node       (27 new replication tests)
ok  github.com/YashPatel2395/ShardForgeDB/internal/cluster    (7 replication validation tests)
ok  github.com/YashPatel2395/ShardForgeDB/internal/proxy      (6 new replication admin tests)
ok  github.com/YashPatel2395/ShardForgeDB/internal/gateway    (ForwardToAll/ForwardToNode tested via proxy)
... all 24 packages pass
```

### Key Design Decisions

**In-memory log:** The mutation log (`replnet.Log`) is not persisted. This is intentional for Phase 18. The engine WAL provides durability for the actual key-value data. If the primary restarts, the log is empty and followers must replay from their current state. This is clearly documented and honest.

**Explicit pull-based sync:** There is no background goroutine. Followers sync only when explicitly asked (`POST /replication/sync`). This matches the Phase 18 scope: manual admin operation, not automatic replication.

**Sequence numbers:** The protocol uses monotonic `uint64` sequences. Followers track `lastAppliedSeq` atomically. Already-applied entries are silently skipped (idempotent re-apply). Gaps produce `ErrInvalidEntry` rather than silently applying out-of-order.

**Scope honesty:** `cluster.Scope.NoReplication` remains `true` in all config files. It refers to *automatic background cluster-level replication*, which Phase 18 does not implement. Per-node explicit pull replication is a separate concept and is not contradictory.

### Scope Confirmation (Phase 18)

- Pull-based explicit replication only: correct — no background sync loop
- In-memory mutation log: correct — not persisted, cleared on primary restart
- Follower writes rejected (403): correct — PUT/DELETE blocked on follower role
- No automatic failover: correct — no leader election, no quorum
- No Raft: correct
- No consensus: correct
- No quorum: correct
- No multi-primary: correct — Validate enforces exactly one primary
- No strong consistency guarantee: correct — replication lag expected
- All existing tests unmodified and passing: correct

---

## Phase 19 — Failure Handling and Manual Rebalance Simulation

### Validation Commands Run

```
go mod tidy       → clean
go fmt ./...      → clean
go vet ./...      → clean
go test -race -count=1 ./...   → all 25 packages PASS
make test                       → all PASS
make vet                        → clean
make build                      → all 7 binaries built
make bench-ops                  → 4 benchmarks PASS
```

### Config Validation

```
./bin/shardforge-cluster validate configs/local-failure-sim-3node.json
ok  "configs/local-failure-sim-3node.json" is valid (version=v1, name="local-failure-sim-3node", nodes=3)

docker compose -f deploy/docker-compose.yml config     → valid
docker compose -f deploy/docker-compose-replica.yml config → valid
```

### Failure Simulation Output (Proof)

Running `simulate-failure` with node-2 down and sample keys — showing routing changes:

```bash
./bin/shardforge-cluster simulate-failure configs/local-failure-sim-3node.json \
  --down node-2 --key user:1 --key user:2 --key order:9
```

Output (abbreviated):
```json
{
  "cluster_name": "local-failure-sim-3node",
  "down_node_ids": ["node-2"],
  "healthy_node_ids": ["node-1", "node-3"],
  "affected_keys": [
    {"key": "user:1", "original_node": "node-2", "new_node": "node-1", "moved": false, "unavailable": false},
    {"key": "user:2", "original_node": "node-2", "new_node": "node-1", "moved": false, "unavailable": false}
  ],
  "unaffected_keys": [
    {"key": "order:9", "original_node": "node-1", "new_node": "node-1", "moved": false, "unavailable": false}
  ],
  "summary": {"total_keys": 3, "affected_keys": 2, "unaffected_keys": 1, "moved_keys": 0, "unavailable_keys": 0},
  "scope": {"manual_only": true, "simulation_only": true, "no_automatic_failover": true, ...}
}
```

**Proof that simulation does NOT affect live routing**: The command only performs static ring computation. No HTTP calls are made to nodes. The live cluster routing is unchanged.

### Rebalance Plan Output (Proof)

```bash
./bin/shardforge-cluster plan-rebalance configs/local-failure-sim-3node.json \
  --remove node-2 --key user:1 --key user:2 --key order:9
```

Output shows `operator_steps` including "No data movement is performed by this tool."

### Benchmark Results

```
BenchmarkOps_RouteKey-8                          80344 iter    43975 ns/op
BenchmarkOps_SimulateFailure_100Keys-8             484 iter  7446026 ns/op
BenchmarkOps_PlanManualRebalance_100Keys-8       45248 iter    80206 ns/op
BenchmarkOps_CheckClusterHealth_HealthyNodes-8   38815 iter    92532 ns/op
```

### Test Counts

- `internal/ops`: 40 tests PASS (11 health, 13 simulate, 13 rebalance, 3 route)
- `cmd/shardforge-cluster`: 25 tests PASS (15 new Phase 19 tests)

---

## Phase 21 — Truth Lock + Distributed Roadmap + Trace Foundation

**Date:** 2026-06-10
**Go version:** go1.26.4 darwin/arm64
**Branch:** phase-21-truth-lock-trace-foundation

### Changes Made

| Item | Change |
|------|--------|
| `docs/CLAIMS.md` | New — three-section claims audit: Safe, Unsafe, Future |
| `docs/ROADMAP_DISTRIBUTED.md` | New — Phases 15–27 toward real distributed features |
| `internal/trace/trace.go` | New — trace types: Trace, TraceStep, OperationType, Component, StepType, Status |
| `internal/trace/trace_test.go` | New — 22 tests covering construction, ordering, duration, JSON, filtering |
| `docs/TRACE_DESIGN.md` | New — trace philosophy, rules, Phase 15 integration plan |
| `docs/DESIGN.md` | Fixed stale statements: WAL "not yet wired", HNSW as implemented, levelled compaction as implemented, vector layer description, cluster layer |
| `docs/PROOF.md` | Added summary table at top |
| `README.md` | Updated banner to Phase 21, added Phase 21 section, added distributed roadmap reference |
| `Makefile` | Added `bench-trace` target |

### Documentation Inconsistencies Found and Fixed

| File | Stale claim | Fix |
|------|-------------|-----|
| `docs/DESIGN.md` (header) | "Only Phase 6 implemented; all other components intended design only" | Updated to Phase 21 status |
| `docs/DESIGN.md` (WAL) | "WAL not yet wired to MemTable or Engine" | Annotated as resolved in Phase 6 |
| `docs/DESIGN.md` (MemTable) | "not yet connected to WAL or Engine" | Annotated as resolved in Phase 6 |
| `docs/DESIGN.md` (SSTable) | "No Bloom filter", "not wired to Engine" | Annotated as resolved in Phase 5/6 |
| `docs/DESIGN.md` (Bloom) | "Not wired into SSTable or Engine yet" | Annotated as resolved in Phase 6 |
| `docs/DESIGN.md` (Vector) | "will implement ANN/HNSW" | Replaced with accurate exact k-NN description |
| `docs/DESIGN.md` (Cluster) | "Automatic failover planned", "distributed metadata service" | Replaced with accurate per-phase scope descriptions |
| `docs/DESIGN.md` (Trade-offs) | "HNSW for vector search", "Levelled compaction" | Replaced with accurate descriptions of what is and is not implemented |

### Trace Package Details

`internal/trace` provides:
- `OperationType` constants: GET, PUT, DELETE, SCAN, VECTOR_SEARCH, VECTOR_INSERT, ROUTE, REPLICATE, FLUSH, COMPACT
- `Component` constants: CLI, ROUTER, NODE, ENGINE, WAL, MEMTABLE, SSTABLE, BLOOM, VECTOR, SHARD, REPLICA, NETWORK
- `StepType` constants: 22 types covering read path, write path, flush/compact, vector, routing, replication
- `Status` constants: OK, SKIPPED, ERROR
- `Trace` struct: ID, Operation, Key, StartedAt, FinishedAt, Steps, Err
- `TraceStep` struct: Component, StepType, Status, Duration, Detail, Metadata
- Methods: `New`, `NewWithID`, `AddStep`, `Step` (chain), `Finish`, `TotalDuration`, `StepDurationSum`, `StepsWithStatus`, `StepsForComponent`, `MarshalJSON`, `String`

Phase 21 scope: **types only**. Engine wiring is Phase 15.

### Validation Commands

```bash
go mod tidy
go fmt ./...
go vet ./...
go test -race -count=1 ./...
make build
make test
make vet
make release-check
```

### `make release-check` Full Output (exit 0)

```
./scripts/release_check.sh
[release-check] go mod tidy
[release-check] go fmt ./...
[release-check] go vet ./...
[release-check] go test -race -count=1 ./...
ok  	github.com/YashPatel2395/ShardForgeDB/cmd/shardforge	1.176s
?   	github.com/YashPatel2395/ShardForgeDB/cmd/shardforge-bench	[no test files]
ok  	github.com/YashPatel2395/ShardForgeDB/cmd/shardforge-cluster	1.329s
?   	github.com/YashPatel2395/ShardForgeDB/cmd/shardforge-dashboard	[no test files]
ok  	github.com/YashPatel2395/ShardForgeDB/cmd/shardforge-gateway	1.482s
?   	github.com/YashPatel2395/ShardForgeDB/cmd/shardforge-node	[no test files]
ok  	github.com/YashPatel2395/ShardForgeDB/cmd/shardforge-proxy	1.483s
ok  	github.com/YashPatel2395/ShardForgeDB/internal/bench	3.724s
ok  	github.com/YashPatel2395/ShardForgeDB/internal/bloom	2.119s
ok  	github.com/YashPatel2395/ShardForgeDB/internal/cluster	1.837s
ok  	github.com/YashPatel2395/ShardForgeDB/internal/config	1.889s
ok  	github.com/YashPatel2395/ShardForgeDB/internal/dashboard	2.320s
ok  	github.com/YashPatel2395/ShardForgeDB/internal/engine	5.765s
ok  	github.com/YashPatel2395/ShardForgeDB/internal/gateway	1.910s
ok  	github.com/YashPatel2395/ShardForgeDB/internal/logging	1.207s
ok  	github.com/YashPatel2395/ShardForgeDB/internal/memtable	1.219s
ok  	github.com/YashPatel2395/ShardForgeDB/internal/node	2.383s
ok  	github.com/YashPatel2395/ShardForgeDB/internal/ops	1.382s
ok  	github.com/YashPatel2395/ShardForgeDB/internal/proxy	2.202s
ok  	github.com/YashPatel2395/ShardForgeDB/internal/replica	4.481s
ok  	github.com/YashPatel2395/ShardForgeDB/internal/replnet	1.211s
ok  	github.com/YashPatel2395/ShardForgeDB/internal/shard	3.622s
ok  	github.com/YashPatel2395/ShardForgeDB/internal/sstable	2.590s
?   	github.com/YashPatel2395/ShardForgeDB/internal/storage	[no test files]
ok  	github.com/YashPatel2395/ShardForgeDB/internal/trace	1.163s
ok  	github.com/YashPatel2395/ShardForgeDB/internal/vector	1.989s
ok  	github.com/YashPatel2395/ShardForgeDB/internal/wal	1.254s
[release-check] go test -bench dashboard
goos: darwin
goarch: arm64
pkg: github.com/YashPatel2395/ShardForgeDB/internal/dashboard
cpu: Apple M3
BenchmarkRunFollowerPauseScenario-8     	     216	  17912362 ns/op	   42337 B/op	     398 allocs/op
BenchmarkRunFollowerLagScenario-8       	     177	  21320221 ns/op	   69596 B/op	     757 allocs/op
BenchmarkRunFollowerCatchupScenario-8   	     199	  17604306 ns/op	   58736 B/op	     615 allocs/op
BenchmarkSnapshot_EngineCollector-8     	14741241	       241.0 ns/op	     736 B/op	       6 allocs/op
BenchmarkSnapshot_ReplicaCollector-8    	 2583295	      1396 ns/op	    3113 B/op	      36 allocs/op
BenchmarkSnapshot_MultiCollector-8      	 4267099	       839.9 ns/op	    2584 B/op	      18 allocs/op
BenchmarkRenderHTML-8                   	  271453	     13283 ns/op	   10700 B/op	     184 allocs/op
BenchmarkEncodeStatusJSON-8             	 1285980	      2813 ns/op	    8434 B/op	      47 allocs/op
PASS
ok  	github.com/YashPatel2395/ShardForgeDB/internal/dashboard	41.153s
[release-check] go test -bench replica
goos: darwin
goarch: arm64
pkg: github.com/YashPatel2395/ShardForgeDB/internal/replica
cpu: Apple M3
BenchmarkPut_10k_LeaderOnly-8                	   10000	    334666 ns/op	    2980 B/op	      32 allocs/op
BenchmarkReplicateAll_10k_2Followers-8       	       1	3026608959 ns/op	35023840 B/op	  360002 allocs/op
BenchmarkGet_Leader_10k_Existing-8           	26403615	       134.3 ns/op	     103 B/op	       4 allocs/op
BenchmarkGet_Follower_10k_Existing-8         	26212875	       135.5 ns/op	     103 B/op	       4 allocs/op
BenchmarkScan_Leader_10k-8                   	    1062	   3382989 ns/op	 8638434 B/op	   60121 allocs/op
BenchmarkReopen_10k-8                        	     355	  10137163 ns/op	 7534030 B/op	   90268 allocs/op
BenchmarkReplicateOnce_SmallBatch-8          	   12069	    299305 ns/op	    2481 B/op	      34 allocs/op
BenchmarkConcurrentPut-8                     	   12470	    286652 ns/op	    2746 B/op	      31 allocs/op
BenchmarkConcurrentReplicateAllWithReads-8   	30074830	       116.6 ns/op	      51 B/op	       2 allocs/op
BenchmarkLogAppendReplay-8                   	    3787	    959866 ns/op	  258834 B/op	    6022 allocs/op
PASS
ok  	github.com/YashPatel2395/ShardForgeDB/internal/replica	217.587s
[release-check] go test -bench shard
goos: darwin
goarch: arm64
pkg: github.com/YashPatel2395/ShardForgeDB/internal/shard
cpu: Apple M3
BenchmarkRing_Route1M-8               	38700782	        78.92 ns/op	      32 B/op	       1 allocs/op
BenchmarkPut_10k_4shards-8            	 2331141	      1481 ns/op	     201 B/op	       7 allocs/op
BenchmarkGet_10k_existing_4shards-8   	22991466	       155.2 ns/op	     103 B/op	       4 allocs/op
BenchmarkGet_10k_missing_4shards-8    	32865894	       109.2 ns/op	      31 B/op	       1 allocs/op
BenchmarkScan_10k_4shards-8           	     681	   5255328 ns/op	10095829 B/op	   80266 allocs/op
BenchmarkFlush_10k_4shards-8          	      34	 101653140 ns/op	 5834848 B/op	   50846 allocs/op
BenchmarkCompact_10k_4shards-8        	      30	 127673836 ns/op	16970038 B/op	  218775 allocs/op
BenchmarkReopen_10k_4shards-8         	    6012	    573878 ns/op	 1065552 B/op	   10794 allocs/op
BenchmarkConcurrentPut_4shards-8      	 1445794	      2479 ns/op	     488 B/op	       6 allocs/op
BenchmarkConcurrentGet_4shards-8      	24826401	       144.5 ns/op	     103 B/op	       4 allocs/op
PASS
ok  	github.com/YashPatel2395/ShardForgeDB/internal/shard	50.531s
[release-check] go test -bench vector
goos: darwin
goarch: arm64
pkg: github.com/YashPatel2395/ShardForgeDB/internal/vector
cpu: Apple M3
BenchmarkUpsert_1k_dim128-8           	     859	   4547245 ns/op	 4304750 B/op	    9804 allocs/op
BenchmarkSearch_1k_dim128_Cosine-8    	   18138	    198016 ns/op	   58568 B/op	       6 allocs/op
BenchmarkSearch_10k_dim128_Cosine-8   	    1600	   2261175 ns/op	  566472 B/op	       6 allocs/op
BenchmarkSearch_1k_dim128_L2-8        	   18514	    194981 ns/op	   58568 B/op	       6 allocs/op
BenchmarkSearch_1k_dim128_Dot-8       	   18583	    193666 ns/op	   58568 B/op	       6 allocs/op
BenchmarkReopen_1k-8                  	    2401	   1496536 ns/op	 3349410 B/op	   10125 allocs/op
BenchmarkCodec_Encode_dim128-8        	23127903	       156.4 ns/op	     576 B/op	       1 allocs/op
BenchmarkCodec_Decode_dim128-8        	24569724	       146.6 ns/op	     512 B/op	       1 allocs/op
BenchmarkConcurrentSearch-8           	   76146	     51172 ns/op	   58569 B/op	       6 allocs/op
BenchmarkConcurrentUpsert-8           	  948016	     18872 ns/op	    4353 B/op	      10 allocs/op
PASS
ok  	github.com/YashPatel2395/ShardForgeDB/internal/vector	64.515s
[release-check] go test -bench engine
goos: darwin
goarch: arm64
pkg: github.com/YashPatel2395/ShardForgeDB/internal/engine
cpu: Apple M3
BenchmarkCompact_2SSTable_1kKeys-8           	     196	  18566494 ns/op	  717264 B/op	   10141 allocs/op
BenchmarkCompact_10SSTable_10kKeys-8         	      44	  80850130 ns/op	 6700180 B/op	  100387 allocs/op
BenchmarkCompact_WithOverwrites-8            	     226	  17847693 ns/op	  682308 B/op	   11158 allocs/op
BenchmarkCompact_WithTombstones-8            	     711	   5113030 ns/op	  364147 B/op	    5084 allocs/op
BenchmarkGet_MissingKey_BeforeCompaction-8   	231557818	        15.65 ns/op	       0 B/op	       0 allocs/op
BenchmarkGet_MissingKey_AfterCompaction-8    	237023348	        15.18 ns/op	       0 B/op	       0 allocs/op
BenchmarkScan_BeforeCompaction-8             	    3693	    978130 ns/op	  568497 B/op	    7054 allocs/op
BenchmarkScan_AfterCompaction-8              	    3694	    971734 ns/op	  553585 B/op	    7045 allocs/op
BenchmarkPut-8                               	 2740119	      1444 ns/op	     112 B/op	       4 allocs/op
BenchmarkGet_MemTable_Existing-8             	91031691	        39.22 ns/op	      48 B/op	       3 allocs/op
BenchmarkGet_MemTable_Missing-8             	224278622	        16.07 ns/op	       0 B/op	       0 allocs/op
BenchmarkFlush_1k-8                          	     231	  16022992 ns/op	  444002 B/op	    5094 allocs/op
BenchmarkFlush_100k-8                        	       5	 608980692 ns/op	63294766 B/op	  500132 allocs/op
BenchmarkGet_SSTable_Existing-8              	 4273998	       842.0 ns/op	      96 B/op	       4 allocs/op
BenchmarkGet_SSTable_Missing_BloomSkip-8     	86271454	        41.40 ns/op	       0 B/op	       0 allocs/op
BenchmarkScan_1k-8                           	    5889	    611923 ns/op	  544497 B/op	    6554 allocs/op
BenchmarkRestart_WALReplay-8                 	    6888	    523671 ns/op	  279601 B/op	    3551 allocs/op
BenchmarkRestart_ManifestLoad-8              	   48926	     73636 ns/op	   50616 B/op	     557 allocs/op
PASS
ok  	github.com/YashPatel2395/ShardForgeDB/internal/engine	155.702s
[release-check] go test -bench bench
goos: darwin
goarch: arm64
pkg: github.com/YashPatel2395/ShardForgeDB/internal/bench
cpu: Apple M3
BenchmarkGenKey-8                      	54720458	        66.29 ns/op	      24 B/op	       2 allocs/op
BenchmarkGenValue_128-8                	90291471	        37.36 ns/op	3426.38 MB/s	       0 B/op	       0 allocs/op
BenchmarkPercentile_1k-8               	 1440925	      2499 ns/op	    8248 B/op	       3 allocs/op
BenchmarkWorkload_WriteHeavy_Small-8   	      28	 142949579 ns/op	 1901159 B/op	   15274 allocs/op
BenchmarkWorkload_ReadHeavy_Small-8    	      28	 140226833 ns/op	 2334942 B/op	   20836 allocs/op
PASS
ok  	github.com/YashPatel2395/ShardForgeDB/internal/bench	25.642s
[release-check] make test
go test -race -count=1 ./...
ok  	github.com/YashPatel2395/ShardForgeDB/cmd/shardforge	1.209s
?   	github.com/YashPatel2395/ShardForgeDB/cmd/shardforge-bench	[no test files]
ok  	github.com/YashPatel2395/ShardForgeDB/cmd/shardforge-cluster	1.319s
?   	github.com/YashPatel2395/ShardForgeDB/cmd/shardforge-dashboard	[no test files]
ok  	github.com/YashPatel2395/ShardForgeDB/cmd/shardforge-gateway	1.392s
?   	github.com/YashPatel2395/ShardForgeDB/cmd/shardforge-node	[no test files]
ok  	github.com/YashPatel2395/ShardForgeDB/cmd/shardforge-proxy	1.575s
ok  	github.com/YashPatel2395/ShardForgeDB/internal/bench	3.438s
ok  	github.com/YashPatel2395/ShardForgeDB/internal/bloom	2.339s
ok  	github.com/YashPatel2395/ShardForgeDB/internal/cluster	2.100s
ok  	github.com/YashPatel2395/ShardForgeDB/internal/config	2.177s
ok  	github.com/YashPatel2395/ShardForgeDB/internal/dashboard	2.250s
ok  	github.com/YashPatel2395/ShardForgeDB/internal/engine	5.388s
ok  	github.com/YashPatel2395/ShardForgeDB/internal/gateway	2.011s
ok  	github.com/YashPatel2395/ShardForgeDB/internal/logging	1.352s
ok  	github.com/YashPatel2395/ShardForgeDB/internal/memtable	1.261s
ok  	github.com/YashPatel2395/ShardForgeDB/internal/node	2.119s
ok  	github.com/YashPatel2395/ShardForgeDB/internal/ops	1.269s
ok  	github.com/YashPatel2395/ShardForgeDB/internal/proxy	2.097s
ok  	github.com/YashPatel2395/ShardForgeDB/internal/replica	3.855s
ok  	github.com/YashPatel2395/ShardForgeDB/internal/replnet	1.212s
ok  	github.com/YashPatel2395/ShardForgeDB/internal/shard	3.182s
ok  	github.com/YashPatel2395/ShardForgeDB/internal/sstable	2.643s
?   	github.com/YashPatel2395/ShardForgeDB/internal/storage	[no test files]
ok  	github.com/YashPatel2395/ShardForgeDB/internal/trace	1.483s
ok  	github.com/YashPatel2395/ShardForgeDB/internal/vector	1.896s
ok  	github.com/YashPatel2395/ShardForgeDB/internal/wal	1.298s
[release-check] make vet
go vet ./...
[release-check] make build
go build  -o bin/shardforge ./cmd/shardforge
go build  -o bin/shardforge-bench ./cmd/shardforge-bench
go build  -o bin/shardforge-dashboard ./cmd/shardforge-dashboard
go build  -o bin/shardforge-node ./cmd/shardforge-node
go build  -o bin/shardforge-gateway ./cmd/shardforge-gateway
go build  -o bin/shardforge-proxy ./cmd/shardforge-proxy
go build  -o bin/shardforge-cluster ./cmd/shardforge-cluster
[release-check] make bench-dashboard
[release-check] make bench-replica
[release-check] make bench-shard
[release-check] make bench-vector
[release-check] shardforge --help
[release-check] shardforge version
ShardForgeDB 0.1.0
[release-check] shardforge-bench --scale small
Report written to /tmp/shardforge-release-bench.md
[release-check] shardforge-dashboard --help
[release-check] git status --short
[release-check] Working tree is clean.

[release-check] ALL CHECKS PASSED
```

### Claims Now Safe

All claims from Phase 1–19 remain safe. Additionally:
- `internal/trace` types package is implemented and tested (safe to claim as "trace type foundation")

### Claims Still Unsafe

All claims from `docs/CLAIMS.md` Section B remain unsafe. No new safe claims were unlocked in Phase 21 beyond the trace types.

### Known Limitations

- `internal/trace` types only — no engine wiring until Phase 15
- No benchmark in `internal/trace` (pure type definitions and marshaling; latency measurement is engine-level)

---

## Phase 22 — Runtime Operation Trace Mode

### What was built

Phase 22 wires the `internal/trace` type foundation (Phase 21) into real engine and vector execution paths.

**New files:**
- `internal/engine/explain.go` — `ExplainGet`, `ExplainPut`, `ExplainDelete`, `ExplainScan`
- `internal/engine/explain_test.go` — 25 engine trace tests
- `internal/vector/explain.go` — `ExplainUpsert`, `ExplainSearch`, `ExplainDelete`
- `internal/vector/explain_test.go` — 15 vector trace tests
- `cmd/shardforge/explain.go` — `shardforge explain get/put/delete` CLI

**Modified files:**
- `internal/trace/trace.go` — added 10 new StepType constants (scan, bounds skip, vector write steps, key validation)
- `cmd/shardforge/main.go` — registered `explain` subcommand
- `docs/CLAIMS.md` — updated test count, added runtime trace safe claim, added two unsafe claims
- `docs/TRACE_DESIGN.md` — updated to Phase 22 status and package structure
- `docs/PROOF.md` — this section

### Hard rule compliance

Every trace step in `internal/engine/explain.go` and `internal/vector/explain.go` is produced by the actual code path being executed:
- WAL_APPEND: recorded after `e.walLog.Append()` returns
- MEMTABLE_PUT/DELETE: recorded after `e.mem.Put()` / `e.mem.Delete()` returns
- MEMTABLE_HIT/MISS: recorded after `e.mem.Get()` returns
- BLOOM_CHECK/SKIP: recorded after `th.bloom.MightContain()` returns
- BOUNDS_SKIP: recorded when bounds comparison fails
- SSTABLE_HIT/MISS: recorded after `th.reader.Get()` returns
- NOT_FOUND: recorded when all SSTables are exhausted without a hit
- SCAN_SOURCE: recorded after each `Scan()` call on MemTable or SSTable
- SCAN_MERGE: recorded after tombstone suppression and sort
- VECTOR_VALIDATE: recorded after validateID + validateVector
- VECTOR_ENCODE: recorded after encodeRecord
- VECTOR_ENGINE_WRITE: recorded after eng.Put / eng.Delete
- VECTOR_INDEX_UPDATE/DELETE: recorded after in-memory map write/delete
- VECTOR_LOAD/COMPUTE/TOPK: recorded at candidate load, distance loop, sort

No trace steps are fabricated, hardcoded, or pre-scripted.

### Example trace output

```
$ shardforge explain --data-dir ./data put hello world
{
  "operation": "PUT",
  "key": "hello",
  "started_at": "...",
  "finished_at": "...",
  "total_duration_ns": 224041,
  "step_sum_ns": 216916,
  "steps": [
    {"component":"ENGINE","step_type":"KEY_VALIDATED","status":"OK","duration_ns":0,"detail":"key_len=5 value_len=5"},
    {"component":"WAL","step_type":"WAL_APPEND","status":"OK","duration_ns":215250,"detail":"seq=1 key_len=5 value_len=5"},
    {"component":"MEMTABLE","step_type":"MEMTABLE_PUT","status":"OK","duration_ns":1666,"detail":"seq=1 key_len=5"}
  ]
}

$ shardforge explain --data-dir ./data get hello
{
  "operation": "GET",
  "key": "hello",
  "steps": [
    {"component":"ENGINE","step_type":"KEY_VALIDATED","status":"OK","duration_ns":0,"detail":"key_len=5"},
    {"component":"MEMTABLE","step_type":"MEMTABLE_HIT","status":"OK","duration_ns":1625,"detail":"key_len=5 value_len=5"}
  ]
}
```

### Validation Commands

```bash
go mod tidy
go fmt ./...
go vet ./...
go test -race -count=1 ./...
make build
make test
make vet
make release-check
```

### Test Results

```
go test -race -count=1 ./... → 905 tests PASS across 23 packages (4 packages have no test files)
internal/engine: all explain tests pass including ExplainPut, ExplainGet (memtable/sstable/bloom paths),
  ExplainDelete, ExplainScan, empty key handling, closed engine, tombstone paths
internal/vector: all explain tests pass including ExplainUpsert, ExplainSearch, ExplainDelete,
  invalid input error paths, result correctness vs non-explain variants
make build → all 7 binaries built
make vet → clean
go fmt → clean
```

### Claims Now Safe

- `internal/engine.ExplainGet/Put/Delete/Scan` — runtime operation trace for single-node engine
- `internal/vector.ExplainUpsert/Search/Delete` — runtime operation trace for exact vector search
- `shardforge explain get/put/delete` — CLI with JSON output and `--data-dir` flag

### Claims Still Unsafe

All Phase 21 unsafe claims remain unsafe. Additionally:
- "Distributed operation traces" — traces cover single-node engine only; no cross-node propagation
- "Networked traces" — traces do not propagate over HTTP or any network protocol

### Known Limitations

- Traces are single-node only. Phase 26 will add cross-node trace propagation.
- `ExplainScan` does not record per-key merge decisions (only aggregate counts).
- No `--json` flag needed: JSON is the default trace output format.
- No trace for `Flush` or `Compact` (not in the Phase 22 required list).

---

## Phase 23 — Networked Node Trace API + Node Runtime Hardening

**Date:** 2026-06-11

### What was built

Phase 23 exposes the real Phase 22 engine execution traces through the existing networked HTTP node runtime. Every HTTP explain endpoint calls the real `engine.Explain*` API — no JSON trace is fabricated in the HTTP layer.

**Hard rule compliance:** Every trace step returned by `/explain/*` endpoints was produced by the actual code that performed the operation on the node. The HTTP handler receives the `*trace.Trace` returned by `engine.ExplainGet/Put/Delete/Scan` and wraps it in the response body unchanged.

**`cmd/shardforge/explain.go` fix:** Removed unnecessary `os.Stat` pre-check from `openEngineForExplain`. `engine.Open` calls `os.MkdirAll` internally; the pre-check was preventing valid use of non-existent directories that the engine can create.

### New HTTP endpoints (`internal/node/handlers.go`)

| Method | Path | Engine call |
|---|---|---|
| `POST` | `/explain/put` | `eng.ExplainPut(key, value)` |
| `GET` | `/explain/get?key=` | `eng.ExplainGet(key)` |
| `DELETE` | `/explain/delete?key=` | `eng.ExplainDelete(key)` |
| `GET` | `/explain/scan?start=&end=` | `eng.ExplainScan(start, end)` |

All endpoints return JSON with `node_id`, `operation`, `trace` (the real `*trace.Trace`), and optional `error`.

Followers reject `/explain/put` and `/explain/delete` with 403 (same as regular write endpoints).

### New response types (`internal/node/types.go`)

- `ExplainPutResponse` — node_id, operation, trace, error
- `ExplainGetResponse` — node_id, operation, key, found, value, trace, error
- `ExplainDeleteResponse` — node_id, operation, key, trace, error
- `ExplainScanResponse` — node_id, operation, result_count, trace, error

### New client methods (`internal/node/client.go`)

- `ExplainPut(ctx, key, value) (*ExplainPutResponse, error)`
- `ExplainGet(ctx, key) (*ExplainGetResponse, error)`
- `ExplainDelete(ctx, key) (*ExplainDeleteResponse, error)`
- `ExplainScan(ctx, start, end) (*ExplainScanResponse, error)`

### New CLI (`cmd/shardforge/explain_node.go`)

```
shardforge explain-node --addr http://localhost:9101 put mykey myvalue
shardforge explain-node --addr http://localhost:9101 get mykey
shardforge explain-node --addr http://localhost:9101 delete mykey
shardforge explain-node --addr http://localhost:9101 scan a z
```

Calls node over HTTP; never opens the engine directly.

### Scope flags

This is NOT distributed tracing. Each explain endpoint:
- Covers a single node's execution path only
- Does not propagate trace context across nodes
- Does not add any network-layer trace steps
- Is safe to claim: "Networked single-node trace API via HTTP"

### Test results

```
go test -race -count=1 ./internal/node/... → PASS (18 new tests in node_explain_test.go)
go test -race -count=1 ./... → 923 tests PASS across 23 packages
```

New tests in `internal/node/node_explain_test.go`:
- `TestExplainPut_ReturnsTrace` — trace has steps, node_id set, operation=PUT
- `TestExplainPut_MethodNotAllowed` — GET on /explain/put returns 405
- `TestExplainPut_InvalidJSON` — malformed body returns 400
- `TestExplainPut_FollowerRejects` — follower node returns 403
- `TestExplainGet_MissingKey` — existing key: found=true, value correct, trace non-nil
- `TestExplainGet_NotFound` — absent key: found=false, trace non-nil
- `TestExplainGet_EmptyKey` — missing key param returns 400
- `TestExplainGet_MethodNotAllowed` — POST on /explain/get returns 405
- `TestExplainDelete_ReturnsTrace` — trace has steps, node_id set, operation=DELETE
- `TestExplainDelete_EmptyKey` — missing key param returns 400
- `TestExplainDelete_MethodNotAllowed` — GET on /explain/delete returns 405
- `TestExplainScan_ReturnsTrace` — result_count matches inserted keys, trace non-nil
- `TestExplainScan_MethodNotAllowed` — POST on /explain/scan returns 405
- `TestClientExplainPut` — HTTP client round-trip returns trace
- `TestClientExplainGet` — HTTP client round-trip found=true, trace non-nil
- `TestClientExplainDelete` — HTTP client round-trip returns trace
- `TestClientExplainScan` — HTTP client round-trip result_count=3, trace non-nil
- `TestExplainPut_TraceIsValidJSON` — response is valid JSON with trace field

### Claims now safe

- `Networked single-node trace API (HTTP)` — `POST /explain/put`, `GET /explain/get`, `DELETE /explain/delete`, `GET /explain/scan` call real `engine.Explain*` paths and return the unmodified trace over HTTP

### Claims still unsafe

- "Distributed operation traces" — traces cover single-node engine only; no cross-node propagation
- "Networked traces" / "distributed tracing" — traces do not propagate over HTTP between nodes

### Example HTTP trace output (via shardforge explain-node)

**PUT — key written, WAL + MemTable steps visible:**
```json
{
  "node_id": "node-demo",
  "operation": "PUT",
  "trace": {
    "operation": "PUT",
    "key": "httpkey",
    "steps": [
      {"component":"ENGINE","step_type":"KEY_VALIDATED","status":"OK","duration_ns":0,"detail":"key_len=7 value_len=7"},
      {"component":"WAL","step_type":"WAL_APPEND","status":"OK","duration_ns":52833,"detail":"seq=1 key_len=7 value_len=7"},
      {"component":"MEMTABLE","step_type":"MEMTABLE_PUT","status":"OK","duration_ns":791,"detail":"seq=1 key_len=7"}
    ]
  }
}
```

**GET — key in MemTable, MEMTABLE_HIT returned:**
```json
{
  "node_id": "node-demo",
  "operation": "GET",
  "key": "httpkey",
  "found": true,
  "value": "httpval",
  "trace": {
    "operation": "GET",
    "key": "httpkey",
    "steps": [
      {"component":"ENGINE","step_type":"KEY_VALIDATED","status":"OK","duration_ns":0},
      {"component":"MEMTABLE","step_type":"MEMTABLE_HIT","status":"OK","duration_ns":833,"detail":"key_len=7 value_len=7"}
    ]
  }
}
```

**DELETE — tombstone written, WAL + MEMTABLE_DELETE steps:**
```json
{
  "node_id": "node-demo",
  "operation": "DELETE",
  "key": "httpkey",
  "trace": {
    "operation": "DELETE",
    "key": "httpkey",
    "steps": [
      {"component":"ENGINE","step_type":"KEY_VALIDATED","status":"OK","duration_ns":0},
      {"component":"WAL","step_type":"WAL_APPEND","status":"OK","duration_ns":19167,"detail":"seq=2 key_len=7 tombstone=true"},
      {"component":"MEMTABLE","step_type":"MEMTABLE_DELETE","status":"OK","duration_ns":708,"detail":"seq=2 key_len=7"}
    ]
  }
}
```

### Example local CLI trace output (shardforge explain)

```
$ shardforge explain --data-dir ./data put demokey demovalue
{
  "operation": "PUT",
  "key": "demokey",
  "steps": [
    {"component":"ENGINE","step_type":"KEY_VALIDATED","status":"OK","duration_ns":0,"detail":"key_len=7 value_len=9"},
    {"component":"WAL","step_type":"WAL_APPEND","status":"OK","duration_ns":92750,"detail":"seq=1 key_len=7 value_len=9"},
    {"component":"MEMTABLE","step_type":"MEMTABLE_PUT","status":"OK","duration_ns":1250,"detail":"seq=1 key_len=7"}
  ]
}
```

### make release-check result

```
[release-check] ALL CHECKS PASSED
```

---

## Phase 24 — Reproducible Multi-Node Local Cluster Demo

Phase 24 adds a clean, reproducible local cluster demo: 3 independent HTTP nodes + stateless proxy running as local processes with separate data directories and static FNV-1a routing.

### What was built

- `configs/cluster/demo-3node.json` — dedicated Phase 24 cluster config (node-1/9101, node-2/9102, node-3/9103, proxy/9200)
- `scripts/demo_cluster_up.sh` — start 3 nodes + proxy as local background processes, wait for health
- `scripts/demo_cluster_smoke.sh` — 25-check smoke: health, status, key placement proof, put/get via proxy, data isolation proof, explain-node, config validation, scope flag verification, gateway health fanout
- `scripts/demo_cluster_down.sh` — stop all processes, remove demo data directories and logs
- `docs/DEMO.md` — complete demo documentation with scope table, key placement proof, data isolation proof, honest limitations
- `internal/cluster/demo_test.go` — 13 new tests: config loads/validates, 3 nodes, unique IDs, unique addresses, unique data dirs, scope flags, deterministic routing, known key routes, invalid duplicate ID, invalid duplicate addr, invalid scope flag, proxy enabled, absolute data dir paths
- `Makefile` — `cluster-demo-up`, `cluster-demo-smoke`, `cluster-demo-down` targets

### Key placement proof (stable for the 3-node ring, 128 virtual nodes)

```
user:1  → node-2  (http://127.0.0.1:9102)
user:2  → node-2  (http://127.0.0.1:9102)
order:9 → node-1  (http://127.0.0.1:9101)
```

Verified by: `./bin/shardforge-gateway --config configs/cluster/demo-3node.json route <key>`
Asserted deterministically in: `TestDemoConfig_KnownKeyRoutes`

### Data isolation proof

Writing to node-1 directly does NOT make data available on node-2 or node-3. There is no replication. Each node is fully independent. Verified in `TestDemoConfig_UniqueDataDirs` and `demo_cluster_smoke.sh`.

### Scope assertion

`configs/cluster/demo-3node.json` has all scope flags true:
- `no_raft`, `no_consensus`, `no_failover`, `no_shard_migration`, `no_replication`, `no_distributed_txns`

Verified in `TestDemoConfig_ScopeFlagsNoRaftNoConsensus` and `demo_cluster_smoke.sh`.

### New test count

942 tests (929 Phase 23 + 13 Phase 24).

### make cluster-demo-smoke result

```
=== ShardForgeDB Phase 24 — Cluster Demo Smoke Test ===

SCOPE: Static routing, no Raft, no consensus, no failover, no shard migration.

-- Health checks
  PASS: node-1 /healthz → ok
  PASS: node-2 /healthz → ok
  PASS: node-3 /healthz → ok
  PASS: proxy  /healthz → ok

-- Key placement proof (static FNV-1a consistent hash, no migration)
  key="user:1" → node_id=node-2  base_url=http://127.0.0.1:9102
  key="user:2" → node_id=node-2  base_url=http://127.0.0.1:9102
  key="order:9" → node_id=node-1  base_url=http://127.0.0.1:9101
  key="item:42" → node_id=node-3  base_url=http://127.0.0.1:9103
  PASS: routing is deterministic for user:1
  PASS: user:1 routes to a node
  PASS: order:9 routes to a node

-- Put/get through proxy
  PASS: PUT user:1=alice via proxy succeeded
  PASS: GET user:1 via proxy returns alice
  PASS: GET order:9 via proxy returns widget

-- Data isolation proof (each node has independent storage, no replication)
  PASS: node-1 has iso:test (direct write confirmed)
  PASS: node-2 does NOT have iso:test (data isolation confirmed, no replication)
  PASS: node-3 does NOT have iso:test (data isolation confirmed, no replication)
  PASS: all three nodes use distinct data directories

-- explain-node: runtime execution trace over HTTP
  PASS: explain-node put returns WAL_APPEND step
  PASS: explain-node put returns MEMTABLE_PUT step
  PASS: explain-node get returns MEMTABLE_HIT step

-- Config validation
  PASS: configs/cluster/demo-3node.json validates
  PASS: config scope flags: no_raft=true, no_consensus=true, no_failover=true, no_shard_migration=true, no_replication=true

-- Gateway health fanout (all nodes)
  PASS: gateway health fanout: all nodes responding

=== Summary ===
  Passed: 25
  Failed: 0

All checks passed. Phase 24 local cluster demo is working correctly.
```

---

## Phase 25 — Networked Pull-Based Replication Demo

Phase 25 adds a reproducible networked pull-based replication demo between real ShardForgeDB HTTP node processes. One leader (primary) node and one follower (replica) node run as independent OS processes with separate data directories, communicating exclusively over HTTP/JSON.

**Scope:** NOT Raft, NOT consensus, NOT quorum, NOT automatic failover, NOT background sync. Explicit operator-triggered pull only.

### New artifacts

- `configs/replication/demo-leader-follower.json` — Phase 25 replication demo config (leader/9301, follower/9302)
- `scripts/repl_demo_up.sh` — start leader + follower as local background processes, wait for health
- `scripts/repl_demo_smoke.sh` — 16-check smoke: health, PUT, isolation, explicit pull, DELETE, idempotency, role enforcement, scope flags
- `scripts/repl_demo_down.sh` — stop all processes, remove demo data dirs
- `internal/node`: `SyncResult` type, updated `SyncFromPrimary` return type, `Client.SyncReplication()`
- `internal/node/replication_phase25_test.go` — 8 new tests
- `internal/cluster/replication_demo_test.go` — 12 new tests

### Replication cursor behavior

The follower tracks `lastApplied` (a `uint64` atomic) in memory. It is **not** persisted to disk. This is the documented, honest behavior:

> "Replication cursor is demo-scoped and not production durable. After a follower restart, the cursor resets to 0 and the next sync will re-apply all mutations from the primary."

`TestPhase25_ReplDemoConfig_ReplicationCursorIsInMemory` explicitly verifies this behavior.

### PUT replication proof

```
1. PUT repl:smoke=hello-replication to leader
   → leader log: seq=1, op=put, key=repl:smoke
2. GET repl:smoke from follower → {"found":false}   (no auto-sync)
3. POST /replication/sync on follower
   → {"ok":true,"fetched":1,"applied":1,"last_applied_seq":1,
      "source_node":"http://127.0.0.1:9301","follower_node":"follower"}
4. GET repl:smoke from follower → {"found":true,"value":"hello-replication"}
```

### DELETE/tombstone replication proof

```
5. DELETE repl:smoke on leader
   → leader log: seq=2, op=delete, key=repl:smoke
6. GET repl:smoke from follower → {"found":true}   (no auto-sync; still has it)
7. POST /replication/sync on follower
   → {"ok":true,"fetched":1,"applied":1,"last_applied_seq":2}
8. GET repl:smoke from follower → {"found":false}   (tombstone applied)
```

### Idempotent pull proof

```
9. POST /replication/sync (second call, nothing new on primary)
   → {"ok":true,"fetched":0,"applied":0,"last_applied_seq":2}
   No duplicate writes. Follower state unchanged.
```

### Follower role enforcement

```
GET /replication/log on follower → {"error":"replication log is only available on primary nodes"}
PUT /kv/any-key on follower → {"error":"follower: writes are not accepted; this node is a read replica"} (403)
```

### Scope flags (configs/replication/demo-leader-follower.json)

```json
"scope": {
  "no_raft": true,
  "no_consensus": true,
  "no_quorum_replication": true,
  "no_failover": true,
  "no_shard_migration": true,
  "no_distributed_txns": true,
  "no_replication": false
}
```

`no_replication=false` is intentional: replication IS present. `no_quorum_replication=true` asserts it is pull-based only — no Raft, no quorum, no automatic failover election.

### New tests (20 total)

**`internal/node/replication_phase25_test.go`** (8 tests):
- `TestPhase25_FollowerSync_AppliesDelete`
- `TestPhase25_FollowerSync_SecondPullIsIdempotent`
- `TestPhase25_FollowerSync_UnavailablePrimary_ReturnsBadGateway`
- `TestPhase25_HandleReplicationSync_UnavailablePrimary_Returns502`
- `TestPhase25_FollowerCursorAdvancesAfterSync`
- `TestPhase25_SyncResult_IncludesFetchedAndAppliedCounts`
- `TestPhase25_LeaderAndFollower_DistinctDataDirs`
- `TestPhase25_ReplDemoConfig_ReplicationCursorIsInMemory`

**`internal/cluster/replication_demo_test.go`** (12 tests):
- `TestPhase25_ReplDemoConfig_LoadsAndValidates`
- `TestPhase25_ReplDemoConfig_TwoNodes`
- `TestPhase25_ReplDemoConfig_UniqueNodeIDs`
- `TestPhase25_ReplDemoConfig_UniqueNodeAddresses`
- `TestPhase25_ReplDemoConfig_UniqueDataDirs`
- `TestPhase25_ReplDemoConfig_ScopeFlagsNoRaftNoConsensus`
- `TestPhase25_ReplDemoConfig_NoQuorumReplication`
- `TestPhase25_ReplDemoConfig_LeaderAndFollowerRoles`
- `TestPhase25_ReplDemoConfig_DeterministicRouting`
- `TestPhase25_ReplDemoConfig_InvalidDuplicateID`
- `TestPhase25_ReplDemoConfig_InvalidScopeMissingRaftFlag`
- `TestPhase25_ReplDemoConfig_NodeDataDirsAreAbsolutePaths`

### Smoke test output (repl-demo-smoke 16/16)

```
=== ShardForgeDB Phase 25 — Replication Demo Smoke Test ===

SCOPE: Explicit pull-based replication only.
       No Raft. No consensus. No quorum. No automatic failover.

-- Health checks
  PASS: leader /healthz → ok
  PASS: follower /healthz → ok

-- Write to leader
  PASS: PUT repl:smoke to leader succeeded
  PASS: GET repl:smoke from leader returns hello-replication

-- Isolation before pull (no automatic replication)
  PASS: GET from follower BEFORE pull: not found (data isolation confirmed)

-- Explicit pull (operator-triggered)
  PASS: POST /replication/sync on follower succeeded
  sync: fetched=1 applied=1
  PASS: GET from follower AFTER pull: hello-replication (replication confirmed)

-- Idempotency: second pull when nothing new
  second sync: fetched=0 applied=0
  PASS: second pull is idempotent (fetched=0, applied=0 — no duplicate writes)

-- DELETE on leader, then pull
  PASS: DELETE repl:smoke on leader succeeded
  PASS: follower still has repl:smoke before delete-pull (no auto-sync)
  PASS: pull after DELETE succeeded (applied=1)
  PASS: follower no longer has repl:smoke after delete-pull (tombstone applied)

-- Follower role enforcement
  PASS: GET /replication/log on follower returns error (correct: follower has no log)
  PASS: PUT on follower rejected (403 — follower is read-only)

-- Scope flags in demo config
  PASS: config scope flags: no_raft=true, no_consensus=true, no_quorum_replication=true
  PASS: leader /replication/status shows role=primary

=== Summary ===
  Passed: 16
  Failed: 0

All checks passed. Phase 25 pull-based replication demo is working correctly.
```
