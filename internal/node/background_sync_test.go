package node

import (
	"context"
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/YashPatel2395/ShardForgeDB/internal/replnet"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// minBgSyncCfg returns a BackgroundSyncConfig with valid positive values.
func minBgSyncCfg() BackgroundSyncConfig {
	return BackgroundSyncConfig{
		Enabled:        true,
		Interval:       Duration{100 * time.Millisecond},
		RequestTimeout: Duration{2 * time.Second},
		InitialBackoff: Duration{50 * time.Millisecond},
		MaxBackoff:     Duration{500 * time.Millisecond},
		JitterFraction: 0,
	}
}

// newTestWorker creates a worker with a controllable timer and fake clock.
// syncFn is the injected sync function; call mustStartWorker to start it.
func newTestWorker(t *testing.T, cfg BackgroundSyncConfig, syncFn func(ctx context.Context) (SyncResult, error)) *backgroundSyncWorker {
	t.Helper()
	w := newBackgroundSyncWorker(cfg, syncFn)
	// Disable jitter for deterministic tests.
	w.jitterFn = func(_ float64, _ time.Duration) time.Duration { return 0 }
	return w
}

// mustStartWorker calls w.start() and fails the test on error.
func mustStartWorker(t *testing.T, w *backgroundSyncWorker) {
	t.Helper()
	if err := w.start(); err != nil {
		t.Fatalf("worker.start(): %v", err)
	}
}

// controlledTimer provides a timer channel that the test drives manually.
type controlledTimer struct {
	mu      sync.Mutex
	pending []chan time.Time // one per afterFn call
}

func (ct *controlledTimer) afterFn(d time.Duration) (<-chan time.Time, func() bool) {
	ch := make(chan time.Time, 1)
	ct.mu.Lock()
	ct.pending = append(ct.pending, ch)
	ct.mu.Unlock()
	return ch, func() bool { return true }
}

// fireNext fires the next pending timer. Returns false if none are pending after waiting briefly.
func (ct *controlledTimer) fireNext(t *testing.T) bool {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ct.mu.Lock()
		if len(ct.pending) > 0 {
			ch := ct.pending[0]
			ct.pending = ct.pending[1:]
			ct.mu.Unlock()
			ch <- time.Now()
			return true
		}
		ct.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

// waitForState polls the worker status until the expected state is reached or timeout.
func waitForState(t *testing.T, w *backgroundSyncWorker, want WorkerState) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if w.Status().State == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for state %q; got %q", want, w.Status().State)
}

// waitForAttempts polls until TotalAttempts >= n.
func waitForAttempts(t *testing.T, w *backgroundSyncWorker, n int64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if w.Status().TotalAttempts >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for TotalAttempts >= %d; got %d", n, w.Status().TotalAttempts)
}

// waitForSuccesses polls until TotalSuccesses >= n.
func waitForSuccesses(t *testing.T, w *backgroundSyncWorker, n int64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if w.Status().TotalSuccesses >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for TotalSuccesses >= %d; got %d", n, w.Status().TotalSuccesses)
}

// ── Configuration tests ────────────────────────────────────────────────────────

func TestBackgroundSync_Config_DisabledByDefault(t *testing.T) {
	cfg := BackgroundSyncConfig{}
	if cfg.Enabled {
		t.Fatal("BackgroundSyncConfig should be disabled by default")
	}
}

func TestBackgroundSync_Config_FollowerEnabled_Accepted(t *testing.T) {
	opts := Options{
		NodeID:  "f1",
		Addr:    "127.0.0.1:0",
		DataDir: t.TempDir(),
		Replication: ReplicationOptions{
			Role:           replnet.RoleFollower,
			PrimaryBaseURL: "http://127.0.0.1:9999",
			BackgroundSync: minBgSyncCfg(),
		},
	}
	if err := opts.validate(); err != nil {
		t.Fatalf("expected no error for follower with bg sync enabled, got: %v", err)
	}
}

func TestBackgroundSync_Config_PrimaryEnabled_Rejected(t *testing.T) {
	opts := Options{
		NodeID:  "primary1",
		Addr:    "127.0.0.1:0",
		DataDir: t.TempDir(),
		Replication: ReplicationOptions{
			Role:           replnet.RolePrimary,
			BackgroundSync: minBgSyncCfg(),
		},
	}
	err := opts.validate()
	if err == nil {
		t.Fatal("expected error: background sync must not be enabled on primary")
	}
	if !errors.Is(err, ErrInvalidOptions) {
		t.Errorf("expected ErrInvalidOptions, got: %v", err)
	}
}

func TestBackgroundSync_Config_StandaloneEnabled_Rejected(t *testing.T) {
	opts := Options{
		NodeID:  "sa1",
		Addr:    "127.0.0.1:0",
		DataDir: t.TempDir(),
		Replication: ReplicationOptions{
			Role:           "",
			BackgroundSync: minBgSyncCfg(),
		},
	}
	err := opts.validate()
	if err == nil {
		t.Fatal("expected error: background sync must not be enabled on standalone")
	}
	if !errors.Is(err, ErrInvalidOptions) {
		t.Errorf("expected ErrInvalidOptions, got: %v", err)
	}
}

func TestBackgroundSync_Config_InvalidInterval_Rejected(t *testing.T) {
	cfg := minBgSyncCfg()
	cfg.Interval = Duration{0}
	err := cfg.validate()
	if err == nil {
		t.Fatal("expected error for zero Interval")
	}
}

func TestBackgroundSync_Config_InvalidRequestTimeout_Rejected(t *testing.T) {
	cfg := minBgSyncCfg()
	cfg.RequestTimeout = Duration{0}
	err := cfg.validate()
	if err == nil {
		t.Fatal("expected error for zero RequestTimeout")
	}
}

func TestBackgroundSync_Config_InvalidInitialBackoff_Rejected(t *testing.T) {
	cfg := minBgSyncCfg()
	cfg.InitialBackoff = Duration{0}
	err := cfg.validate()
	if err == nil {
		t.Fatal("expected error for zero InitialBackoff")
	}
}

func TestBackgroundSync_Config_MaxBackoffLessThanInitial_Rejected(t *testing.T) {
	cfg := minBgSyncCfg()
	cfg.InitialBackoff = Duration{200 * time.Millisecond}
	cfg.MaxBackoff = Duration{100 * time.Millisecond}
	err := cfg.validate()
	if err == nil {
		t.Fatal("expected error: MaxBackoff < InitialBackoff")
	}
}

func TestBackgroundSync_Config_JitterOutOfRange_Rejected(t *testing.T) {
	cfg := minBgSyncCfg()
	cfg.JitterFraction = 1.1
	err := cfg.validate()
	if err == nil {
		t.Fatal("expected error: JitterFraction > 1.0")
	}

	cfg.JitterFraction = -0.1
	err = cfg.validate()
	if err == nil {
		t.Fatal("expected error: JitterFraction < 0")
	}
}

func TestBackgroundSync_Config_NaNJitter_Rejected(t *testing.T) {
	cfg := minBgSyncCfg()
	cfg.JitterFraction = math.NaN()
	err := cfg.validate()
	if err == nil {
		t.Fatal("expected error: JitterFraction = NaN (NaN comparisons return false, must be explicitly guarded)")
	}
}

func TestBackgroundSync_Config_InfJitter_Rejected(t *testing.T) {
	cfg := minBgSyncCfg()
	cfg.JitterFraction = math.Inf(1) // +Inf
	err := cfg.validate()
	if err == nil {
		t.Fatal("expected error: JitterFraction = +Inf")
	}

	cfg.JitterFraction = math.Inf(-1) // -Inf
	err = cfg.validate()
	if err == nil {
		t.Fatal("expected error: JitterFraction = -Inf")
	}
}

func TestBackgroundSync_Config_Disabled_ValidatesOK(t *testing.T) {
	cfg := BackgroundSyncConfig{Enabled: false}
	// All other fields zero: should pass since Enabled=false.
	if err := cfg.validate(); err != nil {
		t.Fatalf("disabled config should validate OK, got: %v", err)
	}
}

func TestBackgroundSync_Config_OldConfig_BackwardCompatible(t *testing.T) {
	// Old Phase 25/26 config: no BackgroundSync field → zero value → disabled.
	opts := Options{
		NodeID:  "follower",
		Addr:    "127.0.0.1:0",
		DataDir: t.TempDir(),
		Replication: ReplicationOptions{
			Role:           replnet.RoleFollower,
			PrimaryBaseURL: "http://127.0.0.1:9999",
			// BackgroundSync intentionally absent (zero value).
		},
	}
	if err := opts.validate(); err != nil {
		t.Fatalf("old config without BackgroundSync should still validate: %v", err)
	}
}

// ── Lifecycle tests ────────────────────────────────────────────────────────────

func TestBackgroundSync_Worker_StartsOnce(t *testing.T) {
	cfg := minBgSyncCfg()
	cfg.Interval = Duration{10 * time.Second} // long so we can inspect state

	callCount := int64(0)
	w := newTestWorker(t, cfg, func(ctx context.Context) (SyncResult, error) {
		atomic.AddInt64(&callCount, 1)
		return SyncResult{LastAppliedSeq: 1, PrimaryLatestSeq: 1, LagKnown: true}, nil
	})

	mustStartWorker(t, w)
	defer w.stop()

	// Wait for the initial sync to complete.
	waitForSuccesses(t, w, 1)
	if w.Status().State != WorkerStateRunning {
		t.Errorf("expected running state, got %q", w.Status().State)
	}
}

func TestBackgroundSync_Worker_StartTwice_ReturnsAlreadyStarted(t *testing.T) {
	cfg := minBgSyncCfg()
	cfg.Interval = Duration{10 * time.Second}

	w := newTestWorker(t, cfg, func(ctx context.Context) (SyncResult, error) {
		return SyncResult{}, nil
	})

	mustStartWorker(t, w)
	defer w.stop()

	// Second start must return ErrAlreadyStarted and must not launch another goroutine.
	err := w.start()
	if !errors.Is(err, ErrAlreadyStarted) {
		t.Errorf("second start() returned %v, want ErrAlreadyStarted", err)
	}
}

func TestBackgroundSync_Worker_StopBeforeStart_Safe(t *testing.T) {
	cfg := minBgSyncCfg()
	w := newTestWorker(t, cfg, func(ctx context.Context) (SyncResult, error) {
		return SyncResult{}, nil
	})
	// stop() before start() must be a silent no-op (not panic, not deadlock).
	w.stop()
}

func TestBackgroundSync_Worker_StopTwice_Safe(t *testing.T) {
	cfg := minBgSyncCfg()
	cfg.Interval = Duration{10 * time.Second}

	w := newTestWorker(t, cfg, func(ctx context.Context) (SyncResult, error) {
		return SyncResult{}, nil
	})
	mustStartWorker(t, w)
	waitForSuccesses(t, w, 1)

	w.stop() // first stop
	w.stop() // second stop: must be a silent no-op (not panic, not deadlock)
	if w.Status().State != WorkerStateStopped {
		t.Errorf("state after double stop = %q, want stopped", w.Status().State)
	}
}

func TestBackgroundSync_Worker_StartAfterStop_Rejected(t *testing.T) {
	cfg := minBgSyncCfg()
	cfg.Interval = Duration{10 * time.Second}

	w := newTestWorker(t, cfg, func(ctx context.Context) (SyncResult, error) {
		return SyncResult{}, nil
	})
	mustStartWorker(t, w)
	waitForSuccesses(t, w, 1)
	w.stop()

	// start() after stop() must return ErrAlreadyStarted (worker is single-use).
	err := w.start()
	if !errors.Is(err, ErrAlreadyStarted) {
		t.Errorf("start() after stop() returned %v, want ErrAlreadyStarted", err)
	}
}

func TestBackgroundSync_Worker_StopsOnClose(t *testing.T) {
	cfg := minBgSyncCfg()
	cfg.Interval = Duration{10 * time.Second}

	w := newTestWorker(t, cfg, func(ctx context.Context) (SyncResult, error) {
		return SyncResult{}, nil
	})

	mustStartWorker(t, w)
	waitForSuccesses(t, w, 1)

	w.stop() // must return promptly
	if w.Status().State != WorkerStateStopped {
		t.Errorf("expected stopped state, got %q", w.Status().State)
	}
	if w.Status().Running {
		t.Error("Running should be false after stop")
	}
}

func TestBackgroundSync_Worker_NoGoroutineLeak(t *testing.T) {
	cfg := minBgSyncCfg()
	cfg.Interval = Duration{10 * time.Second}

	doneCh := make(chan struct{})
	w := newTestWorker(t, cfg, func(ctx context.Context) (SyncResult, error) {
		return SyncResult{}, nil
	})

	var stopped atomic.Bool
	go func() {
		if err := w.start(); err != nil {
			t.Errorf("start: %v", err)
			close(doneCh)
			return
		}
		waitForSuccesses(t, w, 1)
		w.stop()
		stopped.Store(true)
		close(doneCh)
	}()

	select {
	case <-doneCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout: worker goroutine did not stop within 5s (goroutine leak suspected)")
	}
	if !stopped.Load() {
		t.Fatal("worker did not stop")
	}
}

func TestBackgroundSync_Worker_CancellationInterruptsWait(t *testing.T) {
	ct := &controlledTimer{}
	cfg := minBgSyncCfg()
	cfg.Interval = Duration{10 * time.Second}

	w := newTestWorker(t, cfg, func(ctx context.Context) (SyncResult, error) {
		return SyncResult{}, nil
	})
	w.afterFn = ct.afterFn

	mustStartWorker(t, w)
	waitForSuccesses(t, w, 1) // initial sync done; now waiting for timer

	// Cancel before the timer fires.
	start := time.Now()
	w.stop()
	elapsed := time.Since(start)

	// Should stop well before the 10s interval.
	if elapsed > 3*time.Second {
		t.Errorf("stop took %v; expected < 3s (cancellation should interrupt the wait)", elapsed)
	}
}

func TestBackgroundSync_Worker_CancellationInterruptsInFlightRequest(t *testing.T) {
	cfg := minBgSyncCfg()
	cfg.Interval = Duration{10 * time.Second}
	cfg.RequestTimeout = Duration{30 * time.Second} // long; cancellation should cut it short

	started := make(chan struct{})
	w := newTestWorker(t, cfg, func(ctx context.Context) (SyncResult, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		// Block until context is cancelled.
		<-ctx.Done()
		return SyncResult{}, ctx.Err()
	})

	mustStartWorker(t, w)
	<-started // sync is in-flight

	start := time.Now()
	w.stop()
	elapsed := time.Since(start)

	if elapsed > 3*time.Second {
		t.Errorf("stop with in-flight request took %v; expected < 3s", elapsed)
	}
}

func TestBackgroundSync_ShutdownCancellation_NotCountedAsFailure(t *testing.T) {
	cfg := minBgSyncCfg()
	cfg.Interval = Duration{10 * time.Second}
	cfg.RequestTimeout = Duration{30 * time.Second}

	// syncFn blocks until its context is cancelled; returns context error.
	inFlight := make(chan struct{}, 1)
	w := newTestWorker(t, cfg, func(ctx context.Context) (SyncResult, error) {
		select {
		case inFlight <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return SyncResult{}, ctx.Err()
	})

	mustStartWorker(t, w)

	select {
	case <-inFlight:
	case <-time.After(5 * time.Second):
		t.Fatal("syncFn not called within 5s")
	}

	w.stop()

	st := w.Status()
	// Cancellation due to worker shutdown must not be counted as a failure.
	if st.TotalFailures != 0 {
		t.Errorf("TotalFailures = %d, want 0 (shutdown cancellation is not a failure)", st.TotalFailures)
	}
	if st.ConsecutiveFailures != 0 {
		t.Errorf("ConsecutiveFailures = %d, want 0", st.ConsecutiveFailures)
	}
	if st.LastError != "" {
		t.Errorf("LastError = %q, want empty (shutdown is not an error)", st.LastError)
	}
}

// ── Automatic propagation tests ────────────────────────────────────────────────

func TestBackgroundSync_Worker_InitialSyncImmediate(t *testing.T) {
	cfg := minBgSyncCfg()
	cfg.Interval = Duration{10 * time.Second}

	called := make(chan struct{}, 1)
	w := newTestWorker(t, cfg, func(ctx context.Context) (SyncResult, error) {
		select {
		case called <- struct{}{}:
		default:
		}
		return SyncResult{LastAppliedSeq: 1, PrimaryLatestSeq: 1, LagKnown: true}, nil
	})

	mustStartWorker(t, w)
	defer w.stop()

	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("initial sync not called within 2s")
	}
}

func TestBackgroundSync_Worker_SuccessUpdatesStatus(t *testing.T) {
	cfg := minBgSyncCfg()
	cfg.Interval = Duration{10 * time.Second}

	w := newTestWorker(t, cfg, func(ctx context.Context) (SyncResult, error) {
		return SyncResult{
			Fetched:          3,
			Applied:          3,
			LastAppliedSeq:   5,
			PrimaryLatestSeq: 5,
			LagKnown:         true,
		}, nil
	})

	mustStartWorker(t, w)
	defer w.stop()

	waitForSuccesses(t, w, 1)
	st := w.Status()

	if st.TotalSuccesses != 1 {
		t.Errorf("TotalSuccesses = %d, want 1", st.TotalSuccesses)
	}
	if st.LastFetched != 3 {
		t.Errorf("LastFetched = %d, want 3", st.LastFetched)
	}
	if st.LastApplied != 3 {
		t.Errorf("LastApplied = %d, want 3", st.LastApplied)
	}
	if st.FollowerLastAppliedSeq != 5 {
		t.Errorf("FollowerLastAppliedSeq = %d, want 5", st.FollowerLastAppliedSeq)
	}
}

func TestBackgroundSync_Worker_EmptySuccessRemainsHealthy(t *testing.T) {
	cfg := minBgSyncCfg()
	cfg.Interval = Duration{10 * time.Second}

	w := newTestWorker(t, cfg, func(ctx context.Context) (SyncResult, error) {
		// Primary caught up: empty batch but primary_latest_seq = last applied.
		return SyncResult{
			Fetched:          0,
			Applied:          0,
			LastAppliedSeq:   3,
			PrimaryLatestSeq: 3,
			LagKnown:         true,
		}, nil
	})

	mustStartWorker(t, w)
	defer w.stop()

	waitForSuccesses(t, w, 1)
	st := w.Status()

	if st.State != WorkerStateRunning {
		t.Errorf("expected running after empty success, got %q", st.State)
	}
	if st.TotalFailures != 0 {
		t.Errorf("expected 0 failures after empty success, got %d", st.TotalFailures)
	}
	if !st.LagKnown {
		t.Error("LagKnown should be true after successful sync")
	}
	if st.LagEntries != 0 {
		t.Errorf("LagEntries should be 0 when caught up, got %d", st.LagEntries)
	}
}

// ── Retry and backoff tests ────────────────────────────────────────────────────

func TestBackgroundSync_Worker_TemporaryFailureEntersBackoff(t *testing.T) {
	ct := &controlledTimer{}
	cfg := minBgSyncCfg()
	cfg.InitialBackoff = Duration{200 * time.Millisecond}
	cfg.MaxBackoff = Duration{2 * time.Second}

	failErr := errors.New("connection refused (injected)")
	w := newTestWorker(t, cfg, func(ctx context.Context) (SyncResult, error) {
		return SyncResult{}, failErr
	})
	w.afterFn = ct.afterFn

	mustStartWorker(t, w)
	defer w.stop()

	// Wait for initial sync to fail.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if w.Status().TotalFailures >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Fire the backoff timer to trigger the next attempt.
	if !ct.fireNext(t) {
		t.Fatal("no pending timer found")
	}

	// Second failure.
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if w.Status().TotalFailures >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	st := w.Status()
	if st.TotalFailures < 2 {
		t.Errorf("TotalFailures = %d, want >= 2", st.TotalFailures)
	}
	if st.ConsecutiveFailures < 2 {
		t.Errorf("ConsecutiveFailures = %d, want >= 2", st.ConsecutiveFailures)
	}
	if st.LagKnown {
		t.Error("LagKnown should be false while primary is unreachable")
	}
}

func TestBackgroundSync_Worker_BackoffDoublesAndCaps(t *testing.T) {
	ct := &controlledTimer{}
	cfg := minBgSyncCfg()
	cfg.InitialBackoff = Duration{100 * time.Millisecond}
	cfg.MaxBackoff = Duration{400 * time.Millisecond}

	failErr := errors.New("injected failure")
	attemptCount := int64(0)
	w := newTestWorker(t, cfg, func(ctx context.Context) (SyncResult, error) {
		atomic.AddInt64(&attemptCount, 1)
		return SyncResult{}, failErr
	})
	w.afterFn = ct.afterFn

	mustStartWorker(t, w)
	defer w.stop()

	// Wait for initial sync failure.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(&attemptCount) >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Fire timer → 2nd attempt (backoff = 100ms → 200ms).
	ct.fireNext(t)
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(&attemptCount) >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Fire timer → 3rd attempt (backoff = 200ms → 400ms = cap).
	ct.fireNext(t)
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(&attemptCount) >= 3 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Fire timer → 4th attempt (backoff still = 400ms, capped).
	ct.fireNext(t)
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(&attemptCount) >= 4 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	st := w.Status()
	if st.CurrentBackoffMs != 400 {
		t.Errorf("CurrentBackoffMs = %d, want 400 (capped at MaxBackoff)", st.CurrentBackoffMs)
	}
}

func TestBackgroundSync_Worker_SuccessResetsBackoff(t *testing.T) {
	ct := &controlledTimer{}
	cfg := minBgSyncCfg()
	cfg.InitialBackoff = Duration{200 * time.Millisecond}
	cfg.MaxBackoff = Duration{2 * time.Second}

	failThenSucceed := int64(0)
	w := newTestWorker(t, cfg, func(ctx context.Context) (SyncResult, error) {
		if atomic.AddInt64(&failThenSucceed, 1) == 1 {
			return SyncResult{}, errors.New("first attempt fails")
		}
		return SyncResult{LastAppliedSeq: 1, PrimaryLatestSeq: 1, LagKnown: true}, nil
	})
	w.afterFn = ct.afterFn

	mustStartWorker(t, w)
	defer w.stop()

	// Wait for initial sync failure.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if w.Status().TotalFailures >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Fire the backoff timer → triggers 2nd attempt which succeeds.
	ct.fireNext(t)

	waitForSuccesses(t, w, 1)
	st := w.Status()
	if st.CurrentBackoffMs != 0 {
		t.Errorf("CurrentBackoffMs should be 0 after success (backoff reset), got %d", st.CurrentBackoffMs)
	}
	if st.ConsecutiveFailures != 0 {
		t.Errorf("ConsecutiveFailures should be 0 after success, got %d", st.ConsecutiveFailures)
	}
}

func TestBackgroundSync_Worker_JitterWithinBounds(t *testing.T) {
	cfg := minBgSyncCfg()
	cfg.JitterFraction = 0.25
	cfg.InitialBackoff = Duration{200 * time.Millisecond}

	// Run 50 jitter samples and verify all are within [0, base*fraction].
	w := newBackgroundSyncWorker(cfg, func(ctx context.Context) (SyncResult, error) {
		return SyncResult{}, nil
	})
	base := cfg.InitialBackoff.Duration
	for i := 0; i < 50; i++ {
		j := w.jitterFn(cfg.JitterFraction, base)
		maxJitter := time.Duration(float64(base) * cfg.JitterFraction)
		if j < 0 || j > maxJitter {
			t.Errorf("jitter %v out of [0, %v]", j, maxJitter)
		}
	}
}

// ── Concurrency tests ──────────────────────────────────────────────────────────

func TestBackgroundSync_ErrSyncInProgress_TreatedAsSkip(t *testing.T) {
	ct := &controlledTimer{}
	cfg := minBgSyncCfg()
	cfg.Interval = Duration{10 * time.Second}

	// First call: normal success. Second call: ErrSyncInProgress. Third: success.
	call := int64(0)
	w := newTestWorker(t, cfg, func(ctx context.Context) (SyncResult, error) {
		n := atomic.AddInt64(&call, 1)
		switch n {
		case 1:
			return SyncResult{LastAppliedSeq: 1, PrimaryLatestSeq: 1, LagKnown: true}, nil
		case 2:
			return SyncResult{}, ErrSyncInProgress
		default:
			return SyncResult{LastAppliedSeq: 1, PrimaryLatestSeq: 1, LagKnown: true}, nil
		}
	})
	w.afterFn = ct.afterFn

	mustStartWorker(t, w)
	defer w.stop()

	waitForSuccesses(t, w, 1) // initial success

	ct.fireNext(t) // trigger ErrSyncInProgress attempt

	// Wait for skipped_busy to be incremented.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if w.Status().TotalSkippedBusy >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	st := w.Status()
	if st.TotalSkippedBusy < 1 {
		t.Errorf("TotalSkippedBusy = %d, want >= 1", st.TotalSkippedBusy)
	}
	// ErrSyncInProgress should not count as a failure.
	if st.TotalFailures != 0 {
		t.Errorf("TotalFailures = %d, want 0 (busy-skip is not a failure)", st.TotalFailures)
	}
	// Backoff should not increase.
	if st.CurrentBackoffMs != 0 {
		t.Errorf("CurrentBackoffMs = %d, want 0 (no backoff after skip)", st.CurrentBackoffMs)
	}
	// TotalAttempts should not include skips.
	if st.TotalAttempts != 1 {
		t.Errorf("TotalAttempts = %d, want 1 (skips not counted as attempts)", st.TotalAttempts)
	}
}

func TestBackgroundSync_ErrSyncInProgress_AfterBackoff_ResetsCurrentBackoff(t *testing.T) {
	// Prove: when ErrSyncInProgress is returned after a prior temporary failure
	// (which set currentBackoff > 0), the backoff is reset to 0 so the next wait
	// uses the normal Interval rather than the accumulated backoff.
	ct := &controlledTimer{}
	cfg := minBgSyncCfg()
	cfg.InitialBackoff = Duration{200 * time.Millisecond}
	cfg.MaxBackoff = Duration{2 * time.Second}

	call := int64(0)
	w := newTestWorker(t, cfg, func(ctx context.Context) (SyncResult, error) {
		n := atomic.AddInt64(&call, 1)
		if n == 1 {
			return SyncResult{}, errors.New("temporary failure — sets backoff")
		}
		return SyncResult{}, ErrSyncInProgress
	})
	w.afterFn = ct.afterFn

	mustStartWorker(t, w)
	defer w.stop()

	// Wait for first failure (backoff > 0).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if w.Status().TotalFailures >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if w.Status().TotalFailures == 0 {
		t.Fatal("timeout waiting for first failure")
	}

	// Fire backoff timer → 2nd call returns ErrSyncInProgress → currentBackoff reset to 0.
	ct.fireNext(t)

	// Wait for the skip to be counted.
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if w.Status().TotalSkippedBusy >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if w.Status().TotalSkippedBusy == 0 {
		t.Fatal("timeout waiting for skip to be counted")
	}

	// After the skip, run() re-enters the loop top and should see currentBackoff==0
	// → WorkerStateRunning (not backing_off) and CurrentBackoffMs==0.
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		st := w.Status()
		if st.CurrentBackoffMs == 0 && st.State == WorkerStateRunning {
			return // test passes
		}
		time.Sleep(5 * time.Millisecond)
	}
	st := w.Status()
	t.Errorf("after ErrSyncInProgress: CurrentBackoffMs=%d State=%q, want 0 and running (backoff reset)",
		st.CurrentBackoffMs, st.State)
}

func TestBackgroundSync_Worker_ConcurrentStartStop(t *testing.T) {
	// Prove: concurrent start() + stop() never deadlocks and the goroutine always exits
	// cleanly (no leak). The lifecycleMu in start() ensures wg.Add(1) happens-before
	// stop()'s wg.Wait(), so the WaitGroup is always balanced regardless of scheduling.
	for i := 0; i < 100; i++ {
		cfg := minBgSyncCfg()
		cfg.Interval = Duration{10 * time.Second}

		w := newTestWorker(t, cfg, func(ctx context.Context) (SyncResult, error) {
			return SyncResult{}, nil
		})

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = w.start()
		}()
		go func() {
			defer wg.Done()
			w.stop()
		}()
		wg.Wait()

		// Drain: if start() won the race, the goroutine is running; stop it.
		w.stop()

		// Final state: either the goroutine never started (state = disabled or starting),
		// or it started and has now been stopped (state = stopped).
		// In no case should we deadlock or leak a goroutine.
		state := w.Status().State
		if state != WorkerStateStopped && state != WorkerStateDisabled && state != WorkerStateStarting {
			t.Errorf("iteration %d: unexpected final state %q after concurrent start+stop", i, state)
		}
	}
}

func TestBackgroundSync_StatusRead_RaceSafe(t *testing.T) {
	cfg := minBgSyncCfg()
	cfg.Interval = Duration{5 * time.Millisecond}

	var calls int64
	w := newTestWorker(t, cfg, func(ctx context.Context) (SyncResult, error) {
		atomic.AddInt64(&calls, 1)
		return SyncResult{LastAppliedSeq: 1, PrimaryLatestSeq: 1, LagKnown: true}, nil
	})

	mustStartWorker(t, w)
	defer w.stop()

	// Concurrently read status while the worker runs.
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = w.Status()
				time.Sleep(time.Millisecond)
			}
		}()
	}
	wg.Wait()
}

// ── Lag tests ──────────────────────────────────────────────────────────────────

func TestBackgroundSync_Lag_KnownAfterSuccess(t *testing.T) {
	cfg := minBgSyncCfg()
	cfg.Interval = Duration{10 * time.Second}

	w := newTestWorker(t, cfg, func(ctx context.Context) (SyncResult, error) {
		return SyncResult{
			LastAppliedSeq:   3,
			PrimaryLatestSeq: 7,
			LagKnown:         true,
		}, nil
	})

	mustStartWorker(t, w)
	defer w.stop()

	waitForSuccesses(t, w, 1)
	st := w.Status()

	if !st.LagKnown {
		t.Error("LagKnown should be true after successful sync")
	}
	if st.LagEntries != 4 { // 7 - 3 = 4
		t.Errorf("LagEntries = %d, want 4", st.LagEntries)
	}
	if st.PrimaryLatestSeq != 7 {
		t.Errorf("PrimaryLatestSeq = %d, want 7", st.PrimaryLatestSeq)
	}
	if st.LagObservedAt == nil {
		t.Error("LagObservedAt should be set after successful sync")
	}
}

func TestBackgroundSync_Lag_ZeroWhenCaughtUp(t *testing.T) {
	cfg := minBgSyncCfg()
	cfg.Interval = Duration{10 * time.Second}

	w := newTestWorker(t, cfg, func(ctx context.Context) (SyncResult, error) {
		return SyncResult{
			LastAppliedSeq:   5,
			PrimaryLatestSeq: 5,
			LagKnown:         true,
		}, nil
	})

	mustStartWorker(t, w)
	defer w.stop()

	waitForSuccesses(t, w, 1)
	st := w.Status()

	if !st.LagKnown {
		t.Error("LagKnown should be true when caught up")
	}
	if st.LagEntries != 0 {
		t.Errorf("LagEntries = %d, want 0 when caught up", st.LagEntries)
	}
}

func TestBackgroundSync_Lag_UnknownAfterFailure(t *testing.T) {
	ct := &controlledTimer{}
	cfg := minBgSyncCfg()
	cfg.Interval = Duration{10 * time.Second}

	// First sync succeeds (lag known), second fails.
	call := int64(0)
	w := newTestWorker(t, cfg, func(ctx context.Context) (SyncResult, error) {
		if atomic.AddInt64(&call, 1) == 1 {
			return SyncResult{LastAppliedSeq: 3, PrimaryLatestSeq: 3, LagKnown: true}, nil
		}
		return SyncResult{}, errors.New("primary down")
	})
	w.afterFn = ct.afterFn

	mustStartWorker(t, w)
	defer w.stop()

	waitForSuccesses(t, w, 1) // lag is known

	ct.fireNext(t) // triggers failure

	// Wait for failure.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if w.Status().TotalFailures >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	st := w.Status()
	if st.LagKnown {
		t.Error("LagKnown should be false after primary failure")
	}
}

func TestBackgroundSync_Lag_NegativeClampedToZero(t *testing.T) {
	cfg := minBgSyncCfg()
	cfg.Interval = Duration{10 * time.Second}

	// Pathological: primary_latest_seq < last_applied_seq (should not happen but guarded).
	w := newTestWorker(t, cfg, func(ctx context.Context) (SyncResult, error) {
		return SyncResult{
			LastAppliedSeq:   10,
			PrimaryLatestSeq: 5, // lower than applied — defensive clamp
			LagKnown:         true,
		}, nil
	})

	mustStartWorker(t, w)
	defer w.stop()

	waitForSuccesses(t, w, 1)
	st := w.Status()

	if st.LagEntries < 0 {
		t.Errorf("LagEntries = %d; must be >= 0 (defensive clamp)", st.LagEntries)
	}
}

func TestBackgroundSync_Lag_NotReportedAsZeroWhenUnknown(t *testing.T) {
	cfg := minBgSyncCfg()
	cfg.Interval = Duration{10 * time.Second}

	// Sync always fails: lag should never be known.
	w := newTestWorker(t, cfg, func(ctx context.Context) (SyncResult, error) {
		return SyncResult{}, errors.New("unreachable")
	})

	mustStartWorker(t, w)
	defer w.stop()

	waitForAttempts(t, w, 1)
	st := w.Status()
	if st.LagKnown {
		t.Error("LagKnown must be false when primary has never been reached")
	}
}

// ── Terminal error tests ────────────────────────────────────────────────────────

func TestBackgroundSync_ReplicationGap_EntersBlockedState(t *testing.T) {
	cfg := minBgSyncCfg()
	cfg.Interval = Duration{10 * time.Second}

	gapErr := &replnet.ReplicationGapError{
		RequestedAfter:    3,
		FirstAvailableSeq: 10,
		LatestSeq:         20,
	}
	w := newTestWorker(t, cfg, func(ctx context.Context) (SyncResult, error) {
		return SyncResult{}, gapErr
	})

	mustStartWorker(t, w)
	defer w.stop()

	waitForState(t, w, WorkerStateBlocked)
	st := w.Status()

	if st.BlockedReason != "replication_gap" {
		t.Errorf("BlockedReason = %q, want %q", st.BlockedReason, "replication_gap")
	}
	if st.Gap == nil {
		t.Error("Gap should be non-nil in blocked state")
	}
	if st.Gap.RequestedAfter != 3 {
		t.Errorf("Gap.RequestedAfter = %d, want 3", st.Gap.RequestedAfter)
	}
	if st.LagKnown {
		t.Error("LagKnown should be false in blocked state")
	}
}

func TestBackgroundSync_ReplicationGap_DoesNotRetry(t *testing.T) {
	cfg := minBgSyncCfg()
	cfg.Interval = Duration{10 * time.Millisecond}

	gapErr := &replnet.ReplicationGapError{
		RequestedAfter:    1,
		FirstAvailableSeq: 5,
		LatestSeq:         10,
	}
	calls := int64(0)
	w := newTestWorker(t, cfg, func(ctx context.Context) (SyncResult, error) {
		atomic.AddInt64(&calls, 1)
		return SyncResult{}, gapErr
	})

	mustStartWorker(t, w)
	defer w.stop()

	waitForState(t, w, WorkerStateBlocked)
	time.Sleep(100 * time.Millisecond) // give time for spurious retries

	if c := atomic.LoadInt64(&calls); c != 1 {
		t.Errorf("syncFn called %d times, want exactly 1 (no retry after gap)", c)
	}
}

func TestBackgroundSync_ReplicationGap_GapMetadataAvailable(t *testing.T) {
	cfg := minBgSyncCfg()
	cfg.Interval = Duration{10 * time.Second}

	gapErr := &replnet.ReplicationGapError{
		RequestedAfter:    5,
		FirstAvailableSeq: 15,
		LatestSeq:         25,
	}
	w := newTestWorker(t, cfg, func(ctx context.Context) (SyncResult, error) {
		return SyncResult{}, gapErr
	})

	mustStartWorker(t, w)
	defer w.stop()

	waitForState(t, w, WorkerStateBlocked)
	st := w.Status()

	if st.Gap.RequestedAfter != 5 {
		t.Errorf("Gap.RequestedAfter = %d, want 5", st.Gap.RequestedAfter)
	}
	if st.Gap.FirstAvailableSeq != 15 {
		t.Errorf("Gap.FirstAvailableSeq = %d, want 15", st.Gap.FirstAvailableSeq)
	}
	if st.Gap.LatestSeq != 25 {
		t.Errorf("Gap.LatestSeq = %d, want 25", st.Gap.LatestSeq)
	}
}

func TestBackgroundSync_BlockedState_FieldsClearedAndRunningFalse(t *testing.T) {
	// Prove: entering blocked state clears Running, CurrentBackoffMs, and NextRetryAt.
	// To exercise a non-zero NextRetryAt before the gap, we trigger a temporary failure
	// first (which sets backoff), then fire the timer to trigger the gap error.
	ct := &controlledTimer{}
	cfg := minBgSyncCfg()
	cfg.InitialBackoff = Duration{500 * time.Millisecond}
	cfg.Interval = Duration{10 * time.Second}

	call := int64(0)
	w := newTestWorker(t, cfg, func(ctx context.Context) (SyncResult, error) {
		n := atomic.AddInt64(&call, 1)
		if n == 1 {
			return SyncResult{}, errors.New("temporary failure — populates backoff fields")
		}
		return SyncResult{}, &replnet.ReplicationGapError{
			RequestedAfter: 1, FirstAvailableSeq: 5, LatestSeq: 10,
		}
	})
	w.afterFn = ct.afterFn

	mustStartWorker(t, w)
	defer w.stop()

	// Wait for first failure (which sets backoff and NextRetryAt).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if w.Status().TotalFailures >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Fire backoff timer → 2nd call returns gap → blocked.
	ct.fireNext(t)
	waitForState(t, w, WorkerStateBlocked)

	st := w.Status()
	if st.Running {
		t.Error("Running should be false in blocked state (no future sync loop will execute)")
	}
	if st.CurrentBackoffMs != 0 {
		t.Errorf("CurrentBackoffMs = %d in blocked state, want 0", st.CurrentBackoffMs)
	}
	if st.NextRetryAt != nil {
		t.Error("NextRetryAt should be nil in blocked state (no future retry)")
	}
}

// ── Timestamp UTC tests ────────────────────────────────────────────────────────

func TestBackgroundSync_Timestamps_AreUTC(t *testing.T) {
	cfg := minBgSyncCfg()
	cfg.Interval = Duration{10 * time.Second}

	w := newTestWorker(t, cfg, func(ctx context.Context) (SyncResult, error) {
		return SyncResult{LastAppliedSeq: 1, PrimaryLatestSeq: 1, LagKnown: true}, nil
	})
	// Production nowFn returns UTC; verify that timestamps reflect this.
	// newTestWorker inherits the production nowFn from newBackgroundSyncWorker.

	mustStartWorker(t, w)
	defer w.stop()

	waitForSuccesses(t, w, 1)
	st := w.Status()

	if st.LastAttemptAt == nil {
		t.Fatal("LastAttemptAt not set after first sync attempt")
	}
	if st.LastAttemptAt.Location() != time.UTC {
		t.Errorf("LastAttemptAt.Location() = %v, want UTC", st.LastAttemptAt.Location())
	}
	if st.LastSuccessAt == nil {
		t.Fatal("LastSuccessAt not set after success")
	}
	if st.LastSuccessAt.Location() != time.UTC {
		t.Errorf("LastSuccessAt.Location() = %v, want UTC", st.LastSuccessAt.Location())
	}
	if st.LagObservedAt == nil {
		t.Fatal("LagObservedAt not set after success with LagKnown=true")
	}
	if st.LagObservedAt.Location() != time.UTC {
		t.Errorf("LagObservedAt.Location() = %v, want UTC", st.LagObservedAt.Location())
	}
}

// ── Duration JSON marshal/unmarshal tests ──────────────────────────────────────

func TestDuration_MarshalJSON_RoundTrip(t *testing.T) {
	d := Duration{500 * time.Millisecond}
	b, err := d.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if string(b) != `"500ms"` {
		t.Errorf("MarshalJSON = %s, want %q", b, "500ms")
	}

	var d2 Duration
	if err := d2.UnmarshalJSON(b); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if d2.Duration != d.Duration {
		t.Errorf("round-trip: got %v, want %v", d2.Duration, d.Duration)
	}
}

func TestDuration_UnmarshalJSON_InvalidString_Errors(t *testing.T) {
	var d Duration
	if err := d.UnmarshalJSON([]byte(`"not-a-duration"`)); err == nil {
		t.Fatal("expected error for invalid duration string")
	}
}

func TestDuration_UnmarshalJSON_NonString_Errors(t *testing.T) {
	var d Duration
	if err := d.UnmarshalJSON([]byte(`42`)); err == nil {
		t.Fatal("expected error when JSON value is not a string")
	}
}
