package node

import (
	"context"
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
//   - Replication is explicit pull-based only (no background sync loop).
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

	// Replication state. replLog is non-nil only for primary nodes.
	// replicator is non-nil only for follower nodes.
	// lastApplied is the last replication log seq applied (follower only).
	replLog     *replnet.Log
	replicator  *replnet.Replicator
	lastApplied uint64 // accessed via atomic
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
		s.replLog = replnet.NewLog()
	case replnet.RoleFollower:
		s.replicator = replnet.NewReplicator(opts.Replication.PrimaryBaseURL, 0)
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

	// Serve blocks; ErrServerClosed is normal on shutdown.
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// StartBackground binds and starts the server in a goroutine.
// It waits until the listener is bound before returning so callers can
// immediately use Addr(). Returns any bind error synchronously.
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
	return nil
}

// Close shuts down the HTTP server and closes the Engine.
// It is safe to call Close multiple times.
func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true

	var firstErr error
	if s.srv != nil {
		if err := s.srv.Shutdown(context.Background()); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if s.replLog != nil {
		if err := s.replLog.Close(); err != nil && firstErr == nil {
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
		if s.replLog != nil {
			if st, err := s.replLog.Stats(); err == nil {
				lastSeq = st.LastSeq
			}
		}
		return replnet.ReplicaStatus{
			Role:         replnet.RolePrimary,
			LastLocalSeq: lastSeq,
		}
	case replnet.RoleFollower:
		return replnet.ReplicaStatus{
			Role:           replnet.RoleFollower,
			PrimaryBaseURL: s.opts.Replication.PrimaryBaseURL,
			LastAppliedSeq: atomic.LoadUint64(&s.lastApplied),
		}
	default:
		return replnet.ReplicaStatus{}
	}
}

// ReplicationEntries returns up to limit log entries with Seq > after.
// Only valid for primary nodes; returns nil for standalone and follower nodes.
func (s *Server) ReplicationEntries(after uint64, limit int) ([]replnet.Entry, error) {
	if s.replLog == nil {
		return nil, nil
	}
	return s.replLog.EntriesAfter(after, limit)
}

// ApplyReplicationEntries applies a batch of entries from the primary to the local engine.
// Entries must be in ascending Seq order. Already-applied entries (Seq <= lastApplied) are
// safely skipped. Out-of-order gaps return replnet.ErrInvalidEntry.
// Returns the last applied sequence number after this batch.
func (s *Server) ApplyReplicationEntries(entries []replnet.Entry) (uint64, error) {
	last := atomic.LoadUint64(&s.lastApplied)
	for _, e := range entries {
		if e.Seq <= last {
			continue // already applied
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
	return last, nil
}

// SyncFromPrimary pulls new entries from the primary and applies them locally.
// Only valid for follower nodes; returns an error for primary and standalone nodes.
func (s *Server) SyncFromPrimary(ctx context.Context) (replnet.ReplicaStatus, error) {
	if s.replicator == nil {
		return replnet.ReplicaStatus{}, fmt.Errorf("node: SyncFromPrimary called on non-follower node")
	}
	after := atomic.LoadUint64(&s.lastApplied)
	entries, err := s.replicator.PullEntries(ctx, after, 0)
	if err != nil {
		return s.ReplicationStatus(), fmt.Errorf("node: pull from primary: %w", err)
	}
	if _, err := s.ApplyReplicationEntries(entries); err != nil {
		return s.ReplicationStatus(), err
	}
	return s.ReplicationStatus(), nil
}
