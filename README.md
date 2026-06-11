# ShardForgeDB

**An explainable Go database engine built layer-by-layer, from WAL-backed storage to SSTables, Bloom filters, exact vector search, HTTP nodes, client-side routing, read-replica sync, and ops simulation.**

Status: `v0.2.0` portfolio release candidate — 20-phase build complete.

---

## What it implements

| Layer | Feature |
|---|---|
| Storage | WAL-backed durability (CRC-checksummed, binary, little-endian) |
| Storage | MemTable (ordered, concurrent in-memory write buffer) |
| Storage | SSTables (sorted, immutable on-disk segments with index + Bloom sidecar) |
| Storage | Bloom filter (FNV-1a double hashing, deterministic, serializable) |
| Storage | Single-node LSM-tree Engine (WAL + MemTable + SSTables + Bloom) |
| Storage | Manual full compaction with atomic manifest swap |
| Benchmarking | Six-workload benchmark CLI with P50/P95/P99 latency and Markdown output |
| Search | Exact vector search (cosine / L2 / dot product, engine-backed persistence) |
| Simulation | Local consistent-hash sharding over multiple in-process engines |
| Simulation | Local leader/follower replication with pause/lag/catch-up controls |
| Simulation | Local HTTP dashboard with chaos scenarios and timeline events |
| Network | Real networked HTTP node runtime (`shardforge-node`), Docker Compose 3-node demo |
| Network | Client-side consistent-hash routing gateway (`shardforge-gateway`) |
| Network | Stateless HTTP routing proxy (`shardforge-proxy`) |
| Config | Static cluster metadata (`internal/cluster`, JSON config, `shardforge-cluster` CLI) |
| Replication | Explicit pull-based read replicas v1 (`internal/replnet`) |
| Ops | Health check, failure simulation, manual rebalance planning (`internal/ops`) |

## What it does NOT implement

- No Raft, no Paxos, no consensus
- No quorum replication, no automatic failover
- No automatic rebalancing, no shard migration, no real data movement between nodes
- No dynamic membership, no service discovery, no gossip
- No distributed transactions
- No ANN / HNSW / IVF (vector search is exact brute-force only)
- No background compaction (manual only)
- No production fault tolerance

---

## Quickstart

```bash
# Run all tests (race detector on)
go test ./...

# Build all 7 binaries into bin/
make build

# Help
./bin/shardforge --help
./bin/shardforge-bench --help
./bin/shardforge-node --help
./bin/shardforge-gateway --help
./bin/shardforge-proxy --help
./bin/shardforge-cluster --help
```

---

## Single-node demo

```bash
# Start a node
./bin/shardforge-node --node-id node-1 --addr 127.0.0.1:9101 --data-dir /tmp/sfdb-node-1

# In another terminal:
curl -s http://127.0.0.1:9101/healthz
curl -s -X PUT http://127.0.0.1:9101/kv/user:1 -H "Content-Type: application/json" -d '{"value":"alice"}'
curl -s http://127.0.0.1:9101/kv/user:1
curl -s -X DELETE http://127.0.0.1:9101/kv/user:1

# Run benchmark report (small scale)
./bin/shardforge-bench --scale small --out /tmp/bench.md
```

---

## Networked 3-node + proxy demo

```bash
# Start Docker Compose (3 independent nodes + stateless proxy)
make node-demo

# Nodes and proxy health
curl http://localhost:9101/healthz
curl http://localhost:9102/healthz
curl http://localhost:9103/healthz
curl http://localhost:9200/healthz

# Route a key (shows which node wins, deterministic)
curl http://localhost:9200/route/user:1

# Put/get via proxy (routes to correct node automatically)
curl -X PUT http://localhost:9200/kv/user:1 -H "Content-Type: application/json" -d '{"value":"alice"}'
curl http://localhost:9200/kv/user:1

# Tear down
make node-demo-down
```

---

## Read-replica demo

```bash
# Start 1-primary + 2-replica + proxy
make replica-demo

# Write to primary
curl -X PUT http://localhost:9111/kv/user:1 -H "Content-Type: application/json" -d '{"value":"alice"}'

# Check replication status (follower lag expected)
curl http://localhost:9210/replication/status

# Sync replica-1 explicitly
curl -X POST http://localhost:9210/replication/sync-node/node-replica-1

# Verify replica-1 has the value
curl http://localhost:9112/kv/user:1

# Tear down
make replica-demo-down
```

---

## Ops simulation demo

These commands run **without live nodes** — pure static ring computation:

```bash
# Check cluster health (diagnostic; exits 0 even if nodes are down)
./bin/shardforge-cluster health configs/local-failure-sim-3node.json

# Simulate node-2 failure: show routing impact on sample keys
./bin/shardforge-cluster simulate-failure configs/local-failure-sim-3node.json \
  --down node-2 --key user:1 --key user:2 --key order:9

# Plan manual rebalance after removing node-2
./bin/shardforge-cluster plan-rebalance configs/local-failure-sim-3node.json \
  --remove node-2 --key user:1 --key user:2 --key order:9
```

Output includes a `scope` object with all flags true:
`manual_only`, `simulation_only`, `no_automatic_failover`, `no_data_movement`, etc.

---

## Architecture overview

```
┌─────────────────────────────────────────────────────┐
│  shardforge-cluster CLI  (ops: health, simulate, plan)│
│  shardforge-proxy        (HTTP routing proxy)         │
│  shardforge-gateway      (client-side ring routing)   │
└───────────────────┬─────────────────────────────────┘
                    │ HTTP/JSON
┌───────────────────▼─────────────────────────────────┐
│  shardforge-node  (independent HTTP node processes)   │
│    primary │ follower-1 │ follower-2                  │
│  internal/replnet  (in-memory mutation log + pull)    │
└───────────────────┬─────────────────────────────────┘
                    │
┌───────────────────▼─────────────────────────────────┐
│  Engine  (LSM-tree key-value + vector)               │
│    WAL → MemTable → SSTables → Bloom filters         │
│    Manual compaction   Vector index (exact k-NN)     │
└─────────────────────────────────────────────────────┘
┌─────────────────────────────────────────────────────┐
│  Simulation layers (single-process, no networking)   │
│    Shard (FNV-1a ring)   Replica (op-log)            │
│    Dashboard (HTTP + chaos scenarios)                 │
│    Ops (health checks, failure sim, rebalance plan)  │
└─────────────────────────────────────────────────────┘
```

---

## Phase timeline

| Phase | What was built |
|---|---|
| 1 | CLI skeleton, config loading, structured logging, CI |
| 2 | WAL — append-only CRC-checksummed write-ahead log |
| 3 | MemTable — ordered concurrent in-memory write buffer |
| 4 | SSTable — sorted immutable on-disk segment format |
| 5 | Bloom filter — probabilistic membership, binary serialization |
| 6 | Engine — single-node LSM-tree (WAL + MemTable + SSTable + Bloom) |
| 7 | Manual full compaction with atomic manifest swap |
| 8 | Benchmark CLI with six workloads, P50/P95/P99 latency |
| 9 | Exact vector search (cosine / L2 / dot, engine-backed) |
| 10 | Local FNV-1a consistent-hash sharding over multiple engines |
| 11 | Local leader/follower replication simulation |
| 12 | Local HTTP dashboard and chaos/failure scenarios |
| 13 | Polish — release scripts, docs, Makefile hardening |
| 14 | Real networked HTTP node runtime + Docker Compose 3-node |
| 15 | Client-side consistent-hash routing gateway |
| 16 | Stateless HTTP routing proxy (10 endpoints) |
| 17 | Static cluster metadata (typed JSON config, `shardforge-cluster` CLI) |
| 18 | Networked read replicas v1 (pull-based, in-memory mutation log) |
| 19 | Ops simulation — health checks, failure sim, rebalance planning |
| 20 | Final polish — architecture docs, claims audit, launch readiness |

---

## Engineering skills demonstrated

- **Storage engine design** — LSM-tree from scratch: WAL, MemTable, SSTable, Bloom, compaction
- **File format engineering** — binary formats with CRC, index blocks, magic numbers, atomic swap
- **Probabilistic data structures** — Bloom filter with double hashing, configurable FPR
- **Exact vector search** — cosine / L2 / dot product, engine-backed persistence
- **HTTP systems design** — real networked nodes, proxy, gateway, REST/JSON API
- **Deterministic routing** — FNV-1a consistent-hash ring, virtual nodes, weight support
- **Replication semantics** — primary/follower roles, monotonic sequences, explicit pull sync
- **Ops simulation** — health visibility, failure impact simulation, manual rebalance planning
- **Benchmark-driven development** — reproducible Go benchmarks with allocation tracking
- **Test-driven development** — 700+ tests, race detector on every run
- **Honest documentation** — scope flags in every component, forbidden claims list

---

## Build and test

```bash
make build       # compile all 7 binaries
make test        # go test -race -count=1 ./...
make vet         # go vet ./...
make bench-ops   # ops package benchmarks
make bench-replnet # replnet benchmarks
make bench-cluster # cluster benchmarks
make final-smoke # fast end-to-end smoke validation
```

---

## Config validation

```bash
./bin/shardforge-cluster validate configs/local-3node.json
./bin/shardforge-cluster validate configs/local-read-replica-3node.json
./bin/shardforge-cluster validate configs/local-failure-sim-3node.json
```

---

## See also

- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) — full architecture walkthrough
- [`docs/CLAIMS.md`](docs/CLAIMS.md) — safe and forbidden claims
- [`docs/DESIGN.md`](docs/DESIGN.md) — per-phase design decisions
- [`docs/PROOF.md`](docs/PROOF.md) — per-phase validation evidence
- [`docs/BENCHMARKS.md`](docs/BENCHMARKS.md) — reproducible benchmark results
- [`docs/FINAL_REPORT.md`](docs/FINAL_REPORT.md) — engineering summary report
- [`docs/RESUME_LINKEDIN.md`](docs/RESUME_LINKEDIN.md) — recruiter-ready descriptions
- [`docs/ROADMAP.md`](docs/ROADMAP.md) — future work
- [`docs/DEMO_SCRIPT.md`](docs/DEMO_SCRIPT.md) — 5-minute demo guide
