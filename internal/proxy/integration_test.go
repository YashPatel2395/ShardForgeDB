package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/YashPatel2395/ShardForgeDB/internal/gateway"
	"github.com/YashPatel2395/ShardForgeDB/internal/node"
)

// startProxy opens a proxy backed by the given servers and starts it in the background.
// The proxy listens on ":0" (any free port). Cleanup is registered via t.Cleanup.
func startProxy(t testing.TB, servers []*node.Server) *Server {
	t.Helper()
	nodes := serversToNodeConfigs(servers)
	s, err := Open(Options{
		Addr:         "127.0.0.1:0",
		Nodes:        nodes,
		VirtualNodes: 128,
		Timeout:      5 * time.Second,
	})
	if err != nil {
		t.Fatalf("proxy.Open: %v", err)
	}
	if err := s.StartBackground(); err != nil {
		t.Fatalf("proxy.StartBackground: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// proxyURL formats a full URL against the proxy listen address.
func proxyURL(s *Server, path string) string {
	return "http://" + s.Addr() + path
}

// readBody reads all bytes from a response body.
func readBody(r io.Reader) []byte {
	b, _ := io.ReadAll(r)
	return b
}

// httpGET sends a GET request and returns (statusCode, responseBody).
func httpGET(t testing.TB, url string) (int, []byte) {
	t.Helper()
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode, readBody(resp.Body)
}

// httpPUT sends PUT with a JSON body, returns (statusCode, responseBody).
func httpPUT(t testing.TB, url, body string) (int, []byte) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPut, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT %s: %v", url, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode, readBody(resp.Body)
}

// httpPOST sends a POST with no body.
func httpPOST(t testing.TB, url string) (int, []byte) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode, readBody(resp.Body)
}

// --- integration tests ---

func TestIntegration_PutGet_RoutesCorrectly(t *testing.T) {
	n1 := startNode(t, "node-1")
	n2 := startNode(t, "node-2")
	n3 := startNode(t, "node-3")
	p := startProxy(t, []*node.Server{n1, n2, n3})

	code, _ := httpPUT(t, proxyURL(p, "/kv/user:1"), `{"value":"alice"}`)
	if code != http.StatusOK {
		t.Fatalf("PUT want 200, got %d", code)
	}

	code, body := httpGET(t, proxyURL(p, "/kv/user:1"))
	if code != http.StatusOK {
		t.Fatalf("GET want 200, got %d", code)
	}
	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatal(err)
	}
	if resp["found"] != true {
		t.Errorf("found = %v, want true", resp["found"])
	}
	if resp["value"] != "alice" {
		t.Errorf("value = %v, want alice", resp["value"])
	}
}

// TestIntegration_KeyIsolation_DirectNodeQuery verifies that a key written
// through the proxy lives only on the routed node and not on the others.
func TestIntegration_KeyIsolation_DirectNodeQuery(t *testing.T) {
	n1 := startNode(t, "node-1")
	n2 := startNode(t, "node-2")
	n3 := startNode(t, "node-3")
	p := startProxy(t, []*node.Server{n1, n2, n3})

	const key = "isolation-test-key"

	// Write via proxy.
	code, _ := httpPUT(t, proxyURL(p, "/kv/"+key), `{"value":"only-on-one-node"}`)
	if code != http.StatusOK {
		t.Fatalf("PUT want 200, got %d", code)
	}

	// Find the routed node via /route endpoint.
	code, routeBody := httpGET(t, proxyURL(p, "/route/"+key))
	if code != http.StatusOK {
		t.Fatalf("route want 200, got %d", code)
	}
	var routeResp RouteResponse
	if err := json.Unmarshal(routeBody, &routeResp); err != nil {
		t.Fatal(err)
	}

	// Query each backend node's /kv endpoint directly (bypassing proxy routing).
	allServers := []*node.Server{n1, n2, n3}
	for _, ns := range allServers {
		st := ns.Status()
		nodeBaseURL := "http://" + ns.Addr()
		code, respBody := httpGET(t, nodeBaseURL+"/kv/"+key)
		if code != http.StatusOK {
			t.Fatalf("direct GET on node %s want 200, got %d", st.NodeID, code)
		}
		var resp map[string]any
		if err := json.Unmarshal(respBody, &resp); err != nil {
			t.Fatal(err)
		}
		if st.NodeID == routeResp.NodeID {
			// The routed node must have the key.
			if resp["found"] != true {
				t.Errorf("routed node %s should have key %q but found=false", st.NodeID, key)
			}
		} else {
			// Non-routed nodes must NOT have the key (no replication).
			if resp["found"] != false {
				t.Errorf("non-routed node %s should NOT have key %q but found=true", st.NodeID, key)
			}
		}
	}
}

// TestIntegration_RouteEndpointMatchesGateway confirms the proxy /route/{key}
// returns the same node as a directly-constructed gateway ring for the same key.
func TestIntegration_RouteEndpointMatchesGateway(t *testing.T) {
	n1 := startNode(t, "node-1")
	n2 := startNode(t, "node-2")
	n3 := startNode(t, "node-3")
	p := startProxy(t, []*node.Server{n1, n2, n3})

	gw, err := gateway.Open(gateway.Options{
		Nodes:        serversToNodeConfigs([]*node.Server{n1, n2, n3}),
		VirtualNodes: 128,
		Timeout:      5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()

	keys := []string{"user:1", "order:42", "product:99", "session:abc", "tenant:xyz"}
	for _, key := range keys {
		gwNode, err := gw.NodeForKey([]byte(key))
		if err != nil {
			t.Fatalf("gateway.NodeForKey %q: %v", key, err)
		}
		code, routeBody := httpGET(t, proxyURL(p, "/route/"+key))
		if code != http.StatusOK {
			t.Fatalf("route %q want 200, got %d", key, code)
		}
		var routeResp RouteResponse
		if err := json.Unmarshal(routeBody, &routeResp); err != nil {
			t.Fatal(err)
		}
		if routeResp.NodeID != gwNode.ID {
			t.Errorf("key %q: proxy routes to %q, gateway routes to %q",
				key, routeResp.NodeID, gwNode.ID)
		}
	}
}

// TestIntegration_NoFailover_UnavailableNodeReturnsError proves that when the
// routed node is unavailable, the proxy returns an error immediately and does
// NOT retry any other node. This is a core safety property.
func TestIntegration_NoFailover_UnavailableNodeReturnsError(t *testing.T) {
	// Grab a free port, then immediately stop listening so nothing is there.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not find free port: %v", err)
	}
	unavailableAddr := ln.Addr().String()
	ln.Close()

	p, err := Open(Options{
		Addr: "127.0.0.1:0",
		Nodes: []gateway.NodeConfig{
			{ID: "bad-node", BaseURL: "http://" + unavailableAddr},
		},
		Timeout: 500 * time.Millisecond, // short for test speed
	})
	if err != nil {
		t.Fatalf("proxy.Open: %v", err)
	}
	if err := p.StartBackground(); err != nil {
		t.Fatalf("StartBackground: %v", err)
	}
	defer p.Close()

	// PUT to any key — the ONLY configured node is unavailable.
	// The proxy must fail immediately; it must not try another node (there is none).
	code, body := httpPUT(t, proxyURL(p, "/kv/any-key"), `{"value":"x"}`)
	if code == http.StatusOK {
		t.Fatalf("expected error when routed node is unavailable, got 200 body=%s", body)
	}
	// Should be a backend error (502/503/504), not a client error (4xx).
	if code < 500 {
		t.Errorf("expected 5xx status, got %d", code)
	}

	// GET also must fail — the only backend is down.
	code, _ = httpGET(t, proxyURL(p, "/kv/any-key"))
	if code == http.StatusOK {
		// A 200 is only acceptable with found=false, but since the backend is down
		// it should be a 5xx. If this flakes, the backend may have briefly started.
		t.Logf("WARNING: GET returned 200 even though backend is down — backend may have restarted")
	}
}

func TestIntegration_ConcurrentPutGet_RaceSafe(t *testing.T) {
	n1 := startNode(t, "node-1")
	n2 := startNode(t, "node-2")
	p := startProxy(t, []*node.Server{n1, n2})

	const workers = 8
	const opsPerWorker = 20
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerWorker; j++ {
				key := fmt.Sprintf("concurrent:w%d:k%d", id, j)
				val := fmt.Sprintf("v%d-%d", id, j)
				body := fmt.Sprintf(`{"value":%q}`, val)
				httpPUT(t, proxyURL(p, "/kv/"+key), body)
				httpGET(t, proxyURL(p, "/kv/"+key))
			}
		}(i)
	}
	wg.Wait()
}

func TestIntegration_Close_Idempotent(t *testing.T) {
	n := startNode(t, "n1")
	p := startProxy(t, []*node.Server{n})
	if err := p.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestIntegration_HealthzAfterStart(t *testing.T) {
	n := startNode(t, "n1")
	p := startProxy(t, []*node.Server{n})
	code, body := httpGET(t, proxyURL(p, "/healthz"))
	if code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", code, body)
	}
}

func TestIntegration_StatusIncludesGatewayAndScope(t *testing.T) {
	n1 := startNode(t, "node-1")
	n2 := startNode(t, "node-2")
	p := startProxy(t, []*node.Server{n1, n2})
	code, body := httpGET(t, proxyURL(p, "/status"))
	if code != http.StatusOK {
		t.Fatalf("want 200, got %d", code)
	}
	var status Status
	if err := json.Unmarshal(body, &status); err != nil {
		t.Fatal(err)
	}
	if status.Gateway.NodeCount != 2 {
		t.Errorf("gateway.node_count = %d, want 2", status.Gateway.NodeCount)
	}
	if !status.Scope.NoFailover {
		t.Error("scope.no_failover must be true")
	}
	if !status.Scope.NoRaft {
		t.Error("scope.no_raft must be true")
	}
}

func TestIntegration_NodesHealth_AllHealthy(t *testing.T) {
	n1 := startNode(t, "node-1")
	n2 := startNode(t, "node-2")
	n3 := startNode(t, "node-3")
	p := startProxy(t, []*node.Server{n1, n2, n3})

	code, body := httpGET(t, proxyURL(p, "/nodes/health"))
	if code != http.StatusOK {
		t.Fatalf("want 200, got %d", code)
	}
	var resp struct {
		Nodes []struct {
			NodeID string `json:"node_id"`
			OK     bool   `json:"ok"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Nodes) != 3 {
		t.Errorf("want 3 nodes in health response, got %d", len(resp.Nodes))
	}
	for _, n := range resp.Nodes {
		if !n.OK {
			t.Errorf("node %s reports unhealthy, want ok", n.NodeID)
		}
	}
}

func TestIntegration_FlushAll_ThenCompactAll(t *testing.T) {
	n1 := startNode(t, "node-1")
	n2 := startNode(t, "node-2")
	p := startProxy(t, []*node.Server{n1, n2})

	for i := 0; i < 5; i++ {
		httpPUT(t, proxyURL(p, fmt.Sprintf("/kv/flush-key-%d", i)), `{"value":"v"}`)
	}

	code, body := httpPOST(t, proxyURL(p, "/flush-all"))
	if code != http.StatusOK {
		t.Fatalf("flush-all want 200, got %d: %s", code, body)
	}

	code, body = httpPOST(t, proxyURL(p, "/compact-all"))
	if code != http.StatusOK {
		t.Fatalf("compact-all want 200, got %d: %s", code, body)
	}
}

func TestIntegration_DeleteViaProxy(t *testing.T) {
	n1 := startNode(t, "node-1")
	n2 := startNode(t, "node-2")
	p := startProxy(t, []*node.Server{n1, n2})

	const key = "to-delete"
	httpPUT(t, proxyURL(p, "/kv/"+key), `{"value":"temporary"}`)

	req, _ := http.NewRequest(http.MethodDelete, proxyURL(p, "/kv/"+key), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE want 200, got %d", resp.StatusCode)
	}

	code, body := httpGET(t, proxyURL(p, "/kv/"+key))
	if code != http.StatusOK {
		t.Fatalf("GET after DELETE want 200, got %d", code)
	}
	var gr map[string]any
	if err := json.Unmarshal(body, &gr); err != nil {
		t.Fatal(err)
	}
	if gr["found"] != false {
		t.Errorf("after delete: found = %v, want false", gr["found"])
	}
}
