package ops

import (
	"fmt"
	"sort"

	"github.com/YashPatel2395/ShardForgeDB/internal/cluster"
	"github.com/YashPatel2395/ShardForgeDB/internal/gateway"
)

// operatorSteps are the manual steps an operator must take to apply a rebalance plan.
// These are hardcoded because this tool never moves data automatically.
var operatorSteps = []string{
	"1. Review node health with: shardforge-cluster health <config>",
	"2. Stop sending new writes to the removed node(s) manually.",
	"3. Start replacement node(s) manually if needed.",
	"4. Update the static cluster config file manually.",
	"5. Restart the proxy/gateway with the updated config.",
	"6. Manually sync read replicas if using read-replica mode.",
	"7. Verify key routing and application-level correctness.",
	"8. NOTE: No data movement is performed by this tool. Data on removed nodes is not migrated automatically.",
}

// PlanManualRebalance returns a pure, read-only plan showing how sample key routing
// changes when removedNodeIDs are taken out and addedNodes are introduced.
//
// Behavior:
//   - Returns ErrUnknownNode if any removed node ID is not in cfg.
//   - Returns ErrInvalidSimulation if no sample keys are provided.
//   - Returns ErrNoHealthyNodes if the resulting node set is empty.
//   - Does not call live nodes, write files, or move data.
//   - Does not mutate cfg.
func PlanManualRebalance(cfg cluster.Config, removedNodeIDs []string, addedNodes []cluster.Node, sampleKeys []string) (RebalancePlan, error) {
	if len(sampleKeys) == 0 {
		return RebalancePlan{}, fmt.Errorf("%w: at least one sample key is required", ErrInvalidSimulation)
	}

	// Build set of existing node IDs for validation.
	existingIDs := make(map[string]bool, len(cfg.Nodes))
	for _, n := range cfg.Nodes {
		existingIDs[n.ID] = true
	}

	// Deduplicate and validate removed node IDs.
	removeSet := make(map[string]bool)
	for _, id := range removedNodeIDs {
		if !existingIDs[id] {
			return RebalancePlan{}, fmt.Errorf("%w: %q", ErrUnknownNode, id)
		}
		removeSet[id] = true
	}

	// Build the planned node set: existing minus removed, plus added.
	plannedNodes := make([]cluster.Node, 0, len(cfg.Nodes))
	for _, n := range cfg.Nodes {
		if !removeSet[n.ID] {
			plannedNodes = append(plannedNodes, n)
		}
	}
	for _, n := range addedNodes {
		plannedNodes = append(plannedNodes, n)
	}

	if len(plannedNodes) == 0 {
		return RebalancePlan{}, ErrNoHealthyNodes
	}

	// Build gateway for current config (for routing sample keys before rebalance).
	currentGW, err := buildGateway(cfg.Nodes, cfg.Routing.VirtualNodes)
	if err != nil {
		return RebalancePlan{}, fmt.Errorf("ops: plan rebalance: current config: %w", err)
	}
	defer currentGW.Close()

	// Build gateway for planned config.
	plannedGW, err := buildGateway(plannedNodes, cfg.Routing.VirtualNodes)
	if err != nil {
		return RebalancePlan{}, fmt.Errorf("ops: plan rebalance: planned config: %w", err)
	}
	defer plannedGW.Close()

	movements := make([]KeyMovement, 0, len(sampleKeys))
	movedCount := 0
	for _, key := range sampleKeys {
		km := KeyMovement{Key: key}

		fromRouted, err := currentGW.NodeForKey([]byte(key))
		if err == nil {
			km.FromNode = fromRouted.ID
		}

		toRouted, err := plannedGW.NodeForKey([]byte(key))
		if err == nil {
			km.ToNode = toRouted.ID
		}

		km.Moved = km.FromNode != km.ToNode
		if km.Moved {
			movedCount++
		}
		movements = append(movements, km)
	}

	// Collect removed / added node IDs for the plan summary.
	removedList := make([]string, 0, len(removeSet))
	for id := range removeSet {
		removedList = append(removedList, id)
	}
	sort.Strings(removedList)

	addedList := make([]string, 0, len(addedNodes))
	for _, n := range addedNodes {
		addedList = append(addedList, n.ID)
	}
	sort.Strings(addedList)

	return RebalancePlan{
		ClusterName:  cfg.Name,
		RemovedNodes: removedList,
		AddedNodes:   addedList,
		KeyMovements: movements,
		Summary: RebalanceSummary{
			TotalKeys:  len(sampleKeys),
			MovedKeys:  movedCount,
			StableKeys: len(sampleKeys) - movedCount,
		},
		OperatorSteps: operatorSteps,
		Scope:         DefaultOpsScope(),
	}, nil
}

// buildGateway constructs a gateway from a raw node list for routing simulation.
// Does not validate cluster config (allows partial/modified node sets).
func buildGateway(nodes []cluster.Node, virtualNodes int) (*gateway.Gateway, error) {
	if len(nodes) == 0 {
		return nil, ErrNoHealthyNodes
	}
	vn := virtualNodes
	if vn <= 0 {
		vn = cluster.DefaultVirtualNodes
	}
	gwnodes := make([]gateway.NodeConfig, len(nodes))
	for i, n := range nodes {
		w := n.Weight
		if w <= 0 {
			w = 1
		}
		gwnodes[i] = gateway.NodeConfig{ID: n.ID, BaseURL: n.BaseURL, Weight: w}
	}
	return gateway.Open(gateway.Options{Nodes: gwnodes, VirtualNodes: vn})
}
