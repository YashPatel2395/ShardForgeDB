#!/usr/bin/env bash
# scripts/release_check.sh — strict final release validation for ShardForgeDB
# Runs the full test + benchmark + build + CLI gate.
# Exit code 0 = release gate passed.
#
# Note: does NOT run make bench-report to avoid modifying docs/BENCHMARKS.md.
set -euo pipefail

cd "$(dirname "$0")/.."

echo "[release-check] go mod tidy"
go mod tidy
if ! git diff --quiet; then
  echo "[release-check] FAIL: go mod tidy produced uncommitted changes" >&2
  git diff --stat >&2
  exit 1
fi

echo "[release-check] go fmt ./..."
go fmt ./...
if ! git diff --quiet; then
  echo "[release-check] FAIL: go fmt produced uncommitted changes" >&2
  git diff --stat >&2
  exit 1
fi

echo "[release-check] go vet ./..."
go vet ./...

echo "[release-check] go test -race -count=1 ./..."
go test -race -count=1 ./...

echo "[release-check] go test -bench dashboard"
go test -bench=. -benchmem -benchtime=3s ./internal/dashboard/...

echo "[release-check] go test -bench replica"
go test -bench=. -benchmem -benchtime=3s ./internal/replica/...

echo "[release-check] go test -bench shard"
go test -bench=. -benchmem -benchtime=3s ./internal/shard/...

echo "[release-check] go test -bench vector"
go test -bench=. -benchmem -benchtime=3s ./internal/vector/...

echo "[release-check] go test -bench engine"
go test -bench=. -benchmem -benchtime=3s ./internal/engine/...

echo "[release-check] go test -bench bench"
go test -bench=. -benchmem -benchtime=3s ./internal/bench/...

echo "[release-check] make test"
make test

echo "[release-check] make vet"
make vet

echo "[release-check] make build"
make build

echo "[release-check] make bench-dashboard"
make bench-dashboard

echo "[release-check] make bench-replica"
make bench-replica

echo "[release-check] make bench-shard"
make bench-shard

echo "[release-check] make bench-vector"
make bench-vector

echo "[release-check] shardforge --help"
./bin/shardforge --help >/dev/null

echo "[release-check] shardforge version"
./bin/shardforge version

echo "[release-check] shardforge-bench --scale small"
./bin/shardforge-bench --scale small --out /tmp/shardforge-release-bench.md >/dev/null
echo "[release-check] Benchmark report written to /tmp/shardforge-release-bench.md"

echo "[release-check] shardforge-dashboard --help"
./bin/shardforge-dashboard --help >/dev/null

echo "[release-check] git status --short"
STATUS=$(git status --short)
if [ -n "$STATUS" ]; then
  echo "[release-check] FAIL: working tree is not clean" >&2
  echo "$STATUS" >&2
  exit 1
fi
echo "[release-check] Working tree is clean."

echo ""
echo "[release-check] ALL CHECKS PASSED"
