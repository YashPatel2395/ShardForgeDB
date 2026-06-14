#!/usr/bin/env bash
# scripts/repl_restart_demo_up.sh — Phase 26 Durable Replication Restart Recovery Demo: start
#
# Scope: Explicit pull-based replication between two real HTTP node processes.
#        Primary maintains a durable binary journal (replication.journal).
#        Follower persists its replication cursor (replication_state.json).
#        Both survive process restarts. Replication is still operator-triggered only.
#
#        NOT Raft. NOT consensus. NOT quorum. NOT automatic failover.
#        NOT automatic background sync. Replication is operator-triggered via POST /replication/sync.
#
# Ports:
#   leader   127.0.0.1:9401   role=primary
#   follower 127.0.0.1:9402   role=follower, primary-url=http://127.0.0.1:9401
#
# Data dirs:
#   /tmp/sfdb-restart-leader
#   /tmp/sfdb-restart-follower
#
# PID file: /tmp/sfdb-restart-pids
set -eu

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$REPO_ROOT/bin/shardforge-node"
PID_FILE="/tmp/sfdb-restart-pids"
LOG_DIR="/tmp/sfdb-restart-logs"

LEADER_ADDR="127.0.0.1:9401"
FOLLOWER_ADDR="127.0.0.1:9402"
LEADER_DIR="/tmp/sfdb-restart-leader"
FOLLOWER_DIR="/tmp/sfdb-restart-follower"

echo ""
echo "=== ShardForgeDB Phase 26 — Durable Replication Restart Recovery Demo ==="
echo ""
echo "SCOPE: Two independent HTTP nodes with DURABLE replication state."
echo "       Primary: binary journal survives restarts (replication.journal)."
echo "       Follower: replication cursor survives restarts (replication_state.json)."
echo "       Replication is still PULL-BASED and OPERATOR-TRIGGERED only."
echo "       NOT Raft. NOT consensus. NOT quorum. NOT automatic failover."
echo ""

if [ ! -x "$BIN" ]; then
  echo "ERROR: $BIN not found. Run 'make build' first." >&2
  exit 1
fi

# Check for stale processes on our ports.
for PORT in 9401 9402; do
  if lsof -i ":$PORT" -sTCP:LISTEN -t >/dev/null 2>&1; then
    echo "ERROR: port $PORT is already in use. Run './scripts/repl_restart_demo_down.sh' first." >&2
    exit 1
  fi
done

mkdir -p "$LOG_DIR" "$LEADER_DIR" "$FOLLOWER_DIR"
rm -f "$PID_FILE"

echo "-- Starting leader (primary)"
"$BIN" \
  --node-id leader \
  --addr "$LEADER_ADDR" \
  --data-dir "$LEADER_DIR" \
  --replication-role primary \
  >"$LOG_DIR/leader.log" 2>&1 &
LEADER_PID=$!
echo "$LEADER_PID" >> "$PID_FILE"
echo "  leader   $LEADER_ADDR  data=$LEADER_DIR  pid=$LEADER_PID"

echo "-- Starting follower"
"$BIN" \
  --node-id follower \
  --addr "$FOLLOWER_ADDR" \
  --data-dir "$FOLLOWER_DIR" \
  --replication-role follower \
  --primary-url "http://$LEADER_ADDR" \
  >"$LOG_DIR/follower.log" 2>&1 &
FOLLOWER_PID=$!
echo "$FOLLOWER_PID" >> "$PID_FILE"
echo "  follower $FOLLOWER_ADDR  data=$FOLLOWER_DIR  pid=$FOLLOWER_PID"

echo ""
echo "-- Waiting for health checks"

wait_for_health() {
  local LABEL="$1"
  local URL="$2"
  local MAX=20
  local I=0
  while [ $I -lt $MAX ]; do
    if curl -sf "$URL/healthz" >/dev/null 2>&1; then
      echo "  OK: $LABEL"
      return 0
    fi
    sleep 0.3
    I=$((I+1))
  done
  echo "  FAILED: $LABEL did not become healthy within 6s" >&2
  echo "  Logs: $LOG_DIR/${LABEL}.log" >&2
  cat "$LOG_DIR/${LABEL}.log" >&2 || true
  return 1
}

wait_for_health "leader"   "http://$LEADER_ADDR"
wait_for_health "follower" "http://$FOLLOWER_ADDR"

echo ""
echo "Restart recovery demo is up. PIDs written to $PID_FILE"
echo ""
echo "SCOPE REMINDER:"
echo "  - Primary journal: $LEADER_DIR/replication.journal (binary, survives restart)"
echo "  - Follower cursor: $FOLLOWER_DIR/replication_state.json (JSON, survives restart)"
echo "  - Replication is PULL-BASED and OPERATOR-TRIGGERED only"
echo "  - No background sync loop. No Raft. No consensus. No quorum."
echo ""
echo "Quick check:"
echo "  curl http://127.0.0.1:9401/healthz"
echo "  curl http://127.0.0.1:9402/replication/status"
echo ""
echo "Run smoke test: ./scripts/repl_restart_demo_smoke.sh"
echo "Tear down:      ./scripts/repl_restart_demo_down.sh"
