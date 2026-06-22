---- MODULE ShardForgeRaft ----
\* ShardForgeDB — Raft Consensus Skeleton
\* Phase 29: Architecture and Specification Only.
\* This is a syntactically valid TLA+ skeleton.
\* Full action specifications are added in Phases 31-33.

EXTENDS Naturals, Sequences, FiniteSets

\* ---------------------------------------------------------------------------
\* CONSTANTS
\* ---------------------------------------------------------------------------

CONSTANTS
    Nodes,      \* The set of all nodes in the cluster (e.g., {n1, n2, n3})
    Values      \* The set of values that can be written (e.g., {v1, v2})

\* Derived constants
Quorum == {Q \in SUBSET Nodes : Cardinality(Q) * 2 > Cardinality(Nodes)}

\* Node states
Follower  == "Follower"
Candidate == "Candidate"
Leader    == "Leader"

\* Sentinel: no vote cast
None == "none"

\* ---------------------------------------------------------------------------
\* VARIABLES
\* ---------------------------------------------------------------------------

VARIABLES
    currentTerm,   \* currentTerm[n] : the current Raft term for node n
    votedFor,      \* votedFor[n]    : the node n voted for in its current term (or None)
    state,         \* state[n]       : the role of node n (Follower/Candidate/Leader)
    log,           \* log[n]         : the log of node n; a sequence of [term |-> t, value |-> v]
    commitIndex    \* commitIndex[n] : the highest log index known to be committed on node n

vars == <<currentTerm, votedFor, state, log, commitIndex>>

\* ---------------------------------------------------------------------------
\* TYPE INVARIANT
\* ---------------------------------------------------------------------------

TypeInvariant ==
    /\ currentTerm \in [Nodes -> Nat]
    /\ votedFor    \in [Nodes -> Nodes \union {None}]
    /\ state       \in [Nodes -> {Follower, Candidate, Leader}]
    /\ log         \in [Nodes -> Seq([term : Nat, value : Values])]
    /\ commitIndex \in [Nodes -> Nat]

\* ---------------------------------------------------------------------------
\* HELPER FUNCTIONS
\* ---------------------------------------------------------------------------

\* LogTerm: the term of the last entry in node n's log (0 if log is empty)
LogTerm(n) ==
    IF Len(log[n]) = 0 THEN 0 ELSE log[n][Len(log[n])].term

\* LogLen: length of node n's log
LogLen(n) == Len(log[n])

\* IsUpToDate: node n's log is at least as up-to-date as node m's log
\* (per Raft §5.4.1: compare last entry term, then length)
IsUpToDate(n, m) ==
    \/ LogTerm(n) > LogTerm(m)
    \/ /\ LogTerm(n) = LogTerm(m)
       /\ LogLen(n) >= LogLen(m)

\* Leader in a given term (at most one per term by ElectionSafety)
LeadersInTerm(t) ==
    {n \in Nodes : state[n] = Leader /\ currentTerm[n] = t}

\* ---------------------------------------------------------------------------
\* ELECTION SAFETY INVARIANT
\* At most one leader per term.
\* ---------------------------------------------------------------------------

ElectionSafety ==
    \A t \in Nat : Cardinality(LeadersInTerm(t)) <= 1

\* ---------------------------------------------------------------------------
\* LOG MATCHING INVARIANT
\* If two nodes have an entry at the same index with the same term,
\* their logs are identical through that index.
\* ---------------------------------------------------------------------------

LogMatchingInvariant ==
    \A n, m \in Nodes :
        \A i \in 1..Min(LogLen(n), LogLen(m)) :
            log[n][i].term = log[m][i].term =>
                SubSeq(log[n], 1, i) = SubSeq(log[m], 1, i)

\* ---------------------------------------------------------------------------
\* LEADER COMPLETENESS INVARIANT
\* If an entry is committed, every future leader's log contains it.
\* (Stated as: a leader's log is at least as long as the committed prefix.)
\* ---------------------------------------------------------------------------

LeaderCompletenessInvariant ==
    \A n \in Nodes :
        state[n] = Leader =>
            LogLen(n) >= commitIndex[n]

\* ---------------------------------------------------------------------------
\* INIT
\* ---------------------------------------------------------------------------

Init ==
    /\ currentTerm = [n \in Nodes |-> 0]
    /\ votedFor    = [n \in Nodes |-> None]
    /\ state       = [n \in Nodes |-> Follower]
    /\ log         = [n \in Nodes |-> <<>>]
    /\ commitIndex = [n \in Nodes |-> 0]

\* ---------------------------------------------------------------------------
\* ACTIONS (skeleton — full specification in Phases 31-33)
\* ---------------------------------------------------------------------------

\* BecomeCandidate: a Follower times out and starts an election
BecomeCandidate(n) ==
    /\ state[n] = Follower
    /\ state'       = [state       EXCEPT ![n] = Candidate]
    /\ currentTerm' = [currentTerm EXCEPT ![n] = currentTerm[n] + 1]
    /\ votedFor'    = [votedFor    EXCEPT ![n] = n]
    /\ UNCHANGED <<log, commitIndex>>

\* BecomeLeader: a Candidate that has a quorum of votes becomes Leader
\* (vote collection is modeled abstractly here; detailed in Phase 32)
BecomeLeader(n, Q) ==
    /\ state[n] = Candidate
    /\ Q \in Quorum
    /\ n \in Q
    /\ \A m \in Q : IsUpToDate(n, m)
    /\ state' = [state EXCEPT ![n] = Leader]
    /\ UNCHANGED <<currentTerm, votedFor, log, commitIndex>>

\* BecomeFollower: any node that observes a higher term steps down
BecomeFollower(n, newTerm) ==
    /\ newTerm > currentTerm[n]
    /\ currentTerm' = [currentTerm EXCEPT ![n] = newTerm]
    /\ votedFor'    = [votedFor    EXCEPT ![n] = None]
    /\ state'       = [state       EXCEPT ![n] = Follower]
    /\ UNCHANGED <<log, commitIndex>>

\* AppendEntry: a Leader appends a new entry (client request)
AppendEntry(n, v) ==
    /\ state[n] = Leader
    /\ log' = [log EXCEPT ![n] = Append(log[n], [term |-> currentTerm[n], value |-> v])]
    /\ UNCHANGED <<currentTerm, votedFor, state, commitIndex>>

\* AdvanceCommitIndex: Leader advances commitIndex when majority has the entry
\* (majority matchIndex check is modeled abstractly; detailed in Phase 33)
AdvanceCommitIndex(n, newCommitIndex) ==
    /\ state[n] = Leader
    /\ newCommitIndex > commitIndex[n]
    /\ newCommitIndex <= LogLen(n)
    /\ \E Q \in Quorum :
           \A m \in Q : LogLen(m) >= newCommitIndex
    /\ commitIndex' = [commitIndex EXCEPT ![n] = newCommitIndex]
    /\ UNCHANGED <<currentTerm, votedFor, state, log>>

\* Restart: a node crashes and loses all volatile state
\* (currentTerm and votedFor persist on disk; state and in-flight messages are lost)
Restart(n) ==
    /\ state'       = [state       EXCEPT ![n] = Follower]
    /\ UNCHANGED <<currentTerm, votedFor, log, commitIndex>>

\* ---------------------------------------------------------------------------
\* NEXT
\* ---------------------------------------------------------------------------

Next ==
    \/ \E n \in Nodes :
           \/ BecomeCandidate(n)
           \/ Restart(n)
           \/ \E v \in Values : AppendEntry(n, v)
           \/ \E i \in Nat : AdvanceCommitIndex(n, i)
    \/ \E n \in Nodes, Q \in SUBSET Nodes : BecomeLeader(n, Q)
    \/ \E n \in Nodes, t \in Nat : BecomeFollower(n, t)

\* ---------------------------------------------------------------------------
\* SPEC
\* ---------------------------------------------------------------------------

Spec == Init /\ [][Next]_vars

\* ---------------------------------------------------------------------------
\* THEOREMS
\* ---------------------------------------------------------------------------

THEOREM TypeInvariant
    \* TypeInvariant is preserved by Init and Next.
    \* Proof: TLC model checking (Phase 32+).

THEOREM ElectionSafety
    \* At most one leader per term.
    \* Proof: TLC model checking (Phase 32+).

====
