package node

import (
	"errors"
	"fmt"
	"os"

	"github.com/YashPatel2395/ShardForgeDB/internal/replnet"
)

// ErrInvalidOptions is returned when Options validation fails.
var ErrInvalidOptions = errors.New("node: invalid options")

// ErrClosed is returned when an operation is attempted on a closed Server.
var ErrClosed = errors.New("node: closed")

// ErrSyncInProgress is returned by SyncFromPrimary when a sync is already running.
// Concurrent syncs are rejected to prevent double-application of the same batch.
var ErrSyncInProgress = errors.New("node: sync already in progress")

// validate checks that required Options fields are set and the DataDir can be created.
func (o Options) validate() error {
	if o.NodeID == "" {
		return fmt.Errorf("%w: NodeID is required", ErrInvalidOptions)
	}
	if o.Addr == "" {
		return fmt.Errorf("%w: Addr is required", ErrInvalidOptions)
	}
	if o.DataDir == "" {
		return fmt.Errorf("%w: DataDir is required", ErrInvalidOptions)
	}
	if err := os.MkdirAll(o.DataDir, 0o755); err != nil {
		return fmt.Errorf("%w: cannot create DataDir %q: %v", ErrInvalidOptions, o.DataDir, err)
	}

	// Validate replication role.
	role := o.Replication.Role
	switch role {
	case "", replnet.RolePrimary:
		// valid
	case replnet.RoleFollower:
		if o.Replication.PrimaryBaseURL == "" {
			return fmt.Errorf("%w: PrimaryBaseURL is required when Role is %q", ErrInvalidOptions, replnet.RoleFollower)
		}
	default:
		return fmt.Errorf("%w: unknown replication role %q (want %q, %q, or empty for standalone)",
			ErrInvalidOptions, role, replnet.RolePrimary, replnet.RoleFollower)
	}
	return nil
}
