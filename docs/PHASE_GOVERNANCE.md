# ShardForgeDB — Phase Governance

**Version 1.0 — Established Phase 29 (2026-06-22)**

This document defines the non-negotiable rules for all phases in the ShardForgeDB distributed v1.0 program (Phases 29–46). Every phase must comply with these rules without exception.

---

## 1. Non-Negotiable Rules

### 1.1 No Implementation Without Specification

No production Go code implementing a distributed capability may be written until:
- The architecture specification for that capability exists in an approved document
- The consistency contract for that capability is defined in `docs/CONSISTENCY_MODEL.md`
- The failure modes for that capability are listed in `docs/DISTRIBUTED_FAILURE_MODEL.md`
- The test strategy for that capability is defined in `docs/DISTRIBUTED_TEST_STRATEGY.md`
- The claim-unlock condition is listed in `docs/DISTRIBUTED_V1_ARCHITECTURE.md`

### 1.2 No Claim Before Implementation is Complete

A capability claim may only be added to `docs/CLAIMS.md` Section A (Safe Claims) after:
- All production Go code for the capability is merged to main
- All required tests for the capability pass (with race detector enabled)
- All required benchmarks for the capability produce documented results
- All required demos for the capability are reproducible
- The capability's phase acceptance criteria are fully met

### 1.3 No Phase May Start Until the Previous Phase is Accepted

A phase is started only after the previous phase's acceptance criteria are fully met and documented. Phases may not overlap in implementation scope.

### 1.4 The Existing Test Suite May Never Be Weakened

- No existing test may be deleted or modified to make it pass under a less strict condition
- No existing test may be marked as skipped or have its assertions removed
- If a new capability changes the behavior of an existing feature, the existing tests must be updated to verify the new correct behavior (not relaxed)
- `go test -race -count=1 ./...` must pass before any phase commit is merged

### 1.5 No Production Distributed Code in Phase 29

Phase 29 is an architecture and specification phase. The following are explicitly forbidden in Phase 29:
- Raft implementation code
- Leader election code
- Automatic failover code
- Quorum write code
- Shard migration code
- Background compaction code
- HNSW implementation code
- Any new HTTP endpoints for distributed capabilities
- Any changes to existing runtime behavior

### 1.6 The v0.5.0-portfolio Tag Is Immutable

The git tag `v0.5.0-portfolio` and the GitHub Release for that tag may not be modified, deleted, or re-tagged for any reason.

### 1.7 All Forbidden Claims Must Remain Forbidden Until the Specified Phase

A claim listed in `docs/CLAIMS.md` Section B (Unsafe Claims) remains forbidden until the exact phase listed in Section C (Future Claims) is accepted. No exception exists for marketing, portfolio, or recruiting purposes.

### 1.8 Formal Specifications Must Precede Implementation

For Raft and any consensus protocol, a TLA+ specification must be reviewed and verified before production code is written. The formal specification serves as the correctness oracle.

---

## 2. Phase States

### ACCEPTED

A phase reaches ACCEPTED state when all of the following are true:
- All required production Go code is merged to main
- `go test -race -count=1 ./...` passes with at least the test count from the previous phase
- All required benchmarks are documented in `docs/BENCHMARKS.md`
- All required demos run reproducibly
- All required documentation is present and accurate
- `docs/CLAIMS.md` has been updated to move the phase's unlocked claim from Section C to Section A
- The phase acceptance criteria in `docs/ROADMAP_DISTRIBUTED.md` are fully met
- The phase author has explicitly signed off that all forbidden claims remain forbidden

### HOLD

A phase is placed on HOLD when any of the following occur:
- A test failure is discovered in any existing test
- A forbidden claim is found in any documentation, demo script, or commit message
- An invariant violation is detected in the deterministic simulator
- A linearizability violation is found by the invariant checker
- The phase's implementation does not match its specification
- An architecture defect is discovered that affects safety or correctness

A phase on HOLD may not be merged until all HOLD conditions are resolved.

---

## 3. Phase Artifact Checklist

Every phase must produce all of the following artifacts before reaching ACCEPTED state:

### 3.1 Code Artifacts
- [ ] All production Go files implementing the phase's scope
- [ ] All test files covering the phase's scope (Layer 1–4 minimum; higher layers as specified)
- [ ] All benchmark files covering the phase's performance claims

### 3.2 Documentation Artifacts
- [ ] `docs/ROADMAP_DISTRIBUTED.md` — phase section updated with completion status
- [ ] `docs/CLAIMS.md` — phase's unlocked claim moved from Section C to Section A
- [ ] `docs/CLAIMS.md` — any newly discovered forbidden claims added to Section B
- [ ] `docs/ARCHITECTURE.md` — phase's component added to the architecture diagram
- [ ] `docs/BENCHMARKS.md` — phase's benchmark results documented

### 3.3 Verification Artifacts
- [ ] `go test -race -count=1 ./...` output showing all tests pass
- [ ] Benchmark output for the phase's performance claims
- [ ] Demo script output for the phase's required demos
- [ ] Deterministic simulator run output for the phase's fault injection scenarios
- [ ] (Phase 46 only) Linearizability checker output for 500 runs

### 3.4 Formal Verification Artifacts
- [ ] (Phase 31–36) TLA+ specification for the relevant Raft sub-protocol
- [ ] (Phase 31–36) TLC model check output showing no invariant violations

---

## 4. Claim Governance

### 4.1 Adding a Safe Claim

A new safe claim requires:
1. Evidence: the specific Go file and test that proves the claim
2. Scope: explicit statement of what the claim covers and what it does NOT cover
3. Negative space: any related capability that is NOT covered must be listed as an unsafe claim if it might be inferred

### 4.2 Safe Claim Wording Standards

- Claims must use specific, verifiable language ("majority-committed writes acknowledged only after N/2+1 nodes confirm", not "distributed writes")
- Claims must not imply capabilities that are not implemented ("automatic failover" implies Raft; do not use it unless Raft is implemented)
- Claims must include scope limitations inline ("per-shard only", "single metadata group", "no cross-shard transactions")

### 4.3 Unsafe Claim Discovery

If a new unsafe claim is discovered (e.g., a phrase in a demo script that implies an unimplemented capability), the discovering author must:
1. Remove the phrase immediately
2. Add the claim to Section B of `docs/CLAIMS.md` with explanation
3. Search all documentation, README, and scripts for similar language

---

## 5. Review Requirements

Every phase PR requires:
- Self-review checklist completed by the author
- All CI checks passing (test, vet, build)
- Documentation review verifying no forbidden claims are introduced
- For Phases 31–36: formal specification review

---

## 6. Escalation

If a phase is found to contain a safety defect (invariant violation, linearizability violation, or committed data loss) after ACCEPTED state, the phase is immediately placed on HOLD and the defect must be resolved before any subsequent phase may proceed. The defect must be documented in `docs/DISTRIBUTED_FAILURE_MODEL.md` along with the fix.
