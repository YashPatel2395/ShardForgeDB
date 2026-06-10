package node

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newTestServer opens a Server backed by a temp dir. It registers tb.Cleanup to Close.
func newTestServer(tb testing.TB, nodeID string) *Server {
	tb.Helper()
	dir := tb.TempDir()
	s, err := Open(Options{
		NodeID:  nodeID,
		Addr:    "127.0.0.1:0",
		DataDir: dir,
	})
	if err != nil {
		tb.Fatalf("Open: %v", err)
	}
	tb.Cleanup(func() { _ = s.Close() })
	return s
}

// doRequest sends a request against the server's Handler and returns the recorder.
func doRequest(tb testing.TB, s *Server, method, path, body string) *httptest.ResponseRecorder {
	tb.Helper()
	var bodyReader *strings.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	} else {
		bodyReader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, bodyReader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	return w
}

// decodeJSON decodes the response body into v.
func decodeJSON(tb testing.TB, w *httptest.ResponseRecorder, v any) {
	tb.Helper()
	if err := json.NewDecoder(w.Body).Decode(v); err != nil {
		tb.Fatalf("decode response JSON: %v (body: %s)", err, w.Body.String())
	}
}

// --- Option validation tests ---

func TestOpen_ValidOptions(t *testing.T) {
	s := newTestServer(t, "node-1")
	if s == nil {
		t.Fatal("expected non-nil Server")
	}
}

func TestOpen_MissingNodeID(t *testing.T) {
	_, err := Open(Options{
		NodeID:  "",
		Addr:    "127.0.0.1:0",
		DataDir: t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected error for missing NodeID")
	}
}

func TestOpen_MissingDataDir(t *testing.T) {
	_, err := Open(Options{
		NodeID:  "node-x",
		Addr:    "127.0.0.1:0",
		DataDir: "",
	})
	if err == nil {
		t.Fatal("expected error for missing DataDir")
	}
}

func TestOpen_MissingAddr(t *testing.T) {
	_, err := Open(Options{
		NodeID:  "node-x",
		Addr:    "",
		DataDir: t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected error for missing Addr")
	}
}

// --- /healthz ---

func TestHealthz_OK(t *testing.T) {
	s := newTestServer(t, "node-1")
	w := doRequest(t, s, http.MethodGet, "/healthz", "")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp healthResponse
	decodeJSON(t, w, &resp)
	if resp.Status != "ok" {
		t.Errorf("want status=ok, got %q", resp.Status)
	}
	if resp.NodeID != "node-1" {
		t.Errorf("want node_id=node-1, got %q", resp.NodeID)
	}
}

func TestHealthz_MethodNotAllowed(t *testing.T) {
	s := newTestServer(t, "node-1")
	w := doRequest(t, s, http.MethodPost, "/healthz", "")
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405, got %d", w.Code)
	}
}

// --- /status ---

func TestStatus_OK(t *testing.T) {
	s := newTestServer(t, "node-2")
	w := doRequest(t, s, http.MethodGet, "/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp Status
	decodeJSON(t, w, &resp)
	if resp.NodeID != "node-2" {
		t.Errorf("want node_id=node-2, got %q", resp.NodeID)
	}
	if resp.DataDir == "" {
		t.Error("want non-empty data_dir")
	}
	if resp.StartedAt.IsZero() {
		t.Error("want non-zero started_at")
	}
}

func TestStatus_NonZeroStartedAt(t *testing.T) {
	s := newTestServer(t, "node-ts")
	before := time.Now()
	st := s.Status()
	after := time.Now()
	if st.StartedAt.Before(before.Add(-time.Second)) || st.StartedAt.After(after.Add(time.Second)) {
		t.Errorf("StartedAt %v out of expected range [%v, %v]", st.StartedAt, before, after)
	}
}

func TestStatus_MethodNotAllowed(t *testing.T) {
	s := newTestServer(t, "node-1")
	w := doRequest(t, s, http.MethodDelete, "/status", "")
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405, got %d", w.Code)
	}
}

// --- /kv PUT + GET ---

func TestKV_PutGet(t *testing.T) {
	s := newTestServer(t, "node-1")

	// PUT
	w := doRequest(t, s, http.MethodPut, "/kv/hello", `{"value":"world"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("put: want 200, got %d; body: %s", w.Code, w.Body.String())
	}
	var putResp putResponse
	decodeJSON(t, w, &putResp)
	if !putResp.OK {
		t.Error("put: want ok=true")
	}

	// GET
	w = doRequest(t, s, http.MethodGet, "/kv/hello", "")
	if w.Code != http.StatusOK {
		t.Fatalf("get: want 200, got %d", w.Code)
	}
	var getResp getResponse
	decodeJSON(t, w, &getResp)
	if !getResp.Found {
		t.Fatal("get: want found=true")
	}
	if getResp.Value != "world" {
		t.Errorf("get: want value=world, got %q", getResp.Value)
	}
	if getResp.NodeID != "node-1" {
		t.Errorf("get: want node_id=node-1, got %q", getResp.NodeID)
	}
}

func TestKV_GetMissing(t *testing.T) {
	s := newTestServer(t, "node-1")
	w := doRequest(t, s, http.MethodGet, "/kv/no-such-key", "")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp getResponse
	decodeJSON(t, w, &resp)
	if resp.Found {
		t.Error("want found=false for missing key")
	}
}

// --- /kv DELETE ---

func TestKV_Delete(t *testing.T) {
	s := newTestServer(t, "node-1")

	// Insert key.
	doRequest(t, s, http.MethodPut, "/kv/todel", `{"value":"v"}`)

	// Delete it.
	w := doRequest(t, s, http.MethodDelete, "/kv/todel", "")
	if w.Code != http.StatusOK {
		t.Fatalf("delete: want 200, got %d", w.Code)
	}
	var resp deleteResponse
	decodeJSON(t, w, &resp)
	if !resp.OK {
		t.Error("delete: want ok=true")
	}

	// Confirm it is gone.
	w = doRequest(t, s, http.MethodGet, "/kv/todel", "")
	var getResp getResponse
	decodeJSON(t, w, &getResp)
	if getResp.Found {
		t.Error("expected key to be deleted")
	}
}

// --- /scan ---

func TestScan_SortedEntries(t *testing.T) {
	s := newTestServer(t, "node-1")
	keys := []string{"b", "a", "c"}
	for _, k := range keys {
		doRequest(t, s, http.MethodPut, "/kv/"+k, `{"value":"`+k+`-val"}`)
	}

	w := doRequest(t, s, http.MethodGet, "/scan?start=a&end=z", "")
	if w.Code != http.StatusOK {
		t.Fatalf("scan: want 200, got %d; body: %s", w.Code, w.Body.String())
	}
	var resp scanResponse
	decodeJSON(t, w, &resp)
	if len(resp.Entries) != 3 {
		t.Fatalf("want 3 entries, got %d", len(resp.Entries))
	}
	// Entries must be in ascending key order.
	for i := 1; i < len(resp.Entries); i++ {
		if resp.Entries[i].Key < resp.Entries[i-1].Key {
			t.Errorf("entries not sorted: %q before %q", resp.Entries[i-1].Key, resp.Entries[i].Key)
		}
	}
}

func TestScan_MethodNotAllowed(t *testing.T) {
	s := newTestServer(t, "node-1")
	w := doRequest(t, s, http.MethodPost, "/scan", "")
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405, got %d", w.Code)
	}
}

// --- /flush ---

func TestFlush_OK(t *testing.T) {
	s := newTestServer(t, "node-1")
	doRequest(t, s, http.MethodPut, "/kv/fkey", `{"value":"fval"}`)
	w := doRequest(t, s, http.MethodPost, "/flush", "")
	if w.Code != http.StatusOK {
		t.Fatalf("flush: want 200, got %d; body: %s", w.Code, w.Body.String())
	}
	var resp opResponse
	decodeJSON(t, w, &resp)
	if !resp.OK {
		t.Error("flush: want ok=true")
	}
}

func TestFlush_MethodNotAllowed(t *testing.T) {
	s := newTestServer(t, "node-1")
	w := doRequest(t, s, http.MethodGet, "/flush", "")
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405, got %d", w.Code)
	}
}

// --- /compact ---

func TestCompact_OK(t *testing.T) {
	s := newTestServer(t, "node-1")
	// flush first so there is something to compact
	doRequest(t, s, http.MethodPut, "/kv/ckey", `{"value":"cval"}`)
	doRequest(t, s, http.MethodPost, "/flush", "")
	w := doRequest(t, s, http.MethodPost, "/compact", "")
	if w.Code != http.StatusOK {
		t.Fatalf("compact: want 200, got %d; body: %s", w.Code, w.Body.String())
	}
	var resp opResponse
	decodeJSON(t, w, &resp)
	if !resp.OK {
		t.Error("compact: want ok=true")
	}
}

func TestCompact_MethodNotAllowed(t *testing.T) {
	s := newTestServer(t, "node-1")
	w := doRequest(t, s, http.MethodGet, "/compact", "")
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405, got %d", w.Code)
	}
}

// --- empty key ---

func TestKV_EmptyKeyRejected(t *testing.T) {
	s := newTestServer(t, "node-1")
	w := doRequest(t, s, http.MethodPut, "/kv/", `{"value":"v"}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400 for empty key, got %d", w.Code)
	}
}

// --- Close idempotent ---

func TestClose_Idempotent(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(Options{NodeID: "node-x", Addr: "127.0.0.1:0", DataDir: dir})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("second Close (idempotent): %v", err)
	}
}

// --- /kv method validation ---

func TestKV_WrongMethod(t *testing.T) {
	s := newTestServer(t, "node-1")
	w := doRequest(t, s, http.MethodPost, "/kv/somekey", `{"value":"v"}`)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405 for POST /kv/, got %d", w.Code)
	}
}

// --- invalid JSON body ---

func TestKV_InvalidJSONBody(t *testing.T) {
	s := newTestServer(t, "node-1")
	w := doRequest(t, s, http.MethodPut, "/kv/k", "not-json")
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400 for invalid JSON body, got %d", w.Code)
	}
}
