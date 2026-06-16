# ShardForgeDB — Claims Audit

**Phase 28 — Manual Promotion and Controlled Failover (Hardening Pass 2)**

This file is the authoritative record of what ShardForgeDB can and cannot claim. All documentation, README copy, demo scripts, and recruiter materials must comply with this list. If a claim does not appear in Section A, it must not be made.

---

## A. Safe Claims

The following are accurate, honest descriptions of what exists in the codebase with tests and benchmarks to back them up.

| Claim | Evidence |
|---|---|
| WAL-backed key-value engine | `internal/wal` — CRC-32-checksummed binary WAL, `Append`/`Replay`, partial-tail handling, corruption detection |
| MemTable | `internal/memtable` — sorted, concurrent in-memory write buffer, tombstones, size accounting |
| SSTable | `internal/sstable` — sorted immutable on-disk segments, index block, CRC footer, atomic creation via temp+rename |
| Bloom filter | `internal/bloom` — FNV-1a double hashing, configurable FPR, binary serialization with CRC |
| Single-node LSM-tree Engine | `internal/engine` — WAL + MemTable + SSTables + Bloom, `MANIFEST.json`, WAL replay on restart |
| Manual full compaction | `internal/engine.Compact()` — merges all SSTables, drops tombstones, atomic manifest swap |
| Exact vector search (cosine / L2 / dot) | `internal/vector` — brute-force k-NN, engine-backed persistence, namespace isolation |
| Local consistent-hash sharding (simulation) | `internal/shard` — FNV-1a ring over multiple in-process Engine instances; no cross-process networking |
| Local leader/follower replication (simulation) | `internal/replica` — binary op-log, leader-commit semantics, follower pause/lag/catch-up; no networking |
| Local HTTP observability dashboard (simulation) | `internal/dashboard` — HTML + JSON, three deterministic chaos scenarios; no real distributed node discovery |
| Six-workload benchmark CLI | `internal/bench` + `cmd/shardforge-bench` — P50/P95/P99, Markdown report, reproducible |
| Real networked HTTP node runtime | `internal/node` + `cmd/shardforge-node` — independent HTTP/JSON processes, full CRUD API |
| Client-side consistent-hash routing | `internal/gateway` + `cmd/shardforge-gateway` — FNV-1a ring, virtual nodes, weight support |
| Stateless HTTP routing proxy | `internal/proxy` + `cmd/shardforge-proxy` — 10 endpoints, no failover, explicit scope flags |
| Static cluster metadata | `internal/cluster` + `cmd/shardforge-cluster` — typed JSON config, validate/print/example |
| Explicit pull-based read-replica sync with durable state | `internal/replnet` — durable binary journal (`DurableLog`; per-Append fsync, rollback on write/sync failure, `ErrPoisonedLog` when rollback fails, replay boundary checks: seq 0 rejected, first seq ≠ 1 rejected, gaps/duplicates/regressions rejected, MaxUint64 rejected to prevent nextSeq wrap); identity-bound durable follower cursor (`ReplicationStateStore`; versioned JSON with `version`, `follower_node_id`, `primary_url`, `last_applied_seq`, `updated_at`, `checksum`; CRC32 over all fields; directory fsync best-effort after rename; `ErrInvalidSeqRegression` on backward moves; `ErrFollowerIdentityMismatch`/`ErrPrimaryIdentityMismatch` on mismatch — never silently reset to zero); gap detection (HTTP 409); follower pulls on demand; 403 for follower writes; `ErrSyncInProgress` guard against concurrent syncs. **Crash window caveat:** a mutation applied to the engine but lost before `journal.Append` (process crash between the two) is visible in the engine but absent from the replication log — follower cannot replicate it without a reseed. |
| Health check visibility | `internal/ops.CheckClusterHealth` — HTTP /healthz polling, latency, sorted results |
| Failure simulation (no live calls) | `internal/ops.SimulateFailure` — routing impact on sample keys, pure ring computation |
| Manual rebalance planning (no data movement) | `internal/ops.PlanManualRebalance` — key movement plan, operator steps, pure computation |
| Runtime operation trace mode (single-node) | `internal/engine.ExplainGet/Put/Delete/Scan`, `internal/vector.ExplainUpsert/Search/Delete` — real execution-path traces, JSON output, `shardforge explain` CLI; no fabricated steps |
| Networked single-node trace API (HTTP) | `internal/node` — `POST /explain/put`, `GET /explain/get`, `DELETE /explain/delete`, `GET /explain/scan` call real `engine.Explain*` paths; `node.Client.ExplainGet/Put/Delete/Scan`; `shardforge explain-node` CLI; single-node HTTP only |
| 1264 race-safe tests across 23 packages | `go test -race -count=1 ./...` — 1264 passing tests, 23 packages with test files, 4 packages with no test files (`cmd/shardforge-bench`, `cmd/shardforge-dashboard`, `cmd/shardforge-node`, `internal/storage`) |
| Reproducible benchmarks | `make bench-*` targets, results in `docs/BENCHMARKS.md` |
| Reproducible local three-node HTTP cluster demo with static routing | `scripts/demo_cluster_{up,smoke,down}.sh` — 3 independent HTTP nodes + stateless proxy, FNV-1a routing, separate data dirs, data isolation proven; `configs/cluster/demo-3node.json`; `make cluster-demo-{up,smoke,down}`; see `docs/DEMO.md` |
| Networked explicit pull-based replication demo between HTTP nodes | `scripts/repl_demo_{up,smoke,down}.sh` — leader (primary) + follower (replica) as real HTTP processes; explicit pull via `POST /replication/sync`; PUT+DELETE replication proven; idempotent pull proven; follower write-rejection proven; `configs/replication/demo-leader-follower.json`; `make repl-demo-{up,smoke,down}` |
| Durable replication restart recovery demo | `scripts/repl_restart_demo_{up,smoke,down}.sh` — 18 checks: primary journal survives restart, follower cursor restored after restart, replication resumes from correct position, idempotent pull; `make repl-restart-demo-{up,smoke,down}` |
| Automatic background pull replication with lag tracking | `internal/node/background_sync.go` — configurable background goroutine (interval, backoff, jitter), polls primary every 500ms, exponential backoff on failure, `ErrSyncInProgress`→skip, `*ReplicationGapError`→terminal blocked state, `lag_entries`/`lag_known` always set after any successful sync; `--bg-sync` CLI flags; `GET /replication/status` includes `background_sync` block; `scripts/repl_auto_demo_{up,smoke,down}.sh` — 24 checks; `make repl-auto-demo-{up,smoke,down}` |
| Operator-controlled manual promotion (planned failover, no automatic failover) | `POST /replication/quiesce` on primary: write-fence via RWMutex write gate, persist quiesce record (NewQuiesceID crypto/rand, CRC32 checksum, quiesceMu-serialized). `POST /replication/promote` on follower: 16 precondition checks, promotionBarrier+double-check drain, CreateJournalBaseline (idempotent), SavePromotionRecord (commit point), DurableLog starts at N+1. Cross-validated startup: baseline.base_seq == promotion_record.inherited_last_seq. Stable error codes (writeJSONError, code field). Typed ReplicationStatusResponse Phase 28 fields. 147 Phase 28 tests (26 hardening + prior Phase 28 tests). See `docs/MANUAL_PROMOTION_DESIGN.md`. NOT automatic failover; requires operator to stop old primary first. |
| 1264 race-safe tests across 23 packages | `go test -race -count=1 ./...` — 1264 passing tests |

---

## B. Unsafe Claims

The following must **never** appear in documentation, README, demo scripts, or recruiter materials. None of these are implemented.

| Forbidden claim | Why it is wrong |
|---|---|
| "Real distributed database" | Nodes are independent HTTP processes; there is no coordination, no consensus, no quorum |
| "Real networked cluster" | Three independent HTTP nodes behind a proxy is not a coordinated cluster |
| "Distributed sharding" | Sharding is local in-process (`internal/shard`); the gateway is client-side routing only |
| "RPC between database nodes" | Nodes communicate only via HTTP/JSON; there is no binary RPC protocol |
| "Raft" or "Paxos" | No consensus protocol of any kind exists in the codebase |
| "Consensus" | No quorum voting, no leader election, no replicated log with commit index |
| "Automatic leader election" | Leaders are statically configured; there is no election |
| "Quorum replication" | Replication is explicit pull-based, not quorum-acknowledged |
| "Fault-tolerant cluster" | The proxy returns 502 on node failure; there is no automatic recovery |
| "Automatic failover" | All failover is manual operator action |
| "Shard migration" | No data movement between nodes exists |
| "Distributed vector search" | Vector search is local and exact; no distributed execution |
| "ANN" / "HNSW" / "IVF" | Vector search is exact brute-force O(n·d); no approximation |
| "Background compaction" | Compaction is manual only (`Compact()` must be called explicitly) |
| "Levelled compaction" | Only full manual compaction is implemented; no levelled or tiered compaction |
| "Distributed transactions" | No cross-node transactions of any kind |
| "Production monitoring" | The dashboard is local-only; no distributed node discovery |
| "Self-healing cluster" | No automatic recovery from node failure |
| "Strong consistency" | Follower reads may lag behind primary by an arbitrary number of ops |
| "Automatic failover" | Follower can replicate automatically but is NOT promoted when primary fails |
| "Write forwarding" | Follower rejects writes even with background sync enabled |
| "Service discovery" / "gossip" | Cluster membership is static JSON loaded at startup |
| "Dynamic membership" | No join/leave protocol exists |
| "Distributed operation traces" | Traces cover single-node engine only; no cross-node trace propagation |
| "Networked traces" / "distributed tracing" | Traces do not propagate over HTTP or any network protocol |

---

## C. Future Claims

The following may become safe only after the specific future phases listed. None are implemented today.

| Future claim | Required before claiming | Phase |
|---|---|---|
| "Real distributed sharding" | Key ranges actually stored on separate node processes via networking | Phase 18 |
| "RPC-based node communication" | Binary RPC protocol between nodes replaces HTTP | Phase 16+ |
| "Networked multi-node database" | Nodes coordinate via a real distributed protocol (not just shared config) | Phase 18 |
| "Networked replication" | Replication is triggered automatically, not only on explicit pull | Phase 20 |
| "Majority / quorum writes" | Write acknowledged only after N/2+1 nodes confirm | Phase 22 |
| "Automatic failover" | Follower is automatically promoted when primary fails | Phase 23 |
| "Raft" | Full leader election, term handling, replicated log, commit index, voting, tests | Phase 23 |
| "Real cluster dashboard" | Dashboard polls live distributed nodes | Phase 26 |
| "Distributed vector search" | Query is routed and executed across multiple nodes | Phase 25 |
| "ANN vector search" | Approximate nearest-neighbour index (HNSW, IVF, or equivalent) | Phase 25 |
| "Background compaction" | Automatic threshold-triggered compaction without explicit call | Phase 24 |
| "Block cache" | In-memory LRU cache for hot SSTable blocks | Phase 24 |
| "Operation traces" | Real execution paths produce per-operation trace steps | Phase 15 |

---

## Scope flags enforced in code

Every ops result returns a `scope` object in JSON with all flags `true`:
- `manual_only`
- `simulation_only`
- `no_automatic_failover`
- `no_automatic_rebalancing`
- `no_shard_migration`
- `no_data_movement`
- `no_consensus`
- `no_raft`

Every cluster config includes a `scope` object with all limitation flags documented as `true`.

---

## Correct wording examples

### Resume / LinkedIn (safe)

```
Built an explainable Go database engine with WAL-backed LSM-tree storage, exact vector search,
real networked HTTP node runtime, client-side consistent-hash routing, stateless proxy,
explicit pull-based read replicas with durable journal and cursor, automatic background pull
replication with lag tracking, operator-controlled planned failover (quiesce + promote), and
ops simulation tools — 1264 race-safe tests across 28 phases, reproducible benchmarks.
```

### What to say if asked "is it distributed?"

```
ShardForgeDB has real independent HTTP node processes and a client-side routing gateway, but the
nodes do not coordinate — there is no consensus, no quorum, no automatic failover. The roadmap
includes real distributed sharding and Raft-based consensus as future phases.
```

### What to say if asked "does it use HNSW or ANN?"

```
No. Vector search is exact brute-force k-NN (cosine, L2, or dot product). HNSW and approximate
nearest-neighbour search are planned for a future phase but are not implemented.
```
