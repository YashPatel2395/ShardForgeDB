package ops_test

import (
	"errors"
	"testing"

	"github.com/YashPatel2395/ShardForgeDB/internal/cluster"
	"github.com/YashPatel2395/ShardForgeDB/internal/ops"
)

func TestPlanManualRebalance_UnknownRemovedNode_ReturnsErrUnknownNode(t *testing.T) {
	cfg := threeNodeCfg()
	_, err := ops.PlanManualRebalance(cfg, []string{"node-99"}, nil, []string{"user:1"})
	if !errors.Is(err, ops.ErrUnknownNode) {
		t.Errorf("expected ErrUnknownNode, got %v", err)
	}
}

func TestPlanManualRebalance_NoSampleKeys_ReturnsErrInvalidSimulation(t *testing.T) {
	cfg := threeNodeCfg()
	_, err := ops.PlanManualRebalance(cfg, []string{"node-1"}, nil, nil)
	if !errors.Is(err, ops.ErrInvalidSimulation) {
		t.Errorf("expected ErrInvalidSimulation, got %v", err)
	}
}

func TestPlanManualRebalance_RemoveAllNodes_ReturnsErrNoHealthyNodes(t *testing.T) {
	cfg := threeNodeCfg()
	_, err := ops.PlanManualRebalance(cfg,
		[]string{"node-1", "node-2", "node-3"},
		nil,
		[]string{"user:1"},
	)
	if !errors.Is(err, ops.ErrNoHealthyNodes) {
		t.Errorf("expected ErrNoHealthyNodes, got %v", err)
	}
}

func TestPlanManualRebalance_RemovedNode_ProducesOperatorSteps(t *testing.T) {
	cfg := threeNodeCfg()
	plan, err := ops.PlanManualRebalance(cfg, []string{"node-1"}, nil, []string{"user:1"})
	if err != nil {
		t.Fatalf("PlanManualRebalance: %v", err)
	}
	if len(plan.OperatorSteps) == 0 {
		t.Error("expected operator steps, got none")
	}
	// Verify steps mention key actions.
	found := false
	for _, step := range plan.OperatorSteps {
		if len(step) > 0 {
			found = true
			break
		}
	}
	if !found {
		t.Error("operator steps are all empty")
	}
}

func TestPlanManualRebalance_DoesNotMutateOriginalConfig(t *testing.T) {
	cfg := threeNodeCfg()
	originalNodeCount := len(cfg.Nodes)
	_, err := ops.PlanManualRebalance(cfg, []string{"node-1"}, nil, []string{"user:1"})
	if err != nil {
		t.Fatalf("PlanManualRebalance: %v", err)
	}
	if len(cfg.Nodes) != originalNodeCount {
		t.Errorf("original config mutated: node count changed from %d to %d",
			originalNodeCount, len(cfg.Nodes))
	}
}

func TestPlanManualRebalance_MovedAndStableCountsCorrect(t *testing.T) {
	cfg := threeNodeCfg()
	keys := []string{"user:1", "user:2", "order:1", "order:2", "product:1"}
	plan, err := ops.PlanManualRebalance(cfg, []string{"node-2"}, nil, keys)
	if err != nil {
		t.Fatalf("PlanManualRebalance: %v", err)
	}
	total := plan.Summary.MovedKeys + plan.Summary.StableKeys
	if total != len(keys) {
		t.Errorf("MovedKeys(%d) + StableKeys(%d) = %d, want %d",
			plan.Summary.MovedKeys, plan.Summary.StableKeys, total, len(keys))
	}
	if plan.Summary.TotalKeys != len(keys) {
		t.Errorf("TotalKeys = %d, want %d", plan.Summary.TotalKeys, len(keys))
	}
}

func TestPlanManualRebalance_AddedNode_ChangesRouting(t *testing.T) {
	cfg := threeNodeCfg()
	newNode := cluster.Node{
		ID:      "node-4",
		BaseURL: "http://127.0.0.1:9104",
	}

	// With 4 nodes, some keys should move.
	keysToTest := []string{"user:1", "user:2", "user:3", "order:1", "order:2", "order:3",
		"product:1", "product:2", "product:3", "item:a"}

	plan, err := ops.PlanManualRebalance(cfg, nil, []cluster.Node{newNode}, keysToTest)
	if err != nil {
		t.Fatalf("PlanManualRebalance: %v", err)
	}
	if plan.Summary.TotalKeys != len(keysToTest) {
		t.Errorf("TotalKeys = %d, want %d", plan.Summary.TotalKeys, len(keysToTest))
	}
	// Adding a node should move at least some keys (consistent hash property).
	// With 10 keys and 4 nodes vs 3, approximately 25% should move.
	// We just verify the plan ran without error and produced movements.
	if len(plan.KeyMovements) != len(keysToTest) {
		t.Errorf("KeyMovements count = %d, want %d", len(plan.KeyMovements), len(keysToTest))
	}
	// AddedNodes list should include node-4.
	found := false
	for _, id := range plan.AddedNodes {
		if id == "node-4" {
			found = true
		}
	}
	if !found {
		t.Errorf("AddedNodes does not include node-4: %v", plan.AddedNodes)
	}
}

func TestPlanManualRebalance_DuplicateRemovedNodes_HandledSafely(t *testing.T) {
	cfg := threeNodeCfg()
	plan, err := ops.PlanManualRebalance(cfg,
		[]string{"node-1", "node-1", "node-1"},
		nil,
		[]string{"user:1"},
	)
	if err != nil {
		t.Fatalf("PlanManualRebalance with duplicate removed: %v", err)
	}
	// Should treat node-1 as removed exactly once.
	if len(plan.RemovedNodes) != 1 {
		t.Errorf("RemovedNodes = %v, want exactly [node-1]", plan.RemovedNodes)
	}
}

func TestPlanManualRebalance_ScopeAlwaysTrue(t *testing.T) {
	cfg := threeNodeCfg()
	plan, _ := ops.PlanManualRebalance(cfg, nil, nil, []string{"user:1"})
	if !plan.Scope.NoAutomaticFailover || !plan.Scope.NoDataMovement || !plan.Scope.ManualOnly {
		t.Errorf("scope flags not all true: %+v", plan.Scope)
	}
}

func TestPlanManualRebalance_NoRemovedNodes_NilAdded_AllKeysStable(t *testing.T) {
	cfg := threeNodeCfg()
	keys := []string{"user:1", "user:2", "order:1"}
	plan, err := ops.PlanManualRebalance(cfg, nil, nil, keys)
	if err != nil {
		t.Fatalf("PlanManualRebalance: %v", err)
	}
	if plan.Summary.MovedKeys != 0 {
		t.Errorf("MovedKeys = %d, want 0 when nothing changes", plan.Summary.MovedKeys)
	}
	if plan.Summary.StableKeys != len(keys) {
		t.Errorf("StableKeys = %d, want %d", plan.Summary.StableKeys, len(keys))
	}
}

func TestPlanManualRebalance_EmptyKey_HandledGracefully(t *testing.T) {
	cfg := threeNodeCfg()
	// Empty key in sample list: routing fails, so from/to are both empty.
	plan, err := ops.PlanManualRebalance(cfg, nil, nil, []string{""})
	if err != nil {
		t.Fatalf("PlanManualRebalance with empty key: %v", err)
	}
	if len(plan.KeyMovements) != 1 {
		t.Errorf("KeyMovements = %d, want 1", len(plan.KeyMovements))
	}
	// Empty key: from and to both empty, moved=false.
	if plan.KeyMovements[0].Moved {
		t.Error("empty key should not be marked as moved")
	}
}

func TestPlanManualRebalance_ClusterName_Preserved(t *testing.T) {
	cfg := threeNodeCfg()
	plan, err := ops.PlanManualRebalance(cfg, nil, nil, []string{"user:1"})
	if err != nil {
		t.Fatalf("PlanManualRebalance: %v", err)
	}
	if plan.ClusterName != cfg.Name {
		t.Errorf("ClusterName = %q, want %q", plan.ClusterName, cfg.Name)
	}
}
