package node

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"
)

// BenchmarkHandler_Put measures PUT /kv/{key} latency through the HTTP handler.
func BenchmarkHandler_Put(b *testing.B) {
	s := newBenchServer(b, "bench-put")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("/kv/bench-key-%d", i)
		val := `{"value":"bench-value"}`
		w := doRequest(b, s, "PUT", key, val)
		if w.Code != 200 {
			b.Fatalf("PUT: want 200, got %d", w.Code)
		}
	}
}

// BenchmarkHandler_Get measures GET /kv/{key} latency through the HTTP handler (MemTable hit).
func BenchmarkHandler_Get(b *testing.B) {
	s := newBenchServer(b, "bench-get")
	// Pre-populate one key.
	doRequest(b, s, "PUT", "/kv/bench-get-key", `{"value":"bench-get-val"}`)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		doRequest(b, s, "GET", "/kv/bench-get-key", "")
	}
}

// BenchmarkHandler_Status measures GET /status latency.
func BenchmarkHandler_Status(b *testing.B) {
	s := newBenchServer(b, "bench-status")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		doRequest(b, s, "GET", "/status", "")
	}
}

// BenchmarkHandler_Scan measures GET /scan latency with 100 pre-populated entries.
func BenchmarkHandler_Scan(b *testing.B) {
	s := newBenchServer(b, "bench-scan")
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("/kv/scan-%04d", i)
		doRequest(b, s, "PUT", key, `{"value":"v"}`)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		doRequest(b, s, "GET", "/scan?start=scan-&end=scan-~", "")
	}
}

// BenchmarkClient_Put measures Client.Put latency via httptest.Server.
func BenchmarkClient_Put(b *testing.B) {
	s := newBenchServer(b, "bench-client-put")
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	c := NewClient(ts.URL, 10*time.Second)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := []byte(fmt.Sprintf("ck-%d", i))
		if err := c.Put(ctx, key, []byte("cv")); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkClient_Get measures Client.Get latency via httptest.Server (MemTable hit).
func BenchmarkClient_Get(b *testing.B) {
	s := newBenchServer(b, "bench-client-get")
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	c := NewClient(ts.URL, 10*time.Second)
	ctx := context.Background()
	_ = c.Put(ctx, []byte("bench-get-k"), []byte("bench-get-v"))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err := c.Get(ctx, []byte("bench-get-k"))
		if err != nil {
			b.Fatal(err)
		}
	}
}

// newBenchServer opens a node Server for benchmarks. Uses newTestServer since it accepts testing.TB.
func newBenchServer(b *testing.B, nodeID string) *Server {
	b.Helper()
	return newTestServer(b, nodeID)
}
