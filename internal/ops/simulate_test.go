package ops_test

import (
	"errors"
	"testing"

	"github.com/YashPatel2395/ShardForgeDB/internal/cluster"
	"github.com/YashPatel2395/ShardForgeDB/internal/ops"
)

// threeNodeCfg returns a standard 3-node cluster config for simulation tests.
func threeNodeCfg() cluster.Config {
	return cluster.DefaultConfig("test-cluster", []cluster.Node{
		{ID: "node-1", BaseURL: "http://127.0.0.1:9101"},
		{ID: "node-2", BaseURL: "http://127.0.0.1:9102"},
		{ID: "node-3", BaseURL: "http://127.0.0.1:9103"},
	})
}

// TestRouteKey_Deterministic verifies same key always routes to same node.
func TestRouteKey_Deterministic(t *testing.T) {
	cfg := threeNodeCfg()
	n1, err := ops.RouteKey(cfg, "user:1")
	if err != nil {
		t.Fatalf("RouteKey: %v", err)
	}
	n2, err := ops.RouteKey(cfg, "user:1")
	if err != nil {
		t.Fatalf("RouteKey second call: %v", err)
	}
	if n1 != n2 {
		t.Errorf("RouteKey not deterministic: got %q then %q", n1, n2)
	}
}

func TestRouteKey_EmptyKey_ReturnsError(t *testing.T) {
	cfg := threeNodeCfg()
	_, err := ops.RouteKey(cfg, "")
	if err == nil {
		t.Fatal("expected error for empty key, got nil")
	}
}

func TestRouteKey_InvalidConfig_ReturnsError(t *testing.T) {
	cfg := cluster.Config{} // invalid
	_, err := ops.RouteKey(cfg, "key")
	if err == nil {
		t.Fatal("expected error for invalid config, got nil")
	}
}

func TestRouteKey_ReturnsKnownNodeID(t *testing.T) {
	cfg := threeNodeCfg()
	nodeID, err := ops.RouteKey(cfg, "order:42")
	if err != nil {
		t.Fatalf("RouteKey: %v", err)
	}
	valid := map[string]bool{"node-1": true, "node-2": true, "node-3": true}
	if !valid[nodeID] {
		t.Errorf("RouteKey returned unknown nodeID %q", nodeID)
	}
}

func TestRouteKeyWithAvailableNodes_ExcludesDownNode(t *testing.T) {
	cfg := threeNodeCfg()

	// Find which node user:1 normally routes to.
	origNode, err := ops.RouteKey(cfg, "user:1")
	if err != nil {
		t.Fatalf("RouteKey: %v", err)
	}

	// Exclude origNode; result should differ or be one of the remaining nodes.
	remaining := make([]string, 0, 2)
	for _, n := range cfg.Nodes {
		if n.ID != origNode {
			remaining = append(remaining, n.ID)
		}
	}

	newNode, err := ops.RouteKeyWithAvailableNodes(cfg, "user:1", remaining)
	if err != nil {
		t.Fatalf("RouteKeyWithAvailableNodes: %v", err)
	}

	// Result must not be the excluded node.
	if newNode == origNode {
		t.Errorf("RouteKeyWithAvailableNodes returned excluded node %q", origNode)
	}
	// Result must be one of the remaining nodes.
	valid := make(map[string]bool)
	for _, id := range remaining {
		valid[id] = true
	}
	if !valid[newNode] {
		t.Errorf("RouteKeyWithAvailableNodes returned unknown node %q", newNode)
	}
}

func TestRouteKeyWithAvailableNodes_NoHealthyNodes_ReturnsError(t *testing.T) {
	cfg := threeNodeCfg()
	_, err := ops.RouteKeyWithAvailableNodes(cfg, "user:1", []string{})
	if !errors.Is(err, ops.ErrNoHealthyNodes) {
		t.Errorf("expected ErrNoHealthyNodes, got %v", err)
	}
}

func TestRouteKeyWithAvailableNodes_NonExistentIDs_ReturnsError(t *testing.T) {
	cfg := threeNodeCfg()
	_, err := ops.RouteKeyWithAvailableNodes(cfg, "user:1", []string{"phantom-node"})
	if !errors.Is(err, ops.ErrNoHealthyNodes) {
		t.Errorf("expected ErrNoHealthyNodes for all non-existent IDs, got %v", err)
	}
}

func TestSimulateFailure_UnknownDownNode_ReturnsErrUnknownNode(t *testing.T) {
	cfg := threeNodeCfg()
	_, err := ops.SimulateFailure(cfg, ops.FailureSimulationRequest{
		DownNodeIDs: []string{"node-99"},
		SampleKeys:  []string{"user:1"},
	})
	if !errors.Is(err, ops.ErrUnknownNode) {
		t.Errorf("expected ErrUnknownNode, got %v", err)
	}
}

func TestSimulateFailure_NoSampleKeys_ReturnsErrInvalidSimulation(t *testing.T) {
	cfg := threeNodeCfg()
	_, err := ops.SimulateFailure(cfg, ops.FailureSimulationRequest{
		DownNodeIDs: []string{"node-1"},
		SampleKeys:  nil,
	})
	if !errors.Is(err, ops.ErrInvalidSimulation) {
		t.Errorf("expected ErrInvalidSimulation, got %v", err)
	}
}

func TestSimulateFailure_OneDownNode_ReportsAffectedKeys(t *testing.T) {
	cfg := threeNodeCfg()

	// Find which key routes to node-1.
	var keyForNode1 string
	for _, candidate := range []string{"user:1", "user:2", "order:1", "order:2", "product:1", "product:2", "item:a", "item:b"} {
		n, _ := ops.RouteKey(cfg, candidate)
		if n == "node-1" {
			keyForNode1 = candidate
			break
		}
	}
	if keyForNode1 == "" {
		t.Skip("could not find a key routing to node-1 in test candidates")
	}

	res, err := ops.SimulateFailure(cfg, ops.FailureSimulationRequest{
		DownNodeIDs: []string{"node-1"},
		SampleKeys:  []string{keyForNode1},
	})
	if err != nil {
		t.Fatalf("SimulateFailure: %v", err)
	}
	if res.Summary.AffectedKeys != 1 {
		t.Errorf("AffectedKeys = %d, want 1", res.Summary.AffectedKeys)
	}
}

func TestSimulateFailure_NoDownNodes_NoAffectedKeys(t *testing.T) {
	cfg := threeNodeCfg()
	res, err := ops.SimulateFailure(cfg, ops.FailureSimulationRequest{
		DownNodeIDs: []string{},
		SampleKeys:  []string{"user:1", "user:2", "order:1"},
	})
	if err != nil {
		t.Fatalf("SimulateFailure: %v", err)
	}
	if res.Summary.AffectedKeys != 0 {
		t.Errorf("AffectedKeys = %d, want 0 when no nodes down", res.Summary.AffectedKeys)
	}
	if res.Summary.UnaffectedKeys != 3 {
		t.Errorf("UnaffectedKeys = %d, want 3", res.Summary.UnaffectedKeys)
	}
}

func TestSimulateFailure_AllNodesDown_MarksKeysUnavailable(t *testing.T) {
	cfg := threeNodeCfg()
	res, err := ops.SimulateFailure(cfg, ops.FailureSimulationRequest{
		DownNodeIDs: []string{"node-1", "node-2", "node-3"},
		SampleKeys:  []string{"user:1", "user:2", "order:1"},
	})
	if err != nil {
		t.Fatalf("SimulateFailure: %v", err)
	}
	if res.Summary.UnavailableKeys != 3 {
		t.Errorf("UnavailableKeys = %d, want 3", res.Summary.UnavailableKeys)
	}
	if len(res.HealthyNodeIDs) != 0 {
		t.Errorf("HealthyNodeIDs should be empty, got %v", res.HealthyNodeIDs)
	}
}

func TestSimulateFailure_ScopeAlwaysTrue(t *testing.T) {
	cfg := threeNodeCfg()
	res, _ := ops.SimulateFailure(cfg, ops.FailureSimulationRequest{
		SampleKeys: []string{"user:1"},
	})
	if !res.Scope.NoAutomaticFailover || !res.Scope.NoRaft || !res.Scope.ManualOnly {
		t.Errorf("scope flags not all true: %+v", res.Scope)
	}
}

func TestSimulateFailure_DuplicateDownNodes_HandledSafely(t *testing.T) {
	cfg := threeNodeCfg()
	res, err := ops.SimulateFailure(cfg, ops.FailureSimulationRequest{
		DownNodeIDs: []string{"node-1", "node-1", "node-1"},
		SampleKeys:  []string{"user:1"},
	})
	if err != nil {
		t.Fatalf("SimulateFailure with duplicate down nodes: %v", err)
	}
	// Should treat node-1 as down exactly once.
	if len(res.DownNodeIDs) != 1 {
		t.Errorf("DownNodeIDs after dedup = %v, want exactly [node-1]", res.DownNodeIDs)
	}
}

func TestSimulateFailure_SummaryTotalsConsistent(t *testing.T) {
	cfg := threeNodeCfg()
	keys := []string{"user:1", "user:2", "order:1", "order:2", "product:1"}
	res, err := ops.SimulateFailure(cfg, ops.FailureSimulationRequest{
		DownNodeIDs: []string{"node-2"},
		SampleKeys:  keys,
	})
	if err != nil {
		t.Fatalf("SimulateFailure: %v", err)
	}
	total := res.Summary.AffectedKeys + res.Summary.UnaffectedKeys
	if total != len(keys) {
		t.Errorf("AffectedKeys(%d) + UnaffectedKeys(%d) = %d, want %d",
			res.Summary.AffectedKeys, res.Summary.UnaffectedKeys, total, len(keys))
	}
	if res.Summary.TotalKeys != len(keys) {
		t.Errorf("TotalKeys = %d, want %d", res.Summary.TotalKeys, len(keys))
	}
}

func TestSimulateFailure_EmptyKeyBehavior(t *testing.T) {
	cfg := threeNodeCfg()
	// An empty key in the sample list: the function should handle it gracefully
	// by marking it unavailable (since RouteKey returns an error for empty key).
	res, err := ops.SimulateFailure(cfg, ops.FailureSimulationRequest{
		DownNodeIDs: []string{},
		SampleKeys:  []string{""},
	})
	if err != nil {
		t.Fatalf("SimulateFailure with empty key: %v", err)
	}
	// Empty key should appear in affected (unavailable) since routing fails.
	if res.Summary.TotalKeys != 1 {
		t.Errorf("TotalKeys = %d, want 1", res.Summary.TotalKeys)
	}
}

func TestSimulateFailure_HealthyNodeIDsExcludeDownNodes(t *testing.T) {
	cfg := threeNodeCfg()
	res, err := ops.SimulateFailure(cfg, ops.FailureSimulationRequest{
		DownNodeIDs: []string{"node-1"},
		SampleKeys:  []string{"user:1"},
	})
	if err != nil {
		t.Fatalf("SimulateFailure: %v", err)
	}
	for _, id := range res.HealthyNodeIDs {
		if id == "node-1" {
			t.Errorf("HealthyNodeIDs contains down node node-1: %v", res.HealthyNodeIDs)
		}
	}
	if len(res.HealthyNodeIDs) != 2 {
		t.Errorf("HealthyNodeIDs = %v, want 2 nodes", res.HealthyNodeIDs)
	}
}
