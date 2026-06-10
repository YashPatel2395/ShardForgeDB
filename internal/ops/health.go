package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"

	"github.com/YashPatel2395/ShardForgeDB/internal/cluster"
)

const defaultHealthTimeout = 5 * time.Second

// DefaultOpsScope returns an OpsScope with all limitation flags set to true.
// This asserts that the ops layer is simulation/planning only.
func DefaultOpsScope() OpsScope {
	return OpsScope{
		ManualOnly:             true,
		SimulationOnly:         true,
		NoAutomaticFailover:    true,
		NoAutomaticRebalancing: true,
		NoShardMigration:       true,
		NoDataMovement:         true,
		NoConsensus:            true,
		NoRaft:                 true,
	}
}

// CheckClusterHealth calls GET /healthz on every configured node and returns
// a ClusterHealth snapshot. Results are sorted by node ID.
//
// Behavior:
//   - A node is healthy if it responds 2xx with JSON {"status":"ok"}.
//   - Connection errors, timeouts, non-2xx, and bad JSON are all unhealthy.
//   - Never panics. Never retries. Never mutates the cluster config.
//   - This is diagnostic only. No failover, no rerouting.
//
// timeout <= 0 uses a 5-second default.
func CheckClusterHealth(ctx context.Context, cfg cluster.Config, timeout time.Duration) (ClusterHealth, error) {
	if err := cluster.Validate(cfg); err != nil {
		return ClusterHealth{}, fmt.Errorf("ops: health check: invalid config: %w", err)
	}
	if timeout <= 0 {
		timeout = defaultHealthTimeout
	}

	now := time.Now().UTC()
	httpClient := &http.Client{Timeout: timeout}

	results := make([]NodeHealth, len(cfg.Nodes))
	for i, n := range cfg.Nodes {
		results[i] = checkNode(ctx, httpClient, n.ID, n.BaseURL)
	}

	// Sort by node ID for deterministic output.
	sort.Slice(results, func(i, j int) bool {
		return results[i].NodeID < results[j].NodeID
	})

	summary := HealthSummary{Total: len(results)}
	for _, r := range results {
		switch r.State {
		case HealthHealthy:
			summary.Healthy++
		case HealthUnhealthy:
			summary.Unhealthy++
		default:
			summary.Unknown++
		}
	}

	return ClusterHealth{
		Name:      cfg.Name,
		CheckedAt: now,
		Nodes:     results,
		Summary:   summary,
		Scope:     DefaultOpsScope(),
	}, nil
}

// checkNode performs a single /healthz check and returns the result.
// Never panics. Never retries.
func checkNode(ctx context.Context, client *http.Client, nodeID, baseURL string) NodeHealth {
	h := NodeHealth{
		NodeID:    nodeID,
		BaseURL:   baseURL,
		CheckedAt: time.Now().UTC(),
	}

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/healthz", nil)
	if err != nil {
		h.State = HealthUnhealthy
		h.Error = fmt.Sprintf("build request: %v", err)
		return h
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	h.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		h.State = HealthUnhealthy
		h.Error = fmt.Sprintf("connection error: %v", err)
		return h
	}
	defer resp.Body.Close()

	h.HTTPStatus = resp.StatusCode
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		h.State = HealthUnhealthy
		h.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
		return h
	}

	var payload struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		h.State = HealthUnhealthy
		h.Error = fmt.Sprintf("invalid JSON response: %v", err)
		return h
	}
	if payload.Status != "ok" {
		h.State = HealthUnhealthy
		h.Error = fmt.Sprintf("unexpected status %q", payload.Status)
		return h
	}

	h.State = HealthHealthy
	return h
}
