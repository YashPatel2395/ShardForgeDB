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
		// Try to decode a server-side error message.
		var errResp errorResponse
		if jsonErr := json.Unmarshal(respBytes, &errResp); jsonErr == nil && errResp.Error != "" {
			return fmt.Errorf("server error %d: %s", resp.StatusCode, errResp.Error)
		}
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBytes))
	}

	if out != nil {
		if err := json.Unmarshal(respBytes, out); err != nil {
			return fmt.Errorf("invalid JSON response: %w", err)
		}
	}
	return nil
}
