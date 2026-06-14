#!/usr/bin/env bash
# scripts/repl_auto_demo_smoke.sh — Phase 27 Automatic Background Pull Replication: smoke test
#
# Scope: Proves that automatic background pull replication works without manual sync.
#        Requires repl_auto_demo_up.sh to have been run first.
#
# Checks performed (24 total):
#   1.  leader /healthz → ok
#   2.  follower /healthz → ok
#   3.  follower background_sync.enabled = true
#   4.  follower background_sync.state = running
#   5.  PUT key1 to leader
#   6.  Poll follower until key1 appears (NO /replication/sync call)
#   7.  Follower replication status shows last_applied_seq > 0
#   8.  Lag is known (lag_known=true) and lag_entries=0
#   9.  Write 5 more keys to leader — all 5 propagate automatically
#   10. DELETE key1 on leader — propagates automatically
#   11. Follower enters backing_off state after primary stopped
#   12. total_sync_failures > 0 while primary is stopped
#   13. next_retry_at is set while follower is backing_off
#   14. Follower lag_known becomes false (primary unreachable = stale lag)
#   15. Leader /healthz → ok after restart
#   16. Follower background sync recovers automatically (lag_known=true)
#   17. current_backoff_ms=0 after recovery (backoff fully reset)
#   18. next_retry_at=null after recovery
#   19. background_sync.state=running after recovery
#   20. Write key_after_restart to leader — propagates automatically
#   21. Restart follower (same addr+dir) — prove durable cursor survives
#   22. Follower /healthz → ok after restart
#   23. Follower last_applied_seq > 0 immediately after restart (cursor restored from state file)
#   24. Follower receives key_after_follower_restart automatically (no manual sync)
set -eu

LEADER="http://127.0.0.1:9501"
FOLLOWER="http://127.0.0.1:9502"
LEADER_ADDR="127.0.0.1:9501"
FOLLOWER_ADDR="127.0.0.1:9502"
LEADER_DIR="/tmp/sfdb-auto-leader"
FOLLOWER_DIR="/tmp/sfdb-auto-follower"
LOG_DIR="/tmp/sfdb-auto-logs"
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$REPO_ROOT/bin/shardforge-node"

PASS=0
FAIL=0

ok()   { echo "  PASS: $1"; PASS=$((PASS+1)); }
fail() { echo "  FAIL: $1"; FAIL=$((FAIL+1)); }

# Poll URL until expected value appears in output (up to MAX iterations × SLEEP).
poll_until() {
  local LABEL="$1"
  local URL="$2"
  local EXPECT="$3"
  local MAX="${4:-40}"
  local SLEEP="${5:-0.3}"
  local I=0
  while [ $I -lt $MAX ]; do
    RESP=$(curl -sf "$URL" 2>/dev/null || true)
    if echo "$RESP" | grep -q "$EXPECT" 2>/dev/null; then
      return 0
    fi
    sleep "$SLEEP"
    I=$((I+1))
  done
  return 1
}

wait_for_health() {
  local LABEL="$1"
  local URL="$2"
  local MAX=40
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

json_field() {
  python3 -c "import sys,json; d=json.load(sys.stdin); ${1}" 2>/dev/null || echo "?"
}

echo ""
echo "=== ShardForgeDB Phase 27 — Automatic Background Pull Replication Smoke Test ==="
echo ""
echo "SCOPE: Proves automatic replication without POST /replication/sync."
echo "       No Raft. No consensus. No quorum. No automatic failover."
echo ""

# ── 1-2. Initial health checks ────────────────────────────────────────────────
echo "-- Initial health checks"

STATUS=$(curl -sf "$LEADER/healthz" 2>/dev/null || true)
if echo "$STATUS" | grep -q '"ok"'; then
  ok "leader /healthz → ok"
else
  fail "leader /healthz did not return ok"
fi

STATUS=$(curl -sf "$FOLLOWER/healthz" 2>/dev/null || true)
if echo "$STATUS" | grep -q '"ok"'; then
  ok "follower /healthz → ok"
else
  fail "follower /healthz did not return ok"
fi

# ── 3-4. Background sync is enabled and running ────────────────────────────────
echo ""
echo "-- Background sync state"

FSTATUS=$(curl -sf "$FOLLOWER/replication/status" 2>/dev/null || true)
BG_ENABLED=$(echo "$FSTATUS" | python3 -c "
import sys,json
d=json.load(sys.stdin)
bg=d.get('background_sync',{})
print(bg.get('background_sync_enabled','false'))
" 2>/dev/null || echo "false")
if [ "$BG_ENABLED" = "True" ] || [ "$BG_ENABLED" = "true" ]; then
  ok "background_sync.enabled = true"
else
  fail "background_sync.enabled should be true (got: $BG_ENABLED)"
fi

# Wait for worker to be running.
BG_STATE="unknown"
for I in $(seq 1 20); do
  FSTATUS=$(curl -sf "$FOLLOWER/replication/status" 2>/dev/null || true)
  BG_STATE=$(echo "$FSTATUS" | python3 -c "
import sys,json
d=json.load(sys.stdin)
bg=d.get('background_sync',{})
print(bg.get('background_sync_state','unknown'))
" 2>/dev/null || echo "unknown")
  if [ "$BG_STATE" = "running" ]; then
    break
  fi
  sleep 0.3
done
if [ "$BG_STATE" = "running" ]; then
  ok "background_sync.state = running"
else
  fail "background_sync.state = $BG_STATE (want running)"
fi

# ── 5-8. Automatic PUT propagation ───────────────────────────────────────────
echo ""
echo "-- Automatic PUT propagation (no /replication/sync)"

KEY1="auto:key1"
VAL1="auto-value-1"
PUT1=$(curl -sf -X PUT "$LEADER/kv/$KEY1" \
  -H 'Content-Type: application/json' \
  -d "{\"value\":\"$VAL1\"}" 2>/dev/null || true)
if echo "$PUT1" | grep -q '"ok":true'; then
  ok "PUT $KEY1 to leader"
else
  fail "PUT $KEY1 failed: $PUT1"
fi

# Wait for automatic propagation (NOT using /replication/sync).
if poll_until "auto propagation of $KEY1" "$FOLLOWER/kv/$KEY1" "\"$VAL1\""; then
  ok "follower received $KEY1 automatically (no manual sync)"
else
  fail "follower did not receive $KEY1 automatically within 12s"
fi

# Verify cursor advanced.
FSTATUS=$(curl -sf "$FOLLOWER/replication/status" 2>/dev/null || true)
LAST_SEQ=$(echo "$FSTATUS" | python3 -c "
import sys,json
d=json.load(sys.stdin)
r=d.get('replication',{})
print(r.get('last_applied_seq',0))
" 2>/dev/null || echo "0")
if [ "$LAST_SEQ" != "0" ] && [ "$LAST_SEQ" != "?" ]; then
  ok "follower last_applied_seq=$LAST_SEQ (> 0)"
else
  fail "follower last_applied_seq=$LAST_SEQ (should be > 0)"
fi

# Verify lag is known and zero.
LAG_KNOWN="false"
LAG_ENTRIES="99"
for I in $(seq 1 20); do
  BGST=$(curl -sf "$FOLLOWER/replication/status" 2>/dev/null || true)
  LAG_KNOWN=$(echo "$BGST" | python3 -c "
import sys,json
d=json.load(sys.stdin)
bg=d.get('background_sync',{})
print(bg.get('lag_known','false'))
" 2>/dev/null || echo "false")
  LAG_ENTRIES=$(echo "$BGST" | python3 -c "
import sys,json
d=json.load(sys.stdin)
bg=d.get('background_sync',{})
print(bg.get('lag_entries',99))
" 2>/dev/null || echo "99")
  if ([ "$LAG_KNOWN" = "True" ] || [ "$LAG_KNOWN" = "true" ]) && [ "$LAG_ENTRIES" = "0" ]; then
    break
  fi
  sleep 0.3
done
if ([ "$LAG_KNOWN" = "True" ] || [ "$LAG_KNOWN" = "true" ]) && [ "$LAG_ENTRIES" = "0" ]; then
  ok "lag_known=true and lag_entries=0 (fully caught up)"
else
  fail "lag not converged to zero: lag_known=$LAG_KNOWN lag_entries=$LAG_ENTRIES"
fi

# ── 9-10. Multiple keys propagate ─────────────────────────────────────────────
echo ""
echo "-- Multiple mutations propagate automatically"

for I in 1 2 3 4 5; do
  curl -sf -X PUT "$LEADER/kv/batch:key$I" \
    -H 'Content-Type: application/json' \
    -d "{\"value\":\"batch-val-$I\"}" >/dev/null 2>&1 || true
done

ALL_OK=true
for I in 1 2 3 4 5; do
  if ! poll_until "batch:key$I" "$FOLLOWER/kv/batch:key$I" "batch-val-$I" 40 0.3; then
    ALL_OK=false
    break
  fi
done
if $ALL_OK; then
  ok "all 5 batch keys propagated automatically"
else
  fail "not all batch keys propagated within timeout"
fi

# ── 11-12. Automatic DELETE propagation ───────────────────────────────────────
echo ""
echo "-- Automatic DELETE propagation"

curl -sf -X DELETE "$LEADER/kv/$KEY1" >/dev/null 2>&1 || true

for I in $(seq 1 40); do
  GETF=$(curl -sf "$FOLLOWER/kv/$KEY1" 2>/dev/null || true)
  FOUND=$(echo "$GETF" | python3 -c "
import sys,json
d=json.load(sys.stdin)
print(d.get('found','true'))
" 2>/dev/null || echo "true")
  if [ "$FOUND" = "False" ] || [ "$FOUND" = "false" ]; then
    ok "DELETE propagated automatically to follower (key gone)"
    break
  fi
  if [ "$I" -eq 40 ]; then
    fail "DELETE did not propagate within 12s"
  fi
  sleep 0.3
done

# ── 11-14. Primary stop → follower enters backoff, lag becomes unknown ─────────
echo ""
echo "-- Stop primary — follower should enter backoff / lag becomes unknown"

LEADER_PID=$(lsof -i ":9501" -sTCP:LISTEN -t 2>/dev/null || true)
if [ -n "$LEADER_PID" ]; then
  kill "$LEADER_PID" 2>/dev/null || true
  sleep 0.5
  echo "  killed leader pid $LEADER_PID"
fi

# Check 11: Poll for backing_off state.
BG_STATE_AFTER_KILL="unknown"
BACKING_OFF_BGST=""
for I in $(seq 1 40); do
  BGST=$(curl -sf "$FOLLOWER/replication/status" 2>/dev/null || true)
  BG_STATE_AFTER_KILL=$(echo "$BGST" | python3 -c "
import sys,json
d=json.load(sys.stdin)
bg=d.get('background_sync',{})
print(bg.get('background_sync_state','unknown'))
" 2>/dev/null || echo "unknown")
  if [ "$BG_STATE_AFTER_KILL" = "backing_off" ]; then
    BACKING_OFF_BGST="$BGST"
    break
  fi
  sleep 0.3
done
if [ "$BG_STATE_AFTER_KILL" = "backing_off" ]; then
  ok "follower enters backing_off state after primary stopped"
else
  fail "background_sync.state=$BG_STATE_AFTER_KILL (want backing_off)"
fi

# Check 12: total_sync_failures > 0.
TOTAL_FAILURES=$(echo "$BACKING_OFF_BGST" | python3 -c "
import sys,json
d=json.load(sys.stdin)
bg=d.get('background_sync',{})
print(bg.get('total_sync_failures',0))
" 2>/dev/null || echo "0")
if [ "$TOTAL_FAILURES" != "0" ] && [ "$TOTAL_FAILURES" != "?" ]; then
  ok "total_sync_failures=$TOTAL_FAILURES > 0 while backing_off"
else
  fail "total_sync_failures=$TOTAL_FAILURES (should be > 0 after primary stopped)"
fi

# Check 13: next_retry_at is present.
NEXT_RETRY=$(echo "$BACKING_OFF_BGST" | python3 -c "
import sys,json
d=json.load(sys.stdin)
bg=d.get('background_sync',{})
v=bg.get('next_retry_at',None)
print('present' if v is not None else 'absent')
" 2>/dev/null || echo "absent")
if [ "$NEXT_RETRY" = "present" ]; then
  ok "next_retry_at is set while follower is backing_off"
else
  fail "next_retry_at absent while backing_off (should be set)"
fi

# Check 14: lag_known becomes false after primary is unreachable.
LAG_UNKNOWN=false
for I in $(seq 1 30); do
  BGST=$(curl -sf "$FOLLOWER/replication/status" 2>/dev/null || true)
  LAG_KNOWN=$(echo "$BGST" | python3 -c "
import sys,json
d=json.load(sys.stdin)
bg=d.get('background_sync',{})
print(bg.get('lag_known','true'))
" 2>/dev/null || echo "true")
  if [ "$LAG_KNOWN" = "False" ] || [ "$LAG_KNOWN" = "false" ]; then
    LAG_UNKNOWN=true
    break
  fi
  sleep 0.4
done
if $LAG_UNKNOWN; then
  ok "lag_known=false after primary stopped (stale lag correctly reported as unknown)"
else
  fail "lag_known still true after primary stopped (should be false/stale)"
fi

# ── 15-19. Restart primary — follower recovers automatically ──────────────────
echo ""
echo "-- Restart primary — follower should recover automatically"

"$BIN" \
  --node-id leader \
  --addr "$LEADER_ADDR" \
  --data-dir "$LEADER_DIR" \
  --replication-role primary \
  >>"$LOG_DIR/leader.log" 2>&1 &
NEW_LEADER_PID=$!
echo "  restarted leader pid $NEW_LEADER_PID"

# Check 15: leader /healthz → ok after restart.
if wait_for_health "leader" "$LEADER"; then
  ok "leader /healthz → ok after restart"
else
  fail "leader did not recover within 12s"
fi

# Check 16: Wait for follower to recover (lag_known becomes true again).
LAG_RECOVERED=false
RECOVERY_BGST=""
for I in $(seq 1 40); do
  BGST=$(curl -sf "$FOLLOWER/replication/status" 2>/dev/null || true)
  LAG_KNOWN=$(echo "$BGST" | python3 -c "
import sys,json
d=json.load(sys.stdin)
bg=d.get('background_sync',{})
print(bg.get('lag_known','false'))
" 2>/dev/null || echo "false")
  if [ "$LAG_KNOWN" = "True" ] || [ "$LAG_KNOWN" = "true" ]; then
    LAG_RECOVERED=true
    RECOVERY_BGST="$BGST"
    break
  fi
  sleep 0.4
done
if $LAG_RECOVERED; then
  ok "follower background sync recovered automatically (lag_known=true)"
else
  fail "follower did not recover: lag_known still false after 16s"
fi

# Check 17: current_backoff_ms=0 after recovery (backoff fully reset).
# current_backoff_ms is json:"...,omitempty" so the field is ABSENT when 0.
# Use None as default and treat absent (None) the same as explicit 0.
BACKOFF_MS=$(echo "$RECOVERY_BGST" | python3 -c "
import sys,json
d=json.load(sys.stdin)
bg=d.get('background_sync',{})
v=bg.get('current_backoff_ms', None)
print(0 if v is None else v)
" 2>/dev/null || echo "?")
if [ "$BACKOFF_MS" = "0" ]; then
  ok "current_backoff_ms=0 after recovery (backoff reset)"
else
  fail "current_backoff_ms=$BACKOFF_MS after recovery (want 0)"
fi

# Check 18: next_retry_at=null after recovery.
NEXT_RETRY_AFTER=$(echo "$RECOVERY_BGST" | python3 -c "
import sys,json
d=json.load(sys.stdin)
bg=d.get('background_sync',{})
v=bg.get('next_retry_at',None)
print('present' if v is not None else 'absent')
" 2>/dev/null || echo "present")
if [ "$NEXT_RETRY_AFTER" = "absent" ]; then
  ok "next_retry_at=null after recovery"
else
  fail "next_retry_at still set after recovery (should be null)"
fi

# Check 19: background_sync.state=running after recovery.
RECOVERY_STATE=$(echo "$RECOVERY_BGST" | python3 -c "
import sys,json
d=json.load(sys.stdin)
bg=d.get('background_sync',{})
print(bg.get('background_sync_state','unknown'))
" 2>/dev/null || echo "unknown")
if [ "$RECOVERY_STATE" = "running" ]; then
  ok "background_sync.state=running after recovery (backoff reset)"
else
  # State may still be transitioning; re-check once.
  sleep 1
  BGST=$(curl -sf "$FOLLOWER/replication/status" 2>/dev/null || true)
  RECOVERY_STATE=$(echo "$BGST" | python3 -c "
import sys,json
d=json.load(sys.stdin)
bg=d.get('background_sync',{})
print(bg.get('background_sync_state','unknown'))
" 2>/dev/null || echo "unknown")
  if [ "$RECOVERY_STATE" = "running" ]; then
    ok "background_sync.state=running after recovery (backoff reset)"
  else
    fail "background_sync.state=$RECOVERY_STATE (want running after recovery)"
  fi
fi

# ── 20. New key after restart propagates ─────────────────────────────────────
echo ""
echo "-- Write after primary restart propagates automatically"

KEY_AFTER="auto:after-restart"
VAL_AFTER="after-restart-value"
curl -sf -X PUT "$LEADER/kv/$KEY_AFTER" \
  -H 'Content-Type: application/json' \
  -d "{\"value\":\"$VAL_AFTER\"}" >/dev/null 2>&1 || true

if poll_until "auto propagation after restart" "$FOLLOWER/kv/$KEY_AFTER" "\"$VAL_AFTER\""; then
  ok "key written after primary restart propagated automatically"
else
  fail "key after restart did not propagate within 12s"
fi

# ── 21-24. Follower restart — durable cursor survives ─────────────────────────
echo ""
echo "-- Restart follower — durable cursor must survive"

FOLLOWER_PID=$(lsof -i ":9502" -sTCP:LISTEN -t 2>/dev/null || true)
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
  --bg-sync \
  --bg-sync-interval 500ms \
  --bg-sync-request-timeout 2s \
  --bg-sync-initial-backoff 250ms \
  --bg-sync-max-backoff 5s \
  --bg-sync-jitter-fraction 0.10 \
  >>"$LOG_DIR/follower.log" 2>&1 &
NEW_FOLLOWER_PID=$!
echo "  restarted follower pid $NEW_FOLLOWER_PID"

# Check 22: follower /healthz → ok after restart.
if wait_for_health "follower" "$FOLLOWER"; then
  ok "follower /healthz → ok after restart"
else
  fail "follower did not recover within 12s"
fi

# Check 23: last_applied_seq > 0 immediately after restart (cursor restored from state file,
# before the background worker has a chance to sync new entries).
RESTART_SEQ=$(curl -sf "$FOLLOWER/replication/status" 2>/dev/null \
  | python3 -c "
import sys,json
d=json.load(sys.stdin)
r=d.get('replication',{})
print(r.get('last_applied_seq',0))
" 2>/dev/null || echo "0")
if [ "$RESTART_SEQ" != "0" ] && [ "$RESTART_SEQ" != "?" ]; then
  ok "follower last_applied_seq=$RESTART_SEQ > 0 immediately after restart (cursor restored from state file)"
else
  fail "follower last_applied_seq=$RESTART_SEQ after restart (should be > 0 — cursor not restored)"
fi

# ── 24. New key after follower restart propagates automatically ────────────────
echo ""
echo "-- Write after follower restart propagates automatically"

KEY_AFTER_F="auto:after-follower-restart"
VAL_AFTER_F="after-follower-restart-value"
curl -sf -X PUT "$LEADER/kv/$KEY_AFTER_F" \
  -H 'Content-Type: application/json' \
  -d "{\"value\":\"$VAL_AFTER_F\"}" >/dev/null 2>&1 || true

if poll_until "auto propagation after follower restart" "$FOLLOWER/kv/$KEY_AFTER_F" "\"$VAL_AFTER_F\""; then
  ok "key after follower restart propagated automatically (durable cursor preserved)"
else
  fail "key after follower restart did not propagate within 12s"
fi

# Bonus: verify manual sync endpoint still available (returns 200 OK or 409 if bg-worker busy).
# Uses -w "%{http_code}" so that a 409 (ErrSyncInProgress) is accepted as valid behavior.
SYNC_HTTP=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$FOLLOWER/replication/sync" 2>/dev/null || echo "000")
if [ "$SYNC_HTTP" = "200" ] || [ "$SYNC_HTTP" = "409" ]; then
  ok "POST /replication/sync returns $SYNC_HTTP (200=sync'd, 409=bg-worker busy — both valid)"
else
  fail "POST /replication/sync unexpected status $SYNC_HTTP"
fi

# ── Summary ───────────────────────────────────────────────────────────────────
echo ""
echo "=== Summary ==="
echo "  Passed: $PASS"
echo "  Failed: $FAIL"
echo ""
echo "SCOPE REMINDER:"
echo "  - Background sync pulls automatically every 500ms (no manual POST /replication/sync needed)"
echo "  - Exponential backoff on primary failure; resets on recovery"
echo "  - Lag tracked as operation count (primary_latest_seq - follower_last_applied_seq)"
echo "  - Durable cursor (replication_state.json) survives follower restarts"
echo "  - Durable journal (replication.journal) survives primary restarts"
echo "  - No Raft. No consensus. No quorum. No automatic failover."
echo "  - Phase 26 crash window: engine commit → journal append"
echo ""

if [ "$FAIL" -eq 0 ]; then
  echo "All $PASS checks passed. Phase 27 automatic background pull replication is working correctly."
  exit 0
else
  echo "$FAIL check(s) failed. Review output above."
  exit 1
fi
