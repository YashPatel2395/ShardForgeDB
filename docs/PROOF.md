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

*Future phases will append their own sections to this document.*
