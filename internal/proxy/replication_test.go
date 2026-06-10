package proxy

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/YashPatel2395/ShardForgeDB/internal/node"
	"github.com/YashPatel2395/ShardForgeDB/internal/replnet"
)

// startPrimaryNode opens and starts a node in primary replication role.
func startPrimaryNode(t testing.TB, id string) *node.Server {
	t.Helper()
	s, err := node.Open(node.Options{
		NodeID:  id,
		Addr:    "127.0.0.1:0",
		DataDir: t.TempDir(),
		Replication: node.ReplicationOptions{
			Role: replnet.RolePrimary,
		},
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

// startFollowerNode opens and starts a node in follower replication role.
func startFollowerNode(t testing.TB, id, primaryURL string) *node.Server {
	t.Helper()
	s, err := node.Open(node.Options{
		NodeID:  id,
		Addr:    "127.0.0.1:0",
		DataDir: t.TempDir(),
		Replication: node.ReplicationOptions{
			Role:           replnet.RoleFollower,
			PrimaryBaseURL: primaryURL,
		},
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

// kvPutOnNode sends a PUT /kv/{key} directly to the node handler.
func kvPutOnNode(t testing.TB, s *node.Server, key, value string) {
	t.Helper()
	body := bytes.NewReader([]byte(`{"value":"` + value + `"}`))
	req := httptest.NewRequest(http.MethodPut, "/kv/"+key, body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("kvPutOnNode %q: got %d (body=%s)", key, w.Code, w.Body.String())
	}
}

// --- GET /replication/status ---

func TestHandleReplicationStatus_FansOutToNodes(t *testing.T) {
	n1 := startNode(t, "n1")
	n2 := startNode(t, "n2")
	proxy := openProxy(t, []*node.Server{n1, n2})

	w := doHandler(t, proxy, http.MethodGet, "/replication/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /replication/status: got %d (body=%s)", w.Code, w.Body.String())
	}
	var resp map[string]any
	decodeJSON(t, w, &resp)
	nodes, ok := resp["nodes"].([]any)
	if !ok {
		t.Fatalf("nodes field missing or wrong type: %v", resp)
	}
	if len(nodes) != 2 {
		t.Errorf("got %d nodes, want 2", len(nodes))
	}
}

func TestHandleReplicationStatus_WrongMethod(t *testing.T) {
	n1 := startNode(t, "n1")
	proxy := openProxy(t, []*node.Server{n1})

	w := doHandler(t, proxy, http.MethodPost, "/replication/status", "")
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("got %d, want 405", w.Code)
	}
}

// --- POST /replication/sync-node/{nodeID} ---

func TestHandleReplicationSyncNode_UnknownNode_Returns404(t *testing.T) {
	n1 := startNode(t, "n1")
	proxy := openProxy(t, []*node.Server{n1})

	w := doHandler(t, proxy, http.MethodPost, "/replication/sync-node/does-not-exist", "")
	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", w.Code)
	}
}

func TestHandleReplicationSyncNode_EmptyNodeID(t *testing.T) {
	n1 := startNode(t, "n1")
	proxy := openProxy(t, []*node.Server{n1})

	w := doHandler(t, proxy, http.MethodPost, "/replication/sync-node/", "")
	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", w.Code)
	}
}

func TestHandleReplicationSyncNode_WrongMethod(t *testing.T) {
	n1 := startNode(t, "n1")
	proxy := openProxy(t, []*node.Server{n1})

	w := doHandler(t, proxy, http.MethodGet, "/replication/sync-node/n1", "")
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("got %d, want 405", w.Code)
	}
}

func TestHandleReplicationSyncNode_FollowerNode_Succeeds(t *testing.T) {
	primary := startPrimaryNode(t, "node-primary")
	// Write some data to the primary.
	kvPutOnNode(t, primary, "sync-test-key", "sync-test-val")

	follower := startFollowerNode(t, "node-follower", "http://"+primary.Addr())
	proxy := openProxy(t, []*node.Server{primary, follower})

	w := doHandler(t, proxy, http.MethodPost, "/replication/sync-node/node-follower", "")
	if w.Code != http.StatusOK {
		t.Fatalf("sync-node/node-follower: got %d (body=%s)", w.Code, w.Body.String())
	}
}
