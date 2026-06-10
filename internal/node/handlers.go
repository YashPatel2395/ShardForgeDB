package node

import (
	"encoding/json"
	"net/http"
	"strings"
)

// registerRoutes wires all HTTP endpoints into the mux.
func (s *Server) registerRoutes() {
	s.mux.HandleFunc("/healthz", s.handleHealthz)
	s.mux.HandleFunc("/status", s.handleStatus)
	s.mux.HandleFunc("/kv/", s.handleKV)
	s.mux.HandleFunc("/scan", s.handleScan)
	s.mux.HandleFunc("/flush", s.handleFlush)
	s.mux.HandleFunc("/compact", s.handleCompact)
}

// writeJSON encodes v as JSON and writes it with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON error response.
func (s *Server) writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg, NodeID: s.opts.NodeID})
}

// handleHealthz serves GET /healthz.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok", NodeID: s.opts.NodeID})
}

// handleStatus serves GET /status.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, s.Status())
}

// handleKV dispatches /kv/{key} to the appropriate verb handler.
func (s *Server) handleKV(w http.ResponseWriter, r *http.Request) {
	// Extract key from path: /kv/<key>
	rawKey := strings.TrimPrefix(r.URL.Path, "/kv/")
	if rawKey == "" {
		s.writeError(w, http.StatusBadRequest, "key is required")
		return
	}
	key := []byte(rawKey)

	switch r.Method {
	case http.MethodGet:
		s.handleKVGet(w, r, key)
	case http.MethodPut:
		s.handleKVPut(w, r, key)
	case http.MethodDelete:
		s.handleKVDelete(w, r, key)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleKVPut(w http.ResponseWriter, r *http.Request, key []byte) {
	var req putRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if err := s.eng.Put(key, []byte(req.Value)); err != nil {
		s.writeError(w, http.StatusInternalServerError, "put failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, putResponse{OK: true, NodeID: s.opts.NodeID})
}

func (s *Server) handleKVGet(w http.ResponseWriter, r *http.Request, key []byte) {
	val, found, err := s.eng.Get(key)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "get failed: "+err.Error())
		return
	}
	resp := getResponse{Found: found, Key: string(key), NodeID: s.opts.NodeID}
	if found {
		resp.Value = string(val)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleKVDelete(w http.ResponseWriter, r *http.Request, key []byte) {
	if err := s.eng.Delete(key); err != nil {
		s.writeError(w, http.StatusInternalServerError, "delete failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, deleteResponse{OK: true, NodeID: s.opts.NodeID})
}

// handleScan serves GET /scan?start=<start>&end=<end>.
func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	q := r.URL.Query()
	start := []byte(q.Get("start"))
	end := []byte(q.Get("end"))

	entries, err := s.eng.Scan(start, end)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "scan failed: "+err.Error())
		return
	}

	out := make([]Entry, len(entries))
	for i, e := range entries {
		out[i] = Entry{Key: string(e.Key), Value: string(e.Value)}
	}
	writeJSON(w, http.StatusOK, scanResponse{NodeID: s.opts.NodeID, Entries: out})
}

// handleFlush serves POST /flush.
func (s *Server) handleFlush(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if err := s.eng.Flush(); err != nil {
		writeJSON(w, http.StatusInternalServerError, opResponse{OK: false, NodeID: s.opts.NodeID, Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, opResponse{OK: true, NodeID: s.opts.NodeID})
}

// handleCompact serves POST /compact.
func (s *Server) handleCompact(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if err := s.eng.Compact(); err != nil {
		writeJSON(w, http.StatusInternalServerError, opResponse{OK: false, NodeID: s.opts.NodeID, Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, opResponse{OK: true, NodeID: s.opts.NodeID})
}
