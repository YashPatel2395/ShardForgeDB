# ShardForgeDB — Distributed Test Strategy

**Phase 29 — Specification Only. No implementation in this phase.**

This document defines the 15 test layers for the ShardForgeDB v1.0-distributed system, the deterministic simulator interfaces, and the minimum acceptance gates for each phase.

---

## Deterministic Simulator Interfaces

The deterministic simulator (implemented in Phase 30) is the foundation for all distributed testing. It provides complete control over time, network, and disk behavior via the following interfaces. These are interface designs only — no implementation in Phase 29.

### Virtual Clock Interface

```go
// VirtualClock provides deterministic time for all timeouts and timers.
// Goroutines call clock.Now() instead of time.Now().
// Goroutines call clock.After(d) instead of time.After(d).
// The test driver calls clock.Advance(d) to move time forward.
type VirtualClock interface {
    Now() time.Time
    After(d time.Duration) <-chan time.Time
    NewTimer(d time.Duration) VirtualTimer
    Advance(d time.Duration)
    // AdvanceTo advances to the given absolute time.
    AdvanceTo(t time.Time)
}

type VirtualTimer interface {
    C() <-chan time.Time
    Reset(d time.Duration) bool
    Stop() bool
}
```

### Node Scheduler Interface

```go
// NodeScheduler controls when goroutines on a simulated node are allowed to run.
// This enables deterministic interleaving of concurrent operations.
type NodeScheduler interface {
    // Pause suspends all goroutines on the named node.
    Pause(nodeID string)
    // Resume allows goroutines on the named node to run.
    Resume(nodeID string)
    // Step advances one goroutine on the named node by one scheduling quantum.
    Step(nodeID string)
    // RunUntilIdle runs all goroutines until no runnable goroutines remain.
    RunUntilIdle()
}
```

### Network Transport Interface

```go
// NetworkTransport provides a simulated network for inter-node RPCs.
// Messages are enqueued and delivered based on the fault injector's policy.
type NetworkTransport interface {
    // Send enqueues a message from src to dst.
    Send(src, dst string, msg Message) error
    // Receive blocks until a message arrives for the given node.
    Receive(nodeID string) (Message, error)
    // Partition blocks all messages between the given node sets.
    Partition(group1, group2 []string)
    // Heal removes all partitions.
    Heal()
    // SetDelay configures a fixed or random delay for messages between src and dst.
    SetDelay(src, dst string, delay time.Duration)
    // SetDropRate configures a probability of dropping messages between src and dst.
    SetDropRate(src, dst string, rate float64)
    // SetDuplicateRate configures a probability of duplicating messages.
    SetDuplicateRate(src, dst string, rate float64)
    // SetReorderBuffer configures a reorder buffer of size n for messages.
    SetReorderBuffer(src, dst string, n int)
}
```

### Disk Abstraction Interface

```go
// DiskAbstraction provides a simulated disk for durability testing.
// It can inject faults at any point in the write/fsync/read path.
type DiskAbstraction interface {
    // Write writes data to the given path.
    Write(path string, data []byte) error
    // Read reads data from the given path.
    Read(path string) ([]byte, error)
    // Fsync durably commits all pending writes for the given path.
    Fsync(path string) error
    // InjectWriteError causes the next Write to the given path to fail.
    InjectWriteError(path string, err error)
    // InjectFsyncError causes the next Fsync to the given path to fail.
    InjectFsyncError(path string, err error)
    // InjectCorruption flips bits in the given byte range of the given path.
    InjectCorruption(path string, offset, length int)
    // Snapshot captures the current disk state for later restore.
    Snapshot() DiskSnapshot
    // Restore restores the disk to a previously captured state.
    Restore(s DiskSnapshot)
}
```

### Fault Injector Interface

```go
// FaultInjector provides a high-level API for orchestrating fault scenarios.
// It coordinates the VirtualClock, NodeScheduler, NetworkTransport, and DiskAbstraction.
type FaultInjector interface {
    // CrashNode immediately stops all goroutines on the given node and discards
    // any unflushed writes (simulating a power-loss crash).
    CrashNode(nodeID string)
    // RestartNode starts a fresh instance of the given node.
    RestartNode(nodeID string)
    // PauseNode pauses all goroutines on the given node (simulating a GC pause).
    PauseNode(nodeID string)
    // ResumeNode resumes a paused node.
    ResumeNode(nodeID string)
    // Partition partitions the network between the given node groups.
    Partition(group1, group2 []string)
    // HealPartition heals all network partitions.
    HealPartition()
    // SlowDisk makes all disk operations on the given node take the given duration.
    SlowDisk(nodeID string, latency time.Duration)
    // FullDisk makes all disk write operations on the given node fail with ErrDiskFull.
    FullDisk(nodeID string)
    // RestoreDisk removes all disk fault injections on the given node.
    RestoreDisk(nodeID string)
}
```

### Event Recorder Interface

```go
// EventRecorder records all significant events in the simulation for later inspection.
type EventRecorder interface {
    // Record records an event.
    Record(event Event)
    // Events returns all recorded events.
    Events() []Event
    // Filter returns events matching the given predicate.
    Filter(pred func(Event) bool) []Event
}

type Event struct {
    Time   time.Time
    NodeID string
    Type   EventType
    Detail string
}
```

### Invariant Checker Interface

```go
// InvariantChecker verifies global invariants after every event in the simulation.
type InvariantChecker interface {
    // Check verifies all registered invariants against the current cluster state.
    // Returns a list of violated invariants (empty if all hold).
    Check(state ClusterState) []InvariantViolation
    // Register adds an invariant to the checker.
    Register(name string, check func(ClusterState) error)
}
```

### Replay by Seed Interface

```go
// Simulator is the top-level deterministic simulator. Given the same seed,
// it produces the same sequence of events, faults, and outcomes.
type Simulator interface {
    // SetSeed sets the random seed for all non-deterministic choices.
    SetSeed(seed int64)
    // Run runs the simulation until the given stop condition or step limit is reached.
    Run(scenario Scenario, stopCond func(ClusterState) bool, maxSteps int) SimResult
    // Replay replays the simulation with the given seed and records the event log.
    Replay(seed int64) SimResult
}
```

---

## 15 Test Layers

### Layer 1: Unit Tests — Raft State Machine

**Scope:** Individual Raft actions (RequestVote, AppendEntries, InstallSnapshot) tested in isolation. No networking, no disk, no goroutines.

**What is tested:**
- Term advancement logic
- Vote-granting rules (log up-to-date check, one vote per term)
- AppendEntries validation (prevLogIndex, prevLogTerm, conflict detection)
- CommitIndex advancement (majority acknowledgment)
- Leader demotion on receiving higher term

**Minimum gate:** 100% of Raft actions have dedicated unit tests; all invariants hold on every test case.

**Phase:** 31–33

---

### Layer 2: Unit Tests — Raft Persistent State

**Scope:** WAL-backed term, votedFor, and log entry durability tested without networking.

**What is tested:**
- Write term → crash → read term (must match)
- Write votedFor → crash → read votedFor (must match)
- Write log entries → crash → replay log (must produce identical entries)
- Partial tail truncation on crash recovery

**Minimum gate:** WAL format tests pass on all crash scenarios; no data loss on replay.

**Phase:** 31

---

### Layer 3: Unit Tests — State Machine

**Scope:** The shard state machine (engine-backed) tested in isolation.

**What is tested:**
- Apply Put → Get returns value
- Apply Delete → Get returns not-found
- Apply duplicate request (same idempotency key) → same result returned without re-applying
- Snapshot → Restore → Apply subsequent entries (state is identical to non-snapshot path)

**Minimum gate:** 100% of state machine operations have tests; idempotency is verified.

**Phase:** 34, 35, 37

---

### Layer 4: Integration Tests — Single Raft Group (In-Process)

**Scope:** A complete Raft group (3 or 5 nodes) running in-process with real goroutines and the deterministic simulator's virtual clock and network transport.

**What is tested:**
- Leader election from cold start
- Write via leader → committed and applied on all replicas
- Kill leader → new election → writes continue
- Kill minority → writes continue
- Kill majority → writes blocked → restore majority → writes resume
- Log replication consistency (all replicas have identical applied state)

**Minimum gate:** 500 randomly seeded simulation runs pass all invariant checks with no violations.

**Phase:** 30–33

---

### Layer 5: Integration Tests — Snapshots and Log Compaction

**Scope:** Snapshot creation, InstallSnapshot, and log truncation tested end-to-end.

**What is tested:**
- Write N entries → snapshot created → log truncated → node restarts → state is correct
- Slow follower falls behind by > snapshot threshold → InstallSnapshot sent → follower state matches leader
- Snapshot corruption detected → install fails → node reports error

**Minimum gate:** 100 randomly seeded snapshot + restart scenarios pass.

**Phase:** 35

---

### Layer 6: Integration Tests — Membership Changes

**Scope:** Joint consensus membership changes tested in-process.

**What is tested:**
- Add a node to a 3-node group → writes commit during transition → final group is 4 nodes
- Remove a node from a 3-node group → writes commit during transition → final group is 2 nodes
- Concurrent membership change rejected
- Membership change with partitioned node

**Minimum gate:** 100 randomly seeded membership change scenarios pass; no split-brain observed.

**Phase:** 36

---

### Layer 7: Integration Tests — Metadata Group

**Scope:** The metadata Raft group tested with shard assignment and epoch operations.

**What is tested:**
- Shard assignment stored and retrieved
- Epoch incremented on assignment change
- Stale epoch write rejected
- Metadata group leader election and failover

**Minimum gate:** 50 randomly seeded metadata group scenarios pass.

**Phase:** 38

---

### Layer 8: Integration Tests — Router and Automatic Failover

**Scope:** The router's leader-aware routing and automatic failover tested end-to-end.

**What is tested:**
- Write → routed to shard leader → committed
- Kill leader → router detects NOT_LEADER → new election → router retries → committed
- Router cache stale → router receives redirect → refreshes cache → commits
- Idempotent retry on leader failover (same result, not double-apply)

**Minimum gate:** 200 randomly seeded router + failover scenarios pass.

**Phase:** 39

---

### Layer 9: Integration Tests — Shard Migration

**Scope:** Live shard migration tested end-to-end with fault injection at every stage.

**What is tested:**
- Migration completes without fault
- Migration interrupted at each stage → rollback → retry → completes
- Write during migration → committed on correct replica set
- Router follows epoch correctly during and after migration
- Vector search during migration returns correct results

**Minimum gate:** 100 randomly seeded migration scenarios with fault injection pass.

**Phase:** 40

---

### Layer 10: Integration Tests — Compaction and Cache

**Scope:** Background compaction and block cache tested with realistic workloads.

**What is tested:**
- Write 10,000 keys → SSTable count stays bounded (compaction fires)
- Cache hit rate on repeated reads > 80% in benchmark
- Compaction does not corrupt state (reads after compaction return same values)
- Cache eviction policy is LRU (verified by access pattern)

**Minimum gate:** 10 compaction stress tests pass; cache hit rate benchmark passes.

**Phase:** 41

---

### Layer 11: Integration Tests — Vector Search

**Scope:** Distributed HNSW vector search tested against exact brute-force results.

**What is tested:**
- Insert 1,000 vectors across 3 shards → distributed top-10 search matches exact brute-force top-10
- HNSW persists across restarts (checkpoint + log replay)
- Delete vector → vector does not appear in future search results
- Partial shard failure → partial result returned with indication

**Minimum gate:** HNSW recall@10 >= 95% on standard benchmark datasets; partial failure indication verified.

**Phase:** 42, 43

---

### Layer 12: Deterministic Simulator — Fault Injection

**Scope:** Full-cluster fault injection scenarios using the deterministic simulator.

**What is tested:**
- Network partition scenarios (minority, majority, asymmetric)
- Disk fault injection (full disk, fsync failure, corruption)
- Process crash + restart at every point in the write path
- Clock skew injection
- Message drop, duplication, and reordering
- All combinations of failures with concurrent writes

**Minimum gate:** 1,000 randomly seeded fault injection scenarios pass all invariant checks.

**Phase:** 30 (infrastructure); used in all subsequent phases

---

### Layer 13: Linearizability Checking

**Scope:** Linearizability of the complete operation history is verified using a model checker (Porcupine or equivalent).

**What is tested:**
- Record all client operations (invocation + response) during a fault injection run
- Feed the history to the linearizability checker
- The checker verifies that the history is equivalent to a valid sequential history

**Minimum gate:** 500 fault injection runs pass the linearizability checker with no violations.

**Phase:** 46

---

### Layer 14: Chaos Tests — Real Process + Network

**Scope:** Chaos tests against real OS processes (not simulated) to verify that the simulator's conclusions hold in production conditions.

**What is tested:**
- Kill processes via SIGKILL at random times during concurrent writes
- Inject network delays using tc/netem
- Run workloads for 10 minutes with random failures every 30 seconds
- Verify no linearizability violations after recovery

**Minimum gate:** 10 chaos test runs of 10 minutes each, no linearizability violations.

**Phase:** 46

---

### Layer 15: Performance Benchmarks

**Scope:** Throughput and latency benchmarks for the distributed engine.

**What is benchmarked:**
- Write throughput (ops/sec) for 3-node, 5-node clusters
- Read throughput (ops/sec) for linearizable and follower reads
- P50/P95/P99 write latency
- P50/P95/P99 linearizable read latency
- Vector search latency (distributed, 3 shards, top-10)
- Migration throughput (GB/sec during snapshot transfer)

**Minimum gate:** Benchmarks run without errors; results documented in `docs/BENCHMARKS.md`.

**Phase:** 46

---

## Minimum Phase Gates

Each phase must pass its required test layers before its claims are unlocked:

| Phase | Required Test Layers Before Unlock |
|---|---|
| 30 | Layer 12 infrastructure operational; seed-based replay verified |
| 31 | Layer 2 (persistent state); Layer 1 (WAL unit tests) |
| 32 | Layer 1 (election actions); Layer 4 (election integration) |
| 33 | Layer 1 (AppendEntries); Layer 4 (write + commit integration) |
| 34 | Layer 3 (idempotency); Layer 4 (linearizable reads); Layer 7 (read-your-writes) |
| 35 | Layer 5 (snapshots) |
| 36 | Layer 6 (membership changes) |
| 37 | Layer 3 (state machine) |
| 38 | Layer 7 (metadata group) |
| 39 | Layer 8 (router + failover) |
| 40 | Layer 9 (migration) |
| 41 | Layer 10 (compaction + cache) |
| 42 | Layer 11 (HNSW) |
| 43 | Layer 11 (distributed vector search) |
| 44 | Observability tests (metrics endpoint, trace sampling) |
| 45 | Security tests (mTLS handshake, auth rejection) |
| 46 | Layer 13 (linearizability); Layer 14 (chaos); Layer 15 (benchmarks) |
