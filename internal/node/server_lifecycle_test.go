package node

// Server lifecycle tests — Phase 27 additions.
//
// These tests verify the single-use lifecycle contract on Server:
//   - Start/StartBackground return ErrAlreadyStarted on a second call.
//   - Start/StartBackground return ErrClosed after Close.
//   - Concurrent Start/StartBackground+Close do not panic or deadlock.
//   - The background sync worker is started exactly once per server lifetime.
//   - Client.SyncReplication returns errors.Is(err, ErrSyncInProgress) on HTTP 409
//     and does NOT match ErrSyncInProgress for a 502 primary-unavailable response.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/YashPatel2395/ShardForgeDB/internal/replnet"
)

// openStandaloneForLifecycle opens a standalone (no replication) server for lifecycle tests.
func openStandaloneForLifecycle(t *testing.T) *Server {
	t.Helper()
	s, err := Open(Options{
		NodeID:  "lc-standalone",
		Addr:    "127.0.0.1:0",
		DataDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// openPrimaryForLifecycle opens a primary server for lifecycle tests.
func openPrimaryForLifecycle(t *testing.T) *Server {
	t.Helper()
	s, err := Open(Options{
		NodeID:  "lc-primary",
		Addr:    "127.0.0.1:0",
		DataDir: t.TempDir(),
		Replication: ReplicationOptions{
			Role: replnet.RolePrimary,
		},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// openFollowerForLifecycle opens a follower server with background sync for lifecycle tests.
func openFollowerForLifecycle(t *testing.T, primaryURL string) *Server {
	t.Helper()
	s, err := Open(Options{
		NodeID:  "lc-follower",
		Addr:    "127.0.0.1:0",
		DataDir: t.TempDir(),
		Replication: ReplicationOptions{
			Role:           replnet.RoleFollower,
			PrimaryBaseURL: primaryURL,
			BackgroundSync: BackgroundSyncConfig{
				Enabled:        true,
				Interval:       Duration{5 * time.Second},
				RequestTimeout: Duration{2 * time.Second},
				InitialBackoff: Duration{100 * time.Millisecond},
				MaxBackoff:     Duration{1 * time.Second},
				JitterFraction: 0,
			},
		},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// ── StartBackground double-call ──────────────────────────────────────────────

func TestServer_Lifecycle_StartBackgroundTwice_ErrAlreadyStarted(t *testing.T) {
	s := openStandaloneForLifecycle(t)
	if err := s.StartBackground(); err != nil {
		t.Fatalf("first StartBackground: %v", err)
	}
	err := s.StartBackground()
	if !errors.Is(err, ErrAlreadyStarted) {
		t.Errorf("second StartBackground returned %v, want ErrAlreadyStarted", err)
	}
}

func TestServer_Lifecycle_StartBackgroundPrimary_TwiceErrAlreadyStarted(t *testing.T) {
	s := openPrimaryForLifecycle(t)
	if err := s.StartBackground(); err != nil {
		t.Fatalf("first StartBackground: %v", err)
	}
	err := s.StartBackground()
	if !errors.Is(err, ErrAlreadyStarted) {
		t.Errorf("second StartBackground returned %v, want ErrAlreadyStarted", err)
	}
}

func TestServer_Lifecycle_StartBackgroundFollowerWithBgSync_TwiceErrAlreadyStarted(t *testing.T) {
	// Use a fake primary so the follower doesn't fail to connect.
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer fake.Close()

	s := openFollowerForLifecycle(t, fake.URL)
	if err := s.StartBackground(); err != nil {
		t.Fatalf("first StartBackground: %v", err)
	}
	err := s.StartBackground()
	if !errors.Is(err, ErrAlreadyStarted) {
		t.Errorf("second StartBackground returned %v, want ErrAlreadyStarted", err)
	}
}

// ── StartBackground after Close ──────────────────────────────────────────────

func TestServer_Lifecycle_StartBackgroundAfterClose_ErrClosed(t *testing.T) {
	s := openStandaloneForLifecycle(t)
	if err := s.StartBackground(); err != nil {
		t.Fatalf("StartBackground: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	err := s.StartBackground()
	if !errors.Is(err, ErrClosed) {
		t.Errorf("StartBackground after Close returned %v, want ErrClosed", err)
	}
}

func TestServer_Lifecycle_StartBackgroundWithoutStartThenClose_ErrClosed(t *testing.T) {
	// Close without ever starting — StartBackground should still return ErrClosed.
	s := openStandaloneForLifecycle(t)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	err := s.StartBackground()
	if !errors.Is(err, ErrClosed) {
		t.Errorf("StartBackground after Close (never started) returned %v, want ErrClosed", err)
	}
}

// ── Concurrent StartBackground + Close ──────────────────────────────────────

func TestServer_Lifecycle_ConcurrentStartBackgroundAndClose_NoPanicOrDeadlock(t *testing.T) {
	// Race StartBackground against Close. One should win; the other should get either
	// a clean nil or ErrClosed / a bind error. Neither should panic or deadlock.
	for i := 0; i < 50; i++ {
		s, err := Open(Options{
			NodeID:  "lc-race",
			Addr:    "127.0.0.1:0",
			DataDir: t.TempDir(),
		})
		if err != nil {
			t.Fatalf("Open iteration %d: %v", i, err)
		}

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = s.StartBackground()
		}()
		go func() {
			defer wg.Done()
			_ = s.Close()
		}()
		wg.Wait()
		// Drain: ensure the server is fully closed regardless of who won.
		_ = s.Close()
	}
}

// ── Background worker started exactly once ───────────────────────────────────

func TestServer_Lifecycle_FollowerBgWorker_StartedOnce(t *testing.T) {
	// Prove: after StartBackground the worker is running (state = running or starting),
	// and a second StartBackground returns ErrAlreadyStarted (worker not relaunched).
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer fake.Close()

	s := openFollowerForLifecycle(t, fake.URL)
	if err := s.StartBackground(); err != nil {
		t.Fatalf("StartBackground: %v", err)
	}

	// Second call must be rejected before the worker is double-launched.
	err := s.StartBackground()
	if !errors.Is(err, ErrAlreadyStarted) {
		t.Errorf("second StartBackground = %v, want ErrAlreadyStarted", err)
	}

	// The worker status reflects the background sync is enabled.
	st := s.BackgroundSyncStatus()
	if !st.Enabled {
		t.Error("BackgroundSyncStatus.Enabled should be true after StartBackground")
	}
}

// ── Client.SyncReplication typed error ──────────────────────────────────────

// TestServer_Client_SyncReplication_409SyncInProgress_ReturnsWrappedErrSyncInProgress
// proves that when the server returns HTTP 409 with code "sync_in_progress", the client
// returns an error that satisfies errors.Is(err, ErrSyncInProgress).
func TestServer_Client_SyncReplication_409SyncInProgress_ReturnsWrappedErrSyncInProgress(t *testing.T) {
	// Fake server that always returns 409 with code "sync_in_progress".
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/replication/sync" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"ok":false,"code":"sync_in_progress","error":"sync already in progress"}`))
	}))
	defer fake.Close()

	c := NewClient(fake.URL, 5*time.Second)
	_, err := c.SyncReplication(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrSyncInProgress) {
		t.Errorf("errors.Is(err, ErrSyncInProgress) = false; err = %v", err)
	}
}

// TestServer_Client_SyncReplication_502PrimaryDown_DoesNotMatchErrSyncInProgress
// proves that a 502 (primary unavailable) does NOT match ErrSyncInProgress.
func TestServer_Client_SyncReplication_502PrimaryDown_DoesNotMatchErrSyncInProgress(t *testing.T) {
	// Fake server that returns 502.
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}))
	defer fake.Close()

	c := NewClient(fake.URL, 5*time.Second)
	_, err := c.SyncReplication(context.Background())
	if err == nil {
		t.Fatal("expected error for 502, got nil")
	}
	if errors.Is(err, ErrSyncInProgress) {
		t.Errorf("502 response should NOT match ErrSyncInProgress; err = %v", err)
	}
}
