package node

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/YashPatel2395/ShardForgeDB/internal/trace"
)

// hasStepType returns true if any step in tr has the given step type.
func hasStepType(tr *trace.Trace, st trace.StepType) bool {
	for _, s := range tr.Steps {
		if s.StepType == st {
			return true
		}
	}
	return false
}

// hasAnyStepType returns true if any step in tr has one of the given step types.
func hasAnyStepType(tr *trace.Trace, types ...trace.StepType) bool {
	for _, want := range types {
		if hasStepType(tr, want) {
			return true
		}
	}
	return false
}

// --- /explain/put ---

func TestExplainPut_StepTypes(t *testing.T) {
	s := newTestServer(t, "node-1")
	body := `{"key":"hello","value":"world"}`
	w := doRequest(t, s, http.MethodPost, "/explain/put", body)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp ExplainPutResponse
	decodeJSON(t, w, &resp)
	if resp.NodeID != "node-1" {
		t.Errorf("want node_id=node-1, got %q", resp.NodeID)
	}
	if resp.Operation != "PUT" {
		t.Errorf("want operation=PUT, got %q", resp.Operation)
	}
	if resp.Trace == nil {
		t.Fatal("want non-nil trace")
	}
	if !hasStepType(resp.Trace, trace.StepTypeWALAppend) {
		t.Error("want WAL_APPEND step in put trace")
	}
	if !hasStepType(resp.Trace, trace.StepTypeMemTablePut) {
		t.Error("want MEMTABLE_PUT step in put trace")
	}
}

func TestExplainPut_MethodNotAllowed(t *testing.T) {
	s := newTestServer(t, "node-1")
	w := doRequest(t, s, http.MethodGet, "/explain/put", "")
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", w.Code)
	}
}

func TestExplainPut_InvalidJSON(t *testing.T) {
	s := newTestServer(t, "node-1")
	w := doRequest(t, s, http.MethodPost, "/explain/put", `{bad json}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestExplainPut_EmptyKey_ReturnsError(t *testing.T) {
	s := newTestServer(t, "node-1")
	w := doRequest(t, s, http.MethodPost, "/explain/put", `{"key":"","value":"v"}`)
	// engine.ExplainPut returns an error for empty key; handler returns 422 with trace
	if w.Code == http.StatusOK {
		t.Fatal("want non-200 for empty key")
	}
	var resp ExplainPutResponse
	decodeJSON(t, w, &resp)
	if resp.Error == "" {
		t.Error("want non-empty error for empty key")
	}
}

func TestExplainPut_TraceNoRawValue(t *testing.T) {
	s := newTestServer(t, "node-1")
	w := doRequest(t, s, http.MethodPost, "/explain/put", `{"key":"secret","value":"s3cr3t"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	// Raw value "s3cr3t" must not appear anywhere in the trace JSON.
	body := w.Body.String()
	var resp ExplainPutResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Trace == nil {
		t.Fatal("want trace")
	}
	for _, step := range resp.Trace.Steps {
		if step.Detail == "s3cr3t" {
			t.Error("trace step detail must not contain raw value")
		}
		for _, v := range step.Metadata {
			if v == "s3cr3t" {
				t.Error("trace step metadata must not contain raw value")
			}
		}
	}
}

func TestExplainPut_FollowerRejects(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(Options{
		NodeID:  "follower-1",
		Addr:    "127.0.0.1:0",
		DataDir: dir,
		Replication: ReplicationOptions{
			Role:           "follower",
			PrimaryBaseURL: "http://127.0.0.1:9999",
		},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	w := doRequest(t, s, http.MethodPost, "/explain/put", `{"key":"k","value":"v"}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", w.Code)
	}
}

func TestExplainPut_TraceIsValidJSON(t *testing.T) {
	s := newTestServer(t, "node-1")
	w := doRequest(t, s, http.MethodPost, "/explain/put", `{"key":"jsonkey","value":"jsonval"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if _, ok := raw["trace"]; !ok {
		t.Error("response missing trace field")
	}
}

// --- /explain/get ---

func TestExplainGet_MemTableHit(t *testing.T) {
	s := newTestServer(t, "node-1")
	doRequest(t, s, http.MethodPut, "/kv/mykey", `{"value":"myvalue"}`)

	w := doRequest(t, s, http.MethodGet, "/explain/get?key=mykey", "")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp ExplainGetResponse
	decodeJSON(t, w, &resp)
	if resp.NodeID != "node-1" {
		t.Errorf("want node_id=node-1, got %q", resp.NodeID)
	}
	if resp.Operation != "GET" {
		t.Errorf("want operation=GET, got %q", resp.Operation)
	}
	if !resp.Found {
		t.Error("want found=true for existing key")
	}
	if resp.Value != "myvalue" {
		t.Errorf("want value=myvalue, got %q", resp.Value)
	}
	if resp.Trace == nil {
		t.Fatal("want non-nil trace")
	}
	if !hasStepType(resp.Trace, trace.StepTypeMemTableHit) {
		t.Error("want MEMTABLE_HIT step in get-after-put trace")
	}
}

func TestExplainGet_NotFound_HasNotFoundStep(t *testing.T) {
	s := newTestServer(t, "node-1")
	w := doRequest(t, s, http.MethodGet, "/explain/get?key=noexist", "")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp ExplainGetResponse
	decodeJSON(t, w, &resp)
	if resp.Found {
		t.Error("want found=false for absent key")
	}
	if resp.Trace == nil {
		t.Fatal("want non-nil trace")
	}
	if !hasStepType(resp.Trace, trace.StepTypeNotFound) {
		t.Error("want NOT_FOUND step in get-miss trace")
	}
}

func TestExplainGet_AfterFlush_SSTableAndBloomSteps(t *testing.T) {
	s := newTestServer(t, "node-1")
	// Put then flush so key ends up in an SSTable
	doRequest(t, s, http.MethodPut, "/kv/flushkey", `{"value":"flushval"}`)
	flushW := doRequest(t, s, http.MethodPost, "/flush", "")
	if flushW.Code != http.StatusOK {
		t.Fatalf("flush: want 200, got %d: %s", flushW.Code, flushW.Body.String())
	}

	w := doRequest(t, s, http.MethodGet, "/explain/get?key=flushkey", "")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp ExplainGetResponse
	decodeJSON(t, w, &resp)
	if !resp.Found {
		t.Error("want found=true after flush")
	}
	if resp.Trace == nil {
		t.Fatal("want non-nil trace")
	}
	// After flush the key is in an SSTable; expect bloom and sstable steps.
	hasBloom := hasAnyStepType(resp.Trace, trace.StepTypeBloomCheck, trace.StepTypeBloomSkip, trace.StepTypeBoundsSkip)
	hasSSTable := hasAnyStepType(resp.Trace, trace.StepTypeSSTableHit, trace.StepTypeSSTableMiss)
	if !hasBloom && !hasSSTable {
		t.Errorf("want BLOOM or SSTABLE steps after flush; got steps: %v", stepNames(resp.Trace))
	}
}

func TestExplainGet_EmptyKey(t *testing.T) {
	s := newTestServer(t, "node-1")
	w := doRequest(t, s, http.MethodGet, "/explain/get", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestExplainGet_MethodNotAllowed(t *testing.T) {
	s := newTestServer(t, "node-1")
	w := doRequest(t, s, http.MethodPost, "/explain/get?key=k", "")
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", w.Code)
	}
}

// --- /explain/delete ---

func TestExplainDelete_StepTypes(t *testing.T) {
	s := newTestServer(t, "node-1")
	doRequest(t, s, http.MethodPut, "/kv/todelete", `{"value":"v"}`)

	w := doRequest(t, s, http.MethodDelete, "/explain/delete?key=todelete", "")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp ExplainDeleteResponse
	decodeJSON(t, w, &resp)
	if resp.NodeID != "node-1" {
		t.Errorf("want node_id=node-1, got %q", resp.NodeID)
	}
	if resp.Operation != "DELETE" {
		t.Errorf("want operation=DELETE, got %q", resp.Operation)
	}
	if resp.Trace == nil {
		t.Fatal("want non-nil trace")
	}
	if !hasStepType(resp.Trace, trace.StepTypeWALAppend) {
		t.Error("want WAL_APPEND step in delete trace")
	}
	if !hasStepType(resp.Trace, trace.StepTypeMemTableDelete) {
		t.Error("want MEMTABLE_DELETE step in delete trace")
	}
}

func TestExplainDelete_EmptyKey(t *testing.T) {
	s := newTestServer(t, "node-1")
	w := doRequest(t, s, http.MethodDelete, "/explain/delete", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestExplainDelete_MethodNotAllowed(t *testing.T) {
	s := newTestServer(t, "node-1")
	w := doRequest(t, s, http.MethodGet, "/explain/delete?key=k", "")
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", w.Code)
	}
}

func TestExplainDelete_FollowerRejects(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(Options{
		NodeID:  "follower-1",
		Addr:    "127.0.0.1:0",
		DataDir: dir,
		Replication: ReplicationOptions{
			Role:           "follower",
			PrimaryBaseURL: "http://127.0.0.1:9999",
		},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	w := doRequest(t, s, http.MethodDelete, "/explain/delete?key=k", "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", w.Code)
	}
}

// --- /explain/scan ---

func TestExplainScan_StepTypes(t *testing.T) {
	s := newTestServer(t, "node-1")
	doRequest(t, s, http.MethodPut, "/kv/a", `{"value":"1"}`)
	doRequest(t, s, http.MethodPut, "/kv/b", `{"value":"2"}`)

	w := doRequest(t, s, http.MethodGet, "/explain/scan?start=a&end=z", "")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp ExplainScanResponse
	decodeJSON(t, w, &resp)
	if resp.NodeID != "node-1" {
		t.Errorf("want node_id=node-1, got %q", resp.NodeID)
	}
	if resp.Operation != "SCAN" {
		t.Errorf("want operation=SCAN, got %q", resp.Operation)
	}
	if resp.ResultCount != 2 {
		t.Errorf("want result_count=2, got %d", resp.ResultCount)
	}
	if resp.Trace == nil {
		t.Fatal("want non-nil trace")
	}
	if !hasStepType(resp.Trace, trace.StepTypeScanSource) {
		t.Error("want SCAN_SOURCE step in scan trace")
	}
	if !hasStepType(resp.Trace, trace.StepTypeScanMerge) {
		t.Error("want SCAN_MERGE step in scan trace")
	}
}

func TestExplainScan_MethodNotAllowed(t *testing.T) {
	s := newTestServer(t, "node-1")
	w := doRequest(t, s, http.MethodPost, "/explain/scan", "")
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", w.Code)
	}
}

// --- Client tests ---

func TestClientExplainPut_StepTypes(t *testing.T) {
	s := newTestServer(t, "node-1")
	if err := s.StartBackground(); err != nil {
		t.Fatalf("StartBackground: %v", err)
	}
	c := NewClient("http://"+s.Addr(), 5*time.Second)
	ctx := context.Background()

	resp, err := c.ExplainPut(ctx, []byte("ckey"), []byte("cval"))
	if err != nil {
		t.Fatalf("ExplainPut: %v", err)
	}
	if resp.Trace == nil {
		t.Fatal("want non-nil trace")
	}
	if !hasStepType(resp.Trace, trace.StepTypeWALAppend) {
		t.Error("want WAL_APPEND step")
	}
	if !hasStepType(resp.Trace, trace.StepTypeMemTablePut) {
		t.Error("want MEMTABLE_PUT step")
	}
}

func TestClientExplainGet_Found(t *testing.T) {
	s := newTestServer(t, "node-1")
	if err := s.StartBackground(); err != nil {
		t.Fatalf("StartBackground: %v", err)
	}
	c := NewClient("http://"+s.Addr(), 5*time.Second)
	ctx := context.Background()

	if err := c.Put(ctx, []byte("gkey"), []byte("gval")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	resp, err := c.ExplainGet(ctx, []byte("gkey"))
	if err != nil {
		t.Fatalf("ExplainGet: %v", err)
	}
	if !resp.Found {
		t.Error("want found=true")
	}
	if resp.Trace == nil {
		t.Fatal("want non-nil trace")
	}
	if !hasStepType(resp.Trace, trace.StepTypeMemTableHit) {
		t.Error("want MEMTABLE_HIT step")
	}
}

func TestClientExplainGet_NotFound(t *testing.T) {
	s := newTestServer(t, "node-1")
	if err := s.StartBackground(); err != nil {
		t.Fatalf("StartBackground: %v", err)
	}
	c := NewClient("http://"+s.Addr(), 5*time.Second)
	ctx := context.Background()

	resp, err := c.ExplainGet(ctx, []byte("no-such-key"))
	if err != nil {
		t.Fatalf("ExplainGet: %v", err)
	}
	if resp.Found {
		t.Error("want found=false")
	}
	if !hasStepType(resp.Trace, trace.StepTypeNotFound) {
		t.Error("want NOT_FOUND step for missing key")
	}
}

func TestClientExplainDelete(t *testing.T) {
	s := newTestServer(t, "node-1")
	if err := s.StartBackground(); err != nil {
		t.Fatalf("StartBackground: %v", err)
	}
	c := NewClient("http://"+s.Addr(), 5*time.Second)
	ctx := context.Background()

	if err := c.Put(ctx, []byte("dkey"), []byte("dval")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	resp, err := c.ExplainDelete(ctx, []byte("dkey"))
	if err != nil {
		t.Fatalf("ExplainDelete: %v", err)
	}
	if resp.Trace == nil {
		t.Fatal("want non-nil trace")
	}
	if !hasStepType(resp.Trace, trace.StepTypeWALAppend) {
		t.Error("want WAL_APPEND step")
	}
	if !hasStepType(resp.Trace, trace.StepTypeMemTableDelete) {
		t.Error("want MEMTABLE_DELETE step")
	}
}

func TestClientExplainScan(t *testing.T) {
	s := newTestServer(t, "node-1")
	if err := s.StartBackground(); err != nil {
		t.Fatalf("StartBackground: %v", err)
	}
	c := NewClient("http://"+s.Addr(), 5*time.Second)
	ctx := context.Background()

	for _, kv := range [][2]string{{"x1", "v1"}, {"x2", "v2"}, {"x3", "v3"}} {
		if err := c.Put(ctx, []byte(kv[0]), []byte(kv[1])); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}

	resp, err := c.ExplainScan(ctx, []byte("x"), []byte("y"))
	if err != nil {
		t.Fatalf("ExplainScan: %v", err)
	}
	if resp.ResultCount != 3 {
		t.Errorf("want result_count=3, got %d", resp.ResultCount)
	}
	if resp.Trace == nil {
		t.Fatal("want non-nil trace")
	}
	if !hasStepType(resp.Trace, trace.StepTypeScanSource) {
		t.Error("want SCAN_SOURCE step")
	}
}

func TestClientExplainGet_NodeUnavailable(t *testing.T) {
	// Pick an ephemeral port that nothing listens on.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close() // close immediately so the port is not listening

	c := NewClient("http://"+addr, 200*time.Millisecond)
	ctx := context.Background()
	_, err = c.ExplainGet(ctx, []byte("k"))
	if err == nil {
		t.Fatal("want error for unavailable node, got nil")
	}
}

// stepNames returns step type names for test failure messages.
func stepNames(tr *trace.Trace) []trace.StepType {
	types := make([]trace.StepType, len(tr.Steps))
	for i, s := range tr.Steps {
		types[i] = s.StepType
	}
	return types
}
