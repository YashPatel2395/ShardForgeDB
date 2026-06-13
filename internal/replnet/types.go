package replnet

import (
	"fmt"
	"time"
)

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

// LogStats is a point-in-time snapshot of an in-memory Log's state.
type LogStats struct {
	// Count is the total number of entries in the log.
	Count int `json:"count"`

	// LastSeq is the sequence number of the most recent entry (0 if empty).
	LastSeq uint64 `json:"last_seq"`
}

// DurableLogStats is a point-in-time snapshot of a DurableLog's state.
type DurableLogStats struct {
	// Count is the total number of entries in the journal.
	Count int `json:"count"`

	// LastSeq is the sequence number of the most recent entry (0 if empty).
	LastSeq uint64 `json:"last_seq"`

	// FirstAvailableSeq is the lowest sequence number present in the journal.
	// 0 means the journal is empty. Used for gap detection.
	FirstAvailableSeq uint64 `json:"first_available_seq"`

	// JournalBytes is the current byte size of the journal file.
	JournalBytes int64 `json:"journal_bytes"`

	// Durable is always true for DurableLog; distinguishes it from the in-memory Log.
	Durable bool `json:"durable"`
}

// ReplicationGapError is returned (HTTP 409) when the follower's cursor is behind
// the primary's earliest retained journal entry. The follower cannot resume replication
// without a manual reseed or snapshot restore.
type ReplicationGapError struct {
	// RequestedAfter is the follower's last applied seq (the "after" parameter).
	RequestedAfter uint64 `json:"requested_after"`
	// FirstAvailableSeq is the lowest seq the primary can serve.
	FirstAvailableSeq uint64 `json:"first_available_seq"`
	// LatestSeq is the primary's most recent seq.
	LatestSeq uint64 `json:"latest_seq"`
}

func (e *ReplicationGapError) Error() string {
	return fmt.Sprintf("replication gap: requested after %d but first available is %d (latest=%d)",
		e.RequestedAfter, e.FirstAvailableSeq, e.LatestSeq)
}

// Unwrap allows errors.Is(err, ErrReplicationGap) to work.
func (e *ReplicationGapError) Unwrap() error { return ErrReplicationGap }

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

	// Durable is true when the replication state is persisted to disk (Phase 26+).
	// For primary: means the mutation log is written to a binary journal file.
	// For follower: means the replication cursor is saved to replication_state.json.
	Durable bool `json:"durable,omitempty"`

	// StatePersistent is true for followers whose cursor survives restarts (Phase 26+).
	StatePersistent bool `json:"state_persistent,omitempty"`
}
