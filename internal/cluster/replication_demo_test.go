package cluster_test

// Phase 25 — Replication demo config tests.
//
// Tests the configs/replication/demo-leader-follower.json cluster config that
// describes Phase 25's leader+follower replication demo.
//
// Scope: static config validation only. No Raft, no consensus, no quorum, no failover.
// Placed in internal/cluster to avoid the node→cluster→gateway→node import cycle.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/YashPatel2395/ShardForgeDB/internal/cluster"
	"github.com/YashPatel2395/ShardForgeDB/internal/gateway"
)

// repoRootForRepl returns the absolute path to the repository root, relative to this file.
func repoRootForRepl(tb testing.TB) string {
	tb.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		tb.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
}

func loadReplDemoConfig(tb testing.TB) cluster.Config {
	tb.Helper()
	root := repoRootForRepl(tb)
	path := filepath.Join(root, "configs", "replication", "demo-leader-follower.json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		tb.Fatalf("replication demo config not found: %s", path)
	}
	cfg, err := cluster.Load(path)
	if err != nil {
		tb.Fatalf("cluster.Load: %v", err)
	}
	return cfg
}

func TestPhase25_ReplDemoConfig_LoadsAndValidates(t *testing.T) {
	cfg := loadReplDemoConfig(t)
	if err := cluster.Validate(cfg); err != nil {
		t.Fatalf("cluster.Validate: %v", err)
	}
}

func TestPhase25_ReplDemoConfig_TwoNodes(t *testing.T) {
	cfg := loadReplDemoConfig(t)
	if len(cfg.Nodes) != 2 {
		t.Errorf("node count = %d, want 2 (leader + follower)", len(cfg.Nodes))
	}
}

func TestPhase25_ReplDemoConfig_UniqueNodeIDs(t *testing.T) {
	cfg := loadReplDemoConfig(t)
	seen := make(map[string]struct{})
	for _, n := range cfg.Nodes {
		if _, dup := seen[n.ID]; dup {
			t.Errorf("duplicate node ID: %q", n.ID)
		}
		seen[n.ID] = struct{}{}
	}
}

func TestPhase25_ReplDemoConfig_UniqueNodeAddresses(t *testing.T) {
	cfg := loadReplDemoConfig(t)
	seen := make(map[string]struct{})
	for _, n := range cfg.Nodes {
		if _, dup := seen[n.BaseURL]; dup {
			t.Errorf("duplicate node base_url: %q", n.BaseURL)
		}
		seen[n.BaseURL] = struct{}{}
	}
}

func TestPhase25_ReplDemoConfig_UniqueDataDirs(t *testing.T) {
	cfg := loadReplDemoConfig(t)
	seen := make(map[string]struct{})
	for _, n := range cfg.Nodes {
		if n.DataDir == "" {
			continue
		}
		if _, dup := seen[n.DataDir]; dup {
			t.Errorf("duplicate node data_dir: %q", n.DataDir)
		}
		seen[n.DataDir] = struct{}{}
	}
}

func TestPhase25_ReplDemoConfig_ScopeFlagsNoRaftNoConsensus(t *testing.T) {
	cfg := loadReplDemoConfig(t)
	s := cfg.Scope
	if !s.NoRaft {
		t.Error("scope.no_raft must be true")
	}
	if !s.NoConsensus {
		t.Error("scope.no_consensus must be true")
	}
	if !s.NoFailover {
		t.Error("scope.no_failover must be true")
	}
	if !s.NoShardMigration {
		t.Error("scope.no_shard_migration must be true")
	}
	if !s.NoDistributedTxns {
		t.Error("scope.no_distributed_txns must be true")
	}
}

func TestPhase25_ReplDemoConfig_NoQuorumReplication(t *testing.T) {
	cfg := loadReplDemoConfig(t)
	s := cfg.Scope
	// Replication IS enabled, so no_replication must be false.
	if s.NoReplication {
		t.Error("scope.no_replication must be false (replication IS present in this config)")
	}
	// But no_quorum_replication must be true (no Raft, no quorum, no automatic failover).
	if !s.NoQuorumReplication {
		t.Error("scope.no_quorum_replication must be true (pull-based only, no quorum)")
	}
}

func TestPhase25_ReplDemoConfig_LeaderAndFollowerRoles(t *testing.T) {
	cfg := loadReplDemoConfig(t)
	var primaryCount, followerCount int
	for _, n := range cfg.Nodes {
		if n.Replication.Enabled {
			switch n.Replication.Role {
			case "primary":
				primaryCount++
			case "follower":
				followerCount++
			}
		}
	}
	if primaryCount != 1 {
		t.Errorf("primary count = %d, want 1", primaryCount)
	}
	if followerCount != 1 {
		t.Errorf("follower count = %d, want 1", followerCount)
	}
}

func TestPhase25_ReplDemoConfig_DeterministicRouting(t *testing.T) {
	cfg := loadReplDemoConfig(t)
	opts, err := cluster.GatewayOptions(cfg, 0)
	if err != nil {
		t.Fatalf("GatewayOptions: %v", err)
	}
	gw, err := gateway.Open(opts)
	if err != nil {
		t.Fatalf("gateway.Open: %v", err)
	}

	// Routing must be deterministic across 3 calls.
	for i := 0; i < 3; i++ {
		n1, err := gw.NodeForKey([]byte("repl:key:1"))
		if err != nil {
			t.Fatalf("NodeForKey run %d: %v", i, err)
		}
		n2, err := gw.NodeForKey([]byte("repl:key:1"))
		if err != nil {
			t.Fatalf("NodeForKey run %d (second): %v", i, err)
		}
		if n1.ID != n2.ID {
			t.Errorf("run %d: routing not deterministic for repl:key:1: %q vs %q", i, n1.ID, n2.ID)
		}
	}
}

func TestPhase25_ReplDemoConfig_InvalidDuplicateID(t *testing.T) {
	cfgJSON := `{
		"version": "v1",
		"name": "bad-replication",
		"routing": {"algorithm": "fnv1a-consistent-hash", "virtual_nodes": 128},
		"proxy": {"enabled": false, "addr": "127.0.0.1:9200"},
		"nodes": [
			{"id": "leader", "base_url": "http://127.0.0.1:9301", "weight": 1,
			 "replication": {"enabled": true, "role": "primary"}},
			{"id": "leader", "base_url": "http://127.0.0.1:9302", "weight": 1,
			 "replication": {"enabled": true, "role": "follower", "primary": "leader"}}
		],
		"scope": {"static_config_only": true, "no_dynamic_membership": true, "no_discovery": true,
		          "no_consensus": true, "no_raft": true, "no_replication": false,
		          "no_quorum_replication": true, "no_failover": true,
		          "no_shard_migration": true, "no_distributed_txns": true}
	}`

	var cfg cluster.Config
	if err := json.Unmarshal([]byte(cfgJSON), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	cfg = cluster.Normalize(cfg)
	if err := cluster.Validate(cfg); err == nil {
		t.Error("expected error for duplicate node ID, got nil")
	}
}

func TestPhase25_ReplDemoConfig_InvalidScopeMissingRaftFlag(t *testing.T) {
	cfgJSON := `{
		"version": "v1",
		"name": "bad-scope",
		"routing": {"algorithm": "fnv1a-consistent-hash", "virtual_nodes": 128},
		"proxy": {"enabled": false, "addr": "127.0.0.1:9200"},
		"nodes": [
			{"id": "leader", "base_url": "http://127.0.0.1:9301", "weight": 1,
			 "replication": {"enabled": true, "role": "primary"}},
			{"id": "follower", "base_url": "http://127.0.0.1:9302", "weight": 1,
			 "replication": {"enabled": true, "role": "follower", "primary": "leader"}}
		],
		"scope": {"static_config_only": true, "no_dynamic_membership": true, "no_discovery": true,
		          "no_consensus": true, "no_raft": false,
		          "no_replication": false, "no_quorum_replication": true,
		          "no_failover": true, "no_shard_migration": true, "no_distributed_txns": true}
	}`

	var cfg cluster.Config
	if err := json.Unmarshal([]byte(cfgJSON), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	cfg = cluster.Normalize(cfg)
	if err := cluster.Validate(cfg); err == nil {
		t.Error("expected error for no_raft=false, got nil")
	}
}

func TestPhase25_ReplDemoConfig_NodeDataDirsAreAbsolutePaths(t *testing.T) {
	cfg := loadReplDemoConfig(t)
	for _, n := range cfg.Nodes {
		if n.DataDir == "" {
			continue
		}
		if !filepath.IsAbs(n.DataDir) {
			t.Errorf("node %q data_dir %q is not an absolute path", n.ID, n.DataDir)
		}
	}
}
