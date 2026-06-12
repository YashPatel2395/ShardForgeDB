#!/usr/bin/env bash
# scripts/repl_demo_down.sh — Phase 25 Networked Pull-Based Replication Demo: teardown
#
# Stops leader and follower processes and removes demo data directories.
set -eu

PID_FILE="/tmp/sfdb-repl-pids"
LOG_DIR="/tmp/sfdb-repl-logs"

echo ""
echo "=== ShardForgeDB Phase 25 — Replication Demo Teardown ==="
echo ""

echo "-- Stopping processes"

if [ -f "$PID_FILE" ]; then
  while IFS= read -r PID; do
    if [ -n "$PID" ] && kill -0 "$PID" 2>/dev/null; then
      kill "$PID" 2>/dev/null || true
      echo "  killed pid $PID"
    fi
  done < "$PID_FILE"
  rm -f "$PID_FILE"
else
  echo "  No PID file found at $PID_FILE — nothing to stop."
fi

# Kill any stragglers by port.
for PORT in 9301 9302; do
  STALE=$(lsof -i ":$PORT" -sTCP:LISTEN -t 2>/dev/null || true)
  if [ -n "$STALE" ]; then
    kill $STALE 2>/dev/null || true
    echo "  killed stale process on port $PORT (pid $STALE)"
  fi
done

echo ""
echo "-- Removing demo data directories"

for DIR in /tmp/sfdb-repl-leader /tmp/sfdb-repl-follower; do
  if [ -d "$DIR" ]; then
    rm -rf "$DIR"
    echo "  removed $DIR"
  fi
done

echo ""
echo "-- Removing demo logs"
if [ -d "$LOG_DIR" ]; then
  rm -rf "$LOG_DIR"
  echo "  removed $LOG_DIR"
fi

echo ""
echo "Done. Replication demo is down."
