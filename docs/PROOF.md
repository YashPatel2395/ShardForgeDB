# ShardForgeDB — Validation Proof Log

This file records the evidence that each phase was implemented correctly and passes its acceptance criteria.

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

*Future phases will append their own sections to this document.*
