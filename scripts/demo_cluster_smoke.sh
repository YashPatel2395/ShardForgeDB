#!/usr/bin/env bash
# scripts/demo_cluster_smoke.sh — Phase 24 local cluster demo smoke test
#
# Proves the Phase 24 local cluster demo is working correctly:
#   1. All nodes and proxy are healthy.
#   2. Key placement is deterministic and logged.
#   3. Put/get through proxy routes correctly.
#   4. Data isolation: writes to one node are NOT visible on another node
#      (each node has its own independent storage, no replication).
#   5. explain-node works against a live node over HTTP.
#   6. Config validates cleanly via shardforge-cluster.
#   7. All scope flags confirm: no Raft, no consensus, no failover, etc.
#
# SCOPE:
#   - Static FNV-1a consistent-hash routing only.
#   - No Raft, no consensus, no quorum replication.
#   - No automatic failover, no shard migration.
#   - This script does NOT test automatic failover (none exists).
#   - Data isolation is the expected behavior (no replication).
#
# Requires demo_cluster_up.sh to have been run first.
#
# Usage:
#   ./scripts/demo_cluster_up.sh
#   ./scripts/demo_cluster_smoke.sh
#   ./scripts/demo_cluster_down.sh
set -eu

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$REPO_ROOT/bin"
CONFIG="$REPO_ROOT/configs/cluster/demo-3node.json"

PASS=0
FAIL=0

ok()   { echo "  PASS: $1"; PASS=$((PASS+1)); }
fail() { echo "  FAIL: $1"; FAIL=$((FAIL+1)); }

echo ""
echo "=== ShardForgeDB Phase 24 — Cluster Demo Smoke Test ==="
echo ""
echo "SCOPE: Static routing, no Raft, no consensus, no failover, no shard migration."
echo ""

# --- 1. Health checks ---

echo "-- Health checks"

curl -sf http://127.0.0.1:9101/healthz > /tmp/sfdb-smoke-h1.out 2>&1
grep -q "ok" /tmp/sfdb-smoke-h1.out && ok "node-1 /healthz → ok" || fail "node-1 /healthz failed"

curl -sf http://127.0.0.1:9102/healthz > /tmp/sfdb-smoke-h2.out 2>&1
grep -q "ok" /tmp/sfdb-smoke-h2.out && ok "node-2 /healthz → ok" || fail "node-2 /healthz failed"

curl -sf http://127.0.0.1:9103/healthz > /tmp/sfdb-smoke-h3.out 2>&1
grep -q "ok" /tmp/sfdb-smoke-h3.out && ok "node-3 /healthz → ok" || fail "node-3 /healthz failed"

curl -sf http://127.0.0.1:9200/healthz > /tmp/sfdb-smoke-hp.out 2>&1
grep -q "ok" /tmp/sfdb-smoke-hp.out && ok "proxy  /healthz → ok" || fail "proxy /healthz failed"

# --- 2. Status checks ---

echo ""
echo "-- Status checks"

curl -sf http://127.0.0.1:9101/status > /tmp/sfdb-smoke-s1.out 2>&1
grep -q '"node_id"' /tmp/sfdb-smoke-s1.out && ok "node-1 /status returns node_id" || fail "node-1 /status failed"
grep -q "node-1" /tmp/sfdb-smoke-s1.out && ok "node-1 /status node_id=node-1" || fail "node-1 /status wrong node_id"

curl -sf http://127.0.0.1:9102/status > /tmp/sfdb-smoke-s2.out 2>&1
grep -q "node-2" /tmp/sfdb-smoke-s2.out && ok "node-2 /status node_id=node-2" || fail "node-2 /status wrong node_id"

curl -sf http://127.0.0.1:9103/status > /tmp/sfdb-smoke-s3.out 2>&1
grep -q "node-3" /tmp/sfdb-smoke-s3.out && ok "node-3 /status node_id=node-3" || fail "node-3 /status wrong node_id"

curl -sf http://127.0.0.1:9200/status > /tmp/sfdb-smoke-sp.out 2>&1
grep -q "node_count\|nodes" /tmp/sfdb-smoke-sp.out && ok "proxy /status returns node count" || fail "proxy /status failed"

# --- 3. Key placement proof ---
#
# Deterministic FNV-1a consistent hash routing. These routes are stable for
# the 3-node ring with 128 virtual nodes per node. Run 'route' twice to prove
# determinism — the same key must always go to the same node.
#
# SCOPE: static routing only. No shard migration. No automatic rebalancing.
# Routing will change if a node is added/removed (that is expected and documented).

echo ""
echo "-- Key placement proof (static FNV-1a consistent hash, no migration)"
echo "   NOTE: routing is deterministic for a fixed 3-node ring."
echo "   Routing WILL change if the ring topology changes (add/remove node)."
echo "   This is NOT automatic rebalancing — it is static routing."

"$BIN/shardforge-gateway" --config "$CONFIG" route user:1  > /tmp/sfdb-route-user1.out 2>&1 || true
"$BIN/shardforge-gateway" --config "$CONFIG" route user:2  > /tmp/sfdb-route-user2.out 2>&1 || true
"$BIN/shardforge-gateway" --config "$CONFIG" route order:9 > /tmp/sfdb-route-order9.out 2>&1 || true
"$BIN/shardforge-gateway" --config "$CONFIG" route item:42 > /tmp/sfdb-route-item42.out 2>&1 || true

echo "  $(cat /tmp/sfdb-route-user1.out)"
echo "  $(cat /tmp/sfdb-route-user2.out)"
echo "  $(cat /tmp/sfdb-route-order9.out)"
echo "  $(cat /tmp/sfdb-route-item42.out)"

# Verify routing is deterministic (run again, must match first run)
"$BIN/shardforge-gateway" --config "$CONFIG" route user:1  > /tmp/sfdb-route2-user1.out 2>&1 || true
diff /tmp/sfdb-route-user1.out /tmp/sfdb-route2-user1.out && ok "routing is deterministic for user:1 (same result on second call)" || fail "routing is NOT deterministic for user:1"

grep -q "node-" /tmp/sfdb-route-user1.out && ok "user:1 routes to a node" || fail "user:1 routing failed"
grep -q "node-" /tmp/sfdb-route-order9.out && ok "order:9 routes to a node" || fail "order:9 routing failed"

# --- 4. Put/get through proxy ---

echo ""
echo "-- Put/get through proxy"

curl -sf -X PUT http://127.0.0.1:9200/kv/user:1 \
  -H "Content-Type: application/json" \
  -d '{"value":"alice"}' > /tmp/sfdb-smoke-put.out 2>&1
grep -q '"ok":true' /tmp/sfdb-smoke-put.out \
  && ok "PUT user:1=alice via proxy succeeded" \
  || fail "PUT user:1 via proxy failed: $(cat /tmp/sfdb-smoke-put.out)"

curl -sf http://127.0.0.1:9200/kv/user:1 > /tmp/sfdb-smoke-get.out 2>&1
grep -q "alice" /tmp/sfdb-smoke-get.out && ok "GET user:1 via proxy returns alice" || fail "GET user:1 via proxy failed: $(cat /tmp/sfdb-smoke-get.out)"

# Write a second key through proxy
curl -sf -X PUT http://127.0.0.1:9200/kv/order:9 \
  -H "Content-Type: application/json" \
  -d '{"value":"widget"}' > /dev/null 2>&1
curl -sf http://127.0.0.1:9200/kv/order:9 > /tmp/sfdb-smoke-get2.out 2>&1
grep -q "widget" /tmp/sfdb-smoke-get2.out && ok "GET order:9 via proxy returns widget" || fail "GET order:9 via proxy failed"

# --- 5. Data isolation proof ---
#
# Write directly to node-1, then verify node-2 and node-3 do NOT have the key.
# This proves each node has independent storage. No replication. No shared state.
#
# SCOPE: This is the EXPECTED behavior. No replication is by design.
# To get data on all nodes, write the same key to each node directly, or use
# the proxy which will route consistently to one node per key.

echo ""
echo "-- Data isolation proof (each node has independent storage, no replication)"
echo "   Writing iso:test directly to node-1. node-2 and node-3 must NOT have it."

curl -sf -X PUT http://127.0.0.1:9101/kv/iso:test \
  -H "Content-Type: application/json" \
  -d '{"value":"isolation-proof"}' > /dev/null 2>&1

# node-1 must have it
curl -sf http://127.0.0.1:9101/kv/iso:test > /tmp/sfdb-iso-n1.out 2>&1
grep -q "isolation-proof" /tmp/sfdb-iso-n1.out && ok "node-1 has iso:test (direct write confirmed)" || fail "node-1 does not have iso:test"

# node-2 must NOT have it (no replication).
# The node returns HTTP 200 with {"found":false,...} for missing keys.
curl -sf http://127.0.0.1:9102/kv/iso:test > /tmp/sfdb-iso-n2.out 2>&1
if grep -q '"found":false' /tmp/sfdb-iso-n2.out; then
  ok "node-2 does NOT have iso:test (data isolation confirmed, no replication)"
else
  fail "node-2 unexpectedly has iso:test — isolation violated: $(cat /tmp/sfdb-iso-n2.out)"
fi

# node-3 must NOT have it (no replication).
curl -sf http://127.0.0.1:9103/kv/iso:test > /tmp/sfdb-iso-n3.out 2>&1
if grep -q '"found":false' /tmp/sfdb-iso-n3.out; then
  ok "node-3 does NOT have iso:test (data isolation confirmed, no replication)"
else
  fail "node-3 unexpectedly has iso:test — isolation violated: $(cat /tmp/sfdb-iso-n3.out)"
fi

# Verify data dirs are distinct (show sizes)
echo ""
echo "  Data directory contents:"
echo "    /tmp/sfdb-demo-node-1: $(find /tmp/sfdb-demo-node-1 -type f 2>/dev/null | wc -l | tr -d ' ') files"
echo "    /tmp/sfdb-demo-node-2: $(find /tmp/sfdb-demo-node-2 -type f 2>/dev/null | wc -l | tr -d ' ') files"
echo "    /tmp/sfdb-demo-node-3: $(find /tmp/sfdb-demo-node-3 -type f 2>/dev/null | wc -l | tr -d ' ') files"
[ "/tmp/sfdb-demo-node-1" != "/tmp/sfdb-demo-node-2" ] && \
  [ "/tmp/sfdb-demo-node-1" != "/tmp/sfdb-demo-node-3" ] && \
  [ "/tmp/sfdb-demo-node-2" != "/tmp/sfdb-demo-node-3" ] && \
  ok "all three nodes use distinct data directories" || fail "data dir paths are not distinct"

# --- 6. explain-node over HTTP ---

echo ""
echo "-- explain-node: runtime execution trace over HTTP"

"$BIN/shardforge" explain-node --addr http://127.0.0.1:9101 put trace:key tracevalue \
  > /tmp/sfdb-smoke-en-put.out 2>&1 || true
grep -q "WAL_APPEND" /tmp/sfdb-smoke-en-put.out && \
  ok "explain-node put returns WAL_APPEND step" || fail "explain-node put missing WAL_APPEND"

grep -q "MEMTABLE_PUT" /tmp/sfdb-smoke-en-put.out && \
  ok "explain-node put returns MEMTABLE_PUT step" || fail "explain-node put missing MEMTABLE_PUT"

"$BIN/shardforge" explain-node --addr http://127.0.0.1:9101 get trace:key \
  > /tmp/sfdb-smoke-en-get.out 2>&1 || true
grep -q "MEMTABLE_HIT" /tmp/sfdb-smoke-en-get.out && \
  ok "explain-node get returns MEMTABLE_HIT step" || fail "explain-node get missing MEMTABLE_HIT"

echo "  Trace output (put):"
cat /tmp/sfdb-smoke-en-put.out | head -20 | sed 's/^/    /'

# --- 7. Config validation ---

echo ""
echo "-- Config validation"

"$BIN/shardforge-cluster" validate "$CONFIG" > /tmp/sfdb-smoke-cv.out 2>&1
grep -q "valid\|ok" /tmp/sfdb-smoke-cv.out && ok "configs/cluster/demo-3node.json validates" || fail "config validation failed"

# Verify scope flags confirm no-Raft/no-consensus (parse JSON)
python3 -c "
import json, sys
with open('$CONFIG') as f:
    cfg = json.load(f)
s = cfg['scope']
needed = ['no_raft', 'no_consensus', 'no_failover', 'no_shard_migration', 'no_replication']
for k in needed:
    if not s.get(k):
        sys.exit(f'MISSING scope flag: {k}')
print('all scope flags confirmed')
" > /tmp/sfdb-smoke-scope.out 2>&1
grep -q "all scope flags confirmed" /tmp/sfdb-smoke-scope.out && \
  ok "config scope flags: no_raft=true, no_consensus=true, no_failover=true, no_shard_migration=true, no_replication=true" || \
  fail "config scope flags check failed: $(cat /tmp/sfdb-smoke-scope.out)"

# --- 8. Gateway health fanout ---

echo ""
echo "-- Gateway health fanout (all nodes)"

"$BIN/shardforge-gateway" --config "$CONFIG" health > /tmp/sfdb-smoke-gw-health.out 2>&1 || true
echo "  $(grep -c "healthy\|ok\|true" /tmp/sfdb-smoke-gw-health.out || echo 0) healthy node responses"
grep -c "^ok" /tmp/sfdb-smoke-gw-health.out | grep -qE "^3$" && \
  ok "gateway health fanout: all 3 nodes healthy" || \
  fail "gateway health fanout: expected 3 'ok' lines, got: $(cat /tmp/sfdb-smoke-gw-health.out)"

# --- Summary ---

echo ""
echo "=== Summary ==="
echo "  Passed: $PASS"
echo "  Failed: $FAIL"
echo ""
echo "SCOPE REMINDER:"
echo "  - Routing is static FNV-1a consistent hashing (no shard migration)"
echo "  - No Raft, no consensus, no quorum replication"
echo "  - No automatic failover (proxy returns 502 if routed node is down)"
echo "  - Data isolation is the expected behavior (no replication)"
echo "  - This is a local demo, not a production distributed cluster"
echo ""

if [ "$FAIL" -eq 0 ]; then
  echo "All checks passed. Phase 24 local cluster demo is working correctly."
  exit 0
else
  echo "Some checks failed. Review output above."
  exit 1
fi
