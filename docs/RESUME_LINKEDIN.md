# ShardForgeDB — Resume & LinkedIn Content

All content here complies with the safe claims list in `docs/CLAIMS.md`.

---

## Resume Bullets

### 2-bullet version

- Built **ShardForgeDB**, a 20-phase explainable Go database engine implementing a WAL-backed LSM-tree key-value store, exact vector search, real networked HTTP nodes, stateless routing proxy, explicit pull-based read replicas, and ops simulation tools — 700+ race-safe tests, reproducible benchmarks at every phase.
- Designed binary file formats (WAL records, SSTables, Bloom filters) with CRC checksums, index blocks, and crash-safe atomic creation; built a client-side consistent-hash routing gateway and stateless proxy layer over independent HTTP node processes.

### 3-bullet version

- Built **ShardForgeDB** from scratch in Go: WAL-backed LSM-tree engine (WAL, MemTable, SSTables, Bloom filters), manual full compaction with atomic manifest swap, and exact k-NN vector search (cosine, L2, dot product) with engine-backed persistence.
- Implemented a real networked node runtime (`shardforge-node`), client-side consistent-hash routing gateway, and stateless HTTP proxy over independent node processes; added explicit pull-based read replicas with in-memory mutation log and follower write rejection.
- Built ops simulation tools: cluster health visibility, failure impact simulation (no live calls), and manual rebalance planning (no data movement) — 700+ race-safe tests, 120+ benchmarks, and honest documentation across 25 packages.

### 4-bullet version

- Built **ShardForgeDB** (Go, 20 phases): WAL-backed LSM-tree engine with binary CRC-checksummed file formats, manual compaction, and WAL replay recovery — designed with crash-safety and correctness as primary constraints.
- Implemented exact k-NN vector search (cosine, L2, dot product) with namespace isolation and engine-backed persistence; built local consistent-hash sharding over multiple engines and local leader/follower replication simulation with pause/lag/catch-up controls.
- Designed real networked infrastructure: independent HTTP node processes, a client-side FNV-1a consistent-hash routing gateway with virtual nodes and weight support, a stateless HTTP proxy with 10 endpoints, and explicit pull-based read replicas backed by an in-memory mutation log.
- Added ops simulation tools (health polling, failure simulation, manual rebalance planning — all pure computation, no automatic failover); 700+ race-safe tests across 25 packages, 120+ reproducible benchmarks, and explicit scope documentation at every layer.

---

## LinkedIn Post

Meet ShardForgeDB — an explainable database engine I built layer by layer in Go.

Not another CRUD tutorial. This is a 20-phase, ground-up implementation of real database internals:

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

**What I'm most proud of:**

Every layer is honest about what it does and doesn't do. The codebase includes a `CLAIMS.md` with safe and forbidden claims. The ops layer explicitly returns scope flags (`manual_only`, `no_automatic_failover`, `no_consensus`) in every result. There's no Raft here — and the docs say so explicitly.

700+ race-safe tests. 120+ reproducible benchmarks. 25 packages, all green.

GitHub: https://github.com/YashPatel2395/ShardForgeDB

#Go #DatabaseInternals #SoftwareEngineering #OpenSource

---

## GitHub Description

### One-line description

Explainable Go database engine: WAL-backed LSM-tree, exact vector search, HTTP nodes, consistent-hash routing, read replicas, ops simulation — 20 phases, 700+ tests.

### Short paragraph (README tagline / repo About)

ShardForgeDB is a 20-phase Go database engine built for learning and portfolio purposes. It implements a WAL-backed LSM-tree key-value store, exact vector search, real networked HTTP node processes, a stateless routing proxy, explicit pull-based read replicas, and operations simulation tools. It does not implement Raft, consensus, automatic failover, or production fault tolerance. Every phase has tests, benchmarks, and honest scope documentation.

---

## Skills List

For resume skills section or LinkedIn skills — add these based on what was actually implemented:

**Languages / Tools:**
- Go (1.26, darwin/arm64)
- Docker, Docker Compose
- GitHub Actions (CI)

**Database internals:**
- Write-ahead log (WAL) design
- LSM-tree (Log-Structured Merge-tree)
- MemTable, SSTable, Bloom filter
- Manual full compaction
- WAL replay and crash recovery
- Binary file format design (CRC-32, index blocks, atomic file creation)

**Distributed systems concepts (educational implementation):**
- Consistent hashing (FNV-1a, virtual nodes, weight)
- Client-side routing gateway
- Stateless HTTP proxy
- Explicit pull-based replication
- Leader/follower roles and follower write rejection
- Ops simulation: health polling, failure impact, manual rebalance planning

**Software engineering:**
- Test-driven development (700+ race-safe tests)
- Benchmark-driven development (120+ reproducible benchmarks)
- Concurrent programming (sync.RWMutex, race detector on all CI runs)
- HTTP/JSON API design
- Binary protocol design
- Scope-honest documentation (explicit claims audit)
