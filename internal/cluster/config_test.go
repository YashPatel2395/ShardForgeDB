package cluster_test

import (
	"testing"

	"github.com/YashPatel2395/ShardForgeDB/internal/cluster"
)

func TestDefaultScope_AllTrue(t *testing.T) {
	s := cluster.DefaultScope()
	if !s.StaticConfigOnly {
		t.Error("StaticConfigOnly must be true")
	}
	if !s.NoDynamicMembership {
		t.Error("NoDynamicMembership must be true")
	}
	if !s.NoDiscovery {
		t.Error("NoDiscovery must be true")
	}
	if !s.NoConsensus {
		t.Error("NoConsensus must be true")
	}
	if !s.NoRaft {
		t.Error("NoRaft must be true")
	}
	if !s.NoReplication {
		t.Error("NoReplication must be true")
	}
	if !s.NoFailover {
		t.Error("NoFailover must be true")
	}
	if !s.NoShardMigration {
		t.Error("NoShardMigration must be true")
	}
	if !s.NoDistributedTxns {
		t.Error("NoDistributedTxns must be true")
	}
}

func TestExampleLocal3Node_ValidatesCleanly(t *testing.T) {
	cfg := cluster.ExampleLocal3Node()
	if err := cluster.Validate(cfg); err != nil {
		t.Fatalf("ExampleLocal3Node() failed validation: %v", err)
	}
}

func TestExampleLocal3Node_HasThreeNodes(t *testing.T) {
	cfg := cluster.ExampleLocal3Node()
	if len(cfg.Nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(cfg.Nodes))
	}
}

func TestDefaultConfig_ValidatesCleanly(t *testing.T) {
	nodes := []cluster.Node{
		{ID: "n1", BaseURL: "http://127.0.0.1:9101"},
		{ID: "n2", BaseURL: "http://127.0.0.1:9102"},
	}
	cfg := cluster.DefaultConfig("test", nodes)
	if err := cluster.Validate(cfg); err != nil {
		t.Fatalf("DefaultConfig() failed validation: %v", err)
	}
}

func TestDefaultConfig_HasCorrectVersion(t *testing.T) {
	cfg := cluster.DefaultConfig("test", []cluster.Node{
		{ID: "n1", BaseURL: "http://127.0.0.1:9101"},
	})
	if cfg.Version != cluster.CurrentVersion {
		t.Errorf("Version = %q, want %q", cfg.Version, cluster.CurrentVersion)
	}
}

func TestDefaultConfig_HasCorrectAlgorithm(t *testing.T) {
	cfg := cluster.DefaultConfig("test", []cluster.Node{
		{ID: "n1", BaseURL: "http://127.0.0.1:9101"},
	})
	if cfg.Routing.Algorithm != cluster.RoutingFNV1AConsistentHash {
		t.Errorf("Algorithm = %q, want %q", cfg.Routing.Algorithm, cluster.RoutingFNV1AConsistentHash)
	}
}

func TestNormalize_FillsVirtualNodes(t *testing.T) {
	cfg := cluster.Config{
		Version: cluster.CurrentVersion,
		Name:    "x",
		Routing: cluster.Routing{Algorithm: cluster.RoutingFNV1AConsistentHash, VirtualNodes: 0},
		Nodes:   []cluster.Node{{ID: "n1", BaseURL: "http://127.0.0.1:9101"}},
		Scope:   cluster.DefaultScope(),
	}
	out := cluster.Normalize(cfg)
	if out.Routing.VirtualNodes != cluster.DefaultVirtualNodes {
		t.Errorf("VirtualNodes = %d, want %d", out.Routing.VirtualNodes, cluster.DefaultVirtualNodes)
	}
}

func TestNormalize_FillsProxyAddr(t *testing.T) {
	cfg := cluster.Config{
		Version: cluster.CurrentVersion,
		Name:    "x",
		Routing: cluster.Routing{Algorithm: cluster.RoutingFNV1AConsistentHash, VirtualNodes: 128},
		Proxy:   cluster.Proxy{Addr: ""},
		Nodes:   []cluster.Node{{ID: "n1", BaseURL: "http://127.0.0.1:9101"}},
		Scope:   cluster.DefaultScope(),
	}
	out := cluster.Normalize(cfg)
	if out.Proxy.Addr != cluster.DefaultProxyAddr {
		t.Errorf("Proxy.Addr = %q, want %q", out.Proxy.Addr, cluster.DefaultProxyAddr)
	}
}

func TestNormalize_FillsNodeWeight(t *testing.T) {
	cfg := cluster.Config{
		Version: cluster.CurrentVersion,
		Name:    "x",
		Routing: cluster.Routing{Algorithm: cluster.RoutingFNV1AConsistentHash, VirtualNodes: 128},
		Nodes:   []cluster.Node{{ID: "n1", BaseURL: "http://127.0.0.1:9101", Weight: 0}},
		Scope:   cluster.DefaultScope(),
	}
	out := cluster.Normalize(cfg)
	if out.Nodes[0].Weight != 1 {
		t.Errorf("Node[0].Weight = %d, want 1", out.Nodes[0].Weight)
	}
}

func TestNormalize_FillsScope_WhenAllFalse(t *testing.T) {
	cfg := cluster.Config{
		Version: cluster.CurrentVersion,
		Name:    "x",
		Routing: cluster.Routing{Algorithm: cluster.RoutingFNV1AConsistentHash, VirtualNodes: 128},
		Nodes:   []cluster.Node{{ID: "n1", BaseURL: "http://127.0.0.1:9101", Weight: 1}},
		Scope:   cluster.Scope{}, // all false
	}
	out := cluster.Normalize(cfg)
	if !out.Scope.NoRaft {
		t.Error("Normalize did not fill scope when all flags were false")
	}
}

func TestNormalize_DoesNotOverrideExistingVirtualNodes(t *testing.T) {
	cfg := cluster.Config{
		Version: cluster.CurrentVersion,
		Name:    "x",
		Routing: cluster.Routing{Algorithm: cluster.RoutingFNV1AConsistentHash, VirtualNodes: 64},
		Nodes:   []cluster.Node{{ID: "n1", BaseURL: "http://127.0.0.1:9101", Weight: 1}},
		Scope:   cluster.DefaultScope(),
	}
	out := cluster.Normalize(cfg)
	if out.Routing.VirtualNodes != 64 {
		t.Errorf("VirtualNodes = %d, want 64 (Normalize must not override explicit value)", out.Routing.VirtualNodes)
	}
}

func TestNormalize_DoesNotMutateOriginal(t *testing.T) {
	original := cluster.Config{
		Version: cluster.CurrentVersion,
		Name:    "x",
		Routing: cluster.Routing{Algorithm: cluster.RoutingFNV1AConsistentHash, VirtualNodes: 0},
		Nodes:   []cluster.Node{{ID: "n1", BaseURL: "http://127.0.0.1:9101", Weight: 0}},
	}
	_ = cluster.Normalize(original)
	if original.Routing.VirtualNodes != 0 {
		t.Error("Normalize mutated the original VirtualNodes")
	}
	if original.Nodes[0].Weight != 0 {
		t.Error("Normalize mutated the original Node.Weight")
	}
}
