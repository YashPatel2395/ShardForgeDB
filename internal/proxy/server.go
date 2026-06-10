package proxy

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/YashPatel2395/ShardForgeDB/internal/gateway"
)

const defaultTimeout = 5 * time.Second
const defaultVirtualNodes = 128
const defaultAddr = "127.0.0.1:9200"

// Server is a stateless HTTP routing proxy for ShardForgeDB.
//
// It exposes one HTTP/JSON API and routes key-value requests to independent
// shardforge-node processes via internal/gateway.
//
// Scope limitations:
//   - Stateless client-side routing only. The proxy stores no data.
//   - No Raft. No consensus. No quorum replication. No automatic leader election.
//   - No distributed sharding. No networked replication. No automatic failover.
//   - No retry to another node. If the routed node is unavailable, the operation fails.
type Server struct {
	opts    Options
	gw      *gateway.Gateway
	mux     *http.ServeMux
	srv     *http.Server
	ln      net.Listener
	mu      sync.Mutex
	closed  bool
	startAt time.Time
}

// Open validates opts, opens the gateway, and wires up the HTTP mux.
// It does NOT start the listener; call Start or StartBackground.
func Open(opts Options) (*Server, error) {
	if len(opts.Nodes) == 0 {
		return nil, fmt.Errorf("%w: at least one node is required", ErrInvalidOptions)
	}
	if opts.Addr == "" {
		opts.Addr = defaultAddr
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	vn := opts.VirtualNodes
	if vn <= 0 {
		vn = defaultVirtualNodes
	}

	gw, err := gateway.Open(gateway.Options{
		Nodes:        opts.Nodes,
		VirtualNodes: vn,
		Timeout:      timeout,
	})
	if err != nil {
		return nil, fmt.Errorf("proxy: gateway open: %w", err)
	}

	s := &Server{
		opts:    opts,
		gw:      gw,
		mux:     http.NewServeMux(),
		startAt: time.Now(),
	}
	s.registerRoutes()
	return s, nil
}

// Handler returns the http.Handler for this server. Useful for httptest.
func (s *Server) Handler() http.Handler {
	return s.mux
}

// Addr returns the resolved listen address after Start/StartBackground,
// or the configured Addr before the listener is bound.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln != nil {
		return s.ln.Addr().String()
	}
	return s.opts.Addr
}

// Start binds the configured Addr and blocks serving HTTP requests.
// It returns nil when the server is stopped cleanly via Close.
func (s *Server) Start() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrClosed
	}
	ln, err := net.Listen("tcp", s.opts.Addr)
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("proxy: listen %s: %w", s.opts.Addr, err)
	}
	s.ln = ln
	srv := &http.Server{Handler: s.mux}
	s.srv = srv
	s.mu.Unlock()

	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// StartBackground binds and starts the server in a goroutine.
// It waits until the listener is bound before returning so callers can
// immediately use Addr(). Bind errors are returned synchronously.
func (s *Server) StartBackground() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrClosed
	}
	ln, err := net.Listen("tcp", s.opts.Addr)
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("proxy: listen %s: %w", s.opts.Addr, err)
	}
	s.ln = ln
	srv := &http.Server{Handler: s.mux}
	s.srv = srv
	s.mu.Unlock()

	go func() { _ = srv.Serve(ln) }()
	return nil
}

// Close shuts down the HTTP server and closes the underlying gateway.
// Safe to call multiple times.
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
	if err := s.gw.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// Status returns a point-in-time snapshot of the server state.
func (s *Server) Status() Status {
	return Status{
		Addr:      s.Addr(),
		StartedAt: s.startAt,
		Gateway:   s.gw.Stats(),
		Scope: Scope{
			ClientSideRoutingOnly: true,
			NoReplication:         true,
			NoRaft:                true,
			NoConsensus:           true,
			NoFailover:            true,
		},
	}
}
