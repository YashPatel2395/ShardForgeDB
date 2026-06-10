package proxy

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/YashPatel2395/ShardForgeDB/internal/node"
)

// benchClient returns an http.Client configured for connection reuse.
// Bodies must be fully drained before Close() to enable keep-alive reuse.
func benchClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 100,
			IdleConnTimeout:     90 * time.Second,
			DisableKeepAlives:   false,
		},
	}
}

// drainAndClose reads the response body to EOF and closes it so the
// underlying TCP connection can be reused (HTTP/1.1 keep-alive requirement).
func drainAndClose(resp *http.Response) {
	if resp != nil && resp.Body != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

// newBenchProxy opens a proxy backed by nodeCount fresh nodes.
func newBenchProxy(b *testing.B, nodeCount int) *Server {
	b.Helper()
	servers := make([]*node.Server, nodeCount)
	for i := 0; i < nodeCount; i++ {
		s, err := node.Open(node.Options{
			NodeID:  fmt.Sprintf("bench-node-%d", i+1),
			Addr:    "127.0.0.1:0",
			DataDir: b.TempDir(),
		})
		if err != nil {
			b.Fatalf("node.Open: %v", err)
		}
		if err := s.StartBackground(); err != nil {
			b.Fatalf("node.StartBackground: %v", err)
		}
		b.Cleanup(func() { _ = s.Close() })
		servers[i] = s
	}
	p, err := Open(Options{
		Addr:         "127.0.0.1:0",
		Nodes:        serversToNodeConfigs(servers),
		VirtualNodes: 128,
		Timeout:      5 * time.Second,
	})
	if err != nil {
		b.Fatalf("proxy.Open: %v", err)
	}
	if err := p.StartBackground(); err != nil {
		b.Fatalf("proxy.StartBackground: %v", err)
	}
	b.Cleanup(func() { _ = p.Close() })
	return p
}

// BenchmarkProxy_Route measures GET /route/{key} — pure ring computation, no backend call.
func BenchmarkProxy_Route(b *testing.B) {
	p := newBenchProxy(b, 3)
	client := benchClient()
	url := proxyURL(p, "/route/bench-routing-key")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := client.Get(url)
		if err != nil {
			b.Fatal(err)
		}
		drainAndClose(resp)
	}
}

// BenchmarkProxy_Put measures PUT /kv/{key} routed through the proxy to a backend node.
func BenchmarkProxy_Put(b *testing.B) {
	p := newBenchProxy(b, 3)
	client := benchClient()
	body := `{"value":"bench-value"}`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("bench-put-key-%d", i)
		req, _ := http.NewRequest(http.MethodPut, proxyURL(p, "/kv/"+key), strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			b.Fatal(err)
		}
		drainAndClose(resp)
	}
}

// BenchmarkProxy_Get measures GET /kv/{key} routed through the proxy.
func BenchmarkProxy_Get(b *testing.B) {
	p := newBenchProxy(b, 3)
	client := benchClient()

	// Pre-populate a key.
	req, _ := http.NewRequest(http.MethodPut, proxyURL(p, "/kv/bench-get-key"),
		strings.NewReader(`{"value":"bench-val"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		b.Fatalf("pre-populate: %v", err)
	}
	drainAndClose(resp)

	url := proxyURL(p, "/kv/bench-get-key")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r, err := client.Get(url)
		if err != nil {
			b.Fatal(err)
		}
		drainAndClose(r)
	}
}

// BenchmarkProxy_Status measures GET /status.
func BenchmarkProxy_Status(b *testing.B) {
	p := newBenchProxy(b, 3)
	client := benchClient()
	url := proxyURL(p, "/status")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := client.Get(url)
		if err != nil {
			b.Fatal(err)
		}
		drainAndClose(resp)
	}
}

// BenchmarkProxy_NodesHealth measures GET /nodes/health (fans out to all nodes).
func BenchmarkProxy_NodesHealth(b *testing.B) {
	p := newBenchProxy(b, 3)
	client := benchClient()
	url := proxyURL(p, "/nodes/health")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := client.Get(url)
		if err != nil {
			b.Fatal(err)
		}
		drainAndClose(resp)
	}
}

// BenchmarkProxy_FlushAll measures POST /flush-all (fans out to all nodes).
func BenchmarkProxy_FlushAll(b *testing.B) {
	p := newBenchProxy(b, 3)
	client := benchClient()
	url := proxyURL(p, "/flush-all")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req, _ := http.NewRequest(http.MethodPost, url, nil)
		resp, err := client.Do(req)
		if err != nil {
			b.Fatal(err)
		}
		drainAndClose(resp)
	}
}

// BenchmarkProxy_CompactAll measures POST /compact-all (fans out to all nodes).
func BenchmarkProxy_CompactAll(b *testing.B) {
	p := newBenchProxy(b, 3)
	client := benchClient()
	url := proxyURL(p, "/compact-all")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req, _ := http.NewRequest(http.MethodPost, url, nil)
		resp, err := client.Do(req)
		if err != nil {
			b.Fatal(err)
		}
		drainAndClose(resp)
	}
}
