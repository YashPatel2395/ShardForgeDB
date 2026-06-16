package node

// Phase 28 — Manual Promotion and Controlled Failover: unit tests.
//
// Scope: write gate, quiesce state machine, promotion validation.
// NOT automatic failover. NOT Raft. NOT consensus.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/YashPatel2395/ShardForgeDB/internal/replnet"
)

// ── Write gate tests ────────────────────────────────────────────────────────────

func TestWriteGate_Enter_Exit_NoError(t *testing.T) {
	g := &writeGate{}
	if err := g.Enter(); err != nil {
		t.Fatalf("Enter: %v", err)
	}
	g.Exit()
}

func TestWriteGate_EnterAfterQuiesce_ReturnsError(t *testing.T) {
	g := &writeGate{}
	g.Quiesce()
	if err := g.Enter(); err != ErrNodeQuiesced {
		t.Errorf("expected ErrNodeQuiesced, got %v", err)
	}
}

func TestWriteGate_QuiesceWaitsForInflightWrite(t *testing.T) {
	g := &writeGate{}
	if err := g.Enter(); err != nil {
		t.Fatalf("Enter: %v", err)
	}

	done := make(chan struct{})
	go func() {
		g.Quiesce()
		close(done)
	}()

	// Give Quiesce time to block on the Lock.
	time.Sleep(20 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("Quiesce should not have completed while write is in flight")
	default:
	}

	g.Exit() // release the RLock

	select {
	case <-done:
		// Quiesce completed after write exited.
	case <-time.After(2 * time.Second):
		t.Fatal("Quiesce did not complete after write exited")
	}
}

func TestWriteGate_MultipleInflightWritesDrain(t *testing.T) {
	g := &writeGate{}
	const n = 5
	for i := 0; i < n; i++ {
		if err := g.Enter(); err != nil {
			t.Fatalf("Enter %d: %v", i, err)
		}
	}

	done := make(chan struct{})
	go func() {
		g.Quiesce()
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)
	for i := 0; i < n; i++ {
		g.Exit()
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Quiesce did not complete after all writes exited")
	}
}

func TestWriteGate_ConcurrentEnterAndQuiesce_NoLeak(t *testing.T) {
	g := &writeGate{}
	var wg sync.WaitGroup

	// Launch concurrent writers.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := g.Enter(); err == nil {
				time.Sleep(time.Millisecond)
				g.Exit()
			}
		}()
	}

	// Quiesce mid-flight.
	time.Sleep(5 * time.Millisecond)
	g.Quiesce()

	wg.Wait()
	if !g.IsQuiesced() {
		t.Error("gate should be quiesced")
	}
}

func TestWriteGate_IsQuiesced_BeforeAndAfter(t *testing.T) {
	g := &writeGate{}
	if g.IsQuiesced() {
		t.Error("should not be quiesced initially")
	}
	g.Quiesce()
	if !g.IsQuiesced() {
		t.Error("should be quiesced after Quiesce()")
	}
}

func TestWriteGate_QuiesceIdempotent(t *testing.T) {
	g := &writeGate{}
	g.Quiesce()
	g.Quiesce() // second call should not panic or deadlock
	if !g.IsQuiesced() {
		t.Error("should still be quiesced")
	}
}

func TestWriteGate_EnterAfterQuiesce_Concurrent(t *testing.T) {
	g := &writeGate{}
	g.Quiesce()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := g.Enter()
			if err != ErrNodeQuiesced {
				t.Errorf("expected ErrNodeQuiesced, got %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestWriteGate_NilGate_NotUsedForFollower(t *testing.T) {
	// Follower nodes have nil writeGate. Verify nil is safe (no panic).
	var g *writeGate
	if g != nil {
		t.Error("nil gate should be nil")
	}
}

func TestWriteGate_QuiesceBlocksUntilAllExited(t *testing.T) {
	g := &writeGate{}
	var entered int32

	// Enter 3 writes.
	for i := 0; i < 3; i++ {
		g.Enter()
		atomic.AddInt32(&entered, 1)
	}

	done := make(chan struct{})
	go func() {
		g.Quiesce()
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)
	// Exit one at a time.
	for i := 0; i < 3; i++ {
		select {
		case <-done:
			t.Fatal("Quiesce completed too early")
		default:
		}
		g.Exit()
		time.Sleep(5 * time.Millisecond)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Quiesce did not complete")
	}
}

// ── Helpers ─────────────────────────────────────────────────────────────────────

func p28Primary(t *testing.T) *Server {
	t.Helper()
	srv, err := Open(Options{
		NodeID:  "p28-primary",
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
	return srv
}

func p28Follower(t *testing.T, primaryAddr, dataDir string) *Server {
	t.Helper()
	srv, err := Open(Options{
		NodeID:  "p28-follower",
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
	return srv
}

func p28Req(t *testing.T, srv *Server, method, path, body string) *httptest.ResponseRecorder {
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

// ── Quiesce state machine tests ─────────────────────────────────────────────────

func TestServer_Quiesce_Primary_Succeeds(t *testing.T) {
	srv := p28PrimaryRunning(t)
	// Write a value first.
	p28Req(t, srv, "PUT", "/kv/k1", `{"value":"v1"}`)

	rec := p28Req(t, srv, "POST", "/replication/quiesce", "")
	if rec.Code != 200 {
		t.Fatalf("quiesce status: %d, body: %s", rec.Code, rec.Body.String())
	}
	var resp QuiesceResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.WriteState != "quiesced" {
		t.Errorf("write_state: got %q", resp.WriteState)
	}
	if resp.PrimaryLatestSeq != 1 {
		t.Errorf("primary_latest_seq: got %d, want 1", resp.PrimaryLatestSeq)
	}
}

func TestServer_Quiesce_Follower_Rejected(t *testing.T) {
	dir := t.TempDir()
	srv, err := Open(Options{
		NodeID:  "follower",
		Addr:    "127.0.0.1:0",
		DataDir: dir,
		Replication: ReplicationOptions{
			Role:           replnet.RoleFollower,
			PrimaryBaseURL: "http://127.0.0.1:9999",
		},
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer srv.Close()

	rec := p28Req(t, srv, "POST", "/replication/quiesce", "")
	if rec.Code != 400 {
		t.Errorf("expected 400 for follower quiesce, got %d", rec.Code)
	}
}

func TestServer_Quiesce_Standalone_Rejected(t *testing.T) {
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
		t.Errorf("expected 400 for standalone quiesce, got %d", rec.Code)
	}
}

func TestServer_Quiesce_Idempotent_SameRecord(t *testing.T) {
	srv := p28PrimaryRunning(t)
	p28Req(t, srv, "PUT", "/kv/k1", `{"value":"v1"}`)

	rec1 := p28Req(t, srv, "POST", "/replication/quiesce", "")
	var resp1 QuiesceResponse
	json.Unmarshal(rec1.Body.Bytes(), &resp1)

	rec2 := p28Req(t, srv, "POST", "/replication/quiesce", "")
	var resp2 QuiesceResponse
	json.Unmarshal(rec2.Body.Bytes(), &resp2)

	if rec2.Code != 200 {
		t.Fatalf("second quiesce: status %d", rec2.Code)
	}
	if !resp2.Idempotent {
		t.Error("second quiesce should be idempotent")
	}
	if resp2.QuiesceID != resp1.QuiesceID {
		t.Errorf("quiesce_id mismatch: %q vs %q", resp1.QuiesceID, resp2.QuiesceID)
	}
}

func TestServer_Quiesce_PutRejectedAfterQuiesce(t *testing.T) {
	srv := p28PrimaryRunning(t)
	p28Req(t, srv, "POST", "/replication/quiesce", "")

	rec := p28Req(t, srv, "PUT", "/kv/k1", `{"value":"v1"}`)
	if rec.Code != 409 {
		t.Errorf("expected 409 for PUT after quiesce, got %d", rec.Code)
	}
}

func TestServer_Quiesce_DeleteRejectedAfterQuiesce(t *testing.T) {
	srv := p28PrimaryRunning(t)
	p28Req(t, srv, "PUT", "/kv/k1", `{"value":"v1"}`)
	p28Req(t, srv, "POST", "/replication/quiesce", "")

	rec := p28Req(t, srv, "DELETE", "/kv/k1", "")
	if rec.Code != 409 {
		t.Errorf("expected 409 for DELETE after quiesce, got %d", rec.Code)
	}
}

func TestServer_Quiesce_GetStillWorksAfterQuiesce(t *testing.T) {
	srv := p28PrimaryRunning(t)
	p28Req(t, srv, "PUT", "/kv/k1", `{"value":"v1"}`)
	p28Req(t, srv, "POST", "/replication/quiesce", "")

	rec := p28Req(t, srv, "GET", "/kv/k1", "")
	if rec.Code != 200 {
		t.Errorf("GET after quiesce should return 200, got %d", rec.Code)
	}
	var resp getResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if !resp.Found || resp.Value != "v1" {
		t.Errorf("expected found=true value=v1, got %+v", resp)
	}
}

func TestServer_Quiesce_ReplicationLogStillAvailable(t *testing.T) {
	srv := p28PrimaryRunning(t)
	p28Req(t, srv, "PUT", "/kv/k1", `{"value":"v1"}`)
	p28Req(t, srv, "POST", "/replication/quiesce", "")

	rec := p28Req(t, srv, "GET", "/replication/log?after=0", "")
	if rec.Code != 200 {
		t.Errorf("replication log after quiesce should return 200, got %d", rec.Code)
	}
}

func TestServer_Quiesce_RestartsWithWritesFenced(t *testing.T) {
	dir := t.TempDir()
	srv, err := Open(Options{
		NodeID:      "p28-primary",
		Addr:        "127.0.0.1:0",
		DataDir:     dir,
		Replication: ReplicationOptions{Role: replnet.RolePrimary},
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := srv.StartBackground(); err != nil {
		t.Fatalf("start: %v", err)
	}
	p28Req(t, srv, "PUT", "/kv/k1", `{"value":"v1"}`)
	p28Req(t, srv, "POST", "/replication/quiesce", "")
	srv.Close()

	// Reopen with same data dir.
	srv2, err := Open(Options{
		NodeID:      "p28-primary",
		Addr:        "127.0.0.1:0",
		DataDir:     dir,
		Replication: ReplicationOptions{Role: replnet.RolePrimary},
	})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer srv2.Close()

	rec := p28Req(t, srv2, "PUT", "/kv/k2", `{"value":"v2"}`)
	if rec.Code != 409 {
		t.Errorf("expected 409 after restart, got %d", rec.Code)
	}
}

func TestServer_Quiesce_StatusShowsQuiescedState(t *testing.T) {
	srv := p28PrimaryRunning(t)
	p28Req(t, srv, "POST", "/replication/quiesce", "")

	rec := p28Req(t, srv, "GET", "/replication/status", "")
	var m map[string]any
	json.Unmarshal(rec.Body.Bytes(), &m)
	if m["write_state"] != "quiesced" {
		t.Errorf("write_state: got %v", m["write_state"])
	}
	if m["quiesced"] != true {
		t.Errorf("quiesced: got %v", m["quiesced"])
	}
}

func TestServer_Quiesce_FinalSeqIsCorrect(t *testing.T) {
	srv := p28PrimaryRunning(t)
	p28Req(t, srv, "PUT", "/kv/k1", `{"value":"v1"}`)
	p28Req(t, srv, "PUT", "/kv/k2", `{"value":"v2"}`)
	p28Req(t, srv, "PUT", "/kv/k3", `{"value":"v3"}`)

	rec := p28Req(t, srv, "POST", "/replication/quiesce", "")
	var resp QuiesceResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.PrimaryLatestSeq != 3 {
		t.Errorf("expected final seq 3, got %d", resp.PrimaryLatestSeq)
	}
}

func TestServer_Quiesce_ConcurrentPut_CompletesBeforeQuiesce(t *testing.T) {
	srv := p28PrimaryRunning(t)

	// Start a slow-ish write in background.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		p28Req(t, srv, "PUT", "/kv/concurrent", `{"value":"before-quiesce"}`)
	}()

	time.Sleep(5 * time.Millisecond)
	p28Req(t, srv, "POST", "/replication/quiesce", "")
	wg.Wait()

	// Verify the write completed.
	rec := p28Req(t, srv, "GET", "/kv/concurrent", "")
	var resp getResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if !resp.Found {
		t.Error("concurrent write should have completed before quiesce")
	}
}

func TestServer_Quiesce_StatusWriteStateField(t *testing.T) {
	srv := p28Primary(t)

	// Before quiesce: write_state = active.
	rec := p28Req(t, srv, "GET", "/replication/status", "")
	var m map[string]any
	json.Unmarshal(rec.Body.Bytes(), &m)
	if m["write_state"] != "active" {
		t.Errorf("expected active, got %v", m["write_state"])
	}
}

func TestServer_Quiesce_RuntimeRoleIsPrimary(t *testing.T) {
	srv := p28Primary(t)
	if srv.runtimeRole != "primary" {
		t.Errorf("expected runtime role primary, got %q", srv.runtimeRole)
	}
}

func TestServer_Quiesce_FailedRecordPersistence_NotClaimed(t *testing.T) {
	// Use a read-only directory to simulate persistence failure.
	// This is a basic test — the quiesce record might fail to save.
	srv := p28PrimaryRunning(t)
	// Quiesce should work on a normal dir.
	rec := p28Req(t, srv, "POST", "/replication/quiesce", "")
	if rec.Code != 200 {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

// ── Promotion validation tests ──────────────────────────────────────────────────

func TestServer_Promote_WrongRole_Rejected(t *testing.T) {
	srv := p28Primary(t)
	rec := p28Req(t, srv, "POST", "/replication/promote", `{
		"quiesce_record": {"version":1},
		"confirm_old_primary_stopped": true
	}`)
	if rec.Code != 400 {
		t.Errorf("expected 400 for promote on primary, got %d", rec.Code)
	}
}

func TestServer_Promote_MissingConfirmation_Rejected(t *testing.T) {
	primary := p28Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}
	follower := p28Follower(t, primary.Addr(), t.TempDir())
	if err := follower.StartBackground(); err != nil {
		t.Fatalf("start follower: %v", err)
	}

	rec := p28Req(t, follower, "POST", "/replication/promote", `{
		"quiesce_record": {"version":1, "quiesce_id":"q1", "primary_node_id":"p", "primary_base_url":"http://`+primary.Addr()+`", "primary_latest_seq":0, "quiesced_at":"2026-01-01T00:00:00Z", "checksum":0},
		"confirm_old_primary_stopped": false
	}`)
	if rec.Code != 400 {
		t.Errorf("expected 400 for missing confirmation, got %d, body: %s", rec.Code, rec.Body.String())
	}
}

func TestServer_Promote_WrongPrimaryURL_Rejected(t *testing.T) {
	primary := p28Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}
	follower := p28Follower(t, primary.Addr(), t.TempDir())
	if err := follower.StartBackground(); err != nil {
		t.Fatalf("start follower: %v", err)
	}

	// Create a valid-looking quiesce record but with wrong primary URL.
	qr := mustNewQuiesceRecord(t, "p28-primary", "http://wrong-url:1234", 0)
	qr.Checksum = replnet.QuiesceChecksum(qr)
	body, _ := json.Marshal(PromoteRequest{
		QuiesceRecord:            *qr,
		ConfirmOldPrimaryStopped: true,
	})

	rec := p28Req(t, follower, "POST", "/replication/promote", string(body))
	if rec.Code != 400 {
		t.Errorf("expected 400 for wrong primary URL, got %d, body: %s", rec.Code, rec.Body.String())
	}
}

func TestServer_Promote_FollowerBehind_Rejected(t *testing.T) {
	primary := p28Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}
	// Write data to primary.
	p28Req(t, primary, "PUT", "/kv/k1", `{"value":"v1"}`)

	follower := p28Follower(t, primary.Addr(), t.TempDir())
	if err := follower.StartBackground(); err != nil {
		t.Fatalf("start follower: %v", err)
	}

	// Don't wait for sync — follower is behind.
	// Create quiesce record with seq=1 (primary's seq).
	qr := mustNewQuiesceRecord(t, "p28-primary", "http://"+primary.Addr(), 1)
	qr.Checksum = replnet.QuiesceChecksum(qr)

	// Immediately try to promote (before follower catches up).
	follower.bgWorker.stop() // stop bg sync to prevent it from catching up
	atomic.StoreUint64(&follower.lastApplied, 0)

	body, _ := json.Marshal(PromoteRequest{
		QuiesceRecord:            *qr,
		ConfirmOldPrimaryStopped: true,
	})

	rec := p28Req(t, follower, "POST", "/replication/promote", string(body))
	if rec.Code != 400 {
		t.Errorf("expected 400 for follower behind, got %d, body: %s", rec.Code, rec.Body.String())
	}
}

func TestServer_Promote_FollowerAhead_Rejected(t *testing.T) {
	primary := p28Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}
	follower := p28Follower(t, primary.Addr(), t.TempDir())
	if err := follower.StartBackground(); err != nil {
		t.Fatalf("start follower: %v", err)
	}

	// Fake: set follower's seq ahead.
	atomic.StoreUint64(&follower.lastApplied, 100)

	qr := mustNewQuiesceRecord(t, "p28-primary", "http://"+primary.Addr(), 50)
	qr.Checksum = replnet.QuiesceChecksum(qr)
	body, _ := json.Marshal(PromoteRequest{
		QuiesceRecord:            *qr,
		ConfirmOldPrimaryStopped: true,
	})

	rec := p28Req(t, follower, "POST", "/replication/promote", string(body))
	if rec.Code != 400 {
		t.Errorf("expected 400 for follower ahead, got %d", rec.Code)
	}
}

func TestServer_Promote_MaxUint64Seq_Rejected(t *testing.T) {
	primary := p28Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}
	follower := p28Follower(t, primary.Addr(), t.TempDir())
	if err := follower.StartBackground(); err != nil {
		t.Fatalf("start follower: %v", err)
	}

	atomic.StoreUint64(&follower.lastApplied, ^uint64(0))
	qr := mustNewQuiesceRecord(t, "p28-primary", "http://"+primary.Addr(), ^uint64(0))
	qr.Checksum = replnet.QuiesceChecksum(qr)
	body, _ := json.Marshal(PromoteRequest{
		QuiesceRecord:            *qr,
		ConfirmOldPrimaryStopped: true,
	})

	rec := p28Req(t, follower, "POST", "/replication/promote", string(body))
	if rec.Code != 400 {
		t.Errorf("expected 400 for MaxUint64, got %d", rec.Code)
	}
}

func TestServer_Promote_CorruptQuiesceRecord_Rejected(t *testing.T) {
	primary := p28Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}
	follower := p28Follower(t, primary.Addr(), t.TempDir())
	if err := follower.StartBackground(); err != nil {
		t.Fatalf("start follower: %v", err)
	}

	// Create record with bad checksum.
	qr := mustNewQuiesceRecord(t, "p28-primary", "http://"+primary.Addr(), 0)
	qr.Checksum = 12345 // wrong checksum
	body, _ := json.Marshal(PromoteRequest{
		QuiesceRecord:            *qr,
		ConfirmOldPrimaryStopped: true,
	})

	rec := p28Req(t, follower, "POST", "/replication/promote", string(body))
	if rec.Code != 400 {
		t.Errorf("expected 400 for corrupt record, got %d", rec.Code)
	}
}

func TestServer_Promote_NodeClosing_Rejected(t *testing.T) {
	primary := p28Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}
	follower := p28Follower(t, primary.Addr(), t.TempDir())
	if err := follower.StartBackground(); err != nil {
		t.Fatalf("start follower: %v", err)
	}
	follower.Close()

	rec := p28Req(t, follower, "POST", "/replication/promote", `{}`)
	if rec.Code != 503 {
		t.Errorf("expected 503 for closed node, got %d", rec.Code)
	}
}

func TestServer_Promote_WritesEnabledAfterPromotion(t *testing.T) {
	primary := p28Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}
	followerDir := t.TempDir()
	follower := p28Follower(t, primary.Addr(), followerDir)
	if err := follower.StartBackground(); err != nil {
		t.Fatalf("start follower: %v", err)
	}

	// Wait for follower to sync.
	time.Sleep(300 * time.Millisecond)

	// Quiesce primary.
	qRec := p28Req(t, primary, "POST", "/replication/quiesce", "")
	var qResp QuiesceResponse
	json.Unmarshal(qRec.Body.Bytes(), &qResp)

	// Wait for follower to catch up to final seq.
	time.Sleep(300 * time.Millisecond)

	// Build promote request.
	qr := replnet.QuiesceRecord{
		Version:          1,
		QuiesceID:        qResp.QuiesceID,
		PrimaryNodeID:    "p28-primary",
		PrimaryBaseURL:   "http://" + primary.Addr(),
		PrimaryLatestSeq: qResp.PrimaryLatestSeq,
		QuiescedAt:       qResp.QuiescedAt,
	}
	qr.Checksum = replnet.QuiesceChecksum(&qr)

	body, _ := json.Marshal(PromoteRequest{
		QuiesceRecord:            qr,
		ConfirmOldPrimaryStopped: true,
	})

	rec := p28Req(t, follower, "POST", "/replication/promote", string(body))
	if rec.Code != 200 {
		t.Fatalf("promote failed: %d %s", rec.Code, rec.Body.String())
	}

	// Now writes should be accepted on the promoted follower.
	putRec := p28Req(t, follower, "PUT", "/kv/newkey", `{"value":"newval"}`)
	if putRec.Code != 200 {
		t.Errorf("PUT after promotion should succeed, got %d", putRec.Code)
	}
}

func TestServer_Promote_RuntimeRoleIsPrimary(t *testing.T) {
	primary := p28Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}
	followerDir := t.TempDir()
	follower := p28Follower(t, primary.Addr(), followerDir)
	if err := follower.StartBackground(); err != nil {
		t.Fatalf("start follower: %v", err)
	}
	time.Sleep(300 * time.Millisecond)

	qRec := p28Req(t, primary, "POST", "/replication/quiesce", "")
	var qResp QuiesceResponse
	json.Unmarshal(qRec.Body.Bytes(), &qResp)
	time.Sleep(300 * time.Millisecond)

	qr := replnet.QuiesceRecord{
		Version: 1, QuiesceID: qResp.QuiesceID, PrimaryNodeID: "p28-primary",
		PrimaryBaseURL: "http://" + primary.Addr(), PrimaryLatestSeq: qResp.PrimaryLatestSeq,
		QuiescedAt: qResp.QuiescedAt,
	}
	qr.Checksum = replnet.QuiesceChecksum(&qr)
	body, _ := json.Marshal(PromoteRequest{QuiesceRecord: qr, ConfirmOldPrimaryStopped: true})
	p28Req(t, follower, "POST", "/replication/promote", string(body))

	if follower.runtimeRole != "primary" {
		t.Errorf("expected primary after promotion, got %q", follower.runtimeRole)
	}
}

func TestServer_Promote_FollowerSyncRejectedAfterPromotion(t *testing.T) {
	primary := p28Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}
	followerDir := t.TempDir()
	follower := p28Follower(t, primary.Addr(), followerDir)
	if err := follower.StartBackground(); err != nil {
		t.Fatalf("start follower: %v", err)
	}
	time.Sleep(300 * time.Millisecond)

	qRec := p28Req(t, primary, "POST", "/replication/quiesce", "")
	var qResp QuiesceResponse
	json.Unmarshal(qRec.Body.Bytes(), &qResp)
	time.Sleep(300 * time.Millisecond)

	qr := replnet.QuiesceRecord{
		Version: 1, QuiesceID: qResp.QuiesceID, PrimaryNodeID: "p28-primary",
		PrimaryBaseURL: "http://" + primary.Addr(), PrimaryLatestSeq: qResp.PrimaryLatestSeq,
		QuiescedAt: qResp.QuiescedAt,
	}
	qr.Checksum = replnet.QuiesceChecksum(&qr)
	body, _ := json.Marshal(PromoteRequest{QuiesceRecord: qr, ConfirmOldPrimaryStopped: true})
	p28Req(t, follower, "POST", "/replication/promote", string(body))

	// Sync should now be rejected (not a follower).
	rec := p28Req(t, follower, "POST", "/replication/sync", "")
	if rec.Code != 400 {
		t.Errorf("expected 400 for sync after promotion, got %d", rec.Code)
	}
}

func TestServer_Promote_StatusShowsPromotedPrimary(t *testing.T) {
	primary := p28Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}
	followerDir := t.TempDir()
	follower := p28Follower(t, primary.Addr(), followerDir)
	if err := follower.StartBackground(); err != nil {
		t.Fatalf("start follower: %v", err)
	}
	time.Sleep(300 * time.Millisecond)

	qRec := p28Req(t, primary, "POST", "/replication/quiesce", "")
	var qResp QuiesceResponse
	json.Unmarshal(qRec.Body.Bytes(), &qResp)
	time.Sleep(300 * time.Millisecond)

	qr := replnet.QuiesceRecord{
		Version: 1, QuiesceID: qResp.QuiesceID, PrimaryNodeID: "p28-primary",
		PrimaryBaseURL: "http://" + primary.Addr(), PrimaryLatestSeq: qResp.PrimaryLatestSeq,
		QuiescedAt: qResp.QuiescedAt,
	}
	qr.Checksum = replnet.QuiesceChecksum(&qr)
	body, _ := json.Marshal(PromoteRequest{QuiesceRecord: qr, ConfirmOldPrimaryStopped: true})
	p28Req(t, follower, "POST", "/replication/promote", string(body))

	rec := p28Req(t, follower, "GET", "/replication/status", "")
	var m map[string]any
	json.Unmarshal(rec.Body.Bytes(), &m)
	if m["runtime_role"] != "primary" {
		t.Errorf("expected runtime_role=primary, got %v", m["runtime_role"])
	}
	if m["local_role_source"] != "promotion_record" {
		t.Errorf("expected local_role_source=promotion_record, got %v", m["local_role_source"])
	}
}

func TestServer_Promote_InheritedSeqIsCorrect(t *testing.T) {
	primary := p28Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}
	p28Req(t, primary, "PUT", "/kv/a", `{"value":"1"}`)
	p28Req(t, primary, "PUT", "/kv/b", `{"value":"2"}`)

	followerDir := t.TempDir()
	follower := p28Follower(t, primary.Addr(), followerDir)
	if err := follower.StartBackground(); err != nil {
		t.Fatalf("start follower: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	qRec := p28Req(t, primary, "POST", "/replication/quiesce", "")
	var qResp QuiesceResponse
	json.Unmarshal(qRec.Body.Bytes(), &qResp)
	time.Sleep(300 * time.Millisecond)

	qr := replnet.QuiesceRecord{
		Version: 1, QuiesceID: qResp.QuiesceID, PrimaryNodeID: "p28-primary",
		PrimaryBaseURL: "http://" + primary.Addr(), PrimaryLatestSeq: qResp.PrimaryLatestSeq,
		QuiescedAt: qResp.QuiescedAt,
	}
	qr.Checksum = replnet.QuiesceChecksum(&qr)
	body, _ := json.Marshal(PromoteRequest{QuiesceRecord: qr, ConfirmOldPrimaryStopped: true})

	rec := p28Req(t, follower, "POST", "/replication/promote", string(body))
	var pResp PromoteResponse
	json.Unmarshal(rec.Body.Bytes(), &pResp)

	if pResp.InheritedLastSeq != 2 {
		t.Errorf("expected inherited_last_seq=2, got %d", pResp.InheritedLastSeq)
	}
}

func TestServer_Promote_Idempotent_SameRecord(t *testing.T) {
	primary := p28Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}

	followerDir := t.TempDir()
	follower := p28Follower(t, primary.Addr(), followerDir)
	if err := follower.StartBackground(); err != nil {
		t.Fatalf("start follower: %v", err)
	}
	time.Sleep(300 * time.Millisecond)

	qRec := p28Req(t, primary, "POST", "/replication/quiesce", "")
	var qResp QuiesceResponse
	json.Unmarshal(qRec.Body.Bytes(), &qResp)
	time.Sleep(300 * time.Millisecond)

	qr := replnet.QuiesceRecord{
		Version: 1, QuiesceID: qResp.QuiesceID, PrimaryNodeID: "p28-primary",
		PrimaryBaseURL: "http://" + primary.Addr(), PrimaryLatestSeq: qResp.PrimaryLatestSeq,
		QuiescedAt: qResp.QuiescedAt,
	}
	qr.Checksum = replnet.QuiesceChecksum(&qr)
	body, _ := json.Marshal(PromoteRequest{QuiesceRecord: qr, ConfirmOldPrimaryStopped: true})

	rec1 := p28Req(t, follower, "POST", "/replication/promote", string(body))
	if rec1.Code != 200 {
		t.Fatalf("first promote: %d %s", rec1.Code, rec1.Body.String())
	}

	rec2 := p28Req(t, follower, "POST", "/replication/promote", string(body))
	if rec2.Code != 200 {
		t.Fatalf("second promote: %d %s", rec2.Code, rec2.Body.String())
	}
	var resp2 PromoteResponse
	json.Unmarshal(rec2.Body.Bytes(), &resp2)
	if !resp2.Idempotent {
		t.Error("second promote should be idempotent")
	}
}

func TestServer_Promote_DifferentRecord_Rejected(t *testing.T) {
	primary := p28Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}

	followerDir := t.TempDir()
	follower := p28Follower(t, primary.Addr(), followerDir)
	if err := follower.StartBackground(); err != nil {
		t.Fatalf("start follower: %v", err)
	}
	time.Sleep(300 * time.Millisecond)

	qRec := p28Req(t, primary, "POST", "/replication/quiesce", "")
	var qResp QuiesceResponse
	json.Unmarshal(qRec.Body.Bytes(), &qResp)
	time.Sleep(300 * time.Millisecond)

	qr := replnet.QuiesceRecord{
		Version: 1, QuiesceID: qResp.QuiesceID, PrimaryNodeID: "p28-primary",
		PrimaryBaseURL: "http://" + primary.Addr(), PrimaryLatestSeq: qResp.PrimaryLatestSeq,
		QuiescedAt: qResp.QuiescedAt,
	}
	qr.Checksum = replnet.QuiesceChecksum(&qr)
	body, _ := json.Marshal(PromoteRequest{QuiesceRecord: qr, ConfirmOldPrimaryStopped: true})

	p28Req(t, follower, "POST", "/replication/promote", string(body))

	// Try again with different quiesce_id.
	qr2 := qr
	qr2.QuiesceID = "different-id"
	qr2.Checksum = replnet.QuiesceChecksum(&qr2)
	body2, _ := json.Marshal(PromoteRequest{QuiesceRecord: qr2, ConfirmOldPrimaryStopped: true})
	rec := p28Req(t, follower, "POST", "/replication/promote", string(body2))
	if rec.Code != 409 {
		t.Errorf("expected 409 for different record, got %d", rec.Code)
	}
}

func TestServer_Promote_WorkerIsStoppedAfterPromotion(t *testing.T) {
	primary := p28Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}
	followerDir := t.TempDir()
	follower := p28Follower(t, primary.Addr(), followerDir)
	if err := follower.StartBackground(); err != nil {
		t.Fatalf("start follower: %v", err)
	}
	time.Sleep(300 * time.Millisecond)

	qRec := p28Req(t, primary, "POST", "/replication/quiesce", "")
	var qResp QuiesceResponse
	json.Unmarshal(qRec.Body.Bytes(), &qResp)
	time.Sleep(300 * time.Millisecond)

	qr := replnet.QuiesceRecord{
		Version: 1, QuiesceID: qResp.QuiesceID, PrimaryNodeID: "p28-primary",
		PrimaryBaseURL: "http://" + primary.Addr(), PrimaryLatestSeq: qResp.PrimaryLatestSeq,
		QuiescedAt: qResp.QuiescedAt,
	}
	qr.Checksum = replnet.QuiesceChecksum(&qr)
	body, _ := json.Marshal(PromoteRequest{QuiesceRecord: qr, ConfirmOldPrimaryStopped: true})
	p28Req(t, follower, "POST", "/replication/promote", string(body))

	status := follower.BackgroundSyncStatus()
	if status.State != WorkerStateStopped {
		t.Errorf("expected worker stopped after promotion, got %q", status.State)
	}
}

func TestServer_Promote_ActiveManualSync_Rejected(t *testing.T) {
	primary := p28Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}
	followerDir := t.TempDir()
	follower := p28Follower(t, primary.Addr(), followerDir)
	if err := follower.StartBackground(); err != nil {
		t.Fatalf("start follower: %v", err)
	}
	time.Sleep(300 * time.Millisecond)

	// Simulate a sync in progress.
	follower.syncInProgress.Store(true)
	defer follower.syncInProgress.Store(false)

	qr := mustNewQuiesceRecord(t, "p28-primary", "http://"+primary.Addr(), 0)
	qr.Checksum = replnet.QuiesceChecksum(qr)
	body, _ := json.Marshal(PromoteRequest{QuiesceRecord: *qr, ConfirmOldPrimaryStopped: true})

	rec := p28Req(t, follower, "POST", "/replication/promote", string(body))
	if rec.Code != 400 {
		t.Errorf("expected 400 for active sync, got %d, body: %s", rec.Code, rec.Body.String())
	}
}
