package ops_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/YashPatel2395/ShardForgeDB/internal/cluster"
	"github.com/YashPatel2395/ShardForgeDB/internal/ops"
)

// bench100Keys generates 100 sample keys for benchmarks.
func bench100Keys() []string {
	keys := make([]string, 100)
	for i := range keys {
		keys[i] = generateKey(i)
	}
	return keys
}

func generateKey(i int) string {
	prefixes := []string{"user:", "order:", "product:", "item:", "session:"}
	return prefixes[i%len(prefixes)] + itoa(i)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	b := make([]byte, 0, 10)
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func BenchmarkOps_RouteKey(b *testing.B) {
	cfg := cluster.DefaultConfig("bench", []cluster.Node{
		{ID: "node-1", BaseURL: "http://127.0.0.1:9101"},
		{ID: "node-2", BaseURL: "http://127.0.0.1:9102"},
		{ID: "node-3", BaseURL: "http://127.0.0.1:9103"},
	})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ops.RouteKey(cfg, "user:bench-key")
	}
}

func BenchmarkOps_SimulateFailure_100Keys(b *testing.B) {
	cfg := cluster.DefaultConfig("bench", []cluster.Node{
		{ID: "node-1", BaseURL: "http://127.0.0.1:9101"},
		{ID: "node-2", BaseURL: "http://127.0.0.1:9102"},
		{ID: "node-3", BaseURL: "http://127.0.0.1:9103"},
	})
	keys := bench100Keys()
	req := ops.FailureSimulationRequest{
		DownNodeIDs: []string{"node-2"},
		SampleKeys:  keys,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ops.SimulateFailure(cfg, req)
	}
}

func BenchmarkOps_PlanManualRebalance_100Keys(b *testing.B) {
	cfg := cluster.DefaultConfig("bench", []cluster.Node{
		{ID: "node-1", BaseURL: "http://127.0.0.1:9101"},
		{ID: "node-2", BaseURL: "http://127.0.0.1:9102"},
		{ID: "node-3", BaseURL: "http://127.0.0.1:9103"},
	})
	keys := bench100Keys()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ops.PlanManualRebalance(cfg, []string{"node-2"}, nil, keys)
	}
}

// BenchmarkOps_CheckClusterHealth_HealthyNodes benchmarks health checks
// against local test servers to avoid network variability.
func BenchmarkOps_CheckClusterHealth_HealthyNodes(b *testing.B) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})
	s1 := httptest.NewServer(handler)
	s2 := httptest.NewServer(handler)
	s3 := httptest.NewServer(handler)
	defer s1.Close()
	defer s2.Close()
	defer s3.Close()

	cfg := cluster.Normalize(cluster.Config{
		Version: "v1",
		Name:    "bench",
		Routing: cluster.Routing{Algorithm: "fnv1a-consistent-hash", VirtualNodes: 128},
		Proxy:   cluster.Proxy{Addr: "127.0.0.1:9200"},
		Nodes: []cluster.Node{
			{ID: "node-1", BaseURL: s1.URL},
			{ID: "node-2", BaseURL: s2.URL},
			{ID: "node-3", BaseURL: s3.URL},
		},
		Scope: cluster.DefaultScope(),
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ops.CheckClusterHealth(context.Background(), cfg, time.Second)
	}
}
