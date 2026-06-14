package node

// Phase 27 — Automatic Background Pull Replication tests.
//
// Scope: background worker that periodically calls SyncFromPrimary automatically.
// No Raft, no consensus, no quorum, no automatic failover, no write forwarding.
// Replication is pull-based only; these tests prove automatic pull without
// POST /replication/sync.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/YashPatel2395/ShardForgeDB/internal/replnet"
)

// ── helpers ────────────────────────────────────────────────────────────────────

// bgIntegrationCfg returns a BackgroundSyncConfig suitable for fast integration tests.
// Short interval so automatic sync happens quickly in tests.
func bgIntegrationCfg() BackgroundSyncConfig {
	return BackgroundSyncConfig{
		Enabled:        true,
		Interval:       Duration{80 * time.Millisecond},
		RequestTimeout: Duration{3 * time.Second},
		InitialBackoff: Duration{50 * time.Millisecond},
		MaxBackoff:     Duration{500 * time.Millisecond},
		JitterFraction: 0,
	}
}

// openBgPrimary opens a primary server for Phase 27 tests.
func openBgPrimary(t *testing.T) *Server {
	t.Helper()
	srv, err := Open(Options{
		NodeID:  "p27-primary",
		Addr:    "127.0.0.1:0",
		DataDir: t.TempDir(),
		Replication: ReplicationOptions{
			Role: replnet.RolePrimary,
		},
	})
	if err != nil {
		t.Fatalf("open primary: %v", err)
	}
	t.Cleanup(func() { srv.Close() })
	if err := srv.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}
	return srv
}

// openBgFollower opens a follower with background sync enabled.
func openBgFollower(t *testing.T, primaryAddr, dataDir string) *Server {
	t.Helper()
	srv, err := Open(Options{
		NodeID:  "p27-follower",
		Addr:    "127.0.0.1:0",
		DataDir: dataDir,
		Replication: ReplicationOptions{
			Role:           replnet.RoleFollower,
			PrimaryBaseURL: "http://" + primaryAddr,
			BackgroundSync: bgIntegrationCfg(),
		},
	})
	if err != nil {
		t.Fatalf("open follower: %v", err)
	}
	t.Cleanup(func() { srv.Close() })
	if err := srv.StartBackground(); err != nil {
		t.Fatalf("start follower: %v", err)
	}
	return srv
}

// p27Req performs an HTTP request against a Server's handler and returns the recorder.
func p27Req(t *testing.T, srv *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	var err error
	if body != "" {
		req, err = http.NewRequest(method, path, strings.NewReader(body))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
		}
	} else {
		req, err = http.NewRequest(method, path, nil)
	}
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

// p27Decode decodes JSON from a recorder body into v.
func p27Decode(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(v); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
}

// waitForKeyOnFollower polls follower until key appears or 10s elapsed.
func waitForKeyOnFollower(t *testing.T, follower *Server, key string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		rec := p27Req(t, follower, http.MethodGet, "/kv/"+key, "")
		if rec.Code == http.StatusOK {
			var resp struct {
				Found bool `json:"found"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&resp); err == nil && resp.Found {
				return
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("timeout: key %q did not appear on follower within 10s", key)
}

// waitForKeyGoneOnFollower polls follower until key is absent or 10s elapsed.
func waitForKeyGoneOnFollower(t *testing.T, follower *Server, key string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		rec := p27Req(t, follower, http.MethodGet, "/kv/"+key, "")
		if rec.Code == http.StatusOK {
			var resp struct {
				Found bool `json:"found"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&resp); err == nil && !resp.Found {
				return
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("timeout: key %q still present on follower after 10s", key)
}

// waitForBgWorkerState polls until background sync worker reaches want state.
func waitForBgWorkerState(t *testing.T, srv *Server, want WorkerState) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if srv.BackgroundSyncStatus().State == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for bg state %q; got %q", want, srv.BackgroundSyncStatus().State)
}

// waitForBgLagKnown polls until background sync reports lag_known=true.
func waitForBgLagKnown(t *testing.T, srv *Server) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if srv.BackgroundSyncStatus().LagKnown {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout: LagKnown did not become true within 10s")
}

// waitForBgZeroLag polls until LagKnown && LagEntries == 0.
func waitForBgZeroLag(t *testing.T, srv *Server) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		st := srv.BackgroundSyncStatus()
		if st.LagKnown && st.LagEntries == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	st := srv.BackgroundSyncStatus()
	t.Fatalf("timeout: LagKnown=%v LagEntries=%d (want known=true, entries=0)", st.LagKnown, st.LagEntries)
}

// ── Automatic propagation tests ────────────────────────────────────────────────

func TestPhase27_AutoPUT_PropagatesWithoutManualSync(t *testing.T) {
	primary := openBgPrimary(t)
	follower := openBgFollower(t, primary.Addr(), t.TempDir())

	p27Req(t, primary, http.MethodPut, "/kv/auto-key1", `{"value":"auto-value1"}`)

	// Key must appear on follower without POST /replication/sync.
	waitForKeyOnFollower(t, follower, "auto-key1")
}

func TestPhase27_AutoDELETE_PropagatesWithoutManualSync(t *testing.T) {
	primary := openBgPrimary(t)
	follower := openBgFollower(t, primary.Addr(), t.TempDir())

	p27Req(t, primary, http.MethodPut, "/kv/del-key", `{"value":"to-delete"}`)
	waitForKeyOnFollower(t, follower, "del-key")

	p27Req(t, primary, http.MethodDelete, "/kv/del-key", "")
	waitForKeyGoneOnFollower(t, follower, "del-key")
}

func TestPhase27_MultipleOrdered_PropagateAutomatically(t *testing.T) {
	primary := openBgPrimary(t)
	follower := openBgFollower(t, primary.Addr(), t.TempDir())

	keys := []string{"m27-1", "m27-2", "m27-3", "m27-4", "m27-5"}
	for _, k := range keys {
		p27Req(t, primary, http.MethodPut, "/kv/"+k, `{"value":"v"}`)
	}
	for _, k := range keys {
		waitForKeyOnFollower(t, follower, k)
	}
}

func TestPhase27_EmptySync_WorkerRemainsHealthy(t *testing.T) {
	primary := openBgPrimary(t)
	_ = primary

	follower, err := Open(Options{
		NodeID:  "follower-empty",
		Addr:    "127.0.0.1:0",
		DataDir: t.TempDir(),
		Replication: ReplicationOptions{
			Role:           replnet.RoleFollower,
			PrimaryBaseURL: "http://" + primary.Addr(),
			BackgroundSync: bgIntegrationCfg(),
		},
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { follower.Close() })
	if err := follower.StartBackground(); err != nil {
		t.Fatalf("start: %v", err)
	}

	// No writes: initial sync returns empty; worker should remain healthy.
	waitForBgWorkerState(t, follower, WorkerStateRunning)
	waitForBgLagKnown(t, follower)

	st := follower.BackgroundSyncStatus()
	if st.TotalFailures != 0 {
		t.Errorf("TotalFailures = %d, want 0 for empty syncs", st.TotalFailures)
	}
	if st.LagEntries != 0 {
		t.Errorf("LagEntries = %d, want 0 when primary is empty", st.LagEntries)
	}
}

func TestPhase27_DurableCursorAdvancesOnAutoSync(t *testing.T) {
	primary := openBgPrimary(t)
	follower := openBgFollower(t, primary.Addr(), t.TempDir())

	p27Req(t, primary, http.MethodPut, "/kv/cursor-key", `{"value":"cv"}`)
	waitForKeyOnFollower(t, follower, "cursor-key")

	st := follower.BackgroundSyncStatus()
	if st.FollowerLastAppliedSeq == 0 {
		t.Error("FollowerLastAppliedSeq should be > 0 after applying entries")
	}
}

// ── Lag tracking ───────────────────────────────────────────────────────────────

func TestPhase27_Lag_KnownAndZeroWhenCaughtUp(t *testing.T) {
	primary := openBgPrimary(t)
	follower := openBgFollower(t, primary.Addr(), t.TempDir())

	p27Req(t, primary, http.MethodPut, "/kv/lag-key", `{"value":"v"}`)
	waitForKeyOnFollower(t, follower, "lag-key")
	waitForBgZeroLag(t, follower)

	st := follower.BackgroundSyncStatus()
	if !st.LagKnown {
		t.Error("LagKnown should be true when caught up")
	}
	if st.LagEntries != 0 {
		t.Errorf("LagEntries = %d, want 0 when caught up", st.LagEntries)
	}
	if st.LagObservedAt == nil {
		t.Error("LagObservedAt should be set after catching up")
	}
}

func TestPhase27_Lag_UnknownAfterPrimaryFailure(t *testing.T) {
	ct := &controlledTimer{}
	cfg := bgIntegrationCfg()

	// First sync succeeds; second sync fails (primary gone).
	call := 0
	w := newTestWorker(t, cfg, func(ctx context.Context) (SyncResult, error) {
		call++
		if call == 1 {
			return SyncResult{LastAppliedSeq: 3, PrimaryLatestSeq: 3, LagKnown: true}, nil
		}
		return SyncResult{}, errors.New("connection refused")
	})
	w.afterFn = ct.afterFn

	mustStartWorker(t, w)
	defer w.stop()

	waitForSuccesses(t, w, 1) // lag known after first success

	ct.fireNext(t) // trigger failure

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if w.Status().TotalFailures >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if w.Status().LagKnown {
		t.Error("LagKnown should be false after primary failure")
	}
}

func TestPhase27_Lag_NeverFalselyZeroWhenUnknown(t *testing.T) {
	cfg := bgIntegrationCfg()
	w := newTestWorker(t, cfg, func(ctx context.Context) (SyncResult, error) {
		return SyncResult{}, errors.New("always fails")
	})

	mustStartWorker(t, w)
	defer w.stop()

	waitForAttempts(t, w, 1)
	st := w.Status()
	if st.LagKnown {
		t.Error("LagKnown must be false when primary was never reached")
	}
}

// ── Replication-gap terminal handling ──────────────────────────────────────────

func TestPhase27_ReplicationGap_BlockedState_ManualSyncStillReturnsGap(t *testing.T) {
	// Use a real Server with a fake primary that always returns HTTP 409 (gap).
	// Once the bg worker enters blocked state, verify that a manual POST /replication/sync
	// also returns HTTP 409 with gap details and that the cursor is unchanged.
	gapErr := &replnet.ReplicationGapError{
		RequestedAfter:    2,
		FirstAvailableSeq: 5,
		LatestSeq:         10,
	}
	fakePrimary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/replication/log") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": gapErr.Error(),
				"gap":   gapErr,
			})
			return
		}
		// /healthz, /replication/status etc. return 200.
		w.WriteHeader(http.StatusOK)
	}))
	defer fakePrimary.Close()

	follower, err := Open(Options{
		NodeID:  "gap-test-follower",
		Addr:    "127.0.0.1:0",
		DataDir: t.TempDir(),
		Replication: ReplicationOptions{
			Role:           replnet.RoleFollower,
			PrimaryBaseURL: fakePrimary.URL,
			BackgroundSync: bgIntegrationCfg(),
		},
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { follower.Close() })
	if err := follower.StartBackground(); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Wait for bg worker to enter blocked state.
	waitForBgWorkerState(t, follower, WorkerStateBlocked)

	// Cursor must be 0 (never advanced: gap on first sync).
	cursorBefore := follower.BackgroundSyncStatus().FollowerLastAppliedSeq

	// POST /replication/sync must also return 409 with gap info.
	rec := p27Req(t, follower, http.MethodPost, "/replication/sync", "")
	if rec.Code != http.StatusConflict {
		t.Errorf("POST /replication/sync = %d, want 409 Conflict when gap exists", rec.Code)
	}

	var body struct {
		Gap   *replnet.ReplicationGapError `json:"gap"`
		Error string                       `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode gap response: %v", err)
	}
	if body.Gap == nil {
		t.Error("gap field must be present in 409 response")
	}

	// Cursor must not have changed.
	if follower.BackgroundSyncStatus().FollowerLastAppliedSeq != cursorBefore {
		t.Errorf("cursor changed from %d after gap error (must be unchanged)", cursorBefore)
	}
}

func TestPhase27_ReplicationGap_DoesNotRetryInBackground(t *testing.T) {
	calls := 0
	cfg := bgIntegrationCfg()
	cfg.Interval = Duration{10 * time.Millisecond}

	w := newTestWorker(t, cfg, func(ctx context.Context) (SyncResult, error) {
		calls++
		return SyncResult{}, &replnet.ReplicationGapError{
			RequestedAfter: 1, FirstAvailableSeq: 5, LatestSeq: 10,
		}
	})

	mustStartWorker(t, w)
	defer w.stop()

	waitForState(t, w, WorkerStateBlocked)
	time.Sleep(150 * time.Millisecond) // give time for spurious retries

	if calls > 1 {
		t.Errorf("syncFn called %d times after gap; expected exactly 1 (no retry)", calls)
	}
}

// ── Status API ─────────────────────────────────────────────────────────────────

func TestPhase27_Status_PrimaryReportsBgDisabled(t *testing.T) {
	primary := openBgPrimary(t)

	rec := p27Req(t, primary, http.MethodGet, "/replication/status", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /replication/status = %d", rec.Code)
	}

	var body struct {
		BackgroundSync BackgroundSyncStatus `json:"background_sync"`
	}
	p27Decode(t, rec, &body)

	if body.BackgroundSync.Enabled {
		t.Error("primary should report background_sync_enabled=false")
	}
	if body.BackgroundSync.State != WorkerStateDisabled {
		t.Errorf("primary state = %q, want disabled", body.BackgroundSync.State)
	}
}

func TestPhase27_Status_FollowerReportsBgEnabled(t *testing.T) {
	primary := openBgPrimary(t)
	follower := openBgFollower(t, primary.Addr(), t.TempDir())
	waitForBgWorkerState(t, follower, WorkerStateRunning)

	rec := p27Req(t, follower, http.MethodGet, "/replication/status", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /replication/status = %d", rec.Code)
	}

	var body struct {
		BackgroundSync BackgroundSyncStatus `json:"background_sync"`
	}
	p27Decode(t, rec, &body)

	if !body.BackgroundSync.Enabled {
		t.Error("follower should report background_sync_enabled=true")
	}
	if body.BackgroundSync.State == WorkerStateDisabled {
		t.Error("follower bg state should not be disabled")
	}
}

func TestPhase27_Status_ReadsDontTriggerNetworkCalls(t *testing.T) {
	// BackgroundSyncStatus() must be fast and not make HTTP calls.
	primary := openBgPrimary(t)
	follower := openBgFollower(t, primary.Addr(), t.TempDir())
	waitForBgWorkerState(t, follower, WorkerStateRunning)

	// Call BackgroundSyncStatus() 1000 times and verify it returns immediately.
	start := time.Now()
	for i := 0; i < 1000; i++ {
		_ = follower.BackgroundSyncStatus()
	}
	elapsed := time.Since(start)
	if elapsed > 500*time.Millisecond {
		t.Errorf("BackgroundSyncStatus 1000 calls took %v; must be fast (no network)", elapsed)
	}
}

// ── Manual sync coexistence ────────────────────────────────────────────────────

func TestPhase27_ManualSync_StillWorksWithBgEnabled(t *testing.T) {
	primary := openBgPrimary(t)
	follower := openBgFollower(t, primary.Addr(), t.TempDir())

	p27Req(t, primary, http.MethodPut, "/kv/manual27-key", `{"value":"mv"}`)
	waitForKeyOnFollower(t, follower, "manual27-key") // automatic sync got it

	// Manual sync should return 200 or 409 (if bg is in-flight).
	rec := p27Req(t, follower, http.MethodPost, "/replication/sync", "")
	if rec.Code != http.StatusOK && rec.Code != http.StatusConflict {
		t.Errorf("POST /replication/sync = %d, want 200 or 409", rec.Code)
	}
}

func TestPhase27_ManualSync_ResponseIncludesBgEnabled(t *testing.T) {
	// Stop the background worker before calling manual sync so there is no race
	// on syncInProgress; the bgWorker field remains non-nil so BackgroundSyncEnabled
	// is correctly reported as true in the SyncResult.
	primary := openBgPrimary(t)
	follower := openBgFollower(t, primary.Addr(), t.TempDir())
	waitForBgWorkerState(t, follower, WorkerStateRunning)
	waitForBgLagKnown(t, follower)

	follower.bgWorker.stop() // quiesce background; bgWorker is still non-nil

	result, err := follower.SyncFromPrimary(context.Background())
	if err != nil {
		t.Fatalf("SyncFromPrimary: %v", err)
	}
	if !result.BackgroundSyncEnabled {
		t.Error("BackgroundSyncEnabled must be true when bgWorker is configured, even after worker is stopped")
	}
}

// ── Restart behavior ───────────────────────────────────────────────────────────

func TestPhase27_FollowerRestart_ResumesFromDurableCursor(t *testing.T) {
	primary := openBgPrimary(t)
	followerDir := t.TempDir()
	follower := openBgFollower(t, primary.Addr(), followerDir)

	// Write 3 entries.
	p27Req(t, primary, http.MethodPut, "/kv/pre-rst-1", `{"value":"a"}`)
	p27Req(t, primary, http.MethodPut, "/kv/pre-rst-2", `{"value":"b"}`)
	p27Req(t, primary, http.MethodPut, "/kv/pre-rst-3", `{"value":"c"}`)
	waitForKeyOnFollower(t, follower, "pre-rst-3")
	waitForBgZeroLag(t, follower)

	cursorBefore := follower.BackgroundSyncStatus().FollowerLastAppliedSeq
	follower.Close()

	// Write 2 more while follower is down.
	p27Req(t, primary, http.MethodPut, "/kv/post-rst-4", `{"value":"d"}`)
	p27Req(t, primary, http.MethodPut, "/kv/post-rst-5", `{"value":"e"}`)

	// Restart follower.
	follower2 := openBgFollower(t, primary.Addr(), followerDir)

	// Cursor must not have reset to 0.
	waitForBgWorkerState(t, follower2, WorkerStateRunning)
	if cursorBefore > 0 {
		st := follower2.BackgroundSyncStatus()
		// After first sync, FollowerLastAppliedSeq >= cursorBefore.
		waitForBgZeroLag(t, follower2)
		if follower2.BackgroundSyncStatus().FollowerLastAppliedSeq < cursorBefore {
			t.Errorf("cursor after restart = %d, want >= %d (cursor regression)",
				st.FollowerLastAppliedSeq, cursorBefore)
		}
	}

	// New keys should arrive automatically.
	waitForKeyOnFollower(t, follower2, "post-rst-4")
	waitForKeyOnFollower(t, follower2, "post-rst-5")
}

func TestPhase27_FollowerRestart_CursorRestoredBeforeWorkerStart(t *testing.T) {
	// Prove the durable cursor is restored from the state file at Open() time,
	// before StartBackground() is called. The in-memory cursor must equal the
	// persisted cursor before the background worker fires a single sync.
	primary := openBgPrimary(t)
	followerDir := t.TempDir()

	// Sync through N entries.
	follower := openBgFollower(t, primary.Addr(), followerDir)
	p27Req(t, primary, http.MethodPut, "/kv/cursor-proof-1", `{"value":"a"}`)
	p27Req(t, primary, http.MethodPut, "/kv/cursor-proof-2", `{"value":"b"}`)
	p27Req(t, primary, http.MethodPut, "/kv/cursor-proof-3", `{"value":"c"}`)
	waitForKeyOnFollower(t, follower, "cursor-proof-3")

	// Poll until the background sync STATUS reflects FollowerLastAppliedSeq >= 3.
	// The status is updated in doSync AFTER ApplyReplicationEntries returns (which saves
	// the state file). Polling the status field — rather than waitForBgZeroLag — avoids
	// a window where waitForBgZeroLag returns on stale lag=0 from a previous sync cycle
	// while the current sync is between the engine write and the status update.
	var cursorN uint64
	{
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			cursorN = follower.BackgroundSyncStatus().FollowerLastAppliedSeq
			if cursorN >= 3 {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if cursorN < 3 {
			t.Fatalf("timeout: FollowerLastAppliedSeq=%d, want >= 3 (state file must be saved)", cursorN)
		}
	}
	follower.Close()

	// Write N+1 and N+2 while follower is down.
	p27Req(t, primary, http.MethodPut, "/kv/cursor-proof-4", `{"value":"d"}`)
	p27Req(t, primary, http.MethodPut, "/kv/cursor-proof-5", `{"value":"e"}`)

	// Open follower WITHOUT starting (worker not yet running).
	follower2, err := Open(Options{
		NodeID:  "p27-follower",
		Addr:    "127.0.0.1:0",
		DataDir: followerDir,
		Replication: ReplicationOptions{
			Role:           replnet.RoleFollower,
			PrimaryBaseURL: "http://" + primary.Addr(),
			BackgroundSync: bgIntegrationCfg(),
		},
	})
	if err != nil {
		t.Fatalf("reopen follower: %v", err)
	}
	t.Cleanup(func() { follower2.Close() })

	// Verify in-memory cursor == cursorN before any sync (restored from state file).
	inMemCursor := atomic.LoadUint64(&follower2.lastApplied)
	if inMemCursor != cursorN {
		t.Errorf("cursor after Open (before Start) = %d, want %d (must be restored from state store)",
			inMemCursor, cursorN)
	}

	// Now start (which starts the bg worker).
	if err := follower2.StartBackground(); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Worker should fetch only N+1 and N+2.
	waitForKeyOnFollower(t, follower2, "cursor-proof-4")
	waitForKeyOnFollower(t, follower2, "cursor-proof-5")
	waitForBgZeroLag(t, follower2)

	finalCursor := follower2.BackgroundSyncStatus().FollowerLastAppliedSeq
	if finalCursor != cursorN+2 {
		t.Errorf("final cursor = %d, want %d (exactly 2 new entries applied)",
			finalCursor, cursorN+2)
	}
}

func TestPhase27_DeleteAfterRestart_PropagatesAutomatically(t *testing.T) {
	primary := openBgPrimary(t)
	followerDir := t.TempDir()
	follower := openBgFollower(t, primary.Addr(), followerDir)

	p27Req(t, primary, http.MethodPut, "/kv/del-rst-key", `{"value":"x"}`)
	waitForKeyOnFollower(t, follower, "del-rst-key")
	follower.Close()

	// DELETE while follower is down.
	p27Req(t, primary, http.MethodDelete, "/kv/del-rst-key", "")

	// Restart follower.
	follower2 := openBgFollower(t, primary.Addr(), followerDir)
	waitForKeyGoneOnFollower(t, follower2, "del-rst-key")
}

func TestPhase27_BothRestart_NoDuplicateSemanticEffects(t *testing.T) {
	primaryDir := t.TempDir()
	followerDir := t.TempDir()

	// Start both.
	primary, err := Open(Options{
		NodeID:      "p27-p2",
		Addr:        "127.0.0.1:0",
		DataDir:     primaryDir,
		Replication: ReplicationOptions{Role: replnet.RolePrimary},
	})
	if err != nil {
		t.Fatalf("open primary: %v", err)
	}
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}

	follower, err := Open(Options{
		NodeID:  "p27-f2",
		Addr:    "127.0.0.1:0",
		DataDir: followerDir,
		Replication: ReplicationOptions{
			Role:           replnet.RoleFollower,
			PrimaryBaseURL: "http://" + primary.Addr(),
			BackgroundSync: bgIntegrationCfg(),
		},
	})
	if err != nil {
		t.Fatalf("open follower: %v", err)
	}
	if err := follower.StartBackground(); err != nil {
		t.Fatalf("start follower: %v", err)
	}

	p27Req(t, primary, http.MethodPut, "/kv/both-restart", `{"value":"v1"}`)
	waitForKeyOnFollower(t, follower, "both-restart")
	waitForBgZeroLag(t, follower)

	primaryAddr := primary.Addr()
	follower.Close()
	primary.Close()

	// Restart primary.
	primary2, err := Open(Options{
		NodeID:      "p27-p2",
		Addr:        primaryAddr,
		DataDir:     primaryDir,
		Replication: ReplicationOptions{Role: replnet.RolePrimary},
	})
	if err != nil {
		t.Fatalf("reopen primary: %v", err)
	}
	t.Cleanup(func() { primary2.Close() })
	if err := primary2.StartBackground(); err != nil {
		t.Fatalf("restart primary: %v", err)
	}

	// Restart follower.
	follower2, err := Open(Options{
		NodeID:  "p27-f2",
		Addr:    "127.0.0.1:0",
		DataDir: followerDir,
		Replication: ReplicationOptions{
			Role:           replnet.RoleFollower,
			PrimaryBaseURL: "http://" + primary2.Addr(),
			BackgroundSync: bgIntegrationCfg(),
		},
	})
	if err != nil {
		t.Fatalf("reopen follower: %v", err)
	}
	t.Cleanup(func() { follower2.Close() })
	if err := follower2.StartBackground(); err != nil {
		t.Fatalf("restart follower: %v", err)
	}

	// Write new entry after restart.
	p27Req(t, primary2, http.MethodPut, "/kv/after-both-restart", `{"value":"v2"}`)
	waitForKeyOnFollower(t, follower2, "after-both-restart")

	// Original key must still be present (idempotent reapplication, no loss).
	rec := p27Req(t, follower2, http.MethodGet, "/kv/both-restart", "")
	var getResp struct {
		Found bool   `json:"found"`
		Value string `json:"value"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&getResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !getResp.Found {
		t.Error("original key must still be present after both-node restart")
	}
	if getResp.Value != "v1" {
		t.Errorf("value = %q, want %q", getResp.Value, "v1")
	}
}

// ── SyncFromPrimary result includes lag fields ─────────────────────────────────

func TestPhase27_SyncFromPrimary_ResultIncludesLagFields(t *testing.T) {
	primary := openBgPrimary(t)

	// Use follower WITHOUT background sync for clean manual test.
	follower, err := Open(Options{
		NodeID:  "follower-manual27",
		Addr:    "127.0.0.1:0",
		DataDir: t.TempDir(),
		Replication: ReplicationOptions{
			Role:           replnet.RoleFollower,
			PrimaryBaseURL: "http://" + primary.Addr(),
		},
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { follower.Close() })
	if err := follower.StartBackground(); err != nil {
		t.Fatalf("start: %v", err)
	}

	p27Req(t, primary, http.MethodPut, "/kv/lag-field-key", `{"value":"lv"}`)

	result, err := follower.SyncFromPrimary(context.Background())
	if err != nil {
		t.Fatalf("SyncFromPrimary: %v", err)
	}
	if result.PrimaryLatestSeq == 0 {
		t.Error("PrimaryLatestSeq should be > 0 after a PUT on primary")
	}
	if !result.LagKnown {
		t.Error("LagKnown should be true after successful sync")
	}
	if result.LagEntriesAfterSync != 0 {
		t.Errorf("LagEntriesAfterSync = %d, want 0 when fully caught up", result.LagEntriesAfterSync)
	}
	if result.BackgroundSyncEnabled {
		t.Error("BackgroundSyncEnabled should be false for follower without bg sync")
	}
}

func TestPhase27_SyncFromPrimary_BgEnabled_ReportedInResult(t *testing.T) {
	// Stop the background worker so manual sync doesn't race on syncInProgress;
	// bgWorker is still non-nil so BackgroundSyncEnabled is reported as true.
	primary := openBgPrimary(t)
	follower := openBgFollower(t, primary.Addr(), t.TempDir())
	waitForBgWorkerState(t, follower, WorkerStateRunning)
	waitForBgLagKnown(t, follower)

	follower.bgWorker.stop() // quiesce; bgWorker field stays non-nil

	result, err := follower.SyncFromPrimary(context.Background())
	if err != nil {
		t.Fatalf("SyncFromPrimary: %v", err)
	}
	if !result.BackgroundSyncEnabled {
		t.Error("BackgroundSyncEnabled should be true when bgWorker is configured")
	}
}

// ── Phase-26 compatibility: absent primary_latest_seq ─────────────────────────

func TestPhase27_PrimaryLatestSeq_AbsentMeansLagUnknown(t *testing.T) {
	// Simulate a Phase-26-style primary whose /replication/log response omits
	// primary_latest_seq. SyncFromPrimary must set LagKnown=false in the result
	// rather than falsely inferring "lag=0" from the zero value.
	fakePrimary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/replication/log") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			// Intentionally omit primary_latest_seq (Phase-26 style).
			_ = json.NewEncoder(w).Encode(map[string]any{
				"node_id": "old-primary",
				"after":   0,
				"count":   0,
				"entries": []any{},
			})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer fakePrimary.Close()

	follower, err := Open(Options{
		NodeID:  "p27-compat-follower",
		Addr:    "127.0.0.1:0",
		DataDir: t.TempDir(),
		Replication: ReplicationOptions{
			Role:           replnet.RoleFollower,
			PrimaryBaseURL: fakePrimary.URL,
		},
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { follower.Close() })
	if err := follower.StartBackground(); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Manual sync against Phase-26-style primary.
	result, err := follower.SyncFromPrimary(context.Background())
	if err != nil {
		t.Fatalf("SyncFromPrimary: %v", err)
	}
	// When primary omits primary_latest_seq, LagKnown must be false.
	if result.LagKnown {
		t.Error("LagKnown should be false when primary_latest_seq is absent (Phase-26 compat)")
	}
	if result.PrimaryLatestSeq != 0 {
		t.Errorf("PrimaryLatestSeq = %d, want 0 when field is absent", result.PrimaryLatestSeq)
	}
}

// ── follower without bg sync reports disabled ──────────────────────────────────

func TestPhase27_FollowerNoBg_ReportsDisabled(t *testing.T) {
	srv, err := Open(Options{
		NodeID:  "follower-no-bg27",
		Addr:    "127.0.0.1:0",
		DataDir: t.TempDir(),
		Replication: ReplicationOptions{
			Role:           replnet.RoleFollower,
			PrimaryBaseURL: "http://127.0.0.1:9999",
		},
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { srv.Close() })
	if err := srv.StartBackground(); err != nil {
		t.Fatalf("start: %v", err)
	}

	st := srv.BackgroundSyncStatus()
	if st.Enabled {
		t.Error("Enabled should be false when background sync is not configured")
	}
	if st.State != WorkerStateDisabled {
		t.Errorf("State = %q, want %q", st.State, WorkerStateDisabled)
	}
}

// ── replication/log response includes primary_latest_seq ──────────────────────

func TestPhase27_ReplicationLog_IncludesPrimaryLatestSeq(t *testing.T) {
	primary := openBgPrimary(t)

	p27Req(t, primary, http.MethodPut, "/kv/log-seq-key", `{"value":"v"}`)

	rec := p27Req(t, primary, http.MethodGet, "/replication/log?after=0", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /replication/log = %d", rec.Code)
	}

	var body struct {
		PrimaryLatestSeq uint64 `json:"primary_latest_seq"`
		Count            int    `json:"count"`
	}
	p27Decode(t, rec, &body)

	if body.PrimaryLatestSeq == 0 {
		t.Error("primary_latest_seq should be > 0 after a PUT")
	}
	if body.Count == 0 {
		t.Error("count should be > 0 after a PUT")
	}
}

// ── Typed client ──────────────────────────────────────────────────────────────

func TestPhase27_ReplicationStatus_TypedClient(t *testing.T) {
	primary := openBgPrimary(t)
	follower := openBgFollower(t, primary.Addr(), t.TempDir())
	waitForBgWorkerState(t, follower, WorkerStateRunning)

	c := NewClient("http://"+follower.Addr(), 5*time.Second)
	resp, err := c.ReplicationStatus(context.Background())
	if err != nil {
		t.Fatalf("ReplicationStatus: %v", err)
	}
	if resp.NodeID == "" {
		t.Error("NodeID should be non-empty")
	}
	if !resp.BackgroundSync.Enabled {
		t.Error("BackgroundSync.Enabled should be true for follower with bg sync")
	}
	if resp.BackgroundSync.State == WorkerStateDisabled {
		t.Errorf("BackgroundSync.State = %q, want non-disabled", resp.BackgroundSync.State)
	}
	if resp.Replication.Role != replnet.RoleFollower {
		t.Errorf("Replication.Role = %q, want %q", resp.Replication.Role, replnet.RoleFollower)
	}
}

// ── UTC timestamp verification ────────────────────────────────────────────────

func TestPhase27_WorkerTimestamps_AreUTC(t *testing.T) {
	primary := openBgPrimary(t)
	follower := openBgFollower(t, primary.Addr(), t.TempDir())
	waitForBgLagKnown(t, follower)

	st := follower.BackgroundSyncStatus()

	if st.LastSuccessAt == nil {
		t.Fatal("LastSuccessAt not set after successful sync")
	}
	if st.LastSuccessAt.Location() != time.UTC {
		t.Errorf("LastSuccessAt.Location() = %v, want UTC", st.LastSuccessAt.Location())
	}
	if st.LagObservedAt == nil {
		t.Fatal("LagObservedAt not set after successful sync with LagKnown=true")
	}
	if st.LagObservedAt.Location() != time.UTC {
		t.Errorf("LagObservedAt.Location() = %v, want UTC", st.LagObservedAt.Location())
	}
	if st.LastAttemptAt == nil {
		t.Fatal("LastAttemptAt not set after sync attempt")
	}
	if st.LastAttemptAt.Location() != time.UTC {
		t.Errorf("LastAttemptAt.Location() = %v, want UTC", st.LastAttemptAt.Location())
	}
}
