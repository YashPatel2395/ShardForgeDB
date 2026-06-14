package node

import (
	"context"
	"errors"
	"math/rand"
	"sync"
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
		nowFn:  time.Now,
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

// start spawns the background goroutine. Must be called at most once.
func (w *backgroundSyncWorker) start() {
	ctx, cancel := context.WithCancel(context.Background())
	w.ctx = ctx
	w.cancel = cancel

	w.mu.Lock()
	w.status.State = WorkerStateStarting
	w.mu.Unlock()

	w.wg.Add(1)
	go w.run()
}

// stop signals the worker to stop and waits for the goroutine to exit.
// Safe to call before start() or after the worker has already stopped.
func (w *backgroundSyncWorker) stop() {
	if w.cancel != nil {
		w.cancel()
	}
	w.wg.Wait()
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
// Updates *currentBackoff: resets to 0 on success, doubles on failure (capped at MaxBackoff).
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
	ctx, cancel := context.WithTimeout(w.ctx, w.cfg.RequestTimeout.Duration)
	defer cancel()

	result, err := w.syncFn(ctx)
	afterNow := w.nowFn()

	// ── ErrSyncInProgress: skip, not a failure ─────────────────────────────────
	if errors.Is(err, ErrSyncInProgress) {
		w.mu.Lock()
		w.status.TotalAttempts-- // skips are not counted as attempts
		w.status.TotalSkippedBusy++
		w.mu.Unlock()
		// Leave currentBackoff unchanged; wait the normal interval.
		return false
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
			w.status.Running = true // goroutine stays alive; user must restart
			w.status.BlockedReason = "replication_gap"
			w.status.Gap = gapErr
			w.status.LagKnown = false
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

	// Lag is always known after a successful sync: we contacted the primary and
	// received an authoritative primary_latest_seq. If primary_latest_seq == 0,
	// the primary has no entries yet — that means lag == 0.
	var lagEntries int64
	if result.PrimaryLatestSeq >= result.LastAppliedSeq {
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

	// Lag tracking: always known after a successful sync (even empty primary).
	w.status.FollowerLastAppliedSeq = result.LastAppliedSeq
	w.status.PrimaryLatestSeq = result.PrimaryLatestSeq
	w.status.LagKnown = true
	w.status.LagEntries = lagEntries
	w.status.LagObservedAt = &afterNow
	w.mu.Unlock()

	return false
}
