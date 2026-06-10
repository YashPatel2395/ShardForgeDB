package gateway

import (
	"fmt"
	"testing"
)

// threeNodes is a standard 3-node config used across ring tests.
var threeNodes = []NodeConfig{
	{ID: "node-1", BaseURL: "http://127.0.0.1:9101"},
	{ID: "node-2", BaseURL: "http://127.0.0.1:9102"},
	{ID: "node-3", BaseURL: "http://127.0.0.1:9103"},
}

// --- Validation tests ---

func TestRing_RejectsEmptyNodeList(t *testing.T) {
	_, err := newHashRing(nil, 128)
	if err == nil {
		t.Fatal("expected error for empty node list")
	}
}

func TestRing_RejectsEmptyNodeID(t *testing.T) {
	_, err := newHashRing([]NodeConfig{{ID: "", BaseURL: "http://x"}}, 128)
	if err == nil {
		t.Fatal("expected error for empty node ID")
	}
}

func TestRing_RejectsEmptyBaseURL(t *testing.T) {
	_, err := newHashRing([]NodeConfig{{ID: "n1", BaseURL: ""}}, 128)
	if err == nil {
		t.Fatal("expected error for empty BaseURL")
	}
}

func TestRing_RejectsDuplicateNodeID(t *testing.T) {
	nodes := []NodeConfig{
		{ID: "n1", BaseURL: "http://a"},
		{ID: "n1", BaseURL: "http://b"},
	}
	_, err := newHashRing(nodes, 128)
	if err == nil {
		t.Fatal("expected error for duplicate node ID")
	}
}

func TestRing_RejectsDuplicateBaseURL(t *testing.T) {
	nodes := []NodeConfig{
		{ID: "n1", BaseURL: "http://a"},
		{ID: "n2", BaseURL: "http://a"},
	}
	_, err := newHashRing(nodes, 128)
	if err == nil {
		t.Fatal("expected error for duplicate BaseURL")
	}
}

// --- Default virtual nodes ---

func TestRing_DefaultVirtualNodes(t *testing.T) {
	// passing 0 should use the default (128); newHashRing enforces default internally
	r, err := newHashRing(threeNodes, 128)
	if err != nil {
		t.Fatalf("newHashRing: %v", err)
	}
	// 3 nodes × 128 virtual nodes × weight 1 = 384 points
	if len(r.points) != 384 {
		t.Errorf("want 384 ring points, got %d", len(r.points))
	}
	// Also verify that passing 0 is treated as 128 (same count).
	r2, err := newHashRing(threeNodes, 0)
	if err != nil {
		t.Fatalf("newHashRing(0): %v", err)
	}
	if len(r2.points) != 384 {
		t.Errorf("want 384 points with virtualNodes=0, got %d", len(r2.points))
	}
}

// --- Routing determinism ---

func TestRing_SameKeyRoutesToSameNode(t *testing.T) {
	r, _ := newHashRing(threeNodes, 128)
	first, _ := r.nodeForKey([]byte("user:alice"))
	for i := 0; i < 100; i++ {
		got, _ := r.nodeForKey([]byte("user:alice"))
		if got.ID != first.ID {
			t.Fatalf("routing is not deterministic: got %q then %q", first.ID, got.ID)
		}
	}
}

func TestRing_InputOrderDoesNotAffectRouting(t *testing.T) {
	// Same nodes, different order.
	ordered := []NodeConfig{
		{ID: "node-1", BaseURL: "http://127.0.0.1:9101"},
		{ID: "node-2", BaseURL: "http://127.0.0.1:9102"},
		{ID: "node-3", BaseURL: "http://127.0.0.1:9103"},
	}
	reversed := []NodeConfig{
		{ID: "node-3", BaseURL: "http://127.0.0.1:9103"},
		{ID: "node-2", BaseURL: "http://127.0.0.1:9102"},
		{ID: "node-1", BaseURL: "http://127.0.0.1:9101"},
	}
	r1, _ := newHashRing(ordered, 128)
	r2, _ := newHashRing(reversed, 128)

	keys := []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta"}
	for _, k := range keys {
		n1, _ := r1.nodeForKey([]byte(k))
		n2, _ := r2.nodeForKey([]byte(k))
		if n1.ID != n2.ID {
			t.Errorf("key %q: ordered→%s, reversed→%s", k, n1.ID, n2.ID)
		}
	}
}

func TestRing_DifferentKeysDistribute(t *testing.T) {
	r, _ := newHashRing(threeNodes, 128)
	seen := make(map[string]bool)
	keys := []string{
		"user:1", "user:2", "user:3", "user:4",
		"order:1", "order:2", "order:3",
		"product:1", "product:2",
		"session:abc", "session:def",
	}
	for _, k := range keys {
		n, _ := r.nodeForKey([]byte(k))
		seen[n.ID] = true
	}
	if len(seen) < 2 {
		t.Errorf("expected keys to distribute across at least 2 nodes, only got %v", seen)
	}
}

func TestRing_WrapAround(t *testing.T) {
	// Use one node so the ring always returns the same node (trivial wrap).
	single := []NodeConfig{{ID: "n1", BaseURL: "http://x"}}
	r, _ := newHashRing(single, 128)

	// Any key must route to n1.
	for _, k := range []string{"aaa", "bbb", "zzz", "000"} {
		n, err := r.nodeForKey([]byte(k))
		if err != nil {
			t.Errorf("nodeForKey(%q): %v", k, err)
		}
		if n.ID != "n1" {
			t.Errorf("expected n1, got %q", n.ID)
		}
	}
}

// --- Weight ---

func TestRing_WeightIncreasesRingPoints(t *testing.T) {
	// Use a higher base virtualNodes to reduce hash-space variance.
	weighted := []NodeConfig{
		{ID: "heavy", BaseURL: "http://a", Weight: 3},
		{ID: "light", BaseURL: "http://b", Weight: 1},
	}
	r, _ := newHashRing(weighted, 256)
	// heavy: 256*3=768, light: 256*1=256 → total 1024
	if len(r.points) != 1024 {
		t.Errorf("want 1024 ring points, got %d", len(r.points))
	}

	// Heavier node should win more keys with high virtual-node counts.
	heavyCount := 0
	total := 2000
	for i := 0; i < total; i++ {
		key := []byte(fmt.Sprintf("weight-test-key-%06d", i))
		n, _ := r.nodeForKey(key)
		if n.ID == "heavy" {
			heavyCount++
		}
	}
	// With 3× weight and 256 virtual nodes, heavy should reliably win >55%.
	threshold := total * 55 / 100
	if heavyCount < threshold {
		t.Errorf("weighted node should win majority; heavy=%d/%d (threshold %d)", heavyCount, total, threshold)
	}
}

func TestRing_ZeroWeightTreatedAsOne(t *testing.T) {
	nodes := []NodeConfig{
		{ID: "n1", BaseURL: "http://a", Weight: 0},
		{ID: "n2", BaseURL: "http://b", Weight: 0},
	}
	r, _ := newHashRing(nodes, 128)
	// 0 → treated as 1: 128+128 = 256 points
	if len(r.points) != 256 {
		t.Errorf("want 256 points, got %d", len(r.points))
	}
}

// --- Empty key ---

func TestRing_EmptyKeyReturnsError(t *testing.T) {
	r, _ := newHashRing(threeNodes, 128)
	_, err := r.nodeForKey(nil)
	if err != ErrInvalidKey {
		t.Errorf("want ErrInvalidKey, got %v", err)
	}
	_, err = r.nodeForKey([]byte(""))
	if err != ErrInvalidKey {
		t.Errorf("want ErrInvalidKey for empty string, got %v", err)
	}
}

// --- nodeByID ---

func TestRing_NodeByID_Found(t *testing.T) {
	r, _ := newHashRing(threeNodes, 128)
	n, err := r.nodeByID("node-2")
	if err != nil {
		t.Fatalf("nodeByID: %v", err)
	}
	if n.BaseURL != "http://127.0.0.1:9102" {
		t.Errorf("want 9102, got %q", n.BaseURL)
	}
}

func TestRing_NodeByID_NotFound(t *testing.T) {
	r, _ := newHashRing(threeNodes, 128)
	_, err := r.nodeByID("does-not-exist")
	if err == nil {
		t.Fatal("expected ErrUnknownNode")
	}
}

// --- Determinism across two ring instances ---

func TestRing_DeterministicAcrossInstances(t *testing.T) {
	r1, _ := newHashRing(threeNodes, 128)
	r2, _ := newHashRing(threeNodes, 128)
	keys := []string{"foo", "bar", "baz", "qux", "quux", "user:999", "order:42"}
	for _, k := range keys {
		n1, _ := r1.nodeForKey([]byte(k))
		n2, _ := r2.nodeForKey([]byte(k))
		if n1.ID != n2.ID {
			t.Errorf("key %q: r1→%s, r2→%s (not deterministic across instances)", k, n1.ID, n2.ID)
		}
	}
}
