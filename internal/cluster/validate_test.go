package cluster_test

import (
	"errors"
	"testing"

	"github.com/YashPatel2395/ShardForgeDB/internal/cluster"
)

// validConfig returns a minimal valid normalised Config for use in tests.
func validConfig() cluster.Config {
	return cluster.Normalize(cluster.Config{
		Version: cluster.CurrentVersion,
		Name:    "test",
		Routing: cluster.Routing{
			Algorithm:    cluster.RoutingFNV1AConsistentHash,
			VirtualNodes: 128,
		},
		Nodes: []cluster.Node{
			{ID: "n1", BaseURL: "http://127.0.0.1:9101"},
		},
		Scope: cluster.DefaultScope(),
	})
}

func TestValidate_ValidConfig(t *testing.T) {
	if err := cluster.Validate(validConfig()); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestValidate_EmptyVersion_ReturnsUnsupportedVersion(t *testing.T) {
	cfg := validConfig()
	cfg.Version = ""
	err := cluster.Validate(cfg)
	if !errors.Is(err, cluster.ErrUnsupportedVersion) {
		t.Errorf("expected ErrUnsupportedVersion, got %v", err)
	}
}

func TestValidate_WrongVersion_ReturnsUnsupportedVersion(t *testing.T) {
	cfg := validConfig()
	cfg.Version = "v99"
	err := cluster.Validate(cfg)
	if !errors.Is(err, cluster.ErrUnsupportedVersion) {
		t.Errorf("expected ErrUnsupportedVersion, got %v", err)
	}
}

func TestValidate_EmptyName_ReturnsInvalidConfig(t *testing.T) {
	cfg := validConfig()
	cfg.Name = ""
	err := cluster.Validate(cfg)
	if !errors.Is(err, cluster.ErrInvalidConfig) {
		t.Errorf("expected ErrInvalidConfig for empty name, got %v", err)
	}
}

func TestValidate_WrongAlgorithm_ReturnsUnsupportedRouting(t *testing.T) {
	cfg := validConfig()
	cfg.Routing.Algorithm = "raft-ring"
	err := cluster.Validate(cfg)
	if !errors.Is(err, cluster.ErrUnsupportedRouting) {
		t.Errorf("expected ErrUnsupportedRouting, got %v", err)
	}
}

func TestValidate_ZeroVirtualNodes_ReturnsInvalidConfig(t *testing.T) {
	cfg := validConfig()
	cfg.Routing.VirtualNodes = 0
	err := cluster.Validate(cfg)
	if !errors.Is(err, cluster.ErrInvalidConfig) {
		t.Errorf("expected ErrInvalidConfig for zero virtual_nodes, got %v", err)
	}
}

func TestValidate_EmptyNodes_ReturnsInvalidConfig(t *testing.T) {
	cfg := validConfig()
	cfg.Nodes = nil
	err := cluster.Validate(cfg)
	if !errors.Is(err, cluster.ErrInvalidConfig) {
		t.Errorf("expected ErrInvalidConfig for empty nodes, got %v", err)
	}
}

func TestValidate_EmptyNodeID_ReturnsInvalidConfig(t *testing.T) {
	cfg := validConfig()
	cfg.Nodes[0].ID = ""
	err := cluster.Validate(cfg)
	if !errors.Is(err, cluster.ErrInvalidConfig) {
		t.Errorf("expected ErrInvalidConfig for empty node ID, got %v", err)
	}
}

func TestValidate_EmptyBaseURL_ReturnsInvalidConfig(t *testing.T) {
	cfg := validConfig()
	cfg.Nodes[0].BaseURL = ""
	err := cluster.Validate(cfg)
	if !errors.Is(err, cluster.ErrInvalidConfig) {
		t.Errorf("expected ErrInvalidConfig for empty base_url, got %v", err)
	}
}

func TestValidate_NonHTTPBaseURL_ReturnsInvalidConfig(t *testing.T) {
	cfg := validConfig()
	cfg.Nodes[0].BaseURL = "grpc://127.0.0.1:9101"
	err := cluster.Validate(cfg)
	if !errors.Is(err, cluster.ErrInvalidConfig) {
		t.Errorf("expected ErrInvalidConfig for non-http base_url, got %v", err)
	}
}

func TestValidate_HTTPSBaseURL_Valid(t *testing.T) {
	cfg := validConfig()
	cfg.Nodes[0].BaseURL = "https://example.com:9101"
	err := cluster.Validate(cfg)
	if err != nil {
		t.Errorf("expected nil error for https base_url, got %v", err)
	}
}

func TestValidate_DuplicateNodeIDs_ReturnsInvalidConfig(t *testing.T) {
	cfg := validConfig()
	cfg.Nodes = []cluster.Node{
		{ID: "n1", BaseURL: "http://127.0.0.1:9101", Weight: 1},
		{ID: "n1", BaseURL: "http://127.0.0.1:9102", Weight: 1},
	}
	err := cluster.Validate(cfg)
	if !errors.Is(err, cluster.ErrInvalidConfig) {
		t.Errorf("expected ErrInvalidConfig for duplicate node IDs, got %v", err)
	}
}

func TestValidate_DuplicateBaseURLs_ReturnsInvalidConfig(t *testing.T) {
	cfg := validConfig()
	cfg.Nodes = []cluster.Node{
		{ID: "n1", BaseURL: "http://127.0.0.1:9101", Weight: 1},
		{ID: "n2", BaseURL: "http://127.0.0.1:9101", Weight: 1},
	}
	err := cluster.Validate(cfg)
	if !errors.Is(err, cluster.ErrInvalidConfig) {
		t.Errorf("expected ErrInvalidConfig for duplicate base URLs, got %v", err)
	}
}

func TestValidate_ZeroWeight_ReturnsInvalidConfig(t *testing.T) {
	cfg := validConfig()
	cfg.Nodes[0].Weight = 0
	err := cluster.Validate(cfg)
	if !errors.Is(err, cluster.ErrInvalidConfig) {
		t.Errorf("expected ErrInvalidConfig for zero weight (use Normalize first), got %v", err)
	}
}

func TestValidate_ScopeFalseFlag_ReturnsInvalidConfig(t *testing.T) {
	cfg := validConfig()
	cfg.Scope.NoRaft = false
	err := cluster.Validate(cfg)
	if !errors.Is(err, cluster.ErrInvalidConfig) {
		t.Errorf("expected ErrInvalidConfig when scope.no_raft=false, got %v", err)
	}
}

func TestValidate_ScopeNoReplicationFalse_ReturnsInvalidConfig(t *testing.T) {
	cfg := validConfig()
	cfg.Scope.NoReplication = false
	err := cluster.Validate(cfg)
	if !errors.Is(err, cluster.ErrInvalidConfig) {
		t.Errorf("expected ErrInvalidConfig when scope.no_replication=false, got %v", err)
	}
}

func TestValidate_DataDirWithTraversal_ReturnsInvalidConfig(t *testing.T) {
	cfg := validConfig()
	cfg.Nodes[0].DataDir = "/data/../secret"
	err := cluster.Validate(cfg)
	if !errors.Is(err, cluster.ErrInvalidConfig) {
		t.Errorf("expected ErrInvalidConfig for data_dir with '..', got %v", err)
	}
}

func TestValidate_AbsoluteDataDir_Valid(t *testing.T) {
	cfg := validConfig()
	cfg.Nodes[0].DataDir = "/data/node-1"
	if err := cluster.Validate(cfg); err != nil {
		t.Errorf("expected nil for absolute data_dir, got %v", err)
	}
}

func TestValidate_DoesNotMutateOriginal(t *testing.T) {
	cfg := validConfig()
	origName := cfg.Name
	origAlgo := cfg.Routing.Algorithm
	_ = cluster.Validate(cfg)
	if cfg.Name != origName {
		t.Error("Validate mutated cfg.Name")
	}
	if cfg.Routing.Algorithm != origAlgo {
		t.Error("Validate mutated cfg.Routing.Algorithm")
	}
}
