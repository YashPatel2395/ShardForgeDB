#!/usr/bin/env bash
# scripts/repl_failover_demo_up.sh — Phase 28 Manual Promotion and Controlled Failover Demo: startup
#
# Scope: Proves manual promotion workflow between two real HTTP node processes.
#        The operator explicitly quiesces the primary, verifies follower sync,
#        stops the primary, and promotes the follower.
#        NOT Raft. NOT consensus. NOT quorum. NOT automatic failover.
#
# Ports:
#   primary  127.0.0.1:9601   role=primary
#   follower 127.0.0.1:9602   role=follower, bg-sync=500ms
#
# Data dirs:
#   /tmp/sfdb-failover-primary
#   /tmp/sfdb-failover-follower
#
# PID file: /tmp/sfdb-failover-pids
set -eu

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$REPO_ROOT/bin/shardforge-node"
PID_FILE="/tmp/sfdb-failover-pids"
LOG_DIR="/tmp/sfdb-failover-logs"

PRIMARY_ADDR="127.0.0.1:9601"
FOLLOWER_ADDR="127.0.0.1:9602"
PRIMARY_DIR="/tmp/sfdb-failover-primary"
FOLLOWER_DIR="/tmp/sfdb-failover-follower"

echo ""
echo "=== ShardForgeDB Phase 28 — Manual Promotion and Controlled Failover Demo ==="
echo ""
echo "SCOPE: Two independent HTTP nodes. Primary accepts writes and maintains durable journal."
echo "       Follower polls the primary automatically every 500ms (background sync)."
echo "       Promotion is strictly operator-controlled (no automatic failover)."
echo "       NOT Raft. NOT consensus. NOT quorum. NOT automatic failover."
echo ""

if [ ! -x "$BIN" ]; then
  echo "ERROR: $BIN not found. Run 'make build' first." >&2
  exit 1
fi

# Check for stale processes on our ports.
for PORT in 9601 9602; do
  if lsof -i ":$PORT" -sTCP:LISTEN -t >/dev/null 2>&1; then
    echo "ERROR: port $PORT is already in use. Run './scripts/repl_failover_demo_down.sh' first." >&2
    exit 1
  fi
done

mkdir -p "$LOG_DIR" "$PRIMARY_DIR" "$FOLLOWER_DIR"
rm -f "$PID_FILE"

echo "-- Starting primary"
"$BIN" \
  --node-id primary-1 \
  --addr "$PRIMARY_ADDR" \
  --data-dir "$PRIMARY_DIR" \
  --replication-role primary \
  >"$LOG_DIR/primary.log" 2>&1 &
PRIMARY_PID=$!
echo "$PRIMARY_PID" >> "$PID_FILE"
echo "  primary  $PRIMARY_ADDR  data=$PRIMARY_DIR  pid=$PRIMARY_PID"

echo "-- Starting follower (background sync every 500ms)"
"$BIN" \
  --node-id follower-1 \
  --addr "$FOLLOWER_ADDR" \
  --data-dir "$FOLLOWER_DIR" \
  --replication-role follower \
  --primary-url "http://$PRIMARY_ADDR" \
  --bg-sync \
  --bg-sync-interval 500ms \
  --bg-sync-request-timeout 2s \
  --bg-sync-initial-backoff 250ms \
  --bg-sync-max-backoff 5s \
  --bg-sync-jitter-fraction 0.10 \
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
  cat "$LOG_DIR/${LABEL}.log" >&2 || true
  return 1
}

wait_for_health "primary"  "http://$PRIMARY_ADDR"
wait_for_health "follower" "http://$FOLLOWER_ADDR"

echo ""
echo "Manual promotion demo is up. PIDs written to $PID_FILE"
echo ""
echo "Run smoke test: ./scripts/repl_failover_demo_smoke.sh"
echo "Tear down:      ./scripts/repl_failover_demo_down.sh"
