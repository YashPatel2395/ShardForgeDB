// Package node implements a real networked database node for ShardForgeDB.
//
// Scope: Each node is an independent single-process key-value store backed by a local Engine.
// Nodes communicate over HTTP/JSON. This is NOT distributed consensus, NOT Raft,
// NOT quorum replication, NOT automatic leader election. It is a networked node runtime
// foundation — each node owns its own data directory and serves a simple key-value API.
package node

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/YashPatel2395/ShardForgeDB/internal/replnet"
	"github.com/YashPatel2395/ShardForgeDB/internal/trace"
)

// Duration is a time.Duration that marshals/unmarshals as a Go duration string (e.g. "500ms").
// Used in BackgroundSyncConfig so JSON config files can use human-readable durations.
type Duration struct {
	time.Duration
}

// MarshalJSON encodes the duration as a quoted string (e.g. "500ms").
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.Duration.String())
}

// UnmarshalJSON decodes a quoted duration string (e.g. "500ms") into d.
func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("node: duration must be a string (e.g. \"500ms\"): %w", err)
	}
	dur, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("node: invalid duration %q: %w", s, err)
	}
	d.Duration = dur
	return nil
}

// BackgroundSyncConfig configures the optional background pull-replication worker.
// Valid only for follower nodes. Ignored (and rejected) for primary and standalone nodes.
//
// Scope: automatic pull-replication only. No push, no Raft, no consensus, no failover.
type BackgroundSyncConfig struct {
	// Enabled controls whether the background worker is active.
	// Default: false. Must be false for primary and standalone nodes.
	Enabled bool `json:"enabled"`

	// Interval is the wait between a successful sync and the next poll attempt.
	// Must be positive when Enabled is true.
	Interval Duration `json:"interval"`

	// RequestTimeout is the per-request HTTP timeout for calls to the primary.
	// Must be positive when Enabled is true.
	RequestTimeout Duration `json:"request_timeout"`

	// InitialBackoff is the delay after the first consecutive failure.
	// Must be positive when Enabled is true.
	InitialBackoff Duration `json:"initial_backoff"`

	// MaxBackoff caps the exponential backoff growth.
	// Must be >= InitialBackoff when Enabled is true.
	MaxBackoff Duration `json:"max_backoff"`

	// JitterFraction adds a random component to backoff delays to prevent
	// thundering-herd behavior. Range [0.0, 1.0]. 0 disables jitter.
	JitterFraction float64 `json:"jitter_fraction"`
}

// WorkerState describes the current lifecycle state of the background sync worker.
type WorkerState string

const (
	// WorkerStateDisabled means background sync is not configured or not applicable.
	WorkerStateDisabled WorkerState = "disabled"
	// WorkerStateStarting means the worker goroutine is initializing.
	WorkerStateStarting WorkerState = "starting"
	// WorkerStateRunning means the worker is actively syncing or waiting for the next interval.
	WorkerStateRunning WorkerState = "running"
	// WorkerStateBackingOff means the worker is waiting after a temporary failure.
	WorkerStateBackingOff WorkerState = "backing_off"
	// WorkerStateBlocked means the worker has encountered a terminal error (e.g. replication gap)
	// and will not retry until the node is restarted or reconfigured.
	WorkerStateBlocked WorkerState = "blocked"
	// WorkerStateStopping means Close has been called and the worker is draining.
	WorkerStateStopping WorkerState = "stopping"
	// WorkerStateStopped means the worker goroutine has exited.
	WorkerStateStopped WorkerState = "stopped"
)

// BackgroundSyncStatus is a point-in-time snapshot of the background sync worker state.
// Included in GET /replication/status responses for follower nodes.
type BackgroundSyncStatus struct {
	// Lifecycle.
	Enabled bool        `json:"background_sync_enabled"`
	Running bool        `json:"background_sync_running"`
	State   WorkerState `json:"background_sync_state"`

	// Timing (UTC, omitted when not yet observed).
	LastAttemptAt *time.Time `json:"last_sync_attempt_at,omitempty"`
	LastSuccessAt *time.Time `json:"last_sync_success_at,omitempty"`
	LastFailureAt *time.Time `json:"last_sync_failure_at,omitempty"`

	// Per-attempt result (from the last successful sync).
	LastFetched int `json:"last_sync_fetched"`
	LastApplied int `json:"last_sync_applied"`

	// Cumulative counters (since process start).
	TotalAttempts    int64 `json:"total_sync_attempts"`
	TotalSuccesses   int64 `json:"total_sync_successes"`
	TotalFailures    int64 `json:"total_sync_failures"`
	TotalSkippedBusy int64 `json:"total_sync_skipped_busy"`

	// Retry / backoff state.
	ConsecutiveFailures int        `json:"consecutive_failures"`
	CurrentBackoffMs    int64      `json:"current_backoff_ms,omitempty"`
	NextRetryAt         *time.Time `json:"next_retry_at,omitempty"`
	LastError           string     `json:"last_sync_error,omitempty"`

	// Lag tracking.
	// LagKnown is false when lag has never been observed or when the primary
	// is currently unreachable. Never interpret LagEntries as zero-lag when LagKnown is false.
	FollowerLastAppliedSeq uint64     `json:"follower_last_applied_seq"`
	PrimaryLatestSeq       uint64     `json:"primary_latest_seq,omitempty"`
	LagEntries             int64      `json:"lag_entries"`
	LagKnown               bool       `json:"lag_known"`
	LagObservedAt          *time.Time `json:"lag_observed_at,omitempty"`

	// Terminal (blocked) state.
	BlockedReason string                       `json:"blocked_reason,omitempty"`
	Gap           *replnet.ReplicationGapError `json:"replication_gap,omitempty"`
}

// NodeID identifies a node in the network. It is a human-readable string such as "node-1".
type NodeID string

// ReplicationOptions configures optional networked replication for a node.
// Leave zero-valued for standalone mode (no replication).
type ReplicationOptions struct {
	// Role is the replication role: replnet.RolePrimary or replnet.RoleFollower.
	// Empty means standalone (no replication).
	Role replnet.Role

	// PrimaryBaseURL is the HTTP base URL of the primary node.
	// Required when Role == replnet.RoleFollower; ignored otherwise.
	PrimaryBaseURL string

	// BackgroundSync configures optional automatic background pull replication.
	// Valid only for follower nodes. Primary and standalone nodes must leave this
	// at its zero value (Enabled: false).
	BackgroundSync BackgroundSyncConfig
}

// Options configures a node Server.
type Options struct {
	// NodeID is a unique identifier for this node. Required.
	NodeID string

	// Addr is the TCP address the HTTP server listens on (e.g. "127.0.0.1:9101").
	// Required; must be non-empty.
	Addr string

	// DataDir is the directory where this node's Engine files are stored.
	// Required; will be created if it does not exist.
	DataDir string

	// WALSyncOnWrite enables fsync after every WAL append.
	// Increases durability at the cost of write throughput.
	WALSyncOnWrite bool

	// MemTableMaxBytes is the MemTable size threshold that triggers flush.
	// Zero means the engine default (64 MiB).
	MemTableMaxBytes uint64

	// Replication configures optional networked replication.
	// Zero value means standalone mode.
	Replication ReplicationOptions
}

// Status is returned by GET /status and the Server.Status() method.
type Status struct {
	NodeID      string                `json:"node_id"`
	Addr        string                `json:"addr"`
	DataDir     string                `json:"data_dir"`
	StartedAt   time.Time             `json:"started_at"`
	Engine      EngineStatus          `json:"engine"`
	Replication replnet.ReplicaStatus `json:"replication,omitempty"`
}

// EngineStatus holds a point-in-time snapshot of the local engine counters.
type EngineStatus struct {
	MemTableEntries     int    `json:"memtable_entries"`
	MemTableApproxBytes uint64 `json:"memtable_approx_bytes"`
	SSTableCount        int    `json:"sstable_count"`
	NextSeq             uint64 `json:"next_seq"`
	FlushCount          uint64 `json:"flush_count"`
	CompactionCount     uint64 `json:"compaction_count"`
}

// Entry is a key-value pair returned by GET /scan.
type Entry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// putRequest is the JSON body for PUT /kv/{key}.
type putRequest struct {
	Value string `json:"value"`
}

// putResponse is the JSON body returned by PUT /kv/{key}.
type putResponse struct {
	OK     bool   `json:"ok"`
	NodeID string `json:"node_id"`
}

// getResponse is the JSON body returned by GET /kv/{key}.
type getResponse struct {
	Found  bool   `json:"found"`
	Key    string `json:"key"`
	Value  string `json:"value,omitempty"`
	NodeID string `json:"node_id"`
}

// deleteResponse is the JSON body returned by DELETE /kv/{key}.
type deleteResponse struct {
	OK     bool   `json:"ok"`
	NodeID string `json:"node_id"`
}

// healthResponse is the JSON body returned by GET /healthz.
type healthResponse struct {
	Status string `json:"status"`
	NodeID string `json:"node_id"`
}

// opResponse is the JSON body returned by POST /flush and POST /compact.
type opResponse struct {
	OK     bool   `json:"ok"`
	NodeID string `json:"node_id"`
	Error  string `json:"error,omitempty"`
}

// scanResponse is the JSON body returned by GET /scan.
type scanResponse struct {
	NodeID  string  `json:"node_id"`
	Entries []Entry `json:"entries"`
}

// errorResponse is returned on any handler error.
type errorResponse struct {
	Error  string `json:"error"`
	Code   string `json:"code,omitempty"`   // stable machine-readable code (Phase 28+)
	NodeID string `json:"node_id,omitempty"`
}

// HTTPStatusError is returned by Client methods when the server responds with a non-2xx status.
// The Code field is a stable machine-readable identifier corresponding to the server's "code"
// JSON field. Use errors.Is to map against package-level sentinel errors.
type HTTPStatusError struct {
	StatusCode int    // HTTP status code (e.g. 409)
	Code       string // stable machine-readable code (e.g. "node_quiesced")
	Message    string // human-readable error message from server
}

func (e *HTTPStatusError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("HTTP %d (%s): %s", e.StatusCode, e.Code, e.Message)
	}
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Message)
}

// Is reports whether this error matches a well-known sentinel.
// Mapping of server code values to package-level sentinel errors:
//
//	"node_quiesced"               → ErrNodeQuiesced
//	"sync_in_progress"            → ErrSyncInProgress
//	"promotion_sequence_mismatch" → ErrPromotionSequenceMismatch
//	"promotion_source_mismatch"   → ErrPromotionSourceMismatch
//	"promotion_record_invalid"    → ErrPromotionRecordInvalid
//	"already_promoted"            → ErrAlreadyPromoted
//	"promotion_in_progress"       → ErrPromotionInProgress
//	"promotion_not_ready"         → ErrPromotionNotReady
//	"quiesce_failed_fenced"       → ErrQuiesceFailedFenced
func (e *HTTPStatusError) Is(target error) bool {
	switch e.Code {
	case "node_quiesced":
		return target == ErrNodeQuiesced
	case "sync_in_progress":
		return target == ErrSyncInProgress
	case "promotion_sequence_mismatch":
		return target == ErrPromotionSequenceMismatch
	case "promotion_source_mismatch":
		return target == ErrPromotionSourceMismatch
	case "promotion_record_invalid":
		return target == ErrPromotionRecordInvalid
	case "already_promoted":
		return target == ErrAlreadyPromoted
	case "promotion_in_progress":
		return target == ErrPromotionInProgress
	case "promotion_not_ready":
		return target == ErrPromotionNotReady
	case "quiesce_failed_fenced":
		return target == ErrQuiesceFailedFenced
	}
	return false
}

// explainPutRequest is the JSON body for POST /explain/put.
type explainPutRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// SyncResult is returned by SyncFromPrimary and POST /replication/sync.
// It reports how many entries were fetched from the primary and how many
// were newly applied (skipping already-applied entries).
//
// Scope: explicit pull-based replication only. No quorum, no Raft.
type SyncResult struct {
	// SourceNode is the HTTP base URL of the primary this follower pulled from.
	SourceNode string `json:"source_node"`
	// FollowerNode is the node_id of this follower.
	FollowerNode string `json:"follower_node"`
	// Fetched is the total number of entries returned by the primary.
	Fetched int `json:"fetched"`
	// Applied is the number of entries newly written to the local engine.
	// Applied <= Fetched. Already-applied entries are skipped (idempotent).
	Applied int `json:"applied"`
	// LastAppliedSeq is the follower's replication cursor after this sync.
	LastAppliedSeq uint64 `json:"last_applied_seq"`
	// PrimaryLatestSeq is the primary's highest sequence number at the time of pull.
	// Used for operation-count lag computation. Zero if the primary has no entries.
	PrimaryLatestSeq uint64 `json:"primary_latest_seq,omitempty"`
	// LagEntriesAfterSync is the operation-count lag after this sync completes.
	// lag = primary_latest_seq - last_applied_seq, clamped to >= 0.
	// Only meaningful when LagKnown is true.
	LagEntriesAfterSync int64 `json:"lag_entries_after_sync,omitempty"`
	// LagKnown reports whether LagEntriesAfterSync is a valid value.
	// False when the primary did not report a latest sequence.
	LagKnown bool `json:"lag_known"`
	// BackgroundSyncEnabled reports whether background sync is configured on this follower.
	BackgroundSyncEnabled bool `json:"background_sync_enabled"`
	// Replication is the full replica status snapshot after the sync.
	Replication replnet.ReplicaStatus `json:"replication"`
	// Gap is non-nil when the primary returned HTTP 409 (replication gap).
	// The follower's cursor is behind the primary's earliest retained entry.
	Gap *replnet.ReplicationGapError `json:"gap,omitempty"`
}

// ReplicationStatusResponse is returned by GET /replication/status and
// Client.ReplicationStatus(). It gives callers a typed view of the replication,
// background-sync, and Phase 28 promotion/quiesce state.
type ReplicationStatusResponse struct {
	NodeID         string                `json:"node_id"`
	Replication    replnet.ReplicaStatus `json:"replication"`
	BackgroundSync BackgroundSyncStatus  `json:"background_sync"`

	// Phase 28 fields.
	RuntimeRole     string `json:"runtime_role"`
	LocalRoleSource string `json:"local_role_source"`

	// Write-gate state (primary and standalone only; empty for followers).
	// Values: "active", "quiesce_failed_fenced", "quiesced".
	WriteState   string `json:"write_state,omitempty"`
	Quiesced     bool   `json:"quiesced,omitempty"`
	QuiesceState string `json:"quiesce_state,omitempty"`

	// Quiesce record fields (non-empty when quiesced or quiesce_failed_fenced).
	QuiesceID         string `json:"quiesce_id,omitempty"`
	QuiescedAt        string `json:"quiesced_at,omitempty"`
	QuiescedLatestSeq uint64 `json:"quiesced_latest_seq,omitempty"`

	// Promotion state (follower only, or "promoted" after promotion).
	PromotionState string `json:"promotion_state,omitempty"`

	// Promotion detail fields (populated after promotion from follower → primary).
	PromotionSourceNodeID    string `json:"promotion_source_node_id,omitempty"`
	PromotionSourceBaseURL   string `json:"promotion_source_base_url,omitempty"`
	InheritedLastSeq         uint64 `json:"inherited_last_seq,omitempty"`
	PromotedAt               string `json:"promoted_at,omitempty"`
	PromotionDurableCommitted bool   `json:"promotion_durable_committed,omitempty"`

	// Pending quiesce detail (non-empty when quiesce_state == "quiesce_failed_fenced").
	PendingQuiesceID  string `json:"pending_quiesce_id,omitempty"`
	PendingQuiesceSeq uint64 `json:"pending_quiesce_seq,omitempty"`

	// QuiesceIntentState indicates that a quiesce intent record is durably written but the
	// final QuiesceRecord has not been committed yet. Values: "" (none) or "active".
	QuiesceIntentState string `json:"quiesce_intent_state,omitempty"`
}

// ExplainPutResponse is the JSON body returned by POST /explain/put.
type ExplainPutResponse struct {
	NodeID    string       `json:"node_id"`
	Operation string       `json:"operation"`
	Trace     *trace.Trace `json:"trace"`
	Error     string       `json:"error,omitempty"`
}

// ExplainGetResponse is the JSON body returned by GET /explain/get?key=.
type ExplainGetResponse struct {
	NodeID    string       `json:"node_id"`
	Operation string       `json:"operation"`
	Key       string       `json:"key"`
	Found     bool         `json:"found"`
	Value     string       `json:"value,omitempty"`
	Trace     *trace.Trace `json:"trace"`
	Error     string       `json:"error,omitempty"`
}

// ExplainDeleteResponse is the JSON body returned by DELETE /explain/delete?key=.
type ExplainDeleteResponse struct {
	NodeID    string       `json:"node_id"`
	Operation string       `json:"operation"`
	Key       string       `json:"key"`
	Trace     *trace.Trace `json:"trace"`
	Error     string       `json:"error,omitempty"`
}

// ExplainScanResponse is the JSON body returned by GET /explain/scan?start=&end=.
type ExplainScanResponse struct {
	NodeID      string       `json:"node_id"`
	Operation   string       `json:"operation"`
	ResultCount int          `json:"result_count"`
	Trace       *trace.Trace `json:"trace"`
	Error       string       `json:"error,omitempty"`
}

// ── Phase 28: Manual promotion and controlled failover types ─────────────────

// QuiesceResponse is returned by POST /replication/quiesce.
type QuiesceResponse struct {
	NodeID           string `json:"node_id"`
	WriteState       string `json:"write_state"` // "quiesced"
	QuiesceID        string `json:"quiesce_id"`
	PrimaryLatestSeq uint64 `json:"primary_latest_seq"`
	QuiescedAt       string `json:"quiesced_at"`
	Idempotent       bool   `json:"idempotent"` // true if already quiesced
}

// PromoteRequest is the body of POST /replication/promote.
type PromoteRequest struct {
	QuiesceRecord            replnet.QuiesceRecord `json:"quiesce_record"`
	ConfirmOldPrimaryStopped bool                  `json:"confirm_old_primary_stopped"`
}

// PromoteResponse is returned by POST /replication/promote.
type PromoteResponse struct {
	NodeID           string `json:"node_id"`
	NewRole          string `json:"new_role"` // "primary"
	QuiesceID        string `json:"quiesce_id"`
	InheritedLastSeq uint64 `json:"inherited_last_seq"`
	PromotedAt       string `json:"promoted_at"`
	Idempotent       bool   `json:"idempotent"`
}
