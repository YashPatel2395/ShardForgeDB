// Package replica implements a local, in-process leader/follower replication
// simulation for key-value operations.
//
// # What this is
//
// A deterministic, single-process simulation of leader-to-follower operation
// propagation using multiple local Engine instances and an append-only
// replication log. It proves leader/follower write routing, deterministic
// operation ordering, follower catch-up, read consistency choices, commit index
// tracking, and restart/recovery — all without any networking.
//
// # What this is NOT
//
// This is NOT real networking. It is NOT RPC. It is NOT Raft. It is NOT full
// consensus. It is NOT automatic leader election. It is NOT fault-tolerant
// quorum replication. It is NOT distributed. Followers are local Engine
// instances in the same process, not remote nodes.
//
// # Leader-commit semantics
//
// Put and Delete write to the leader Engine immediately. The operation is
// appended to the durable replication log. The commit index advances once the
// leader write succeeds. Followers apply committed operations later by calling
// ReplicateOnce or ReplicateAll. This is NOT quorum replication — a single
// leader write commits without waiting for any follower acknowledgement.
//
// # Directory layout
//
//	<Dir>/
//	  REPLICATION.json          — sharding manifest (written atomically)
//	  replog/
//	    log.dat                 — append-only binary replication log
//	  replicas/
//	    replica-0000/           — engine directory for replica 0 (leader)
//	    replica-0001/           — engine directory for replica 1 (follower)
//	      APPLIED               — last applied log index (text)
//	    replica-0002/
//	      APPLIED
package replica

import "errors"

// ── Sentinel errors ───────────────────────────────────────────────────────────

var (
	// ErrClosed is returned when an operation is attempted on a closed Store.
	ErrClosed = errors.New("replica: closed")

	// ErrInvalidOptions is returned when Open is called with invalid options.
	ErrInvalidOptions = errors.New("replica: invalid options")

	// ErrInvalidKey is returned when an empty key is provided.
	ErrInvalidKey = errors.New("replica: invalid key")

	// ErrInvalidReplica is returned when a replica ID is out of range or
	// otherwise invalid for the requested operation.
	ErrInvalidReplica = errors.New("replica: invalid replica")

	// ErrInvalidReadMode is returned when an unknown ReadMode is passed to
	// Get or Scan.
	ErrInvalidReadMode = errors.New("replica: invalid read mode")

	// ErrNotLeader is returned when a write operation is attempted on a
	// non-leader replica (reserved for future enforcement).
	ErrNotLeader = errors.New("replica: not leader")

	// ErrCorruptManifest is returned when the manifest file is missing,
	// corrupt, or contains invalid data.
	ErrCorruptManifest = errors.New("replica: corrupt manifest")

	// ErrReplicaMismatch is returned on reopen when the caller-supplied
	// ReplicaCount or LeaderID does not match the manifest.
	ErrReplicaMismatch = errors.New("replica: manifest/options mismatch")
)

// ── Role ──────────────────────────────────────────────────────────────────────

// Role is the replication role of a replica.
type Role string

const (
	RoleLeader   Role = "leader"
	RoleFollower Role = "follower"
)

// ── ReadMode ──────────────────────────────────────────────────────────────────

// ReadMode controls which replica handles a Get or Scan call.
//
// Follower reads may be stale: a follower that has not yet applied all
// committed operations will return older values or miss keys that the leader
// has already committed.
type ReadMode string

const (
	// ReadLeader always reads from the leader replica.
	ReadLeader ReadMode = "leader"

	// ReadFollower reads from the replica specified by replicaID. The
	// replicaID must be a follower (not the leader). Returns ErrInvalidReplica
	// if the ID is the leader or out of range.
	ReadFollower ReadMode = "follower"

	// ReadAny reads from the replica specified by replicaID. If replicaID < 0,
	// reads from the leader. If replicaID >= 0, reads from that replica
	// regardless of role.
	ReadAny ReadMode = "any"
)

// ── LogIndex ──────────────────────────────────────────────────────────────────

// LogIndex is a 1-based, monotonically increasing position in the replication
// log. Index 0 means "no operation applied yet".
type LogIndex uint64

// ── OperationType ─────────────────────────────────────────────────────────────

// OperationType identifies the kind of operation stored in the replication log.
type OperationType uint8

const (
	OpPut    OperationType = 1
	OpDelete OperationType = 2
)

// ── Operation ─────────────────────────────────────────────────────────────────

// Operation is one committed entry in the replication log.
type Operation struct {
	Index LogIndex
	Type  OperationType
	Key   []byte
	Value []byte
}

// ── Options ───────────────────────────────────────────────────────────────────

// Options configures a replica Store.
type Options struct {
	// Dir is the root directory. Required.
	Dir string

	// ReplicaCount is the total number of replicas (leader + followers).
	// Required and must be > 0 on first Open.
	// On reopen, 0 means "use manifest value".
	ReplicaCount int

	// LeaderID is the replica ID that acts as leader. Default 0 on first Open.
	// On reopen, if negative, load from manifest.
	// Non-negative values that differ from manifest return ErrReplicaMismatch.
	LeaderID int

	// ReplicaPrefix is the directory name prefix for replica subdirectories.
	// Default: "replica".
	ReplicaPrefix string
}

// ── ReplicaInfo ───────────────────────────────────────────────────────────────

// ReplicaInfo describes one replica.
type ReplicaInfo struct {
	ID   int
	Role Role
	Name string
	Path string
}

// ── Entry ─────────────────────────────────────────────────────────────────────

// Entry is a live key-value pair returned by Scan.
// Tombstones are never returned to callers.
type Entry struct {
	Key   []byte
	Value []byte
	Seq   uint64
}

// ── Stats ─────────────────────────────────────────────────────────────────────

// Stats aggregates store-wide and per-replica statistics.
type Stats struct {
	ReplicaCount int
	LeaderID     int

	// CommitIndex is the log index of the last committed operation.
	CommitIndex LogIndex

	// AppliedIndex is the minimum applied index across all replicas (for
	// convenience). Use ReplicaStats for per-replica values.
	AppliedIndex LogIndex

	Replicas []ReplicaStats
}

// ReplicaStats holds per-replica statistics.
type ReplicaStats struct {
	ID   int
	Role Role
	Name string
	Path string

	AppliedIndex          LogIndex
	EngineSSTableCount    int
	EngineFlushCount      uint64
	EngineCompactionCount uint64
}
