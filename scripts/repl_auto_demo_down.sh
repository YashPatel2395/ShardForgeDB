#!/usr/bin/env bash
# scripts/repl_auto_demo_down.sh — Phase 27 Automatic Background Pull Replication Demo: teardown
set -eu

PID_FILE="/tmp/sfdb-auto-pids"
LEADER_DIR="/tmp/sfdb-auto-leader"
FOLLOWER_DIR="/tmp/sfdb-auto-follower"
LOG_DIR="/tmp/sfdb-auto-logs"

echo ""
echo "=== ShardForgeDB Phase 27 — Automatic Background Replication Demo: teardown ==="
echo ""

# Kill any process listening on the demo ports.
for PORT in 9501 9502; do
  PID=$(lsof -i ":$PORT" -sTCP:LISTEN -t 2>/dev/null || true)
  if [ -n "$PID" ]; then
    kill "$PID" 2>/dev/null || true
    echo "  killed pid $PID on port $PORT"
  fi
done

# Also kill from PID file if it exists.
if [ -f "$PID_FILE" ]; then
  while IFS= read -r PID; do
    if [ -n "$PID" ] && kill -0 "$PID" 2>/dev/null; then
      kill "$PID" 2>/dev/null || true
      echo "  killed pid $PID from PID file"
    fi
  done < "$PID_FILE"
  rm -f "$PID_FILE"
fi

# Remove data directories and logs.
rm -rf "$LEADER_DIR" "$FOLLOWER_DIR" "$LOG_DIR"

echo ""
echo "Automatic background replication demo torn down."
