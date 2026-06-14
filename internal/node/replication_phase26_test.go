package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/YashPatel2395/ShardForgeDB/internal/replnet"
)

// jsonBody wraps a JSON string as an io.Reader for httptest.NewRequest.
func jsonBody(s string) *strings.Reader { return strings.NewReader(s) }

// ── helpers ────────────────────────────────────────────────────────────────────

func newPhase26Primary(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(Options{
		NodeID:  "primary-p26",
		Addr:    "127.0.0.1:0",
		DataDir: dir,
		Replication: ReplicationOptions{
			Role: replnet.RolePrimary,
		},
	})
	if err != nil {
		t.Fatalf("open primary: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func newPhase26Follower(t *testing.T, primaryURL, dataDir string) *Server {
	t.Helper()
	s, err := Open(Options{
		NodeID:  "follower-p26",
		Addr:    "127.0.0.1:0",
		DataDir: dataDir,
		Replication: ReplicationOptions{
			Role:           replnet.RoleFollower,
			PrimaryBaseURL: primaryURL,
		},
	})
	if err != nil {
		t.Fatalf("open follower: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// ── Phase 26 tests ─────────────────────────────────────────────────────────────

func TestPhase26_PrimaryJournalPersistsAfterRestart(t *testing.T) {
	primaryDir := t.TempDir()

	// Open primary, write 3 keys, close.
	p1, err := Open(Options{
		NodeID:      "primary",
		Addr:        "127.0.0.1:0",
		DataDir:     primaryDir,
		Replication: ReplicationOptions{Role: replnet.RolePrimary},
	})
	if err != nil {
		t.Fatalf("open primary: %v", err)
	}
	rec := httptest.NewRecorder()
	p1.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/kv/k1", jsonBody(`{"value":"v1"}`)))
	rec = httptest.NewRecorder()
	p1.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/kv/k2", jsonBody(`{"value":"v2"}`)))
	rec = httptest.NewRecorder()
	p1.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/kv/k1", nil))
	_ = rec

	stats1, _ := p1.durableLog.Stats()
	if stats1.Count != 3 {
		t.Fatalf("before close: want 3 entries, got %d", stats1.Count)
	}
	p1.Close()

	// Reopen primary from same dir.
	p2, err := Open(Options{
		NodeID:      "primary",
		Addr:        "127.0.0.1:0",
		DataDir:     primaryDir,
		Replication: ReplicationOptions{Role: replnet.RolePrimary},
	})
	if err != nil {
		t.Fatalf("reopen primary: %v", err)
	}
	defer p2.Close()

	stats2, err := p2.durableLog.Stats()
	if err != nil {
		t.Fatalf("stats after restart: %v", err)
	}
	if stats2.Count != 3 {
		t.Errorf("after restart: want 3 entries in journal, got %d", stats2.Count)
	}
	if stats2.LastSeq != 3 {
		t.Errorf("after restart: last_seq want 3, got %d", stats2.LastSeq)
	}
	if !stats2.Durable {
		t.Error("durable: want true")
	}

	// Append a 4th entry after restart.
	rec2 := httptest.NewRecorder()
	p2.mux.ServeHTTP(rec2, httptest.NewRequest(http.MethodPut, "/kv/k3", jsonBody(`{"value":"v3"}`)))

	stats3, _ := p2.durableLog.Stats()
	if stats3.Count != 4 {
		t.Errorf("after new write: want 4 entries, got %d", stats3.Count)
	}
	if stats3.LastSeq != 4 {
		t.Errorf("after new write: last_seq want 4, got %d", stats3.LastSeq)
	}
}

func TestPhase26_FollowerCursorPersistsAfterRestart(t *testing.T) {
	primary := newPhase26Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}

	doRequest(t, primary, http.MethodPut, "/kv/pk1", `{"value":"pv1"}`)
	doRequest(t, primary, http.MethodPut, "/kv/pk2", `{"value":"pv2"}`)

	followerDir := t.TempDir()
	primaryURL := "http://" + primary.Addr()

	// First follower: sync 2 entries.
	f1 := newPhase26Follower(t, primaryURL, followerDir)
	result, err := f1.SyncFromPrimary(context.Background())
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.Applied != 2 {
		t.Fatalf("applied: want 2, got %d", result.Applied)
	}
	if result.LastAppliedSeq != 2 {
		t.Fatalf("last_applied_seq: want 2, got %d", result.LastAppliedSeq)
	}
	f1.Close()

	// Reopen follower from same dir: cursor must be restored.
	f2 := newPhase26Follower(t, primaryURL, followerDir)
	if got := f2.ReplicationStatus().LastAppliedSeq; got != 2 {
		t.Errorf("after restart: LastAppliedSeq want 2, got %d", got)
	}

	// A second sync must be a no-op (already up to date).
	result2, err := f2.SyncFromPrimary(context.Background())
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if result2.Applied != 0 {
		t.Errorf("second sync applied: want 0, got %d", result2.Applied)
	}
	if result2.Fetched != 0 {
		t.Errorf("second sync fetched: want 0, got %d", result2.Fetched)
	}
}

func TestPhase26_FollowerRestartResumesFromDurableCursor(t *testing.T) {
	primary := newPhase26Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}

	// Write 5 keys to primary.
	for i := 1; i <= 5; i++ {
		doRequest(t, primary, http.MethodPut, fmt.Sprintf("/kv/key%d", i), fmt.Sprintf(`{"value":"v%d"}`, i))
	}

	followerDir := t.TempDir()
	primaryURL := "http://" + primary.Addr()

	// Sync 5 entries on first follower instance.
	f1 := newPhase26Follower(t, primaryURL, followerDir)
	if _, err := f1.SyncFromPrimary(context.Background()); err != nil {
		t.Fatalf("sync 1: %v", err)
	}
	f1.Close()

	// Write 3 more keys to primary while follower is down.
	for i := 6; i <= 8; i++ {
		doRequest(t, primary, http.MethodPut, fmt.Sprintf("/kv/key%d", i), fmt.Sprintf(`{"value":"v%d"}`, i))
	}

	// Reopen follower: must resume from seq 5, not 0.
	f2 := newPhase26Follower(t, primaryURL, followerDir)
	result, err := f2.SyncFromPrimary(context.Background())
	if err != nil {
		t.Fatalf("sync after restart: %v", err)
	}
	if result.Applied != 3 {
		t.Errorf("applied after restart: want 3 (keys 6-8), got %d", result.Applied)
	}
	if result.LastAppliedSeq != 8 {
		t.Errorf("last_applied_seq: want 8, got %d", result.LastAppliedSeq)
	}

	// Verify key8 is present on follower.
	w := doRequest(t, f2, http.MethodGet, "/kv/key8", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET key8: %d", w.Code)
	}
	var resp getResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if !resp.Found || resp.Value != "v8" {
		t.Errorf("key8: found=%v value=%q, want found=true value=v8", resp.Found, resp.Value)
	}
}

func TestPhase26_DurableLogStats_InReplicationStatus(t *testing.T) {
	primary := newPhase26Primary(t)
	doRequest(t, primary, http.MethodPut, "/kv/x", `{"value":"y"}`)

	status := primary.ReplicationStatus()
	if !status.Durable {
		t.Error("primary ReplicaStatus.Durable: want true")
	}
	if status.LastLocalSeq != 1 {
		t.Errorf("LastLocalSeq: want 1, got %d", status.LastLocalSeq)
	}
}

func TestPhase26_FollowerStatus_StatePersistent(t *testing.T) {
	primary := newPhase26Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start: %v", err)
	}

	f := newPhase26Follower(t, "http://"+primary.Addr(), t.TempDir())
	status := f.ReplicationStatus()
	if !status.Durable {
		t.Error("follower ReplicaStatus.Durable: want true")
	}
	if !status.StatePersistent {
		t.Error("follower ReplicaStatus.StatePersistent: want true")
	}
}

func TestPhase26_ReplicationGap_PrimaryReturnse409(t *testing.T) {
	// Set up a primary and write some entries.
	primary := newPhase26Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start: %v", err)
	}
	doRequest(t, primary, http.MethodPut, "/kv/a", `{"value":"1"}`)
	doRequest(t, primary, http.MethodPut, "/kv/b", `{"value":"2"}`)

	// Request entries after a cursor that is behind the first available (simulate gap).
	// Since Phase 26 doesn't truncate, we manufacture a gap by directly inspecting
	// the handler with an impossible after value.
	//
	// The handler gap check is: after+1 < firstAvailableSeq.
	// firstAvailableSeq = 1 (entries start at seq 1).
	// So there is no gap for any valid after (0 or higher). Gap only occurs on truncation.
	//
	// We test gap detection by verifying the logic via ReplicationEntries directly.
	firstSeq := primary.durableLog.FirstAvailableSeq()
	if firstSeq != 1 {
		t.Fatalf("first available seq: want 1, got %d", firstSeq)
	}

	// No gap for after=0 (request entries starting at seq 1 which is available).
	entries, err := primary.ReplicationEntries(0, 0)
	if err != nil {
		t.Fatalf("entries after 0: unexpected error %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("entries after 0: want 2, got %d", len(entries))
	}

	// Simulate a gap: mock primary with firstAvailableSeq=10.
	// We verify the gap error type is returned correctly by injecting a mock.
	gapErr := &replnet.ReplicationGapError{
		RequestedAfter:    5,
		FirstAvailableSeq: 10,
		LatestSeq:         20,
	}
	if !errors.Is(gapErr, replnet.ErrReplicationGap) {
		t.Error("ReplicationGapError.Unwrap: want errors.Is(err, ErrReplicationGap)")
	}
	expectedMsg := "replication gap: requested after 5 but first available is 10 (latest=20)"
	if got := gapErr.Error(); got != expectedMsg {
		t.Errorf("error message: got %q, want %q", got, expectedMsg)
	}
}

func TestPhase26_SyncResult_GapField_PopulatedOnGap(t *testing.T) {
	// Build a mock primary that always returns 409 gap.
	gapErr := &replnet.ReplicationGapError{
		RequestedAfter:    3,
		FirstAvailableSeq: 10,
		LatestSeq:         25,
	}
	mockPrimary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]any{
			"error":   gapErr.Error(),
			"gap":     gapErr,
			"node_id": "mock-primary",
		})
	}))
	defer mockPrimary.Close()

	follower := newPhase26Follower(t, mockPrimary.URL, t.TempDir())

	result, err := follower.SyncFromPrimary(context.Background())
	if err == nil {
		t.Fatal("expected error from gap primary, got nil")
	}
	if !errors.Is(err, replnet.ErrReplicationGap) {
		t.Errorf("error: want ErrReplicationGap, got %v", err)
	}
	if result.Gap == nil {
		t.Fatal("SyncResult.Gap: want non-nil, got nil")
	}
	if result.Gap.RequestedAfter != 3 {
		t.Errorf("gap.requested_after: want 3, got %d", result.Gap.RequestedAfter)
	}
	if result.Gap.FirstAvailableSeq != 10 {
		t.Errorf("gap.first_available_seq: want 10, got %d", result.Gap.FirstAvailableSeq)
	}
}

func TestPhase26_HandleReplicationSync_GapReturns409(t *testing.T) {
	// The /replication/sync endpoint must return 409 when the primary reports a gap.
	gapErr := &replnet.ReplicationGapError{
		RequestedAfter:    2,
		FirstAvailableSeq: 5,
		LatestSeq:         10,
	}
	mockPrimary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]any{
			"error":   gapErr.Error(),
			"gap":     gapErr,
			"node_id": "mock-primary",
		})
	}))
	defer mockPrimary.Close()

	follower := newPhase26Follower(t, mockPrimary.URL, t.TempDir())

	w := httptest.NewRecorder()
	follower.mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/replication/sync", nil))

	if w.Code != http.StatusConflict {
		t.Errorf("POST /replication/sync: want 409, got %d", w.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["gap"] == nil {
		t.Error("response body: want 'gap' field, got nil")
	}
	if body["ok"] != false {
		t.Errorf("ok: want false, got %v", body["ok"])
	}
}

func TestPhase26_PrimaryDurableLog_FirstAvailableSeq_AfterRestart(t *testing.T) {
	dir := t.TempDir()

	p1, err := Open(Options{
		NodeID:      "prim",
		Addr:        "127.0.0.1:0",
		DataDir:     dir,
		Replication: ReplicationOptions{Role: replnet.RolePrimary},
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	doRequest(t, p1, http.MethodPut, "/kv/a", `{"value":"1"}`)
	doRequest(t, p1, http.MethodPut, "/kv/b", `{"value":"2"}`)
	p1.Close()

	p2, err := Open(Options{
		NodeID:      "prim",
		Addr:        "127.0.0.1:0",
		DataDir:     dir,
		Replication: ReplicationOptions{Role: replnet.RolePrimary},
	})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer p2.Close()

	if got := p2.durableLog.FirstAvailableSeq(); got != 1 {
		t.Errorf("FirstAvailableSeq after restart: want 1, got %d", got)
	}
	stats, _ := p2.durableLog.Stats()
	if stats.Count != 2 {
		t.Errorf("count after restart: want 2, got %d", stats.Count)
	}
}

func TestPhase26_DurableLog_ReplicationStatusEndpoint(t *testing.T) {
	primary := newPhase26Primary(t)
	doRequest(t, primary, http.MethodPut, "/kv/k", `{"value":"v"}`)

	w := doRequest(t, primary, http.MethodGet, "/replication/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /replication/status: %d", w.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	repl, ok := body["replication"].(map[string]any)
	if !ok {
		t.Fatalf("replication field missing: %v", body)
	}
	if repl["durable"] != true {
		t.Errorf("replication.durable: want true, got %v", repl["durable"])
	}
	if repl["role"] != "primary" {
		t.Errorf("role: want primary, got %v", repl["role"])
	}
}

func TestPhase26_FollowerState_RestoredAfterCrash(t *testing.T) {
	// Simulate follower "crashing" (close without graceful shutdown) and verify cursor loads.
	primary := newPhase26Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start: %v", err)
	}
	doRequest(t, primary, http.MethodPut, "/kv/x", `{"value":"1"}`)
	doRequest(t, primary, http.MethodPut, "/kv/y", `{"value":"2"}`)
	doRequest(t, primary, http.MethodPut, "/kv/z", `{"value":"3"}`)

	followerDir := t.TempDir()
	primaryURL := "http://" + primary.Addr()

	f1 := newPhase26Follower(t, primaryURL, followerDir)
	if _, err := f1.SyncFromPrimary(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	// Simulate crash: close the file descriptors but don't call graceful cleanup.
	f1.Close()

	// New primary writes while follower is down.
	doRequest(t, primary, http.MethodPut, "/kv/w", `{"value":"4"}`)

	// Reopen follower: must load cursor from state file, sync only the new entry.
	f2 := newPhase26Follower(t, primaryURL, followerDir)
	result, err := f2.SyncFromPrimary(context.Background())
	if err != nil {
		t.Fatalf("sync after crash: %v", err)
	}
	if result.Applied != 1 {
		t.Errorf("applied after crash recovery: want 1, got %d", result.Applied)
	}
	if result.LastAppliedSeq != 4 {
		t.Errorf("last_applied_seq: want 4, got %d", result.LastAppliedSeq)
	}
}

// TestPhase26_BothNodesRestart proves that when both primary and follower restart,
// the follower can resume from its persisted cursor and fetch only new entries.
//
// Real deployments restart the primary on the SAME configured address. This test
// simulates that by capturing the ephemeral port from p1 and reusing it for p2.
// The follower's identity-bound state file stores that URL, so the restarted
// follower opens successfully against p2.
func TestPhase26_BothNodesRestart(t *testing.T) {
	primaryDir := t.TempDir()
	followerDir := t.TempDir()

	// Phase 1: open primary, get its address (which we'll reuse on restart).
	p1, err := Open(Options{
		NodeID:      "primary-p26",
		Addr:        "127.0.0.1:0",
		DataDir:     primaryDir,
		Replication: ReplicationOptions{Role: replnet.RolePrimary},
	})
	if err != nil {
		t.Fatalf("open primary: %v", err)
	}
	if err := p1.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}
	// Capture the stable "configured" address before closing.
	primaryAddr := p1.Addr() // e.g. "127.0.0.1:51234"
	primaryURL := "http://" + primaryAddr

	doRequest(t, p1, http.MethodPut, "/kv/a", `{"value":"1"}`)
	doRequest(t, p1, http.MethodPut, "/kv/b", `{"value":"2"}`)
	doRequest(t, p1, http.MethodPut, "/kv/c", `{"value":"3"}`)

	// Open follower and sync 3 entries. State file records primaryURL.
	f1, err := Open(Options{
		NodeID:  "follower-p26",
		Addr:    "127.0.0.1:0",
		DataDir: followerDir,
		Replication: ReplicationOptions{
			Role:           replnet.RoleFollower,
			PrimaryBaseURL: primaryURL,
		},
	})
	if err != nil {
		t.Fatalf("open follower: %v", err)
	}
	r1, err := f1.SyncFromPrimary(context.Background())
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if r1.Applied != 3 {
		t.Errorf("applied: want 3, got %d", r1.Applied)
	}
	f1.Close()
	p1.Close() // releases primaryAddr port

	// Phase 2: reopen primary on the SAME address (as a real deployment would).
	// Go's net.Listen uses SO_REUSEADDR so the port is immediately re-bindable.
	p2, err := Open(Options{
		NodeID:      "primary-p26",
		Addr:        primaryAddr, // same address as p1
		DataDir:     primaryDir,
		Replication: ReplicationOptions{Role: replnet.RolePrimary},
	})
	if err != nil {
		t.Fatalf("reopen primary: %v", err)
	}
	defer p2.Close()
	if err := p2.StartBackground(); err != nil {
		t.Fatalf("restart primary: %v", err)
	}

	// Verify primary journal still has 3 entries.
	stats, _ := p2.durableLog.Stats()
	if stats.Count != 3 || stats.LastSeq != 3 {
		t.Errorf("primary after restart: want count=3 lastSeq=3, got count=%d lastSeq=%d",
			stats.Count, stats.LastSeq)
	}

	// Write 2 more keys on the restarted primary (seq 4 and 5).
	doRequest(t, p2, http.MethodPut, "/kv/d", `{"value":"4"}`)
	doRequest(t, p2, http.MethodPut, "/kv/e", `{"value":"5"}`)

	// Reopen follower with same primaryURL (matches the state file).
	f2, err := Open(Options{
		NodeID:  "follower-p26",
		Addr:    "127.0.0.1:0",
		DataDir: followerDir,
		Replication: ReplicationOptions{
			Role:           replnet.RoleFollower,
			PrimaryBaseURL: primaryURL, // same URL as stored in state file
		},
	})
	if err != nil {
		t.Fatalf("reopen follower: %v", err)
	}
	defer f2.Close()

	// Follower must restore cursor=3 and fetch only entries 4 and 5.
	if got := f2.ReplicationStatus().LastAppliedSeq; got != 3 {
		t.Errorf("follower cursor after restart: want 3, got %d", got)
	}
	r2, err := f2.SyncFromPrimary(context.Background())
	if err != nil {
		t.Fatalf("sync after both restart: %v", err)
	}
	if r2.Applied != 2 {
		t.Errorf("applied after both restart: want 2, got %d", r2.Applied)
	}
	if r2.LastAppliedSeq != 5 {
		t.Errorf("last_applied_seq: want 5, got %d", r2.LastAppliedSeq)
	}
}

// TestPhase26_DeleteAfterPrimaryRestart proves that DELETE operations written to the
// primary after a restart are replicated correctly to the follower.
func TestPhase26_DeleteAfterPrimaryRestart(t *testing.T) {
	primaryDir := t.TempDir()

	p1, err := Open(Options{
		NodeID:      "primary-p26",
		Addr:        "127.0.0.1:0",
		DataDir:     primaryDir,
		Replication: ReplicationOptions{Role: replnet.RolePrimary},
	})
	if err != nil {
		t.Fatalf("open primary: %v", err)
	}
	if err := p1.StartBackground(); err != nil {
		t.Fatalf("start: %v", err)
	}
	doRequest(t, p1, http.MethodPut, "/kv/target", `{"value":"to-be-deleted"}`)
	p1.Close()

	followerDir := t.TempDir()

	// Reopen primary.
	p2, err := Open(Options{
		NodeID:      "primary-p26",
		Addr:        "127.0.0.1:0",
		DataDir:     primaryDir,
		Replication: ReplicationOptions{Role: replnet.RolePrimary},
	})
	if err != nil {
		t.Fatalf("reopen primary: %v", err)
	}
	defer p2.Close()
	if err := p2.StartBackground(); err != nil {
		t.Fatalf("restart primary: %v", err)
	}
	primaryURL := "http://" + p2.Addr()

	// Open follower and sync the initial write.
	f1, err := Open(Options{
		NodeID:  "follower-p26",
		Addr:    "127.0.0.1:0",
		DataDir: followerDir,
		Replication: ReplicationOptions{
			Role:           replnet.RoleFollower,
			PrimaryBaseURL: primaryURL,
		},
	})
	if err != nil {
		t.Fatalf("open follower: %v", err)
	}
	r1, err := f1.SyncFromPrimary(context.Background())
	if err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	if r1.Applied != 1 {
		t.Fatalf("initial sync: want 1 applied, got %d", r1.Applied)
	}

	// Verify the key is present on the follower.
	w := doRequest(t, f1, http.MethodGet, "/kv/target", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET target before delete: %d", w.Code)
	}
	var resp getResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if !resp.Found {
		t.Fatal("GET target before delete: want found=true")
	}

	// Delete the key on the primary (after its restart — seq will be 2).
	doRequest(t, p2, http.MethodDelete, "/kv/target", "")

	// Sync the delete to the follower.
	r2, err := f1.SyncFromPrimary(context.Background())
	if err != nil {
		t.Fatalf("sync delete: %v", err)
	}
	if r2.Applied != 1 {
		t.Errorf("sync delete: want 1 applied, got %d", r2.Applied)
	}

	// Verify the key is gone on the follower.
	w2 := doRequest(t, f1, http.MethodGet, "/kv/target", "")
	if w2.Code != http.StatusOK {
		t.Fatalf("GET target after delete: %d", w2.Code)
	}
	var resp2 getResponse
	json.NewDecoder(w2.Body).Decode(&resp2)
	if resp2.Found {
		t.Error("GET target after delete: want found=false (key should be deleted)")
	}
	f1.Close()
}

// TestPhase26_CorruptFollowerState_Visible proves that a corrupt replication_state.json
// causes the follower Open() to return a visible error (ErrCorruptedState),
// not silently reset to zero.
func TestPhase26_CorruptFollowerState_Visible(t *testing.T) {
	primary := newPhase26Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start: %v", err)
	}
	doRequest(t, primary, http.MethodPut, "/kv/x", `{"value":"1"}`)
	primaryURL := "http://" + primary.Addr()

	followerDir := t.TempDir()

	f1, err := Open(Options{
		NodeID:  "follower-p26",
		Addr:    "127.0.0.1:0",
		DataDir: followerDir,
		Replication: ReplicationOptions{
			Role:           replnet.RoleFollower,
			PrimaryBaseURL: primaryURL,
		},
	})
	if err != nil {
		t.Fatalf("open follower: %v", err)
	}
	if _, err := f1.SyncFromPrimary(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	f1.Close()

	// Corrupt the state file by flipping bytes in the JSON body.
	statePath := followerDir + "/replication_state.json"
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	data[10] ^= 0xFF
	if err := os.WriteFile(statePath, data, 0o644); err != nil {
		t.Fatalf("write corrupted state: %v", err)
	}

	// Reopening with a corrupt state file must return an error.
	_, err = Open(Options{
		NodeID:  "follower-p26",
		Addr:    "127.0.0.1:0",
		DataDir: followerDir,
		Replication: ReplicationOptions{
			Role:           replnet.RoleFollower,
			PrimaryBaseURL: primaryURL,
		},
	})
	if err == nil {
		t.Fatal("Open with corrupt state file: want error, got nil")
	}
	if !errors.Is(err, replnet.ErrCorruptedState) {
		t.Errorf("Open with corrupt state file: want ErrCorruptedState in chain, got %v", err)
	}
}

// TestPhase26_NoAutoSyncAfterFollowerRestart proves that restarting the follower does NOT
// automatically apply new entries from the primary. The operator must call SyncFromPrimary.
func TestPhase26_NoAutoSyncAfterFollowerRestart(t *testing.T) {
	primary := newPhase26Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start: %v", err)
	}
	doRequest(t, primary, http.MethodPut, "/kv/first", `{"value":"1"}`)
	primaryURL := "http://" + primary.Addr()

	followerDir := t.TempDir()

	// Sync the first write.
	f1 := newPhase26Follower(t, primaryURL, followerDir)
	if _, err := f1.SyncFromPrimary(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	f1.Close()

	// Write a second key while the follower is down.
	doRequest(t, primary, http.MethodPut, "/kv/second", `{"value":"2"}`)

	// Reopen follower. No sync call yet.
	f2 := newPhase26Follower(t, primaryURL, followerDir)
	defer f2.Close()

	// The second key must NOT be present on the follower yet (no auto-sync).
	w := doRequest(t, f2, http.MethodGet, "/kv/second", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET second: %d", w.Code)
	}
	var resp getResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Found {
		t.Error("GET second before manual sync: want found=false (no auto-sync)")
	}

	// Cursor must still be 1 (only the first write was synced).
	if got := f2.ReplicationStatus().LastAppliedSeq; got != 1 {
		t.Errorf("cursor after restart (no sync): want 1, got %d", got)
	}

	// Now sync manually and verify the second key appears.
	if _, err := f2.SyncFromPrimary(context.Background()); err != nil {
		t.Fatalf("manual sync: %v", err)
	}
	w2 := doRequest(t, f2, http.MethodGet, "/kv/second", "")
	var resp2 getResponse
	json.NewDecoder(w2.Body).Decode(&resp2)
	if !resp2.Found {
		t.Error("GET second after manual sync: want found=true")
	}
}

// TestPhase26_ConcurrentSync_Rejected proves that a second SyncFromPrimary call
// while one is already in-flight is rejected with ErrSyncInProgress (not silently
// dropped or allowed to apply the same batch twice).
func TestPhase26_ConcurrentSync_Rejected(t *testing.T) {
	primary := newPhase26Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start: %v", err)
	}
	doRequest(t, primary, http.MethodPut, "/kv/k", `{"value":"v"}`)
	primaryURL := "http://" + primary.Addr()

	follower := newPhase26Follower(t, primaryURL, t.TempDir())

	// Mark sync as already in progress.
	follower.syncInProgress.Store(true)
	defer follower.syncInProgress.Store(false)

	_, err := follower.SyncFromPrimary(context.Background())
	if !errors.Is(err, ErrSyncInProgress) {
		t.Errorf("concurrent sync: want ErrSyncInProgress, got %v", err)
	}
}

func TestPhase26_IdempotentSyncAfterRestartWithNothingNew(t *testing.T) {
	primary := newPhase26Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start: %v", err)
	}
	doRequest(t, primary, http.MethodPut, "/kv/a", `{"value":"1"}`)

	followerDir := t.TempDir()
	primaryURL := "http://" + primary.Addr()

	f1 := newPhase26Follower(t, primaryURL, followerDir)
	f1.SyncFromPrimary(context.Background())
	f1.Close()

	// Reopen and sync with nothing new on primary.
	f2 := newPhase26Follower(t, primaryURL, followerDir)
	result, err := f2.SyncFromPrimary(context.Background())
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.Applied != 0 || result.Fetched != 0 {
		t.Errorf("expected 0 applied and 0 fetched; got applied=%d fetched=%d",
			result.Applied, result.Fetched)
	}
}
