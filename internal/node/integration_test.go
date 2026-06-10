package node

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// --- Multi-server isolation tests ---

// TestMultipleServers_IndependentData verifies that writing to node-1 does not appear on node-2.
func TestMultipleServers_IndependentData(t *testing.T) {
	s1 := newTestServer(t, "node-1")
	s2 := newTestServer(t, "node-2")

	// Write to s1 only.
	w := doRequest(t, s1, "PUT", "/kv/shared-key", `{"value":"from-node-1"}`)
	if w.Code != 200 {
		t.Fatalf("s1 PUT: want 200, got %d", w.Code)
	}

	// s2 must not have the key.
	w2 := doRequest(t, s2, "GET", "/kv/shared-key", "")
	var resp getResponse
	decodeJSON(t, w2, &resp)
	if resp.Found {
		t.Error("node-2 should not see key written to node-1")
	}
}

// TestMultipleServers_Concurrent verifies that multiple servers can run simultaneously.
func TestMultipleServers_Concurrent(t *testing.T) {
	servers := make([]*Server, 3)
	for i := range servers {
		servers[i] = newTestServer(t, fmt.Sprintf("node-%d", i+1))
	}

	var wg sync.WaitGroup
	for i, s := range servers {
		i, s := i, s
		wg.Add(1)
		go func() {
			defer wg.Done()
			key := fmt.Sprintf("/kv/key-%d", i)
			val := fmt.Sprintf(`{"value":"val-%d"}`, i)
			w := doRequest(t, s, "PUT", key, val)
			if w.Code != 200 {
				t.Errorf("server %d PUT: want 200, got %d", i, w.Code)
			}
		}()
	}
	wg.Wait()
}

// TestRestart_PreservesData verifies that data survives a Server close + reopen on the same DataDir.
func TestRestart_PreservesData(t *testing.T) {
	dir := t.TempDir()

	// Open, write, flush, close.
	s1, err := Open(Options{NodeID: "node-restart", Addr: "127.0.0.1:0", DataDir: dir})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := doRequest(t, s1, "PUT", "/kv/persist-key", `{"value":"persist-val"}`)
	if w.Code != 200 {
		t.Fatalf("PUT: want 200, got %d; body: %s", w.Code, w.Body.String())
	}
	// Flush so data is in an SSTable (WAL replay on reopen also works, but flush guarantees persistence).
	doRequest(t, s1, "POST", "/flush", "")
	if err := s1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopen on the same DataDir.
	s2, err := Open(Options{NodeID: "node-restart", Addr: "127.0.0.1:0", DataDir: dir})
	if err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	defer s2.Close()

	w2 := doRequest(t, s2, "GET", "/kv/persist-key", "")
	var resp getResponse
	decodeJSON(t, w2, &resp)
	if !resp.Found {
		t.Error("expected key to survive restart")
	}
	if resp.Value != "persist-val" {
		t.Errorf("want persist-val, got %q", resp.Value)
	}
}

// TestThreeHTTPServers_ClientTalkToAll starts 3 real HTTP servers on :0 and talks to each.
func TestThreeHTTPServers_ClientTalkToAll(t *testing.T) {
	type nodeInfo struct {
		srv *Server
		url string
	}
	nodes := make([]nodeInfo, 3)

	for i := range nodes {
		s := newTestServer(t, fmt.Sprintf("node-%d", i+1))
		if err := s.StartBackground(); err != nil {
			t.Fatalf("StartBackground node-%d: %v", i+1, err)
		}
		t.Cleanup(func() { _ = s.Close() })
		nodes[i] = nodeInfo{srv: s, url: "http://" + s.Addr()}
	}

	ctx := context.Background()
	for _, n := range nodes {
		c := NewClient(n.url, 5*time.Second)
		if err := c.Health(ctx); err != nil {
			t.Errorf("Health %s: %v", n.url, err)
		}
	}

	// Write unique key to each node and verify isolation.
	for i, n := range nodes {
		c := NewClient(n.url, 5*time.Second)
		key := []byte(fmt.Sprintf("node%d-key", i+1))
		val := []byte(fmt.Sprintf("node%d-val", i+1))
		if err := c.Put(ctx, key, val); err != nil {
			t.Errorf("Put to node-%d: %v", i+1, err)
		}
		// Check that this key is NOT on the other nodes.
		for j, other := range nodes {
			if j == i {
				continue
			}
			oc := NewClient(other.url, 5*time.Second)
			_, found, err := oc.Get(ctx, key)
			if err != nil {
				t.Errorf("Get from node-%d: %v", j+1, err)
			}
			if found {
				t.Errorf("key written to node-%d unexpectedly found on node-%d", i+1, j+1)
			}
		}
	}
}

// TestConcurrentPutGet_Race exercises concurrent Put/Get to one node to catch data races.
func TestConcurrentPutGet_Race(t *testing.T) {
	s := newTestServer(t, "node-race")
	if err := s.StartBackground(); err != nil {
		t.Fatalf("StartBackground: %v", err)
	}

	c := NewClient("http://"+s.Addr(), 5*time.Second)
	ctx := context.Background()

	const goroutines = 10
	const opsEach = 20
	var wg sync.WaitGroup

	for g := 0; g < goroutines; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < opsEach; i++ {
				key := []byte(fmt.Sprintf("g%d-k%d", g, i))
				val := []byte(fmt.Sprintf("g%d-v%d", g, i))
				if err := c.Put(ctx, key, val); err != nil {
					t.Errorf("concurrent Put: %v", err)
					return
				}
				_, _, err := c.Get(ctx, key)
				if err != nil {
					t.Errorf("concurrent Get: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
}

// TestClose_AfterCloseReturnsError verifies that StartBackground after Close returns ErrClosed.
func TestClose_AfterCloseIsNoop(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(Options{NodeID: "node-noop", Addr: "127.0.0.1:0", DataDir: dir})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Second Close must not panic or return unexpected errors.
	if err := s.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	// StartBackground on a closed server must return ErrClosed.
	if err := s.StartBackground(); err != ErrClosed {
		t.Errorf("want ErrClosed, got %v", err)
	}
}

// TestStartBackground_ThreeNodes verifies that three real servers bind to distinct ports.
func TestStartBackground_ThreeNodes(t *testing.T) {
	addrs := make(map[string]bool)
	for i := 0; i < 3; i++ {
		s := newTestServer(t, fmt.Sprintf("node-%d", i+1))
		if err := s.StartBackground(); err != nil {
			t.Fatalf("StartBackground: %v", err)
		}
		t.Cleanup(func() { _ = s.Close() })
		addr := s.Addr()
		if addrs[addr] {
			t.Errorf("duplicate address %q across nodes", addr)
		}
		addrs[addr] = true
	}
}
