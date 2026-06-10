package replnet

import "time"

// Role identifies the replication role of a node.
type Role string

const (
	// RolePrimary is the node that accepts writes and maintains the mutation log.
	RolePrimary Role = "primary"

	// RoleFollower is the node that pulls entries from the primary and applies them locally.
	// Followers reject client PUT/DELETE requests.
	RoleFollower Role = "follower"
)

// Operation identifies the type of mutation recorded in an Entry.
type Operation string

const (
	// OpPut represents a key-value insertion or update.
	OpPut Operation = "put"

	// OpDelete represents a key deletion.
	OpDelete Operation = "delete"
)

// Entry is one mutation record in the replication log.
type Entry struct {
	// Seq is a monotonically increasing sequence number assigned by the primary.
	// Seq starts at 1; 0 is the zero value and means "no entries yet".
	Seq uint64 `json:"seq"`

	// Op is the operation type: put or delete.
	Op Operation `json:"op"`

	// Key is the affected key.
	Key string `json:"key"`

	// Value is the written value. Empty for delete operations.
	Value string `json:"value,omitempty"`

	// Timestamp is when the entry was appended on the primary (UTC).
	Timestamp time.Time `json:"timestamp"`
}

// LogStats is a point-in-time snapshot of a Log's state.
type LogStats struct {
	// Count is the total number of entries in the log.
	Count int `json:"count"`

	// LastSeq is the sequence number of the most recent entry (0 if empty).
	LastSeq uint64 `json:"last_seq"`
}

// ReplicaStatus describes the current replication state of a node.
type ReplicaStatus struct {
	// Role is the node's replication role (primary, follower, or empty = standalone).
	Role Role `json:"role,omitempty"`

	// PrimaryBaseURL is the HTTP base URL of the primary node.
	// Non-empty only for follower nodes.
	PrimaryBaseURL string `json:"primary_base_url,omitempty"`

	// LastLocalSeq is the last sequence number in the local log (primary only).
	LastLocalSeq uint64 `json:"last_local_seq,omitempty"`

	// LastAppliedSeq is the last sequence number applied from the primary (follower only).
	LastAppliedSeq uint64 `json:"last_applied_seq,omitempty"`

	// PendingFromPrimary is the number of entries available on the primary
	// that have not yet been applied locally. 0 means up-to-date or unknown.
	PendingFromPrimary int `json:"pending_from_primary,omitempty"`
}
