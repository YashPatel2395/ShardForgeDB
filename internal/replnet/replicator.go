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

// logResponse is the JSON structure returned by GET /replication/log.
type logResponse struct {
	NodeID  string  `json:"node_id"`
	After   uint64  `json:"after"`
	Count   int     `json:"count"`
	Entries []Entry `json:"entries"`
}

// gapResponse is the JSON structure returned by GET /replication/log when HTTP 409.
type gapResponse struct {
	Error string               `json:"error"`
	Gap   *ReplicationGapError `json:"gap"`
}

// PullEntries fetches up to limit entries with Seq > after from the primary's
// GET /replication/log endpoint. Returns the entries in ascending Seq order.
//
// If the primary returns HTTP 409 (replication gap), PullEntries returns a
// *ReplicationGapError which wraps ErrReplicationGap.
func (r *Replicator) PullEntries(ctx context.Context, after uint64, limit int) ([]Entry, error) {
	if limit <= 0 {
		limit = DefaultEntriesLimit
	}
	url := fmt.Sprintf("%s/replication/log?after=%d&limit=%d", r.primaryBaseURL, after, limit)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("replnet: build request: %w", err)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("replnet: pull from primary: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusConflict {
		// Replication gap: decode structured error.
		var body gapResponse
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			return nil, fmt.Errorf("%w: primary returned 409 (could not decode gap body: %v)",
				ErrReplicationGap, err)
		}
		if body.Gap != nil {
			return nil, body.Gap
		}
		return nil, fmt.Errorf("%w: %s", ErrReplicationGap, body.Error)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("replnet: primary returned HTTP %d", resp.StatusCode)
	}

	var body logResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("replnet: decode log response: %w", err)
	}
	return body.Entries, nil
}
