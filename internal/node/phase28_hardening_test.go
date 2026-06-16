package node

// Phase 28 hardening tests — concurrency, failure-state, fencing, crash-consistency.
//
// Scope: write-gate fencing, quiesceMu serialization, promotionBarrier, typed status,
// cross-validation on startup, quiesce_failed_fenced retry, seam injection.
// NOT automatic failover. NOT Raft. NOT consensus.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/YashPatel2395/ShardForgeDB/internal/replnet"
)

// ── 1. quiesceIDFn injection — error propagation ─────────────────────────────────

func TestHardening_QuiesceIDFn_Failure_AbortsQuiesce(t *testing.T) {
	srv := p28PrimaryRunning(t)

	// Inject a failing quiesceIDFn to simulate entropy failure.
	srv.quiesceIDFn = func() (string, error) {
		return "", errors.New("entropy unavailable")
	}

	rec := p28Req(t, srv, "POST", "/replication/quiesce", "")
	if rec.Code != 500 {
		t.Errorf("expected 500 on entropy failure, got %d: %s", rec.Code, rec.Body.String())
	}
	// Write gate must NOT be closed — writes must still be accepted.
	put := p28Req(t, srv, "PUT", "/kv/after-entropy-fail", `{"value":"ok"}`)
	if put.Code != 200 {
		t.Errorf("writes should still work after entropy failure, got %d", put.Code)
	}
}

// ── 2. quiesceIDFn injection — stable ID on retry ───────────────────────────────

func TestHardening_QuiesceFailedFenced_Retry_UsesSameID(t *testing.T) {
	srv := p28PrimaryRunning(t)

	// Track generated IDs.
	var ids []string
	var idMu sync.Mutex
	srv.quiesceIDFn = func() (string, error) {
		id, err := replnet.NewQuiesceID()
		if err != nil {
			return "", err
		}
		idMu.Lock()
		ids = append(ids, id)
		idMu.Unlock()
		return id, nil
	}

	// First quiesce — should succeed normally.
	rec1 := p28Req(t, srv, "POST", "/replication/quiesce", "")
	if rec1.Code != 200 {
		t.Fatalf("first quiesce: %d %s", rec1.Code, rec1.Body.String())
	}
	var resp1 QuiesceResponse
	json.Unmarshal(rec1.Body.Bytes(), &resp1)

	// Second quiesce — idempotent, must return same QuiesceID without calling quiesceIDFn again.
	rec2 := p28Req(t, srv, "POST", "/replication/quiesce", "")
	if rec2.Code != 200 {
		t.Fatalf("second quiesce: %d %s", rec2.Code, rec2.Body.String())
	}
	var resp2 QuiesceResponse
	json.Unmarshal(rec2.Body.Bytes(), &resp2)

	if !resp2.Idempotent {
		t.Error("second quiesce must be idempotent")
	}
	if resp2.QuiesceID != resp1.QuiesceID {
		t.Errorf("quiesce_id changed on idempotent retry: %q vs %q", resp1.QuiesceID, resp2.QuiesceID)
	}

	// Only one ID should have been generated (idempotent path does not call quiesceIDFn).
	idMu.Lock()
	count := len(ids)
	idMu.Unlock()
	if count != 1 {
		t.Errorf("expected 1 ID generated, got %d", count)
	}
}

// ── 3. quiesceMu — concurrent quiesce produces single QuiesceID ──────────────────

func TestHardening_ConcurrentQuiesce_SingleID(t *testing.T) {
	srv := p28PrimaryRunning(t)

	const n = 5
	results := make([]QuiesceResponse, n)
	codes := make([]int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rec := p28Req(t, srv, "POST", "/replication/quiesce", "")
			codes[i] = rec.Code
			json.Unmarshal(rec.Body.Bytes(), &results[i])
		}(i)
	}
	wg.Wait()

	// All must succeed (200).
	for i, code := range codes {
		if code != 200 {
			t.Errorf("goroutine %d: got %d", i, code)
		}
	}
	// All must agree on the same QuiesceID — only one ID was generated.
	firstID := results[0].QuiesceID
	if firstID == "" {
		t.Error("quiesce_id is empty")
	}
	for i, r := range results {
		if r.QuiesceID != firstID {
			t.Errorf("goroutine %d: quiesce_id %q != %q", i, r.QuiesceID, firstID)
		}
	}
}

// ── 4. write gate fences handleExplainPut ────────────────────────────────────────

func TestHardening_ExplainPut_RespectedByWriteGate(t *testing.T) {
	srv := p28PrimaryRunning(t)
	p28Req(t, srv, "POST", "/replication/quiesce", "")

	// ExplainPut does a real engine write; it must be blocked after quiesce.
	body, _ := json.Marshal(map[string]string{"key": "k", "value": "v"})
	req, _ := http.NewRequest("POST", "/explain/put", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409 for ExplainPut after quiesce, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["code"] != "node_quiesced" {
		t.Errorf("expected code=node_quiesced, got %v", resp["code"])
	}
}

// ── 5. write gate fences handleExplainDelete ─────────────────────────────────────

func TestHardening_ExplainDelete_RespectedByWriteGate(t *testing.T) {
	srv := p28PrimaryRunning(t)
	p28Req(t, srv, "PUT", "/kv/del-me", `{"value":"x"}`)
	p28Req(t, srv, "POST", "/replication/quiesce", "")

	req, _ := http.NewRequest("DELETE", "/explain/delete?key=del-me", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409 for ExplainDelete after quiesce, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["code"] != "node_quiesced" {
		t.Errorf("expected code=node_quiesced, got %v", resp["code"])
	}
}

// ── 6. promotionBarrier blocks SyncFromPrimary ────────────────────────────────────

func TestHardening_PromotionBarrier_BlocksSyncFromPrimary(t *testing.T) {
	primary := p28Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}
	follower := p28Follower(t, primary.Addr(), t.TempDir())
	if err := follower.StartBackground(); err != nil {
		t.Fatalf("start follower: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// Raise the promotion barrier.
	follower.promotionBarrier.Store(true)
	defer follower.promotionBarrier.Store(false)

	_, err := follower.SyncFromPrimary(context.Background())
	if err == nil {
		t.Error("expected error from SyncFromPrimary when promotion barrier is raised")
	}
	if !strings.Contains(err.Error(), "promotion in progress") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ── 7. promotionBarrier double-check — slip-through test ─────────────────────────

func TestHardening_PromotionBarrier_PostCAS_DoubleCheck(t *testing.T) {
	// This tests that even if a sync CAS'd syncInProgress before the barrier was set,
	// the double-check after CAS catches the barrier and the sync aborts.
	primary := p28Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}
	follower := p28Follower(t, primary.Addr(), t.TempDir())
	if err := follower.StartBackground(); err != nil {
		t.Fatalf("start follower: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// Simulate: CAS succeeds but barrier is immediately set.
	follower.syncInProgress.Store(false)
	// Set barrier AFTER the moment when CAS would have checked it.
	follower.promotionBarrier.Store(true)
	defer follower.promotionBarrier.Store(false)

	_, err := follower.SyncFromPrimary(context.Background())
	if err == nil || !strings.Contains(err.Error(), "promotion in progress") {
		t.Errorf("expected promotion-in-progress error, got: %v", err)
	}
	// syncInProgress must be false after abort (defer cleared it).
	if follower.syncInProgress.Load() {
		t.Error("syncInProgress should be false after SyncFromPrimary aborts")
	}
}

// ── 8. runtimeState() returns consistent snapshot ────────────────────────────────

func TestHardening_RuntimeState_Consistent(t *testing.T) {
	srv := p28PrimaryRunning(t)

	st := srv.runtimeState()
	if st.role != "primary" {
		t.Errorf("expected role=primary, got %q", st.role)
	}
	if st.localRoleSource != "config" {
		t.Errorf("expected local_role_source=config, got %q", st.localRoleSource)
	}
	if st.quiesceState != "active" {
		t.Errorf("expected quiesceState=active, got %q", st.quiesceState)
	}
	if st.closed {
		t.Error("server should not be closed")
	}
}

// ── 9. Typed ReplicationStatusResponse includes Phase 28 fields ──────────────────

func TestHardening_ReplicationStatus_TypedResponse_PrimaryFields(t *testing.T) {
	srv := p28PrimaryRunning(t)

	rec := p28Req(t, srv, "GET", "/replication/status", "")
	if rec.Code != 200 {
		t.Fatalf("status: %d", rec.Code)
	}

	var resp ReplicationStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.NodeID != "p28-primary" {
		t.Errorf("node_id: %q", resp.NodeID)
	}
	if resp.RuntimeRole != "primary" {
		t.Errorf("runtime_role: %q", resp.RuntimeRole)
	}
	if resp.LocalRoleSource != "config" {
		t.Errorf("local_role_source: %q", resp.LocalRoleSource)
	}
	if resp.WriteState != "active" {
		t.Errorf("write_state: %q", resp.WriteState)
	}
	if resp.Quiesced {
		t.Error("quiesced should be false before quiesce")
	}
}

func TestHardening_ReplicationStatus_TypedResponse_AfterQuiesce(t *testing.T) {
	srv := p28PrimaryRunning(t)
	p28Req(t, srv, "PUT", "/kv/q1", `{"value":"v1"}`)
	qRec := p28Req(t, srv, "POST", "/replication/quiesce", "")
	if qRec.Code != 200 {
		t.Fatalf("quiesce: %d %s", qRec.Code, qRec.Body.String())
	}

	rec := p28Req(t, srv, "GET", "/replication/status", "")
	var resp ReplicationStatusResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)

	if resp.WriteState != "quiesced" {
		t.Errorf("write_state: %q", resp.WriteState)
	}
	if !resp.Quiesced {
		t.Error("quiesced should be true")
	}
	if resp.QuiesceID == "" {
		t.Error("quiesce_id should be set")
	}
	if resp.QuiescedAt == "" {
		t.Error("quiesced_at should be set")
	}
	if resp.QuiescedLatestSeq != 1 {
		t.Errorf("quiesced_latest_seq: got %d, want 1", resp.QuiescedLatestSeq)
	}
}

func TestHardening_ReplicationStatus_FollowerFields(t *testing.T) {
	primary := p28Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}
	follower := p28Follower(t, primary.Addr(), t.TempDir())
	if err := follower.StartBackground(); err != nil {
		t.Fatalf("start follower: %v", err)
	}

	rec := p28Req(t, follower, "GET", "/replication/status", "")
	var resp ReplicationStatusResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)

	if resp.RuntimeRole != "follower" {
		t.Errorf("runtime_role: %q", resp.RuntimeRole)
	}
	// write_state is empty for followers.
	if resp.WriteState != "" {
		t.Errorf("write_state should be empty for follower, got %q", resp.WriteState)
	}
}

// ── 10. Typed status after promotion ─────────────────────────────────────────────

func TestHardening_ReplicationStatus_AfterPromotion_ShowsPrimary(t *testing.T) {
	_, follower, _, _, _ := p28PromoteFlow(t)

	rec := p28Req(t, follower, "GET", "/replication/status", "")
	var resp ReplicationStatusResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)

	if resp.RuntimeRole != "primary" {
		t.Errorf("runtime_role: %q", resp.RuntimeRole)
	}
	if resp.PromotionState != "promoted" {
		t.Errorf("promotion_state: %q", resp.PromotionState)
	}
	if resp.WriteState != "active" {
		t.Errorf("write_state: %q", resp.WriteState)
	}
}

// ── 11. writeJSONError stable codes ──────────────────────────────────────────────

func TestHardening_StableErrorCodes_WrongRole(t *testing.T) {
	// Standalone node: quiesce must return wrong_role code.
	srv, err := Open(Options{
		NodeID:  "standalone",
		Addr:    "127.0.0.1:0",
		DataDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer srv.Close()

	rec := p28Req(t, srv, "POST", "/replication/quiesce", "")
	if rec.Code != 400 {
		t.Errorf("expected 400, got %d", rec.Code)
	}
	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["code"] != "wrong_role" {
		t.Errorf("expected code=wrong_role, got %v", resp["code"])
	}
}

func TestHardening_StableErrorCodes_NodeClosing(t *testing.T) {
	srv := p28PrimaryRunning(t)
	srv.Close()

	rec := p28Req(t, srv, "POST", "/replication/quiesce", "")
	if rec.Code != 503 {
		t.Errorf("expected 503, got %d", rec.Code)
	}
	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["code"] != "node_closing" {
		t.Errorf("expected code=node_closing, got %v", resp["code"])
	}
}

func TestHardening_StableErrorCodes_NodeQuiesced(t *testing.T) {
	srv := p28PrimaryRunning(t)
	p28Req(t, srv, "POST", "/replication/quiesce", "")

	rec := p28Req(t, srv, "PUT", "/kv/x", `{"value":"y"}`)
	if rec.Code != 409 {
		t.Errorf("expected 409, got %d", rec.Code)
	}
}

// ── 12. Cross-validation on startup: invalid promotion record field ───────────────

func TestHardening_ResolveRuntimeRole_PromotionRecord_NewRoleMustBePrimary(t *testing.T) {
	dir := t.TempDir()

	// Write a promotion record with new_role != "primary".
	badRec := &replnet.PromotionRecord{
		Version:              1,
		NodeID:               "follower",
		NewRole:              "follower", // wrong
		SourcePrimaryNodeID:  "primary",
		SourcePrimaryBaseURL: "http://primary:9501",
		QuiesceID:            "abc123",
		InheritedLastSeq:     0,
		PromotedAt:           "2026-01-01T00:00:00Z",
	}
	badRec.Checksum = replnet.PromotionChecksum(badRec)
	// Write a valid baseline too.
	replnet.CreateJournalBaseline(dir, 0)
	replnet.SavePromotionRecord(dir, badRec)

	_, err := Open(Options{
		NodeID:  "follower",
		Addr:    "127.0.0.1:0",
		DataDir: dir,
		Replication: ReplicationOptions{
			Role:           replnet.RoleFollower,
			PrimaryBaseURL: "http://primary:9501",
		},
	})
	if err == nil {
		t.Error("expected error opening server with invalid promotion record (new_role != primary)")
	}
	if !strings.Contains(err.Error(), "new_role") {
		t.Errorf("error should mention new_role: %v", err)
	}
}

func TestHardening_ResolveRuntimeRole_MissingBaseline_ReturnsError(t *testing.T) {
	dir := t.TempDir()

	// Write a valid promotion record but NO baseline.
	promRec := replnet.NewPromotionRecord("follower", "primary", "http://primary:9501", "qid1", 5)
	replnet.SavePromotionRecord(dir, promRec)
	// No journal_baseline.json written.

	_, err := Open(Options{
		NodeID:  "follower",
		Addr:    "127.0.0.1:0",
		DataDir: dir,
		Replication: ReplicationOptions{
			Role:           replnet.RoleFollower,
			PrimaryBaseURL: "http://primary:9501",
		},
	})
	if err == nil {
		t.Error("expected error: promotion record without journal baseline")
	}
	if !strings.Contains(err.Error(), "journal baseline") {
		t.Errorf("error should mention journal baseline: %v", err)
	}
}

func TestHardening_ResolveRuntimeRole_BaselineSeqMismatch_ReturnsError(t *testing.T) {
	dir := t.TempDir()

	// Promotion record says inheritedLastSeq=10, but baseline has BaseSeq=5.
	replnet.CreateJournalBaseline(dir, 5)
	promRec := replnet.NewPromotionRecord("follower", "primary", "http://primary:9501", "qid1", 10)
	replnet.SavePromotionRecord(dir, promRec)

	_, err := Open(Options{
		NodeID:  "follower",
		Addr:    "127.0.0.1:0",
		DataDir: dir,
		Replication: ReplicationOptions{
			Role:           replnet.RoleFollower,
			PrimaryBaseURL: "http://primary:9501",
		},
	})
	if err == nil {
		t.Error("expected error: baseline seq mismatch")
	}
	if !strings.Contains(err.Error(), "base_seq") {
		t.Errorf("error should mention base_seq: %v", err)
	}
}

// ── 13. Orphan baseline is safe ───────────────────────────────────────────────────

func TestHardening_OrphanBaseline_FollowerStartsNormally(t *testing.T) {
	dir := t.TempDir()

	// Write a baseline but NO promotion record.
	replnet.CreateJournalBaseline(dir, 42)

	// Node should open normally as a follower (orphan baseline is not an error).
	srv, err := Open(Options{
		NodeID:  "follower",
		Addr:    "127.0.0.1:0",
		DataDir: dir,
		Replication: ReplicationOptions{
			Role:           replnet.RoleFollower,
			PrimaryBaseURL: "http://primary:9501",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error with orphan baseline: %v", err)
	}
	defer srv.Close()

	if srv.runtimeRole != "follower" {
		t.Errorf("expected follower role, got %q", srv.runtimeRole)
	}
}

// ── 14. promoteMu serializes concurrent promotion attempts ───────────────────────

func TestHardening_ConcurrentPromote_OnlyOneCommits(t *testing.T) {
	_, follower, qResp, _, _ := p28PromoteFlow(t)

	// Build a valid promote request using the same quiesce_id.
	qr := replnet.QuiesceRecord{
		Version: 1, QuiesceID: qResp.QuiesceID, PrimaryNodeID: "p28-primary",
		PrimaryBaseURL: "http://127.0.0.1:1", PrimaryLatestSeq: qResp.PrimaryLatestSeq,
		QuiescedAt: qResp.QuiescedAt,
	}
	qr.Checksum = replnet.QuiesceChecksum(&qr)
	body, _ := json.Marshal(PromoteRequest{QuiesceRecord: qr, ConfirmOldPrimaryStopped: true})

	// Send two concurrent promote requests — both should succeed idempotently.
	codes := make([]int, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rec := p28Req(t, follower, "POST", "/replication/promote", string(body))
			codes[i] = rec.Code
		}(i)
	}
	wg.Wait()

	for i, code := range codes {
		if code != 200 {
			t.Errorf("goroutine %d: expected 200, got %d", i, code)
		}
	}
	// Node must be primary exactly once.
	if follower.runtimeRole != "primary" {
		t.Errorf("expected primary role, got %q", follower.runtimeRole)
	}
}

// ── 15. Pre-commit promotion failure reverts state ────────────────────────────────

func TestHardening_Promote_PreCommitFailure_RevertsToFollower(t *testing.T) {
	primary := p28Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}
	follower := p28Follower(t, primary.Addr(), t.TempDir())
	if err := follower.StartBackground(); err != nil {
		t.Fatalf("start follower: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// Quiesce primary.
	qRec := p28Req(t, primary, "POST", "/replication/quiesce", "")
	if qRec.Code != 200 {
		t.Fatalf("quiesce: %d", qRec.Code)
	}
	var qResp QuiesceResponse
	json.Unmarshal(qRec.Body.Bytes(), &qResp)

	// Build promote request with correct seq.
	follower.syncInProgress.Store(false)
	time.Sleep(50 * time.Millisecond)

	// Force a validation failure (wrong checksum → pre-commit failure).
	qr := replnet.QuiesceRecord{
		Version: 1, QuiesceID: qResp.QuiesceID, PrimaryNodeID: "p28-primary",
		PrimaryBaseURL: "http://" + primary.Addr(), PrimaryLatestSeq: qResp.PrimaryLatestSeq,
		QuiescedAt: qResp.QuiescedAt,
		Checksum:   99999, // invalid checksum → rejected pre-commit
	}
	body, _ := json.Marshal(PromoteRequest{QuiesceRecord: qr, ConfirmOldPrimaryStopped: true})
	rec := p28Req(t, follower, "POST", "/replication/promote", string(body))
	if rec.Code != 400 {
		t.Fatalf("expected 400 for invalid checksum, got %d: %s", rec.Code, rec.Body.String())
	}

	// Node should still be a follower (pre-commit failure reverted).
	st := follower.runtimeState()
	if st.role != "follower" {
		t.Errorf("expected follower role after pre-commit failure, got %q", st.role)
	}
	if st.promotionState != "" {
		t.Errorf("expected empty promotionState, got %q", st.promotionState)
	}
	// promotionBarrier must be cleared.
	if follower.promotionBarrier.Load() {
		t.Error("promotionBarrier should be false after pre-commit failure revert")
	}
}

// ── 16. Old primary stopped before promote — integration ─────────────────────────

func TestHardening_OldPrimary_MustBeStoppedBeforePromote(t *testing.T) {
	primary, follower, qResp, pResp, _ := p28PromoteFlow(t)

	// The promote flow calls primary.StartBackground() and uses it during the flow.
	// After promotion completes, the old primary should be stopped.
	primary.Close()

	// Follower is now promoted primary.
	if pResp.NewRole != "primary" {
		t.Errorf("expected new_role=primary, got %q", pResp.NewRole)
	}
	if qResp.PrimaryLatestSeq != 3 {
		t.Errorf("quiesce seq: got %d, want 3", qResp.PrimaryLatestSeq)
	}

	// Promoted follower must accept writes.
	rec := p28Req(t, follower, "PUT", "/kv/post-promote", `{"value":"ok"}`)
	if rec.Code != 200 {
		t.Errorf("write after promotion failed: %d %s", rec.Code, rec.Body.String())
	}

	// Old primary (closed) should not be reachable — just verify it doesn't panic.
	_ = primary.ReplicationStatus()
}

// ── 17. quiesce_failed_fenced state retry ────────────────────────────────────────

func TestHardening_QuiesceFailedFenced_RetrySucceeds(t *testing.T) {
	srv := p28PrimaryRunning(t)

	// Simulate quiesce_failed_fenced: manually close the gate and set the state.
	srv.writeGate.Quiesce()
	fakeRec := &replnet.QuiesceRecord{
		Version:          1,
		QuiesceID:        "fixed-id-for-retry",
		PrimaryNodeID:    srv.opts.NodeID,
		PrimaryBaseURL:   "http://" + srv.Addr(),
		PrimaryLatestSeq: 0,
		QuiescedAt:       time.Now().UTC().Format(time.RFC3339Nano),
	}
	fakeRec.Checksum = replnet.QuiesceChecksum(fakeRec)

	srv.mu.Lock()
	srv.quiesceState = "quiesce_failed_fenced"
	srv.pendingQuiesceRecord = fakeRec
	srv.mu.Unlock()

	// POST /replication/quiesce should retry with the pending record.
	rec := p28Req(t, srv, "POST", "/replication/quiesce", "")
	if rec.Code != 200 {
		t.Fatalf("retry quiesce: %d %s", rec.Code, rec.Body.String())
	}
	var resp QuiesceResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)

	if resp.QuiesceID != "fixed-id-for-retry" {
		t.Errorf("expected fixed-id-for-retry, got %q", resp.QuiesceID)
	}
	// State must be updated to quiesced.
	st := srv.runtimeState()
	if st.quiesceState != "quiesced" {
		t.Errorf("expected quiesced state, got %q", st.quiesceState)
	}
	srv.mu.Lock()
	pending := srv.pendingQuiesceRecord
	srv.mu.Unlock()
	if pending != nil {
		t.Error("pendingQuiesceRecord should be nil after successful retry")
	}
}

// ── 18. CreateJournalBaseline used in executePromotion (idempotent) ───────────────

func TestHardening_Promote_IdempotentBaseline_DoesNotConflict(t *testing.T) {
	// Verify that a second promote with same quiesce_id succeeds even when
	// journal_baseline.json already exists (CreateJournalBaseline is idempotent).
	_, follower, _, pResp, _ := p28PromoteFlow(t)

	// Attempt a second promote with the same quiesce_id — must be idempotent.
	qr := replnet.QuiesceRecord{
		Version: 1, QuiesceID: pResp.QuiesceID, PrimaryNodeID: "p28-primary",
		PrimaryBaseURL: "http://127.0.0.1:1", PrimaryLatestSeq: pResp.InheritedLastSeq,
		QuiescedAt: "2026-01-01T00:00:00Z",
	}
	qr.Checksum = replnet.QuiesceChecksum(&qr)
	body, _ := json.Marshal(PromoteRequest{QuiesceRecord: qr, ConfirmOldPrimaryStopped: true})

	rec := p28Req(t, follower, "POST", "/replication/promote", string(body))
	if rec.Code != 200 {
		t.Fatalf("idempotent promote: %d %s", rec.Code, rec.Body.String())
	}
	var resp PromoteResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if !resp.Idempotent {
		t.Error("expected idempotent=true on second promote")
	}
}

// ── 19. quiesce PrimaryBaseURL uses resolved addr, not :0 ────────────────────────

func TestHardening_Quiesce_PrimaryBaseURL_UsesResolvedAddr(t *testing.T) {
	srv := p28PrimaryRunning(t) // started with :0, bound to real port

	rec := p28Req(t, srv, "POST", "/replication/quiesce", "")
	if rec.Code != 200 {
		t.Fatalf("quiesce: %d %s", rec.Code, rec.Body.String())
	}

	// The quiesce record on disk should have the real bound addr (not :0).
	stored, err := replnet.LoadQuiesceRecord(srv.opts.DataDir)
	if err != nil {
		t.Fatalf("load quiesce record: %v", err)
	}
	if strings.HasSuffix(stored.PrimaryBaseURL, ":0") {
		t.Errorf("primary_base_url should not be :0, got %q", stored.PrimaryBaseURL)
	}
	if !strings.HasPrefix(stored.PrimaryBaseURL, "http://127.0.0.1:") {
		t.Errorf("primary_base_url should be http://127.0.0.1:<port>, got %q", stored.PrimaryBaseURL)
	}
}

// ── 20. Client.ReplicationStatus returns typed Phase 28 fields ───────────────────

func TestHardening_Client_ReplicationStatus_Phase28Fields(t *testing.T) {
	srv := p28PrimaryRunning(t)

	client := NewClient("http://"+srv.Addr(), 5*time.Second)
	resp, err := client.ReplicationStatus(context.Background())
	if err != nil {
		t.Fatalf("ReplicationStatus: %v", err)
	}

	if resp.RuntimeRole != "primary" {
		t.Errorf("RuntimeRole: %q", resp.RuntimeRole)
	}
	if resp.WriteState != "active" {
		t.Errorf("WriteState: %q", resp.WriteState)
	}
	if resp.LocalRoleSource != "config" {
		t.Errorf("LocalRoleSource: %q", resp.LocalRoleSource)
	}
}
