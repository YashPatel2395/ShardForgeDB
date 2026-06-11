# ShardForgeDB — Demo Script

A guided 5–8 minute demo script for presenting the project to a recruiter, professor, or senior engineer.

---

## Before you start

Build binaries:
```bash
make build
```

Make sure Docker is running if you want to demo the networked node sections.

---

## Step 1 — Explain the project goal (1 minute)

> "ShardForgeDB is a 20-phase explainable Go database engine I built from scratch.
> The goal was not to compete with Postgres or RocksDB — it was to deeply understand how
> database internals actually work by implementing each layer from scratch: WAL, MemTable,
> SSTables, Bloom filters, exact vector search, networked HTTP nodes, routing, replication,
> and ops simulation.
>
> Every phase produces its own tests, benchmarks, and honest documentation. I'll walk you
> through the key layers in a few minutes."

---

## Step 2 — Show tests (30 seconds)

```bash
go test -race -count=1 ./... 2>&1 | tail -30
```

> "700+ tests across 25 packages. Race detector on in every run. All pass."

---

## Step 3 — Build (15 seconds)

```bash
make build
ls -la bin/
```

> "Seven binaries: the main CLI, bench tool, dashboard, node, gateway, proxy, and cluster tool."

---

## Step 4 — Single-node + benchmark (1 minute)

```bash
# Start a node
./bin/shardforge-node --node-id node-1 --addr 127.0.0.1:9101 --data-dir /tmp/sfdb-node-1 &
sleep 0.5

# Write and read
curl -s http://127.0.0.1:9101/healthz
curl -s -X PUT http://127.0.0.1:9101/kv/user:1 \
     -H "Content-Type: application/json" -d '{"value":"alice"}' | python3 -m json.tool
curl -s http://127.0.0.1:9101/kv/user:1 | python3 -m json.tool

kill %1
```

> "Each node is a real independent HTTP process backed by a local LSM-tree engine.
> WAL ensures durability. MemTable buffers writes. SSTables persist data.
> Bloom filters skip unnecessary disk reads."

```bash
# Benchmark
./bin/shardforge-bench --scale small --out /tmp/bench.md
cat /tmp/bench.md | head -60
```

> "Six workloads: write-heavy, read-heavy, mixed, scan, compaction, restart.
> Results include P50/P95/P99 latency. Everything is reproducible."

---

## Step 5 — Networked 3-node + proxy (2 minutes)

```bash
# Start Docker Compose (3 independent nodes + stateless routing proxy)
docker compose -f deploy/docker-compose.yml up --build -d
sleep 3

# All nodes healthy
curl -s http://localhost:9101/healthz
curl -s http://localhost:9102/healthz
curl -s http://localhost:9103/healthz

# Consistent-hash routing — same key always goes to same node
curl -s http://localhost:9200/route/user:1
curl -s http://localhost:9200/route/user:1   # same node
curl -s http://localhost:9200/route/order:5  # different node

# Put and get via proxy
curl -s -X PUT http://localhost:9200/kv/user:1 \
     -H "Content-Type: application/json" -d '{"value":"alice"}' | python3 -m json.tool
curl -s http://localhost:9200/kv/user:1 | python3 -m json.tool

docker compose -f deploy/docker-compose.yml down -v
```

> "The proxy uses FNV-1a consistent hashing to route every key to the same node.
> There's no Raft, no consensus — nodes are independent. If a node goes down,
> the proxy returns 502 immediately. No retry to another node, because without
> replication, the key only exists on one node."

---

## Step 6 — Read-replica sync (1.5 minutes)

```bash
# Start 1-primary + 2-replica + proxy
docker compose -f deploy/docker-compose-replica.yml up --build -d
sleep 3

# Write to primary
curl -s -X PUT http://localhost:9111/kv/user:1 \
     -H "Content-Type: application/json" -d '{"value":"alice"}' | python3 -m json.tool

# Follower doesn't have it yet (replication is on-demand, not automatic)
curl -s http://localhost:9112/kv/user:1 | python3 -m json.tool

# Check replication status
curl -s http://localhost:9210/replication/status | python3 -m json.tool

# Sync replica-1 explicitly
curl -s -X POST http://localhost:9210/replication/sync-node/node-replica-1 | python3 -m json.tool

# Now replica-1 has it
curl -s http://localhost:9112/kv/user:1 | python3 -m json.tool

# Writes to follower are rejected (403)
curl -s -X PUT http://localhost:9112/kv/user:1 \
     -H "Content-Type: application/json" -d '{"value":"bob"}'

docker compose -f deploy/docker-compose-replica.yml down -v
```

> "Replication is explicit pull-based. The primary keeps an in-memory mutation log.
> Followers sync on demand via HTTP — no background goroutine, no Raft, no automatic failover.
> Follower write rejection is enforced with 403. This is honest about what Phase 18 implements."

---

## Step 7 — Ops simulation (1 minute)

```bash
# No live nodes needed — pure ring computation

# Simulate node-2 going down
./bin/shardforge-cluster simulate-failure configs/local-failure-sim-3node.json \
  --down node-2 --key user:1 --key user:2 --key order:9 | python3 -m json.tool

# Plan manual rebalance after removing node-2
./bin/shardforge-cluster plan-rebalance configs/local-failure-sim-3node.json \
  --remove node-2 --key user:1 --key user:2 --key order:9 | python3 -m json.tool
```

> "No live node calls. This is pure ring computation — it shows which keys are affected
> and generates a step-by-step operator plan for recovery. The scope object in the output
> has all flags true: manual_only, no_automatic_failover, no_data_movement.
> Real failover would require Raft or a consensus protocol — that's clearly out of scope."

---

## Step 8 — Explain limitations honestly (30 seconds)

> "To be clear about what this is not:
>
> - No Raft, no consensus, no quorum
> - No automatic failover — all recovery is manual
> - No data movement between nodes — rebalancing is planning-only
> - No distributed transactions
> - Vector search is exact brute-force, not ANN
> - Compaction is manual, not automatic
>
> These are intentional design choices for an educational project. The goal was
> to understand each layer deeply, not to build a production system."

---

## Step 9 — Close with engineering impact (30 seconds)

> "What I got from this:
>
> - Real understanding of LSM-tree mechanics — WAL, MemTable, SSTable, compaction
> - Experience designing binary file formats with CRC, index blocks, atomic writes
> - HTTP systems design at the node, gateway, and proxy level
> - Replication semantics: sequence numbers, pull-based sync, follower state tracking
> - Test-driven development: 700+ race-safe tests, reproducible benchmarks at every phase
> - Honest documentation discipline: scope flags, forbidden claims list
>
> The project is 20 phases, fully tested, benchmarked, and documented.
> See docs/FINAL_REPORT.md for the full engineering summary."

---

## Tips for presenting

- Use `python3 -m json.tool` to pretty-print JSON output on the fly
- Have Docker Compose running before the demo starts to save time
- If Docker is unavailable, the ops simulation demo (Step 7) works without any running nodes
- The benchmark (Step 4) takes ~30 seconds for `--scale small` — run it before presenting
