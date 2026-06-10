// Package replnet implements networked replication primitives for ShardForgeDB.
//
// Scope:
//   - Explicit pull-based replication only. No automatic background sync loop.
//   - In-memory mutation log (not persisted; cleared on restart).
//   - Follower pulls entries from primary via HTTP; applies them locally.
//   - No Raft. No consensus. No quorum. No automatic failover. No leader election.
//   - No strong consistency guarantee. Replication lag is expected under load.
package replnet

import "errors"

// ErrInvalidRole is returned when an unrecognised replication role is used.
var ErrInvalidRole = errors.New("replnet: invalid role")

// ErrInvalidEntry is returned when an entry is malformed or out-of-order.
var ErrInvalidEntry = errors.New("replnet: invalid entry")

// ErrClosed is returned when an operation is attempted on a closed Log.
var ErrClosed = errors.New("replnet: closed")
