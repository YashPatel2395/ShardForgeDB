package replnet_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/YashPatel2395/ShardForgeDB/internal/replnet"
)

// fakeLogServer returns a test HTTP server that simulates a primary's /replication/log endpoint.
func fakeLogServer(t *testing.T, entries []replnet.Entry) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/replication/log" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"node_id": "primary",
			"after":   0,
			"count":   len(entries),
			"entries": entries,
		})
	}))
}

func TestReplicator_PullEntries_ReturnsEntries(t *testing.T) {
	entries := []replnet.Entry{
		{Seq: 1, Op: replnet.OpPut, Key: "a", Value: "1", Timestamp: time.Now()},
		{Seq: 2, Op: replnet.OpDelete, Key: "b", Timestamp: time.Now()},
	}
	srv := fakeLogServer(t, entries)
	defer srv.Close()

	r := replnet.NewReplicator(srv.URL, 0)
	got, err := r.PullEntries(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("PullEntries: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
	if got[0].Key != "a" || got[0].Op != replnet.OpPut {
		t.Errorf("entry[0] unexpected: %+v", got[0])
	}
	if got[1].Key != "b" || got[1].Op != replnet.OpDelete {
		t.Errorf("entry[1] unexpected: %+v", got[1])
	}
}

func TestReplicator_PullEntries_EmptyLog_ReturnsEmpty(t *testing.T) {
	srv := fakeLogServer(t, nil)
	defer srv.Close()

	r := replnet.NewReplicator(srv.URL, 0)
	got, err := r.PullEntries(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("PullEntries: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d entries, want 0", len(got))
	}
}

func TestReplicator_PullEntries_ServerError_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	r := replnet.NewReplicator(srv.URL, 0)
	_, err := r.PullEntries(context.Background(), 0, 0)
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}

func TestReplicator_PullEntries_BadJSON_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not-json"))
	}))
	defer srv.Close()

	r := replnet.NewReplicator(srv.URL, 0)
	_, err := r.PullEntries(context.Background(), 0, 0)
	if err == nil {
		t.Fatal("expected error for bad JSON, got nil")
	}
}

func TestReplicator_PullEntries_CancelledContext_ReturnsError(t *testing.T) {
	srv := fakeLogServer(t, nil)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	r := replnet.NewReplicator(srv.URL, 0)
	_, err := r.PullEntries(ctx, 0, 0)
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}

func TestNewReplicator_ZeroTimeout_UsesDefault(t *testing.T) {
	// Should not panic; just verify construction succeeds.
	r := replnet.NewReplicator("http://127.0.0.1:9999", 0)
	if r == nil {
		t.Fatal("NewReplicator returned nil")
	}
}
