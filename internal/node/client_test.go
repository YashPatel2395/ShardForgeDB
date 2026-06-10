package node

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newTestHTTPServer starts an httptest.Server backed by a node Server.
// Returns the node Server and the httptest.Server. Both are cleaned up via tb.Cleanup.
func newTestHTTPServer(tb testing.TB, nodeID string) (*Server, *httptest.Server) {
	tb.Helper()
	s := newTestServer(tb, nodeID)
	ts := httptest.NewServer(s.Handler())
	tb.Cleanup(ts.Close)
	return s, ts
}

func TestClient_PutGet(t *testing.T) {
	_, ts := newTestHTTPServer(t, "node-client")
	c := NewClient(ts.URL, 5*time.Second)

	ctx := context.Background()
	if err := c.Put(ctx, []byte("key1"), []byte("val1")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	val, found, err := c.Get(ctx, []byte("key1"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !found {
		t.Fatal("Get: expected found=true")
	}
	if string(val) != "val1" {
		t.Errorf("Get: want val1, got %q", val)
	}
}

func TestClient_GetMissing(t *testing.T) {
	_, ts := newTestHTTPServer(t, "node-client")
	c := NewClient(ts.URL, 5*time.Second)

	val, found, err := c.Get(context.Background(), []byte("no-such-key"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if found {
		t.Fatalf("Get: expected found=false, got val=%q", val)
	}
}

func TestClient_Delete(t *testing.T) {
	_, ts := newTestHTTPServer(t, "node-client")
	c := NewClient(ts.URL, 5*time.Second)
	ctx := context.Background()

	_ = c.Put(ctx, []byte("dk"), []byte("dv"))
	if err := c.Delete(ctx, []byte("dk")); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, found, _ := c.Get(ctx, []byte("dk"))
	if found {
		t.Error("expected key to be deleted")
	}
}

func TestClient_Health(t *testing.T) {
	_, ts := newTestHTTPServer(t, "node-hc")
	c := NewClient(ts.URL, 5*time.Second)
	if err := c.Health(context.Background()); err != nil {
		t.Fatalf("Health: %v", err)
	}
}

func TestClient_Status(t *testing.T) {
	_, ts := newTestHTTPServer(t, "node-st")
	c := NewClient(ts.URL, 5*time.Second)
	st, err := c.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.NodeID != "node-st" {
		t.Errorf("want node_id=node-st, got %q", st.NodeID)
	}
	if st.StartedAt.IsZero() {
		t.Error("want non-zero started_at")
	}
}

func TestClient_Scan(t *testing.T) {
	_, ts := newTestHTTPServer(t, "node-scan")
	c := NewClient(ts.URL, 5*time.Second)
	ctx := context.Background()

	for _, k := range []string{"s:b", "s:a", "s:c"} {
		_ = c.Put(ctx, []byte(k), []byte(k+"-v"))
	}
	entries, err := c.Scan(ctx, []byte("s:"), []byte("s:~"))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("want 3 entries, got %d", len(entries))
	}
}

func TestClient_Flush(t *testing.T) {
	_, ts := newTestHTTPServer(t, "node-fl")
	c := NewClient(ts.URL, 5*time.Second)
	ctx := context.Background()
	_ = c.Put(ctx, []byte("fk"), []byte("fv"))
	if err := c.Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}
}

func TestClient_Compact(t *testing.T) {
	_, ts := newTestHTTPServer(t, "node-co")
	c := NewClient(ts.URL, 5*time.Second)
	ctx := context.Background()
	_ = c.Put(ctx, []byte("ck"), []byte("cv"))
	_ = c.Flush(ctx)
	if err := c.Compact(ctx); err != nil {
		t.Fatalf("Compact: %v", err)
	}
}

// TestClient_Timeout verifies that a cancelled context produces a clear error.
func TestClient_Timeout(t *testing.T) {
	// Create a server that never responds (hangs forever).
	hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block indefinitely.
		<-r.Context().Done()
	}))
	defer hang.Close()

	c := NewClient(hang.URL, 30*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := c.Health(ctx)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

// TestClient_InvalidJSON verifies that a server returning garbage JSON produces a clear error.
func TestClient_InvalidJSON(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not-json{{{"))
	}))
	defer bad.Close()

	c := NewClient(bad.URL, 5*time.Second)
	_, err := c.Status(context.Background())
	if err == nil {
		t.Fatal("expected invalid JSON error, got nil")
	}
}

// TestClient_ServerSideError verifies that a 5xx response with a JSON error body is surfaced.
func TestClient_ServerSideError(t *testing.T) {
	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "disk full"})
	}))
	defer errSrv.Close()

	c := NewClient(errSrv.URL, 5*time.Second)
	err := c.Health(context.Background())
	if err == nil {
		t.Fatal("expected server-side error, got nil")
	}
}

// TestClient_NodeUnavailable verifies that a connection refused error surfaces clearly.
func TestClient_NodeUnavailable(t *testing.T) {
	// Port 1 is almost certainly not listening.
	c := NewClient("http://127.0.0.1:1", 200*time.Millisecond)
	err := c.Health(context.Background())
	if err == nil {
		t.Fatal("expected node unavailable error, got nil")
	}
}
