# ShardForgeDB — Phase 24 Local Cluster Demo

**Phase 24 — Reproducible Multi-Node Local Cluster Demo**

This document describes the Phase 24 demo: a clean, reproducible three-node cluster
running as independent local processes with a stateless routing proxy.

---

## Scope

This is a **local multi-node HTTP demo with static metadata and client/proxy routing**.

| Feature | Status |
|---------|--------|
| 3 independent HTTP nodes | ✓ implemented |
| Stateless routing proxy | ✓ implemented (shardforge-proxy) |
| FNV-1a consistent-hash routing | ✓ implemented (shardforge-gateway) |
| Separate data directories per node | ✓ implemented (each node is independent) |
| Health endpoints | ✓ implemented (/healthz on every node + proxy) |
| Status endpoints | ✓ implemented (/status on every node + proxy) |
| explain-node over HTTP | ✓ implemented (Phase 23) |
| Real distributed cluster | ✗ NOT implemented |
| Raft / Paxos / consensus | ✗ NOT implemented |
| Quorum replication | ✗ NOT implemented |
| Automatic failover | ✗ NOT implemented |
| Shard migration | ✗ NOT implemented |
| Dynamic membership | ✗ NOT implemented |
| Background compaction | ✗ NOT implemented |
| ANN vector search | ✗ NOT implemented |
| Distributed tracing | ✗ NOT implemented |

**Routing behavior:**
- Requests are routed by the proxy using FNV-1a consistent hashing over the static node ring.
- Each key routes to exactly one node, deterministically.
- If the routed node is down, the proxy returns 502 (no retry, no failover, by design).
- No data movement. Routing changes when the ring topology changes (add/remove node).

---

## Quick Start

```bash
# Build all binaries first
make build

# Start the demo cluster (3 nodes + proxy, local processes)
make cluster-demo-up

# Run the smoke test
make cluster-demo-smoke

# Tear down
make cluster-demo-down
```

Or run the scripts directly:

```bash
./scripts/demo_cluster_up.sh
./scripts/demo_cluster_smoke.sh
./scripts/demo_cluster_down.sh
```

---

## Node Configuration

| Node | ID | Port | Data Directory |
|------|-----|------|---------------|
| Node 1 | `node-1` | `127.0.0.1:9101` | `/tmp/sfdb-demo-node-1` |
| Node 2 | `node-2` | `127.0.0.1:9102` | `/tmp/sfdb-demo-node-2` |
| Node 3 | `node-3` | `127.0.0.1:9103` | `/tmp/sfdb-demo-node-3` |
| Proxy  | —  | `127.0.0.1:9200` | — (stateless) |

Config file: `configs/cluster/demo-3node.json`

---

## Key Placement Proof

Routing is deterministic for a fixed ring topology. Sample keys for the 3-node demo:

```
user:1  → node-2  (http://127.0.0.1:9102)
user:2  → node-2  (http://127.0.0.1:9102)
order:9 → node-1  (http://127.0.0.1:9101)
```

Show routing for any key (no network call — pure ring computation):

```bash
./bin/shardforge-gateway --config configs/cluster/demo-3node.json route user:1
./bin/shardforge-gateway --config configs/cluster/demo-3node.json route user:2
./bin/shardforge-gateway --config configs/cluster/demo-3node.json route order:9
./bin/shardforge-gateway --config configs/cluster/demo-3node.json route item:42
```

**Important scope note:**
- Routing is static — computed purely from the FNV-1a hash ring.
- Routing will change if a node is added or removed (consistent-hash behavior — expected).
- There is NO automatic rebalancing. Data does NOT move when routing changes.
- To use a different ring size, update the config and restart the cluster.

---

## Data Isolation Proof

Each node uses its own independent data directory. There is **no replication**.

Writing to `node-1` directly does NOT make the data available on `node-2` or `node-3`:

```bash
# Write directly to node-1
curl -X PUT http://127.0.0.1:9101/kv/iso:test \
  -H "Content-Type: application/json" \
  -d '{"value":"isolation-proof"}'

# node-1 has it
curl http://127.0.0.1:9101/kv/iso:test
# → {"value":"isolation-proof"}

# node-2 does NOT have it (404 — no replication)
curl -v http://127.0.0.1:9102/kv/iso:test
# → 404 Not Found

# node-3 does NOT have it (404 — no replication)
curl -v http://127.0.0.1:9103/kv/iso:test
# → 404 Not Found
```

This is the **expected and correct** behavior. There is no replication. Each node is independent.

To have data available via the proxy, write it through the proxy:

```bash
curl -X PUT http://127.0.0.1:9200/kv/user:1 \
  -H "Content-Type: application/json" \
  -d '{"value":"alice"}'
# The proxy routes user:1 → node-2 and writes there.
# Subsequent GET user:1 via proxy also routes to node-2 and returns alice.
```

---

## Put/Get Through Proxy

```bash
# Write (proxy routes to the consistent-hash node)
curl -X PUT http://127.0.0.1:9200/kv/user:1 \
  -H "Content-Type: application/json" \
  -d '{"value":"alice"}'

# Read (same routing, same node)
curl http://127.0.0.1:9200/kv/user:1
# → {"value":"alice"}

# Route check (show which node handles user:1)
curl http://127.0.0.1:9200/route/user:1

# All nodes health via proxy fanout
curl http://127.0.0.1:9200/nodes/health
```

---

## explain-node: Runtime Trace Over HTTP

Each node exposes HTTP explain endpoints that return real execution traces.

```bash
# Trace a PUT on node-1 over HTTP
./bin/shardforge explain-node --addr http://127.0.0.1:9101 put mykey myvalue

# Trace a GET on node-1 over HTTP
./bin/shardforge explain-node --addr http://127.0.0.1:9101 get mykey
```

Example output (PUT trace):
```json
{
  "operation": "PUT",
  "key": "mykey",
  "steps": [
    {"component":"ENGINE","step_type":"KEY_VALIDATED","status":"OK"},
    {"component":"WAL","step_type":"WAL_APPEND","status":"OK","duration_ns":12450},
    {"component":"MEMTABLE","step_type":"MEMTABLE_PUT","status":"OK","duration_ns":2100}
  ]
}
```

---

## Docker Alternative

The Docker Compose 3-node demo (from Phase 16) also works as an alternative:

```bash
# Build and start via Docker Compose (3 independent nodes + proxy)
docker compose -f deploy/docker-compose.yml up --build

# Nodes: 9101, 9102, 9103 — Proxy: 9200
curl http://localhost:9200/healthz
docker compose -f deploy/docker-compose.yml down -v
```

The Phase 24 local scripts are the primary demo path (no Docker required).

---

## Cluster Config

The demo uses `configs/cluster/demo-3node.json`. To validate it:

```bash
./bin/shardforge-cluster validate configs/cluster/demo-3node.json
./bin/shardforge-cluster print   configs/cluster/demo-3node.json
```

The config's `scope` block honestly documents all limitations:
```json
"scope": {
  "static_config_only": true,
  "no_dynamic_membership": true,
  "no_discovery": true,
  "no_consensus": true,
  "no_raft": true,
  "no_replication": true,
  "no_failover": true,
  "no_shard_migration": true,
  "no_distributed_txns": true
}
```

---

## Honest Limitations

- **No automatic failover.** If a node dies, the proxy returns 502 for keys routed to it. Manual intervention required.
- **No shard migration.** If the ring changes (node added/removed), keys re-route to different nodes but data stays on the old node. Data is NOT moved automatically.
- **No replication.** A key written to node-1 is only on node-1. If node-1 fails, that data is unavailable.
- **No consensus.** There is no Raft, no Paxos, no quorum. Nodes are fully independent.
- **No distributed tracing.** The `explain-node` API traces one node's execution path. There is no cross-node trace propagation.
- **Static routing only.** Ring topology is set at startup via the config file. No dynamic membership.
- **No background compaction.** Each node uses manual compaction only.

---

## Safe Claim

The following claim is safe for portfolio/resume use:

> "Reproducible local three-node HTTP cluster demo with static FNV-1a consistent-hash routing, stateless proxy, and per-node explain trace API."

The following claims are NOT safe and must not be made:

- "Real distributed database"
- "Distributed consensus" / Raft / Paxos
- "Quorum replication"
- "Automatic failover" / "self-healing"
- "Shard migration"
- "Dynamic cluster membership"
- "Distributed tracing"

---

## Phase 25 — Networked Pull-Based Replication Demo

Phase 25 adds a reproducible leader+follower HTTP replication demo. See scripts and config below.

### Scope

| What it IS | What it is NOT |
|---|---|
| Explicit pull-based replication | Raft or consensus replication |
| Operator-triggered sync | Automatic background sync |
| PUT and DELETE mutation replication | Quorum replication |
| Idempotent pull (safe to re-run) | Leader election |
| Follower write rejection (403) | Automatic failover |
| Real HTTP node processes | Shared memory / same process |
| In-memory replication cursor | Persistent replication cursor |

### Quick start

```bash
make repl-demo-up
make repl-demo-smoke
make repl-demo-down
```

### Manual demo sequence

```bash
# Start leader and follower
./scripts/repl_demo_up.sh

# Write to leader
curl -X PUT http://127.0.0.1:9301/kv/hello \
  -H 'Content-Type: application/json' \
  -d '{"value":"world"}'

# Confirm follower does NOT have it yet (no auto-sync)
curl http://127.0.0.1:9302/kv/hello
# → {"found":false,"key":"hello","node_id":"follower"}

# Explicit pull (operator-triggered)
curl -X POST http://127.0.0.1:9302/replication/sync
# → {"ok":true,"fetched":1,"applied":1,"last_applied_seq":1,
#    "source_node":"http://127.0.0.1:9301","follower_node":"follower",...}

# Confirm follower now has the key
curl http://127.0.0.1:9302/kv/hello
# → {"found":true,"key":"hello","value":"world","node_id":"follower"}

# Second pull is idempotent
curl -X POST http://127.0.0.1:9302/replication/sync
# → {"ok":true,"fetched":0,"applied":0,"last_applied_seq":1,...}

# Tear down
./scripts/repl_demo_down.sh
```

### Replication cursor

The follower's replication cursor (`last_applied_seq`) is **in-memory only**.

- It resets to 0 on follower restart.
- After restart, the next sync will re-apply all mutations from seq 1 onward.
- This is safe because `ApplyReplicationEntries` is idempotent for already-seen seqs.

> "Replication cursor is demo-scoped and not production durable."

### Safe claim (Phase 25)

> "Networked explicit pull-based replication demo between HTTP nodes."

### Still unsafe (Phase 25)

- Automatic replication, background sync
- Raft, consensus, quorum replication
- Persistent replication cursor
- Leader election, automatic failover
- Distributed transactions, distributed tracing

---

## Phase 27 — Automatic Background Pull Replication Demo

Phase 27 adds automatic background pull replication. The follower polls the primary every 500ms without any manual operator trigger.

### Scope

| What it IS | What it is NOT |
|---|---|
| Automatic background polling (500ms interval) | Raft or consensus replication |
| Configurable interval / backoff / jitter | Automatic failover |
| Exponential backoff on failure | Quorum replication |
| Terminal blocked state on gap detection | Leader election |
| Lag tracking (`lag_entries`, `lag_known`) | Write forwarding to primary |
| Durable cursor survives follower restart | Background compaction |
| Manual sync still available alongside auto | Strong consistency |
| Follower rejects writes (403) | Dynamic membership |

### Quick start

```bash
make build
make repl-auto-demo-up
make repl-auto-demo-smoke
make repl-auto-demo-down
```

### Manual demo sequence

```bash
# Start leader (9501) and follower with background sync (9502)
./scripts/repl_auto_demo_up.sh

# Write to leader — follower will auto-sync within ~500ms
curl -X PUT http://127.0.0.1:9501/kv/hello \
  -H 'Content-Type: application/json' \
  -d '{"value":"world"}'

# Wait ~1s, then check follower (no manual sync needed)
sleep 1
curl http://127.0.0.1:9502/kv/hello
# → {"found":true,"key":"hello","value":"world","node_id":"follower"}

# Check background sync status and lag
curl -s http://127.0.0.1:9502/replication/status | python3 -m json.tool
# Shows: background_sync.state=running, lag_entries=0, lag_known=true

# Manual sync is still available alongside auto
curl -X POST http://127.0.0.1:9502/replication/sync

# Tear down
./scripts/repl_auto_demo_down.sh
```

### CLI flags (Phase 27)

```bash
# Start follower with background sync
./bin/shardforge-node \
  --node-id follower \
  --addr 127.0.0.1:9502 \
  --data-dir /tmp/sfdb-follower \
  --replication-role follower \
  --primary-url http://127.0.0.1:9501 \
  --bg-sync \
  --bg-sync-interval 500ms \
  --bg-sync-request-timeout 2s \
  --bg-sync-initial-backoff 250ms \
  --bg-sync-max-backoff 5s \
  --bg-sync-jitter-fraction 0.10
```

### Lag tracking

After any successful sync (even if no entries were fetched), `lag_known=true` and `lag_entries` reflects the gap between the primary's latest sequence and the follower's last applied sequence. `lag_known=false` only after a failed sync (can't reach primary).

### Safe claim (Phase 27)

> "Automatic background pull replication with configurable interval (500ms), exponential backoff, bounded jitter, lag tracking, and terminal gap detection. NOT Raft. NOT consensus. NOT automatic failover."

### Still unsafe (Phase 27)

- Raft, consensus, quorum replication
- Leader election, automatic failover
- Write forwarding
- Distributed transactions, distributed tracing
- Strong consistency (follower may lag primary)
