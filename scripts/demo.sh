#!/usr/bin/env bash
# scripts/demo.sh — recruiter/demo-friendly sequence for ShardForgeDB
#
# Usage:
#   ./scripts/demo.sh              # build + version + small bench + print dashboard command
#   ./scripts/demo.sh --dashboard  # also start dashboard demo (blocks until Ctrl+C)
set -euo pipefail

cd "$(dirname "$0")/.."

START_DASHBOARD=0
for arg in "$@"; do
  case "$arg" in
    --dashboard) START_DASHBOARD=1 ;;
    *) echo "Unknown flag: $arg" >&2; exit 1 ;;
  esac
done

echo "=== ShardForgeDB Demo ==="
echo ""

echo "[demo] Building binaries..."
make build >/dev/null
echo "[demo] Built: bin/shardforge  bin/shardforge-bench  bin/shardforge-dashboard"
echo ""

echo "[demo] Version:"
./bin/shardforge version
echo ""

echo "[demo] Running small benchmark (writing to /tmp/shardforge-demo-bench.md)..."
./bin/shardforge-bench --scale small --out /tmp/shardforge-demo-bench.md >/dev/null
echo "[demo] Benchmark report written to /tmp/shardforge-demo-bench.md"
echo ""

echo "[demo] Dashboard commands (local HTTP only — no database-node networking):"
echo "  ./bin/shardforge-dashboard --demo"
echo "  ./bin/shardforge-dashboard --demo --run-chaos"
echo "  ./bin/shardforge-dashboard --addr 127.0.0.1:9090 --demo"
echo ""

if [ "$START_DASHBOARD" -eq 1 ]; then
  echo "[demo] Starting dashboard demo at http://127.0.0.1:8080/ (Ctrl+C to stop)..."
  ./bin/shardforge-dashboard --demo
else
  echo "[demo] To start the dashboard, run:"
  echo "  ./bin/shardforge-dashboard --demo --run-chaos"
  echo "  or: ./scripts/demo.sh --dashboard"
fi

echo ""
echo "=== Demo complete ==="
