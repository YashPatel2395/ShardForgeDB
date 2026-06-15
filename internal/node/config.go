package node

import (
	"errors"
	"fmt"
	"math"
	"os"

	"github.com/YashPatel2395/ShardForgeDB/internal/replnet"
)

// ErrInvalidOptions is returned when Options validation fails.
var ErrInvalidOptions = errors.New("node: invalid options")

// ErrClosed is returned when an operation is attempted on a closed Server.
var ErrClosed = errors.New("node: closed")

// ErrAlreadyStarted is returned by Start/StartBackground when called more than once,
// and by backgroundSyncWorker.start() when the worker goroutine has already been launched.
var ErrAlreadyStarted = errors.New("node: server already started")

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
		// valid; background sync must be disabled for non-follower nodes.
		if o.Replication.BackgroundSync.Enabled {
			return fmt.Errorf("%w: BackgroundSync.Enabled is only valid for follower nodes", ErrInvalidOptions)
		}
	case replnet.RoleFollower:
		if o.Replication.PrimaryBaseURL == "" {
			return fmt.Errorf("%w: PrimaryBaseURL is required when Role is %q", ErrInvalidOptions, replnet.RoleFollower)
		}
		if err := o.Replication.BackgroundSync.validate(); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidOptions, err)
		}
	default:
		return fmt.Errorf("%w: unknown replication role %q (want %q, %q, or empty for standalone)",
			ErrInvalidOptions, role, replnet.RolePrimary, replnet.RoleFollower)
	}
	return nil
}

// validate checks BackgroundSyncConfig fields when background sync is enabled.
func (c BackgroundSyncConfig) validate() error {
	if !c.Enabled {
		return nil // disabled: no other fields matter
	}
	if c.Interval.Duration <= 0 {
		return fmt.Errorf("BackgroundSync.Interval must be positive (got %v)", c.Interval.Duration)
	}
	if c.RequestTimeout.Duration <= 0 {
		return fmt.Errorf("BackgroundSync.RequestTimeout must be positive (got %v)", c.RequestTimeout.Duration)
	}
	if c.InitialBackoff.Duration <= 0 {
		return fmt.Errorf("BackgroundSync.InitialBackoff must be positive (got %v)", c.InitialBackoff.Duration)
	}
	if c.MaxBackoff.Duration < c.InitialBackoff.Duration {
		return fmt.Errorf("BackgroundSync.MaxBackoff (%v) must be >= InitialBackoff (%v)",
			c.MaxBackoff.Duration, c.InitialBackoff.Duration)
	}
	// math.IsNaN and math.IsInf must be checked explicitly: NaN comparisons always
	// return false, so `NaN < 0` and `NaN > 1.0` are both false, meaning NaN would
	// pass the range check without the explicit guard.
	if math.IsNaN(c.JitterFraction) || math.IsInf(c.JitterFraction, 0) || c.JitterFraction < 0 || c.JitterFraction > 1.0 {
		return fmt.Errorf("BackgroundSync.JitterFraction must be in [0.0, 1.0] (got %v)", c.JitterFraction)
	}
	return nil
}
