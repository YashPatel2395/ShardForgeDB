package node

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/YashPatel2395/ShardForgeDB/internal/replnet"
)

// registerRoutes wires all HTTP endpoints into the mux.
func (s *Server) registerRoutes() {
	s.mux.HandleFunc("/healthz", s.handleHealthz)
	s.mux.HandleFunc("/status", s.handleStatus)
	s.mux.HandleFunc("/kv/", s.handleKV)
	s.mux.HandleFunc("/scan", s.handleScan)
	s.mux.HandleFunc("/flush", s.handleFlush)
	s.mux.HandleFunc("/compact", s.handleCompact)
	s.mux.HandleFunc("/replication/status", s.handleReplicationStatus)
	s.mux.HandleFunc("/replication/log", s.handleReplicationLog)
	s.mux.HandleFunc("/replication/apply", s.handleReplicationApply)
	s.mux.HandleFunc("/replication/sync", s.handleReplicationSync)
	s.mux.HandleFunc("/explain/put", s.handleExplainPut)
	s.mux.HandleFunc("/explain/get", s.handleExplainGet)
	s.mux.HandleFunc("/explain/delete", s.handleExplainDelete)
	s.mux.HandleFunc("/explain/scan", s.handleExplainScan)
	// Phase 28: manual promotion and controlled failover.
	s.mux.HandleFunc("/replication/quiesce", s.handleQuiesce)
	s.mux.HandleFunc("/replication/promote", s.handlePromote)
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
	if s.runtimeRole == "follower" {
		s.writeError(w, http.StatusForbidden,
			"follower: writes are not accepted; this node is a read replica")
		return
	}
	if s.writeGate != nil {
		if err := s.writeGate.Enter(); err != nil {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":   "node_quiesced",
				"message": "node is write-quiesced",
				"node_id": s.opts.NodeID,
			})
			return
		}
		defer s.writeGate.Exit()
	}
	var req putRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if err := s.eng.Put(key, []byte(req.Value)); err != nil {
		s.writeError(w, http.StatusInternalServerError, "put failed: "+err.Error())
		return
	}
	// Write-after-commit: engine write succeeds first, then append to durable journal.
	// Crash window between these two is documented in REPLICATION_DURABILITY_DESIGN.md.
	if s.durableLog != nil {
		s.durableLog.Append(replnet.OpPut, string(key), req.Value)
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
	if s.runtimeRole == "follower" {
		s.writeError(w, http.StatusForbidden,
			"follower: writes are not accepted; this node is a read replica")
		return
	}
	if s.writeGate != nil {
		if err := s.writeGate.Enter(); err != nil {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":   "node_quiesced",
				"message": "node is write-quiesced",
				"node_id": s.opts.NodeID,
			})
			return
		}
		defer s.writeGate.Exit()
	}
	if err := s.eng.Delete(key); err != nil {
		s.writeError(w, http.StatusInternalServerError, "delete failed: "+err.Error())
		return
	}
	// Write-after-commit: engine write succeeds first, then append to durable journal.
	if s.durableLog != nil {
		s.durableLog.Append(replnet.OpDelete, string(key), "")
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

// handleReplicationStatus serves GET /replication/status.
// Phase 28+: includes write_state, quiesce, promotion, and runtime_role fields.
func (s *Server) handleReplicationStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	s.mu.Lock()
	runtimeRole := s.runtimeRole
	localRoleSource := s.localRoleSource
	quiesceState := s.quiesceState
	quiesceRec := s.quiesceRecord
	promotionState := s.promotionState
	s.mu.Unlock()

	result := map[string]any{
		"node_id":           s.opts.NodeID,
		"replication":       s.ReplicationStatus(),
		"background_sync":   s.BackgroundSyncStatus(),
		"runtime_role":      runtimeRole,
		"local_role_source": localRoleSource,
	}

	if runtimeRole == "primary" || runtimeRole == "standalone" {
		writeState := "active"
		if quiesceState == "quiesced" {
			writeState = "quiesced"
		}
		result["write_state"] = writeState
		result["quiesced"] = quiesceState == "quiesced"
		if quiesceRec != nil {
			result["quiesce_id"] = quiesceRec.QuiesceID
			result["quiesced_at"] = quiesceRec.QuiescedAt
			result["quiesced_latest_seq"] = quiesceRec.PrimaryLatestSeq
		}
	}

	if promotionState == "promoted" {
		result["promotion_state"] = "promoted"
	}

	writeJSON(w, http.StatusOK, result)
}

// handleReplicationLog serves GET /replication/log?after=<seq>&limit=<n>.
// Only valid on primary nodes. Followers and standalone nodes return 403.
func (s *Server) handleReplicationLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.runtimeRole != "primary" {
		s.writeError(w, http.StatusForbidden, "replication log is only available on primary nodes")
		return
	}
	q := r.URL.Query()
	var after uint64
	if v := q.Get("after"); v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid after: "+err.Error())
			return
		}
		after = n
	}
	var limit int
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid limit: "+err.Error())
			return
		}
		limit = n
	}
	entries, err := s.ReplicationEntries(after, limit)
	if err != nil {
		var gapErr *replnet.ReplicationGapError
		if errors.As(err, &gapErr) {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":   gapErr.Error(),
				"gap":     gapErr,
				"node_id": s.opts.NodeID,
			})
			return
		}
		s.writeError(w, http.StatusInternalServerError, "replication log: "+err.Error())
		return
	}
	if entries == nil {
		entries = []replnet.Entry{}
	}
	// Include primary_latest_seq so followers can compute operation-count lag without
	// an extra round-trip. Source: the durable log's current LastSeq.
	var primaryLatestSeq uint64
	if s.durableLog != nil {
		if st, stErr := s.durableLog.Stats(); stErr == nil {
			primaryLatestSeq = st.LastSeq
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"node_id":            s.opts.NodeID,
		"after":              after,
		"count":              len(entries),
		"entries":            entries,
		"primary_latest_seq": primaryLatestSeq,
	})
}

// handleReplicationApply serves POST /replication/apply.
// Only valid on follower nodes. Primary and standalone nodes return 403.
func (s *Server) handleReplicationApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.runtimeRole != "follower" {
		s.writeError(w, http.StatusForbidden, "replication apply is only valid for follower nodes")
		return
	}
	var body struct {
		Entries []replnet.Entry `json:"entries"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	lastSeq, err := s.ApplyReplicationEntries(body.Entries)
	if err != nil {
		s.writeError(w, http.StatusUnprocessableEntity, "apply failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":               true,
		"node_id":          s.opts.NodeID,
		"applied":          len(body.Entries),
		"last_applied_seq": lastSeq,
	})
}

// handleExplainPut serves POST /explain/put.
// Executes a real PUT via engine.ExplainPut and returns the execution trace.
// SCOPE: single-node only. No distributed tracing.
func (s *Server) handleExplainPut(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.runtimeRole == "follower" {
		s.writeError(w, http.StatusForbidden,
			"follower: writes are not accepted; this node is a read replica")
		return
	}
	var req explainPutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	tr, err := s.eng.ExplainPut([]byte(req.Key), []byte(req.Value))
	resp := ExplainPutResponse{
		NodeID:    s.opts.NodeID,
		Operation: "PUT",
		Trace:     tr,
	}
	if err != nil {
		resp.Error = err.Error()
		writeJSON(w, http.StatusUnprocessableEntity, resp)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleExplainGet serves GET /explain/get?key=<key>.
// Executes a real GET via engine.ExplainGet and returns the execution trace.
// SCOPE: single-node only. No distributed tracing.
func (s *Server) handleExplainGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	key := r.URL.Query().Get("key")
	if key == "" {
		s.writeError(w, http.StatusBadRequest, "key query parameter is required")
		return
	}
	tr, val, found, err := s.eng.ExplainGet([]byte(key))
	resp := ExplainGetResponse{
		NodeID:    s.opts.NodeID,
		Operation: "GET",
		Key:       key,
		Found:     found,
		Trace:     tr,
	}
	if found {
		resp.Value = string(val)
	}
	if err != nil {
		resp.Error = err.Error()
		writeJSON(w, http.StatusUnprocessableEntity, resp)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleExplainDelete serves DELETE /explain/delete?key=<key>.
// Executes a real DELETE via engine.ExplainDelete and returns the execution trace.
// SCOPE: single-node only. No distributed tracing.
func (s *Server) handleExplainDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", "DELETE")
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.runtimeRole == "follower" {
		s.writeError(w, http.StatusForbidden,
			"follower: writes are not accepted; this node is a read replica")
		return
	}
	key := r.URL.Query().Get("key")
	if key == "" {
		s.writeError(w, http.StatusBadRequest, "key query parameter is required")
		return
	}
	tr, err := s.eng.ExplainDelete([]byte(key))
	resp := ExplainDeleteResponse{
		NodeID:    s.opts.NodeID,
		Operation: "DELETE",
		Key:       key,
		Trace:     tr,
	}
	if err != nil {
		resp.Error = err.Error()
		writeJSON(w, http.StatusUnprocessableEntity, resp)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleExplainScan serves GET /explain/scan?start=<start>&end=<end>.
// Executes a real SCAN via engine.ExplainScan and returns the execution trace.
// SCOPE: single-node only. No distributed tracing.
func (s *Server) handleExplainScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	q := r.URL.Query()
	start := []byte(q.Get("start"))
	end := []byte(q.Get("end"))
	tr, entries, err := s.eng.ExplainScan(start, end)
	resp := ExplainScanResponse{
		NodeID:      s.opts.NodeID,
		Operation:   "SCAN",
		ResultCount: len(entries),
		Trace:       tr,
	}
	if err != nil {
		resp.Error = err.Error()
		writeJSON(w, http.StatusUnprocessableEntity, resp)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleReplicationSync serves POST /replication/sync.
// Triggers an explicit pull-based sync from the configured primary.
// Only valid for follower nodes. Returns a SyncResult with fetched/applied counts.
//
// Scope: explicit operator-triggered pull only. No background loop, no quorum, no Raft.
func (s *Server) handleReplicationSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.runtimeRole != "follower" {
		s.writeError(w, http.StatusBadRequest, "sync is only valid for follower nodes")
		return
	}
	result, err := s.SyncFromPrimary(r.Context())
	if err != nil {
		// ErrSyncInProgress: a concurrent sync (background worker or another manual call)
		// is already in flight. Return 409 Conflict so the caller can retry.
		if errors.Is(err, ErrSyncInProgress) {
			writeJSON(w, http.StatusConflict, map[string]any{
				"ok":      false,
				"code":    "sync_in_progress",
				"node_id": s.opts.NodeID,
				"error":   err.Error(),
			})
			return
		}
		var gapErr *replnet.ReplicationGapError
		if errors.As(err, &gapErr) {
			writeJSON(w, http.StatusConflict, map[string]any{
				"ok":                      false,
				"node_id":                 s.opts.NodeID,
				"source_node":             result.SourceNode,
				"follower_node":           result.FollowerNode,
				"fetched":                 result.Fetched,
				"applied":                 result.Applied,
				"last_applied_seq":        result.LastAppliedSeq,
				"primary_latest_seq":      result.PrimaryLatestSeq,
				"lag_entries_after_sync":  result.LagEntriesAfterSync,
				"lag_known":               result.LagKnown,
				"background_sync_enabled": result.BackgroundSyncEnabled,
				"replication":             result.Replication,
				"gap":                     gapErr,
				"error":                   err.Error(),
			})
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"ok":                      false,
			"node_id":                 s.opts.NodeID,
			"source_node":             result.SourceNode,
			"follower_node":           result.FollowerNode,
			"fetched":                 result.Fetched,
			"applied":                 result.Applied,
			"last_applied_seq":        result.LastAppliedSeq,
			"primary_latest_seq":      result.PrimaryLatestSeq,
			"lag_entries_after_sync":  result.LagEntriesAfterSync,
			"lag_known":               result.LagKnown,
			"background_sync_enabled": result.BackgroundSyncEnabled,
			"replication":             result.Replication,
			"error":                   err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                      true,
		"node_id":                 s.opts.NodeID,
		"source_node":             result.SourceNode,
		"follower_node":           result.FollowerNode,
		"fetched":                 result.Fetched,
		"applied":                 result.Applied,
		"last_applied_seq":        result.LastAppliedSeq,
		"primary_latest_seq":      result.PrimaryLatestSeq,
		"lag_entries_after_sync":  result.LagEntriesAfterSync,
		"lag_known":               result.LagKnown,
		"background_sync_enabled": result.BackgroundSyncEnabled,
		"replication":             result.Replication,
	})
}

// ── Phase 28: Quiesce and Promote handlers ──────────────────────────────────────

// handleQuiesce serves POST /replication/quiesce.
// Quiesces the primary: drains in-flight writes, records final seq, rejects future writes.
func (s *Server) handleQuiesce(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	s.mu.Lock()
	runtimeRole := s.runtimeRole
	quiesceState := s.quiesceState
	quiesceRec := s.quiesceRecord
	closed := s.closed
	s.mu.Unlock()

	if closed {
		s.writeError(w, http.StatusServiceUnavailable, "node is closing")
		return
	}

	if runtimeRole != "primary" {
		s.writeError(w, http.StatusBadRequest,
			fmt.Sprintf("quiesce is only valid for primary nodes (current role: %s)", runtimeRole))
		return
	}

	// Idempotent: if already quiesced, return the existing record.
	if quiesceState == "quiesced" && quiesceRec != nil {
		writeJSON(w, http.StatusOK, QuiesceResponse{
			NodeID:           s.opts.NodeID,
			WriteState:       "quiesced",
			QuiesceID:        quiesceRec.QuiesceID,
			PrimaryLatestSeq: quiesceRec.PrimaryLatestSeq,
			QuiescedAt:       quiesceRec.QuiescedAt,
			Idempotent:       true,
		})
		return
	}

	if s.writeGate == nil {
		s.writeError(w, http.StatusInternalServerError, "write gate not initialized")
		return
	}

	// Drain all in-flight writes by taking the exclusive lock.
	s.writeGate.Quiesce()

	// Get the final sequence from the journal.
	var latestSeq uint64
	if s.durableLog != nil {
		latestSeq = s.durableLog.LatestSeq()
	}

	// Create and persist the quiesce record.
	baseURL := s.opts.Addr
	if !strings.HasPrefix(baseURL, "http") {
		baseURL = "http://" + baseURL
	}
	rec := replnet.NewQuiesceRecord(s.opts.NodeID, baseURL, latestSeq)
	if err := replnet.SaveQuiesceRecord(s.opts.DataDir, rec); err != nil {
		// Quiesce record persistence failed. The write gate is already closed,
		// so writes are rejected, but the record is not durable.
		s.writeError(w, http.StatusInternalServerError,
			"quiesce record persistence failed: "+err.Error())
		return
	}

	s.mu.Lock()
	s.quiesceRecord = rec
	s.quiesceState = "quiesced"
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, QuiesceResponse{
		NodeID:           s.opts.NodeID,
		WriteState:       "quiesced",
		QuiesceID:        rec.QuiesceID,
		PrimaryLatestSeq: rec.PrimaryLatestSeq,
		QuiescedAt:       rec.QuiescedAt,
		Idempotent:       false,
	})
}

// handlePromote serves POST /replication/promote.
// Promotes a follower to primary after the operator has quiesced and stopped the old primary.
func (s *Server) handlePromote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req PromoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}

	s.mu.Lock()
	runtimeRole := s.runtimeRole
	promotionState := s.promotionState
	existingPromotion := s.promotionRecord
	closed := s.closed
	s.mu.Unlock()

	if closed {
		s.writeError(w, http.StatusServiceUnavailable, "node is closing")
		return
	}

	// Already promoted — idempotent check (must come before role check since
	// after promotion the runtime role is "primary").
	if promotionState == "promoted" && existingPromotion != nil {
		if existingPromotion.QuiesceID == req.QuiesceRecord.QuiesceID {
			writeJSON(w, http.StatusOK, PromoteResponse{
				NodeID:           s.opts.NodeID,
				NewRole:          "primary",
				QuiesceID:        existingPromotion.QuiesceID,
				InheritedLastSeq: existingPromotion.InheritedLastSeq,
				PromotedAt:       existingPromotion.PromotedAt,
				Idempotent:       true,
			})
			return
		}
		s.writeError(w, http.StatusConflict,
			"node is already promoted with a different quiesce record")
		return
	}

	// Must be a follower (check after idempotent check).
	if runtimeRole != "follower" {
		s.writeError(w, http.StatusBadRequest,
			fmt.Sprintf("promote is only valid for follower nodes (current role: %s)", runtimeRole))
		return
	}

	if promotionState == "promoting" {
		s.writeError(w, http.StatusConflict, "promotion is already in progress")
		return
	}

	// Validate preconditions.
	if err := s.validatePromotionPreconditions(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Mark as promoting.
	s.mu.Lock()
	s.promotionState = "promoting"
	s.mu.Unlock()

	// Execute promotion.
	resp, err := s.executePromotion(&req)
	if err != nil {
		s.mu.Lock()
		s.promotionState = ""
		s.mu.Unlock()
		s.writeError(w, http.StatusInternalServerError, "promotion failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, *resp)
}

// validatePromotionPreconditions runs all 17 promotion checks.
func (s *Server) validatePromotionPreconditions(req *PromoteRequest) error {
	qr := &req.QuiesceRecord

	// 4. confirm_old_primary_stopped must be true.
	if !req.ConfirmOldPrimaryStopped {
		return fmt.Errorf("confirm_old_primary_stopped must be true")
	}

	// 5. Version check.
	if qr.Version != 1 {
		return fmt.Errorf("%w: unsupported version %d", ErrPromotionRecordInvalid, qr.Version)
	}

	// 6. Checksum check.
	want := replnet.QuiesceChecksum(qr)
	if qr.Checksum != want {
		return fmt.Errorf("%w: checksum mismatch (got %d, want %d)",
			ErrPromotionRecordInvalid, qr.Checksum, want)
	}

	// 7. primary_node_id is non-empty.
	if qr.PrimaryNodeID == "" {
		return fmt.Errorf("%w: primary_node_id is empty", ErrPromotionRecordInvalid)
	}

	// 8. primary_base_url matches configured primary.
	if qr.PrimaryBaseURL != s.opts.Replication.PrimaryBaseURL {
		return fmt.Errorf("%w: quiesce record primary_base_url %q does not match configured %q",
			ErrPromotionSourceMismatch, qr.PrimaryBaseURL, s.opts.Replication.PrimaryBaseURL)
	}

	// 9. quiesce_id is non-empty.
	if qr.QuiesceID == "" {
		return fmt.Errorf("%w: quiesce_id is empty", ErrPromotionRecordInvalid)
	}

	// 10. primary_latest_seq != MaxUint64 (would overflow).
	if qr.PrimaryLatestSeq == math.MaxUint64 {
		return fmt.Errorf("%w: primary_latest_seq is MaxUint64 (would overflow)",
			ErrPromotionRecordInvalid)
	}

	// 11. Follower's last_applied_seq == quiesce record primary_latest_seq.
	followerSeq := atomic.LoadUint64(&s.lastApplied)
	if followerSeq != qr.PrimaryLatestSeq {
		return fmt.Errorf("%w: follower seq %d != quiesce seq %d",
			ErrPromotionSequenceMismatch, followerSeq, qr.PrimaryLatestSeq)
	}

	// 12. Follower must have applied at least one entry if primary had entries.
	if qr.PrimaryLatestSeq > 0 && followerSeq == 0 {
		return fmt.Errorf("%w: primary had entries (seq=%d) but follower applied none",
			ErrPromotionSequenceMismatch, qr.PrimaryLatestSeq)
	}

	// 13. No sync in progress.
	if s.syncInProgress.Load() {
		return fmt.Errorf("%w: sync is currently in progress", ErrPromotionNotReady)
	}

	// 16. quiesced_at is a valid RFC3339 timestamp.
	if _, err := time.Parse(time.RFC3339Nano, qr.QuiescedAt); err != nil {
		return fmt.Errorf("%w: invalid quiesced_at timestamp: %v", ErrPromotionRecordInvalid, err)
	}

	// 17. Node ID is non-empty.
	if s.opts.NodeID == "" {
		return fmt.Errorf("%w: node_id is empty", ErrPromotionNotReady)
	}

	return nil
}

// executePromotion performs the actual promotion sequence.
func (s *Server) executePromotion(req *PromoteRequest) (*PromoteResponse, error) {
	qr := &req.QuiesceRecord
	dir := s.opts.DataDir

	// 1. Stop background sync worker.
	if s.bgWorker != nil {
		s.bgWorker.stop()
	}

	inheritedSeq := qr.PrimaryLatestSeq

	// 2. Persist journal baseline.
	baseline := &replnet.JournalBaseline{
		Version: 1,
		BaseSeq: inheritedSeq,
	}
	if err := replnet.SaveJournalBaseline(dir, baseline); err != nil {
		return nil, fmt.Errorf("persist journal baseline: %w", err)
	}

	// 3. Persist promotion record.
	promRec := replnet.NewPromotionRecord(
		s.opts.NodeID,
		qr.PrimaryNodeID,
		qr.PrimaryBaseURL,
		qr.QuiesceID,
		inheritedSeq,
	)
	if err := replnet.SavePromotionRecord(dir, promRec); err != nil {
		return nil, fmt.Errorf("persist promotion record: %w", err)
	}

	// 4. Open new DurableLog (will find baseline, start at inheritedSeq+1).
	dl, err := replnet.OpenDurableLog(dir)
	if err != nil {
		return nil, fmt.Errorf("open durable log: %w", err)
	}

	// 5. Switch runtime role.
	s.mu.Lock()
	s.runtimeRole = "primary"
	s.localRoleSource = "promotion_record"
	s.promotionRecord = promRec
	s.promotionState = "promoted"
	s.durableLog = dl
	s.writeGate = &writeGate{}
	s.quiesceState = "active"
	// Clear follower-only state.
	s.replicator = nil
	s.mu.Unlock()

	return &PromoteResponse{
		NodeID:           s.opts.NodeID,
		NewRole:          "primary",
		QuiesceID:        qr.QuiesceID,
		InheritedLastSeq: inheritedSeq,
		PromotedAt:       promRec.PromotedAt,
		Idempotent:       false,
	}, nil
}
