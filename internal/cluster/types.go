// Package cluster implements static cluster metadata for ShardForgeDB.
//
// Scope:
//   - Static, file-based cluster configuration only.
//   - Describes independent shardforge-node processes and routing/proxy settings.
//   - Config is loaded once at startup; there is no dynamic membership update.
//   - No node discovery. No gossip. No Raft. No consensus. No leader election.
//   - No quorum. No replication. No failover. No shard migration.
//   - No resharding. No distributed transactions. No distributed vector search.
//   - Config-driven gateway/proxy startup is the only integration point.
package cluster

// CurrentVersion is the only supported config format version.
const CurrentVersion = "v1"

// RoutingFNV1AConsistentHash is the only supported routing algorithm.
const RoutingFNV1AConsistentHash = "fnv1a-consistent-hash"

// DefaultVirtualNodes is the default number of virtual ring points per node.
const DefaultVirtualNodes = 128

// DefaultProxyAddr is the default proxy listen address.
const DefaultProxyAddr = "127.0.0.1:9200"

// Config is the root cluster configuration type.
// It is serialised as JSON and loaded at startup.
type Config struct {
	Version     string  `json:"version"`
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	Routing     Routing `json:"routing"`
	Proxy       Proxy   `json:"proxy"`
	Nodes       []Node  `json:"nodes"`
	Scope       Scope   `json:"scope"`
}

// Routing describes the key-routing configuration.
type Routing struct {
	// Algorithm must be "fnv1a-consistent-hash".
	Algorithm string `json:"algorithm"`
	// VirtualNodes is the number of ring points per node (before weight scaling).
	// Normalized to DefaultVirtualNodes when <= 0.
	VirtualNodes int `json:"virtual_nodes"`
}

// Proxy describes the optional proxy server settings.
type Proxy struct {
	// Enabled indicates whether a shardforge-proxy should be started with this config.
	Enabled bool `json:"enabled"`
	// Addr is the proxy listen address (e.g. "127.0.0.1:9200").
	// Normalized to DefaultProxyAddr when empty.
	Addr string `json:"addr"`
}

// Replication describes the optional replication role of a node.
// All fields are omitted when replication is not configured.
type Replication struct {
	// Enabled indicates that this node participates in pull-based replication.
	Enabled bool `json:"enabled"`
	// Role is the node's replication role: "primary" or "follower".
	// Empty means standalone (replication disabled).
	Role string `json:"role,omitempty"`
	// Primary is the ID of this node's primary (follower nodes only).
	Primary string `json:"primary,omitempty"`
}

// Node describes one independent shardforge-node process.
type Node struct {
	// ID is a unique human-readable identifier (e.g. "node-1").
	ID string `json:"id"`
	// BaseURL is the HTTP base URL of the node process (e.g. "http://127.0.0.1:9101").
	BaseURL string `json:"base_url"`
	// Addr is the node's listen address; used for documentation / local demo startup.
	Addr string `json:"addr,omitempty"`
	// DataDir is the node's data directory path; used for local demo startup.
	// Must not contain ".." path traversal components.
	DataDir string `json:"data_dir,omitempty"`
	// Weight controls how many virtual ring points this node receives relative to others.
	// Normalized to 1 when <= 0.
	Weight int `json:"weight,omitempty"`
	// Replication describes the optional replication role of this node.
	// Omitted when replication is not configured.
	Replication Replication `json:"replication,omitempty"`
}

// Scope documents what this cluster config does and does not support.
// These fields exist for honest self-documentation and to prevent false capability claims.
//
// For configs without any replication (all nodes standalone):
//   - All fields including NoReplication must be true.
//
// For configs with explicit pull-based read replicas (any node has Replication.Enabled=true):
//   - NoReplication must be omitted or false (replication IS present).
//   - NoQuorumReplication must be true (quorum/automatic replication is not present).
//   - All other fields (NoRaft, NoConsensus, NoFailover, etc.) must still be true.
type Scope struct {
	StaticConfigOnly    bool `json:"static_config_only"`
	NoDynamicMembership bool `json:"no_dynamic_membership"`
	NoDiscovery         bool `json:"no_discovery"`
	NoConsensus         bool `json:"no_consensus"`
	NoRaft              bool `json:"no_raft"`
	// NoReplication must be true for non-replication configs.
	// For read-replica configs (any node has Replication.Enabled=true), this field
	// must be omitted (false) and NoQuorumReplication must be true instead.
	NoReplication bool `json:"no_replication"`
	// NoQuorumReplication must be true for read-replica configs to assert that
	// there is no quorum replication, no automatic failover election, and no Raft log.
	// This field is not required for non-replication configs.
	NoQuorumReplication bool `json:"no_quorum_replication,omitempty"`
	NoFailover          bool `json:"no_failover"`
	NoShardMigration    bool `json:"no_shard_migration"`
	NoDistributedTxns   bool `json:"no_distributed_txns"`
}
