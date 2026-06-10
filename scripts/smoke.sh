#!/usr/bin/env bash
# scripts/smoke.sh — fast smoke validation for ShardForgeDB
# Runs go test, go vet, make build, and CLI checks.
# Exit code 0 = all checks passed.
set -euo pipefail

cd "$(dirname "$0")/.."

echo "[smoke] go test ./..."
go test ./...

echo "[smoke] go vet ./..."
go vet ./...

echo "[smoke] make build"
make build

echo "[smoke] shardforge --help"
./bin/shardforge --help >/dev/null

echo "[smoke] shardforge version"
./bin/shardforge version

echo "[smoke] shardforge-bench --scale small"
./bin/shardforge-bench --scale small --out /tmp/shardforge-smoke-bench.md >/dev/null

echo "[smoke] shardforge-dashboard --help"
./bin/shardforge-dashboard --help >/dev/null

echo "[smoke] OK"
