package cluster

import (
	"fmt"
	"strings"
)

// Validate checks that cfg is a valid, fully-normalised cluster configuration.
// Call Normalize before Validate to fill in defaults.
// Validate does not mutate cfg.
func Validate(cfg Config) error {
	if cfg.Version != CurrentVersion {
		return fmt.Errorf("%w: got %q, want %q", ErrUnsupportedVersion, cfg.Version, CurrentVersion)
	}
	if cfg.Name == "" {
		return fmt.Errorf("%w: name must not be empty", ErrInvalidConfig)
	}
	if cfg.Routing.Algorithm != RoutingFNV1AConsistentHash {
		return fmt.Errorf("%w: unsupported algorithm %q (want %q)", ErrUnsupportedRouting, cfg.Routing.Algorithm, RoutingFNV1AConsistentHash)
	}
	if cfg.Routing.VirtualNodes <= 0 {
		return fmt.Errorf("%w: routing.virtual_nodes must be positive (got %d)", ErrInvalidConfig, cfg.Routing.VirtualNodes)
	}
	if len(cfg.Nodes) == 0 {
		return fmt.Errorf("%w: at least one node is required", ErrInvalidConfig)
	}

	ids := make(map[string]struct{}, len(cfg.Nodes))
	urls := make(map[string]struct{}, len(cfg.Nodes))
	for i, n := range cfg.Nodes {
		if n.ID == "" {
			return fmt.Errorf("%w: node[%d].id must not be empty", ErrInvalidConfig, i)
		}
		if n.BaseURL == "" {
			return fmt.Errorf("%w: node[%d].base_url must not be empty", ErrInvalidConfig, i)
		}
		if !strings.HasPrefix(n.BaseURL, "http://") && !strings.HasPrefix(n.BaseURL, "https://") {
			return fmt.Errorf("%w: node[%d].base_url %q must start with http:// or https://", ErrInvalidConfig, i, n.BaseURL)
		}
		if _, dup := ids[n.ID]; dup {
			return fmt.Errorf("%w: duplicate node id %q", ErrInvalidConfig, n.ID)
		}
		ids[n.ID] = struct{}{}
		if _, dup := urls[n.BaseURL]; dup {
			return fmt.Errorf("%w: duplicate node base_url %q", ErrInvalidConfig, n.BaseURL)
		}
		urls[n.BaseURL] = struct{}{}
		if n.Weight <= 0 {
			return fmt.Errorf("%w: node[%d].weight must be positive (got %d); call Normalize first", ErrInvalidConfig, i, n.Weight)
		}
		if n.DataDir != "" && strings.Contains(n.DataDir, "..") {
			return fmt.Errorf("%w: node[%d].data_dir %q contains path traversal (\"..\")", ErrInvalidConfig, i, n.DataDir)
		}
	}

	// Validate replication config if any node has it enabled.
	if err := validateReplication(cfg.Nodes); err != nil {
		return err
	}

	// Scope flags must all be true to prevent false capability claims.
	s := cfg.Scope
	if !s.StaticConfigOnly {
		return fmt.Errorf("%w: scope.static_config_only must be true", ErrInvalidConfig)
	}
	if !s.NoDynamicMembership {
		return fmt.Errorf("%w: scope.no_dynamic_membership must be true", ErrInvalidConfig)
	}
	if !s.NoDiscovery {
		return fmt.Errorf("%w: scope.no_discovery must be true", ErrInvalidConfig)
	}
	if !s.NoConsensus {
		return fmt.Errorf("%w: scope.no_consensus must be true", ErrInvalidConfig)
	}
	if !s.NoRaft {
		return fmt.Errorf("%w: scope.no_raft must be true", ErrInvalidConfig)
	}
	if !s.NoReplication {
		return fmt.Errorf("%w: scope.no_replication must be true", ErrInvalidConfig)
	}
	if !s.NoFailover {
		return fmt.Errorf("%w: scope.no_failover must be true", ErrInvalidConfig)
	}
	if !s.NoShardMigration {
		return fmt.Errorf("%w: scope.no_shard_migration must be true", ErrInvalidConfig)
	}
	if !s.NoDistributedTxns {
		return fmt.Errorf("%w: scope.no_distributed_txns must be true", ErrInvalidConfig)
	}

	return nil
}

// validateReplication checks per-node replication config consistency.
// It is a no-op when no nodes have Replication.Enabled = true.
func validateReplication(nodes []Node) error {
	var anyEnabled bool
	for _, n := range nodes {
		if n.Replication.Enabled {
			anyEnabled = true
			break
		}
	}
	if !anyEnabled {
		return nil
	}

	// Build an index of node IDs for reference validation.
	nodeIDs := make(map[string]struct{}, len(nodes))
	for _, n := range nodes {
		nodeIDs[n.ID] = struct{}{}
	}

	var primaryCount int
	for i, n := range nodes {
		if !n.Replication.Enabled {
			continue
		}
		switch n.Replication.Role {
		case "primary":
			primaryCount++
		case "follower":
			if n.Replication.Primary == "" {
				return fmt.Errorf("%w: node[%d] (%q) is a follower but replication.primary is empty",
					ErrInvalidConfig, i, n.ID)
			}
			if _, ok := nodeIDs[n.Replication.Primary]; !ok {
				return fmt.Errorf("%w: node[%d] (%q) references unknown primary node %q",
					ErrInvalidConfig, i, n.ID, n.Replication.Primary)
			}
		default:
			return fmt.Errorf("%w: node[%d] (%q) has replication.enabled=true but unsupported role %q (want \"primary\" or \"follower\")",
				ErrInvalidConfig, i, n.ID, n.Replication.Role)
		}
	}

	if primaryCount == 0 {
		return fmt.Errorf("%w: replication is enabled but no node has role \"primary\"", ErrInvalidConfig)
	}
	if primaryCount > 1 {
		return fmt.Errorf("%w: multiple primaries are not supported (found %d)", ErrInvalidConfig, primaryCount)
	}
	return nil
}
