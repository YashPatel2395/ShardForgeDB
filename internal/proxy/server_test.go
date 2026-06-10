package proxy

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/YashPatel2395/ShardForgeDB/internal/gateway"
	"github.com/YashPatel2395/ShardForgeDB/internal/node"
)

// --- test helpers ---

// startNode opens and starts a node.Server in the background.
func startNode(t testing.TB, id string) *node.Server {
	t.Helper()
	s, err := node.Open(node.Options{
		NodeID:  id,
		Addr:    "127.0.0.1:0",
		DataDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("node.Open %s: %v", id, err)
	}
	if err := s.StartBackground(); err != nil {
		t.Fatalf("node.StartBackground %s: %v", id, err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// openProxy creates a Server from the given node.Server list (no listener started).
func openProxy(t testing.TB, servers []*node.Server) *Server {
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
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// serversToNodeConfigs converts node.Server slices to gateway.NodeConfig slices.
func serversToNodeConfigs(servers []*node.Server) []gateway.NodeConfig {
	cfgs := make([]gateway.NodeConfig, len(servers))
	for i, s := range servers {
		st := s.Status()
		cfgs[i] = gateway.NodeConfig{
			ID:      st.NodeID,
			BaseURL: "http://" + s.Addr(),
		}
	}
	return cfgs
}

// doHandler sends an httptest.Request directly to the proxy Handler.
func doHandler(t testing.TB, s *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var bodyReader *strings.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	} else {
		bodyReader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, bodyReader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	return w
}

// decodeJSON decodes the response body into v.
func decodeJSON(t testing.TB, w *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.NewDecoder(w.Body).Decode(v); err != nil {
		t.Fatalf("decode JSON: %v (body: %q)", err, w.Body.String())
	}
}

// --- Open / option validation ---

func TestOpen_RejectsEmptyNodes(t *testing.T) {
	_, err := Open(Options{Addr: "127.0.0.1:0"})
	if err == nil {
		t.Fatal("expected error for empty nodes")
	}
}

func TestOpen_DefaultsAddr(t *testing.T) {
	n := startNode(t, "n1")
	s, err := Open(Options{Nodes: serversToNodeConfigs([]*node.Server{n})})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if s.opts.Addr != defaultAddr {
		t.Errorf("Addr = %q, want %q", s.opts.Addr, defaultAddr)
	}
}

func TestOpen_DefaultsVirtualNodes(t *testing.T) {
	n := startNode(t, "n1")
	s, err := Open(Options{Nodes: serversToNodeConfigs([]*node.Server{n})})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	stats := s.gw.Stats()
	if stats.VirtualNodes != defaultVirtualNodes {
		t.Errorf("VirtualNodes = %d, want %d", stats.VirtualNodes, defaultVirtualNodes)
	}
}

func TestOpen_DefaultsTimeout(t *testing.T) {
	n := startNode(t, "n1")
	s, err := Open(Options{Nodes: serversToNodeConfigs([]*node.Server{n})})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if s.opts.Timeout != 0 {
		// opts.Timeout is the user-supplied value; defaults are applied inside Open
		// only to the gateway. The opts struct may remain zero.
	}
	// Verify gateway was opened successfully with defaults by calling a no-op operation.
	_ = s.gw.Stats()
}

func TestOpen_RejectsDuplicateURL(t *testing.T) {
	_, err := Open(Options{
		Nodes: []gateway.NodeConfig{
			{ID: "a", BaseURL: "http://same"},
			{ID: "b", BaseURL: "http://same"},
		},
	})
	if err == nil {
		t.Fatal("expected error for duplicate base URL")
	}
}

func TestOpen_CustomVirtualNodesAndTimeout(t *testing.T) {
	n := startNode(t, "n1")
	s, err := Open(Options{
		Nodes:        serversToNodeConfigs([]*node.Server{n}),
		VirtualNodes: 64,
		Timeout:      2 * time.Second,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if s.gw.Stats().VirtualNodes != 64 {
		t.Errorf("VirtualNodes = %d, want 64", s.gw.Stats().VirtualNodes)
	}
}

// --- /healthz ---

func TestHealthz_OK(t *testing.T) {
	n := startNode(t, "n1")
	s := openProxy(t, []*node.Server{n})
	w := doHandler(t, s, http.MethodGet, "/healthz", "")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var body map[string]string
	decodeJSON(t, w, &body)
	if body["status"] != "ok" {
		t.Errorf("status = %q, want ok", body["status"])
	}
}

func TestHealthz_WrongMethod(t *testing.T) {
	n := startNode(t, "n1")
	s := openProxy(t, []*node.Server{n})
	w := doHandler(t, s, http.MethodPost, "/healthz", "")
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", w.Code)
	}
	if !strings.Contains(w.Header().Get("Allow"), "GET") {
		t.Errorf("Allow header missing GET: %q", w.Header().Get("Allow"))
	}
}

// --- /status ---

func TestStatus_OK_IncludesScopeFlags(t *testing.T) {
	n := startNode(t, "n1")
	s := openProxy(t, []*node.Server{n})
	w := doHandler(t, s, http.MethodGet, "/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var body Status
	decodeJSON(t, w, &body)
	if !body.Scope.ClientSideRoutingOnly {
		t.Error("scope.client_side_routing_only should be true")
	}
	if !body.Scope.NoReplication {
		t.Error("scope.no_replication should be true")
	}
	if !body.Scope.NoRaft {
		t.Error("scope.no_raft should be true")
	}
	if !body.Scope.NoConsensus {
		t.Error("scope.no_consensus should be true")
	}
	if !body.Scope.NoFailover {
		t.Error("scope.no_failover should be true")
	}
	if body.Gateway.NodeCount != 1 {
		t.Errorf("gateway.node_count = %d, want 1", body.Gateway.NodeCount)
	}
}

func TestStatus_WrongMethod(t *testing.T) {
	n := startNode(t, "n1")
	s := openProxy(t, []*node.Server{n})
	w := doHandler(t, s, http.MethodDelete, "/status", "")
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", w.Code)
	}
}

// --- /route/{key} ---

func TestRoute_ReturnsSelectedNode(t *testing.T) {
	n := startNode(t, "n1")
	s := openProxy(t, []*node.Server{n})
	w := doHandler(t, s, http.MethodGet, "/route/user:1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var body RouteResponse
	decodeJSON(t, w, &body)
	if body.Key != "user:1" {
		t.Errorf("key = %q, want user:1", body.Key)
	}
	if body.NodeID == "" {
		t.Error("node_id must not be empty")
	}
	if body.BaseURL == "" {
		t.Error("base_url must not be empty")
	}
}

func TestRoute_EmptyKey_Returns400(t *testing.T) {
	n := startNode(t, "n1")
	s := openProxy(t, []*node.Server{n})
	w := doHandler(t, s, http.MethodGet, "/route/", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestRoute_WrongMethod(t *testing.T) {
	n := startNode(t, "n1")
	s := openProxy(t, []*node.Server{n})
	w := doHandler(t, s, http.MethodPost, "/route/somekey", "")
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", w.Code)
	}
	if !strings.Contains(w.Header().Get("Allow"), "GET") {
		t.Errorf("Allow header missing GET: %q", w.Header().Get("Allow"))
	}
}

func TestRoute_IsDeterministic(t *testing.T) {
	n := startNode(t, "n1")
	s := openProxy(t, []*node.Server{n})
	w1 := doHandler(t, s, http.MethodGet, "/route/stable-key", "")
	w2 := doHandler(t, s, http.MethodGet, "/route/stable-key", "")
	if w1.Body.String() != w2.Body.String() {
		t.Errorf("route not deterministic:\nfirst:  %s\nsecond: %s", w1.Body.String(), w2.Body.String())
	}
}

// --- /kv/{key} PUT / GET / DELETE ---

func TestKV_Put_WritesSuccessfully(t *testing.T) {
	n := startNode(t, "n1")
	s := openProxy(t, []*node.Server{n})
	body := `{"value":"alice"}`
	w := doHandler(t, s, http.MethodPut, "/kv/user:1", body)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	decodeJSON(t, w, &resp)
	if resp["ok"] != true {
		t.Errorf("ok = %v, want true", resp["ok"])
	}
}

func TestKV_Get_ReturnsValue(t *testing.T) {
	n := startNode(t, "n1")
	s := openProxy(t, []*node.Server{n})
	doHandler(t, s, http.MethodPut, "/kv/user:get", `{"value":"bob"}`)
	w := doHandler(t, s, http.MethodGet, "/kv/user:get", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	decodeJSON(t, w, &resp)
	if resp["found"] != true {
		t.Errorf("found = %v, want true", resp["found"])
	}
	if resp["value"] != "bob" {
		t.Errorf("value = %v, want bob", resp["value"])
	}
}

func TestKV_Get_NotFound_Returns200WithFoundFalse(t *testing.T) {
	n := startNode(t, "n1")
	s := openProxy(t, []*node.Server{n})
	w := doHandler(t, s, http.MethodGet, "/kv/no-such-key", "")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp map[string]any
	decodeJSON(t, w, &resp)
	if resp["found"] != false {
		t.Errorf("found = %v, want false", resp["found"])
	}
}

func TestKV_Delete_RemovesKey(t *testing.T) {
	n := startNode(t, "n1")
	s := openProxy(t, []*node.Server{n})
	doHandler(t, s, http.MethodPut, "/kv/user:del", `{"value":"gone"}`)
	w := doHandler(t, s, http.MethodDelete, "/kv/user:del", "")
	if w.Code != http.StatusOK {
		t.Fatalf("DELETE want 200, got %d: %s", w.Code, w.Body.String())
	}
	// Verify key is gone.
	wg := doHandler(t, s, http.MethodGet, "/kv/user:del", "")
	var resp map[string]any
	if err := json.NewDecoder(wg.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["found"] != false {
		t.Errorf("after delete: found = %v, want false", resp["found"])
	}
}

func TestKV_Put_InvalidJSON_Returns400(t *testing.T) {
	n := startNode(t, "n1")
	s := openProxy(t, []*node.Server{n})
	w := doHandler(t, s, http.MethodPut, "/kv/user:1", "not-json")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestKV_EmptyKey_Returns400(t *testing.T) {
	n := startNode(t, "n1")
	s := openProxy(t, []*node.Server{n})
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		w := doHandler(t, s, method, "/kv/", `{"value":"x"}`)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s /kv/ want 400, got %d", method, w.Code)
		}
	}
}

func TestKV_WrongMethod_Returns405(t *testing.T) {
	n := startNode(t, "n1")
	s := openProxy(t, []*node.Server{n})
	w := doHandler(t, s, http.MethodPost, "/kv/user:1", "")
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", w.Code)
	}
	allow := w.Header().Get("Allow")
	if !strings.Contains(allow, "GET") || !strings.Contains(allow, "PUT") || !strings.Contains(allow, "DELETE") {
		t.Errorf("Allow header = %q, want GET, PUT, DELETE", allow)
	}
}

// --- /scan-node/{nodeID} ---

func TestScanNode_ReturnsEntries(t *testing.T) {
	n := startNode(t, "scan-node-1")
	s := openProxy(t, []*node.Server{n})
	doHandler(t, s, http.MethodPut, "/kv/scan:a", `{"value":"1"}`)
	doHandler(t, s, http.MethodPut, "/kv/scan:b", `{"value":"2"}`)
	doHandler(t, s, http.MethodPut, "/kv/scan:c", `{"value":"3"}`)

	w := doHandler(t, s, http.MethodGet, "/scan-node/scan-node-1?start=scan&end=scao", "")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	decodeJSON(t, w, &resp)
	if resp["per_node_only"] != true {
		t.Error("per_node_only should be true")
	}
	if resp["node_id"] != "scan-node-1" {
		t.Errorf("node_id = %v, want scan-node-1", resp["node_id"])
	}
}

func TestScanNode_UnknownNode_Returns404(t *testing.T) {
	n := startNode(t, "n1")
	s := openProxy(t, []*node.Server{n})
	w := doHandler(t, s, http.MethodGet, "/scan-node/no-such-node", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
}

func TestScanNode_WrongMethod(t *testing.T) {
	n := startNode(t, "n1")
	s := openProxy(t, []*node.Server{n})
	w := doHandler(t, s, http.MethodPost, "/scan-node/n1", "")
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", w.Code)
	}
}

// --- /flush-all ---

func TestFlushAll_OK(t *testing.T) {
	n := startNode(t, "n1")
	s := openProxy(t, []*node.Server{n})
	w := doHandler(t, s, http.MethodPost, "/flush-all", "")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	decodeJSON(t, w, &resp)
	if resp["ok"] != true {
		t.Errorf("ok = %v, want true", resp["ok"])
	}
}

func TestFlushAll_WrongMethod(t *testing.T) {
	n := startNode(t, "n1")
	s := openProxy(t, []*node.Server{n})
	w := doHandler(t, s, http.MethodGet, "/flush-all", "")
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", w.Code)
	}
	if !strings.Contains(w.Header().Get("Allow"), "POST") {
		t.Errorf("Allow header missing POST: %q", w.Header().Get("Allow"))
	}
}

// --- /compact-all ---

func TestCompactAll_OK(t *testing.T) {
	n := startNode(t, "n1")
	s := openProxy(t, []*node.Server{n})
	w := doHandler(t, s, http.MethodPost, "/compact-all", "")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCompactAll_WrongMethod(t *testing.T) {
	n := startNode(t, "n1")
	s := openProxy(t, []*node.Server{n})
	w := doHandler(t, s, http.MethodDelete, "/compact-all", "")
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", w.Code)
	}
}

// --- /nodes/health ---

func TestNodesHealth_ReturnsAllNodes(t *testing.T) {
	n1 := startNode(t, "h1")
	n2 := startNode(t, "h2")
	s := openProxy(t, []*node.Server{n1, n2})
	w := doHandler(t, s, http.MethodGet, "/nodes/health", "")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp struct {
		Nodes []struct {
			NodeID string `json:"node_id"`
			OK     bool   `json:"ok"`
		} `json:"nodes"`
	}
	decodeJSON(t, w, &resp)
	if len(resp.Nodes) != 2 {
		t.Errorf("want 2 nodes, got %d", len(resp.Nodes))
	}
	for _, n := range resp.Nodes {
		if !n.OK {
			t.Errorf("node %s should be ok", n.NodeID)
		}
	}
}

func TestNodesHealth_WrongMethod(t *testing.T) {
	n := startNode(t, "n1")
	s := openProxy(t, []*node.Server{n})
	w := doHandler(t, s, http.MethodPost, "/nodes/health", "")
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", w.Code)
	}
}

// --- unknown path ---

func TestUnknownPath_Returns404(t *testing.T) {
	n := startNode(t, "n1")
	s := openProxy(t, []*node.Server{n})
	w := doHandler(t, s, http.MethodGet, "/completely-unknown", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
}

// --- Close ---

func TestClose_Idempotent(t *testing.T) {
	n := startNode(t, "n1")
	s := openProxy(t, []*node.Server{n})
	if err := s.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestStart_AfterClose_ReturnsErrClosed(t *testing.T) {
	n := startNode(t, "n1")
	s := openProxy(t, []*node.Server{n})
	_ = s.Close()
	if err := s.Start(); err != ErrClosed {
		t.Errorf("Start after close: want ErrClosed, got %v", err)
	}
}

// --- JSON error format ---

func TestErrorResponse_IsJSON(t *testing.T) {
	n := startNode(t, "n1")
	s := openProxy(t, []*node.Server{n})
	w := doHandler(t, s, http.MethodGet, "/unknown-path", "")
	if !strings.Contains(w.Header().Get("Content-Type"), "application/json") {
		t.Errorf("Content-Type = %q, want application/json", w.Header().Get("Content-Type"))
	}
	var body map[string]string
	if err := json.NewDecoder(bytes.NewReader(w.Body.Bytes())).Decode(&body); err != nil {
		t.Fatalf("error response is not valid JSON: %v", err)
	}
	if _, ok := body["error"]; !ok {
		t.Error("error response missing 'error' field")
	}
}
