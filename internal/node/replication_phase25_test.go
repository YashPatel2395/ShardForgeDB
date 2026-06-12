package node

// Phase 25 — Networked Pull-Based Replication Demo tests.
//
// Scope: explicit, operator-triggered pull-based replication between real HTTP node
// processes. No Raft, no consensus, no quorum, no automatic failover, no background sync.
//
// These tests extend the existing replication tests in replication_test.go with new
// scenarios required by Phase 25: DELETE replication, idempotency, unavailable primary,
// cursor advancement, SyncResult counts, distinct data dirs.
//
// Replication demo config tests live in internal/cluster/replication_demo_test.go
// to avoid the node→cluster→gateway→node import cycle.

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/YashPatel2395/ShardForgeDB/internal/replnet"
)

// --- Phase 25: DELETE replication via pull ---

func TestPhase25_FollowerSync_AppliesDelete(t *testing.T) {
	// Start primary as a real HTTP server.
	primary := newPrimaryServer(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}

	// Write a key then delete it on the primary.
	doRequest(t, primary, http.MethodPut, "/kv/delete-me", `{"value":"will-be-gone"}`)
	doRequest(t, primary, http.MethodDelete, "/kv/delete-me", "")

	// Verify primary log has 2 entries (put + delete).
	stats, err := primary.replLog.Stats()
	if err != nil {
		t.Fatalf("log stats: %v", err)
	}
	if stats.Count != 2 {
		t.Errorf("primary log count = %d, want 2 (put + delete)", stats.Count)
	}

	// Start follower.
	follower := newFollowerServer(t, "http://"+primary.Addr())

	// Sync: follower pulls both put and delete.
	w := doRequest(t, follower, http.MethodPost, "/replication/sync", "")
	if w.Code != http.StatusOK {
		t.Fatalf("POST /replication/sync: got %d (body=%s)", w.Code, w.Body.String())
	}

	// Key must be absent on follower (tombstone applied).
	wGet := doRequest(t, follower, http.MethodGet, "/kv/delete-me", "")
	var resp map[string]any
	decodeJSON(t, wGet, &resp)
	if resp["found"] == true {
		t.Errorf("follower has key after delete-pull: %v", resp)
	}
}

// --- Phase 25: Idempotent second pull ---

func TestPhase25_FollowerSync_SecondPullIsIdempotent(t *testing.T) {
	primary := newPrimaryServer(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}

	doRequest(t, primary, http.MethodPut, "/kv/idempotent-key", `{"value":"v1"}`)

	follower := newFollowerServer(t, "http://"+primary.Addr())

	// First pull.
	result1, err := follower.SyncFromPrimary(context.Background())
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if result1.Applied != 1 {
		t.Errorf("first sync applied = %d, want 1", result1.Applied)
	}
	if result1.Fetched != 1 {
		t.Errorf("first sync fetched = %d, want 1", result1.Fetched)
	}

	// Second pull — nothing new on primary.
	result2, err := follower.SyncFromPrimary(context.Background())
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if result2.Fetched != 0 {
		t.Errorf("second sync fetched = %d, want 0 (nothing new)", result2.Fetched)
	}
	if result2.Applied != 0 {
		t.Errorf("second sync applied = %d, want 0 (nothing new)", result2.Applied)
	}

	// Follower still has the key.
	wGet := doRequest(t, follower, http.MethodGet, "/kv/idempotent-key", "")
	var resp map[string]any
	decodeJSON(t, wGet, &resp)
	if resp["value"] != "v1" {
		t.Errorf("follower key value = %v, want v1", resp["value"])
	}
}

// --- Phase 25: Unavailable primary returns clear error ---

func TestPhase25_FollowerSync_UnavailablePrimary_ReturnsBadGateway(t *testing.T) {
	// Point follower at a port with no server.
	follower := newFollowerServer(t, "http://127.0.0.1:19999")

	// SyncFromPrimary must return an error.
	_, err := follower.SyncFromPrimary(context.Background())
	if err == nil {
		t.Fatal("expected error when primary unavailable, got nil")
	}
}

func TestPhase25_HandleReplicationSync_UnavailablePrimary_Returns502(t *testing.T) {
	follower := newFollowerServer(t, "http://127.0.0.1:19998")

	w := doRequest(t, follower, http.MethodPost, "/replication/sync", "")
	if w.Code != http.StatusBadGateway {
		t.Errorf("POST /replication/sync with dead primary: got %d, want 502", w.Code)
	}

	var resp map[string]any
	decodeJSON(t, w, &resp)
	if _, hasError := resp["error"]; !hasError {
		t.Errorf("expected error field in 502 response, got: %v", resp)
	}
	if resp["ok"] != false {
		t.Errorf("expected ok=false in 502 response, got: %v", resp["ok"])
	}
}

// --- Phase 25: Cursor advances after each sync ---

func TestPhase25_FollowerCursorAdvancesAfterSync(t *testing.T) {
	primary := newPrimaryServer(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}

	follower := newFollowerServer(t, "http://"+primary.Addr())

	// Initially cursor is at 0.
	if seq := follower.ReplicationStatus().LastAppliedSeq; seq != 0 {
		t.Errorf("initial LastAppliedSeq = %d, want 0", seq)
	}

	// Write 3 entries on primary.
	doRequest(t, primary, http.MethodPut, "/kv/k1", `{"value":"v1"}`)
	doRequest(t, primary, http.MethodPut, "/kv/k2", `{"value":"v2"}`)
	doRequest(t, primary, http.MethodPut, "/kv/k3", `{"value":"v3"}`)

	// First sync: cursor must advance to 3.
	result, err := follower.SyncFromPrimary(context.Background())
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.LastAppliedSeq != 3 {
		t.Errorf("LastAppliedSeq after sync = %d, want 3", result.LastAppliedSeq)
	}
	if follower.ReplicationStatus().LastAppliedSeq != 3 {
		t.Errorf("follower status LastAppliedSeq = %d, want 3", follower.ReplicationStatus().LastAppliedSeq)
	}

	// Write 2 more on primary.
	doRequest(t, primary, http.MethodPut, "/kv/k4", `{"value":"v4"}`)
	doRequest(t, primary, http.MethodDelete, "/kv/k1", "")

	// Second sync: cursor advances to 5.
	result2, err := follower.SyncFromPrimary(context.Background())
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if result2.LastAppliedSeq != 5 {
		t.Errorf("LastAppliedSeq after second sync = %d, want 5", result2.LastAppliedSeq)
	}
	if result2.Applied != 2 {
		t.Errorf("second sync applied = %d, want 2", result2.Applied)
	}
}

// --- Phase 25: SyncResult includes fetched and applied counts ---

func TestPhase25_SyncResult_IncludesFetchedAndAppliedCounts(t *testing.T) {
	primary := newPrimaryServer(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}

	doRequest(t, primary, http.MethodPut, "/kv/count-test", `{"value":"abc"}`)

	follower := newFollowerServer(t, "http://"+primary.Addr())

	// Sync via HTTP handler to check the JSON response shape.
	w := doRequest(t, follower, http.MethodPost, "/replication/sync", "")
	if w.Code != http.StatusOK {
		t.Fatalf("POST /replication/sync: got %d", w.Code)
	}

	var resp map[string]any
	decodeJSON(t, w, &resp)

	if resp["ok"] != true {
		t.Errorf("ok = %v, want true", resp["ok"])
	}
	if resp["fetched"].(float64) != 1 {
		t.Errorf("fetched = %v, want 1", resp["fetched"])
	}
	if resp["applied"].(float64) != 1 {
		t.Errorf("applied = %v, want 1", resp["applied"])
	}
	if resp["last_applied_seq"].(float64) != 1 {
		t.Errorf("last_applied_seq = %v, want 1", resp["last_applied_seq"])
	}
	if _, hasSourceNode := resp["source_node"]; !hasSourceNode {
		t.Errorf("source_node field missing from sync response")
	}
	if _, hasFollowerNode := resp["follower_node"]; !hasFollowerNode {
		t.Errorf("follower_node field missing from sync response")
	}
	if _, hasReplication := resp["replication"]; !hasReplication {
		t.Errorf("replication field missing from sync response")
	}
}

// --- Phase 25: Leader and follower use distinct data directories ---

func TestPhase25_LeaderAndFollower_DistinctDataDirs(t *testing.T) {
	leaderDir := t.TempDir()
	followerDir := t.TempDir()

	if leaderDir == followerDir {
		t.Fatal("leader and follower data dirs must be distinct")
	}

	leader, err := Open(Options{
		NodeID:  "leader",
		Addr:    "127.0.0.1:0",
		DataDir: leaderDir,
		Replication: ReplicationOptions{
			Role: replnet.RolePrimary,
		},
	})
	if err != nil {
		t.Fatalf("open leader: %v", err)
	}
	defer leader.Close()

	follower, err := Open(Options{
		NodeID:  "follower",
		Addr:    "127.0.0.1:0",
		DataDir: followerDir,
		Replication: ReplicationOptions{
			Role:           replnet.RoleFollower,
			PrimaryBaseURL: "http://127.0.0.1:9999",
		},
	})
	if err != nil {
		t.Fatalf("open follower: %v", err)
	}
	defer follower.Close()

	if leader.opts.DataDir == follower.opts.DataDir {
		t.Errorf("leader and follower share data dir %q — must be separate", leader.opts.DataDir)
	}

	// Write to leader; follower's dir must NOT contain the write.
	doRequest(t, leader, http.MethodPut, "/kv/isolation-test", `{"value":"leader-only"}`)

	// Follower dir must have no key-value data (different engine).
	wGet := doRequest(t, follower, http.MethodGet, "/kv/isolation-test", "")
	var resp map[string]any
	decodeJSON(t, wGet, &resp)
	if resp["found"] == true {
		t.Error("follower has leader's key without replication — data isolation broken")
	}
}

func TestPhase25_ReplDemoConfig_ReplicationCursorIsInMemory(t *testing.T) {
	// The replication cursor (lastApplied) is in-memory only.
	// Verify it resets to 0 after a new Server is opened.
	primary := newPrimaryServer(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}

	doRequest(t, primary, http.MethodPut, "/kv/cursor-test", `{"value":"v1"}`)

	followerDir := t.TempDir()
	follower1, err := Open(Options{
		NodeID:  "follower-1",
		Addr:    "127.0.0.1:0",
		DataDir: followerDir,
		Replication: ReplicationOptions{
			Role:           replnet.RoleFollower,
			PrimaryBaseURL: "http://" + primary.Addr(),
		},
	})
	if err != nil {
		t.Fatalf("open follower: %v", err)
	}

	// Sync once.
	result, err := follower1.SyncFromPrimary(context.Background())
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.LastAppliedSeq != 1 {
		t.Errorf("LastAppliedSeq = %d, want 1", result.LastAppliedSeq)
	}
	_ = follower1.Close()

	// Re-open follower from same dir. Cursor must reset to 0 (in-memory only).
	follower2, err := Open(Options{
		NodeID:  "follower-2",
		Addr:    "127.0.0.1:0",
		DataDir: followerDir,
		Replication: ReplicationOptions{
			Role:           replnet.RoleFollower,
			PrimaryBaseURL: fmt.Sprintf("http://%s", primary.Addr()),
		},
	})
	if err != nil {
		t.Fatalf("re-open follower: %v", err)
	}
	defer follower2.Close()

	// Cursor resets to 0 — this is the documented in-memory-only behavior.
	if seq := follower2.ReplicationStatus().LastAppliedSeq; seq != 0 {
		t.Errorf("LastAppliedSeq after re-open = %d, want 0 (in-memory cursor, not persistent)", seq)
	}
}
