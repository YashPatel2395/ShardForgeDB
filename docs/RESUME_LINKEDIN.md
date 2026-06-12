# ShardForgeDB — Resume & LinkedIn Content

All content here complies with the safe claims list in `docs/CLAIMS.md`.

---

## Resume Bullets

### 2-bullet version

- Built **ShardForgeDB**, a 23-phase explainable Go database engine implementing a WAL-backed LSM-tree key-value store, exact vector search, real networked HTTP nodes, stateless routing proxy, explicit pull-based read replicas, ops simulation tools, and a real-execution trace system — 929 race-safe tests, reproducible benchmarks at every phase.
- Designed binary file formats (WAL records, SSTables, Bloom filters) with CRC checksums, index blocks, and crash-safe atomic creation; built client-side consistent-hash routing, a stateless proxy, and an HTTP explain API that exposes real engine execution traces per operation.

### 3-bullet version

- Built **ShardForgeDB** from scratch in Go: WAL-backed LSM-tree engine (WAL, MemTable, SSTables, Bloom filters), manual full compaction with atomic manifest swap, and exact k-NN vector search (cosine, L2, dot product) with engine-backed persistence.
- Implemented a real networked node runtime (`shardforge-node`), client-side consistent-hash routing gateway, stateless HTTP proxy, and explicit pull-based read replicas with in-memory mutation log; added ops simulation tools for health visibility, failure impact, and rebalance planning.
- Built a runtime explainability system: `ExplainGet/Put/Delete/Scan` produce real per-operation execution traces (WAL_APPEND, MEMTABLE_HIT, BLOOM_SKIP, SSTABLE_HIT) exposed via local CLI and HTTP node endpoints — 929 race-safe tests, 120+ benchmarks, honest scope documentation at every layer.

### 4-bullet version

- Built **ShardForgeDB** (Go, 23 phases): WAL-backed LSM-tree engine with binary CRC-checksummed file formats, manual compaction, and WAL replay recovery — designed with crash-safety and correctness as primary constraints.
- Implemented exact k-NN vector search (cosine, L2, dot product) with namespace isolation and engine-backed persistence; built local consistent-hash sharding over multiple engines and local leader/follower replication simulation with pause/lag/catch-up controls.
- Designed real networked infrastructure: independent HTTP node processes, a client-side FNV-1a consistent-hash routing gateway with virtual nodes and weight support, a stateless HTTP proxy with 10 endpoints, and explicit pull-based read replicas backed by an in-memory mutation log.
- Built a runtime explainability layer: `ExplainGet/Put/Delete/Scan` trace real execution paths through WAL, MemTable, Bloom filters, and SSTables; HTTP `/explain/*` endpoints expose traces from live nodes; `shardforge explain-node` CLI calls nodes over HTTP — 929 race-safe tests, 120+ reproducible benchmarks, and explicit scope documentation at every layer.

---

## LinkedIn Post

Meet ShardForgeDB — an explainable database engine I built layer by layer in Go.

Not another CRUD tutorial. This is a 23-phase, ground-up implementation of real database internals:

**Storage layer:**
- Write-ahead log (WAL) with CRC checksums and crash-safe replay
- MemTable: ordered concurrent in-memory write buffer
- SSTables: sorted immutable on-disk segments with index blocks and atomic creation
- Bloom filters: FNV-1a double hashing with configurable false positive rate
- LSM-tree engine wiring it all together with a manifest and compaction

**Search:**
- Exact k-nearest-neighbour vector search (cosine, L2, dot product) backed by the engine

**Networking:**
- Real independent HTTP node processes — each a complete database node
- Client-side consistent-hash routing gateway
- Stateless HTTP routing proxy
- Explicit pull-based read replicas with in-memory mutation log

**Operations:**
- Cluster health visibility
- Failure impact simulation (pure ring computation — no live calls)
- Manual rebalance planning (no data movement — honest operator output)

**Explainability (the part I'm most proud of):**
- `ExplainGet` walks the real MemTable→Bloom→SSTable path and records every step with wall-clock timing
- `ExplainPut` records WAL_APPEND and MEMTABLE_PUT from the actual write code
- HTTP `/explain/*` endpoints on every node expose these traces over the network
- `shardforge explain-node` CLI calls any live node and prints its real execution trace
- Every step is produced by the real code that performed the operation — no fabricated output

What I'm most proud of: the codebase is explicit about what it does and doesn't do. `docs/CLAIMS.md` lists safe and forbidden claims. The ops layer returns `manual_only`, `no_automatic_failover`, `no_consensus` flags in every result. There's no Raft here — and the docs say so explicitly.

929 race-safe tests. 120+ reproducible benchmarks. 27 packages, all green.

GitHub: https://github.com/YashPatel2395/ShardForgeDB

#Go #DatabaseInternals #SoftwareEngineering #OpenSource

---

## GitHub Description

### One-line description

Explainable Go database engine: WAL-backed LSM-tree, exact vector search, HTTP nodes, consistent-hash routing, read replicas, ops simulation, real execution traces — 23 phases, 929 tests.

### Short paragraph (README tagline / repo About)

ShardForgeDB is a 23-phase Go database engine built for learning and portfolio purposes. It implements a WAL-backed LSM-tree key-value store, exact vector search, real networked HTTP node processes, a stateless routing proxy, explicit pull-based read replicas, operations simulation tools, and a runtime explainability system that traces real engine execution paths. It does not implement Raft, consensus, automatic failover, or production fault tolerance. Every phase has tests, benchmarks, and honest scope documentation.

---

## Skills List

**Languages:** Go

**Storage:** LSM-tree, WAL, MemTable, SSTable, Bloom filter, binary file formats, CRC checksums, crash-safe atomic creation

**Algorithms:** FNV-1a consistent hashing, virtual node ring, exact k-NN (cosine/L2/dot), binary search on dense index

**Networking:** HTTP/JSON server and client, independent node processes, stateless proxy, client-side routing

**Distributed systems concepts (simulated):** leader/follower replication, explicit pull-based sync, failure simulation, rebalance planning

**Explainability:** per-operation execution traces, real wall-clock timing, HTTP trace API, trace CLI

**Testing:** race detector (`-race`), table-driven tests, concurrent tests, 929 tests across 27 packages

**Tooling:** Go modules, GitHub Actions CI, Docker Compose, Makefile, reproducible benchmarks
