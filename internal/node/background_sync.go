package node

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/YashPatel2395/ShardForgeDB/internal/replnet"
)

// backgroundSyncWorker periodically calls SyncFromPrimary to pull new entries from
// the primary automatically. It is owned by the Server and started/stopped with it.
//
// Scope: pull-based automatic sync only. No push, no Raft, no consensus, no failover.
// The worker reuses the same SyncFromPrimary path used by POST /replication/sync, so all
// durability, idempotency, and concurrency guarantees from Phase 26 are preserved.
type backgroundSyncWorker struct {
	cfg BackgroundSyncConfig

	// syncFn is the synchronization function to call each interval.
	// In production this is server.SyncFromPrimary. Injectable for tests.
	syncFn func(ctx context.Context) (SyncResult, error)

	// nowFn returns the current time. Injectable for tests.
	nowFn func() time.Time

	// afterFn creates a timer channel that fires after d. Injectable for tests.
	// Returns the channel and a stop function (mirrors time.NewTimer API).
	afterFn func(d time.Duration) (<-chan time.Time, func() bool)

	// jitterFn computes jitter to add to backoff. Injectable for deterministic tests.
	// fraction is in [0.0, 1.0]; base is the current backoff duration.
	jitterFn func(fraction float64, base time.Duration) time.Duration

	// preLaunchHook is called inside start() while holding lifecycleMu, after cancel
	// has been assigned but before wg.Add(1) and goroutine launch. Test seam only;
	// nil in production.
	preLaunchHook func()

	// started guards against duplicate start() calls. CompareAndSwap(false, true) in
	// start() ensures the goroutine is launched at most once. The CAS is performed
	// inside lifecycleMu so that if stop() wins the mutex first (observes cancel==nil),
	// start() has NOT consumed the CAS slot and a subsequent call to start() will
	// still succeed.
	started atomic.Bool

	// lifecycleMu serialises the CAS + cancel-assignment + wg.Add(1) + goroutine-launch
	// sequence in start() with the cancel-read sequence in stop(). This makes start()
	// linearizable: either stop() completes before start() begins (no-op, CAS unused),
	// or start() runs to completion before stop() can read cancel.
	lifecycleMu sync.Mutex

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu     sync.RWMutex
	status BackgroundSyncStatus
}

// newBackgroundSyncWorker constructs a worker with production defaults.
// syncFn is the actual synchronization function (server.SyncFromPrimary).
func newBackgroundSyncWorker(cfg BackgroundSyncConfig, syncFn func(ctx context.Context) (SyncResult, error)) *backgroundSyncWorker {
	w := &backgroundSyncWorker{
		cfg:    cfg,
		syncFn: syncFn,
		// nowFn always returns UTC so that all timestamps stored in BackgroundSyncStatus
		// have a canonical timezone regardless of the host's local clock setting.
		nowFn: func() time.Time { return time.Now().UTC() },
		afterFn: func(d time.Duration) (<-chan time.Time, func() bool) {
			t := time.NewTimer(d)
			return t.C, t.Stop
		},
		jitterFn: func(fraction float64, base time.Duration) time.Duration {
			if fraction <= 0 || base <= 0 {
				return 0
			}
			maxJitter := float64(base) * fraction
			//nolint:gosec // math/rand is fine for jitter; no security requirement
			return time.Duration(rand.Float64() * maxJitter)
		},
	}
	w.status = BackgroundSyncStatus{
		Enabled: cfg.Enabled,
		State:   WorkerStateDisabled,
	}
	return w
}

// start spawns the background goroutine. Returns ErrAlreadyStarted if called more
// than once. The returned error is always typed so callers can use errors.Is.
// Safe to call while holding an outer lock (the function is non-blocking: it only
// initialises state and launches a goroutine, then returns immediately).
func (w *backgroundSyncWorker) start() error {
	// Hold lifecycleMu for the entire start sequence: CAS → cancel-assignment →
	// preLaunchHook → wg.Add(1) → goroutine launch.
	//
	// This makes start() linearizable with respect to stop():
	//   • If stop() acquires lifecycleMu first: it sees cancel==nil, returns no-op,
	//     and the CAS has NOT been consumed — a subsequent start() will still succeed.
	//   • If start() acquires lifecycleMu first: stop() blocks until wg.Add(1) has
	//     been called and the goroutine is launched, guaranteeing wg.Wait() always
	//     has a matching Done().
	w.lifecycleMu.Lock()
	defer w.lifecycleMu.Unlock()

	if !w.started.CompareAndSwap(false, true) {
		return ErrAlreadyStarted
	}

	ctx, cancel := context.WithCancel(context.Background())

	w.mu.Lock()
	w.ctx = ctx
	w.cancel = cancel
	w.status.State = WorkerStateStarting
	w.mu.Unlock()

	if w.preLaunchHook != nil {
		w.preLaunchHook()
	}

	w.wg.Add(1)
	go w.run()
	return nil
}

// stop signals the worker to stop and waits for the goroutine to exit.
// Safe to call before start() (no-op), after the worker has already stopped
// (no-op because context is idempotent and wg is already at zero), or
// concurrently from multiple goroutines.
func (w *backgroundSyncWorker) stop() {
	// Synchronise with start(): hold lifecycleMu briefly while reading cancel.
	// This ensures that if cancel is non-nil when we observe it, start() has
	// already called wg.Add(1), so our subsequent wg.Wait() will always find a
	// matching Done() from the goroutine.
	w.lifecycleMu.Lock()
	w.mu.RLock()
	cancel := w.cancel
	w.mu.RUnlock()
	w.lifecycleMu.Unlock()

	if cancel == nil {
		// start() was never called (or hasn't set cancel yet); nothing to stop.
		return
	}

	// Transition to Stopping state (unless already blocked or stopped).
	w.mu.Lock()
	state := w.status.State
	if state != WorkerStateBlocked && state != WorkerStateStopped && state != WorkerStateStopping {
		w.status.State = WorkerStateStopping
	}
	w.mu.Unlock()

	cancel()    // idempotent; safe to call multiple times
	w.wg.Wait() // returns immediately if goroutine already exited
}

// Status returns a point-in-time snapshot of the worker's state.
func (w *backgroundSyncWorker) Status() BackgroundSyncStatus {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.status
}

// run is the worker goroutine. It performs an initial sync immediately, then
// loops at the configured interval until the context is cancelled.
func (w *backgroundSyncWorker) run() {
	defer w.wg.Done()
	defer func() {
		w.mu.Lock()
		w.status.State = WorkerStateStopped
		w.status.Running = false
		w.mu.Unlock()
	}()

	w.mu.Lock()
	w.status.State = WorkerStateRunning
	w.status.Running = true
	w.mu.Unlock()

	currentBackoff := time.Duration(0)

	// Initial sync: run immediately on startup without waiting for the interval.
	if blocked := w.doSync(&currentBackoff); blocked {
		// Terminal error (e.g. replication gap). Wait for shutdown; do not retry.
		<-w.ctx.Done()
		return
	}

	for {
		// Choose wait duration: backoff if recovering, normal interval otherwise.
		var waitDur time.Duration
		if currentBackoff > 0 {
			jitter := w.jitterFn(w.cfg.JitterFraction, currentBackoff)
			waitDur = currentBackoff + jitter

			nextRetry := w.nowFn().Add(waitDur)
			w.mu.Lock()
			w.status.State = WorkerStateBackingOff
			w.status.CurrentBackoffMs = currentBackoff.Milliseconds()
			w.status.NextRetryAt = &nextRetry
			w.mu.Unlock()
		} else {
			waitDur = w.cfg.Interval.Duration

			w.mu.Lock()
			w.status.State = WorkerStateRunning
			w.status.CurrentBackoffMs = 0
			w.status.NextRetryAt = nil
			w.mu.Unlock()
		}

		// Wait for the computed duration or shutdown signal.
		timerC, stopTimer := w.afterFn(waitDur)
		select {
		case <-w.ctx.Done():
			stopTimer()
			return
		case <-timerC:
		}

		if blocked := w.doSync(&currentBackoff); blocked {
			<-w.ctx.Done()
			return
		}
	}
}

// doSync calls syncFn once and updates status accordingly.
// Returns true (terminal) if the worker should enter the blocked state and stop retrying.
// Updates *currentBackoff: resets to 0 on success or ErrSyncInProgress, doubles on failure.
func (w *backgroundSyncWorker) doSync(currentBackoff *time.Duration) (blocked bool) {
	now := w.nowFn()

	// Mark attempt start; bump attempt counter.
	w.mu.Lock()
	if w.status.State != WorkerStateBlocked {
		w.status.State = WorkerStateRunning
	}
	w.status.LastAttemptAt = &now
	w.status.TotalAttempts++
	w.mu.Unlock()

	// Call syncFn with a per-request timeout so a hung primary doesn't block forever.
	// The per-request ctx is derived from the worker ctx, so cancelling the worker
	// also cancels in-flight requests.
	reqCtx, cancel := context.WithTimeout(w.ctx, w.cfg.RequestTimeout.Duration)
	defer cancel()

	result, err := w.syncFn(reqCtx)
	afterNow := w.nowFn()

	// ── ErrSyncInProgress: skip, not a failure ─────────────────────────────────
	// A concurrent manual sync (POST /replication/sync) holds the syncInProgress
	// flag. Skip this attempt entirely and reset the backoff to 0 so the next
	// wait uses the normal Interval rather than any previously accumulated backoff.
	if errors.Is(err, ErrSyncInProgress) {
		w.mu.Lock()
		w.status.TotalAttempts-- // skips are not counted as attempts
		w.status.TotalSkippedBusy++
		w.mu.Unlock()
		*currentBackoff = 0 // reset: next wait uses Interval, not backoff
		return false
	}

	// ── Worker context cancelled: clean shutdown ────────────────────────────────
	// Distinguish worker shutdown (w.ctx cancelled) from per-request timeout
	// (reqCtx deadline exceeded while w.ctx is still live). Only the latter is a
	// real failure worth counting. Shutdown is never a failure.
	if w.ctx.Err() != nil {
		// Worker is shutting down. Back out the TotalAttempts increment so
		// shutdown-time in-flight requests are invisible to observers.
		w.mu.Lock()
		w.status.TotalAttempts--
		w.mu.Unlock()
		return false // run() will see ctx.Done() and exit cleanly
	}

	// ── Terminal error: replication gap ────────────────────────────────────────
	if err != nil {
		var gapErr *replnet.ReplicationGapError
		if errors.As(err, &gapErr) {
			w.mu.Lock()
			w.status.TotalFailures++
			w.status.ConsecutiveFailures++
			w.status.LastFailureAt = &afterNow
			w.status.LastError = err.Error()
			w.status.State = WorkerStateBlocked
			// Running=false: no future sync loop will execute while blocked.
			// The goroutine stays alive only to receive the shutdown signal.
			w.status.Running = false
			w.status.BlockedReason = "replication_gap"
			w.status.Gap = gapErr
			w.status.LagKnown = false
			// Clear retry state: no future retry will ever happen from blocked state.
			w.status.CurrentBackoffMs = 0
			w.status.NextRetryAt = nil
			w.mu.Unlock()
			return true // terminal
		}

		// ── Temporary failure: exponential backoff ─────────────────────────────
		w.mu.Lock()
		w.status.TotalFailures++
		w.status.ConsecutiveFailures++
		w.status.LastFailureAt = &afterNow
		w.status.LastError = err.Error()
		w.status.LagKnown = false // lag is stale; primary unreachable
		w.mu.Unlock()

		// Compute next backoff: double the current, cap at MaxBackoff.
		if *currentBackoff <= 0 {
			*currentBackoff = w.cfg.InitialBackoff.Duration
		} else {
			*currentBackoff *= 2
		}
		if *currentBackoff > w.cfg.MaxBackoff.Duration {
			*currentBackoff = w.cfg.MaxBackoff.Duration
		}
		return false
	}

	// ── Success ────────────────────────────────────────────────────────────────
	*currentBackoff = 0 // reset backoff

	// Lag is known only when the primary reported primary_latest_seq.
	// result.LagKnown is false when the primary omits the field (Phase-26 compat).
	var lagEntries int64
	if result.LagKnown && result.PrimaryLatestSeq >= result.LastAppliedSeq {
		lagEntries = int64(result.PrimaryLatestSeq - result.LastAppliedSeq)
	}
	// Defensive clamp: if primary_latest_seq < last_applied_seq (should not occur
	// in correct operation), report 0 rather than a negative value.

	w.mu.Lock()
	w.status.TotalSuccesses++
	w.status.ConsecutiveFailures = 0
	w.status.LastSuccessAt = &afterNow
	w.status.LastError = ""
	w.status.LastFetched = result.Fetched
	w.status.LastApplied = result.Applied
	w.status.State = WorkerStateRunning
	w.status.CurrentBackoffMs = 0
	w.status.NextRetryAt = nil
	w.status.BlockedReason = ""
	w.status.Gap = nil

	// Lag tracking: update fields from the sync result.
	w.status.FollowerLastAppliedSeq = result.LastAppliedSeq
	w.status.PrimaryLatestSeq = result.PrimaryLatestSeq
	w.status.LagKnown = result.LagKnown
	w.status.LagEntries = lagEntries
	if result.LagKnown {
		w.status.LagObservedAt = &afterNow
	}
	w.mu.Unlock()

	return false
}
