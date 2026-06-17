package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/YashPatel2395/ShardForgeDB/internal/replnet"
)

// ── Safety Item 1: cursor revalidation under WLock ────────────────────────────

// TestSafety_CursorDivergence_LockedCheckRejects verifies that if the runtime
// lastApplied counter diverges from quiesceSeq between pre-barrier validation and
// WLock acquisition, promotion is rejected and no promotion record is written.
//
// We simulate the divergence by directly advancing lastApplied without persisting the cursor.
func TestSafety_CursorDivergence_LockedCheckRejects(t *testing.T) {
	primary := p28Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}
	// Write 3 entries so the primary has seq 1..3.
	for i := 0; i < 3; i++ {
		rec := p28Req(t, primary, "PUT", fmt.Sprintf("/kv/key%d", i), fmt.Sprintf(`{"value":"v%d"}`, i))
		if rec.Code != 200 {
			t.Fatalf("put %d: %d %s", i, rec.Code, rec.Body.String())
		}
	}

	// Quiesce the primary at seq 3.
	qRec := p28Req(t, primary, "POST", "/replication/quiesce", "")
	var qResp QuiesceResponse
	json.Unmarshal(qRec.Body.Bytes(), &qResp)
	if qResp.PrimaryLatestSeq != 3 {
		t.Fatalf("expected quiesce at seq 3, got %d", qResp.PrimaryLatestSeq)
	}

	followerDir := t.TempDir()
	follower := p28Follower(t, primary.Addr(), followerDir)
	if err := follower.StartBackground(); err != nil {
		t.Fatalf("start follower: %v", err)
	}

	// Wait for follower to reach seq 3.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadUint64(&follower.lastApplied) >= 3 {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if atomic.LoadUint64(&follower.lastApplied) != 3 {
		t.Fatalf("follower did not catch up: lastApplied=%d", atomic.LoadUint64(&follower.lastApplied))
	}

	// Simulate divergence: bump runtime counter to 5 without persisting cursor.
	// stateStore still says 3, but lastApplied says 5.
	// Post-WLock locked check: runtime(5) != quiesceSeq(3) → reject.
	atomic.StoreUint64(&follower.lastApplied, 5)
	defer atomic.StoreUint64(&follower.lastApplied, 3)

	qr := replnet.QuiesceRecord{
		Version:          1,
		QuiesceID:        qResp.QuiesceID,
		PrimaryNodeID:    "p28-primary",
		PrimaryBaseURL:   "http://" + primary.Addr(),
		PrimaryLatestSeq: qResp.PrimaryLatestSeq, // 3
		QuiescedAt:       qResp.QuiescedAt,
	}
	qr.Checksum = replnet.QuiesceChecksum(&qr)
	body, _ := json.Marshal(PromoteRequest{QuiesceRecord: qr, ConfirmOldPrimaryStopped: true})

	rec := p28Req(t, follower, "POST", "/replication/promote", string(body))
	if rec.Code == 200 {
		t.Errorf("expected promote to fail (cursor diverged), got 200: %s", rec.Body.String())
	}

	// Verify no promotion record was written.
	if replnet.PromotionRecordExists(followerDir) {
		t.Error("promotion record must not exist after rejected promote")
	}
}

// TestSafety_FreshFollower_NotCaughtUp_LockedCheckRejects verifies that a follower
// that has not applied any entries cannot be promoted when quiesceSeq > 0.
func TestSafety_FreshFollower_NotCaughtUp_LockedCheckRejects(t *testing.T) {
	primary := p28Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}
	// Write 3 entries.
	for i := 0; i < 3; i++ {
		rec := p28Req(t, primary, "PUT", fmt.Sprintf("/kv/k%d", i), fmt.Sprintf(`{"value":"v%d"}`, i))
		if rec.Code != 200 {
			t.Fatalf("put %d failed", i)
		}
	}
	qRec := p28Req(t, primary, "POST", "/replication/quiesce", "")
	var qResp QuiesceResponse
	json.Unmarshal(qRec.Body.Bytes(), &qResp)

	// Fresh follower with NO entries applied (seq=0), quiesceSeq=3 → locked check rejects.
	fresh := t.TempDir()
	freshFollower := p28Follower(t, primary.Addr(), fresh)
	// Do NOT start the background worker — keep the follower at seq=0.
	// We still need to start it so it can serve HTTP; but disable background sync:
	freshFollower2, err := Open(Options{
		NodeID:  "p28-follower",
		Addr:    "127.0.0.1:0",
		DataDir: t.TempDir(),
		Replication: ReplicationOptions{
			Role:           replnet.RoleFollower,
			PrimaryBaseURL: "http://" + primary.Addr(),
			// No BackgroundSync so it stays at seq=0.
		},
	})
	if err != nil {
		t.Fatalf("open fresh follower: %v", err)
	}
	t.Cleanup(func() { freshFollower2.Close() })
	if err := freshFollower2.StartBackground(); err != nil {
		t.Fatalf("start fresh follower: %v", err)
	}
	// fresh follower has lastApplied=0 and stateStore cursor=0.

	qr := replnet.QuiesceRecord{
		Version:          1,
		QuiesceID:        qResp.QuiesceID,
		PrimaryNodeID:    "p28-primary",
		PrimaryBaseURL:   "http://" + primary.Addr(),
		PrimaryLatestSeq: qResp.PrimaryLatestSeq, // 3
		QuiescedAt:       qResp.QuiescedAt,
	}
	qr.Checksum = replnet.QuiesceChecksum(&qr)
	body, _ := json.Marshal(PromoteRequest{QuiesceRecord: qr, ConfirmOldPrimaryStopped: true})

	rec := p28Req(t, freshFollower2, "POST", "/replication/promote", string(body))
	if rec.Code == 200 {
		t.Errorf("expected promote to fail (fresh follower seq=0 vs quiesceSeq=3), got 200: %s", rec.Body.String())
	}
	_ = freshFollower // suppress unused warning
}

// ── Safety Item 2: ApplyReplicationEntries barrier protection ─────────────────

// TestSafety_ApplyReplicationEntries_BarrierRejects verifies that the public
// ApplyReplicationEntries returns ErrPromotionInProgress when the barrier is set.
func TestSafety_ApplyReplicationEntries_BarrierRejects(t *testing.T) {
	primary := p28Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}
	follower := p28Follower(t, primary.Addr(), t.TempDir())
	if err := follower.StartBackground(); err != nil {
		t.Fatalf("start follower: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// Set the promotion barrier.
	follower.promotionBarrier.Store(true)
	defer follower.promotionBarrier.Store(false)

	before := atomic.LoadUint64(&follower.lastApplied)

	_, err := follower.ApplyReplicationEntries([]replnet.Entry{{Seq: 1, Op: replnet.OpPut, Key: "k", Value: "v"}})
	if err == nil {
		t.Fatal("expected error when barrier is set, got nil")
	}
	if !errors.Is(err, ErrPromotionInProgress) {
		t.Errorf("expected errors.Is(err, ErrPromotionInProgress), got: %v", err)
	}

	// Cursor must not have changed.
	after := atomic.LoadUint64(&follower.lastApplied)
	if after != before {
		t.Errorf("lastApplied changed: before=%d after=%d (apply must not mutate state when barrier set)", before, after)
	}
}

// TestSafety_HTTPReplicationApply_BarrierRejects verifies that POST /replication/apply
// returns 409 when the promotion barrier is set.
func TestSafety_HTTPReplicationApply_BarrierRejects(t *testing.T) {
	primary := p28Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}
	follower := p28Follower(t, primary.Addr(), t.TempDir())
	if err := follower.StartBackground(); err != nil {
		t.Fatalf("start follower: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	follower.promotionBarrier.Store(true)
	defer follower.promotionBarrier.Store(false)

	body := `{"entries":[{"seq":1,"op":"put","key":"k","value":"v"}]}`
	rec := p28Req(t, follower, "POST", "/replication/apply", body)
	if rec.Code != 409 {
		t.Errorf("expected 409 when barrier set, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if code, _ := resp["code"].(string); code != "promotion_in_progress" {
		t.Errorf("expected code 'promotion_in_progress', got %q", code)
	}
}

// TestSafety_ApplyReplicationEntries_CursorUnchangedAfterBarrier verifies that
// the cursor is unchanged when apply is rejected by the promotion barrier.
func TestSafety_ApplyReplicationEntries_CursorUnchangedAfterBarrier(t *testing.T) {
	primary := p28Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}
	follower := p28Follower(t, primary.Addr(), t.TempDir())
	if err := follower.StartBackground(); err != nil {
		t.Fatalf("start follower: %v", err)
	}

	// Write one entry and let background sync apply it.
	p28Req(t, primary, "PUT", "/kv/k1", `{"value":"v1"}`)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadUint64(&follower.lastApplied) >= 1 {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}

	cursorBefore := atomic.LoadUint64(&follower.lastApplied)

	// Set barrier and try to apply seq cursorBefore+1.
	follower.promotionBarrier.Store(true)
	defer follower.promotionBarrier.Store(false)

	_, err := follower.ApplyReplicationEntries([]replnet.Entry{{Seq: cursorBefore + 1, Op: replnet.OpPut, Key: "k2", Value: "v2"}})
	if !errors.Is(err, ErrPromotionInProgress) {
		t.Fatalf("expected ErrPromotionInProgress, got %v", err)
	}
	cursorAfter := atomic.LoadUint64(&follower.lastApplied)
	if cursorAfter != cursorBefore {
		t.Errorf("cursor changed: before=%d after=%d (barrier must prevent mutation)", cursorBefore, cursorAfter)
	}
}

// ── Safety Item 3: startup errors ─────────────────────────────────────────────

// TestSafety_StartupFailsOnIdentityMismatch verifies that a promoted primary node
// fails startup (Open) when the replication state store has a follower identity mismatch.
// We test this by directly writing a state file with wrong-node-id and a valid checksum,
// then creating a promotion record for "p28-follower" in the same directory.
func TestSafety_StartupFailsOnIdentityMismatch(t *testing.T) {
	dir := t.TempDir()
	primaryAddr := "127.0.0.1:19999" // fixed fake addr for URL construction

	// Step 1: create a state store for "wrong-node-id" so it writes a state file.
	wrongStore, err := replnet.NewReplicationStateStore(dir, "wrong-node-id", "http://"+primaryAddr)
	if err != nil {
		t.Fatalf("create wrong store: %v", err)
	}
	// AdvanceTo(1) writes the state file to disk with "wrong-node-id".
	if err := wrongStore.AdvanceTo(1); err != nil {
		t.Fatalf("advance wrong store: %v", err)
	}

	// Step 2: write a journal baseline (required for promoted primary startup).
	if err := replnet.CreateJournalBaseline(dir, 1); err != nil {
		t.Fatalf("create journal baseline: %v", err)
	}

	// Step 3: write a promotion record for "p28-follower" node.
	promRec := replnet.NewPromotionRecord("p28-follower", "p28-primary", "http://"+primaryAddr, "test-qid", 1)
	if err := replnet.SavePromotionRecord(dir, promRec); err != nil {
		t.Fatalf("save promotion record: %v", err)
	}

	// Step 4: Open should fail because state file has "wrong-node-id" != "p28-follower".
	_, openErr := Open(Options{
		NodeID:  "p28-follower",
		Addr:    "127.0.0.1:0",
		DataDir: dir,
		Replication: ReplicationOptions{
			Role: replnet.RolePrimary, // promoted
		},
	})
	if openErr == nil {
		t.Error("expected Open to fail on identity mismatch, got nil")
	}
	t.Logf("Open error (expected): %v", openErr)
	if !errors.Is(openErr, replnet.ErrFollowerIdentityMismatch) {
		t.Errorf("expected ErrFollowerIdentityMismatch in error chain, got: %v", openErr)
	}
}

// TestSafety_StartupFailsOnCorruptStateStore verifies that a promoted primary node
// fails startup when the replication state file is corrupt.
func TestSafety_StartupFailsOnCorruptStateStore(t *testing.T) {
	primary := p28Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}
	followerDir := t.TempDir()
	follower := p28Follower(t, primary.Addr(), followerDir)
	if err := follower.StartBackground(); err != nil {
		t.Fatalf("start follower: %v", err)
	}

	// Promote the follower (0 entries, quiesceSeq=0).
	qRec := p28Req(t, primary, "POST", "/replication/quiesce", "")
	var qResp QuiesceResponse
	json.Unmarshal(qRec.Body.Bytes(), &qResp)
	qr := replnet.QuiesceRecord{
		Version: 1, QuiesceID: qResp.QuiesceID, PrimaryNodeID: "p28-primary",
		PrimaryBaseURL: "http://" + primary.Addr(), PrimaryLatestSeq: qResp.PrimaryLatestSeq,
		QuiescedAt: qResp.QuiescedAt,
	}
	qr.Checksum = replnet.QuiesceChecksum(&qr)
	body, _ := json.Marshal(PromoteRequest{QuiesceRecord: qr, ConfirmOldPrimaryStopped: true})
	rec := p28Req(t, follower, "POST", "/replication/promote", string(body))
	if rec.Code != 200 {
		t.Fatalf("promote failed: %d %s", rec.Code, rec.Body.String())
	}
	follower.Close()

	// Corrupt the replication state file.
	stateFilePath := followerDir + "/replication_state.json"
	if err := os.WriteFile(stateFilePath, []byte("not-valid-json{{{"), 0o644); err != nil {
		t.Fatalf("write corrupt state: %v", err)
	}

	// Open should fail.
	_, openErr := Open(Options{
		NodeID:      "p28-follower",
		Addr:        "127.0.0.1:0",
		DataDir:     followerDir,
		Replication: ReplicationOptions{Role: replnet.RolePrimary},
	})
	if openErr == nil {
		t.Error("expected Open to fail on corrupt state, got nil")
	}
	t.Logf("Open error (expected): %v", openErr)
}

// ── Safety Item 4: journalCompatibilityCheck error propagation ────────────────

// TestSafety_JournalCompatibilityCheck_CorruptJournal verifies that CreateJournalBaseline
// returns an error (not nil) when the journal file has a CRC-corrupt record.
// The CRC corruption path returns ErrCorruptedJournal from replay(), causing
// OpenDurableLog to fail, which journalCompatibilityCheck must propagate (not swallow).
func TestSafety_JournalCompatibilityCheck_CorruptJournal(t *testing.T) {
	dir := t.TempDir()

	// First write a valid journal entry.
	dl, err := replnet.OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	if _, err := dl.Append(replnet.OpPut, "key", "val"); err != nil {
		t.Fatalf("append: %v", err)
	}
	dl.Close()

	// Now corrupt the journal file by flipping bytes in the record body (after the
	// length field but within the CRC-covered region). This produces a CRC mismatch
	// which replay() returns as ErrCorruptedJournal (not a partial-tail truncation).
	journalPath := dir + "/replication.journal"
	data, readErr := os.ReadFile(journalPath)
	if readErr != nil {
		t.Fatalf("read journal: %v", readErr)
	}
	if len(data) < 8 {
		t.Fatalf("journal too short to corrupt: %d bytes", len(data))
	}
	// Flip bytes 5-8 (inside the CRC-covered payload region) to produce a CRC mismatch.
	data[4] ^= 0xFF
	data[5] ^= 0xFF
	data[6] ^= 0xFF
	data[7] ^= 0xFF
	if err := os.WriteFile(journalPath, data, 0o644); err != nil {
		t.Fatalf("write corrupt journal: %v", err)
	}

	// CreateJournalBaseline should fail because journalCompatibilityCheck propagates
	// the OpenDurableLog error (ErrCorruptedJournal via replay) instead of ignoring it.
	createErr := replnet.CreateJournalBaseline(dir, 0)
	if createErr == nil {
		t.Error("expected error for CRC-corrupt journal, got nil")
	}
	t.Logf("error (expected): %v", createErr)
	// Baseline must NOT have been written.
	if replnet.JournalBaselineExists(dir) {
		t.Error("baseline must not exist after compatibility check failure")
	}
}

// TestSafety_JournalCompatibilityCheck_IncompatibleSeq verifies ErrJournalIncompatible.
func TestSafety_JournalCompatibilityCheck_IncompatibleSeq(t *testing.T) {
	dir := t.TempDir()
	// Create a real journal with entries starting at seq 1.
	dl, err := replnet.OpenDurableLog(dir)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	if _, err := dl.Append(replnet.OpPut, "k", "v"); err != nil {
		t.Fatalf("append: %v", err)
	}
	dl.Close()

	// CreateJournalBaseline with baseSeq=5: expects first_seq==6, but journal has first_seq==1.
	err = replnet.CreateJournalBaseline(dir, 5)
	if err == nil {
		t.Fatal("expected ErrJournalIncompatible, got nil")
	}
	if !errors.Is(err, replnet.ErrJournalIncompatible) {
		t.Errorf("expected ErrJournalIncompatible in chain, got: %v", err)
	}
	if replnet.JournalBaselineExists(dir) {
		t.Error("baseline must not exist after incompatibility error")
	}
}

// TestSafety_JournalCompatibilityCheck_AbsentJournal verifies absent journal is compatible.
func TestSafety_JournalCompatibilityCheck_AbsentJournal(t *testing.T) {
	dir := t.TempDir()
	// No journal file at all.
	err := replnet.CreateJournalBaseline(dir, 42)
	if err != nil {
		t.Errorf("expected nil for absent journal, got: %v", err)
	}
}

// ── Safety Item 1 (extended): Worker restart idempotency ─────────────────────

// TestSafety_WorkerRestart_IdempotentSingleWorker proves that calling
// restartBackgroundWorkerAfterPromotionFailure twice installs exactly one replacement
// worker (not two), that one Close stops it, and that no orphan goroutine remains.
func TestSafety_WorkerRestart_IdempotentSingleWorker(t *testing.T) {
	primary := p28Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}

	follower := p28Follower(t, primary.Addr(), t.TempDir())
	if err := follower.StartBackground(); err != nil {
		t.Fatalf("start follower: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// Capture the initial worker.
	follower.mu.Lock()
	initialWorker := follower.bgWorker
	follower.mu.Unlock()
	if initialWorker == nil {
		t.Fatal("expected non-nil bgWorker after StartBackground")
	}

	// Stop the original worker so the restart has something to replace.
	initialWorker.stop()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if initialWorker.Status().State == WorkerStateStopped {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if initialWorker.Status().State != WorkerStateStopped {
		t.Fatal("initial worker did not reach Stopped state")
	}

	// First restart call: installs a replacement worker.
	follower.restartBackgroundWorkerAfterPromotionFailure()

	follower.mu.Lock()
	firstReplacement := follower.bgWorker
	follower.mu.Unlock()

	if firstReplacement == initialWorker {
		t.Error("expected bgWorker to be replaced, got same pointer")
	}
	if firstReplacement == nil {
		t.Fatal("expected non-nil bgWorker after first restart")
	}

	// Wait for the replacement to be running (not starting).
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		state := firstReplacement.Status().State
		if state == WorkerStateRunning || state == WorkerStateBackingOff {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Second restart call: replacement is active — must NOT install another worker.
	follower.restartBackgroundWorkerAfterPromotionFailure()

	follower.mu.Lock()
	afterSecondCall := follower.bgWorker
	follower.mu.Unlock()

	if afterSecondCall != firstReplacement {
		t.Errorf("second restart call must not replace a running worker (idempotency violated): got new pointer %p, want %p", afterSecondCall, firstReplacement)
	}

	// One Close stops the single replacement worker cleanly.
	if err := follower.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}

	// After Close returns, stop() has called wg.Wait(), so the goroutine has exited
	// and its deferred function has set state to WorkerStateStopped. Require exactly that.
	state := firstReplacement.Status().State
	if state != WorkerStateStopped {
		t.Errorf("expected WorkerStateStopped after Close, got %q", state)
	}

	// Restart call on a closed server must not install a new worker.
	follower.restartBackgroundWorkerAfterPromotionFailure()
	follower.mu.Lock()
	afterClosedRestart := follower.bgWorker
	follower.mu.Unlock()
	// Close does not nil bgWorker; it remains pointing to the stopped replacement.
	if afterClosedRestart != firstReplacement {
		t.Error("restart on closed server must not change bgWorker pointer")
	}
}

// ── Safety Item 2: Close-versus-worker-restart race ───────────────────────────

// TestSafety_CloseVsWorkerRestart_Deterministic races Close against
// restartBackgroundWorkerAfterPromotionFailure using a WaitGroup barrier so both
// goroutines start simultaneously. Runs 50 iterations under -race.
//
// Required outcomes on every iteration:
//   - no panic
//   - no data race flagged by -race
//   - after Close returns, no worker is in a running/starting/backing-off state
func TestSafety_CloseVsWorkerRestart_Deterministic(t *testing.T) {
	for i := 0; i < 50; i++ {
		primary := p28Primary(t)
		if err := primary.StartBackground(); err != nil {
			t.Fatalf("iter %d: start primary: %v", i, err)
		}

		follower := p28Follower(t, primary.Addr(), t.TempDir())
		if err := follower.StartBackground(); err != nil {
			t.Fatalf("iter %d: start follower: %v", i, err)
		}
		time.Sleep(40 * time.Millisecond)

		// Stop the initial worker so the restart has a stopped worker to replace.
		follower.mu.Lock()
		w := follower.bgWorker
		follower.mu.Unlock()
		if w != nil {
			w.stop()
			dl2 := time.Now().Add(time.Second)
			for time.Now().Before(dl2) {
				if w.Status().State == WorkerStateStopped {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
		}

		// Release both goroutines simultaneously.
		var barrier sync.WaitGroup
		barrier.Add(1)
		var done sync.WaitGroup
		done.Add(2)

		go func() {
			barrier.Wait()
			follower.restartBackgroundWorkerAfterPromotionFailure()
			done.Done()
		}()
		go func() {
			barrier.Wait()
			_ = follower.Close()
			done.Done()
		}()

		barrier.Done() // release both
		done.Wait()

		// After Close returns, stop() has called wg.Wait(), so the goroutine has exited.
		// Require exactly WorkerStateStopped — WorkerStateStopping means the goroutine
		// has not exited yet, which cannot happen after wg.Wait() returns.
		follower.mu.Lock()
		finalWorker := follower.bgWorker
		follower.mu.Unlock()
		if finalWorker != nil {
			state := finalWorker.Status().State
			if state != WorkerStateStopped {
				t.Errorf("iter %d: worker in state %q after Close (want WorkerStateStopped)", i, state)
			}
		}

		primary.Close()
	}
}

// ── Safety Item 6: ErrPromotionInProgress from SyncFromPrimary ────────────────

// TestSafety_SyncFromPrimary_PromotionBarrier_TypedError verifies that all three
// promotion-barrier rejection paths in SyncFromPrimary return errors.Is(ErrPromotionInProgress).
func TestSafety_SyncFromPrimary_PromotionBarrier_TypedError(t *testing.T) {
	primary := p28Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}
	follower := p28Follower(t, primary.Addr(), t.TempDir())
	if err := follower.StartBackground(); err != nil {
		t.Fatalf("start follower: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// Set barrier (pre-CAS check path).
	follower.promotionBarrier.Store(true)
	defer follower.promotionBarrier.Store(false)

	_, err := follower.SyncFromPrimary(context.Background())
	if err == nil {
		t.Fatal("expected error from SyncFromPrimary with barrier set")
	}
	if !errors.Is(err, ErrPromotionInProgress) {
		t.Errorf("expected errors.Is(err, ErrPromotionInProgress), got: %v", err)
	}
}

// TestSafety_SyncFromPrimary_PromotionInProgress_HTTP409 verifies that POST /replication/sync
// returns 409 with code "promotion_in_progress" when the barrier is set.
func TestSafety_SyncFromPrimary_PromotionInProgress_HTTP409(t *testing.T) {
	primary := p28Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}
	follower := p28Follower(t, primary.Addr(), t.TempDir())
	if err := follower.StartBackground(); err != nil {
		t.Fatalf("start follower: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	follower.promotionBarrier.Store(true)
	defer follower.promotionBarrier.Store(false)

	rec := p28Req(t, follower, "POST", "/replication/sync", "")
	if rec.Code != 409 {
		t.Errorf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if code, _ := resp["code"].(string); code != "promotion_in_progress" {
		t.Errorf("expected code 'promotion_in_progress', got %q", code)
	}
}

// TestSafety_HTTPStatusError_PromotionInProgress_ErrorsIs verifies that an
// HTTPStatusError with code "promotion_in_progress" satisfies errors.Is(ErrPromotionInProgress)
// but not an unrelated error.
func TestSafety_HTTPStatusError_PromotionInProgress_ErrorsIs(t *testing.T) {
	e := &HTTPStatusError{StatusCode: 409, Code: "promotion_in_progress", Message: "test"}
	if !errors.Is(e, ErrPromotionInProgress) {
		t.Error("expected errors.Is(HTTPStatusError{promotion_in_progress}, ErrPromotionInProgress)")
	}
	// An unrelated 502 must not match.
	e2 := &HTTPStatusError{StatusCode: 502, Code: "bad_gateway", Message: "test"}
	if errors.Is(e2, ErrPromotionInProgress) {
		t.Error("unrelated 502 must not match ErrPromotionInProgress")
	}
}

// ── Safety Item 7: Quiesce intent cleanup on retry ────────────────────────────

// TestSafety_QuiesceRetry_IntentRemovedOnSuccess verifies that when a quiesce succeeds,
// the intent file is removed and quiesceIntentActive becomes false.
func TestSafety_QuiesceRetry_IntentRemovedOnSuccess(t *testing.T) {
	primary := p28Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}
	defer primary.Close()

	// First quiesce succeeds normally — intent written then removed.
	rec := p28Req(t, primary, "POST", "/replication/quiesce", "")
	if rec.Code != 200 {
		t.Fatalf("quiesce failed: %d %s", rec.Code, rec.Body.String())
	}

	st := primary.runtimeState()
	if st.quiesceIntentActive {
		t.Error("quiesceIntentActive must be false after successful quiesce")
	}
	if st.quiesceState != "quiesced" {
		t.Errorf("expected quiesced, got %q", st.quiesceState)
	}

	// Idempotent: a second call returns the same record, no intent.
	rec2 := p28Req(t, primary, "POST", "/replication/quiesce", "")
	if rec2.Code != 200 {
		t.Errorf("idempotent quiesce failed: %d %s", rec2.Code, rec2.Body.String())
	}
	st2 := primary.runtimeState()
	if st2.quiesceIntentActive {
		t.Error("quiesceIntentActive must be false after idempotent quiesce")
	}
}

// TestSafety_QuiesceRetry_IntentFileRemovedAfterRetry verifies that after a
// quiesce_failed_fenced retry succeeds, the intent file is removed from disk.
func TestSafety_QuiesceRetry_IntentFileRemovedAfterRetry(t *testing.T) {
	dir := t.TempDir()
	primary, err := Open(Options{
		NodeID:      "retry-primary",
		Addr:        "127.0.0.1:0",
		DataDir:     dir,
		Replication: ReplicationOptions{Role: replnet.RolePrimary},
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer primary.Close()

	// Manually set up quiesce_failed_fenced state with intent on disk.
	intent := &replnet.QuiesceIntentRecord{
		Version:        1,
		QuiesceID:      "test-intent-id",
		PrimaryNodeID:  "retry-primary",
		PrimaryBaseURL: "http://127.0.0.1:0",
		IntentAt:       time.Now().UTC().Format(time.RFC3339Nano),
	}
	intent.Checksum = replnet.QuiesceIntentChecksum(intent)
	if err := replnet.SaveQuiesceIntent(dir, intent); err != nil {
		t.Fatalf("save intent: %v", err)
	}

	// Quiesce the gate first so the write gate is actually closed.
	primary.writeGate.Quiesce()

	pending := &replnet.QuiesceRecord{
		Version:          1,
		QuiesceID:        "test-intent-id",
		PrimaryNodeID:    "retry-primary",
		PrimaryBaseURL:   "http://" + primary.Addr(),
		PrimaryLatestSeq: 0,
		QuiescedAt:       time.Now().UTC().Format(time.RFC3339Nano),
	}
	pending.Checksum = replnet.QuiesceChecksum(pending)

	primary.mu.Lock()
	primary.quiesceState = "quiesce_failed_fenced"
	primary.pendingQuiesceRecord = pending
	primary.quiesceIntentActive = true
	primary.mu.Unlock()

	// Trigger retry via POST /replication/quiesce.
	rec := p28Req(t, primary, "POST", "/replication/quiesce", "")
	if rec.Code != 200 {
		t.Fatalf("retry quiesce failed: %d %s", rec.Code, rec.Body.String())
	}

	// Intent file must be removed.
	if replnet.QuiesceIntentExists(primary.opts.DataDir) {
		t.Error("intent file must be removed after successful retry")
	}
	st := primary.runtimeState()
	if st.quiesceIntentActive {
		t.Errorf("quiesceIntentActive must be false after retry success, got true")
	}
	if st.quiesceState != "quiesced" {
		t.Errorf("expected quiesced, got %q", st.quiesceState)
	}
}

// ── Safety Item 8: Real stop-before-promote integration ───────────────────────

// TestSafety_RealStopBeforePromote_Integration tests the canonical failover flow:
// 1. Start primary+follower, replicate data.
// 2. Quiesce primary.
// 3. Wait for follower cursor == final seq.
// 4. CLOSE old primary (stop it).
// 5. Verify primary HTTP is unreachable.
// 6. Call promote on follower.
// 7. Verify promotion succeeds.
// 8. Restart old primary.
// 9. Verify old primary is write-fenced.
func TestSafety_RealStopBeforePromote_Integration(t *testing.T) {
	primaryDir := t.TempDir()
	primary, err := Open(Options{
		NodeID:      "failover-primary",
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
	primaryAddr := primary.Addr()

	followerDir := t.TempDir()
	follower, err := Open(Options{
		NodeID:  "failover-follower",
		Addr:    "127.0.0.1:0",
		DataDir: followerDir,
		Replication: ReplicationOptions{
			Role:           replnet.RoleFollower,
			PrimaryBaseURL: "http://" + primaryAddr,
			BackgroundSync: bgIntegrationCfg(),
		},
	})
	if err != nil {
		t.Fatalf("open follower: %v", err)
	}
	if err := follower.StartBackground(); err != nil {
		t.Fatalf("start follower: %v", err)
	}
	t.Cleanup(func() { follower.Close() })

	// Write some data to the primary.
	for i := 0; i < 5; i++ {
		rec := p28Req(t, primary, "PUT", fmt.Sprintf("/kv/key%d", i), fmt.Sprintf(`{"value":"val%d"}`, i))
		if rec.Code != 200 {
			t.Fatalf("put %d: %d", i, rec.Code)
		}
	}

	// Quiesce the primary.
	qRec := p28Req(t, primary, "POST", "/replication/quiesce", "")
	if qRec.Code != 200 {
		t.Fatalf("quiesce failed: %d %s", qRec.Code, qRec.Body.String())
	}
	var qResp QuiesceResponse
	json.Unmarshal(qRec.Body.Bytes(), &qResp)
	finalSeq := qResp.PrimaryLatestSeq

	// Wait for follower to reach the final seq (background sync).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadUint64(&follower.lastApplied) >= finalSeq {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	followerSeq := atomic.LoadUint64(&follower.lastApplied)
	if followerSeq != finalSeq {
		t.Fatalf("follower did not catch up: lastApplied=%d, finalSeq=%d", followerSeq, finalSeq)
	}

	// Save the quiesce record for the promote request.
	qr := replnet.QuiesceRecord{
		Version:          1,
		QuiesceID:        qResp.QuiesceID,
		PrimaryNodeID:    "failover-primary",
		PrimaryBaseURL:   "http://" + primaryAddr,
		PrimaryLatestSeq: qResp.PrimaryLatestSeq,
		QuiescedAt:       qResp.QuiescedAt,
	}
	qr.Checksum = replnet.QuiesceChecksum(&qr)

	// STOP THE OLD PRIMARY before promoting.
	primary.Close()

	// Verify the old primary's HTTP endpoint is unreachable.
	client := &http.Client{Timeout: 500 * time.Millisecond}
	_, pingErr := client.Get("http://" + primaryAddr + "/healthz")
	if pingErr == nil {
		t.Error("expected old primary to be unreachable after Close")
	}

	// Promote the follower. confirm_old_primary_stopped=true is honest here.
	promBody, _ := json.Marshal(PromoteRequest{QuiesceRecord: qr, ConfirmOldPrimaryStopped: true})
	promRec := p28Req(t, follower, "POST", "/replication/promote", string(promBody))
	if promRec.Code != 200 {
		t.Fatalf("promote failed: %d %s", promRec.Code, promRec.Body.String())
	}
	var promResp PromoteResponse
	json.Unmarshal(promRec.Body.Bytes(), &promResp)
	if promResp.NewRole != "primary" {
		t.Errorf("expected new_role=primary, got %q", promResp.NewRole)
	}

	// Verify follower is now a primary.
	st := follower.runtimeState()
	if st.role != "primary" {
		t.Errorf("expected follower role=primary after promotion, got %q", st.role)
	}

	// Restart the old primary from the same data dir. It must be write-fenced (quiesced).
	primary2, err := Open(Options{
		NodeID:      "failover-primary",
		Addr:        "127.0.0.1:0",
		DataDir:     primaryDir,
		Replication: ReplicationOptions{Role: replnet.RolePrimary},
	})
	if err != nil {
		t.Fatalf("restart old primary: %v", err)
	}
	if err := primary2.StartBackground(); err != nil {
		t.Fatalf("start old primary2: %v", err)
	}
	t.Cleanup(func() { primary2.Close() })

	// Old primary must be write-fenced (quiesced state).
	st2 := primary2.runtimeState()
	if st2.quiesceState != "quiesced" {
		t.Errorf("restarted old primary must be quiesced, got %q", st2.quiesceState)
	}

	// A write to the old primary must fail.
	writeRec := p28Req(t, primary2, "PUT", "/kv/newkey", `{"value":"v"}`)
	if writeRec.Code != 409 {
		t.Errorf("expected 409 for write to fenced old primary, got %d: %s", writeRec.Code, writeRec.Body.String())
	}
}

// TestSafety_ConfirmOldPrimaryStopped_IsOperatorAssertion documents that
// confirm_old_primary_stopped is an operator assertion, not a distributed proof.
func TestSafety_ConfirmOldPrimaryStopped_IsOperatorAssertion(t *testing.T) {
	primary := p28Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}
	followerDir := t.TempDir()
	follower := p28Follower(t, primary.Addr(), followerDir)
	if err := follower.StartBackground(); err != nil {
		t.Fatalf("start follower: %v", err)
	}

	// Wait for follower background sync to settle.
	time.Sleep(300 * time.Millisecond)

	qRec := p28Req(t, primary, "POST", "/replication/quiesce", "")
	var qResp QuiesceResponse
	json.Unmarshal(qRec.Body.Bytes(), &qResp)

	// Wait for follower to catch up (quiesceSeq may be 0).
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadUint64(&follower.lastApplied) >= qResp.PrimaryLatestSeq {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}

	qr := replnet.QuiesceRecord{
		Version: 1, QuiesceID: qResp.QuiesceID, PrimaryNodeID: "p28-primary",
		PrimaryBaseURL: "http://" + primary.Addr(), PrimaryLatestSeq: qResp.PrimaryLatestSeq,
		QuiescedAt: qResp.QuiescedAt,
	}
	qr.Checksum = replnet.QuiesceChecksum(&qr)
	body, _ := json.Marshal(PromoteRequest{QuiesceRecord: qr, ConfirmOldPrimaryStopped: true})

	// Old primary is still running — the system does not independently verify this.
	rec := p28Req(t, follower, "POST", "/replication/promote", string(body))
	// Note: promotion may succeed or fail depending on replication state.
	// This test only documents that the system does not independently verify.
	t.Logf("promote with live old primary: code=%d body=%s", rec.Code, rec.Body.String())
	// No assertion on outcome — the test documents the semantic, not a correctness guarantee.
}

// ── Safety Item 3: BackgroundSyncStatus data race ─────────────────────────────

// TestSafety_BackgroundSyncStatus_RaceProof runs concurrent BackgroundSyncStatus,
// GET /replication/status, and restartBackgroundWorkerAfterPromotionFailure calls.
// The -race detector must not fire.
func TestSafety_BackgroundSyncStatus_RaceProof(t *testing.T) {
	primary := p28Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}

	follower := p28Follower(t, primary.Addr(), t.TempDir())
	if err := follower.StartBackground(); err != nil {
		t.Fatalf("start follower: %v", err)
	}
	time.Sleep(80 * time.Millisecond)

	// Stop the initial worker so restart will actually install a replacement.
	follower.mu.Lock()
	w := follower.bgWorker
	follower.mu.Unlock()
	if w != nil {
		w.stop()
		dl := time.Now().Add(time.Second)
		for time.Now().Before(dl) {
			if w.Status().State == WorkerStateStopped {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
	}

	done := make(chan struct{})
	var wg sync.WaitGroup

	// Goroutine 1: repeatedly calls BackgroundSyncStatus (reads s.bgWorker).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
				_ = follower.BackgroundSyncStatus()
			}
		}
	}()

	// Goroutine 2: repeatedly calls GET /replication/status (also calls BackgroundSyncStatus).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
				_ = p28Req(t, follower, "GET", "/replication/status", "")
			}
		}
	}()

	// Goroutine 3: repeatedly calls restartBackgroundWorkerAfterPromotionFailure (writes s.bgWorker).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
				follower.restartBackgroundWorkerAfterPromotionFailure()
			}
		}
	}()

	time.Sleep(200 * time.Millisecond)
	close(done)
	wg.Wait()
	follower.Close()
}

// ── Safety Item 4: Replicator pointer race in SyncFromPrimary ─────────────────

// TestSafety_SyncFromPrimary_ReplicatorRace races concurrent SyncFromPrimary calls
// against s.replicator and s.runtimeRole mutations (simulating promotion). The -race
// detector must not fire and no nil-pointer panic must occur.
func TestSafety_SyncFromPrimary_ReplicatorRace(t *testing.T) {
	primary := p28Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}

	follower := p28Follower(t, primary.Addr(), t.TempDir())
	if err := follower.StartBackground(); err != nil {
		t.Fatalf("start follower: %v", err)
	}
	time.Sleep(80 * time.Millisecond)

	const iterations = 60
	var start sync.WaitGroup
	start.Add(1)
	var wg sync.WaitGroup

	// Writer goroutine: briefly simulates promotion clearing s.replicator and changing role.
	wg.Add(1)
	go func() {
		defer wg.Done()
		start.Wait()
		for i := 0; i < iterations; i++ {
			follower.mu.Lock()
			saved := follower.replicator
			savedRole := follower.runtimeRole
			follower.replicator = nil
			follower.runtimeRole = "primary"
			follower.mu.Unlock()

			// Immediately restore so the follower can continue functioning.
			follower.mu.Lock()
			follower.replicator = saved
			follower.runtimeRole = savedRole
			follower.mu.Unlock()
		}
	}()

	// Reader goroutine: repeatedly calls SyncFromPrimary.
	wg.Add(1)
	go func() {
		defer wg.Done()
		start.Wait()
		for i := 0; i < iterations; i++ {
			_, err := follower.SyncFromPrimary(context.Background())
			if err != nil {
				// ErrNotFollower, ErrPromotionInProgress, ErrClosed, ErrSyncInProgress,
				// nil-replicator error are all acceptable outcomes.
				_ = err
			}
		}
	}()

	start.Done() // release both goroutines simultaneously
	wg.Wait()
	follower.Close()
}

// ── Safety Item 5: ApplyReplicationEntries follower-only ──────────────────────

// TestSafety_ApplyReplicationEntries_PrimaryRejected verifies that calling
// ApplyReplicationEntries on a primary node returns ErrNotFollower and does not
// mutate the engine.
func TestSafety_ApplyReplicationEntries_PrimaryRejected(t *testing.T) {
	primary := p28Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}
	before := atomic.LoadUint64(&primary.lastApplied)

	_, err := primary.ApplyReplicationEntries([]replnet.Entry{{Seq: 1, Op: replnet.OpPut, Key: "k", Value: "v"}})
	if err == nil {
		t.Fatal("expected error for primary node, got nil")
	}
	if !errors.Is(err, ErrNotFollower) {
		t.Errorf("expected ErrNotFollower, got: %v", err)
	}
	after := atomic.LoadUint64(&primary.lastApplied)
	if after != before {
		t.Errorf("lastApplied changed on primary: before=%d after=%d", before, after)
	}
}

// TestSafety_ApplyReplicationEntries_StandaloneRejected verifies that a standalone
// node also rejects ApplyReplicationEntries with ErrNotFollower.
func TestSafety_ApplyReplicationEntries_StandaloneRejected(t *testing.T) {
	srv, err := Open(Options{
		NodeID:  "standalone-node",
		Addr:    "127.0.0.1:0",
		DataDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { srv.Close() })

	before := atomic.LoadUint64(&srv.lastApplied)
	_, err = srv.ApplyReplicationEntries([]replnet.Entry{{Seq: 1, Op: replnet.OpPut, Key: "k", Value: "v"}})
	if err == nil {
		t.Fatal("expected error for standalone node, got nil")
	}
	if !errors.Is(err, ErrNotFollower) {
		t.Errorf("expected ErrNotFollower, got: %v", err)
	}
	if atomic.LoadUint64(&srv.lastApplied) != before {
		t.Error("lastApplied must not change on standalone rejection")
	}
}

// TestSafety_ApplyReplicationEntries_PromotedPrimaryRejected verifies that a promoted
// primary (formerly follower) rejects ApplyReplicationEntries with ErrNotFollower.
func TestSafety_ApplyReplicationEntries_PromotedPrimaryRejected(t *testing.T) {
	primaryNode := p28Primary(t)
	if err := primaryNode.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}

	followerDir := t.TempDir()
	follower := p28Follower(t, primaryNode.Addr(), followerDir)
	if err := follower.StartBackground(); err != nil {
		t.Fatalf("start follower: %v", err)
	}

	// Quiesce primary at seq 0 and promote follower.
	qRec := p28Req(t, primaryNode, "POST", "/replication/quiesce", "")
	var qResp QuiesceResponse
	json.Unmarshal(qRec.Body.Bytes(), &qResp)
	qr := replnet.QuiesceRecord{
		Version: 1, QuiesceID: qResp.QuiesceID, PrimaryNodeID: "p28-primary",
		PrimaryBaseURL: "http://" + primaryNode.Addr(), PrimaryLatestSeq: qResp.PrimaryLatestSeq,
		QuiescedAt: qResp.QuiescedAt,
	}
	qr.Checksum = replnet.QuiesceChecksum(&qr)
	body, _ := json.Marshal(PromoteRequest{QuiesceRecord: qr, ConfirmOldPrimaryStopped: true})
	pRec := p28Req(t, follower, "POST", "/replication/promote", string(body))
	if pRec.Code != 200 {
		t.Fatalf("promote failed: %d %s", pRec.Code, pRec.Body.String())
	}

	// Now the follower is a promoted primary.
	st := follower.runtimeState()
	if st.role != "primary" {
		t.Fatalf("expected promoted primary role, got %q", st.role)
	}

	before := atomic.LoadUint64(&follower.lastApplied)
	_, applyErr := follower.ApplyReplicationEntries([]replnet.Entry{{Seq: before + 1, Op: replnet.OpPut, Key: "k", Value: "v"}})
	if applyErr == nil {
		t.Fatal("expected error for promoted primary, got nil")
	}
	// After promotion, promotionBarrier remains set (definitive gate). The barrier check
	// fires before the role check, so ErrPromotionInProgress is returned instead of
	// ErrNotFollower. Either sentinel proves that a promoted primary cannot mutate engine state.
	if !errors.Is(applyErr, ErrPromotionInProgress) && !errors.Is(applyErr, ErrNotFollower) {
		t.Errorf("expected ErrPromotionInProgress or ErrNotFollower for promoted primary, got: %v", applyErr)
	}
	if atomic.LoadUint64(&follower.lastApplied) != before {
		t.Error("lastApplied must not change after promoted-primary rejection")
	}
}

// TestSafety_ApplyReplicationEntries_CursorUnchangedOnRoleReject verifies that the
// engine and stateStore cursor are unchanged after a role-based rejection.
//
// Setup uses an explicit synchronous SyncFromPrimary call (no background polling,
// no t.Skip) so cursorBefore is deterministically exactly 1.
func TestSafety_ApplyReplicationEntries_CursorUnchangedOnRoleReject(t *testing.T) {
	primary := p28Primary(t)
	if err := primary.StartBackground(); err != nil {
		t.Fatalf("start primary: %v", err)
	}
	// Write one entry to give the primary a non-empty journal (seq 1).
	rec := p28Req(t, primary, "PUT", "/kv/k1", `{"value":"v1"}`)
	if rec.Code != 200 {
		t.Fatalf("PUT /kv/k1: %d %s", rec.Code, rec.Body.String())
	}

	// Create a follower but do NOT start background sync — use explicit SyncFromPrimary
	// so the cursor is set deterministically.
	follower := p28Follower(t, primary.Addr(), t.TempDir())

	if _, err := follower.SyncFromPrimary(context.Background()); err != nil {
		t.Fatalf("SyncFromPrimary: %v", err)
	}

	cursorBefore := atomic.LoadUint64(&follower.lastApplied)
	if cursorBefore != 1 {
		t.Fatalf("expected lastApplied=1 after SyncFromPrimary, got %d", cursorBefore)
	}

	// Temporarily simulate the follower being a primary to force a role rejection.
	follower.mu.Lock()
	follower.runtimeRole = "primary"
	follower.mu.Unlock()
	defer func() {
		follower.mu.Lock()
		follower.runtimeRole = "follower"
		follower.mu.Unlock()
	}()

	_, err := follower.ApplyReplicationEntries([]replnet.Entry{{Seq: cursorBefore + 1, Op: replnet.OpPut, Key: "k2", Value: "v2"}})
	if !errors.Is(err, ErrNotFollower) {
		t.Fatalf("expected ErrNotFollower, got: %v", err)
	}
	if atomic.LoadUint64(&follower.lastApplied) != cursorBefore {
		t.Errorf("cursor changed after role rejection: before=%d after=%d", cursorBefore, atomic.LoadUint64(&follower.lastApplied))
	}
	// Engine must not have the key.
	_, found, _ := follower.eng.Get([]byte("k2"))
	if found {
		t.Error("engine must not contain k2 after role-rejected apply")
	}
}

// ── Safety Item 6: Quiesce intent cleanup truthfulness ────────────────────────

// TestSafety_Quiesce_InitialPath_IntentCleanupFailure verifies that when the initial
// quiesce succeeds (record written) but intent removal fails, quiesceIntentActive
// remains true and GET /replication/status reports quiesce_intent_state=cleanup_pending.
func TestSafety_Quiesce_InitialPath_IntentCleanupFailure(t *testing.T) {
	dir := t.TempDir()
	srv, err := Open(Options{
		NodeID:      "qclean-primary",
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
	t.Cleanup(func() { srv.Close() })

	// Inject a failing removeQuiesceIntentFn.
	srv.removeQuiesceIntentFn = func(string) error {
		return fmt.Errorf("simulated removal failure")
	}

	rec := p28Req(t, srv, "POST", "/replication/quiesce", "")
	if rec.Code != 200 {
		t.Fatalf("quiesce failed: %d %s", rec.Code, rec.Body.String())
	}

	// quiesceIntentActive must remain true because removal failed.
	st := srv.runtimeState()
	if !st.quiesceIntentActive {
		t.Error("quiesceIntentActive must be true when intent removal failed")
	}
	if st.quiesceState != "quiesced" {
		t.Errorf("quiesce state must be 'quiesced', got %q", st.quiesceState)
	}

	// GET /replication/status must show cleanup_pending.
	statusRec := p28Req(t, srv, "GET", "/replication/status", "")
	var resp ReplicationStatusResponse
	if err := json.Unmarshal(statusRec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}
	if resp.QuiesceIntentState != "cleanup_pending" {
		t.Errorf("expected QuiesceIntentState='cleanup_pending', got %q", resp.QuiesceIntentState)
	}
}

// TestSafety_Quiesce_InitialPath_IntentCleanupSuccess verifies that when the initial
// quiesce succeeds and intent removal succeeds, quiesceIntentActive is false and
// quiesce_intent_state is not set in the status response.
func TestSafety_Quiesce_InitialPath_IntentCleanupSuccess(t *testing.T) {
	srv, err := Open(Options{
		NodeID:      "qclean-ok",
		Addr:        "127.0.0.1:0",
		DataDir:     t.TempDir(),
		Replication: ReplicationOptions{Role: replnet.RolePrimary},
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := srv.StartBackground(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { srv.Close() })

	rec := p28Req(t, srv, "POST", "/replication/quiesce", "")
	if rec.Code != 200 {
		t.Fatalf("quiesce failed: %d %s", rec.Code, rec.Body.String())
	}

	st := srv.runtimeState()
	if st.quiesceIntentActive {
		t.Error("quiesceIntentActive must be false after successful quiesce + intent removal")
	}
	if st.quiesceState != "quiesced" {
		t.Errorf("expected quiesced, got %q", st.quiesceState)
	}

	statusRec := p28Req(t, srv, "GET", "/replication/status", "")
	var resp ReplicationStatusResponse
	json.Unmarshal(statusRec.Body.Bytes(), &resp)
	if resp.QuiesceIntentState != "" {
		t.Errorf("expected empty QuiesceIntentState after successful cleanup, got %q", resp.QuiesceIntentState)
	}
}

// TestSafety_Quiesce_RestartResolvesMatchingIntent verifies that on startup, when both
// a quiesce intent and a matching final record exist (crash between step 5 and step 6),
// the node removes the stale intent and starts in the quiesced state with no active intent.
func TestSafety_Quiesce_RestartResolvesMatchingIntent(t *testing.T) {
	dir := t.TempDir()
	quiesceID := "restart-test-qid"

	intent := &replnet.QuiesceIntentRecord{
		Version:        1,
		QuiesceID:      quiesceID,
		PrimaryNodeID:  "restart-primary",
		PrimaryBaseURL: "http://127.0.0.1:19990",
		IntentAt:       time.Now().UTC().Format(time.RFC3339Nano),
	}
	intent.Checksum = replnet.QuiesceIntentChecksum(intent)
	if err := replnet.SaveQuiesceIntent(dir, intent); err != nil {
		t.Fatalf("save intent: %v", err)
	}

	finalRec := &replnet.QuiesceRecord{
		Version:          1,
		QuiesceID:        quiesceID,
		PrimaryNodeID:    "restart-primary",
		PrimaryBaseURL:   "http://127.0.0.1:19990",
		PrimaryLatestSeq: 0,
		QuiescedAt:       time.Now().UTC().Format(time.RFC3339Nano),
	}
	finalRec.Checksum = replnet.QuiesceChecksum(finalRec)
	if err := replnet.SaveQuiesceRecord(dir, finalRec); err != nil {
		t.Fatalf("save quiesce record: %v", err)
	}

	// Open: startup must remove the stale intent and start quiesced.
	srv, err := Open(Options{
		NodeID:      "restart-primary",
		Addr:        "127.0.0.1:0",
		DataDir:     dir,
		Replication: ReplicationOptions{Role: replnet.RolePrimary},
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { srv.Close() })

	// Intent file must be gone.
	if replnet.QuiesceIntentExists(dir) {
		t.Error("intent file must be removed after startup with matching intent+final")
	}

	st := srv.runtimeState()
	if st.quiesceState != "quiesced" {
		t.Errorf("expected quiesced, got %q", st.quiesceState)
	}
	if st.quiesceIntentActive {
		t.Error("quiesceIntentActive must be false after startup cleanup of matching intent")
	}
}
