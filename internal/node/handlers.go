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

// writeJSONError writes a structured JSON error with a stable machine-readable code field.
// Use this for Phase 28 endpoints so callers can distinguish error types programmatically.
func (s *Server) writeJSONError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error":   message,
		"code":    code,
		"node_id": s.opts.NodeID,
	})
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
	if s.runtimeState().role == "follower" {
		s.writeError(w, http.StatusForbidden,
			"follower: writes are not accepted; this node is a read replica")
		return
	}
	if s.writeGate != nil {
		if err := s.writeGate.Enter(); err != nil {
			s.writeJSONError(w, http.StatusConflict, "node_quiesced", "node is write-quiesced")
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
	if s.runtimeState().role == "follower" {
		s.writeError(w, http.StatusForbidden,
			"follower: writes are not accepted; this node is a read replica")
		return
	}
	if s.writeGate != nil {
		if err := s.writeGate.Enter(); err != nil {
			s.writeJSONError(w, http.StatusConflict, "node_quiesced", "node is write-quiesced")
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
// Phase 28+: returns a fully typed ReplicationStatusResponse including write_state,
// quiesce, promotion, and runtime_role fields.
func (s *Server) handleReplicationStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	st := s.runtimeState()

	resp := ReplicationStatusResponse{
		NodeID:          s.opts.NodeID,
		Replication:     s.ReplicationStatus(),
		BackgroundSync:  s.BackgroundSyncStatus(),
		RuntimeRole:     st.role,
		LocalRoleSource: st.localRoleSource,
		PromotionState:  st.promotionState,
	}

	if st.role == "primary" || st.role == "standalone" {
		writeState := "active"
		switch st.quiesceState {
		case "quiesced":
			writeState = "quiesced"
		case "quiesce_failed_fenced":
			writeState = "quiesce_failed_fenced"
		}
		resp.WriteState = writeState
		resp.Quiesced = st.quiesceState == "quiesced"
		resp.QuiesceState = st.quiesceState
		if st.quiesceRecord != nil {
			resp.QuiesceID = st.quiesceRecord.QuiesceID
			resp.QuiescedAt = st.quiesceRecord.QuiescedAt
			resp.QuiescedLatestSeq = st.quiesceRecord.PrimaryLatestSeq
		}
		// Pending quiesce fields (quiesce_failed_fenced state).
		if st.pendingQuiesceRecord != nil {
			resp.PendingQuiesceID = st.pendingQuiesceRecord.QuiesceID
			resp.PendingQuiesceSeq = st.pendingQuiesceRecord.PrimaryLatestSeq
		}
		// Quiesce intent field. Distinguish two intent-active sub-states:
		//   "active"          — intent written, gate not yet closed (quiesce in flight)
		//   "cleanup_pending" — quiesce committed but intent removal failed; safe to ignore
		//                       (startup resolves matching intent+final safely), but exposed
		//                       here so operators can diagnose and manually clean up if needed.
		if st.quiesceIntentActive {
			if st.quiesceState == "quiesced" {
				resp.QuiesceIntentState = "cleanup_pending"
			} else {
				resp.QuiesceIntentState = "active"
			}
		}
	}

	// Promotion detail fields (populated after successful promotion).
	if st.promotionState == "promoted" && st.promotionRecord != nil {
		resp.PromotionSourceNodeID = st.promotionRecord.SourcePrimaryNodeID
		resp.PromotionSourceBaseURL = st.promotionRecord.SourcePrimaryBaseURL
		resp.InheritedLastSeq = st.promotionRecord.InheritedLastSeq
		resp.PromotedAt = st.promotionRecord.PromotedAt
		resp.PromotionDurableCommitted = true
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleReplicationLog serves GET /replication/log?after=<seq>&limit=<n>.
// Only valid on primary nodes. Followers and standalone nodes return 403.
func (s *Server) handleReplicationLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.runtimeState().role != "primary" {
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
// Uses the guarded ApplyReplicationEntries which checks promotionBarrier and holds
// replicationMutationMu.RLock — no apply can proceed while promotion holds the WLock.
func (s *Server) handleReplicationApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.runtimeState().role != "follower" {
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
		if errors.Is(err, ErrPromotionInProgress) {
			s.writeJSONError(w, http.StatusConflict, "promotion_in_progress",
				"promotion in progress: replication apply rejected")
			return
		}
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
	if s.runtimeState().role == "follower" {
		s.writeError(w, http.StatusForbidden,
			"follower: writes are not accepted; this node is a read replica")
		return
	}
	// ExplainPut performs a real engine write; it must respect the write gate.
	if s.writeGate != nil {
		if err := s.writeGate.Enter(); err != nil {
			s.writeJSONError(w, http.StatusConflict, "node_quiesced", "node is write-quiesced")
			return
		}
		defer s.writeGate.Exit()
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
	if s.runtimeState().role == "follower" {
		s.writeError(w, http.StatusForbidden,
			"follower: writes are not accepted; this node is a read replica")
		return
	}
	// ExplainDelete performs a real engine write; it must respect the write gate.
	if s.writeGate != nil {
		if err := s.writeGate.Enter(); err != nil {
			s.writeJSONError(w, http.StatusConflict, "node_quiesced", "node is write-quiesced")
			return
		}
		defer s.writeGate.Exit()
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
	if s.runtimeState().role != "follower" {
		s.writeError(w, http.StatusBadRequest, "sync is only valid for follower nodes")
		return
	}
	result, err := s.SyncFromPrimary(r.Context())
	if err != nil {
		// ErrPromotionInProgress: promotion has raised the barrier; sync is rejected.
		// Return 409 Conflict so the caller knows the follower is transitioning to primary.
		if errors.Is(err, ErrPromotionInProgress) {
			writeJSON(w, http.StatusConflict, map[string]any{
				"ok":      false,
				"code":    "promotion_in_progress",
				"node_id": s.opts.NodeID,
				"error":   err.Error(),
			})
			return
		}
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
//
// Quiesces the primary: drains all in-flight writes, records the final sequence number,
// and rejects all future writes. The operation is idempotent: repeated calls return the
// original quiesce record unchanged.
//
// State machine:
//
//	active              → (writeGate.Quiesce + SaveQuiesceRecord) → quiesced
//	active              → (writeGate.Quiesce, SaveQuiesceRecord fails) → quiesce_failed_fenced
//	quiesce_failed_fenced → (SaveQuiesceRecord retry with same record) → quiesced
//	quiesced            → idempotent 200 (same QuiesceID)
//
// quiesceMu serializes the entire check-transition-persist sequence so two concurrent
// requests cannot generate two different QuiesceIDs or race on the state machine.
func (s *Server) handleQuiesce(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Serialize the entire quiesce operation. A second concurrent request will block here
	// and, once the first completes, observe the updated state and return idempotent/error.
	s.quiesceMu.Lock()
	defer s.quiesceMu.Unlock()

	st := s.runtimeState()

	if st.closed {
		s.writeJSONError(w, http.StatusServiceUnavailable, "node_closing", "node is closing")
		return
	}
	if st.role != "primary" {
		s.writeJSONError(w, http.StatusBadRequest, "wrong_role",
			fmt.Sprintf("quiesce is only valid for primary nodes (current role: %s)", st.role))
		return
	}

	// Idempotent: already quiesced — return the durable record.
	if st.quiesceState == "quiesced" && st.quiesceRecord != nil {
		writeJSON(w, http.StatusOK, QuiesceResponse{
			NodeID:           s.opts.NodeID,
			WriteState:       "quiesced",
			QuiesceID:        st.quiesceRecord.QuiesceID,
			PrimaryLatestSeq: st.quiesceRecord.PrimaryLatestSeq,
			QuiescedAt:       st.quiesceRecord.QuiescedAt,
			Idempotent:       true,
		})
		return
	}

	// Retry: gate was closed but persistence failed on the previous attempt.
	// Reuse the pending record so the QuiesceID is stable across retries.
	if st.quiesceState == "quiesce_failed_fenced" {
		s.mu.Lock()
		pending := s.pendingQuiesceRecord
		s.mu.Unlock()
		if pending == nil {
			s.writeJSONError(w, http.StatusInternalServerError, "internal_error",
				"quiesce_failed_fenced state but no pending record (invariant violated)")
			return
		}
		if err := replnet.SaveQuiesceRecord(s.opts.DataDir, pending); err != nil {
			s.writeJSONError(w, http.StatusInternalServerError, "quiesce_persistence_failed",
				"quiesce record retry failed: "+err.Error())
			return
		}
		// Intent cleanup: remove the intent file that was written in the original attempt.
		// If removal fails, keep quiesceIntentActive=true and note it — the intent will be
		// safely resolved at next startup (matching intent+final IDs → quiesced, cleanup done).
		intentRemoved := s.removeQuiesceIntentFn(s.opts.DataDir) == nil
		s.mu.Lock()
		s.quiesceRecord = pending
		s.quiesceState = "quiesced"
		s.pendingQuiesceRecord = nil
		if intentRemoved {
			s.quiesceIntentActive = false
		}
		// If intent removal failed, quiesceIntentActive remains true.
		// Status reports will show quiesce_intent_state = "cleanup_pending".
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, QuiesceResponse{
			NodeID:           s.opts.NodeID,
			WriteState:       "quiesced",
			QuiesceID:        pending.QuiesceID,
			PrimaryLatestSeq: pending.PrimaryLatestSeq,
			QuiescedAt:       pending.QuiescedAt,
			Idempotent:       false,
		})
		return
	}

	if s.writeGate == nil {
		s.writeJSONError(w, http.StatusInternalServerError, "internal_error",
			"write gate not initialized")
		return
	}

	// Generate the quiesce ID before closing the gate. If entropy is unavailable,
	// abort without touching the gate so writes continue normally.
	qID, err := s.quiesceIDFn()
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, "internal_error",
			"failed to generate quiesce ID: "+err.Error())
		return
	}

	// Use the bound address (not opts.Addr which may be ":0") so the quiesce record
	// contains the actual reachable URL that followers used to connect.
	addr := s.Addr() // acquires s.mu internally; safe since we are not holding s.mu here
	if strings.HasSuffix(addr, ":0") {
		s.writeJSONError(w, http.StatusInternalServerError, "internal_error",
			"node listener not yet bound (addr is :0)")
		return
	}
	baseURL := "http://" + addr

	// Step 2: Persist the quiesce intent BEFORE closing the write gate.
	// This is the crash-safe fence: if the node crashes after intent write but before
	// the final quiesce record, startup will restore the fence rather than silently
	// restoring writes. See resolveRuntimeRole for startup recovery rules.
	intent := &replnet.QuiesceIntentRecord{
		Version:        1,
		QuiesceID:      qID,
		PrimaryNodeID:  s.opts.NodeID,
		PrimaryBaseURL: baseURL,
		IntentAt:       time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := replnet.SaveQuiesceIntent(s.opts.DataDir, intent); err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, "quiesce_persistence_failed",
			"quiesce intent persistence failed: "+err.Error())
		return
	}
	s.mu.Lock()
	s.quiesceIntentActive = true
	s.mu.Unlock()

	// Step 3: Drain all in-flight writes by taking the exclusive write-gate lock.
	// After this returns, all concurrent write handlers have exited and no new write
	// can enter (Enter() will return ErrNodeQuiesced).
	s.writeGate.Quiesce()

	// Step 4–5: Snapshot the final committed sequence and persist the quiesce record.
	var latestSeq uint64
	if s.durableLog != nil {
		latestSeq = s.durableLog.LatestSeq()
	}

	rec := &replnet.QuiesceRecord{
		Version:          1,
		QuiesceID:        qID,
		PrimaryNodeID:    s.opts.NodeID,
		PrimaryBaseURL:   baseURL,
		PrimaryLatestSeq: latestSeq,
		QuiescedAt:       time.Now().UTC().Format(time.RFC3339Nano),
	}
	rec.Checksum = replnet.QuiesceChecksum(rec)

	if err := replnet.SaveQuiesceRecord(s.opts.DataDir, rec); err != nil {
		// The write gate is closed (writes rejected) but the final record is not durable.
		// Enter quiesce_failed_fenced: preserve the pending record so the next retry
		// reuses the same QuiesceID rather than generating a new one.
		// Note: quiesceIntentActive remains true — the intent is still on disk and
		// protects the fence on restart.
		s.mu.Lock()
		s.quiesceState = "quiesce_failed_fenced"
		s.pendingQuiesceRecord = rec
		s.mu.Unlock()
		s.writeJSONError(w, http.StatusInternalServerError, "quiesce_persistence_failed",
			"quiesce record persistence failed: "+err.Error())
		return
	}

	// Step 6: Remove the intent file (crash-safe: intent without final → quiesce_failed_fenced on restart).
	// Capture the result: only clear quiesceIntentActive when removal succeeds. If it fails,
	// the stale intent remains on disk but will be resolved safely on restart (matching
	// intent+final IDs → quiesced, cleanup done). Status exposes cleanup_pending in this case.
	intentRemoved := s.removeQuiesceIntentFn(s.opts.DataDir) == nil

	s.mu.Lock()
	s.quiesceRecord = rec
	s.quiesceState = "quiesced"
	if intentRemoved {
		s.quiesceIntentActive = false
	}
	// If removal failed, quiesceIntentActive remains true; status reports cleanup_pending.
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
//
// Promotes a follower to primary after the operator has quiesced and stopped the old primary.
// The operation requires:
//  1. confirm_old_primary_stopped == true (operator acknowledgment)
//  2. A valid, checksummed QuiesceRecord from the primary
//  3. Follower's last_applied_seq == quiesce record's primary_latest_seq
//
// Concurrency:
//   - promoteMu serializes the entire check-barrier-drain-commit sequence.
//   - promotionBarrier blocks new SyncFromPrimary calls before the bgWorker is stopped.
//   - syncInProgress is drained (polled with timeout) before the final commit.
//
// Failure handling:
//   - Pre-commit (before SavePromotionRecord): revert barrier and promotionState → follower.
//   - Post-commit (after SavePromotionRecord): do NOT revert; node recovers as primary on restart.
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

	// Serialize the entire promotion operation. A second concurrent promote blocks here
	// and, once the first completes, will observe the updated state (promoted) and return
	// the idempotent response rather than attempting a duplicate commit.
	s.promoteMu.Lock()
	defer s.promoteMu.Unlock()

	st := s.runtimeState()

	if st.closed {
		s.writeJSONError(w, http.StatusServiceUnavailable, "node_closing", "node is closing")
		return
	}

	// Idempotent check must come before the role check: after promotion runtimeRole == "primary".
	if st.promotionState == "promoted" && st.promotionRecord != nil {
		if st.promotionRecord.QuiesceID == req.QuiesceRecord.QuiesceID {
			writeJSON(w, http.StatusOK, PromoteResponse{
				NodeID:           s.opts.NodeID,
				NewRole:          "primary",
				QuiesceID:        st.promotionRecord.QuiesceID,
				InheritedLastSeq: st.promotionRecord.InheritedLastSeq,
				PromotedAt:       st.promotionRecord.PromotedAt,
				Idempotent:       true,
			})
			return
		}
		s.writeJSONError(w, http.StatusConflict, "already_promoted",
			"node is already promoted with a different quiesce record")
		return
	}

	// Must be a follower at this point (idempotent check above handles "primary" after promotion).
	if st.role != "follower" {
		s.writeJSONError(w, http.StatusBadRequest, "wrong_role",
			fmt.Sprintf("promote is only valid for follower nodes (current role: %s)", st.role))
		return
	}

	// promoteMu ensures we cannot reach here concurrently, so "promoting" state implies a
	// crashed prior attempt that left the node in a partial state. Surface it explicitly.
	if st.promotionState == "promoting" {
		s.writeJSONError(w, http.StatusConflict, "promotion_in_progress",
			"promotion was previously started but did not complete; check disk state")
		return
	}

	// Validate all 16 preconditions before touching any durable state.
	if err := s.validatePromotionPreconditions(&req); err != nil {
		status, code := promotionErrorToHTTP(err)
		s.writeJSONError(w, status, code, err.Error())
		return
	}

	// Raise the promotion barrier BEFORE stopping the bgWorker so that no new
	// SyncFromPrimary call can start (or persist) after we begin the commit sequence.
	s.promotionBarrier.Store(true)

	// Mark as promoting under s.mu so status endpoints reflect the transition.
	s.mu.Lock()
	s.promotionState = "promoting"
	s.mu.Unlock()

	// Stop the background sync worker (if any). It checks its own context and exits cleanly.
	if s.bgWorker != nil {
		s.bgWorker.stop()
	}

	// Acquire exclusive replication mutex. This blocks until any in-flight SyncFromPrimary
	// or handleReplicationApply releases its RLock. No polling, no timeout — the WLock
	// guarantees mutual exclusion with the third barrier check inside each RLock holder.
	s.replicationMutationMu.Lock()

	// Re-validate dynamic state while holding the exclusive WLock. This catches
	// replication that applied entries between pre-barrier validation and WLock acquisition.
	if lockedErr := s.validatePromotionPreconditionsLocked(&req); lockedErr != nil {
		s.replicationMutationMu.Unlock()
		// Pre-commit: revert barrier and state so the follower can resume.
		s.promotionBarrier.Store(false)
		s.mu.Lock()
		s.promotionState = ""
		s.mu.Unlock()
		s.restartBackgroundWorkerAfterPromotionFailure()
		status, code := promotionErrorToHTTP(lockedErr)
		s.writeJSONError(w, status, code, lockedErr.Error())
		return
	}

	// Execute the crash-consistent two-phase commit:
	//   phase 1: journal baseline (idempotent — orphan baseline is safe on crash)
	//   phase 2: promotion record (commit point — restart recovers as primary)
	resp, err := s.executePromotion(&req)

	// Release the exclusive lock before any further state changes or I/O.
	s.replicationMutationMu.Unlock()

	if err != nil {
		// Check whether the commit point (promotion record) was written.
		// If yes: post-commit failure — do NOT revert; node recovers as primary on restart.
		// If no:  pre-commit failure — safe to revert state so the operator can retry.
		if replnet.PromotionRecordExists(s.opts.DataDir) {
			s.writeJSONError(w, http.StatusInternalServerError, "promotion_failed_post_commit",
				"promotion record was written but subsequent steps failed: "+err.Error())
			return
		}
		// Pre-commit: revert barrier and state so the follower resumes normal operation.
		s.promotionBarrier.Store(false)
		s.mu.Lock()
		s.promotionState = ""
		s.mu.Unlock()

		s.restartBackgroundWorkerAfterPromotionFailure()

		s.writeJSONError(w, http.StatusInternalServerError, "promotion_failed",
			"promotion failed (pre-commit, reverted to follower): "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, *resp)
}

// promotionErrorToHTTP maps a promotion validation error to an HTTP status and stable code.
func promotionErrorToHTTP(err error) (int, string) {
	switch {
	case errors.Is(err, ErrPromotionRecordInvalid):
		return http.StatusBadRequest, "promotion_record_invalid"
	case errors.Is(err, ErrPromotionSequenceMismatch):
		return http.StatusBadRequest, "promotion_sequence_mismatch"
	case errors.Is(err, ErrPromotionSourceMismatch):
		return http.StatusBadRequest, "promotion_source_mismatch"
	case errors.Is(err, ErrPromotionNotReady):
		return http.StatusBadRequest, "promotion_not_ready"
	default:
		return http.StatusBadRequest, "promotion_failed"
	}
}

// validatePromotionPreconditions runs static preconditions (structure and identities).
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

// validatePromotionPreconditionsLocked performs dynamic promotion checks while the
// caller holds replicationMutationMu.WLock. These checks re-read mutable state that
// may have changed between initial validation and WLock acquisition.
//
// Checks:
//   - Server not closed (concurrent Close)
//   - Runtime role still "follower" (concurrent role change — should not happen with promoteMu held)
//   - runtime lastApplied exactly equals quiesceRecord.PrimaryLatestSeq
//   - durable replication cursor also equals quiesceRecord.PrimaryLatestSeq
//
// Caller must hold replicationMutationMu.WLock and promoteMu.
func (s *Server) validatePromotionPreconditionsLocked(req *PromoteRequest) error {
	qr := &req.QuiesceRecord

	// Re-read closed and role atomically.
	s.mu.Lock()
	role := s.runtimeRole
	closed := s.closed
	s.mu.Unlock()

	if closed {
		return fmt.Errorf("%w: server closed during promotion commit", ErrPromotionNotReady)
	}
	if role != "follower" {
		return fmt.Errorf("%w: runtime role changed to %q during promotion", ErrPromotionNotReady, role)
	}

	// Re-read follower runtime cursor. No replication can occur while we hold the WLock,
	// but this catches mutations that slipped in between pre-barrier validation and WLock acquisition.
	followerSeq := atomic.LoadUint64(&s.lastApplied)
	if followerSeq != qr.PrimaryLatestSeq {
		return fmt.Errorf("%w: follower runtime seq %d diverged from quiesce seq %d between validation and lock",
			ErrPromotionSequenceMismatch, followerSeq, qr.PrimaryLatestSeq)
	}

	// Also verify the durable cursor (stateStore) matches. The durable cursor is the
	// authoritative persisted position; a runtime/durable divergence means an in-flight
	// sync applied entries without persisting the cursor, or a previous partial failure.
	if s.stateStore != nil {
		durableCursor := s.stateStore.LastAppliedSeq()
		if durableCursor != qr.PrimaryLatestSeq {
			return fmt.Errorf("%w: durable cursor %d does not match quiesce seq %d (in-flight replication applied between validation and WLock)",
				ErrPromotionSequenceMismatch, durableCursor, qr.PrimaryLatestSeq)
		}
	}

	return nil
}

// executePromotion performs the crash-consistent two-phase commit of the promotion.
//
// Called after: promotionBarrier is set, bgWorker is stopped, syncInProgress is drained.
// Phase 1 (journal baseline): idempotent — an orphan baseline on crash is safe (follower ignores it).
// Phase 2 (promotion record): the commit point — once written, restart recovers as primary.
func (s *Server) executePromotion(req *PromoteRequest) (*PromoteResponse, error) {
	qr := &req.QuiesceRecord
	dir := s.opts.DataDir
	inheritedSeq := qr.PrimaryLatestSeq

	// Phase 1: persist journal baseline (idempotent).
	// CreateJournalBaseline returns nil if already written with the same seq (crash-safe).
	if err := replnet.CreateJournalBaseline(dir, inheritedSeq); err != nil {
		return nil, fmt.Errorf("persist journal baseline: %w", err)
	}

	// Phase 2: persist promotion record (commit point).
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

	// Open new DurableLog (finds the baseline, starts at inheritedSeq+1).
	dl, err := replnet.OpenDurableLog(dir)
	if err != nil {
		return nil, fmt.Errorf("open durable log: %w", err)
	}

	// Switch runtime role atomically under s.mu.
	s.mu.Lock()
	s.runtimeRole = "primary"
	s.localRoleSource = "promotion_record"
	s.promotionRecord = promRec
	s.promotionState = "promoted"
	s.durableLog = dl
	s.writeGate = &writeGate{}
	s.quiesceState = "active"
	s.replicator = nil // clear follower-only state
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
