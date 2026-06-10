package ops

import (
	"fmt"
	"sort"

	"github.com/YashPatel2395/ShardForgeDB/internal/cluster"
	"github.com/YashPatel2395/ShardForgeDB/internal/gateway"
)

// RouteKey returns the node ID responsible for key according to the cluster config's
// consistent-hash ring. Returns an error if the config is invalid or key is empty.
func RouteKey(cfg cluster.Config, key string) (string, error) {
	if key == "" {
		return "", fmt.Errorf("%w: key must not be empty", ErrInvalidSimulation)
	}
	opts, err := cluster.GatewayOptions(cfg, 0)
	if err != nil {
		return "", fmt.Errorf("ops: route key: %w", err)
	}
	gw, err := gateway.Open(opts)
	if err != nil {
		return "", fmt.Errorf("ops: route key: open gateway: %w", err)
	}
	defer gw.Close()

	routed, err := gw.NodeForKey([]byte(key))
	if err != nil {
		return "", fmt.Errorf("ops: route key: %w", err)
	}
	return routed.ID, nil
}

// RouteKeyWithAvailableNodes routes key using only the specified available node IDs.
// Returns ErrNoHealthyNodes if availableNodeIDs is empty or none of them exist in cfg.
// Returns ErrInvalidSimulation if key is empty.
func RouteKeyWithAvailableNodes(cfg cluster.Config, key string, availableNodeIDs []string) (string, error) {
	if key == "" {
		return "", fmt.Errorf("%w: key must not be empty", ErrInvalidSimulation)
	}
	if len(availableNodeIDs) == 0 {
		return "", ErrNoHealthyNodes
	}

	availSet := make(map[string]bool, len(availableNodeIDs))
	for _, id := range availableNodeIDs {
		availSet[id] = true
	}

	// Build a filtered config with only available nodes.
	filtered := make([]cluster.Node, 0, len(availableNodeIDs))
	for _, n := range cfg.Nodes {
		if availSet[n.ID] {
			filtered = append(filtered, n)
		}
	}
	if len(filtered) == 0 {
		return "", ErrNoHealthyNodes
	}

	filteredCfg := cfg
	filteredCfg.Nodes = filtered
	// The filtered config may fail Validate due to scope, so use cluster.GatewayOptions
	// directly via a manually constructed options struct.
	nodes := make([]gateway.NodeConfig, len(filtered))
	for i, n := range filtered {
		w := n.Weight
		if w <= 0 {
			w = 1
		}
		nodes[i] = gateway.NodeConfig{ID: n.ID, BaseURL: n.BaseURL, Weight: w}
	}
	vn := cfg.Routing.VirtualNodes
	if vn <= 0 {
		vn = cluster.DefaultVirtualNodes
	}
	gw, err := gateway.Open(gateway.Options{Nodes: nodes, VirtualNodes: vn})
	if err != nil {
		return "", fmt.Errorf("ops: route key with available nodes: %w", err)
	}
	defer gw.Close()

	routed, err := gw.NodeForKey([]byte(key))
	if err != nil {
		return "", fmt.Errorf("ops: route key with available nodes: %w", err)
	}
	return routed.ID, nil
}

// SimulateFailure shows how sample key routing changes when the specified nodes are down.
//
// Behavior:
//   - Returns ErrUnknownNode if any down node ID is not in cfg.
//   - Returns ErrInvalidSimulation if no sample keys are provided.
//   - Each key is routed on the full config and on the available-nodes subset.
//   - A key is affected if its original node is down or its route changes.
//   - If all nodes are down, all keys are marked unavailable.
//   - No live node calls. No data movement. No automatic rerouting.
func SimulateFailure(cfg cluster.Config, req FailureSimulationRequest) (FailureSimulationResult, error) {
	if len(req.SampleKeys) == 0 {
		return FailureSimulationResult{}, fmt.Errorf("%w: at least one sample key is required", ErrInvalidSimulation)
	}

	// Build a set of all node IDs for validation.
	nodeIDSet := make(map[string]bool, len(cfg.Nodes))
	for _, n := range cfg.Nodes {
		nodeIDSet[n.ID] = true
	}

	// Deduplicate and validate down node IDs.
	downSet := make(map[string]bool)
	for _, id := range req.DownNodeIDs {
		if !nodeIDSet[id] {
			return FailureSimulationResult{}, fmt.Errorf("%w: %q", ErrUnknownNode, id)
		}
		downSet[id] = true
	}

	// Collect healthy node IDs (sorted for determinism).
	var healthyIDs []string
	for _, n := range cfg.Nodes {
		if !downSet[n.ID] {
			healthyIDs = append(healthyIDs, n.ID)
		}
	}
	sort.Strings(healthyIDs)

	downIDs := make([]string, 0, len(downSet))
	for id := range downSet {
		downIDs = append(downIDs, id)
	}
	sort.Strings(downIDs)

	var affected, unaffected []KeyImpact
	allDown := len(healthyIDs) == 0

	for _, key := range req.SampleKeys {
		impact := KeyImpact{Key: key}

		origNode, err := RouteKey(cfg, key)
		if err != nil {
			impact.OriginalNode = ""
			impact.Unavailable = true
			affected = append(affected, impact)
			continue
		}
		impact.OriginalNode = origNode

		if allDown {
			impact.Unavailable = true
			impact.Moved = false
			affected = append(affected, impact)
			continue
		}

		// Route on available nodes only.
		newNode, err := RouteKeyWithAvailableNodes(cfg, key, healthyIDs)
		if err != nil {
			impact.Unavailable = true
			affected = append(affected, impact)
			continue
		}
		impact.NewNode = newNode

		isAffected := downSet[origNode] || newNode != origNode
		impact.Moved = !downSet[origNode] && newNode != origNode

		if isAffected {
			affected = append(affected, impact)
		} else {
			unaffected = append(unaffected, impact)
		}
	}

	// Count unavailable separately.
	unavail := 0
	moved := 0
	for _, k := range affected {
		if k.Unavailable {
			unavail++
		} else if k.Moved {
			moved++
		}
	}

	if affected == nil {
		affected = []KeyImpact{}
	}
	if unaffected == nil {
		unaffected = []KeyImpact{}
	}

	return FailureSimulationResult{
		ClusterName:    cfg.Name,
		DownNodeIDs:    downIDs,
		HealthyNodeIDs: healthyIDs,
		AffectedKeys:   affected,
		UnaffectedKeys: unaffected,
		Summary: FailureImpactSummary{
			TotalKeys:       len(req.SampleKeys),
			AffectedKeys:    len(affected),
			UnaffectedKeys:  len(unaffected),
			MovedKeys:       moved,
			UnavailableKeys: unavail,
		},
		Scope: DefaultOpsScope(),
	}, nil
}
