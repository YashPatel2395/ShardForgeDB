package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sync"
)

// Server is a local HTTP dashboard server. It serves the HTML dashboard,
// JSON status, healthz, and events endpoints. All endpoints are read-only;
// the server never mutates any underlying store.
type Server struct {
	opts      Options
	collector Collector
	handler   http.Handler

	mu     sync.Mutex
	ln     net.Listener
	srv    *http.Server
	closed bool
}

// NewServer creates a new Server. collector must not be nil.
func NewServer(opts Options, collector Collector) (*Server, error) {
	if collector == nil {
		return nil, errors.New("dashboard: collector must not be nil")
	}
	opts.applyDefaults()

	s := &Server{
		opts:      opts,
		collector: collector,
	}
	s.handler = s.buildHandler()
	return s, nil
}

// Handler returns the http.Handler that serves all dashboard endpoints.
// It can be used directly in tests without starting a real listener.
func (s *Server) Handler() http.Handler {
	return s.handler
}

// Start listens on opts.Addr and serves requests until Close is called.
// Start blocks until the server is stopped.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.opts.Addr)
	if err != nil {
		return err
	}

	s.mu.Lock()
	if s.closed {
		_ = ln.Close()
		s.mu.Unlock()
		return errors.New("dashboard: server already closed")
	}
	s.ln = ln
	s.srv = &http.Server{Handler: s.handler}
	s.mu.Unlock()

	err = s.srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Addr returns the address the server is listening on. Returns "" if Start
// has not been called or has not yet bound.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln == nil {
		return s.opts.Addr
	}
	return s.ln.Addr().String()
}

// Close shuts down the server. It is idempotent.
func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.srv != nil {
		return s.srv.Shutdown(context.Background())
	}
	return nil
}

// buildHandler constructs the ServeMux with all routes.
func (s *Server) buildHandler() http.Handler {
	mux := http.NewServeMux()

	// GET / — HTML dashboard
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		snap := s.collector.Snapshot()
		html, err := renderHTML(snap)
		if err != nil {
			http.Error(w, "template error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(html)
	})

	// GET /status — JSON Snapshot
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		snap := s.collector.Snapshot()
		writeJSON(w, http.StatusOK, snap)
	})

	// GET /healthz — JSON {"status":"ok"}
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// GET /events — JSON array of TimelineEvents
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		snap := s.collector.Snapshot()
		events := snap.Events
		if events == nil {
			events = []TimelineEvent{}
		}
		writeJSON(w, http.StatusOK, events)
	})

	return mux
}

// writeJSON encodes v as JSON and writes it to w with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "json encoding error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}
