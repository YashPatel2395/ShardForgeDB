package replnet

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// DefaultReplicatorTimeout is the default HTTP timeout for replicator requests.
const DefaultReplicatorTimeout = 5 * time.Second

// Replicator is an HTTP client that pulls replication entries from a primary node.
//
// Scope:
//   - Pull-based only. The follower calls PullEntries; there is no push from the primary.
//   - No automatic background polling. Callers must invoke PullEntries explicitly.
type Replicator struct {
	primaryBaseURL string
	client         *http.Client
}

// NewReplicator creates a Replicator for the given primary base URL.
// If timeout <= 0, DefaultReplicatorTimeout is used.
func NewReplicator(primaryBaseURL string, timeout time.Duration) *Replicator {
	if timeout <= 0 {
		timeout = DefaultReplicatorTimeout
	}
	return &Replicator{
		primaryBaseURL: primaryBaseURL,
		client:         &http.Client{Timeout: timeout},
	}
}

// PullResult is returned by Replicator.PullEntries.
type PullResult struct {
	// Entries are the log entries returned by the primary, in ascending Seq order.
	Entries []Entry
	// PrimaryLatestSeq is the primary's highest sequence number at the time of the pull.
	// Only meaningful when PrimaryLatestSeqKnown is true.
	PrimaryLatestSeq uint64
	// PrimaryLatestSeqKnown is true when the primary included primary_latest_seq in
	// its response. When false the field was absent (e.g. Phase-26-style primary)
	// and callers must not interpret PrimaryLatestSeq=0 as "primary is empty".
	// This distinction prevents false zero-lag readings against older primaries.
	PrimaryLatestSeqKnown bool
}

// logResponse is the JSON structure returned by GET /replication/log.
// PrimaryLatestSeq is a pointer so json.Unmarshal can distinguish an explicit
// zero ("primary_latest_seq": 0) from a missing field (older primary software).
type logResponse struct {
	NodeID           string  `json:"node_id"`
	After            uint64  `json:"after"`
	Count            int     `json:"count"`
	Entries          []Entry `json:"entries"`
	PrimaryLatestSeq *uint64 `json:"primary_latest_seq"`
}

// gapResponse is the JSON structure returned by GET /replication/log when HTTP 409.
type gapResponse struct {
	Error string               `json:"error"`
	Gap   *ReplicationGapError `json:"gap"`
}

// PullEntries fetches up to limit entries with Seq > after from the primary's
// GET /replication/log endpoint. Returns a PullResult containing the entries in
// ascending Seq order and the primary's latest sequence number for lag tracking.
//
// If the primary returns HTTP 409 (replication gap), PullEntries returns a
// *ReplicationGapError which wraps ErrReplicationGap.
func (r *Replicator) PullEntries(ctx context.Context, after uint64, limit int) (PullResult, error) {
	if limit <= 0 {
		limit = DefaultEntriesLimit
	}
	url := fmt.Sprintf("%s/replication/log?after=%d&limit=%d", r.primaryBaseURL, after, limit)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return PullResult{}, fmt.Errorf("replnet: build request: %w", err)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return PullResult{}, fmt.Errorf("replnet: pull from primary: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusConflict {
		// Replication gap: decode structured error.
		var body gapResponse
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			return PullResult{}, fmt.Errorf("%w: primary returned 409 (could not decode gap body: %v)",
				ErrReplicationGap, err)
		}
		if body.Gap != nil {
			return PullResult{}, body.Gap
		}
		return PullResult{}, fmt.Errorf("%w: %s", ErrReplicationGap, body.Error)
	}

	if resp.StatusCode != http.StatusOK {
		return PullResult{}, fmt.Errorf("replnet: primary returned HTTP %d", resp.StatusCode)
	}

	var body logResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return PullResult{}, fmt.Errorf("replnet: decode log response: %w", err)
	}
	var pls uint64
	known := body.PrimaryLatestSeq != nil
	if known {
		pls = *body.PrimaryLatestSeq
	}
	return PullResult{
		Entries:               body.Entries,
		PrimaryLatestSeq:      pls,
		PrimaryLatestSeqKnown: known,
	}, nil
}
