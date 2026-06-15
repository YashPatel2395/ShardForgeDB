#!/usr/bin/env bash
# scripts/repl_auto_demo_up.sh — Phase 27 Automatic Background Pull Replication Demo: startup
#
# Scope: Proves automatic background pull replication between two real HTTP node processes.
#        The follower pulls from the primary automatically without POST /replication/sync.
#        NOT Raft. NOT consensus. NOT quorum. NOT automatic failover. NOT write forwarding.
#        The Phase 26 crash window remains documented.
#
# Ports:
#   leader   127.0.0.1:9501   role=primary
#   follower 127.0.0.1:9502   role=follower, bg-sync=500ms
#
# Data dirs:
#   /tmp/sfdb-auto-leader
#   /tmp/sfdb-auto-follower
#
# PID file: /tmp/sfdb-auto-pids
set -eu

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$REPO_ROOT/bin/shardforge-node"
PID_FILE="/tmp/sfdb-auto-pids"
LOG_DIR="/tmp/sfdb-auto-logs"

LEADER_ADDR="127.0.0.1:9501"
FOLLOWER_ADDR="127.0.0.1:9502"
LEADER_DIR="/tmp/sfdb-auto-leader"
FOLLOWER_DIR="/tmp/sfdb-auto-follower"

echo ""
echo "=== ShardForgeDB Phase 27 — Automatic Background Pull Replication Demo ==="
echo ""
echo "SCOPE: Two independent HTTP nodes. Leader accepts writes and maintains durable journal."
echo "       Follower polls the leader automatically every 500ms (background sync)."
echo "       NOT Raft. NOT consensus. NOT quorum. NOT automatic failover."
echo "       Crash window: engine commit → journal append (documented in Phase 26)."
echo ""

if [ ! -x "$BIN" ]; then
  echo "ERROR: $BIN not found. Run 'make build' first." >&2
  exit 1
fi

# Check for stale processes on our ports.
for PORT in 9501 9502; do
  if lsof -i ":$PORT" -sTCP:LISTEN -t >/dev/null 2>&1; then
    echo "ERROR: port $PORT is already in use. Run './scripts/repl_auto_demo_down.sh' first." >&2
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

echo "-- Starting follower (background sync every 500ms)"
"$BIN" \
  --node-id follower \
  --addr "$FOLLOWER_ADDR" \
  --data-dir "$FOLLOWER_DIR" \
  --replication-role follower \
  --primary-url "http://$LEADER_ADDR" \
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

wait_for_health "leader"   "http://$LEADER_ADDR"
wait_for_health "follower" "http://$FOLLOWER_ADDR"

echo ""
echo "Background replication demo is up. PIDs written to $PID_FILE"
echo ""
echo "SCOPE REMINDER:"
echo "  - Background sync polls every 500ms automatically"
echo "  - No manual POST /replication/sync needed"
echo "  - No Raft. No consensus. No quorum. No automatic failover."
echo "  - Follower rejects writes (PUT/DELETE return 403)"
echo "  - Phase 26 crash window: engine commit → journal append"
echo ""
echo "Quick check:"
echo "  curl http://127.0.0.1:9501/healthz"
echo "  curl http://127.0.0.1:9502/replication/status"
echo ""
echo "Run smoke test: ./scripts/repl_auto_demo_smoke.sh"
echo "Tear down:      ./scripts/repl_auto_demo_down.sh"
