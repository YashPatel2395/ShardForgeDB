#!/usr/bin/env bash
# scripts/demo_cluster_up.sh — Phase 24 local cluster demo startup
#
# Starts 3 independent shardforge-node processes + 1 stateless routing proxy
# as local background processes. Uses separate data directories under /tmp.
#
# This is NOT a distributed cluster:
#   - No Raft, no consensus, no quorum replication.
#   - No automatic failover, no shard migration.
#   - Each node is independent. Routing is static FNV-1a consistent hashing.
#   - The proxy routes requests to nodes; it does not replicate or coordinate.
#
# Usage:
#   ./scripts/demo_cluster_up.sh
#   ./scripts/demo_cluster_smoke.sh
#   ./scripts/demo_cluster_down.sh
#
# PIDs are written to /tmp/sfdb-demo-pids so demo_cluster_down.sh can stop them.
set -eu

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$REPO_ROOT/bin"
CONFIG="$REPO_ROOT/configs/cluster/demo-3node.json"
PID_FILE="/tmp/sfdb-demo-pids"
LOG_DIR="/tmp/sfdb-demo-logs"

DATA_DIR_1="/tmp/sfdb-demo-node-1"
DATA_DIR_2="/tmp/sfdb-demo-node-2"
DATA_DIR_3="/tmp/sfdb-demo-node-3"

echo ""
echo "=== ShardForgeDB Phase 24 — Local Cluster Demo ==="
echo ""
echo "SCOPE: Three independent HTTP nodes + stateless proxy."
echo "       No Raft. No consensus. No automatic failover."
echo "       No replication. No shard migration. Static routing only."
echo ""

# --- Preflight checks ---

if [ ! -x "$BIN/shardforge-node" ]; then
  echo "ERROR: $BIN/shardforge-node not found. Run 'make build' first."
  exit 1
fi
if [ ! -x "$BIN/shardforge-proxy" ]; then
  echo "ERROR: $BIN/shardforge-proxy not found. Run 'make build' first."
  exit 1
fi
if [ ! -f "$CONFIG" ]; then
  echo "ERROR: $CONFIG not found."
  exit 1
fi

# Check for stale processes on our ports
for port in 9101 9102 9103 9200; do
  if lsof -i ":$port" -sTCP:LISTEN -t >/dev/null 2>&1; then
    echo "WARNING: Port $port is already in use. Run ./scripts/demo_cluster_down.sh first."
    exit 1
  fi
done

# --- Setup ---

mkdir -p "$DATA_DIR_1" "$DATA_DIR_2" "$DATA_DIR_3" "$LOG_DIR"
: > "$PID_FILE"  # truncate

echo "-- Starting nodes"

"$BIN/shardforge-node" \
  --node-id node-1 \
  --addr 127.0.0.1:9101 \
  --data-dir "$DATA_DIR_1" \
  > "$LOG_DIR/node-1.log" 2>&1 &
echo $! >> "$PID_FILE"
echo "  node-1  127.0.0.1:9101  data=$DATA_DIR_1  pid=$!"

"$BIN/shardforge-node" \
  --node-id node-2 \
  --addr 127.0.0.1:9102 \
  --data-dir "$DATA_DIR_2" \
  > "$LOG_DIR/node-2.log" 2>&1 &
echo $! >> "$PID_FILE"
echo "  node-2  127.0.0.1:9102  data=$DATA_DIR_2  pid=$!"

"$BIN/shardforge-node" \
  --node-id node-3 \
  --addr 127.0.0.1:9103 \
  --data-dir "$DATA_DIR_3" \
  > "$LOG_DIR/node-3.log" 2>&1 &
echo $! >> "$PID_FILE"
echo "  node-3  127.0.0.1:9103  data=$DATA_DIR_3  pid=$!"

echo ""
echo "-- Starting proxy"

"$BIN/shardforge-proxy" \
  --config "$CONFIG" \
  > "$LOG_DIR/proxy.log" 2>&1 &
echo $! >> "$PID_FILE"
echo "  proxy   127.0.0.1:9200  config=$CONFIG  pid=$!"

echo ""
echo "-- Waiting for health checks"

wait_for_health() {
  local url="$1"
  local label="$2"
  local max=20
  local i=0
  while [ $i -lt $max ]; do
    if curl -sf "$url" >/dev/null 2>&1; then
      echo "  OK: $label"
      return 0
    fi
    sleep 0.3
    i=$((i+1))
  done
  echo "  TIMEOUT: $label did not become healthy within $((max*300))ms"
  return 1
}

wait_for_health "http://127.0.0.1:9101/healthz" "node-1"
wait_for_health "http://127.0.0.1:9102/healthz" "node-2"
wait_for_health "http://127.0.0.1:9103/healthz" "node-3"
wait_for_health "http://127.0.0.1:9200/healthz" "proxy"

echo ""
echo "Cluster is up. PIDs written to $PID_FILE"
echo ""
echo "Quick check:"
echo "  curl http://127.0.0.1:9101/healthz"
echo "  curl http://127.0.0.1:9200/status"
echo "  curl http://127.0.0.1:9200/route/user:1"
echo ""
echo "Run smoke test: ./scripts/demo_cluster_smoke.sh"
echo "Tear down:      ./scripts/demo_cluster_down.sh"
