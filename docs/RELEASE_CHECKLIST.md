# ShardForgeDB Release Checklist

This checklist must pass before any release or portfolio submission.

---

## Build Checklist

- [ ] `make build` succeeds — produces `bin/shardforge`, `bin/shardforge-bench`, `bin/shardforge-dashboard`
- [ ] `go build ./...` succeeds with no errors
- [ ] `go vet ./...` returns no issues
- [ ] `go mod tidy` produces no uncommitted changes
- [ ] `go fmt ./...` produces no uncommitted changes

## Test Checklist

- [ ] `go test -race -count=1 ./...` — all packages PASS with race detector
- [ ] `make test` passes
- [ ] No flaky tests in CI on at least 3 consecutive runs
- [ ] Dashboard tests cover 405 method rejection and `Allow: GET` header
- [ ] Scenario tests cover nil store input (no panic)
- [ ] Concurrent snapshot tests pass under `-race`

## Benchmark Checklist

- [ ] `make bench-dashboard` — 8 benchmarks PASS
- [ ] `make bench-replica` — 10 benchmarks PASS
- [ ] `make bench-shard` — 10 benchmarks PASS
- [ ] `make bench-vector` — 10 benchmarks PASS
- [ ] `make bench-engine` — benchmarks PASS
- [ ] `make bench-report` — writes `docs/BENCHMARKS.md` without error
- [ ] Manual Phase 10/11/12 BENCHMARKS.md sections preserved after `make bench-report`
  (if stripped by generator: run `git restore docs/BENCHMARKS.md` before committing)

## Demo Checklist

- [ ] `./bin/shardforge --help` prints usage without error
- [ ] `./bin/shardforge version` prints `ShardForgeDB 0.1.0`
- [ ] `./bin/shardforge-bench --scale small --out /tmp/demo.md` completes without error
- [ ] `./bin/shardforge-dashboard --help` prints local-only disclaimer
- [ ] `./bin/shardforge-dashboard --demo` starts HTTP server at `127.0.0.1:8080`
- [ ] `./bin/shardforge-dashboard --demo --run-chaos` runs all 3 chaos scenarios and starts server
- [ ] `GET http://127.0.0.1:8080/` returns HTML with component cards
- [ ] `GET http://127.0.0.1:8080/status` returns valid JSON Snapshot
- [ ] `GET http://127.0.0.1:8080/healthz` returns `{"status":"ok"}`
- [ ] `GET http://127.0.0.1:8080/events` returns JSON array
- [ ] `POST http://127.0.0.1:8080/` returns 405 with `Allow: GET`
- [ ] `./scripts/smoke.sh` passes
- [ ] `./scripts/demo.sh` passes
- [ ] `./scripts/release_check.sh` passes (long-running; includes all benchmarks)

## Scope Honesty Checklist

- [ ] README does not claim "distributed database cluster"
- [ ] README does not claim "production Raft consensus"
- [ ] README does not claim "fault-tolerant quorum replication"
- [ ] README does not claim "real networked cluster"
- [ ] README does not claim "ANN/HNSW/IVF vector database"
- [ ] README does not claim "background compaction"
- [ ] README does not claim "automatic compaction"
- [ ] README does not claim "production monitoring system"
- [ ] README correctly says dashboard is "local HTTP server only"
- [ ] README correctly says replication is "local in-process simulation"
- [ ] README correctly says sharding is "local single-process"
- [ ] `docs/DESIGN.md` does not claim distributed deployment
- [ ] `docs/BENCHMARKS.md` does not say "COMMIT fsync" (implementation uses temp+rename, not fsync)
- [ ] Footer in HTML dashboard reads: "Local dashboard only — no networking, no Raft, no consensus, no distributed cluster."

## Resume / LinkedIn Claims Checklist

### Allowed claims

- Built an explainable Go database engine from scratch.
- Implemented WAL, MemTable, SSTable, Bloom filter, LSM-style Engine.
- Implemented manual full compaction.
- Implemented exact vector search with cosine/L2/dot similarity.
- Implemented local single-process key-value sharding using consistent hashing.
- Implemented local in-process leader/follower replication simulation with pause/lag/catch-up controls.
- Implemented local HTTP observability dashboard and deterministic chaos/failure simulation scenarios.
- Added comprehensive tests (race-safe), reproducible benchmarks, and documentation at every phase.
- Set up GitHub Actions CI for all packages.

### Forbidden claims (these are NOT true and must not be stated)

- ~~"Built a distributed database cluster."~~
- ~~"Implemented production Raft consensus."~~
- ~~"Implemented fault-tolerant quorum replication."~~
- ~~"Built a real networked distributed system."~~
- ~~"Implemented ANN / HNSW / IVF vector search."~~
- ~~"Implemented background compaction."~~
- ~~"Implemented automatic compaction."~~
- ~~"Built a production monitoring system."~~
- ~~"Dashboard monitors a real distributed cluster."~~

### Accurate framing examples

> "Built an explainable Go database engine implementing an LSM-tree (WAL, MemTable, SSTable, Bloom filter), exact vector search, and local in-process simulations of sharding, replication, and a chaos-testing dashboard."

> "Each phase is documented in DESIGN.md and PROOF.md with honest scope boundaries; all claims about what is and is not implemented are explicitly stated in the codebase."

---

## CI Checklist

- [ ] GitHub Actions `CI` workflow passes on `main`
- [ ] GitHub Actions `CI` workflow passes on the current PR branch
- [ ] No flaky test patterns in CI history

---

## Phase 14 — Network Node Runtime Checklist

- [ ] `make build` produces `bin/shardforge-node` in addition to the three prior binaries
- [ ] `./bin/shardforge-node --help` prints scope disclaimer (NOT Raft, NOT consensus, NOT distributed)
- [ ] `go test -race -count=1 ./internal/node/...` — all tests PASS
- [ ] `make bench-node` — 6 benchmarks PASS
- [ ] Single node starts: `./bin/shardforge-node --node-id node-1 --addr 127.0.0.1:9101 --data-dir /tmp/n1`
- [ ] `GET /healthz` returns `{"status":"ok","node_id":"node-1"}`
- [ ] `PUT /kv/{key}` stores key; `GET /kv/{key}` returns it
- [ ] `DELETE /kv/{key}` removes key; subsequent GET returns `found:false`
- [ ] `GET /scan?start=&end=~` returns sorted entries
- [ ] `POST /flush` and `POST /compact` succeed
- [ ] Node restart on same `DataDir` preserves data written before shutdown
- [ ] Writing key to node-1 does NOT appear on node-2 (independent data dirs verified by test)
- [ ] `docker compose -f deploy/docker-compose.yml config` — config valid
- [ ] `docker compose -f deploy/docker-compose.yml up --build` — 3 nodes start (requires Docker daemon)
- [ ] `curl http://localhost:9101/healthz`, `9102/healthz`, `9103/healthz` all return `{"status":"ok",...}`
- [ ] Data written to node-1 does not appear on node-2 or node-3
- [ ] Node restart in Docker Compose preserves data (named volume survives container restart)
- [ ] `docker compose -f deploy/docker-compose.yml down -v` — tears down cleanly

## Phase 14 Scope Honesty Checklist

- [ ] No claim of distributed sharding
- [ ] No claim of networked replication
- [ ] No claim of Raft or consensus
- [ ] No claim of quorum or fault tolerance
- [ ] No claim of automatic leader election
- [ ] `--help` output explicitly states NOT Raft, NOT consensus, NOT distributed sharding
- [ ] Docker Compose README/docs state nodes are independent (no shared state)

---

## Phase 15 — Client-Side Routing Gateway Checklist

- [ ] `make build` produces `bin/shardforge-gateway` as the 5th binary
- [ ] `./bin/shardforge-gateway --help` prints scope disclaimer (client-side only, no Raft, no consensus, no failover)
- [ ] `go test -race -count=1 ./internal/gateway/...` — all 41 tests PASS
- [ ] `make bench-gateway` — 6 benchmarks PASS
- [ ] `route <key>` prints correct node ID and BaseURL (deterministic)
- [ ] `put <key> <value>` and `get <key>` work against live nodes
- [ ] `delete <key>` removes key from routed node
- [ ] `health` prints status for all configured nodes
- [ ] `flush-all` and `compact-all` fan out to all nodes
- [ ] Writing key via gateway only appears on the routed node, not others (test-verified)
- [ ] Same key always routes to same node (test-verified)
- [ ] Unavailable node returns clear error — no retry to another node (test-verified)
- [ ] Close is idempotent; operations after Close return ErrClosed (test-verified)

## Phase 15 Scope Honesty Checklist

- [ ] No claim of distributed sharding inside nodes
- [ ] No claim of networked replication between nodes
- [ ] No claim of Raft or consensus
- [ ] No claim of automatic failover
- [ ] No claim of shard migration or resharding
- [ ] `--help` output explicitly states client-side routing only, no failover
- [ ] PROOF.md documents why no retry is the correct behavior

## Resume / LinkedIn — Phase 15 Additions

### Allowed claims (Phase 15)

- Implemented a client-side routing gateway over independent HTTP nodes using FNV-1a consistent hashing.
- Deterministic key-to-node mapping with configurable virtual nodes and weight support.

### Forbidden claims (Phase 15)

- ~~"Implemented distributed sharding."~~
- ~~"Implemented automatic failover."~~
- ~~"Nodes coordinate through the gateway."~~

---

## Phase 16 — Stateless Gateway Proxy Server

### Validation Commands

```bash
go mod tidy
go fmt ./...
go vet ./...
go test -race -count=1 ./...
go test -bench=. -benchmem -benchtime=3s -run='^$' ./internal/proxy/...
make test
make vet
make build
make bench-proxy
./bin/shardforge-proxy --help
docker compose -f deploy/docker-compose.yml config
git status --short
```

### Allowed claims (Phase 16)

- "Implemented a stateless HTTP gateway proxy that routes requests to independent nodes using consistent hashing."
- "Proxy exposes 10 HTTP/JSON endpoints including /healthz, /status, /route/{key}, /kv/{key}, /scan-node/{nodeID}, /flush-all, /compact-all, /nodes/health."
- "Proxy explicitly documents no-failover, no-replication, no-Raft scope in /status response and startup banner."
- "Docker Compose demo includes proxy + 3 independent nodes."

### Forbidden claims (Phase 16)

- ~~"Implemented automatic failover or distributed fault tolerance."~~
- ~~"Proxy replicates data across nodes."~~
- ~~"Proxy retries requests on failure."~~
- ~~"The system is fault-tolerant."~~
- ~~"The gateway is a distributed router."~~

---

## Phase 17 — Static Cluster Metadata

### Validation Commands

```bash
go mod tidy
go fmt ./...
go vet ./...
go test -race -count=1 ./...
go test -bench=. -benchmem -benchtime=3s ./internal/cluster/...
make test
make vet
make build
make cluster-validate
./bin/shardforge-gateway --config configs/local-3node.json route user:1
./bin/shardforge-cluster validate configs/local-3node.json
./bin/shardforge-cluster validate configs/local-3node-with-proxy.json
./bin/shardforge-cluster validate configs/docker-3node-with-proxy.json
./bin/shardforge-proxy --help
./bin/shardforge-gateway --help
./bin/shardforge-cluster --help
docker compose -f deploy/docker-compose.yml config
git status --short
```

- [ ] `make build` produces 7 binaries including `bin/shardforge-cluster`
- [ ] `./bin/shardforge-cluster validate configs/local-3node.json` exits 0
- [ ] `./bin/shardforge-cluster validate configs/local-3node-with-proxy.json` exits 0
- [ ] `./bin/shardforge-cluster validate configs/docker-3node-with-proxy.json` exits 0
- [ ] `./bin/shardforge-gateway --config configs/local-3node.json route user:1` prints correct node
- [ ] `./bin/shardforge-proxy --help` mentions `--config`
- [ ] `./bin/shardforge-gateway --help` mentions `--config`
- [ ] `go test -race -count=1 ./internal/cluster/...` — all tests PASS
- [ ] `make bench-cluster` — 4 benchmarks PASS

### Phase 17 Scope Honesty Checklist

- [ ] No claim of dynamic cluster membership
- [ ] No claim of service discovery
- [ ] No claim of gossip
- [ ] No claim of Raft or consensus
- [ ] No claim of leader election
- [ ] No claim of replication or failover
- [ ] Config files include `scope` object with all limitations set to `true`
- [ ] Scope flags rejected if any are `false` (Validate enforces this)

### Allowed claims (Phase 17)

- "Implemented static cluster metadata and config-driven gateway/proxy startup."
- "Cluster config is a validated JSON file describing independent nodes, routing settings, and proxy address."
- "Config-based routing is deterministic and produces the same node assignments as --nodes."

### Forbidden claims (Phase 17)

- ~~"Implemented dynamic cluster membership or service discovery."~~
- ~~"Nodes coordinate through the config."~~
- ~~"Config provides automatic node health management."~~

---

## Phase 19 — Failure Handling and Manual Rebalance Simulation

### Phase 19 Validation Gate

- [ ] `go test -race -count=1 ./...` — all packages PASS (including `internal/ops`)
- [ ] `make bench-ops` — 4 benchmarks PASS
- [ ] `./bin/shardforge-cluster health configs/local-failure-sim-3node.json` — exits 0 (nodes may be unhealthy)
- [ ] `./bin/shardforge-cluster simulate-failure configs/local-failure-sim-3node.json --down node-2 --key user:1` — exits 0, valid JSON
- [ ] `./bin/shardforge-cluster plan-rebalance configs/local-failure-sim-3node.json --remove node-2 --key user:1` — exits 0, valid JSON
- [ ] `./bin/shardforge-cluster validate configs/local-failure-sim-3node.json` — exits 0
- [ ] `git status --short` — clean tree
- [ ] `go test -race -count=1 ./internal/ops/...` — 40+ tests PASS
- [ ] `go test -race -count=1 ./cmd/shardforge-cluster/...` — 25+ tests PASS

### Phase 19 Scope Honesty Checklist

- [ ] No claim of automatic failover
- [ ] No claim of automatic rebalancing
- [ ] No claim of shard migration
- [ ] No claim of data movement
- [ ] No claim of automatic rerouting on failure
- [ ] No Raft, no consensus, no quorum
- [ ] OpsScope returned in all results with all 8 flags true
- [ ] CLI commands include scope disclaimers in help text
- [ ] No files written by simulate-failure or plan-rebalance
- [ ] No live node calls made by simulate-failure or plan-rebalance
- [ ] health command exits 0 even if all nodes unhealthy (diagnostic only)

### Allowed claims (Phase 19)

- "Implemented health visibility and manual rebalance simulation for static clusters."
- "Implemented failure simulation showing routing impact for specified down nodes on sample keys."
- "Implemented manual rebalance planning showing key movement when nodes are added or removed."
- "All simulation and planning is pure computation — no live node calls, no data movement."

### Forbidden claims (Phase 19)

- ~~"Implemented automatic failover"~~
- ~~"Implemented automatic rebalancing"~~
- ~~"Implemented shard migration"~~
- ~~"Implemented resharding"~~
- ~~"Implemented self-healing cluster"~~
- ~~"Implemented distributed recovery"~~
- ~~"Implemented production fault tolerance"~~
- ~~"Nodes automatically reroute requests on failure"~~
