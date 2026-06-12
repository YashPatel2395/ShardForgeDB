# ShardForgeDB — Demo Script

A guided 5–8 minute demo script for presenting the project to a recruiter, professor, or senior engineer.

---

## Before you start

Build binaries:
```bash
make build
```

---

## Step 1 — Explain the project goal (1 minute)

> "ShardForgeDB is a 23-phase explainable Go database engine I built from scratch.
> The goal was not to compete with Postgres or RocksDB — it was to deeply understand how
> database internals actually work by implementing each layer from scratch: WAL, MemTable,
> SSTables, Bloom filters, exact vector search, networked HTTP nodes, routing, replication,
> ops simulation, and a runtime explainability system.
>
> Every phase produces its own tests, benchmarks, and honest documentation. I'll walk you
> through the key layers."

---

## Step 2 — Show tests (30 seconds)

```bash
go test -race -count=1 ./... 2>&1 | tail -30
```

> "929 tests across 27 packages. Race detector on in every run. All pass."

---

## Step 3 — Build (15 seconds)

```bash
make build
ls -la bin/
```

> "Seven binaries: the main CLI, bench tool, dashboard, node, gateway, proxy, and cluster tool."

---

## Step 4 — Show the engine trace (2 minutes)

> "This is the feature I'm most proud of. Every key operation produces a real execution trace
> — not simulated, not hardcoded. The trace is built by the actual code running."

```bash
mkdir -p /tmp/sfdb-demo

# Write a key and trace it
./bin/shardforge explain --data-dir /tmp/sfdb-demo put user:1 alice
```

> "Notice: KEY_VALIDATED by the engine coordinator, WAL_APPEND with real nanosecond timing,
> MEMTABLE_PUT. These steps come from the actual write path code."

```bash
# Read it back — should show MEMTABLE_HIT
./bin/shardforge explain --data-dir /tmp/sfdb-demo get user:1
```

> "MEMTABLE_HIT because the key is still in memory. Now let me flush to disk and re-read."

```bash
# Flush to SSTable
./bin/shardforge-node --node-id demo --addr 127.0.0.1:19301 --data-dir /tmp/sfdb-demo &
NODE_PID=$!
sleep 0.5
curl -s -X POST http://127.0.0.1:19301/flush | jq .
./bin/shardforge explain --data-dir /tmp/sfdb-demo get user:1
kill $NODE_PID 2>/dev/null
```

> "Now you see BLOOM_CHECK and SSTABLE_HIT. The Bloom filter said 'maybe present' and the
> SSTable read confirmed it. This is the real LSM-tree read path."

---

## Step 5 — Show explain-node over HTTP (1 minute)

> "The trace system also works over the network. I can ask a running node to explain an
> operation via HTTP."

```bash
./bin/shardforge-node --node-id demo2 --addr 127.0.0.1:19302 --data-dir /tmp/sfdb-demo2 &
NODE_PID=$!
sleep 0.5

./bin/shardforge explain-node --addr http://127.0.0.1:19302 put mykey myval
./bin/shardforge explain-node --addr http://127.0.0.1:19302 get mykey

kill $NODE_PID 2>/dev/null
```

> "The HTTP node executes the real engine trace path and returns it as JSON. Every step in
> that response was produced by the actual code that performed the operation."

---

## Step 6 — Show the benchmark CLI (30 seconds)

```bash
./bin/shardforge-bench --scale small --out /tmp/demo-bench.md
cat /tmp/demo-bench.md
```

> "Six workloads: write-heavy, read-heavy, mixed, scan-heavy, compact, vector search.
> P50/P95/P99 latencies, bytes/op, allocs/op."

---

## Step 7 — Show the cluster tooling (1 minute)

```bash
./bin/shardforge-cluster validate configs/local-3node.json
./bin/shardforge-cluster print configs/local-3node.json
```

> "Static cluster config with validated JSON schema. The cluster tool can also simulate
> failure impact and plan manual rebalancing — all pure computation, no live node calls."

```bash
./bin/shardforge-cluster simulate-failure configs/local-failure-sim-3node.json \
  --down node-2 --key user:1 --key user:2 --key user:3
```

> "Notice the scope flags: manual_only=true, no_automatic_failover=true, no_consensus=true.
> The code is honest about what it does and doesn't do."

---

## Step 8 — Show claims audit (30 seconds)

```bash
head -60 docs/CLAIMS.md
```

> "docs/CLAIMS.md is the authoritative claims registry. Section A: what I can claim with
> evidence. Section B: what is explicitly forbidden to claim. This includes 'distributed
> database', 'Raft', 'consensus', 'ANN'. Every demo, README, and resume bullet must comply."

---

## Step 9 — Close (30 seconds)

> "To summarize: 23 phases from scratch, 929 race-safe tests, 120+ reproducible benchmarks,
> and a runtime explainability system that shows you exactly what the engine did for every
> operation — from local CLI to HTTP node API.
>
> What I care about most: the code is honest. The scope flags, the claims audit, the
> documentation — they all say exactly what this is and what it isn't."

---

## Demo cleanup

```bash
rm -rf /tmp/sfdb-demo /tmp/sfdb-demo2 /tmp/demo-bench.md
```
