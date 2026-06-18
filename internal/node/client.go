package node

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Client is an HTTP/JSON client for a single ShardForgeDB node.
// All network operations go through HTTP; the Client never calls Engine methods directly.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

// NewClient creates a Client targeting baseURL with the given request timeout.
func NewClient(baseURL string, timeout time.Duration) *Client {
	return &Client{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// Health checks GET /healthz. Returns nil if the node reports status "ok".
func (c *Client) Health(ctx context.Context) error {
	var resp healthResponse
	if err := c.doJSON(ctx, http.MethodGet, "/healthz", nil, &resp); err != nil {
		return fmt.Errorf("node client: health: %w", err)
	}
	if resp.Status != "ok" {
		return fmt.Errorf("node client: health: unexpected status %q", resp.Status)
	}
	return nil
}

// Status calls GET /status and returns the node's Status snapshot.
func (c *Client) Status(ctx context.Context) (Status, error) {
	var resp Status
	if err := c.doJSON(ctx, http.MethodGet, "/status", nil, &resp); err != nil {
		return Status{}, fmt.Errorf("node client: status: %w", err)
	}
	return resp, nil
}

// Put calls PUT /kv/{key} with the given value.
func (c *Client) Put(ctx context.Context, key, value []byte) error {
	body := putRequest{Value: string(value)}
	var resp putResponse
	if err := c.doJSON(ctx, http.MethodPut, "/kv/"+url.PathEscape(string(key)), body, &resp); err != nil {
		return fmt.Errorf("node client: put %q: %w", key, err)
	}
	if !resp.OK {
		return fmt.Errorf("node client: put %q: server returned ok=false", key)
	}
	return nil
}

// Get calls GET /kv/{key}. Returns (value, true, nil) if found, ("", false, nil) if not found.
func (c *Client) Get(ctx context.Context, key []byte) ([]byte, bool, error) {
	var resp getResponse
	if err := c.doJSON(ctx, http.MethodGet, "/kv/"+url.PathEscape(string(key)), nil, &resp); err != nil {
		return nil, false, fmt.Errorf("node client: get %q: %w", key, err)
	}
	if !resp.Found {
		return nil, false, nil
	}
	return []byte(resp.Value), true, nil
}

// Delete calls DELETE /kv/{key}.
func (c *Client) Delete(ctx context.Context, key []byte) error {
	var resp deleteResponse
	if err := c.doJSON(ctx, http.MethodDelete, "/kv/"+url.PathEscape(string(key)), nil, &resp); err != nil {
		return fmt.Errorf("node client: delete %q: %w", key, err)
	}
	if !resp.OK {
		return fmt.Errorf("node client: delete %q: server returned ok=false", key)
	}
	return nil
}

// Scan calls GET /scan?start=<start>&end=<end> and returns the entries in sorted order.
func (c *Client) Scan(ctx context.Context, start, end []byte) ([]Entry, error) {
	q := url.Values{}
	q.Set("start", string(start))
	q.Set("end", string(end))
	var resp scanResponse
	if err := c.doJSON(ctx, http.MethodGet, "/scan?"+q.Encode(), nil, &resp); err != nil {
		return nil, fmt.Errorf("node client: scan: %w", err)
	}
	return resp.Entries, nil
}

// Flush calls POST /flush.
func (c *Client) Flush(ctx context.Context) error {
	var resp opResponse
	if err := c.doJSON(ctx, http.MethodPost, "/flush", nil, &resp); err != nil {
		return fmt.Errorf("node client: flush: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("node client: flush: server error: %s", resp.Error)
	}
	return nil
}

// Compact calls POST /compact.
func (c *Client) Compact(ctx context.Context) error {
	var resp opResponse
	if err := c.doJSON(ctx, http.MethodPost, "/compact", nil, &resp); err != nil {
		return fmt.Errorf("node client: compact: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("node client: compact: server error: %s", resp.Error)
	}
	return nil
}

// ReplicationStatus calls GET /replication/status and returns a typed snapshot of
// the node's replication and background-sync state.
// Prefer this over Client.Do + map[string]any when inspecting worker state, lag, or counters.
func (c *Client) ReplicationStatus(ctx context.Context) (ReplicationStatusResponse, error) {
	var resp ReplicationStatusResponse
	if err := c.doJSON(ctx, http.MethodGet, "/replication/status", nil, &resp); err != nil {
		return ReplicationStatusResponse{}, fmt.Errorf("node client: replication status: %w", err)
	}
	return resp, nil
}

// SyncReplication calls POST /replication/sync on a follower node, triggering an
// explicit pull from the configured primary. Returns the SyncResult with fetched,
// applied, and last_applied_seq counts.
//
// If the server returns HTTP 409 with code "sync_in_progress" (a concurrent sync is
// already in flight), the returned error wraps ErrSyncInProgress so callers can use
// errors.Is(err, ErrSyncInProgress). A 409 replication-gap response does NOT match
// ErrSyncInProgress.
//
// Scope: explicit operator-triggered pull only. No background loop, no Raft, no quorum.
func (c *Client) SyncReplication(ctx context.Context) (*SyncResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/replication/sync", nil)
	if err != nil {
		return nil, fmt.Errorf("node client: sync replication: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("node client: sync replication: %w", ctx.Err())
		}
		return nil, fmt.Errorf("node client: sync replication: node unavailable: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("node client: sync replication: read body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Decode with code field for typed error handling.
		var errResp errorResponse
		if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
			httpErr := &HTTPStatusError{
				StatusCode: resp.StatusCode,
				Code:       errResp.Code,
				Message:    errResp.Error,
			}
			return nil, fmt.Errorf("node client: sync replication: %w", httpErr)
		}
		return nil, fmt.Errorf("node client: sync replication: unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var result SyncResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("node client: sync replication: invalid JSON: %w", err)
	}
	return &result, nil
}

// ExplainPut calls POST /explain/put and returns the execution trace for a PUT.
func (c *Client) ExplainPut(ctx context.Context, key, value []byte) (*ExplainPutResponse, error) {
	body := explainPutRequest{Key: string(key), Value: string(value)}
	var resp ExplainPutResponse
	if err := c.doJSON(ctx, http.MethodPost, "/explain/put", body, &resp); err != nil {
		return nil, fmt.Errorf("node client: explain put %q: %w", key, err)
	}
	return &resp, nil
}

// ExplainGet calls GET /explain/get?key=<key> and returns the execution trace for a GET.
func (c *Client) ExplainGet(ctx context.Context, key []byte) (*ExplainGetResponse, error) {
	q := url.Values{}
	q.Set("key", string(key))
	var resp ExplainGetResponse
	if err := c.doJSON(ctx, http.MethodGet, "/explain/get?"+q.Encode(), nil, &resp); err != nil {
		return nil, fmt.Errorf("node client: explain get %q: %w", key, err)
	}
	return &resp, nil
}

// ExplainDelete calls DELETE /explain/delete?key=<key> and returns the execution trace for a DELETE.
func (c *Client) ExplainDelete(ctx context.Context, key []byte) (*ExplainDeleteResponse, error) {
	q := url.Values{}
	q.Set("key", string(key))
	var resp ExplainDeleteResponse
	if err := c.doJSON(ctx, http.MethodDelete, "/explain/delete?"+q.Encode(), nil, &resp); err != nil {
		return nil, fmt.Errorf("node client: explain delete %q: %w", key, err)
	}
	return &resp, nil
}

// ExplainScan calls GET /explain/scan?start=<start>&end=<end> and returns the execution trace for a SCAN.
func (c *Client) ExplainScan(ctx context.Context, start, end []byte) (*ExplainScanResponse, error) {
	q := url.Values{}
	q.Set("start", string(start))
	q.Set("end", string(end))
	var resp ExplainScanResponse
	if err := c.doJSON(ctx, http.MethodGet, "/explain/scan?"+q.Encode(), nil, &resp); err != nil {
		return nil, fmt.Errorf("node client: explain scan: %w", err)
	}
	return &resp, nil
}

// QuiesceReplication calls POST /replication/quiesce on a primary node.
func (c *Client) QuiesceReplication(ctx context.Context) (*QuiesceResponse, error) {
	var resp QuiesceResponse
	if err := c.doJSON(ctx, http.MethodPost, "/replication/quiesce", nil, &resp); err != nil {
		return nil, fmt.Errorf("node client: quiesce: %w", err)
	}
	return &resp, nil
}

// PromoteFollower calls POST /replication/promote on a follower node.
func (c *Client) PromoteFollower(ctx context.Context, req *PromoteRequest) (*PromoteResponse, error) {
	var resp PromoteResponse
	if err := c.doJSON(ctx, http.MethodPost, "/replication/promote", req, &resp); err != nil {
		return nil, fmt.Errorf("node client: promote: %w", err)
	}
	return &resp, nil
}

// Do executes an HTTP request with optional JSON body and decodes the response into a
// map[string]any. Useful for proxy-forwarding when the exact response shape is unknown.
// Returns an error for non-2xx status codes.
func (c *Client) Do(ctx context.Context, method, path string, body any) (map[string]any, error) {
	var out map[string]any
	if err := c.doJSON(ctx, method, path, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// doJSON executes an HTTP request with optional JSON body and decodes the JSON response into out.
// It returns clear errors for: node unavailable, timeout, invalid status code, invalid JSON,
// server-side error responses.
func (c *Client) doJSON(ctx context.Context, method, path string, body any, out any) error {
	var bodyReader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		bodyReader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		// Distinguish timeout from general unavailability.
		if ctx.Err() == context.DeadlineExceeded || ctx.Err() == context.Canceled {
			return fmt.Errorf("node unavailable (context): %w", ctx.Err())
		}
		return fmt.Errorf("node unavailable: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Try to decode a server-side error message with stable code field.
		var errResp errorResponse
		if jsonErr := json.Unmarshal(respBytes, &errResp); jsonErr == nil && errResp.Error != "" {
			return &HTTPStatusError{
				StatusCode: resp.StatusCode,
				Code:       errResp.Code,
				Message:    errResp.Error,
			}
		}
		return &HTTPStatusError{
			StatusCode: resp.StatusCode,
			Message:    string(respBytes),
		}
	}

	if out != nil {
		if err := json.Unmarshal(respBytes, out); err != nil {
			return fmt.Errorf("invalid JSON response: %w", err)
		}
	}
	return nil
}
