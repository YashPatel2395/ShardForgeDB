# ShardForgeDB — Formal Methods Program

**Phase 29 — Specification Only. No TLC model checking results yet (Phase 31 and later).**

This directory contains formal specifications for the correctness-critical components of ShardForgeDB v1.0-distributed. Formal specifications are written in TLA+ and checked with TLC (the TLA+ model checker).

---

## Purpose

Distributed systems have subtle correctness properties that are difficult to verify by testing alone. Formal specifications serve three purposes:

1. **Correctness oracle.** The TLA+ spec defines the exact invariants and liveness properties the implementation must satisfy. If the spec is checked and the implementation matches the spec, the implementation is correct by construction.
2. **Design documentation.** A TLA+ spec forces precise definition of all state variables, all possible actions, and all invariants. This eliminates ambiguity from prose descriptions.
3. **Regression prevention.** Changes to the implementation that might violate an invariant can be caught by re-checking the spec against the new design.

---

## Required Future Specifications

The following TLA+ modules will be written and checked during the specified phases:

| Module | Phase | What it specifies |
|---|---|---|
| `ShardForgeRaft.tla` | Phase 29 (skeleton), Phase 32 (complete election) | Raft election safety, log matching, leader completeness, state-machine safety |
| `ShardForgeJointConsensus.tla` | Phase 36 | Joint-consensus membership change safety: no split-brain during transition |
| `ShardForgeMigration.tla` | Phase 40 | Shard migration ownership: at no point are there zero live replicas; cutover is atomic |
| `ShardForgeEpochFencing.tla` | Phase 38 | Epoch monotonicity and fencing: stale-epoch writes rejected; no write to wrong replica set |
| `ShardForgeFailover.tla` | Phase 39 | Automatic failover safety: new leader has all committed entries; no split-brain |
| `ShardForgeIdempotency.tla` | Phase 34 | Duplicate-request idempotency: same client ID + request ID always produces same result |

---

## ShardForgeRaft.tla — Module Description

### State Variables

- `currentTerm` — per-node current Raft term (natural number)
- `votedFor` — per-node node ID voted for in current term (or "none")
- `state` — per-node role: Follower, Candidate, or Leader
- `log` — per-node sequence of log entries (each entry has term + value)
- `commitIndex` — per-node highest log index known to be committed

### Actions

- `RequestVote` — a Candidate sends a vote request to another node
- `RequestVoteResponse` — a node grants or denies a vote
- `BecomeLeader` — a Candidate that has received majority votes becomes Leader
- `AppendEntries` — a Leader sends log entries (or heartbeat) to a Follower
- `AppendEntriesResponse` — a Follower acknowledges or rejects AppendEntries
- `AdvanceCommitIndex` — Leader advances commitIndex when majority has matched
- `ClientRequest` — a client proposes a write to the Leader
- `BecomeFollower` — any node that sees a higher term transitions to Follower
- `Restart` — a node loses all volatile state and restarts (term and votedFor persist)

### Invariants

- `TypeInvariant` — all state variables have their correct types
- `ElectionSafety` — at most one leader per term
- `LogMatchingInvariant` — if two logs agree at index i with the same term, they agree at all earlier indices
- `LeaderCompletenessInvariant` — if an entry is committed in term T, every future leader's log contains that entry
- `StateMachineSafetyInvariant` — if two nodes apply the log at index i, they apply the same entry

### Liveness Properties

- `LeaderElected` — eventually, some node becomes Leader
- `CommitProgress` — if a Leader proposes an entry, it is eventually committed

### Model Sizes (for TLC)

- Nodes: {n1, n2, n3} (3 nodes)
- Terms: 1..3
- Log entries: value ∈ {v1, v2}
- Maximum log length: 3

These small model bounds make TLC model checking tractable. They do not limit the correctness of the specification.

### Failure Assumptions

The specification models:
- Process crashes (Restart action: volatile state lost, durable state retained)
- Network message loss (messages may be dropped; modeled by non-delivery)
- Network reordering (messages processed in non-FIFO order within the action set)

The specification does NOT model:
- Byzantine failures (nodes always follow the protocol)
- Disk corruption (durable state is assumed correct after restart)

### CI Execution Strategy (Phase 32+)

TLC model checking will run in CI on every PR that modifies a `.tla` or `.cfg` file:
- Command: `java -jar tla2tools.jar -config formal/ShardForgeRaft.cfg formal/ShardForgeRaft.tla`
- Expected output: no invariant violations, no deadlocks
- Run time budget: < 60 seconds for the small model (3 nodes, terms 1..3)
- Larger models (5 nodes, terms 1..5) run nightly

---

## Module Boundaries

Each TLA+ module is self-contained with its own CONSTANTS, VARIABLES, TypeInvariant, Init, Next, invariants, and liveness properties. Modules do not import each other (to keep model checking tractable). Instead, they share assumptions about external behavior (e.g., the Raft module assumes epoch fencing is correct; the fencing module assumes Raft election safety is correct).

---

## Tools

- TLA+ Toolbox: https://github.com/tlaplus/tlaplus
- tla2tools.jar: https://github.com/tlaplus/tlaplus/releases
- TLC (model checker): included in tla2tools.jar
- TLAPS (proof system): for future liveness proofs (Phase 46 scope)
