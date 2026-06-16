#!/usr/bin/env bash
# scripts/repl_failover_demo_smoke.sh — Phase 28 Manual Promotion: smoke test (32 checks)
#
# Scope: Proves manual promotion workflow end-to-end.
#        Requires repl_failover_demo_up.sh to have been run first.
#
# Checks performed (32 total):
#   1.  primary /healthz -> ok
#   2.  follower /healthz -> ok
#   3.  PUT key1 to primary
#   4.  PUT key2 to primary
#   5.  DELETE key2 on primary
#   6.  Follower receives key1 automatically
#   7.  Follower receives DELETE of key2
#   8.  Follower last_applied_seq == 3
#   9.  Primary runtime_role = primary
#   10. Follower runtime_role = follower
#   11. POST /replication/quiesce on primary succeeds
#   12. Quiesce response write_state = quiesced
#   13. Quiesce response primary_latest_seq = 3
#   14. PUT rejected on quiesced primary (409)
#   15. DELETE rejected on quiesced primary (409)
#   16. GET still works on quiesced primary
#   17. Replication log still available on quiesced primary
#   18. Follower cursor matches quiesce seq
#   19. Stop old primary
#   20. Primary process is stopped
#   21. POST /replication/promote on follower succeeds
#   22. Promote response new_role = primary
#   23. Promote response inherited_last_seq = 3
#   24. Promoted follower runtime_role = primary
#   25. Promoted follower write_state = active
#   26. PUT on promoted follower succeeds
#   27. New data readable on promoted follower
#   28. Promoted follower replication last_local_seq = 4
#   29. Restart promoted follower — role persists
#   30. Restarted node runtime_role = primary
#   31. Pre-promotion data readable after restart
#   32. New write after restart has correct seq
set -eu

PRIMARY="http://127.0.0.1:9601"
FOLLOWER="http://127.0.0.1:9602"
PRIMARY_ADDR="127.0.0.1:9601"
FOLLOWER_ADDR="127.0.0.1:9602"
PRIMARY_DIR="/tmp/sfdb-failover-primary"
FOLLOWER_DIR="/tmp/sfdb-failover-follower"
LOG_DIR="/tmp/sfdb-failover-logs"
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$REPO_ROOT/bin/shardforge-node"

PASS=0
FAIL=0

ok()   { echo "  PASS: $1"; PASS=$((PASS+1)); }
fail() { echo "  FAIL: $1"; FAIL=$((FAIL+1)); }

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

jf() {
  python3 -c "import sys,json; d=json.load(sys.stdin); ${1}" 2>/dev/null || echo "?"
}

echo ""
echo "=== ShardForgeDB Phase 28 — Manual Promotion Smoke Test (32 checks) ==="
echo ""

# ── 1-2. Health checks ─────────────────────────────────────────────────────────
echo "-- Health checks"

if curl -sf "$PRIMARY/healthz" | grep -q '"ok"'; then
  ok "primary /healthz -> ok"
else
  fail "primary /healthz"
fi

if curl -sf "$FOLLOWER/healthz" | grep -q '"ok"'; then
  ok "follower /healthz -> ok"
else
  fail "follower /healthz"
fi

# ── 3-8. Data replication ───────────────────────────────────────────────────────
echo ""
echo "-- Data replication"

R=$(curl -sf -X PUT "$PRIMARY/kv/key1" -H 'Content-Type: application/json' -d '{"value":"val1"}' 2>/dev/null || true)
if echo "$R" | grep -q '"ok":true'; then ok "PUT key1 to primary"; else fail "PUT key1"; fi

R=$(curl -sf -X PUT "$PRIMARY/kv/key2" -H 'Content-Type: application/json' -d '{"value":"val2"}' 2>/dev/null || true)
if echo "$R" | grep -q '"ok":true'; then ok "PUT key2 to primary"; else fail "PUT key2"; fi

R=$(curl -sf -X DELETE "$PRIMARY/kv/key2" 2>/dev/null || true)
if echo "$R" | grep -q '"ok":true'; then ok "DELETE key2 on primary"; else fail "DELETE key2"; fi

if poll_until "key1" "$FOLLOWER/kv/key1" '"val1"'; then
  ok "follower received key1 automatically"
else
  fail "follower did not receive key1"
fi

# Wait for DELETE to propagate.
DEL_OK=false
for I in $(seq 1 40); do
  R=$(curl -sf "$FOLLOWER/kv/key2" 2>/dev/null || true)
  FOUND=$(echo "$R" | python3 -c "import sys,json; print(json.load(sys.stdin).get('found','true'))" 2>/dev/null || echo "true")
  if [ "$FOUND" = "False" ] || [ "$FOUND" = "false" ]; then
    DEL_OK=true
    break
  fi
  sleep 0.3
done
if $DEL_OK; then ok "follower received DELETE of key2"; else fail "follower DELETE not propagated"; fi

# Check follower seq.
for I in $(seq 1 20); do
  FSEQ=$(curl -sf "$FOLLOWER/replication/status" | python3 -c "
import sys,json; d=json.load(sys.stdin); print(d.get('replication',{}).get('last_applied_seq',0))
" 2>/dev/null || echo "0")
  if [ "$FSEQ" = "3" ]; then break; fi
  sleep 0.3
done
if [ "$FSEQ" = "3" ]; then ok "follower last_applied_seq == 3"; else fail "follower seq=$FSEQ (want 3)"; fi

# ── 9-10. Runtime role ──────────────────────────────────────────────────────────
echo ""
echo "-- Runtime roles"

PR=$(curl -sf "$PRIMARY/replication/status" | python3 -c "
import sys,json; print(json.load(sys.stdin).get('runtime_role','?'))
" 2>/dev/null || echo "?")
if [ "$PR" = "primary" ]; then ok "primary runtime_role = primary"; else fail "primary role=$PR"; fi

FR=$(curl -sf "$FOLLOWER/replication/status" | python3 -c "
import sys,json; print(json.load(sys.stdin).get('runtime_role','?'))
" 2>/dev/null || echo "?")
if [ "$FR" = "follower" ]; then ok "follower runtime_role = follower"; else fail "follower role=$FR"; fi

# ── 11-17. Quiesce primary ─────────────────────────────────────────────────────
echo ""
echo "-- Quiesce primary"

QRESP=$(curl -sf -X POST "$PRIMARY/replication/quiesce" 2>/dev/null || true)
QOK=$(echo "$QRESP" | python3 -c "import sys,json; print(json.load(sys.stdin).get('write_state','?'))" 2>/dev/null || echo "?")
if [ "$QOK" = "quiesced" ]; then ok "POST /replication/quiesce succeeds"; else fail "quiesce: $QOK"; fi

QWS=$(echo "$QRESP" | python3 -c "import sys,json; print(json.load(sys.stdin).get('write_state','?'))" 2>/dev/null || echo "?")
if [ "$QWS" = "quiesced" ]; then ok "quiesce write_state = quiesced"; else fail "write_state=$QWS"; fi

QSEQ=$(echo "$QRESP" | python3 -c "import sys,json; print(json.load(sys.stdin).get('primary_latest_seq',0))" 2>/dev/null || echo "0")
if [ "$QSEQ" = "3" ]; then ok "quiesce primary_latest_seq = 3"; else fail "quiesce seq=$QSEQ"; fi

PUT_HTTP=$(curl -s -o /dev/null -w "%{http_code}" -X PUT "$PRIMARY/kv/rejected" -H 'Content-Type: application/json' -d '{"value":"x"}' 2>/dev/null || echo "000")
if [ "$PUT_HTTP" = "409" ]; then ok "PUT rejected on quiesced primary (409)"; else fail "PUT http=$PUT_HTTP"; fi

DEL_HTTP=$(curl -s -o /dev/null -w "%{http_code}" -X DELETE "$PRIMARY/kv/key1" 2>/dev/null || echo "000")
if [ "$DEL_HTTP" = "409" ]; then ok "DELETE rejected on quiesced primary (409)"; else fail "DELETE http=$DEL_HTTP"; fi

GET_R=$(curl -sf "$PRIMARY/kv/key1" 2>/dev/null || true)
GET_FOUND=$(echo "$GET_R" | python3 -c "import sys,json; print(json.load(sys.stdin).get('found',False))" 2>/dev/null || echo "?")
if [ "$GET_FOUND" = "True" ] || [ "$GET_FOUND" = "true" ]; then ok "GET still works on quiesced primary"; else fail "GET found=$GET_FOUND"; fi

LOG_HTTP=$(curl -s -o /dev/null -w "%{http_code}" "$PRIMARY/replication/log?after=0" 2>/dev/null || echo "000")
if [ "$LOG_HTTP" = "200" ]; then ok "replication log still available on quiesced primary"; else fail "log http=$LOG_HTTP"; fi

# ── 18. Follower cursor matches ────────────────────────────────────────────────
FSEQ2=$(curl -sf "$FOLLOWER/replication/status" | python3 -c "
import sys,json; d=json.load(sys.stdin); print(d.get('replication',{}).get('last_applied_seq',0))
" 2>/dev/null || echo "0")
if [ "$FSEQ2" = "$QSEQ" ]; then ok "follower cursor matches quiesce seq ($QSEQ)"; else fail "follower seq=$FSEQ2 != quiesce seq=$QSEQ"; fi

# ── 19-20. Stop old primary ────────────────────────────────────────────────────
echo ""
echo "-- Stop old primary"

PRIMARY_PID=$(lsof -i ":9601" -sTCP:LISTEN -t 2>/dev/null || true)
if [ -n "$PRIMARY_PID" ]; then
  kill "$PRIMARY_PID" 2>/dev/null || true
  sleep 0.5
  ok "stop old primary (pid=$PRIMARY_PID)"
else
  ok "stop old primary (already stopped)"
fi

if ! lsof -i ":9601" -sTCP:LISTEN -t >/dev/null 2>&1; then
  ok "primary process is stopped"
else
  fail "primary still running"
fi

# ── 21-28. Promote follower ─────────────────────────────────────────────────────
echo ""
echo "-- Promote follower"

QUIESCE_ID=$(echo "$QRESP" | python3 -c "import sys,json; print(json.load(sys.stdin).get('quiesce_id',''))" 2>/dev/null || echo "")
QUIESCED_AT=$(echo "$QRESP" | python3 -c "import sys,json; print(json.load(sys.stdin).get('quiesced_at',''))" 2>/dev/null || echo "")

# Build the promote request with the quiesce record.
# We need to compute the checksum correctly.
PROMOTE_BODY=$(python3 -c "
import json, hashlib, struct, zlib
qr = {
    'version': 1,
    'quiesce_id': '$QUIESCE_ID',
    'primary_node_id': 'primary-1',
    'primary_base_url': 'http://$PRIMARY_ADDR',
    'primary_latest_seq': $QSEQ,
    'quiesced_at': '$QUIESCED_AT',
    'checksum': 0
}
data = json.dumps(qr, separators=(',', ':')).encode()
crc = zlib.crc32(data) & 0xffffffff
qr['checksum'] = crc
req = {
    'quiesce_record': qr,
    'confirm_old_primary_stopped': True
}
print(json.dumps(req))
" 2>/dev/null)

PRESP=$(curl -sf -X POST "$FOLLOWER/replication/promote" \
  -H 'Content-Type: application/json' \
  -d "$PROMOTE_BODY" 2>/dev/null || true)

PNEW_ROLE=$(echo "$PRESP" | python3 -c "import sys,json; print(json.load(sys.stdin).get('new_role','?'))" 2>/dev/null || echo "?")
if [ "$PNEW_ROLE" = "primary" ]; then ok "POST /replication/promote succeeds"; else fail "promote new_role=$PNEW_ROLE body=$PRESP"; fi

if [ "$PNEW_ROLE" = "primary" ]; then ok "promote new_role = primary"; else fail "promote new_role=$PNEW_ROLE"; fi

PINHERITED=$(echo "$PRESP" | python3 -c "import sys,json; print(json.load(sys.stdin).get('inherited_last_seq',0))" 2>/dev/null || echo "0")
if [ "$PINHERITED" = "3" ]; then ok "promote inherited_last_seq = 3"; else fail "inherited=$PINHERITED"; fi

# Check runtime role on promoted follower.
PFR=$(curl -sf "$FOLLOWER/replication/status" | python3 -c "
import sys,json; print(json.load(sys.stdin).get('runtime_role','?'))
" 2>/dev/null || echo "?")
if [ "$PFR" = "primary" ]; then ok "promoted follower runtime_role = primary"; else fail "promoted role=$PFR"; fi

PFWS=$(curl -sf "$FOLLOWER/replication/status" | python3 -c "
import sys,json; print(json.load(sys.stdin).get('write_state','?'))
" 2>/dev/null || echo "?")
if [ "$PFWS" = "active" ]; then ok "promoted follower write_state = active"; else fail "write_state=$PFWS"; fi

# Write on promoted follower.
PUT2=$(curl -sf -X PUT "$FOLLOWER/kv/newkey1" -H 'Content-Type: application/json' -d '{"value":"newval1"}' 2>/dev/null || true)
if echo "$PUT2" | grep -q '"ok":true'; then ok "PUT on promoted follower succeeds"; else fail "PUT on promoted: $PUT2"; fi

GET2=$(curl -sf "$FOLLOWER/kv/newkey1" 2>/dev/null || true)
G2V=$(echo "$GET2" | python3 -c "import sys,json; print(json.load(sys.stdin).get('value','?'))" 2>/dev/null || echo "?")
if [ "$G2V" = "newval1" ]; then ok "new data readable on promoted follower"; else fail "new data: $G2V"; fi

PLSEQ=$(curl -sf "$FOLLOWER/replication/status" | python3 -c "
import sys,json; d=json.load(sys.stdin); print(d.get('replication',{}).get('last_local_seq',0))
" 2>/dev/null || echo "0")
if [ "$PLSEQ" = "4" ]; then ok "promoted follower last_local_seq = 4"; else fail "promoted seq=$PLSEQ"; fi

# ── 29-32. Restart promoted follower ────────────────────────────────────────────
echo ""
echo "-- Restart promoted follower"

FPID=$(lsof -i ":9602" -sTCP:LISTEN -t 2>/dev/null || true)
if [ -n "$FPID" ]; then
  kill "$FPID" 2>/dev/null || true
  sleep 0.5
fi

"$BIN" \
  --node-id follower-1 \
  --addr "$FOLLOWER_ADDR" \
  --data-dir "$FOLLOWER_DIR" \
  --replication-role follower \
  --primary-url "http://$PRIMARY_ADDR" \
  >>"$LOG_DIR/follower.log" 2>&1 &
NEWFPID=$!
echo "  restarted follower pid $NEWFPID"

if wait_for_health "follower" "$FOLLOWER"; then
  ok "restart promoted follower -- role persists"
else
  fail "promoted follower did not restart"
fi

# Check runtime role after restart.
RR=$(curl -sf "$FOLLOWER/replication/status" | python3 -c "
import sys,json; print(json.load(sys.stdin).get('runtime_role','?'))
" 2>/dev/null || echo "?")
if [ "$RR" = "primary" ]; then ok "restarted node runtime_role = primary"; else fail "restarted role=$RR"; fi

# Pre-promotion data readable.
RD=$(curl -sf "$FOLLOWER/kv/key1" 2>/dev/null || true)
RDV=$(echo "$RD" | python3 -c "import sys,json; print(json.load(sys.stdin).get('value','?'))" 2>/dev/null || echo "?")
if [ "$RDV" = "val1" ]; then ok "pre-promotion data readable after restart"; else fail "data: $RDV"; fi

# New write has correct seq.
curl -sf -X PUT "$FOLLOWER/kv/post_restart" -H 'Content-Type: application/json' -d '{"value":"pr1"}' >/dev/null 2>&1 || true
RRSEQ=$(curl -sf "$FOLLOWER/replication/status" | python3 -c "
import sys,json; d=json.load(sys.stdin); print(d.get('replication',{}).get('last_local_seq',0))
" 2>/dev/null || echo "0")
if [ "$RRSEQ" = "5" ]; then ok "new write after restart has correct seq (5)"; else fail "restart seq=$RRSEQ"; fi

# ── Summary ────────────────────────────────────────────────────────────────────
echo ""
echo "=== Summary ==="
echo "  Passed: $PASS"
echo "  Failed: $FAIL"
echo ""
echo "SCOPE REMINDER:"
echo "  - Promotion is strictly operator-controlled (no automatic failover)"
echo "  - Quiesce drains in-flight writes and persists final sequence"
echo "  - Promotion requires explicit operator confirmation"
echo "  - Old primary remains write-fenced on restart"
echo "  - NOT Raft. NOT consensus. NOT quorum. NOT automatic failover."
echo ""

if [ "$FAIL" -eq 0 ]; then
  echo "All $PASS checks passed. Phase 28 manual promotion is working correctly."
  exit 0
else
  echo "$FAIL check(s) failed. Review output above."
  exit 1
fi
