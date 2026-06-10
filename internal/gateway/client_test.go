package gateway

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/YashPatel2395/ShardForgeDB/internal/node"
)

// startNode opens and starts a node.Server in the background for tests.
// The server is cleaned up via t.Cleanup.
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
		t.Fatalf("StartBackground %s: %v", id, err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// openGateway creates a Gateway over the given node.Server slice.
func openGateway(t testing.TB, servers []*node.Server) *Gateway {
	t.Helper()
	nodes := make([]NodeConfig, len(servers))
	for i, s := range servers {
		nodes[i] = NodeConfig{
			ID:      s.Status().NodeID,
			BaseURL: "http://" + s.Addr(),
		}
	}
	g, err := Open(Options{Nodes: nodes, VirtualNodes: 128, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Open gateway: %v", err)
	}
	t.Cleanup(func() { _ = g.Close() })
	return g
}

// --- Gateway option validation ---

func TestGateway_Open_RejectsEmptyNodes(t *testing.T) {
	_, err := Open(Options{Nodes: nil})
	if err == nil {
		t.Fatal("expected error for empty nodes")
	}
}

func TestGateway_Open_RejectsDuplicateID(t *testing.T) {
	_, err := Open(Options{Nodes: []NodeConfig{
		{ID: "n1", BaseURL: "http://a"},
		{ID: "n1", BaseURL: "http://b"},
	}})
	if err == nil {
		t.Fatal("expected error for duplicate ID")
	}
}

func TestGateway_Open_RejectsDuplicateURL(t *testing.T) {
	_, err := Open(Options{Nodes: []NodeConfig{
		{ID: "n1", BaseURL: "http://a"},
		{ID: "n2", BaseURL: "http://a"},
	}})
	if err == nil {
		t.Fatal("expected error for duplicate URL")
	}
}

// --- NodeForKey ---

func TestGateway_NodeForKey_EmptyKey(t *testing.T) {
	s := startNode(t, "node-1")
	g := openGateway(t, []*node.Server{s})
	_, err := g.NodeForKey(nil)
	if !errors.Is(err, ErrInvalidKey) {
		t.Errorf("want ErrInvalidKey, got %v", err)
	}
}

func TestGateway_NodeForKey_ReturnsSomething(t *testing.T) {
	s := startNode(t, "node-1")
	g := openGateway(t, []*node.Server{s})
	rn, err := g.NodeForKey([]byte("any-key"))
	if err != nil {
		t.Fatalf("NodeForKey: %v", err)
	}
	if rn.ID == "" || rn.BaseURL == "" {
		t.Errorf("expected non-empty RoutedNode, got %+v", rn)
	}
}

// --- Put/Get/Delete routing ---

func TestGateway_PutGet_RoutedCorrectly(t *testing.T) {
	s1 := startNode(t, "node-1")
	s2 := startNode(t, "node-2")
	s3 := startNode(t, "node-3")
	g := openGateway(t, []*node.Server{s1, s2, s3})
	ctx := context.Background()

	key := []byte("user:alice")
	if err := g.Put(ctx, key, []byte("alice-val")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	val, found, err := g.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !found {
		t.Fatal("Get: expected found=true after Put")
	}
	if string(val) != "alice-val" {
		t.Errorf("Get: want alice-val, got %q", val)
	}
}

func TestGateway_SameKeyRoutesToSameNode(t *testing.T) {
	s1 := startNode(t, "node-1")
	s2 := startNode(t, "node-2")
	s3 := startNode(t, "node-3")
	g := openGateway(t, []*node.Server{s1, s2, s3})

	key := []byte("stable-key")
	rn1, _ := g.NodeForKey(key)
	for i := 0; i < 20; i++ {
		rn, _ := g.NodeForKey(key)
		if rn.ID != rn1.ID {
			t.Fatalf("routing changed: %q → %q then %q", key, rn1.ID, rn.ID)
		}
	}
}

func TestGateway_KeyWrittenOnRouted_NotOnOthers(t *testing.T) {
	s1 := startNode(t, "node-1")
	s2 := startNode(t, "node-2")
	s3 := startNode(t, "node-3")
	g := openGateway(t, []*node.Server{s1, s2, s3})
	ctx := context.Background()

	key := []byte("isolated:key:42")
	rn, _ := g.NodeForKey(key)
	_ = g.Put(ctx, key, []byte("v"))

	// Check all non-routed servers directly — key must not exist there.
	for _, s := range []*node.Server{s1, s2, s3} {
		if s.Status().NodeID == rn.ID {
			continue
		}
		c := node.NewClient("http://"+s.Addr(), 5*time.Second)
		_, found, err := c.Get(ctx, key)
		if err != nil {
			t.Errorf("direct get from %s: %v", s.Status().NodeID, err)
		}
		if found {
			t.Errorf("key found on non-routed node %s — isolation broken", s.Status().NodeID)
		}
	}
}

func TestGateway_Delete(t *testing.T) {
	s1 := startNode(t, "node-1")
	g := openGateway(t, []*node.Server{s1})
	ctx := context.Background()

	key := []byte("del-key")
	_ = g.Put(ctx, key, []byte("v"))
	if err := g.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, found, _ := g.Get(ctx, key)
	if found {
		t.Error("key should be gone after Delete")
	}
}

func TestGateway_Put_EmptyKeyRejected(t *testing.T) {
	s := startNode(t, "node-1")
	g := openGateway(t, []*node.Server{s})
	err := g.Put(context.Background(), nil, []byte("v"))
	if !errors.Is(err, ErrInvalidKey) {
		t.Errorf("want ErrInvalidKey, got %v", err)
	}
}

func TestGateway_Get_EmptyKeyRejected(t *testing.T) {
	s := startNode(t, "node-1")
	g := openGateway(t, []*node.Server{s})
	_, _, err := g.Get(context.Background(), nil)
	if !errors.Is(err, ErrInvalidKey) {
		t.Errorf("want ErrInvalidKey, got %v", err)
	}
}

func TestGateway_Delete_EmptyKeyRejected(t *testing.T) {
	s := startNode(t, "node-1")
	g := openGateway(t, []*node.Server{s})
	err := g.Delete(context.Background(), nil)
	if !errors.Is(err, ErrInvalidKey) {
		t.Errorf("want ErrInvalidKey, got %v", err)
	}
}

// --- ScanNode ---

func TestGateway_ScanNode_OnlyScansOneNode(t *testing.T) {
	s1 := startNode(t, "node-1")
	s2 := startNode(t, "node-2")
	g := openGateway(t, []*node.Server{s1, s2})
	ctx := context.Background()

	// Put a key directly on s1 via its client.
	c1 := node.NewClient("http://"+s1.Addr(), 5*time.Second)
	_ = c1.Put(ctx, []byte("only-on-node1"), []byte("v1"))

	// ScanNode on node-1 should find it.
	entries, err := g.ScanNode(ctx, "node-1", nil, nil)
	if err != nil {
		t.Fatalf("ScanNode node-1: %v", err)
	}
	found := false
	for _, e := range entries {
		if e.Key == "only-on-node1" {
			found = true
		}
	}
	if !found {
		t.Error("expected key on node-1 to be visible in ScanNode")
	}

	// ScanNode on node-2 must not find it.
	entries2, err := g.ScanNode(ctx, "node-2", nil, nil)
	if err != nil {
		t.Fatalf("ScanNode node-2: %v", err)
	}
	for _, e := range entries2 {
		if e.Key == "only-on-node1" {
			t.Error("key from node-1 leaked into ScanNode on node-2")
		}
	}
}

func TestGateway_ScanNode_UnknownNodeReturnsError(t *testing.T) {
	s := startNode(t, "node-1")
	g := openGateway(t, []*node.Server{s})
	_, err := g.ScanNode(context.Background(), "ghost-node", nil, nil)
	if !errors.Is(err, ErrUnknownNode) {
		t.Errorf("want ErrUnknownNode, got %v", err)
	}
}

// --- FlushAll / CompactAll ---

func TestGateway_FlushAll(t *testing.T) {
	s1 := startNode(t, "node-1")
	s2 := startNode(t, "node-2")
	g := openGateway(t, []*node.Server{s1, s2})
	ctx := context.Background()
	_ = g.Put(ctx, []byte("fk"), []byte("fv"))
	if err := g.FlushAll(ctx); err != nil {
		t.Fatalf("FlushAll: %v", err)
	}
}

func TestGateway_CompactAll(t *testing.T) {
	s1 := startNode(t, "node-1")
	s2 := startNode(t, "node-2")
	g := openGateway(t, []*node.Server{s1, s2})
	ctx := context.Background()
	_ = g.Put(ctx, []byte("ck"), []byte("cv"))
	_ = g.FlushAll(ctx)
	if err := g.CompactAll(ctx); err != nil {
		t.Fatalf("CompactAll: %v", err)
	}
}

// --- HealthAll ---

func TestGateway_HealthAll_AllHealthy(t *testing.T) {
	s1 := startNode(t, "node-1")
	s2 := startNode(t, "node-2")
	s3 := startNode(t, "node-3")
	g := openGateway(t, []*node.Server{s1, s2, s3})
	results := g.HealthAll(context.Background())
	if len(results) != 3 {
		t.Errorf("want 3 health results, got %d", len(results))
	}
	for id, err := range results {
		if err != nil {
			t.Errorf("node %s unhealthy: %v", id, err)
		}
	}
}

func TestGateway_HealthAll_ReturnsEntryPerNode(t *testing.T) {
	s1 := startNode(t, "node-1")
	s2 := startNode(t, "node-2")
	g := openGateway(t, []*node.Server{s1, s2})
	results := g.HealthAll(context.Background())
	if _, ok := results["node-1"]; !ok {
		t.Error("missing node-1 in HealthAll results")
	}
	if _, ok := results["node-2"]; !ok {
		t.Error("missing node-2 in HealthAll results")
	}
}

// --- No automatic failover ---

func TestGateway_NoAutoFailover(t *testing.T) {
	// Create a gateway pointing at a port where no server is running.
	// Put should fail — there is no retry to another node.
	g, err := Open(Options{
		Nodes: []NodeConfig{
			{ID: "unreachable", BaseURL: "http://127.0.0.1:1"}, // port 1 not listening
		},
		Timeout: 200 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer g.Close()

	err = g.Put(context.Background(), []byte("k"), []byte("v"))
	if err == nil {
		t.Fatal("expected error when node is unreachable (no failover)")
	}
}

// --- Close behavior ---

func TestGateway_Close_Idempotent(t *testing.T) {
	s := startNode(t, "node-1")
	g := openGateway(t, []*node.Server{s})
	if err := g.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := g.Close(); err != nil {
		t.Errorf("second Close (idempotent): %v", err)
	}
}

func TestGateway_OperationsAfterClose_ReturnErrClosed(t *testing.T) {
	s := startNode(t, "node-1")
	g := openGateway(t, []*node.Server{s})
	_ = g.Close()
	ctx := context.Background()

	if err := g.Put(ctx, []byte("k"), []byte("v")); !errors.Is(err, ErrClosed) {
		t.Errorf("Put after close: want ErrClosed, got %v", err)
	}
	if _, _, err := g.Get(ctx, []byte("k")); !errors.Is(err, ErrClosed) {
		t.Errorf("Get after close: want ErrClosed, got %v", err)
	}
	if err := g.Delete(ctx, []byte("k")); !errors.Is(err, ErrClosed) {
		t.Errorf("Delete after close: want ErrClosed, got %v", err)
	}
	if _, err := g.NodeForKey([]byte("k")); !errors.Is(err, ErrClosed) {
		t.Errorf("NodeForKey after close: want ErrClosed, got %v", err)
	}
	if _, err := g.ScanNode(ctx, "node-1", nil, nil); !errors.Is(err, ErrClosed) {
		t.Errorf("ScanNode after close: want ErrClosed, got %v", err)
	}
	if err := g.FlushAll(ctx); !errors.Is(err, ErrClosed) {
		t.Errorf("FlushAll after close: want ErrClosed, got %v", err)
	}
	if err := g.CompactAll(ctx); !errors.Is(err, ErrClosed) {
		t.Errorf("CompactAll after close: want ErrClosed, got %v", err)
	}
}

// --- Stats ---

func TestGateway_Stats(t *testing.T) {
	s1 := startNode(t, "node-1")
	s2 := startNode(t, "node-2")
	g := openGateway(t, []*node.Server{s1, s2})
	st := g.Stats()
	if st.NodeCount != 2 {
		t.Errorf("want NodeCount=2, got %d", st.NodeCount)
	}
	if st.VirtualNodes != 128 {
		t.Errorf("want VirtualNodes=128, got %d", st.VirtualNodes)
	}
	if len(st.Nodes) != 2 {
		t.Errorf("want 2 node IDs in Stats.Nodes, got %d", len(st.Nodes))
	}
}

// --- Client timeout ---

func TestGateway_Timeout_PropagatesclearError(t *testing.T) {
	// Gateway points to a port that is not listening.
	g, _ := Open(Options{
		Nodes:   []NodeConfig{{ID: "unreachable", BaseURL: "http://127.0.0.1:1"}},
		Timeout: 100 * time.Millisecond,
	})
	defer g.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := g.Put(ctx, []byte("k"), []byte("v"))
	if err == nil {
		t.Fatal("expected timeout/unavailable error")
	}
}

// --- Concurrent Put/Get (race detector) ---

func TestGateway_ConcurrentPutGet_Race(t *testing.T) {
	s1 := startNode(t, "node-1")
	s2 := startNode(t, "node-2")
	s3 := startNode(t, "node-3")
	g := openGateway(t, []*node.Server{s1, s2, s3})
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 15; j++ {
				key := []byte(fmt.Sprintf("race-k-%d-%d", i, j))
				_ = g.Put(ctx, key, []byte("v"))
				_, _, _ = g.Get(ctx, key)
			}
		}()
	}
	wg.Wait()
}

// --- Deterministic routing across two gateway instances ---

func TestGateway_DeterministicAcrossTwoInstances(t *testing.T) {
	s1 := startNode(t, "node-1")
	s2 := startNode(t, "node-2")
	s3 := startNode(t, "node-3")

	cfg := []NodeConfig{
		{ID: "node-1", BaseURL: "http://" + s1.Addr()},
		{ID: "node-2", BaseURL: "http://" + s2.Addr()},
		{ID: "node-3", BaseURL: "http://" + s3.Addr()},
	}
	g1, _ := Open(Options{Nodes: cfg, VirtualNodes: 128, Timeout: 5 * time.Second})
	g2, _ := Open(Options{Nodes: cfg, VirtualNodes: 128, Timeout: 5 * time.Second})
	defer g1.Close()
	defer g2.Close()

	keys := []string{"foo", "bar", "baz", "alpha", "beta", "user:99", "order:42"}
	for _, k := range keys {
		rn1, _ := g1.NodeForKey([]byte(k))
		rn2, _ := g2.NodeForKey([]byte(k))
		if rn1.ID != rn2.ID {
			t.Errorf("key %q: g1→%s, g2→%s (routing not deterministic)", k, rn1.ID, rn2.ID)
		}
	}
}
