#!/usr/bin/env bash
# scripts/demo_cluster_down.sh — Phase 24 local cluster demo teardown
#
# Stops all processes started by demo_cluster_up.sh and removes data directories.
#
# Usage:
#   ./scripts/demo_cluster_down.sh
set -eu

PID_FILE="/tmp/sfdb-demo-pids"

echo ""
echo "=== ShardForgeDB Phase 24 — Cluster Demo Teardown ==="
echo ""

# --- Stop processes ---

if [ -f "$PID_FILE" ]; then
  echo "-- Stopping processes"
  while IFS= read -r pid; do
    if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
      kill "$pid" 2>/dev/null || true
      echo "  killed pid $pid"
    fi
  done < "$PID_FILE"
  rm -f "$PID_FILE"
else
  echo "  No PID file found at $PID_FILE — nothing to stop."
fi

# Also kill any stragglers on our ports (belt-and-suspenders)
for port in 9101 9102 9103 9200; do
  pid=$(lsof -i ":$port" -sTCP:LISTEN -t 2>/dev/null || true)
  if [ -n "$pid" ]; then
    kill "$pid" 2>/dev/null || true
    echo "  killed straggler on port $port (pid $pid)"
  fi
done

echo ""
echo "-- Removing demo data directories"

for dir in /tmp/sfdb-demo-node-1 /tmp/sfdb-demo-node-2 /tmp/sfdb-demo-node-3; do
  if [ -d "$dir" ]; then
    rm -rf "$dir"
    echo "  removed $dir"
  fi
done

echo ""
echo "-- Removing demo logs"

LOG_DIR="/tmp/sfdb-demo-logs"
if [ -d "$LOG_DIR" ]; then
  rm -rf "$LOG_DIR"
  echo "  removed $LOG_DIR"
fi

echo ""
echo "Done. Cluster is down."
