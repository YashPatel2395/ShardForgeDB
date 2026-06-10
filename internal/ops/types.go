package ops

import "time"

// HealthState describes the observed health of a node.
type HealthState string

const (
	// HealthUnknown means the health check could not be completed.
	HealthUnknown HealthState = "unknown"
	// HealthHealthy means the node responded 2xx with status "ok".
	HealthHealthy HealthState = "healthy"
	// HealthUnhealthy means the node was unreachable, timed out, returned non-2xx,
	// or returned invalid JSON / unexpected status.
	HealthUnhealthy HealthState = "unhealthy"
)

// NodeHealth is the result of a single /healthz check against one node.
type NodeHealth struct {
	NodeID     string      `json:"node_id"`
	BaseURL    string      `json:"base_url"`
	State      HealthState `json:"state"`
	HTTPStatus int         `json:"http_status,omitempty"`
	LatencyMS  int64       `json:"latency_ms,omitempty"`
	Error      string      `json:"error,omitempty"`
	CheckedAt  time.Time   `json:"checked_at"`
}

// ClusterHealth is the aggregated result of health-checking all nodes.
type ClusterHealth struct {
	Name      string        `json:"name"`
	CheckedAt time.Time     `json:"checked_at"`
	Nodes     []NodeHealth  `json:"nodes"`
	Summary   HealthSummary `json:"summary"`
	Scope     OpsScope      `json:"scope"`
}

// HealthSummary counts node states across the cluster.
type HealthSummary struct {
	Total     int `json:"total"`
	Healthy   int `json:"healthy"`
	Unhealthy int `json:"unhealthy"`
	Unknown   int `json:"unknown"`
}

// OpsScope documents the limits of the ops layer.
// All fields are true to assert no automatic or distributed behavior.
type OpsScope struct {
	ManualOnly             bool `json:"manual_only"`
	SimulationOnly         bool `json:"simulation_only"`
	NoAutomaticFailover    bool `json:"no_automatic_failover"`
	NoAutomaticRebalancing bool `json:"no_automatic_rebalancing"`
	NoShardMigration       bool `json:"no_shard_migration"`
	NoDataMovement         bool `json:"no_data_movement"`
	NoConsensus            bool `json:"no_consensus"`
	NoRaft                 bool `json:"no_raft"`
}

// FailureSimulationRequest describes which nodes to mark as down
// and which sample keys to test.
type FailureSimulationRequest struct {
	DownNodeIDs []string `json:"down_node_ids"`
	SampleKeys  []string `json:"sample_keys"`
}

// FailureSimulationResult shows the routing impact of the specified node failures.
type FailureSimulationResult struct {
	ClusterName    string               `json:"cluster_name"`
	DownNodeIDs    []string             `json:"down_node_ids"`
	HealthyNodeIDs []string             `json:"healthy_node_ids"`
	AffectedKeys   []KeyImpact          `json:"affected_keys"`
	UnaffectedKeys []KeyImpact          `json:"unaffected_keys"`
	Summary        FailureImpactSummary `json:"summary"`
	Scope          OpsScope             `json:"scope"`
}

// KeyImpact describes the routing outcome for a single key under a failure scenario.
type KeyImpact struct {
	Key          string `json:"key"`
	OriginalNode string `json:"original_node"`
	NewNode      string `json:"new_node,omitempty"`
	Moved        bool   `json:"moved"`
	Unavailable  bool   `json:"unavailable"`
}

// FailureImpactSummary counts how many keys were affected.
type FailureImpactSummary struct {
	TotalKeys       int `json:"total_keys"`
	AffectedKeys    int `json:"affected_keys"`
	UnaffectedKeys  int `json:"unaffected_keys"`
	MovedKeys       int `json:"moved_keys"`
	UnavailableKeys int `json:"unavailable_keys"`
}

// RebalancePlan is a pure, read-only plan describing sample key movement
// when nodes are removed and/or added.
// No data movement is performed. Operator action is required.
type RebalancePlan struct {
	ClusterName   string           `json:"cluster_name"`
	RemovedNodes  []string         `json:"removed_nodes"`
	AddedNodes    []string         `json:"added_nodes"`
	KeyMovements  []KeyMovement    `json:"key_movements"`
	Summary       RebalanceSummary `json:"summary"`
	OperatorSteps []string         `json:"operator_steps"`
	Scope         OpsScope         `json:"scope"`
}

// KeyMovement describes the routing change for one key under the rebalance plan.
type KeyMovement struct {
	Key      string `json:"key"`
	FromNode string `json:"from_node"`
	ToNode   string `json:"to_node"`
	Moved    bool   `json:"moved"`
}

// RebalanceSummary counts moved vs stable keys.
type RebalanceSummary struct {
	TotalKeys  int `json:"total_keys"`
	MovedKeys  int `json:"moved_keys"`
	StableKeys int `json:"stable_keys"`
}
