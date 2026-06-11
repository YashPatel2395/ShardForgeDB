package trace_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/YashPatel2395/ShardForgeDB/internal/trace"
)

// ── Construction ─────────────────────────────────────────────────────────────

func TestNew_Fields(t *testing.T) {
	tr := trace.New(trace.OpGet, "user:1")
	if tr.Operation != trace.OpGet {
		t.Fatalf("want operation GET, got %s", tr.Operation)
	}
	if tr.Key != "user:1" {
		t.Fatalf("want key user:1, got %s", tr.Key)
	}
	if tr.StartedAt.IsZero() {
		t.Fatal("StartedAt must not be zero")
	}
	if len(tr.Steps) != 0 {
		t.Fatalf("want 0 steps on new trace, got %d", len(tr.Steps))
	}
	if tr.Err != "" {
		t.Fatalf("want empty Err on new trace, got %q", tr.Err)
	}
}

func TestNewWithID_IDSet(t *testing.T) {
	tr := trace.NewWithID("abc-123", trace.OpPut, "k")
	if tr.ID != "abc-123" {
		t.Fatalf("want ID abc-123, got %q", tr.ID)
	}
}

// ── AddStep / Step ordering ───────────────────────────────────────────────────

func TestAddStep_OrderPreserved(t *testing.T) {
	tr := trace.New(trace.OpGet, "k")
	tr.AddStep(trace.TraceStep{Component: trace.ComponentWAL, StepType: trace.StepTypeWALAppend, Status: trace.StatusOK})
	tr.AddStep(trace.TraceStep{Component: trace.ComponentMemTable, StepType: trace.StepTypeMemTableHit, Status: trace.StatusOK})
	tr.AddStep(trace.TraceStep{Component: trace.ComponentSSTable, StepType: trace.StepTypeSSTableHit, Status: trace.StatusOK})

	if len(tr.Steps) != 3 {
		t.Fatalf("want 3 steps, got %d", len(tr.Steps))
	}
	if tr.Steps[0].Component != trace.ComponentWAL {
		t.Fatal("step 0 must be WAL")
	}
	if tr.Steps[1].Component != trace.ComponentMemTable {
		t.Fatal("step 1 must be MEMTABLE")
	}
	if tr.Steps[2].Component != trace.ComponentSSTable {
		t.Fatal("step 2 must be SSTABLE")
	}
}

func TestStep_ChainReturnsTrace(t *testing.T) {
	tr := trace.New(trace.OpPut, "k").
		Step(trace.ComponentWAL, trace.StepTypeWALAppend, trace.StatusOK, 5*time.Microsecond, "seq=1").
		Step(trace.ComponentMemTable, trace.StepTypeMemTablePut, trace.StatusOK, 1*time.Microsecond, "")

	if len(tr.Steps) != 2 {
		t.Fatalf("want 2 steps from chain, got %d", len(tr.Steps))
	}
}

// ── Finish / Duration ─────────────────────────────────────────────────────────

func TestFinish_TotalDuration(t *testing.T) {
	tr := trace.New(trace.OpGet, "k")
	time.Sleep(1 * time.Millisecond) // ensure measurable wall time
	tr.Finish(nil)

	d := tr.TotalDuration()
	if d <= 0 {
		t.Fatalf("TotalDuration must be positive after Finish, got %v", d)
	}
	if tr.Err != "" {
		t.Fatalf("want empty Err on nil finish, got %q", tr.Err)
	}
}

func TestFinish_ErrorRecorded(t *testing.T) {
	tr := trace.New(trace.OpGet, "k")
	tr.Finish(errors.New("engine closed"))
	if tr.Err != "engine closed" {
		t.Fatalf("want Err=engine closed, got %q", tr.Err)
	}
}

func TestTotalDuration_ZeroIfNotFinished(t *testing.T) {
	tr := trace.New(trace.OpGet, "k")
	if tr.TotalDuration() != 0 {
		t.Fatal("TotalDuration must be zero before Finish is called")
	}
}

func TestStepDurationSum_SumsAllSteps(t *testing.T) {
	tr := trace.New(trace.OpGet, "k").
		Step(trace.ComponentWAL, trace.StepTypeWALAppend, trace.StatusOK, 10*time.Microsecond, "").
		Step(trace.ComponentMemTable, trace.StepTypeMemTableHit, trace.StatusOK, 5*time.Microsecond, "").
		Step(trace.ComponentBloom, trace.StepTypeBloomSkip, trace.StatusSkipped, 2*time.Microsecond, "")

	want := 17 * time.Microsecond
	got := tr.StepDurationSum()
	if got != want {
		t.Fatalf("want step sum %v, got %v", want, got)
	}
}

func TestStepDurationSum_ZeroWithNoSteps(t *testing.T) {
	tr := trace.New(trace.OpGet, "k")
	if tr.StepDurationSum() != 0 {
		t.Fatal("step sum must be zero when no steps")
	}
}

// ── Filtering helpers ─────────────────────────────────────────────────────────

func TestStepsWithStatus_FiltersByStatus(t *testing.T) {
	tr := trace.New(trace.OpGet, "k").
		Step(trace.ComponentBloom, trace.StepTypeBloomSkip, trace.StatusSkipped, 0, "").
		Step(trace.ComponentBloom, trace.StepTypeBloomCheck, trace.StatusOK, 0, "").
		Step(trace.ComponentSSTable, trace.StepTypeSSTableHit, trace.StatusOK, 0, "")

	skipped := tr.StepsWithStatus(trace.StatusSkipped)
	if len(skipped) != 1 {
		t.Fatalf("want 1 skipped step, got %d", len(skipped))
	}
	ok := tr.StepsWithStatus(trace.StatusOK)
	if len(ok) != 2 {
		t.Fatalf("want 2 OK steps, got %d", len(ok))
	}
}

func TestStepsForComponent_FiltersByComponent(t *testing.T) {
	tr := trace.New(trace.OpGet, "k").
		Step(trace.ComponentBloom, trace.StepTypeBloomSkip, trace.StatusSkipped, 0, "sstable=1").
		Step(trace.ComponentBloom, trace.StepTypeBloomCheck, trace.StatusOK, 0, "sstable=2").
		Step(trace.ComponentSSTable, trace.StepTypeSSTableHit, trace.StatusOK, 0, "")

	blooms := tr.StepsForComponent(trace.ComponentBloom)
	if len(blooms) != 2 {
		t.Fatalf("want 2 bloom steps, got %d", len(blooms))
	}
	sstables := tr.StepsForComponent(trace.ComponentSSTable)
	if len(sstables) != 1 {
		t.Fatalf("want 1 sstable step, got %d", len(sstables))
	}
}

func TestStepsWithStatus_EmptyWhenNoMatch(t *testing.T) {
	tr := trace.New(trace.OpGet, "k").
		Step(trace.ComponentMemTable, trace.StepTypeMemTableMiss, trace.StatusOK, 0, "")
	if len(tr.StepsWithStatus(trace.StatusError)) != 0 {
		t.Fatal("want 0 error steps")
	}
}

// ── JSON marshal / unmarshal ──────────────────────────────────────────────────

func TestMarshalJSON_RoundTrip(t *testing.T) {
	tr := trace.NewWithID("tid-1", trace.OpVectorSearch, "ns:query")
	tr.Step(trace.ComponentVector, trace.StepTypeVectorLoad, trace.StatusOK, 10*time.Microsecond, "count=100")
	tr.Step(trace.ComponentVector, trace.StepTypeVectorCompute, trace.StatusOK, 500*time.Microsecond, "metric=cosine")
	tr.Step(trace.ComponentVector, trace.StepTypeVectorTopK, trace.StatusOK, 2*time.Microsecond, "k=5")
	tr.Finish(nil)

	b, err := tr.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}

	// Unmarshal into a generic map to verify key presence
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	fields := []string{"id", "operation", "key", "started_at", "finished_at", "total_duration_ns", "step_sum_ns", "steps"}
	for _, f := range fields {
		if _, ok := m[f]; !ok {
			t.Errorf("JSON missing field %q", f)
		}
	}
	if m["operation"] != string(trace.OpVectorSearch) {
		t.Errorf("want operation VECTOR_SEARCH, got %v", m["operation"])
	}
	if m["id"] != "tid-1" {
		t.Errorf("want id tid-1, got %v", m["id"])
	}

	steps, ok := m["steps"].([]interface{})
	if !ok {
		t.Fatal("steps must be a JSON array")
	}
	if len(steps) != 3 {
		t.Fatalf("want 3 steps in JSON, got %d", len(steps))
	}
}

func TestMarshalJSON_ErrorField(t *testing.T) {
	tr := trace.New(trace.OpGet, "k")
	tr.Finish(errors.New("not found"))

	b, err := tr.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if m["error"] != "not found" {
		t.Errorf("want error=not found, got %v", m["error"])
	}
}

func TestMarshalJSON_TotalDurationPositive(t *testing.T) {
	tr := trace.New(trace.OpPut, "k")
	time.Sleep(1 * time.Millisecond)
	tr.Finish(nil)

	b, err := tr.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	var m map[string]interface{}
	_ = json.Unmarshal(b, &m)

	totalNs, ok := m["total_duration_ns"].(float64)
	if !ok {
		t.Fatal("total_duration_ns must be a number")
	}
	if totalNs <= 0 {
		t.Fatalf("total_duration_ns must be positive, got %v", totalNs)
	}
}

func TestMarshalJSON_FinishedAtOmittedWhenZero(t *testing.T) {
	tr := trace.New(trace.OpGet, "k")
	// Do NOT call Finish

	b, err := tr.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	var m map[string]interface{}
	_ = json.Unmarshal(b, &m)

	if _, ok := m["finished_at"]; ok {
		t.Error("finished_at must be omitted when trace is not finished")
	}
}

// ── Metadata ──────────────────────────────────────────────────────────────────

func TestTraceStep_MetadataPreserved(t *testing.T) {
	step := trace.TraceStep{
		Component: trace.ComponentBloom,
		StepType:  trace.StepTypeBloomCheck,
		Status:    trace.StatusOK,
		Metadata: map[string]string{
			"sstable":  "000003.sst",
			"fpr":      "0.01",
			"bit_size": "9586",
		},
	}
	tr := trace.New(trace.OpGet, "k")
	tr.AddStep(step)

	if tr.Steps[0].Metadata["sstable"] != "000003.sst" {
		t.Fatal("metadata sstable not preserved")
	}
	if tr.Steps[0].Metadata["fpr"] != "0.01" {
		t.Fatal("metadata fpr not preserved")
	}
}

// ── String ────────────────────────────────────────────────────────────────────

func TestString_ContainsKeyFields(t *testing.T) {
	tr := trace.New(trace.OpDelete, "user:42")
	tr.Step(trace.ComponentEngine, trace.StepTypeMemTableDelete, trace.StatusOK, 0, "")
	tr.Finish(nil)

	s := tr.String()
	for _, want := range []string{"DELETE", "user:42", "steps=1", "OK"} {
		if len(s) == 0 {
			t.Fatal("String must be non-empty")
		}
		found := false
		for i := 0; i <= len(s)-len(want); i++ {
			if s[i:i+len(want)] == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("String() %q does not contain %q", s, want)
		}
	}
}

func TestString_ErrorShown(t *testing.T) {
	tr := trace.New(trace.OpGet, "k")
	tr.Finish(errors.New("disk full"))
	s := tr.String()
	if len(s) == 0 {
		t.Fatal("String must be non-empty")
	}
	// Check that the error appears somewhere in the string
	found := false
	target := "disk full"
	for i := 0; i <= len(s)-len(target); i++ {
		if s[i:i+len(target)] == target {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("String() %q does not contain error text", s)
	}
}

// ── All operation types and components are valid constants ────────────────────

func TestOperationTypes_AllDefined(t *testing.T) {
	ops := []trace.OperationType{
		trace.OpGet, trace.OpPut, trace.OpDelete, trace.OpScan,
		trace.OpVectorSearch, trace.OpVectorInsert, trace.OpRoute,
		trace.OpReplicate, trace.OpFlush, trace.OpCompact,
	}
	for _, op := range ops {
		if op == "" {
			t.Error("operation type must not be empty")
		}
	}
}

func TestComponents_AllDefined(t *testing.T) {
	components := []trace.Component{
		trace.ComponentCLI, trace.ComponentRouter, trace.ComponentNode,
		trace.ComponentEngine, trace.ComponentWAL, trace.ComponentMemTable,
		trace.ComponentSSTable, trace.ComponentBloom, trace.ComponentVector,
		trace.ComponentShard, trace.ComponentReplica, trace.ComponentNetwork,
	}
	for _, c := range components {
		if c == "" {
			t.Error("component must not be empty")
		}
	}
}

func TestStepTypes_AllDefined(t *testing.T) {
	types := []trace.StepType{
		trace.StepTypeMemTableHit, trace.StepTypeMemTableMiss,
		trace.StepTypeBloomSkip, trace.StepTypeBloomCheck,
		trace.StepTypeSSTableHit, trace.StepTypeSSTableMiss,
		trace.StepTypeNotFound, trace.StepTypeWALAppend,
		trace.StepTypeMemTablePut, trace.StepTypeMemTableDelete,
		trace.StepTypeFlushStart, trace.StepTypeFlushComplete,
		trace.StepTypeCompactStart, trace.StepTypeCompactComplete,
		trace.StepTypeVectorLoad, trace.StepTypeVectorCompute,
		trace.StepTypeVectorTopK, trace.StepTypeRingLookup,
		trace.StepTypeNodeCall, trace.StepTypeReplicateAppend,
		trace.StepTypeReplicatePull, trace.StepTypeReplicateApply,
	}
	for _, st := range types {
		if st == "" {
			t.Error("step type must not be empty")
		}
	}
}
