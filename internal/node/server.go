package node

import (
	"context"
	"errors"
	"fmt"
	"math"
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
	opts    Options
	eng     *engine.Engine
	mux     *http.ServeMux
	srv     *http.Server
	ln      net.Listener
	mu      sync.Mutex
	started bool // protected by mu; true after first Start/StartBackground call
	closed  bool // protected by mu; true after Close
	start   time.Time

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

	// Phase 28: write gate and promotion state.
	// writeGate guards PUT/DELETE/ExplainPut/ExplainDelete handlers.
	// Writes take RLock; quiesce takes exclusive Lock (drains in-flight writes).
	// Non-nil for primary/standalone; nil for follower (already rejects writes).
	writeGate *writeGate

	// quiesceMu serializes the entire quiesce operation (check → gate → persist → state).
	// Prevents TOCTOU: two concurrent POST /replication/quiesce cannot each generate a
	// different QuiesceID or observe conflicting states.
	quiesceMu sync.Mutex

	// promoteMu serializes the entire promotion operation (check → barrier → drain → commit).
	// Prevents two concurrent POST /replication/promote from both reaching executePromotion.
	promoteMu sync.Mutex

	// promotionBarrier is set true by handlePromote before stopping the bgWorker.
	// SyncFromPrimary and handleReplicationApply check this flag before and after
	// acquiring replicationMutationMu.RLock so that no follower mutation proceeds
	// after the promotion sequence has begun.
	promotionBarrier atomic.Bool

	// replicationMutationMu provides crash-consistent exclusion between follower
	// mutations (SyncFromPrimary, handleReplicationApply — hold RLock) and the
	// promotion commit (handlePromote — holds WLock after stopping the bgWorker).
	// This replaces the previous 5-second drain-poll loop.
	replicationMutationMu sync.RWMutex

	// Quiesce state (primary only). Protected by mu.
	// States: "active", "quiesce_failed_fenced", "quiesced"
	quiesceState         string                 // protected by mu
	quiesceRecord        *replnet.QuiesceRecord // non-nil when quiesceState == "quiesced"
	pendingQuiesceRecord *replnet.QuiesceRecord // non-nil when quiesceState == "quiesce_failed_fenced"
	quiesceIntentActive  bool                   // protected by mu; true between SaveQuiesceIntent and RemoveQuiesceIntent

	// quiesceIDFn generates a random quiesce ID. Defaults to replnet.NewQuiesceID.
	// Replaceable in tests to inject failures or fixed IDs.
	quiesceIDFn func() (string, error)

	// removeQuiesceIntentFn removes the quiesce intent file. Defaults to replnet.RemoveQuiesceIntent.
	// Replaceable in tests to inject removal failures and verify cleanup_pending reporting.
	removeQuiesceIntentFn func(dir string) error

	// Promotion state (follower -> promoted primary). Protected by mu.
	promotionState  string // "", "promoting", "promoted"
	promotionRecord *replnet.PromotionRecord

	// Runtime role — may differ from opts.Replication.Role after promotion.
	// Protected by mu; use runtimeState() to read consistently.
	runtimeRole     string // "primary", "follower", "standalone", ""
	localRoleSource string // "config" or "promotion_record"
}

// runtimeSnapshot is a point-in-time copy of mutable Server fields read under mu.
type runtimeSnapshot struct {
	role                 string
	localRoleSource      string
	quiesceState         string
	quiesceRecord        *replnet.QuiesceRecord
	pendingQuiesceRecord *replnet.QuiesceRecord
	quiesceIntentActive  bool
	promotionState       string
	promotionRecord      *replnet.PromotionRecord
	closed               bool
}

// runtimeState returns a synchronized snapshot of mutable Server state.
// Call this instead of reading s.runtimeRole etc. directly from handlers.
func (s *Server) runtimeState() runtimeSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return runtimeSnapshot{
		role:                 s.runtimeRole,
		localRoleSource:      s.localRoleSource,
		quiesceState:         s.quiesceState,
		quiesceRecord:        s.quiesceRecord,
		pendingQuiesceRecord: s.pendingQuiesceRecord,
		quiesceIntentActive:  s.quiesceIntentActive,
		promotionState:       s.promotionState,
		promotionRecord:      s.promotionRecord,
		closed:               s.closed,
	}
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
		opts:                  opts,
		eng:                   eng,
		mux:                   http.NewServeMux(),
		start:                 time.Now(),
		quiesceIDFn:           replnet.NewQuiesceID,
		removeQuiesceIntentFn: replnet.RemoveQuiesceIntent,
	}

	// Phase 28: resolve runtime role from durable records before initializing
	// replication resources. This determines whether a follower has been promoted.
	if err := s.resolveRuntimeRole(); err != nil {
		eng.Close()
		return nil, fmt.Errorf("node: resolve runtime role: %w", err)
	}

	switch s.runtimeRole {
	case "primary":
		if s.durableLog == nil {
			// Open durable log (promoted primary already has one set by resolveRuntimeRole
			// only if the promotion record was found; config-based primary opens here).
			dl, err := replnet.OpenDurableLog(opts.DataDir)
			if err != nil {
				eng.Close()
				return nil, fmt.Errorf("node: open durable log: %w", err)
			}
			s.durableLog = dl
		}
		// intent-only startup: pendingQuiesceRecord was built with PrimaryLatestSeq=0.
		// Now that the durable log is open, capture the correct seq and re-stamp checksum.
		if s.quiesceState == "quiesce_failed_fenced" && s.pendingQuiesceRecord != nil && s.pendingQuiesceRecord.PrimaryLatestSeq == 0 {
			s.pendingQuiesceRecord.PrimaryLatestSeq = s.durableLog.LatestSeq()
			s.pendingQuiesceRecord.Checksum = replnet.QuiesceChecksum(s.pendingQuiesceRecord)
		}

	case "follower":
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
// Returns ErrClosed if the server has already been closed.
// Returns ErrAlreadyStarted if Start or StartBackground has already been called.
// If background sync is configured on a follower, the worker is started atomically
// with the listener bind (under the server lock) to prevent Close from racing.
func (s *Server) Start() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrClosed
	}
	if s.started {
		s.mu.Unlock()
		return ErrAlreadyStarted
	}
	ln, err := net.Listen("tcp", s.opts.Addr)
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("node: listen %s: %w", s.opts.Addr, err)
	}
	s.ln = ln
	srv := &http.Server{Handler: s.mux}
	s.srv = srv
	s.started = true
	// Start the background sync worker while holding the lock so Close cannot
	// observe a not-yet-started worker and stop a nil-cancel, allowing a subsequent
	// start() call to launch against already-closed resources.
	if s.bgWorker != nil {
		if err := s.bgWorker.start(); err != nil {
			// Worker was already started externally (should not happen in correct use).
			s.mu.Unlock()
			return fmt.Errorf("node: start background worker: %w", err)
		}
	}
	s.mu.Unlock()

	// Serve blocks; ErrServerClosed is normal on shutdown.
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// StartBackground binds and starts the server in a goroutine.
// It waits until the listener is bound before returning so callers can
// immediately use Addr(). Returns any bind error synchronously.
// Returns ErrClosed if the server has already been closed.
// Returns ErrAlreadyStarted if Start or StartBackground has already been called.
// If background sync is configured on a follower, the worker is started atomically
// with the listener bind (under the server lock) to prevent Close from racing.
func (s *Server) StartBackground() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrClosed
	}
	if s.started {
		s.mu.Unlock()
		return ErrAlreadyStarted
	}
	ln, err := net.Listen("tcp", s.opts.Addr)
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("node: listen %s: %w", s.opts.Addr, err)
	}
	s.ln = ln
	srv := &http.Server{Handler: s.mux}
	s.srv = srv
	s.started = true
	// Launch the HTTP server goroutine while still holding the lock so srv is
	// visible to Close, which reads it after acquiring the lock.
	go func() {
		_ = srv.Serve(ln)
	}()
	// Start the background sync worker while holding the lock (see Start() for rationale).
	if s.bgWorker != nil {
		if err := s.bgWorker.start(); err != nil {
			s.mu.Unlock()
			return fmt.Errorf("node: start background worker: %w", err)
		}
	}
	s.mu.Unlock()
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

// resolveRuntimeRole determines the effective role of this node at startup.
// Checks for durable promotion and quiesce records that override static config.
//
// Precedence:
//  1. Promotion record found -> runtimeRole = "primary" (was promoted from follower)
//  2. Quiesce record found (config=primary) -> runtimeRole = "primary", write-fenced
//  3. No durable records -> runtimeRole = config role
func (s *Server) resolveRuntimeRole() error {
	dir := s.opts.DataDir

	// Check for promotion record (follower -> promoted primary).
	if rec, err := replnet.LoadPromotionRecord(dir); err == nil {
		if rec.NodeID != s.opts.NodeID {
			return fmt.Errorf("promotion record node_id %q does not match config node_id %q",
				rec.NodeID, s.opts.NodeID)
		}

		// Cross-validate required fields in the promotion record.
		if rec.NewRole != "primary" {
			return fmt.Errorf("promotion record new_role is %q, want \"primary\"", rec.NewRole)
		}
		if rec.SourcePrimaryNodeID == "" {
			return fmt.Errorf("promotion record source_primary_node_id is empty")
		}
		if rec.SourcePrimaryBaseURL == "" {
			return fmt.Errorf("promotion record source_primary_base_url is empty")
		}
		if rec.QuiesceID == "" {
			return fmt.Errorf("promotion record quiesce_id is empty")
		}
		if rec.InheritedLastSeq == math.MaxUint64 {
			return fmt.Errorf("promotion record inherited_last_seq is MaxUint64 (would overflow)")
		}

		// Verify journal baseline is present and consistent.
		baseline, err := replnet.LoadJournalBaseline(dir)
		if err != nil {
			return fmt.Errorf("load journal baseline for promoted primary: %w", err)
		}
		if baseline == nil {
			return fmt.Errorf("promoted primary has promotion record but no journal baseline (corrupt state)")
		}
		if baseline.BaseSeq != rec.InheritedLastSeq {
			return fmt.Errorf("journal baseline base_seq %d != promotion record inherited_last_seq %d",
				baseline.BaseSeq, rec.InheritedLastSeq)
		}

		// Cross-validate promotion record against persisted follower replication state.
		// NewReplicationStateStore returns (ss, nil) for a missing file (fresh follower, cursor=0).
		// Any other error (identity mismatch, corrupt state, permission error) fails startup —
		// the operator must diagnose before the promoted primary can serve requests.
		// Note: promotion with inherited_last_seq==0 from a fresh follower (missing state file,
		// cursor=0) is valid — the cursor check below only triggers when InheritedLastSeq > 0.
		ss, stateErr := replnet.NewReplicationStateStore(dir, rec.NodeID, rec.SourcePrimaryBaseURL)
		if stateErr != nil {
			return fmt.Errorf("validate replication state at promoted-primary startup: %w", stateErr)
		}
		cursor := ss.LastAppliedSeq()
		if rec.InheritedLastSeq > 0 && cursor != rec.InheritedLastSeq {
			return fmt.Errorf("replication state cursor %d does not match promotion record inherited_last_seq %d (state files from different nodes?)",
				cursor, rec.InheritedLastSeq)
		}

		s.runtimeRole = "primary"
		s.localRoleSource = "promotion_record"
		s.promotionRecord = rec
		s.promotionState = "promoted"
		s.writeGate = &writeGate{}
		s.quiesceState = "active"

		// Open durable log with baseline for promoted primary.
		dl, err := replnet.OpenDurableLog(dir)
		if err != nil {
			return fmt.Errorf("open durable log for promoted primary: %w", err)
		}
		s.durableLog = dl
		return nil
	} else if !errors.Is(err, replnet.ErrPromotionRecordNotFound) {
		return fmt.Errorf("load promotion record: %w", err)
	}
	// Orphan baseline (journal_baseline.json without promotion_record.json) is safe to
	// ignore: the promotion was never committed, and this node starts as a regular follower.

	// No promotion record. Use config role.
	s.runtimeRole = string(s.opts.Replication.Role)
	if s.runtimeRole == "" {
		s.runtimeRole = "standalone"
	}
	s.localRoleSource = "config"

	if s.runtimeRole == "primary" || s.runtimeRole == "standalone" {
		s.writeGate = &writeGate{}
		s.quiesceState = "active"
	}

	// Check if node was quiesced (primary only).
	// Startup ordering: check intent first, then final quiesce record.
	//
	//  intent-only                  → quiesce_failed_fenced (restore fence; operator retries)
	//  final-only                   → quiesced (normal case)
	//  intent + final, matching IDs → quiesced (crash between step 5 and step 6; remove intent)
	//  intent + final, mismatched   → startup error (corrupt state)
	//  corrupt intent               → startup error
	if s.runtimeRole == "primary" {
		// Load intent (if any).
		intent, intentErr := replnet.LoadQuiesceIntent(dir)
		if intentErr != nil && !errors.Is(intentErr, replnet.ErrQuiesceIntentNotFound) {
			return fmt.Errorf("load quiesce intent: %w", intentErr)
		}
		intentExists := intent != nil

		// Load final record (if any).
		finalRec, finalErr := replnet.LoadQuiesceRecord(dir)
		if finalErr != nil && !errors.Is(finalErr, replnet.ErrQuiesceRecordNotFound) {
			return fmt.Errorf("load quiesce record: %w", finalErr)
		}
		finalExists := finalRec != nil

		switch {
		case !intentExists && !finalExists:
			// Normal active primary — no quiesce state.

		case !intentExists && finalExists:
			// Final-only: normal quiesced state. Restore fence.
			s.quiesceRecord = finalRec
			s.quiesceState = "quiesced"
			s.writeGate.Quiesce()

		case intentExists && !finalExists:
			// Intent-only: crash after intent write but before final commit.
			// Restore fence unconditionally; build skeleton pending record (seq
			// will be updated after durable log opens, in Open()).
			s.writeGate.Quiesce()
			pending := &replnet.QuiesceRecord{
				Version:          1,
				QuiesceID:        intent.QuiesceID,
				PrimaryNodeID:    intent.PrimaryNodeID,
				PrimaryBaseURL:   intent.PrimaryBaseURL,
				PrimaryLatestSeq: 0, // updated after durable log opens
				QuiescedAt:       intent.IntentAt,
			}
			pending.Checksum = replnet.QuiesceChecksum(pending)
			s.quiesceState = "quiesce_failed_fenced"
			s.pendingQuiesceRecord = pending

		case intentExists && finalExists:
			if intent.QuiesceID != finalRec.QuiesceID {
				return fmt.Errorf("quiesce intent ID %q does not match final record ID %q (corrupt state)",
					intent.QuiesceID, finalRec.QuiesceID)
			}
			// Matching intent + final: crash after step 5 but before intent cleanup.
			// Remove the stale intent and restore quiesced state.
			if err := replnet.RemoveQuiesceIntent(dir); err != nil {
				return fmt.Errorf("remove stale quiesce intent: %w", err)
			}
			s.quiesceRecord = finalRec
			s.quiesceState = "quiesced"
			s.writeGate.Quiesce()
		}
	}

	return nil
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
	// Use runtime role for accurate status after promotion. Read under mu via
	// runtimeState() to eliminate the data race on s.runtimeRole.
	role := s.runtimeState().role
	switch role {
	case "primary":
		var lastSeq uint64
		var firstSeq uint64
		if s.durableLog != nil {
			if st, err := s.durableLog.Stats(); err == nil {
				lastSeq = st.LastSeq
				firstSeq = st.FirstAvailableSeq
			}
		}
		_ = firstSeq
		return replnet.ReplicaStatus{
			Role:         replnet.RolePrimary,
			LastLocalSeq: lastSeq,
			Durable:      true,
		}
	case "follower":
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

// ApplyReplicationEntries checks the promotion barrier, acquires replicationMutationMu.RLock,
// verifies the node is still a follower, and calls applyReplicationEntriesLocked.
// Use this from callers that do NOT already hold the RLock (e.g. handleReplicationApply,
// external callers).
//
// Returns ErrPromotionInProgress if the promotion barrier is set.
// Returns ErrNotFollower if the node is a primary, standalone, or promoted primary.
// Primary and standalone nodes must not mutate engine state via this path.
func (s *Server) ApplyReplicationEntries(entries []replnet.Entry) (uint64, error) {
	if s.promotionBarrier.Load() {
		return atomic.LoadUint64(&s.lastApplied), fmt.Errorf("node: apply rejected: %w", ErrPromotionInProgress)
	}
	s.replicationMutationMu.RLock()
	defer s.replicationMutationMu.RUnlock()
	if s.promotionBarrier.Load() {
		return atomic.LoadUint64(&s.lastApplied), fmt.Errorf("node: apply rejected: %w", ErrPromotionInProgress)
	}
	// Re-read the role under s.mu after acquiring the RLock. Lock ordering is
	// replicationMutationMu (outer) → s.mu (inner), which is consistent with executePromotion.
	// This ensures primary, standalone, and promoted-primary callers are rejected and do not
	// mutate engine state or advance lastApplied.
	s.mu.Lock()
	role := s.runtimeRole
	s.mu.Unlock()
	if role != "follower" {
		return atomic.LoadUint64(&s.lastApplied), fmt.Errorf("node: apply rejected (role=%q): %w", role, ErrNotFollower)
	}
	return s.applyReplicationEntriesLocked(entries)
}

// applyReplicationEntriesLocked applies a batch of replication entries to the local engine.
// Caller must hold replicationMutationMu.RLock. Use ApplyReplicationEntries if no lock is held.
//
// Entries must be in ascending Seq order. Already-applied entries (Seq <= lastApplied) are
// safely skipped. Out-of-order gaps return replnet.ErrInvalidEntry.
// Returns the last applied sequence number after this batch.
//
// After successful application the follower's cursor is persisted via stateStore (if present).
func (s *Server) applyReplicationEntriesLocked(entries []replnet.Entry) (uint64, error) {
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
	// First barrier check (fast path; no mutex required).
	// Reject syncs during promotion: the barrier is set before the bgWorker is stopped so
	// no new sync can start after the promotion sequence begins.
	if s.promotionBarrier.Load() {
		return SyncResult{}, fmt.Errorf("node: sync rejected: promotion in progress: %w", ErrPromotionInProgress)
	}

	// Capture runtime state under s.mu to eliminate data races on s.runtimeRole,
	// s.replicator, s.closed, and s.bgWorker. These fields are written by executePromotion
	// (under s.mu + replicationMutationMu.WLock) and Close (under s.mu). Reading them
	// without the mutex would be a data race flagged by -race.
	s.mu.Lock()
	closed := s.closed
	role := s.runtimeRole
	replicator := s.replicator
	bgEnabled := s.bgWorker != nil
	s.mu.Unlock()

	if closed {
		return SyncResult{}, ErrClosed
	}
	if role != "follower" {
		return SyncResult{}, fmt.Errorf("node: SyncFromPrimary: not a follower (role: %q): %w", role, ErrNotFollower)
	}
	if replicator == nil {
		return SyncResult{}, fmt.Errorf("node: SyncFromPrimary: replicator is nil (node is not configured as follower)")
	}

	// Reject concurrent syncs. Two concurrent calls could pull the same batch and attempt
	// to apply identical entries, causing sequence-order violations or double-persistence.
	// Choice: reject — the caller should retry after the in-flight sync completes.
	if !s.syncInProgress.CompareAndSwap(false, true) {
		return SyncResult{}, ErrSyncInProgress
	}
	defer s.syncInProgress.Store(false)

	// Post-CAS double-check: if barrier was set between the first load and the CAS, abort.
	if s.promotionBarrier.Load() {
		return SyncResult{}, fmt.Errorf("node: sync rejected: promotion in progress: %w", ErrPromotionInProgress)
	}

	// Acquire shared lock before doing any follower-state mutation. handlePromote holds
	// the exclusive WLock after raising the barrier and stopping bgWorker. A sync that
	// passed both barrier checks above but is waiting here will fail the third barrier
	// check below (inside the RLock) if promotion grabbed WLock in the interim.
	s.replicationMutationMu.RLock()
	defer s.replicationMutationMu.RUnlock()

	// Third barrier check: catches syncs that passed both pre-RLock checks but had to
	// wait while promotion held WLock. The barrier remains true after promotion completes,
	// so this check is definitive.
	if s.promotionBarrier.Load() {
		return SyncResult{}, fmt.Errorf("node: sync rejected: promotion in progress: %w", ErrPromotionInProgress)
	}

	// Use the captured replicator pointer (safe: the Replicator has no Close method and
	// is not destroyed on promotion — the pointer stored in s.replicator is cleared under
	// replicationMutationMu.WLock, which we now hold as RLock, so no mutation can race here).
	before := atomic.LoadUint64(&s.lastApplied)
	pr, err := replicator.PullEntries(ctx, before, 0)
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
	lastSeq, err := s.applyReplicationEntriesLocked(pr.Entries)
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

	// Compute operation-count lag. Lag is only known when the primary reported
	// primary_latest_seq (PrimaryLatestSeqKnown). A uint64 field value of 0 cannot
	// distinguish "primary is empty" from "older primary that omits the field"; the
	// PrimaryLatestSeqKnown flag makes this unambiguous.
	lagKnown := pr.PrimaryLatestSeqKnown
	var lagEntries int64
	if lagKnown && pr.PrimaryLatestSeq >= lastSeq {
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
//
// s.bgWorker is read under s.mu to eliminate a data race with
// restartBackgroundWorkerAfterPromotionFailure, which writes s.bgWorker under s.mu.
// worker.Status() is called outside the server mutex (worker has its own mu; the
// established lock ordering is s.mu → worker.mu, not the reverse).
func (s *Server) BackgroundSyncStatus() BackgroundSyncStatus {
	s.mu.Lock()
	worker := s.bgWorker
	s.mu.Unlock()
	if worker == nil {
		return BackgroundSyncStatus{
			Enabled: false,
			State:   WorkerStateDisabled,
		}
	}
	return worker.Status()
}

// restartBackgroundWorkerAfterPromotionFailure safely restarts the background sync worker
// after a pre-commit promotion failure. It verifies the server is still a follower and
// coordinates with Close to avoid launching against already-closed resources.
//
// Idempotency: the function inspects the current bgWorker state before creating a replacement.
// If a worker is already starting, running, backing off, or blocked, no second worker is created.
// Only a nil, stopped, or disabled worker is replaced.
//
// The caller must have already cleared promotionBarrier (Store(false)) before calling this.
// If Close wins the race, the worker is not started. If the worker starts first, Close will
// observe it under s.mu and stop it cleanly.
//
// Lock ordering: holds s.mu to inspect and write s.bgWorker; calls worker.Status() while
// holding s.mu (safe: worker.mu is a leaf lock — nothing in the worker acquires s.mu, so
// s.mu → worker.mu is a valid ordering).
func (s *Server) restartBackgroundWorkerAfterPromotionFailure() {
	if !s.opts.Replication.BackgroundSync.Enabled {
		return
	}
	worker := newBackgroundSyncWorker(s.opts.Replication.BackgroundSync, s.SyncFromPrimary)
	s.mu.Lock()
	defer s.mu.Unlock()
	// Abort if the server is closing, the role is no longer follower, or the barrier
	// is still set (concurrent promote attempt — should not happen with promoteMu held).
	if s.closed || s.runtimeRole != "follower" || s.promotionBarrier.Load() {
		return
	}
	// Inspect the existing worker. Only replace a worker that is nil, stopped, or in the
	// initial disabled state. Do not create a second worker if one is already active
	// (starting, running, backing off, blocked, or stopping).
	if s.bgWorker != nil {
		state := s.bgWorker.Status().State
		switch state {
		case WorkerStateStopped, WorkerStateDisabled:
			// Stale or never-started: safe to replace.
		default:
			// Active worker present: idempotent no-op.
			return
		}
	}
	if err := worker.start(); err != nil {
		// start() returned ErrAlreadyStarted on a freshly constructed worker — invariant
		// violation. Do not install a broken worker pointer.
		return
	}
	s.bgWorker = worker
}
