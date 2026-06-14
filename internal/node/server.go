package node

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/YashPatel2395/ShardForgeDB/internal/engine"
	"github.com/YashPatel2395/ShardForgeDB/internal/replnet"
)

// Server is a networked database node. It owns a local Engine and serves an HTTP/JSON API.
//
// Scope limitations:
//   - No Raft, no consensus, no quorum replication, no automatic leader election.
//   - No distributed sharding or shard migration.
//   - Replication is pull-based only (explicit via POST /replication/sync, or automatic
//     background pull when BackgroundSync is enabled on a follower node).
//   - Each Server is an independent single-process node.
type Server struct {
	opts   Options
	eng    *engine.Engine
	mux    *http.ServeMux
	srv    *http.Server
	ln     net.Listener
	mu     sync.Mutex
	closed bool
	start  time.Time

	// Replication state.
	// durableLog is non-nil only for primary nodes (Phase 26+: persisted binary journal).
	// stateStore is non-nil only for follower nodes (Phase 26+: persisted cursor).
	// replicator is non-nil only for follower nodes.
	// lastApplied is the last replication log seq applied (follower only; authoritative is stateStore).
	// syncInProgress prevents concurrent SyncFromPrimary calls from applying the same batch twice.
	durableLog     *replnet.DurableLog
	stateStore     *replnet.ReplicationStateStore
	replicator     *replnet.Replicator
	lastApplied    uint64      // accessed via atomic; initialised from stateStore on Open
	syncInProgress atomic.Bool // true while a SyncFromPrimary call is in flight

	// Background sync (Phase 27, follower only).
	// bgWorker is non-nil when BackgroundSync.Enabled is true.
	bgWorker *backgroundSyncWorker
}

// Open validates opts, opens (or creates) the local Engine, and wires up the HTTP mux.
// It does NOT start the listener; call Start() to begin serving requests.
func Open(opts Options) (*Server, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}

	memMax := opts.MemTableMaxBytes
	if memMax == 0 {
		memMax = 64 << 20 // 64 MiB default
	}

	eng, err := engine.Open(engine.Options{
		Dir:              opts.DataDir,
		WALSyncOnWrite:   opts.WALSyncOnWrite,
		MemTableMaxBytes: memMax,
	})
	if err != nil {
		return nil, fmt.Errorf("node: engine open failed: %w", err)
	}

	s := &Server{
		opts:  opts,
		eng:   eng,
		mux:   http.NewServeMux(),
		start: time.Now(),
	}

	switch opts.Replication.Role {
	case replnet.RolePrimary:
		dl, err := replnet.OpenDurableLog(opts.DataDir)
		if err != nil {
			eng.Close()
			return nil, fmt.Errorf("node: open durable log: %w", err)
		}
		s.durableLog = dl

	case replnet.RoleFollower:
		ss, err := replnet.NewReplicationStateStore(opts.DataDir, opts.NodeID, opts.Replication.PrimaryBaseURL)
		if err != nil {
			eng.Close()
			return nil, fmt.Errorf("node: open replication state store: %w", err)
		}
		s.stateStore = ss
		// Restore cursor from persistent state.
		atomic.StoreUint64(&s.lastApplied, ss.LastAppliedSeq())

		// Use a dedicated timeout for background-sync requests if configured,
		// otherwise use the replicator default.
		replTimeout := time.Duration(0)
		if opts.Replication.BackgroundSync.Enabled && opts.Replication.BackgroundSync.RequestTimeout.Duration > 0 {
			replTimeout = opts.Replication.BackgroundSync.RequestTimeout.Duration
		}
		s.replicator = replnet.NewReplicator(opts.Replication.PrimaryBaseURL, replTimeout)

		// Phase 27: optional background sync worker.
		if opts.Replication.BackgroundSync.Enabled {
			s.bgWorker = newBackgroundSyncWorker(opts.Replication.BackgroundSync, s.SyncFromPrimary)
		}
	}

	s.registerRoutes()
	return s, nil
}

// Handler returns the http.Handler for this node. Useful for httptest.
func (s *Server) Handler() http.Handler {
	return s.mux
}

// Addr returns the resolved listen address (e.g. "127.0.0.1:9101").
// Returns the configured Addr before Start() is called.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln != nil {
		return s.ln.Addr().String()
	}
	return s.opts.Addr
}

// Start binds the configured Addr and begins serving HTTP requests.
// It blocks until the server is stopped via Close or an error occurs.
// If background sync is configured on a follower, the worker is started after binding.
func (s *Server) Start() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrClosed
	}
	ln, err := net.Listen("tcp", s.opts.Addr)
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("node: listen %s: %w", s.opts.Addr, err)
	}
	s.ln = ln
	srv := &http.Server{Handler: s.mux}
	s.srv = srv
	s.mu.Unlock()

	// Start the background sync worker after the listener is bound.
	if s.bgWorker != nil {
		s.bgWorker.start()
	}

	// Serve blocks; ErrServerClosed is normal on shutdown.
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// StartBackground binds and starts the server in a goroutine.
// It waits until the listener is bound before returning so callers can
// immediately use Addr(). Returns any bind error synchronously.
// If background sync is configured on a follower, the worker is started after binding.
func (s *Server) StartBackground() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrClosed
	}
	ln, err := net.Listen("tcp", s.opts.Addr)
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("node: listen %s: %w", s.opts.Addr, err)
	}
	s.ln = ln
	srv := &http.Server{Handler: s.mux}
	s.srv = srv
	s.mu.Unlock()

	go func() {
		_ = srv.Serve(ln)
	}()

	// Start the background sync worker after the listener is bound.
	if s.bgWorker != nil {
		s.bgWorker.start()
	}

	return nil
}

// Close shuts down the HTTP server and closes the Engine.
// It is safe to call Close multiple times.
// The background sync worker (if any) is stopped before other resources are released.
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	bgWorker := s.bgWorker
	s.mu.Unlock()

	// Stop the background sync worker first so it does not access resources
	// after they are closed. stop() cancels the context and waits for the goroutine.
	if bgWorker != nil {
		bgWorker.stop()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var firstErr error
	if s.srv != nil {
		if err := s.srv.Shutdown(context.Background()); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if s.durableLog != nil {
		if err := s.durableLog.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if s.eng != nil {
		if err := s.eng.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Status returns a point-in-time snapshot of this node's state.
func (s *Server) Status() Status {
	stats := s.eng.Stats()
	return Status{
		NodeID:    s.opts.NodeID,
		Addr:      s.Addr(),
		DataDir:   s.opts.DataDir,
		StartedAt: s.start,
		Engine: EngineStatus{
			MemTableEntries:     stats.MemTableEntries,
			MemTableApproxBytes: stats.MemTableApproxBytes,
			SSTableCount:        stats.SSTableCount,
			NextSeq:             stats.NextSeq,
			FlushCount:          stats.FlushCount,
			CompactionCount:     stats.CompactionCount,
		},
		Replication: s.ReplicationStatus(),
	}
}

// ReplicationStatus returns the current replication state of this node.
func (s *Server) ReplicationStatus() replnet.ReplicaStatus {
	role := s.opts.Replication.Role
	switch role {
	case replnet.RolePrimary:
		var lastSeq uint64
		var firstSeq uint64
		if s.durableLog != nil {
			if st, err := s.durableLog.Stats(); err == nil {
				lastSeq = st.LastSeq
				firstSeq = st.FirstAvailableSeq
			}
		}
		_ = firstSeq // included in DurableLogStats; not in ReplicaStatus directly
		return replnet.ReplicaStatus{
			Role:         replnet.RolePrimary,
			LastLocalSeq: lastSeq,
			Durable:      true,
		}
	case replnet.RoleFollower:
		return replnet.ReplicaStatus{
			Role:            replnet.RoleFollower,
			PrimaryBaseURL:  s.opts.Replication.PrimaryBaseURL,
			LastAppliedSeq:  atomic.LoadUint64(&s.lastApplied),
			Durable:         true,
			StatePersistent: true,
		}
	default:
		return replnet.ReplicaStatus{}
	}
}

// ReplicationEntries returns up to limit log entries with Seq > after from the durable log.
// Only valid for primary nodes; returns nil, nil for standalone and follower nodes.
// Returns *replnet.ReplicationGapError if after is behind the first available journal entry.
func (s *Server) ReplicationEntries(after uint64, limit int) ([]replnet.Entry, error) {
	if s.durableLog == nil {
		return nil, nil
	}

	// Gap detection: if the follower's cursor is behind the journal's first entry,
	// the follower cannot catch up without a reseed.
	firstSeq := s.durableLog.FirstAvailableSeq()
	if firstSeq > 0 && after+1 < firstSeq {
		st, _ := s.durableLog.Stats()
		return nil, &replnet.ReplicationGapError{
			RequestedAfter:    after,
			FirstAvailableSeq: firstSeq,
			LatestSeq:         st.LastSeq,
		}
	}

	return s.durableLog.EntriesAfter(after, limit)
}

// ApplyReplicationEntries applies a batch of entries from the primary to the local engine.
// Entries must be in ascending Seq order. Already-applied entries (Seq <= lastApplied) are
// safely skipped. Out-of-order gaps return replnet.ErrInvalidEntry.
// Returns the last applied sequence number after this batch.
//
// After successful application the follower's cursor is persisted via stateStore (if present).
func (s *Server) ApplyReplicationEntries(entries []replnet.Entry) (uint64, error) {
	last := atomic.LoadUint64(&s.lastApplied)
	for _, e := range entries {
		if e.Seq <= last {
			continue // already applied; idempotent
		}
		if e.Seq != last+1 {
			return last, fmt.Errorf("%w: expected seq %d, got %d", replnet.ErrInvalidEntry, last+1, e.Seq)
		}
		switch e.Op {
		case replnet.OpPut:
			if err := s.eng.Put([]byte(e.Key), []byte(e.Value)); err != nil {
				return last, fmt.Errorf("node: apply put seq %d: %w", e.Seq, err)
			}
		case replnet.OpDelete:
			if err := s.eng.Delete([]byte(e.Key)); err != nil {
				return last, fmt.Errorf("node: apply delete seq %d: %w", e.Seq, err)
			}
		default:
			return last, fmt.Errorf("%w: unknown op %q at seq %d", replnet.ErrInvalidEntry, e.Op, e.Seq)
		}
		last = e.Seq
		atomic.StoreUint64(&s.lastApplied, last)
	}

	// Persist cursor after batch apply (not per-entry, for performance).
	if s.stateStore != nil && last > 0 {
		if err := s.stateStore.AdvanceTo(last); err != nil {
			// Persistence failure is non-fatal in-process but must be reported so
			// the operator knows the cursor will not survive a restart.
			return last, fmt.Errorf("node: persist replication cursor seq %d: %w", last, err)
		}
	}

	return last, nil
}

// SyncFromPrimary pulls new entries from the primary and applies them locally.
// Only valid for follower nodes; returns an error for primary and standalone nodes.
//
// The returned SyncResult reports how many entries were fetched and how many were
// newly applied. Re-running SyncFromPrimary is idempotent: already-applied entries
// are skipped and Applied will be 0 when there is nothing new.
//
// If the primary returns a replication gap (HTTP 409), SyncFromPrimary propagates
// a *replnet.ReplicationGapError which callers can check with errors.As.
//
// This method is the single authoritative synchronization path used by both
// POST /replication/sync (manual) and the background sync worker (automatic).
func (s *Server) SyncFromPrimary(ctx context.Context) (SyncResult, error) {
	if s.replicator == nil {
		return SyncResult{}, fmt.Errorf("node: SyncFromPrimary called on non-follower node")
	}

	// Reject concurrent syncs. Two concurrent calls could pull the same batch and attempt
	// to apply identical entries, causing sequence-order violations or double-persistence.
	// Choice: reject (B) — the caller should retry after the in-flight sync completes.
	if !s.syncInProgress.CompareAndSwap(false, true) {
		return SyncResult{}, ErrSyncInProgress
	}
	defer s.syncInProgress.Store(false)

	bgEnabled := s.bgWorker != nil

	before := atomic.LoadUint64(&s.lastApplied)
	pr, err := s.replicator.PullEntries(ctx, before, 0)
	if err != nil {
		var gapErr *replnet.ReplicationGapError
		if errors.As(err, &gapErr) {
			return SyncResult{
				SourceNode:            s.opts.Replication.PrimaryBaseURL,
				FollowerNode:          s.opts.NodeID,
				LastAppliedSeq:        before,
				BackgroundSyncEnabled: bgEnabled,
				Replication:           s.ReplicationStatus(),
				Gap:                   gapErr,
			}, err
		}
		return SyncResult{
			SourceNode:            s.opts.Replication.PrimaryBaseURL,
			FollowerNode:          s.opts.NodeID,
			LastAppliedSeq:        before,
			BackgroundSyncEnabled: bgEnabled,
			Replication:           s.ReplicationStatus(),
		}, fmt.Errorf("node: pull from primary: %w", err)
	}

	fetched := len(pr.Entries)
	lastSeq, err := s.ApplyReplicationEntries(pr.Entries)
	if err != nil {
		return SyncResult{
			SourceNode:            s.opts.Replication.PrimaryBaseURL,
			FollowerNode:          s.opts.NodeID,
			Fetched:               fetched,
			LastAppliedSeq:        lastSeq,
			PrimaryLatestSeq:      pr.PrimaryLatestSeq,
			BackgroundSyncEnabled: bgEnabled,
			Replication:           s.ReplicationStatus(),
		}, err
	}

	// Count entries actually applied (seq > before), not just fetched.
	applied := 0
	for _, e := range pr.Entries {
		if e.Seq > before {
			applied++
		}
	}

	// Compute operation-count lag. After a successful pull we always know the
	// primary's latest seq (even if 0 means primary is empty). Clamp defensively.
	lagKnown := true
	var lagEntries int64
	if pr.PrimaryLatestSeq >= lastSeq {
		lagEntries = int64(pr.PrimaryLatestSeq - lastSeq)
	}

	return SyncResult{
		SourceNode:            s.opts.Replication.PrimaryBaseURL,
		FollowerNode:          s.opts.NodeID,
		Fetched:               fetched,
		Applied:               applied,
		LastAppliedSeq:        lastSeq,
		PrimaryLatestSeq:      pr.PrimaryLatestSeq,
		LagEntriesAfterSync:   lagEntries,
		LagKnown:              lagKnown,
		BackgroundSyncEnabled: bgEnabled,
		Replication:           s.ReplicationStatus(),
	}, nil
}

// BackgroundSyncStatus returns the current background sync worker status.
// Returns a disabled-state snapshot when background sync is not configured.
func (s *Server) BackgroundSyncStatus() BackgroundSyncStatus {
	if s.bgWorker == nil {
		return BackgroundSyncStatus{
			Enabled: false,
			State:   WorkerStateDisabled,
		}
	}
	return s.bgWorker.Status()
}
