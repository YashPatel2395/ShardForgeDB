package vector

import (
	"errors"
	"math"
	"os"
	"testing"

	"github.com/YashPatel2395/ShardForgeDB/internal/trace"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func openExplainStore(t *testing.T) *Store {
	t.Helper()
	dir, err := os.MkdirTemp("", "vector-explain-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	st, err := Open(Options{Dir: dir, Dimension: 4, Metric: MetricCosine})
	if err != nil {
		os.RemoveAll(dir)
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
		os.RemoveAll(dir)
	})
	return st
}

func unitVec4(x, y, z, w float32) []float32 {
	v := []float32{x, y, z, w}
	var norm float64
	for _, e := range v {
		norm += float64(e) * float64(e)
	}
	norm = math.Sqrt(norm)
	for i := range v {
		v[i] = float32(float64(v[i]) / norm)
	}
	return v
}

func countVecStepType(tr *trace.Trace, st trace.StepType) int {
	n := 0
	for _, s := range tr.Steps {
		if s.StepType == st {
			n++
		}
	}
	return n
}

// ── ExplainUpsert ─────────────────────────────────────────────────────────────

func TestExplainUpsert_RecordsAllSteps(t *testing.T) {
	st := openExplainStore(t)
	vec := unitVec4(1, 0, 0, 0)

	tr, err := st.ExplainUpsert("v1", vec, nil)
	if err != nil {
		t.Fatalf("ExplainUpsert: %v", err)
	}
	if tr == nil {
		t.Fatal("nil trace")
	}

	// Must have: validate, encode, engine write, index update.
	if countVecStepType(tr, trace.StepTypeVectorValidate) < 1 {
		t.Error("missing VECTOR_VALIDATE step")
	}
	if countVecStepType(tr, trace.StepTypeVectorEncode) < 1 {
		t.Error("missing VECTOR_ENCODE step")
	}
	if countVecStepType(tr, trace.StepTypeVectorEngineWrite) < 1 {
		t.Error("missing VECTOR_ENGINE_WRITE step")
	}
	if countVecStepType(tr, trace.StepTypeVectorIndexUpdate) < 1 {
		t.Error("missing VECTOR_INDEX_UPDATE step")
	}

	if tr.Operation != trace.OpVectorInsert {
		t.Errorf("operation = %q, want VECTOR_INSERT", tr.Operation)
	}
	if tr.Err != "" {
		t.Errorf("unexpected trace error: %s", tr.Err)
	}
}

func TestExplainUpsert_InvalidID_RecordsError(t *testing.T) {
	st := openExplainStore(t)
	vec := unitVec4(1, 0, 0, 0)

	tr, err := st.ExplainUpsert("", vec, nil)
	if err == nil {
		t.Fatal("expected error for empty ID")
	}
	if !errors.Is(err, ErrInvalidID) {
		t.Errorf("error = %v, want ErrInvalidID", err)
	}
	if tr == nil || tr.Err == "" {
		t.Error("trace should record the error")
	}
	validateSteps := tr.StepsWithStatus(trace.StatusError)
	if len(validateSteps) == 0 {
		t.Error("expected at least one ERROR step")
	}
}

func TestExplainUpsert_InvalidVector_RecordsError(t *testing.T) {
	st := openExplainStore(t)
	// Wrong dimension.
	tr, err := st.ExplainUpsert("v1", []float32{1, 2}, nil)
	if err == nil {
		t.Fatal("expected error for wrong dimension")
	}
	if !errors.Is(err, ErrInvalidVector) {
		t.Errorf("error = %v, want ErrInvalidVector", err)
	}
	if tr == nil || tr.Err == "" {
		t.Error("trace should record the error")
	}
}

func TestExplainUpsert_ResultMatchesUpsert(t *testing.T) {
	st := openExplainStore(t)
	vec := unitVec4(1, 0, 0, 0)

	_, err := st.ExplainUpsert("v1", vec, []byte("meta"))
	if err != nil {
		t.Fatalf("ExplainUpsert: %v", err)
	}
	rec, ok, _ := st.Get("v1")
	if !ok {
		t.Fatal("record not found after ExplainUpsert")
	}
	if rec.ID != "v1" {
		t.Errorf("record ID = %q, want v1", rec.ID)
	}
}

// ── ExplainSearch ─────────────────────────────────────────────────────────────

func TestExplainSearch_RecordsCandidateCountAndTopK(t *testing.T) {
	st := openExplainStore(t)

	// Insert 3 vectors.
	for i, v := range [][]float32{
		unitVec4(1, 0, 0, 0),
		unitVec4(0, 1, 0, 0),
		unitVec4(0, 0, 1, 0),
	} {
		if err := st.Upsert(vecID(i), v, nil); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
	}

	query := unitVec4(1, 0, 0, 0)
	tr, results, err := st.ExplainSearch(query, 2)
	if err != nil {
		t.Fatalf("ExplainSearch: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("want 2 results, got %d", len(results))
	}

	// Must record: validate, load, compute, topk.
	if countVecStepType(tr, trace.StepTypeVectorValidate) < 1 {
		t.Error("missing VECTOR_VALIDATE")
	}
	if countVecStepType(tr, trace.StepTypeVectorLoad) < 1 {
		t.Error("missing VECTOR_LOAD")
	}
	if countVecStepType(tr, trace.StepTypeVectorCompute) < 1 {
		t.Error("missing VECTOR_COMPUTE")
	}
	if countVecStepType(tr, trace.StepTypeVectorTopK) < 1 {
		t.Error("missing VECTOR_TOPK")
	}
	if tr.Operation != trace.OpVectorSearch {
		t.Errorf("operation = %q, want VECTOR_SEARCH", tr.Operation)
	}
}

func TestExplainSearch_InvalidK_RecordsError(t *testing.T) {
	st := openExplainStore(t)
	query := unitVec4(1, 0, 0, 0)

	tr, _, err := st.ExplainSearch(query, 0)
	if err == nil {
		t.Fatal("expected error for k=0")
	}
	if !errors.Is(err, ErrInvalidK) {
		t.Errorf("error = %v, want ErrInvalidK", err)
	}
	if tr == nil || tr.Err == "" {
		t.Error("trace should record the error")
	}
}

func TestExplainSearch_WrongDimension_RecordsError(t *testing.T) {
	st := openExplainStore(t)
	query := []float32{1, 0} // dimension 2, store is 4

	tr, _, err := st.ExplainSearch(query, 1)
	if err == nil {
		t.Fatal("expected error for wrong dimension")
	}
	if !errors.Is(err, ErrInvalidVector) {
		t.Errorf("error = %v, want ErrInvalidVector", err)
	}
	if tr == nil {
		t.Fatal("nil trace on error")
	}
}

func TestExplainSearch_EmptyIndex_ReturnsEmptyResults(t *testing.T) {
	st := openExplainStore(t)
	query := unitVec4(1, 0, 0, 0)

	tr, results, err := st.ExplainSearch(query, 5)
	if err != nil {
		t.Fatalf("ExplainSearch on empty index: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("want 0 results, got %d", len(results))
	}
	// VECTOR_LOAD should record candidate_count=0.
	if countVecStepType(tr, trace.StepTypeVectorLoad) < 1 {
		t.Error("missing VECTOR_LOAD step for empty index")
	}
}

func TestExplainSearch_ResultMatchesSearch(t *testing.T) {
	st := openExplainStore(t)
	vec := unitVec4(1, 0, 0, 0)
	if err := st.Upsert("v1", vec, nil); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	query := unitVec4(1, 0, 0, 0)
	regularResults, _ := st.Search(query, 1)
	_, explainResults, _ := st.ExplainSearch(query, 1)

	if len(regularResults) != len(explainResults) {
		t.Errorf("Search=%d, ExplainSearch=%d", len(regularResults), len(explainResults))
	}
	if len(regularResults) > 0 && regularResults[0].ID != explainResults[0].ID {
		t.Errorf("top result: Search=%q ExplainSearch=%q",
			regularResults[0].ID, explainResults[0].ID)
	}
}

// ── ExplainDelete ─────────────────────────────────────────────────────────────

func TestExplainDelete_RecordsTombstoneAndIndex(t *testing.T) {
	st := openExplainStore(t)
	vec := unitVec4(1, 0, 0, 0)

	if err := st.Upsert("v1", vec, nil); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	tr, err := st.ExplainDelete("v1")
	if err != nil {
		t.Fatalf("ExplainDelete: %v", err)
	}

	if countVecStepType(tr, trace.StepTypeVectorValidate) < 1 {
		t.Error("missing VECTOR_VALIDATE step")
	}
	if countVecStepType(tr, trace.StepTypeVectorEngineWrite) < 1 {
		t.Error("missing VECTOR_ENGINE_WRITE step")
	}
	if countVecStepType(tr, trace.StepTypeVectorIndexDelete) < 1 {
		t.Error("missing VECTOR_INDEX_DELETE step")
	}
	if tr.Operation != trace.OpDelete {
		t.Errorf("operation = %q, want DELETE", tr.Operation)
	}
}

func TestExplainDelete_InvalidID_RecordsError(t *testing.T) {
	st := openExplainStore(t)

	tr, err := st.ExplainDelete("")
	if err == nil {
		t.Fatal("expected error for empty ID")
	}
	if !errors.Is(err, ErrInvalidID) {
		t.Errorf("error = %v, want ErrInvalidID", err)
	}
	if tr == nil || tr.Err == "" {
		t.Error("trace should record the error")
	}
}

func TestExplainDelete_ResultMatchesDelete(t *testing.T) {
	st := openExplainStore(t)
	vec := unitVec4(1, 0, 0, 0)

	if err := st.Upsert("v1", vec, nil); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if _, err := st.ExplainDelete("v1"); err != nil {
		t.Fatalf("ExplainDelete: %v", err)
	}
	_, ok, _ := st.Get("v1")
	if ok {
		t.Error("record should be gone after ExplainDelete")
	}
}

// ── Existing API unchanged ────────────────────────────────────────────────────

func TestExistingVectorUpsert_UnchangedByPhase22(t *testing.T) {
	st := openExplainStore(t)
	vec := unitVec4(1, 0, 0, 0)

	if err := st.Upsert("v1", vec, nil); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	_, ok, _ := st.Get("v1")
	if !ok {
		t.Error("record not found after Upsert")
	}
}

func TestExistingVectorSearch_UnchangedByPhase22(t *testing.T) {
	st := openExplainStore(t)
	vec := unitVec4(1, 0, 0, 0)

	if err := st.Upsert("v1", vec, nil); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	results, err := st.Search(unitVec4(1, 0, 0, 0), 1)
	if err != nil || len(results) != 1 {
		t.Errorf("Search: err=%v results=%d", err, len(results))
	}
}

func TestExistingVectorDelete_UnchangedByPhase22(t *testing.T) {
	st := openExplainStore(t)
	vec := unitVec4(1, 0, 0, 0)

	if err := st.Upsert("v1", vec, nil); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := st.Delete("v1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, ok, _ := st.Get("v1")
	if ok {
		t.Error("record should be gone after Delete")
	}
}

// ── Trace JSON ────────────────────────────────────────────────────────────────

func TestExplainSearch_TraceJSON_Valid(t *testing.T) {
	st := openExplainStore(t)
	vec := unitVec4(1, 0, 0, 0)
	if err := st.Upsert("v1", vec, nil); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	tr, _, err := st.ExplainSearch(unitVec4(1, 0, 0, 0), 1)
	if err != nil {
		t.Fatalf("ExplainSearch: %v", err)
	}

	jsonBytes, err2 := tr.MarshalJSON()
	if err2 != nil {
		t.Fatalf("MarshalJSON: %v", err2)
	}
	if len(jsonBytes) == 0 {
		t.Error("empty JSON output")
	}
}

// ── Internal helper ───────────────────────────────────────────────────────────

func vecID(i int) string {
	return [...]string{"v0", "v1", "v2", "v3", "v4", "v5"}[i]
}
