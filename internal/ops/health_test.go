package ops_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/YashPatel2395/ShardForgeDB/internal/cluster"
	"github.com/YashPatel2395/ShardForgeDB/internal/ops"
)

// healthyServer returns a test server that responds with {"status":"ok"}.
func healthyServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
}

// unhealthyServer returns a test server that responds with 500.
func unhealthyServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
}

// badJSONServer returns 200 but invalid JSON.
func badJSONServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not-json"))
	}))
}

// wrongStatusServer returns 200 with status "degraded".
func wrongStatusServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "degraded"})
	}))
}

// buildCfgFromServers builds a cluster.Config using test server URLs.
// It calls cluster.Normalize so that default weights and scope are applied.
func buildCfgFromServers(name string, nodes []cluster.Node) cluster.Config {
	cfg := cluster.Config{
		Version: "v1",
		Name:    name,
		Routing: cluster.Routing{
			Algorithm:    "fnv1a-consistent-hash",
			VirtualNodes: 128,
		},
		Proxy: cluster.Proxy{Addr: "127.0.0.1:9200"},
		Nodes: nodes,
		Scope: cluster.DefaultScope(),
	}
	return cluster.Normalize(cfg)
}

func TestDefaultOpsScope_AllTrue(t *testing.T) {
	s := ops.DefaultOpsScope()
	if !s.ManualOnly {
		t.Error("ManualOnly should be true")
	}
	if !s.SimulationOnly {
		t.Error("SimulationOnly should be true")
	}
	if !s.NoAutomaticFailover {
		t.Error("NoAutomaticFailover should be true")
	}
	if !s.NoAutomaticRebalancing {
		t.Error("NoAutomaticRebalancing should be true")
	}
	if !s.NoShardMigration {
		t.Error("NoShardMigration should be true")
	}
	if !s.NoDataMovement {
		t.Error("NoDataMovement should be true")
	}
	if !s.NoConsensus {
		t.Error("NoConsensus should be true")
	}
	if !s.NoRaft {
		t.Error("NoRaft should be true")
	}
}

func TestCheckClusterHealth_HealthyNode(t *testing.T) {
	srv := healthyServer(t)
	defer srv.Close()

	cfg := buildCfgFromServers("test", []cluster.Node{
		{ID: "node-1", BaseURL: srv.URL},
	})
	h, err := ops.CheckClusterHealth(context.Background(), cfg, time.Second)
	if err != nil {
		t.Fatalf("CheckClusterHealth: %v", err)
	}
	if len(h.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(h.Nodes))
	}
	if h.Nodes[0].State != ops.HealthHealthy {
		t.Errorf("expected healthy, got %q (error: %s)", h.Nodes[0].State, h.Nodes[0].Error)
	}
	if h.Summary.Healthy != 1 {
		t.Errorf("Summary.Healthy = %d, want 1", h.Summary.Healthy)
	}
}

func TestCheckClusterHealth_UnreachableNode_Unhealthy(t *testing.T) {
	// Use a port that is not listening.
	cfg := buildCfgFromServers("test", []cluster.Node{
		{ID: "node-1", BaseURL: "http://127.0.0.1:19999"},
	})
	h, err := ops.CheckClusterHealth(context.Background(), cfg, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("CheckClusterHealth: %v", err)
	}
	if h.Nodes[0].State != ops.HealthUnhealthy {
		t.Errorf("expected unhealthy, got %q", h.Nodes[0].State)
	}
	if h.Nodes[0].Error == "" {
		t.Error("expected error string for unreachable node")
	}
	if h.Summary.Unhealthy != 1 {
		t.Errorf("Summary.Unhealthy = %d, want 1", h.Summary.Unhealthy)
	}
}

func TestCheckClusterHealth_Non200_Unhealthy(t *testing.T) {
	srv := unhealthyServer(t)
	defer srv.Close()

	cfg := buildCfgFromServers("test", []cluster.Node{
		{ID: "node-1", BaseURL: srv.URL},
	})
	h, err := ops.CheckClusterHealth(context.Background(), cfg, time.Second)
	if err != nil {
		t.Fatalf("CheckClusterHealth: %v", err)
	}
	if h.Nodes[0].State != ops.HealthUnhealthy {
		t.Errorf("expected unhealthy, got %q", h.Nodes[0].State)
	}
	if h.Nodes[0].HTTPStatus != 500 {
		t.Errorf("HTTPStatus = %d, want 500", h.Nodes[0].HTTPStatus)
	}
}

func TestCheckClusterHealth_InvalidJSON_Unhealthy(t *testing.T) {
	srv := badJSONServer(t)
	defer srv.Close()

	cfg := buildCfgFromServers("test", []cluster.Node{
		{ID: "node-1", BaseURL: srv.URL},
	})
	h, err := ops.CheckClusterHealth(context.Background(), cfg, time.Second)
	if err != nil {
		t.Fatalf("CheckClusterHealth: %v", err)
	}
	if h.Nodes[0].State != ops.HealthUnhealthy {
		t.Errorf("expected unhealthy for bad JSON, got %q", h.Nodes[0].State)
	}
}

func TestCheckClusterHealth_WrongStatus_Unhealthy(t *testing.T) {
	srv := wrongStatusServer(t)
	defer srv.Close()

	cfg := buildCfgFromServers("test", []cluster.Node{
		{ID: "node-1", BaseURL: srv.URL},
	})
	h, err := ops.CheckClusterHealth(context.Background(), cfg, time.Second)
	if err != nil {
		t.Fatalf("CheckClusterHealth: %v", err)
	}
	if h.Nodes[0].State != ops.HealthUnhealthy {
		t.Errorf("expected unhealthy for status=degraded, got %q", h.Nodes[0].State)
	}
}

func TestCheckClusterHealth_SortsByNodeID(t *testing.T) {
	s1 := healthyServer(t)
	defer s1.Close()
	s2 := healthyServer(t)
	defer s2.Close()
	s3 := healthyServer(t)
	defer s3.Close()

	cfg := buildCfgFromServers("test", []cluster.Node{
		{ID: "node-c", BaseURL: s3.URL},
		{ID: "node-a", BaseURL: s1.URL},
		{ID: "node-b", BaseURL: s2.URL},
	})
	h, err := ops.CheckClusterHealth(context.Background(), cfg, time.Second)
	if err != nil {
		t.Fatalf("CheckClusterHealth: %v", err)
	}
	if !sort.SliceIsSorted(h.Nodes, func(i, j int) bool {
		return h.Nodes[i].NodeID < h.Nodes[j].NodeID
	}) {
		t.Errorf("nodes are not sorted by ID: %v", h.Nodes)
	}
}

func TestCheckClusterHealth_SummaryCountsCorrect(t *testing.T) {
	healthy := healthyServer(t)
	defer healthy.Close()
	bad := unhealthyServer(t)
	defer bad.Close()

	cfg := buildCfgFromServers("test", []cluster.Node{
		{ID: "node-1", BaseURL: healthy.URL},
		{ID: "node-2", BaseURL: bad.URL},
		{ID: "node-3", BaseURL: "http://127.0.0.1:19998"}, // unreachable
	})
	h, err := ops.CheckClusterHealth(context.Background(), cfg, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("CheckClusterHealth: %v", err)
	}
	if h.Summary.Total != 3 {
		t.Errorf("Total = %d, want 3", h.Summary.Total)
	}
	if h.Summary.Healthy != 1 {
		t.Errorf("Healthy = %d, want 1", h.Summary.Healthy)
	}
	if h.Summary.Unhealthy != 2 {
		t.Errorf("Unhealthy = %d, want 2", h.Summary.Unhealthy)
	}
}

func TestCheckClusterHealth_IncludesLatency(t *testing.T) {
	srv := healthyServer(t)
	defer srv.Close()

	cfg := buildCfgFromServers("test", []cluster.Node{
		{ID: "node-1", BaseURL: srv.URL},
	})
	h, err := ops.CheckClusterHealth(context.Background(), cfg, time.Second)
	if err != nil {
		t.Fatalf("CheckClusterHealth: %v", err)
	}
	// Latency should be >= 0 (even very fast local requests take > 0ms).
	if h.Nodes[0].LatencyMS < 0 {
		t.Errorf("LatencyMS = %d, expected >= 0", h.Nodes[0].LatencyMS)
	}
}

func TestCheckClusterHealth_ScopeAlwaysTrue(t *testing.T) {
	srv := healthyServer(t)
	defer srv.Close()

	cfg := buildCfgFromServers("test", []cluster.Node{
		{ID: "node-1", BaseURL: srv.URL},
	})
	h, _ := ops.CheckClusterHealth(context.Background(), cfg, time.Second)
	if !h.Scope.NoAutomaticFailover || !h.Scope.NoRaft || !h.Scope.NoDataMovement {
		t.Errorf("scope flags not all true: %+v", h.Scope)
	}
}

func TestCheckClusterHealth_InvalidConfig_ReturnsError(t *testing.T) {
	cfg := cluster.Config{} // invalid: no nodes, no version
	_, err := ops.CheckClusterHealth(context.Background(), cfg, time.Second)
	if err == nil {
		t.Fatal("expected error for invalid config, got nil")
	}
}
