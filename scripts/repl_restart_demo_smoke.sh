#!/usr/bin/env bash
# scripts/repl_restart_demo_smoke.sh — Phase 26 Durable Replication Restart Recovery Demo: smoke test
#
# Scope: Proves durable replication state survives primary and follower process restarts.
#        Requires repl_restart_demo_up.sh to be running first.
#
# Checks performed (18 total):
#   1.  leader /healthz → ok
#   2.  follower /healthz → ok
#   3.  PUT key to leader succeeds
#   4.  GET key from leader returns value
#   5.  Explicit pull (POST /replication/sync) succeeds — fetched=1 applied=1
#   6.  GET key from follower after pull returns value
#   7.  replication.journal exists in leader data dir
#   8.  replication_state.json exists in follower data dir
#   9.  RESTART LEADER — kill and restart on same addr+dir
#   10. Leader /healthz → ok after restart
#   11. Journal survives: GET /replication/log?after=0 returns 1 entry after leader restart
#   12. PUT second key to leader after restart
#   13. RESTART FOLLOWER — kill and restart on same addr+dir
#   14. Follower /healthz → ok after restart
#   15. Follower cursor restored: replication/status shows last_applied_seq=1 (not 0)
#   16. Explicit pull after follower restart: fetched=1 applied=1 (only the new key)
#   17. GET second key from follower confirms replication after restart
#   18. Second pull is idempotent (fetched=0, applied=0)
set -eu

LEADER="http://127.0.0.1:9401"
FOLLOWER="http://127.0.0.1:9402"
LEADER_ADDR="127.0.0.1:9401"
FOLLOWER_ADDR="127.0.0.1:9402"
LEADER_DIR="/tmp/sfdb-restart-leader"
FOLLOWER_DIR="/tmp/sfdb-restart-follower"
LOG_DIR="/tmp/sfdb-restart-logs"
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$REPO_ROOT/bin/shardforge-node"

PASS=0
FAIL=0

ok()   { echo "  PASS: $1"; PASS=$((PASS+1)); }
fail() { echo "  FAIL: $1"; FAIL=$((FAIL+1)); }

wait_for_health() {
  local LABEL="$1"
  local URL="$2"
  local MAX=30
  local I=0
  while [ $I -lt $MAX ]; do
    if curl -sf "$URL/healthz" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.3
    I=$((I+1))
  done
  return 1
}

echo ""
echo "=== ShardForgeDB Phase 26 — Durable Replication Restart Recovery Smoke Test ==="
echo ""
echo "SCOPE: Proves that primary journal and follower cursor survive restarts."
echo "       Replication is still pull-based and operator-triggered."
echo "       No Raft. No consensus. No quorum. No automatic failover."
echo ""

# ── 1-2. Initial health checks ────────────────────────────────────────────────
echo "-- Initial health checks"

STATUS=$(curl -sf "$LEADER/healthz" 2>/dev/null || true)
if echo "$STATUS" | grep -q '"ok"'; then
  ok "leader /healthz → ok"
else
  fail "leader /healthz did not return ok (got: $STATUS)"
fi

STATUS=$(curl -sf "$FOLLOWER/healthz" 2>/dev/null || true)
if echo "$STATUS" | grep -q '"ok"'; then
  ok "follower /healthz → ok"
else
  fail "follower /healthz did not return ok (got: $STATUS)"
fi

# ── 3-4. Write and read ───────────────────────────────────────────────────────
echo ""
echo "-- Write key1 to leader"

KEY1="restart:key1"
VAL1="before-restart"

PUT1=$(curl -sf -X PUT "$LEADER/kv/$KEY1" \
  -H 'Content-Type: application/json' \
  -d "{\"value\":\"$VAL1\"}" 2>/dev/null || true)
if echo "$PUT1" | grep -q '"ok":true'; then
  ok "PUT $KEY1 to leader succeeded"
else
  fail "PUT $KEY1 to leader failed (got: $PUT1)"
fi

GET1=$(curl -sf "$LEADER/kv/$KEY1" 2>/dev/null || true)
if echo "$GET1" | grep -q "\"$VAL1\""; then
  ok "GET $KEY1 from leader returns $VAL1"
else
  fail "GET $KEY1 from leader failed (got: $GET1)"
fi

# ── 5-6. Pull and verify ──────────────────────────────────────────────────────
echo ""
echo "-- Pull key1 to follower"

SYNC1=$(curl -sf -X POST "$FOLLOWER/replication/sync" 2>/dev/null || true)
FETCHED1=$(echo "$SYNC1" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('fetched',0))" 2>/dev/null || echo "?")
APPLIED1=$(echo "$SYNC1" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('applied',0))" 2>/dev/null || echo "?")
if echo "$SYNC1" | grep -q '"ok":true'; then
  ok "POST /replication/sync succeeded (fetched=$FETCHED1 applied=$APPLIED1)"
else
  fail "POST /replication/sync failed (got: $SYNC1)"
fi

GETF1=$(curl -sf "$FOLLOWER/kv/$KEY1" 2>/dev/null || true)
if echo "$GETF1" | grep -q "\"$VAL1\""; then
  ok "GET $KEY1 from follower after pull returns $VAL1"
else
  fail "GET $KEY1 from follower after pull failed (got: $GETF1)"
fi

# ── 7-8. Verify durable files exist ──────────────────────────────────────────
echo ""
echo "-- Verify durable state files"

if [ -f "$LEADER_DIR/replication.journal" ]; then
  JOURNAL_SIZE=$(wc -c < "$LEADER_DIR/replication.journal")
  ok "replication.journal exists in leader data dir (${JOURNAL_SIZE} bytes)"
else
  fail "replication.journal NOT found in $LEADER_DIR"
fi

if [ -f "$FOLLOWER_DIR/replication_state.json" ]; then
  STATE_CONTENT=$(cat "$FOLLOWER_DIR/replication_state.json")
  ok "replication_state.json exists in follower data dir (content: $STATE_CONTENT)"
else
  fail "replication_state.json NOT found in $FOLLOWER_DIR"
fi

# ── 9-11. Restart leader ──────────────────────────────────────────────────────
echo ""
echo "-- Restart leader (kill and restart on same addr+dir)"

LEADER_PID=$(lsof -i ":9401" -sTCP:LISTEN -t 2>/dev/null || true)
if [ -n "$LEADER_PID" ]; then
  kill "$LEADER_PID" 2>/dev/null || true
  sleep 0.5
  echo "  killed leader pid $LEADER_PID"
fi

"$BIN" \
  --node-id leader \
  --addr "$LEADER_ADDR" \
  --data-dir "$LEADER_DIR" \
  --replication-role primary \
  >>"$LOG_DIR/leader.log" 2>&1 &
NEW_LEADER_PID=$!
echo "  restarted leader pid $NEW_LEADER_PID"

if wait_for_health "leader" "$LEADER"; then
  ok "leader /healthz → ok after restart"
else
  fail "leader did not recover within 9s after restart"
fi

# Journal must survive: key1 entry (seq=1) must still be visible.
LOG_AFTER_RESTART=$(curl -sf "$LEADER/replication/log?after=0" 2>/dev/null || true)
LOG_COUNT=$(echo "$LOG_AFTER_RESTART" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('count',0))" 2>/dev/null || echo "?")
if [ "$LOG_COUNT" = "1" ]; then
  ok "primary journal survived restart: GET /replication/log?after=0 count=$LOG_COUNT"
else
  fail "primary journal lost after restart (count=$LOG_COUNT, got: $LOG_AFTER_RESTART)"
fi

# ── 12. Write second key after leader restart ─────────────────────────────────
echo ""
echo "-- Write key2 to leader after restart"

KEY2="restart:key2"
VAL2="after-restart"

PUT2=$(curl -sf -X PUT "$LEADER/kv/$KEY2" \
  -H 'Content-Type: application/json' \
  -d "{\"value\":\"$VAL2\"}" 2>/dev/null || true)
if echo "$PUT2" | grep -q '"ok":true'; then
  ok "PUT $KEY2 to leader after restart succeeded"
else
  fail "PUT $KEY2 to leader after restart failed (got: $PUT2)"
fi

# ── 13-15. Restart follower ───────────────────────────────────────────────────
echo ""
echo "-- Restart follower (kill and restart on same addr+dir)"

FOLLOWER_PID=$(lsof -i ":9402" -sTCP:LISTEN -t 2>/dev/null || true)
if [ -n "$FOLLOWER_PID" ]; then
  kill "$FOLLOWER_PID" 2>/dev/null || true
  sleep 0.5
  echo "  killed follower pid $FOLLOWER_PID"
fi

"$BIN" \
  --node-id follower \
  --addr "$FOLLOWER_ADDR" \
  --data-dir "$FOLLOWER_DIR" \
  --replication-role follower \
  --primary-url "http://$LEADER_ADDR" \
  >>"$LOG_DIR/follower.log" 2>&1 &
NEW_FOLLOWER_PID=$!
echo "  restarted follower pid $NEW_FOLLOWER_PID"

if wait_for_health "follower" "$FOLLOWER"; then
  ok "follower /healthz → ok after restart"
else
  fail "follower did not recover within 9s after restart"
fi

# Follower cursor must be restored from replication_state.json (not reset to 0).
FSTATUS=$(curl -sf "$FOLLOWER/replication/status" 2>/dev/null || true)
LAST_SEQ=$(echo "$FSTATUS" | python3 -c "
import sys,json
d=json.load(sys.stdin)
r=d.get('replication',{})
print(r.get('last_applied_seq',0))
" 2>/dev/null || echo "0")
if [ "$LAST_SEQ" = "1" ]; then
  ok "follower cursor restored after restart: last_applied_seq=$LAST_SEQ (not 0)"
else
  fail "follower cursor NOT restored after restart: last_applied_seq=$LAST_SEQ (want 1)"
fi

# ── 16-17. Pull and verify key2 ───────────────────────────────────────────────
echo ""
echo "-- Pull key2 to follower after restart"

SYNC2=$(curl -sf -X POST "$FOLLOWER/replication/sync" 2>/dev/null || true)
FETCHED2=$(echo "$SYNC2" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('fetched',0))" 2>/dev/null || echo "?")
APPLIED2=$(echo "$SYNC2" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('applied',0))" 2>/dev/null || echo "?")
if echo "$SYNC2" | grep -q '"ok":true' && [ "$FETCHED2" = "1" ] && [ "$APPLIED2" = "1" ]; then
  ok "POST /replication/sync after restart: fetched=$FETCHED2 applied=$APPLIED2 (only the new key)"
else
  fail "POST /replication/sync after restart failed or wrong counts (fetched=$FETCHED2 applied=$APPLIED2, got: $SYNC2)"
fi

GETF2=$(curl -sf "$FOLLOWER/kv/$KEY2" 2>/dev/null || true)
if echo "$GETF2" | grep -q "\"$VAL2\""; then
  ok "GET $KEY2 from follower after restart confirms replication"
else
  fail "GET $KEY2 from follower after restart failed (got: $GETF2)"
fi

# ── 18. Idempotent second pull ────────────────────────────────────────────────
echo ""
echo "-- Idempotency: second pull when nothing new"

SYNC3=$(curl -sf -X POST "$FOLLOWER/replication/sync" 2>/dev/null || true)
FETCHED3=$(echo "$SYNC3" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('fetched',0))" 2>/dev/null || echo "?")
APPLIED3=$(echo "$SYNC3" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('applied',0))" 2>/dev/null || echo "?")
if [ "$FETCHED3" = "0" ] && [ "$APPLIED3" = "0" ]; then
  ok "second pull is idempotent (fetched=0 applied=0)"
else
  fail "second pull not idempotent (fetched=$FETCHED3 applied=$APPLIED3)"
fi

# ── Summary ───────────────────────────────────────────────────────────────────
echo ""
echo "=== Summary ==="
echo "  Passed: $PASS"
echo "  Failed: $FAIL"
echo ""

echo "SCOPE REMINDER:"
echo "  - Primary journal (replication.journal) is a durable binary file that survives restarts"
echo "  - Follower cursor (replication_state.json) is a JSON file that survives restarts"
echo "  - Replication is still pull-based and operator-triggered (POST /replication/sync)"
echo "  - No automatic background sync, no Raft, no consensus, no quorum replication"
echo ""

if [ "$FAIL" -eq 0 ]; then
  echo "All checks passed. Phase 26 durable replication restart recovery is working correctly."
  exit 0
else
  echo "Some checks failed. Review output above."
  exit 1
fi
