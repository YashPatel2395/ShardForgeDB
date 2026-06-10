package proxy

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/YashPatel2395/ShardForgeDB/internal/gateway"
)

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("/healthz", s.handleHealthz)
	s.mux.HandleFunc("/status", s.handleStatus)
	s.mux.HandleFunc("/route/", s.handleRoute)
	s.mux.HandleFunc("/kv/", s.handleKV)
	s.mux.HandleFunc("/scan-node/", s.handleScanNode)
	s.mux.HandleFunc("/flush-all", s.handleFlushAll)
	s.mux.HandleFunc("/compact-all", s.handleCompactAll)
	s.mux.HandleFunc("/nodes/health", s.handleNodesHealth)
	s.mux.HandleFunc("/replication/status", s.handleReplicationStatus)
	s.mux.HandleFunc("/replication/sync-node/", s.handleReplicationSyncNode)
	s.mux.HandleFunc("/", s.handleNotFound)
}

// writeJSON encodes v as JSON to w with the given status code.
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON error body with the given status code.
func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// --- /healthz ---

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- /status ---

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, s.Status())
}

// --- /route/{key} ---

func (s *Server) handleRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	key := strings.TrimPrefix(r.URL.Path, "/route/")
	if key == "" {
		writeError(w, http.StatusBadRequest, "key is required")
		return
	}
	rn, err := s.gw.NodeForKey([]byte(key))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, RouteResponse{
		Key:     key,
		NodeID:  rn.ID,
		BaseURL: rn.BaseURL,
	})
}

// --- /kv/{key} ---

func (s *Server) handleKV(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/kv/")
	if key == "" {
		writeError(w, http.StatusBadRequest, "key is required")
		return
	}
	switch r.Method {
	case http.MethodPut:
		s.handlePut(w, r, key)
	case http.MethodGet:
		s.handleGet(w, r, key)
	case http.MethodDelete:
		s.handleDelete(w, r, key)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handlePut(w http.ResponseWriter, r *http.Request, key string) {
	var body struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := s.gw.Put(r.Context(), []byte(key), []byte(body.Value)); err != nil {
		writeError(w, backendErrorCode(err), err.Error())
		return
	}
	rn, _ := s.gw.NodeForKey([]byte(key))
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"key":      key,
		"node_id":  rn.ID,
		"base_url": rn.BaseURL,
	})
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request, key string) {
	val, found, err := s.gw.Get(r.Context(), []byte(key))
	if err != nil {
		writeError(w, backendErrorCode(err), err.Error())
		return
	}
	rn, _ := s.gw.NodeForKey([]byte(key))
	writeJSON(w, http.StatusOK, map[string]any{
		"found":    found,
		"key":      key,
		"value":    string(val),
		"node_id":  rn.ID,
		"base_url": rn.BaseURL,
	})
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request, key string) {
	if err := s.gw.Delete(r.Context(), []byte(key)); err != nil {
		writeError(w, backendErrorCode(err), err.Error())
		return
	}
	rn, _ := s.gw.NodeForKey([]byte(key))
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"key":      key,
		"node_id":  rn.ID,
		"base_url": rn.BaseURL,
	})
}

// --- /scan-node/{nodeID} ---

func (s *Server) handleScanNode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	nodeID := strings.TrimPrefix(r.URL.Path, "/scan-node/")
	if nodeID == "" {
		writeError(w, http.StatusBadRequest, "node ID is required")
		return
	}
	q := r.URL.Query()
	start := []byte(q.Get("start"))
	end := []byte(q.Get("end"))

	entries, err := s.gw.ScanNode(r.Context(), nodeID, start, end)
	if err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, gateway.ErrUnknownNode) {
			code = http.StatusNotFound
		}
		writeError(w, code, err.Error())
		return
	}

	type entryJSON struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	out := make([]entryJSON, len(entries))
	for i, e := range entries {
		out[i] = entryJSON{Key: string(e.Key), Value: string(e.Value)}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"node_id":       nodeID,
		"per_node_only": true,
		"count":         len(out),
		"entries":       out,
	})
}

// --- /flush-all ---

func (s *Server) handleFlushAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if err := s.gw.FlushAll(r.Context()); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"flushed_nodes": s.gw.Stats().NodeCount,
	})
}

// --- /compact-all ---

func (s *Server) handleCompactAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if err := s.gw.CompactAll(r.Context()); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":              true,
		"compacted_nodes": s.gw.Stats().NodeCount,
	})
}

// --- /nodes/health ---

func (s *Server) handleNodesHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	results := s.gw.HealthAll(r.Context())

	type nodeResult struct {
		NodeID string `json:"node_id"`
		OK     bool   `json:"ok"`
		Error  string `json:"error,omitempty"`
	}
	nodes := make([]nodeResult, 0, len(results))
	for id, err := range results {
		nr := nodeResult{NodeID: id, OK: err == nil}
		if err != nil {
			nr.Error = err.Error()
		}
		nodes = append(nodes, nr)
	}
	// Sort by node ID for deterministic output.
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].NodeID < nodes[j].NodeID
	})
	writeJSON(w, http.StatusOK, map[string]any{"nodes": nodes})
}

// --- /replication/status ---

// handleReplicationStatus fans out GET /replication/status to all nodes and aggregates results.
func (s *Server) handleReplicationStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	results := s.gw.ForwardToAll(r.Context(), http.MethodGet, "/replication/status", nil)

	type nodeResult struct {
		NodeID string `json:"node_id"`
		OK     bool   `json:"ok"`
		Body   any    `json:"body,omitempty"`
		Error  string `json:"error,omitempty"`
	}
	nodes := make([]nodeResult, 0, len(results))
	for id, res := range results {
		nr := nodeResult{NodeID: id, OK: res.Err == nil}
		if res.Err != nil {
			nr.Error = res.Err.Error()
		} else {
			nr.Body = res.Body
		}
		nodes = append(nodes, nr)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].NodeID < nodes[j].NodeID })
	writeJSON(w, http.StatusOK, map[string]any{"nodes": nodes})
}

// --- /replication/sync-node/{nodeID} ---

// handleReplicationSyncNode forwards POST /replication/sync to the specified node.
func (s *Server) handleReplicationSyncNode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	nodeID := strings.TrimPrefix(r.URL.Path, "/replication/sync-node/")
	if nodeID == "" {
		writeError(w, http.StatusBadRequest, "node ID is required")
		return
	}

	body, err := s.gw.ForwardToNode(r.Context(), nodeID, http.MethodPost, "/replication/sync", nil)
	if err != nil {
		if errors.Is(err, gateway.ErrUnknownNode) {
			writeError(w, http.StatusNotFound, "unknown node: "+nodeID)
			return
		}
		writeError(w, http.StatusBadGateway, "sync failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, body)
}

// --- catch-all ---

func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "not found: "+r.URL.Path)
}

// backendErrorCode maps gateway/node errors to appropriate HTTP status codes.
func backendErrorCode(err error) int {
	if errors.Is(err, gateway.ErrInvalidKey) {
		return http.StatusBadRequest
	}
	if errors.Is(err, gateway.ErrUnknownNode) {
		return http.StatusBadRequest
	}
	if errors.Is(err, gateway.ErrClosed) {
		return http.StatusServiceUnavailable
	}
	// Network/connection errors from backend node → 502 Bad Gateway.
	return http.StatusBadGateway
}
