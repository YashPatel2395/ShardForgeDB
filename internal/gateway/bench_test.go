package gateway

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func newBenchGateway(b *testing.B, nodeCount int) *Gateway {
	b.Helper()
	cfgs := make([]NodeConfig, nodeCount)
	for i := 0; i < nodeCount; i++ {
		// startNode already calls StartBackground and registers cleanup.
		s := startNode(b, fmt.Sprintf("node-%d", i+1))
		cfgs[i] = NodeConfig{
			ID:      s.Status().NodeID,
			BaseURL: "http://" + s.Addr(),
		}
	}
	g, err := Open(Options{Nodes: cfgs, VirtualNodes: 128, Timeout: 5 * time.Second})
	if err != nil {
		b.Fatalf("Open gateway: %v", err)
	}
	b.Cleanup(func() { _ = g.Close() })
	return g
}

func BenchmarkRing_NodeForKey(b *testing.B) {
	nodes := []NodeConfig{
		{ID: "node-1", BaseURL: "http://127.0.0.1:9101"},
		{ID: "node-2", BaseURL: "http://127.0.0.1:9102"},
		{ID: "node-3", BaseURL: "http://127.0.0.1:9103"},
	}
	g, _ := Open(Options{Nodes: nodes, VirtualNodes: 128, Timeout: 5 * time.Second})
	defer g.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = g.NodeForKey([]byte("bench-key-routing"))
	}
}

func BenchmarkGateway_Put(b *testing.B) {
	g := newBenchGateway(b, 3)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := []byte(fmt.Sprintf("bench-put-%d", i))
		_ = g.Put(ctx, key, []byte("bench-value"))
	}
}

func BenchmarkGateway_Get(b *testing.B) {
	g := newBenchGateway(b, 3)
	ctx := context.Background()
	_ = g.Put(ctx, []byte("bench-get-key"), []byte("bench-get-val"))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = g.Get(ctx, []byte("bench-get-key"))
	}
}

func BenchmarkGateway_HealthAll(b *testing.B) {
	g := newBenchGateway(b, 3)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = g.HealthAll(ctx)
	}
}

func BenchmarkGateway_FlushAll(b *testing.B) {
	g := newBenchGateway(b, 3)
	ctx := context.Background()
	_ = g.Put(ctx, []byte("fk"), []byte("fv"))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = g.FlushAll(ctx)
	}
}

func BenchmarkGateway_CompactAll(b *testing.B) {
	g := newBenchGateway(b, 3)
	ctx := context.Background()
	_ = g.Put(ctx, []byte("ck"), []byte("cv"))
	_ = g.FlushAll(ctx)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = g.CompactAll(ctx)
	}
}
