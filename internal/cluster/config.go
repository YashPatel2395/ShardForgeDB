package cluster

// DefaultScope returns a Scope with all limitation flags set to true.
// These flags are honest self-documentation: every ShardForgeDB cluster config
// must acknowledge what the system does and does not implement.
func DefaultScope() Scope {
	return Scope{
		StaticConfigOnly:    true,
		NoDynamicMembership: true,
		NoDiscovery:         true,
		NoConsensus:         true,
		NoRaft:              true,
		NoReplication:       true,
		NoFailover:          true,
		NoShardMigration:    true,
		NoDistributedTxns:   true,
	}
}

// Normalize fills in default values for fields that are missing or zero.
// It does not validate the config; call Validate after Normalize.
// Normalize returns a new Config; the original is not mutated.
func Normalize(cfg Config) Config {
	if cfg.Routing.VirtualNodes <= 0 {
		cfg.Routing.VirtualNodes = DefaultVirtualNodes
	}
	if cfg.Proxy.Addr == "" {
		cfg.Proxy.Addr = DefaultProxyAddr
	}
	nodes := make([]Node, len(cfg.Nodes))
	copy(nodes, cfg.Nodes)
	for i := range nodes {
		if nodes[i].Weight <= 0 {
			nodes[i].Weight = 1
		}
	}
	cfg.Nodes = nodes

	// If all scope flags are false (zero value), apply the default scope.
	s := cfg.Scope
	allFalse := !s.StaticConfigOnly && !s.NoDynamicMembership && !s.NoDiscovery &&
		!s.NoConsensus && !s.NoRaft && !s.NoReplication && !s.NoFailover &&
		!s.NoShardMigration && !s.NoDistributedTxns
	if allFalse {
		cfg.Scope = DefaultScope()
	}
	return cfg
}

// DefaultConfig constructs a normalised Config with the given name and node list.
// Routing defaults to fnv1a-consistent-hash with DefaultVirtualNodes.
// Scope defaults to DefaultScope.
func DefaultConfig(name string, nodes []Node) Config {
	cfg := Config{
		Version: CurrentVersion,
		Name:    name,
		Routing: Routing{
			Algorithm:    RoutingFNV1AConsistentHash,
			VirtualNodes: DefaultVirtualNodes,
		},
		Proxy: Proxy{
			Addr: DefaultProxyAddr,
		},
		Nodes: nodes,
		Scope: DefaultScope(),
	}
	return Normalize(cfg)
}

// ExampleLocal3Node returns a validated example Config for a 3-node local setup
// with nodes on 127.0.0.1:9101–9103 and data dirs under /tmp.
// Used for documentation, testing, and the `shardforge-cluster example-local-3node` command.
func ExampleLocal3Node() Config {
	return DefaultConfig("local-3node", []Node{
		{
			ID:      "node-1",
			BaseURL: "http://127.0.0.1:9101",
			Addr:    "127.0.0.1:9101",
			DataDir: "/tmp/shardforge-node-1",
		},
		{
			ID:      "node-2",
			BaseURL: "http://127.0.0.1:9102",
			Addr:    "127.0.0.1:9102",
			DataDir: "/tmp/shardforge-node-2",
		},
		{
			ID:      "node-3",
			BaseURL: "http://127.0.0.1:9103",
			Addr:    "127.0.0.1:9103",
			DataDir: "/tmp/shardforge-node-3",
		},
	})
}

// ExampleReadReplica3Node returns a validated example Config for a 1-primary + 2-follower
// read-replica setup with nodes on 127.0.0.1:9111–9113.
// Used for documentation, testing, and the `shardforge-cluster example-read-replica-3node` command.
//
// Replication is explicit pull-based: followers call POST /replication/sync to pull entries
// from the primary. There is no automatic background sync loop, no Raft, no automatic failover.
func ExampleReadReplica3Node() Config {
	cfg := DefaultConfig("local-read-replica-3node", []Node{
		{
			ID:      "node-primary",
			BaseURL: "http://127.0.0.1:9111",
			Addr:    "127.0.0.1:9111",
			DataDir: "/tmp/shardforge-primary",
			Replication: Replication{
				Enabled: true,
				Role:    "primary",
			},
		},
		{
			ID:      "node-replica-1",
			BaseURL: "http://127.0.0.1:9112",
			Addr:    "127.0.0.1:9112",
			DataDir: "/tmp/shardforge-replica-1",
			Replication: Replication{
				Enabled: true,
				Role:    "follower",
				Primary: "node-primary",
			},
		},
		{
			ID:      "node-replica-2",
			BaseURL: "http://127.0.0.1:9113",
			Addr:    "127.0.0.1:9113",
			DataDir: "/tmp/shardforge-replica-2",
			Replication: Replication{
				Enabled: true,
				Role:    "follower",
				Primary: "node-primary",
			},
		},
	})
	cfg.Proxy.Addr = "127.0.0.1:9210"
	return cfg
}
