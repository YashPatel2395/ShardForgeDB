package node

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/YashPatel2395/ShardForgeDB/internal/replnet"
)

// newPrimaryServer returns a Server configured as a replication primary.
func newPrimaryServer(tb testing.TB) *Server {
	tb.Helper()
	s, err := Open(Options{
		NodeID:  "primary",
		Addr:    "127.0.0.1:0",
		DataDir: tb.TempDir(),
		Replication: ReplicationOptions{
			Role: replnet.RolePrimary,
		},
	})
	if err != nil {
		tb.Fatalf("Open primary: %v", err)
	}
	tb.Cleanup(func() { _ = s.Close() })
	return s
}

// newFollowerServer returns a Server configured as a replication follower.
func newFollowerServer(tb testing.TB, primaryURL string) *Server {
	tb.Helper()
	s, err := Open(Options{
		NodeID:  "follower",
		Addr:    "127.0.0.1:0",
		DataDir: tb.TempDir(),
		Replication: ReplicationOptions{
			Role:           replnet.RoleFollower,
			PrimaryBaseURL: primaryURL,
		},
	})
	if err != nil {
		tb.Fatalf("Open follower: %v", err)
	}
	tb.Cleanup(func() { _ = s.Close() })
	return s
}

// --- Follower write rejection ---

func TestFollower_Put_Returns403(t *testing.T) {
	follower := newFollowerServer(t, "http://127.0.0.1:9999")
	w := doRequest(t, follower, http.MethodPut, "/kv/key1", `{"value":"v"}`)
	if w.Code != http.StatusForbidden {
		t.Errorf("PUT on follower: got %d, want %d", w.Code, http.StatusForbidden)
	}
	var resp map[string]any
	decodeJSON(t, w, &resp)
	if _, ok := resp["error"]; !ok {
		t.Error("expected error field in response")
	}
}

func TestFollower_Delete_Returns403(t *testing.T) {
	follower := newFollowerServer(t, "http://127.0.0.1:9999")
	w := doRequest(t, follower, http.MethodDelete, "/kv/key1", "")
	if w.Code != http.StatusForbidden {
		t.Errorf("DELETE on follower: got %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestFollower_Get_Allowed(t *testing.T) {
	follower := newFollowerServer(t, "http://127.0.0.1:9999")
	w := doRequest(t, follower, http.MethodGet, "/kv/key1", "")
	if w.Code != http.StatusOK {
		t.Errorf("GET on follower: got %d, want 200", w.Code)
	}
}

// --- Primary replication log ---

func TestPrimary_Put_AppendsToLog(t *testing.T) {
	primary := newPrimaryServer(t)
	doRequest(t, primary, http.MethodPut, "/kv/k1", `{"value":"v1"}`)
	doRequest(t, primary, http.MethodPut, "/kv/k2", `{"value":"v2"}`)

	stats, err := primary.replLog.Stats()
	if err != nil {
		t.Fatalf("log.Stats: %v", err)
	}
	if stats.Count != 2 {
		t.Errorf("log count = %d, want 2", stats.Count)
	}
}

func TestPrimary_Delete_AppendsToLog(t *testing.T) {
	primary := newPrimaryServer(t)
	doRequest(t, primary, http.MethodPut, "/kv/k1", `{"value":"v1"}`)
	doRequest(t, primary, http.MethodDelete, "/kv/k1", "")

	stats, _ := primary.replLog.Stats()
	if stats.Count != 2 {
		t.Errorf("log count = %d, want 2 (put+delete)", stats.Count)
	}
}

func TestStandalone_Put_NoLog(t *testing.T) {
	s := newTestServer(t, "standalone")
	doRequest(t, s, http.MethodPut, "/kv/k1", `{"value":"v1"}`)
	if s.replLog != nil {
		t.Error("standalone node should have nil replLog")
	}
}

// --- GET /replication/status ---

func TestHandleReplicationStatus_Primary(t *testing.T) {
	primary := newPrimaryServer(t)
	doRequest(t, primary, http.MethodPut, "/kv/k1", `{"value":"v"}`)

	w := doRequest(t, primary, http.MethodGet, "/replication/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /replication/status: got %d, want 200", w.Code)
	}
	var resp map[string]any
	decodeJSON(t, w, &resp)
	repl, ok := resp["replication"].(map[string]any)
	if !ok {
		t.Fatalf("replication field missing or wrong type: %v", resp)
	}
	if repl["role"] != "primary" {
		t.Errorf("role = %q, want primary", repl["role"])
	}
}

func TestHandleReplicationStatus_Standalone(t *testing.T) {
	s := newTestServer(t, "standalone")
	w := doRequest(t, s, http.MethodGet, "/replication/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d", w.Code)
	}
}

func TestHandleReplicationStatus_WrongMethod(t *testing.T) {
	s := newTestServer(t, "n1")
	w := doRequest(t, s, http.MethodPost, "/replication/status", "")
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("got %d, want 405", w.Code)
	}
}

// --- GET /replication/log ---

func TestHandleReplicationLog_Primary_ReturnsEntries(t *testing.T) {
	primary := newPrimaryServer(t)
	doRequest(t, primary, http.MethodPut, "/kv/k1", `{"value":"v1"}`)
	doRequest(t, primary, http.MethodPut, "/kv/k2", `{"value":"v2"}`)

	w := doRequest(t, primary, http.MethodGet, "/replication/log?after=0", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /replication/log: got %d", w.Code)
	}
	var resp map[string]any
	decodeJSON(t, w, &resp)
	count, ok := resp["count"].(float64)
	if !ok {
		t.Fatalf("count field missing: %v", resp)
	}
	if count != 2 {
		t.Errorf("count = %v, want 2", count)
	}
}

func TestHandleReplicationLog_After_FiltersEntries(t *testing.T) {
	primary := newPrimaryServer(t)
	doRequest(t, primary, http.MethodPut, "/kv/k1", `{"value":"v1"}`)
	doRequest(t, primary, http.MethodPut, "/kv/k2", `{"value":"v2"}`)
	doRequest(t, primary, http.MethodPut, "/kv/k3", `{"value":"v3"}`)

	w := doRequest(t, primary, http.MethodGet, "/replication/log?after=2", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /replication/log?after=2: got %d", w.Code)
	}
	var resp map[string]any
	decodeJSON(t, w, &resp)
	count := resp["count"].(float64)
	if count != 1 {
		t.Errorf("count = %v, want 1 (only seq 3)", count)
	}
}

func TestHandleReplicationLog_InvalidAfter(t *testing.T) {
	primary := newPrimaryServer(t)
	w := doRequest(t, primary, http.MethodGet, "/replication/log?after=notanumber", "")
	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", w.Code)
	}
}

func TestHandleReplicationLog_Follower_Returns403(t *testing.T) {
	follower := newFollowerServer(t, "http://127.0.0.1:9999")
	w := doRequest(t, follower, http.MethodGet, "/replication/log?after=0", "")
	if w.Code != http.StatusForbidden {
		t.Errorf("GET /replication/log on follower: got %d, want 403", w.Code)
	}
}

func TestHandleReplicationLog_Standalone_Returns403(t *testing.T) {
	s := newTestServer(t, "n1")
	w := doRequest(t, s, http.MethodGet, "/replication/log?after=0", "")
	if w.Code != http.StatusForbidden {
		t.Errorf("GET /replication/log on standalone: got %d, want 403", w.Code)
	}
}

func TestHandleReplicationLog_WrongMethod(t *testing.T) {
	primary := newPrimaryServer(t)
	w := doRequest(t, primary, http.MethodPost, "/replication/log", "")
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("got %d, want 405", w.Code)
	}
}

// --- POST /replication/apply ---

func TestHandleReplicationApply_Follower_AppliesEntries(t *testing.T) {
	follower := newFollowerServer(t, "http://127.0.0.1:9999")

	entries := []replnet.Entry{
		{Seq: 1, Op: replnet.OpPut, Key: "applied-key", Value: "applied-val"},
	}
	body, _ := json.Marshal(map[string]any{"entries": entries})

	w := doRequest(t, follower, http.MethodPost, "/replication/apply", string(body))
	if w.Code != http.StatusOK {
		t.Fatalf("POST /replication/apply on follower: got %d (body=%s)", w.Code, w.Body.String())
	}

	// Verify key was applied to engine.
	wGet := doRequest(t, follower, http.MethodGet, "/kv/applied-key", "")
	var getResp map[string]any
	decodeJSON(t, wGet, &getResp)
	if getResp["value"] != "applied-val" {
		t.Errorf("applied key not readable on follower: %v", getResp)
	}
}

func TestHandleReplicationApply_Primary_Returns403(t *testing.T) {
	primary := newPrimaryServer(t)

	entries := []replnet.Entry{
		{Seq: 1, Op: replnet.OpPut, Key: "should-not-apply", Value: "v"},
	}
	body, _ := json.Marshal(map[string]any{"entries": entries})

	w := doRequest(t, primary, http.MethodPost, "/replication/apply", string(body))
	if w.Code != http.StatusForbidden {
		t.Errorf("POST /replication/apply on primary: got %d, want 403", w.Code)
	}

	// Verify key was NOT written to primary engine.
	wGet := doRequest(t, primary, http.MethodGet, "/kv/should-not-apply", "")
	var getResp map[string]any
	decodeJSON(t, wGet, &getResp)
	if getResp["found"] == true {
		t.Error("primary must not be mutated by /replication/apply")
	}
}

func TestHandleReplicationApply_Standalone_Returns403(t *testing.T) {
	s := newTestServer(t, "n1")

	entries := []replnet.Entry{
		{Seq: 1, Op: replnet.OpPut, Key: "should-not-apply", Value: "v"},
	}
	body, _ := json.Marshal(map[string]any{"entries": entries})

	w := doRequest(t, s, http.MethodPost, "/replication/apply", string(body))
	if w.Code != http.StatusForbidden {
		t.Errorf("POST /replication/apply on standalone: got %d, want 403", w.Code)
	}

	// Verify key was NOT written to standalone engine.
	wGet := doRequest(t, s, http.MethodGet, "/kv/should-not-apply", "")
	var getResp map[string]any
	decodeJSON(t, wGet, &getResp)
	if getResp["found"] == true {
		t.Error("standalone node must not be mutated by /replication/apply")
	}
}

func TestHandleReplicationApply_Follower_InvalidJSON(t *testing.T) {
	follower := newFollowerServer(t, "http://127.0.0.1:9999")
	w := doRequest(t, follower, http.MethodPost, "/replication/apply", "not-json")
	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", w.Code)
	}
}

func TestHandleReplicationApply_WrongMethod(t *testing.T) {
	follower := newFollowerServer(t, "http://127.0.0.1:9999")
	w := doRequest(t, follower, http.MethodGet, "/replication/apply", "")
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("got %d, want 405", w.Code)
	}
}

// --- POST /replication/sync ---

func TestHandleReplicationSync_StandaloneNode_ReturnsBadRequest(t *testing.T) {
	s := newTestServer(t, "n1")
	w := doRequest(t, s, http.MethodPost, "/replication/sync", "")
	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400 for standalone", w.Code)
	}
}

func TestHandleReplicationSync_PrimaryNode_ReturnsBadRequest(t *testing.T) {
	primary := newPrimaryServer(t)
	w := doRequest(t, primary, http.MethodPost, "/replication/sync", "")
	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400 for primary", w.Code)
	}
}

func TestHandleReplicationSync_FollowerWithLivePrimary(t *testing.T) {
	// Start the primary as a real HTTP server.
	primary := newPrimaryServer(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}

	// Write some data on the primary.
	doRequest(t, primary, http.MethodPut, "/kv/sync-key", `{"value":"sync-val"}`)

	// Start the follower pointing at the primary.
	follower := newFollowerServer(t, "http://"+primary.Addr())

	// Sync.
	w := doRequest(t, follower, http.MethodPost, "/replication/sync", "")
	if w.Code != http.StatusOK {
		t.Fatalf("POST /replication/sync: got %d (body=%s)", w.Code, w.Body.String())
	}

	// Verify synced key is readable on the follower.
	wGet := doRequest(t, follower, http.MethodGet, "/kv/sync-key", "")
	var getResp map[string]any
	decodeJSON(t, wGet, &getResp)
	if getResp["value"] != "sync-val" {
		t.Errorf("synced key not readable: %v", getResp)
	}
}

func TestHandleReplicationSync_WrongMethod(t *testing.T) {
	follower := newFollowerServer(t, "http://127.0.0.1:9999")
	w := doRequest(t, follower, http.MethodGet, "/replication/sync", "")
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("got %d, want 405", w.Code)
	}
}

// --- ApplyReplicationEntries ---

func TestApplyReplicationEntries_SkipsAlreadyApplied(t *testing.T) {
	s := newTestServer(t, "n1")

	entries := []replnet.Entry{
		{Seq: 1, Op: replnet.OpPut, Key: "k1", Value: "v1"},
		{Seq: 2, Op: replnet.OpPut, Key: "k2", Value: "v2"},
	}
	s.ApplyReplicationEntries(entries)

	// Apply same entries again — should not fail.
	last, err := s.ApplyReplicationEntries(entries)
	if err != nil {
		t.Fatalf("expected nil for already-applied entries, got %v", err)
	}
	if last != 2 {
		t.Errorf("last = %d, want 2", last)
	}
}

func TestApplyReplicationEntries_OutOfOrder_ReturnsError(t *testing.T) {
	s := newTestServer(t, "n1")

	// Apply seq 1 first.
	s.ApplyReplicationEntries([]replnet.Entry{
		{Seq: 1, Op: replnet.OpPut, Key: "k1", Value: "v1"},
	})

	// Now try to apply seq 3 (gap at 2).
	_, err := s.ApplyReplicationEntries([]replnet.Entry{
		{Seq: 3, Op: replnet.OpPut, Key: "k3", Value: "v3"},
	})
	if err == nil {
		t.Fatal("expected error for out-of-order seq, got nil")
	}
}

// --- SyncFromPrimary ---

func TestSyncFromPrimary_NonFollower_ReturnsError(t *testing.T) {
	s := newTestServer(t, "n1")
	_, err := s.SyncFromPrimary(context.Background())
	if err == nil {
		t.Fatal("expected error for non-follower, got nil")
	}
}

// --- ReplicationStatus ---

func TestReplicationStatus_Standalone_EmptyStatus(t *testing.T) {
	s := newTestServer(t, "n1")
	st := s.ReplicationStatus()
	if st.Role != "" {
		t.Errorf("standalone role = %q, want empty", st.Role)
	}
}

func TestReplicationStatus_Primary_HasRole(t *testing.T) {
	primary := newPrimaryServer(t)
	st := primary.ReplicationStatus()
	if st.Role != replnet.RolePrimary {
		t.Errorf("role = %q, want %q", st.Role, replnet.RolePrimary)
	}
}

func TestReplicationStatus_Follower_HasPrimaryURL(t *testing.T) {
	follower := newFollowerServer(t, "http://127.0.0.1:9999")
	st := follower.ReplicationStatus()
	if st.Role != replnet.RoleFollower {
		t.Errorf("role = %q, want %q", st.Role, replnet.RoleFollower)
	}
	if st.PrimaryBaseURL != "http://127.0.0.1:9999" {
		t.Errorf("PrimaryBaseURL = %q, want http://127.0.0.1:9999", st.PrimaryBaseURL)
	}
}

// --- Role validation in Options.validate ---

func TestOpen_UnknownReplicationRole_ReturnsError(t *testing.T) {
	_, err := Open(Options{
		NodeID:  "n1",
		Addr:    "127.0.0.1:0",
		DataDir: t.TempDir(),
		Replication: ReplicationOptions{
			Role: replnet.Role("leader"), // unknown role
		},
	})
	if err == nil {
		t.Fatal("expected error for unknown replication role")
	}
}

func TestOpen_FollowerWithoutPrimaryURL_ReturnsError(t *testing.T) {
	_, err := Open(Options{
		NodeID:  "n1",
		Addr:    "127.0.0.1:0",
		DataDir: t.TempDir(),
		Replication: ReplicationOptions{
			Role:           replnet.RoleFollower,
			PrimaryBaseURL: "", // missing
		},
	})
	if err == nil {
		t.Fatal("expected error for follower without PrimaryBaseURL")
	}
}

func TestOpen_FollowerWithPrimaryURL_Valid(t *testing.T) {
	s, err := Open(Options{
		NodeID:  "n1",
		Addr:    "127.0.0.1:0",
		DataDir: t.TempDir(),
		Replication: ReplicationOptions{
			Role:           replnet.RoleFollower,
			PrimaryBaseURL: "http://127.0.0.1:9999",
		},
	})
	if err != nil {
		t.Fatalf("expected valid follower options, got %v", err)
	}
	_ = s.Close()
}

func TestOpen_PrimaryRole_Valid(t *testing.T) {
	s, err := Open(Options{
		NodeID:  "n1",
		Addr:    "127.0.0.1:0",
		DataDir: t.TempDir(),
		Replication: ReplicationOptions{
			Role: replnet.RolePrimary,
		},
	})
	if err != nil {
		t.Fatalf("expected valid primary options, got %v", err)
	}
	_ = s.Close()
}

func TestOpen_EmptyRole_Standalone_Valid(t *testing.T) {
	s, err := Open(Options{
		NodeID:  "n1",
		Addr:    "127.0.0.1:0",
		DataDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("expected valid standalone options, got %v", err)
	}
	_ = s.Close()
}
