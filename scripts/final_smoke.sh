#!/usr/bin/env bash
# scripts/final_smoke.sh — Phase 20 final smoke validation
# Runs the full acceptance gate for portfolio launch readiness.
# No Docker required. No network calls. Pure local validation.
set -eu

PASS=0
FAIL=0

ok()   { echo "  PASS: $1"; PASS=$((PASS+1)); }
fail() { echo "  FAIL: $1"; FAIL=$((FAIL+1)); }

echo ""
echo "=== ShardForgeDB Final Smoke Validation ==="
echo ""

# ── Go toolchain checks ───────────────────────────────────────────────
echo "-- Go toolchain"

go mod tidy
ok "go mod tidy succeeded"

go fmt ./... 2>&1 | tee /tmp/sfdb-fmt.out
if [ ! -s /tmp/sfdb-fmt.out ]; then
  ok "go fmt ./... produces no changes"
else
  fail "go fmt produced output (formatting issues)"
fi

go vet ./...
ok "go vet ./... passes"

# ── Tests ─────────────────────────────────────────────────────────────
echo ""
echo "-- Tests (race detector)"
go test -race -count=1 ./...
ok "go test -race -count=1 ./... passes (all 700+ tests)"

# ── Build ─────────────────────────────────────────────────────────────
echo ""
echo "-- Build"
make build
ok "make build produces all 7 binaries"

for bin in shardforge shardforge-bench shardforge-dashboard shardforge-node shardforge-gateway shardforge-proxy shardforge-cluster; do
  if [ -x "bin/$bin" ]; then
    ok "bin/$bin exists and is executable"
  else
    fail "bin/$bin missing or not executable"
  fi
done

# ── CLI checks ────────────────────────────────────────────────────────
echo ""
echo "-- CLI checks"

./bin/shardforge version > /tmp/sfdb-v.out 2>&1; grep -q "ShardForgeDB" /tmp/sfdb-v.out
ok "shardforge version prints version"

./bin/shardforge-bench --help > /tmp/sfdb-h.out 2>&1 || true; grep -qi "scale" /tmp/sfdb-h.out
ok "shardforge-bench --help mentions scale"

./bin/shardforge-dashboard --help > /tmp/sfdb-h.out 2>&1 || true; grep -qi "local" /tmp/sfdb-h.out
ok "shardforge-dashboard --help mentions local"

./bin/shardforge-node --help > /tmp/sfdb-h.out 2>&1 || true; grep -qi "node" /tmp/sfdb-h.out
ok "shardforge-node --help mentions node"

./bin/shardforge-gateway --help > /tmp/sfdb-h.out 2>&1 || true; grep -qi "routing" /tmp/sfdb-h.out
ok "shardforge-gateway --help mentions routing"

./bin/shardforge-proxy --help > /tmp/sfdb-h.out 2>&1 || true; grep -qi "proxy" /tmp/sfdb-h.out
ok "shardforge-proxy --help mentions proxy"

./bin/shardforge-cluster --help > /tmp/sfdb-h.out 2>&1 || true; grep -qi "cluster" /tmp/sfdb-h.out
ok "shardforge-cluster --help mentions cluster"

# ── Config validation ─────────────────────────────────────────────────
echo ""
echo "-- Cluster config validation"

./bin/shardforge-cluster validate configs/local-3node.json
ok "configs/local-3node.json validates"

./bin/shardforge-cluster validate configs/local-3node-with-proxy.json
ok "configs/local-3node-with-proxy.json validates"

./bin/shardforge-cluster validate configs/local-failure-sim-3node.json
ok "configs/local-failure-sim-3node.json validates"

# ── Ops simulation (no live nodes required) ───────────────────────────
echo ""
echo "-- Ops simulation (pure computation)"

./bin/shardforge-cluster simulate-failure configs/local-failure-sim-3node.json \
  --down node-2 --key user:1 --key user:2 > /tmp/sfdb-sim.out 2>&1
grep -q "affected" /tmp/sfdb-sim.out
ok "simulate-failure returns affected field"

./bin/shardforge-cluster plan-rebalance configs/local-failure-sim-3node.json \
  --remove node-2 --key user:1 --key user:2 > /tmp/sfdb-rb.out 2>&1
grep -q "movements" /tmp/sfdb-rb.out
ok "plan-rebalance returns movements field"

# ── Docker Compose config (no daemon required) ────────────────────────
echo ""
echo "-- Docker Compose config validation"

if command -v docker >/dev/null 2>&1; then
  docker compose -f deploy/docker-compose.yml config >/dev/null
  ok "deploy/docker-compose.yml config is valid"

  docker compose -f deploy/docker-compose-replica.yml config >/dev/null
  ok "deploy/docker-compose-replica.yml config is valid"
else
  echo "  SKIP: docker not available — skipping compose config validation"
fi

# ── Summary ───────────────────────────────────────────────────────────
echo ""
echo "=== Summary ==="
echo "  Passed: $PASS"
echo "  Failed: $FAIL"
echo ""

if [ "$FAIL" -eq 0 ]; then
  echo "All checks passed. ShardForgeDB is ready for portfolio launch."
  exit 0
else
  echo "Some checks failed. Review output above before tagging v0.2.0-portfolio."
  exit 1
fi
