// Package replnet implements networked replication primitives for ShardForgeDB.
//
// Scope:
//   - Explicit pull-based replication only. No automatic background sync loop.
//   - Primary mutation log is persisted to a binary journal file (Phase 26+).
//   - Follower cursor is persisted to replication_state.json (Phase 26+).
//   - Follower pulls entries from primary via HTTP; applies them locally.
//   - No Raft. No consensus. No quorum. No automatic failover. No leader election.
//   - No strong consistency guarantee. Replication lag is expected under load.
package replnet

import "errors"

// ErrInvalidRole is returned when an unrecognised replication role is used.
var ErrInvalidRole = errors.New("replnet: invalid role")

// ErrInvalidEntry is returned when an entry is malformed or out-of-order.
var ErrInvalidEntry = errors.New("replnet: invalid entry")

// ErrClosed is returned when an operation is attempted on a closed Log or DurableLog.
var ErrClosed = errors.New("replnet: closed")

// ErrReplicationGap is returned when the follower's cursor is behind the primary's
// earliest available journal entry. The follower cannot catch up without a reseed.
var ErrReplicationGap = errors.New("replnet: replication gap")

// ErrCorruptedJournal is returned when a journal record has a CRC mismatch,
// is structurally invalid, or violates sequence monotonicity.
var ErrCorruptedJournal = errors.New("replnet: corrupted journal record")

// ErrCorruptedState is returned when the replication state file fails its checksum
// verification, JSON decoding, or identity validation.
// The follower must treat its cursor as unknown (reset to 0 or reseed).
var ErrCorruptedState = errors.New("replnet: corrupted replication state")

// ErrInvalidSeqRegression is returned when a sequence number is less than or equal
// to the current tail (sequence must strictly advance), or when StateStore.AdvanceTo
// is called with a seq lower than the current persisted cursor.
var ErrInvalidSeqRegression = errors.New("replnet: invalid sequence regression")

// ErrUnsupportedStateVersion is returned when the replication state file contains
// an unrecognised version field.
var ErrUnsupportedStateVersion = errors.New("replnet: unsupported state version")

// ErrFollowerIdentityMismatch is returned when the follower node ID in the state
// file does not match the node that is trying to load it.
var ErrFollowerIdentityMismatch = errors.New("replnet: follower identity mismatch")

// ErrPrimaryIdentityMismatch is returned when the primary URL in the state file does
// not match the primary this follower is currently configured to use.
var ErrPrimaryIdentityMismatch = errors.New("replnet: primary identity mismatch")
