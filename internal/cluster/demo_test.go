package cluster_test

// Phase 24 — Local Cluster Demo tests.
//
// These tests verify the demo-3node config file that drives the Phase 24
// reproducible multi-node local cluster demo. They do not start any processes.
// All assertions are pure in-memory: config parsing, validation, uniqueness,
// and deterministic routing.
//
// What is tested:
//   - configs/cluster/demo-3node.json loads and validates without error.
//   - All three nodes have unique IDs, unique base_urls, and unique data_dirs.
//   - The scope flags correctly assert no-Raft, no-consensus, no-failover, etc.
//   - Routing for sample keys is deterministic (same key → same node, always).
//   - Invalid configs (duplicate IDs, duplicate addresses) are rejected.
//
// What is NOT tested (by design):
//   - Starting actual node processes.
//   - Docker or network connectivity.
//   - Automatic failover (none exists; proxy returns 502 on node failure).
//   - Shard migration (not implemented).
//   - Consensus (not implemented).

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/YashPatel2395/ShardForgeDB/internal/cluster"
	"github.com/YashPatel2395/ShardForgeDB/internal/gateway"
)

// demoConfigPath returns the absolute path to configs/cluster/demo-3node.json.
func demoConfigPath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// file is .../internal/cluster/demo_test.go — go up 3 levels to repo root
	root := filepath.Join(filepath.Dir(file), "..", "..")
	return filepath.Join(root, "configs", "cluster", "demo-3node.json")
}

// TestDemoConfig_LoadsAndValidates verifies the Phase 24 demo config is a valid cluster config.
func TestDemoConfig_LoadsAndValidates(t *testing.T) {
	cfg, err := cluster.Load(demoConfigPath(t))
	if err != nil {
		t.Fatalf("Load demo-3node.json: %v", err)
	}
	if err := cluster.Validate(cfg); err != nil {
		t.Fatalf("Validate demo-3node.json: %v", err)
	}
}

// TestDemoConfig_ThreeNodes verifies the demo cluster has exactly 3 nodes.
func TestDemoConfig_ThreeNodes(t *testing.T) {
	cfg, err := cluster.Load(demoConfigPath(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Nodes) != 3 {
		t.Errorf("want 3 nodes, got %d", len(cfg.Nodes))
	}
}

// TestDemoConfig_UniqueNodeIDs verifies all three nodes have distinct IDs.
func TestDemoConfig_UniqueNodeIDs(t *testing.T) {
	cfg, err := cluster.Load(demoConfigPath(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	seen := make(map[string]struct{}, len(cfg.Nodes))
	for _, n := range cfg.Nodes {
		if _, dup := seen[n.ID]; dup {
			t.Errorf("duplicate node ID: %q", n.ID)
		}
		seen[n.ID] = struct{}{}
	}
}

// TestDemoConfig_UniqueNodeAddresses verifies all three nodes have distinct base_urls.
func TestDemoConfig_UniqueNodeAddresses(t *testing.T) {
	cfg, err := cluster.Load(demoConfigPath(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	seen := make(map[string]struct{}, len(cfg.Nodes))
	for _, n := range cfg.Nodes {
		if _, dup := seen[n.BaseURL]; dup {
			t.Errorf("duplicate node base_url: %q", n.BaseURL)
		}
		seen[n.BaseURL] = struct{}{}
	}
}

// TestDemoConfig_UniqueDataDirs verifies all three nodes have distinct data_dir paths.
func TestDemoConfig_UniqueDataDirs(t *testing.T) {
	cfg, err := cluster.Load(demoConfigPath(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	seen := make(map[string]struct{}, len(cfg.Nodes))
	for _, n := range cfg.Nodes {
		if n.DataDir == "" {
			t.Errorf("node %q has empty data_dir", n.ID)
			continue
		}
		if _, dup := seen[n.DataDir]; dup {
			t.Errorf("duplicate node data_dir: %q", n.DataDir)
		}
		seen[n.DataDir] = struct{}{}
	}
}

// TestDemoConfig_ScopeFlagsNoRaftNoConsensus verifies the demo config honestly documents
// that it does not implement Raft, consensus, failover, shard migration, or replication.
func TestDemoConfig_ScopeFlagsNoRaftNoConsensus(t *testing.T) {
	cfg, err := cluster.Load(demoConfigPath(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := cfg.Scope
	checks := []struct {
		flag bool
		name string
	}{
		{s.StaticConfigOnly, "static_config_only"},
		{s.NoDynamicMembership, "no_dynamic_membership"},
		{s.NoDiscovery, "no_discovery"},
		{s.NoConsensus, "no_consensus"},
		{s.NoRaft, "no_raft"},
		{s.NoReplication, "no_replication"},
		{s.NoFailover, "no_failover"},
		{s.NoShardMigration, "no_shard_migration"},
		{s.NoDistributedTxns, "no_distributed_txns"},
	}
	for _, c := range checks {
		if !c.flag {
			t.Errorf("scope.%s must be true (honest capability documentation)", c.name)
		}
	}
}

// TestDemoConfig_DeterministicRouting verifies that routing for a fixed key is stable
// across multiple calls. The same key must always resolve to the same node.
//
// Routing depends on the FNV-1a ring topology (3 nodes, 128 virtual nodes each).
// These specific routes are stable for the Phase 24 demo config and are
// asserted here as a regression guard. If the node list changes, these will fail
// and should be updated to match the new topology.
func TestDemoConfig_DeterministicRouting(t *testing.T) {
	cfg, err := cluster.Load(demoConfigPath(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	opts, err := cluster.GatewayOptions(cfg, 0)
	if err != nil {
		t.Fatalf("GatewayOptions: %v", err)
	}
	gw, err := gateway.Open(opts)
	if err != nil {
		t.Fatalf("gateway.Open: %v", err)
	}

	// Run each key 3 times and verify the node is the same each time.
	keys := []string{"user:1", "user:2", "order:9", "item:42", "session:abc"}
	for _, key := range keys {
		var firstNode string
		for i := 0; i < 3; i++ {
			rn, err := gw.NodeForKey([]byte(key))
			if err != nil {
				t.Fatalf("NodeForKey(%q) run %d: %v", key, i+1, err)
			}
			if firstNode == "" {
				firstNode = rn.ID
			} else if rn.ID != firstNode {
				t.Errorf("key %q: non-deterministic routing: got %q on run %d, want %q",
					key, rn.ID, i+1, firstNode)
			}
		}
	}
}

// TestDemoConfig_KnownKeyRoutes documents the expected routing for sample keys
// in the Phase 24 demo ring (3 nodes, 128 virtual nodes, FNV-1a).
// This serves as a key-placement proof: user:1→node-2, order:9→node-1.
// These are stable for the demo-3node topology and serve as regression guards.
func TestDemoConfig_KnownKeyRoutes(t *testing.T) {
	cfg, err := cluster.Load(demoConfigPath(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	opts, err := cluster.GatewayOptions(cfg, 0)
	if err != nil {
		t.Fatalf("GatewayOptions: %v", err)
	}
	gw, err := gateway.Open(opts)
	if err != nil {
		t.Fatalf("gateway.Open: %v", err)
	}

	// Known-stable routing for the Phase 24 demo ring.
	// Verified by running: ./bin/shardforge-gateway --config configs/cluster/demo-3node.json route <key>
	expected := map[string]string{
		"user:1":  "node-2",
		"user:2":  "node-2",
		"order:9": "node-1",
	}
	for key, wantNode := range expected {
		rn, err := gw.NodeForKey([]byte(key))
		if err != nil {
			t.Fatalf("NodeForKey(%q): %v", key, err)
		}
		if rn.ID != wantNode {
			t.Errorf("key %q routes to %q, want %q (FNV-1a ring regression)", key, rn.ID, wantNode)
		}
	}
}

// TestDemoConfig_InvalidDuplicateID verifies the cluster package rejects configs
// with duplicate node IDs.
func TestDemoConfig_InvalidDuplicateID(t *testing.T) {
	cfg := cluster.Normalize(cluster.Config{
		Version: cluster.CurrentVersion,
		Name:    "bad-duplicate-id",
		Routing: cluster.Routing{
			Algorithm:    cluster.RoutingFNV1AConsistentHash,
			VirtualNodes: 128,
		},
		Nodes: []cluster.Node{
			{ID: "node-1", BaseURL: "http://127.0.0.1:9101"},
			{ID: "node-1", BaseURL: "http://127.0.0.1:9102"}, // duplicate ID
			{ID: "node-3", BaseURL: "http://127.0.0.1:9103"},
		},
		Scope: cluster.DefaultScope(),
	})
	if err := cluster.Validate(cfg); err == nil {
		t.Fatal("expected error for duplicate node ID, got nil")
	}
}

// TestDemoConfig_InvalidDuplicateAddress verifies the cluster package rejects configs
// with duplicate node base_urls.
func TestDemoConfig_InvalidDuplicateAddress(t *testing.T) {
	cfg := cluster.Normalize(cluster.Config{
		Version: cluster.CurrentVersion,
		Name:    "bad-duplicate-addr",
		Routing: cluster.Routing{
			Algorithm:    cluster.RoutingFNV1AConsistentHash,
			VirtualNodes: 128,
		},
		Nodes: []cluster.Node{
			{ID: "node-1", BaseURL: "http://127.0.0.1:9101"},
			{ID: "node-2", BaseURL: "http://127.0.0.1:9101"}, // duplicate base_url
			{ID: "node-3", BaseURL: "http://127.0.0.1:9103"},
		},
		Scope: cluster.DefaultScope(),
	})
	if err := cluster.Validate(cfg); err == nil {
		t.Fatal("expected error for duplicate node base_url, got nil")
	}
}

// TestDemoConfig_InvalidScopeMissingRaftFlag verifies configs with false no_raft
// are rejected — enforcing honest capability documentation.
func TestDemoConfig_InvalidScopeMissingRaftFlag(t *testing.T) {
	cfg := cluster.Normalize(cluster.Config{
		Version: cluster.CurrentVersion,
		Name:    "bad-scope",
		Routing: cluster.Routing{
			Algorithm:    cluster.RoutingFNV1AConsistentHash,
			VirtualNodes: 128,
		},
		Nodes: []cluster.Node{
			{ID: "node-1", BaseURL: "http://127.0.0.1:9101"},
		},
		Scope: cluster.Scope{
			StaticConfigOnly:    true,
			NoDynamicMembership: true,
			NoDiscovery:         true,
			NoConsensus:         true,
			NoRaft:              false, // missing: must be true
			NoReplication:       true,
			NoFailover:          true,
			NoShardMigration:    true,
			NoDistributedTxns:   true,
		},
	})
	if err := cluster.Validate(cfg); err == nil {
		t.Fatal("expected error when no_raft=false, got nil")
	}
}

// TestDemoConfig_ProxyEnabled verifies the demo config has the proxy enabled.
func TestDemoConfig_ProxyEnabled(t *testing.T) {
	cfg, err := cluster.Load(demoConfigPath(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Proxy.Enabled {
		t.Error("demo config should have proxy.enabled=true")
	}
	if cfg.Proxy.Addr == "" {
		t.Error("demo config should have proxy.addr set")
	}
}

// TestDemoConfig_NodeDataDirsAreAbsolutePaths verifies data_dir values are absolute
// paths (start with '/') — relative paths would be ambiguous for local demo startup.
func TestDemoConfig_NodeDataDirsAreAbsolutePaths(t *testing.T) {
	cfg, err := cluster.Load(demoConfigPath(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, n := range cfg.Nodes {
		if n.DataDir == "" {
			t.Errorf("node %q: data_dir is empty", n.ID)
			continue
		}
		if n.DataDir[0] != '/' {
			t.Errorf("node %q: data_dir %q is not absolute", n.ID, n.DataDir)
		}
	}
}
