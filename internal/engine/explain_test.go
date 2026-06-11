package engine

import (
	"errors"
	"testing"

	"github.com/YashPatel2395/ShardForgeDB/internal/trace"
)

// ── ExplainPut ────────────────────────────────────────────────────────────────

func TestExplainPut_RecordsWALAndMemTableSteps(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	tr, err := e.ExplainPut([]byte("user:1"), []byte("alice"))
	if err != nil {
		t.Fatalf("ExplainPut: %v", err)
	}
	if tr == nil {
		t.Fatal("nil trace")
	}

	// Must have at least: key validated, WAL append, MemTable put.
	walSteps := tr.StepsForComponent(trace.ComponentWAL)
	memSteps := tr.StepsForComponent(trace.ComponentMemTable)

	if len(walSteps) != 1 {
		t.Errorf("want 1 WAL step, got %d", len(walSteps))
	}
	if len(memSteps) != 1 {
		t.Errorf("want 1 MemTable step, got %d", len(memSteps))
	}
	if walSteps[0].StepType != trace.StepTypeWALAppend {
		t.Errorf("WAL step type = %q, want WAL_APPEND", walSteps[0].StepType)
	}
	if memSteps[0].StepType != trace.StepTypeMemTablePut {
		t.Errorf("MemTable step type = %q, want MEMTABLE_PUT", memSteps[0].StepType)
	}
	if walSteps[0].Status != trace.StatusOK {
		t.Errorf("WAL step status = %q, want OK", walSteps[0].Status)
	}
	if tr.Operation != trace.OpPut {
		t.Errorf("operation = %q, want PUT", tr.Operation)
	}
	if tr.Err != "" {
		t.Errorf("unexpected trace error: %s", tr.Err)
	}
}

func TestExplainPut_EmptyKey_ReturnsError(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	tr, err := e.ExplainPut([]byte{}, []byte("value"))
	if err == nil {
		t.Fatal("expected error for empty key")
	}
	if !errors.Is(err, ErrInvalidKey) {
		t.Errorf("error = %v, want ErrInvalidKey", err)
	}
	if tr == nil {
		t.Fatal("nil trace returned on error")
	}
	if tr.Err == "" {
		t.Error("trace.Err should be set on error")
	}
	// Key validation step must be ERROR.
	engineSteps := tr.StepsForComponent(trace.ComponentEngine)
	if len(engineSteps) == 0 {
		t.Fatal("expected validation step")
	}
	if engineSteps[0].Status != trace.StatusError {
		t.Errorf("validation step status = %q, want ERROR", engineSteps[0].Status)
	}
}

func TestExplainPut_ResultMatchesPut(t *testing.T) {
	dir := tmpDir(t)
	e := openEngine(t, dir)
	defer e.Close()

	// Use ExplainPut; then verify the value is readable via regular Get.
	_, err := e.ExplainPut([]byte("k"), []byte("v"))
	if err != nil {
		t.Fatalf("ExplainPut: %v", err)
	}
	val, ok, err := e.Get([]byte("k"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok || string(val) != "v" {
		t.Errorf("Get after ExplainPut: got (%q, %v), want (\"v\", true)", val, ok)
	}
}

// ── ExplainGet ────────────────────────────────────────────────────────────────

func TestExplainGet_MemTableHit_NoSSTablSteps(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	mustPut(t, e, "user:1", "alice")

	tr, val, found, err := e.ExplainGet([]byte("user:1"))
	if err != nil {
		t.Fatalf("ExplainGet: %v", err)
	}
	if !found || string(val) != "alice" {
		t.Errorf("result: found=%v val=%q, want true/alice", found, val)
	}

	// MemTable hit must be recorded.
	memHits := countStepType(tr, trace.StepTypeMemTableHit)
	if memHits != 1 {
		t.Errorf("want 1 MEMTABLE_HIT, got %d", memHits)
	}
	// No SSTable steps when key is in MemTable.
	sstSteps := tr.StepsForComponent(trace.ComponentSSTable)
	bloomSteps := tr.StepsForComponent(trace.ComponentBloom)
	if len(sstSteps) != 0 {
		t.Errorf("want 0 SSTable steps, got %d", len(sstSteps))
	}
	if len(bloomSteps) != 0 {
		t.Errorf("want 0 Bloom steps, got %d", len(bloomSteps))
	}
}

func TestExplainGet_AfterFlush_RecordsSSTableAndBloom(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	mustPut(t, e, "user:1", "alice")
	mustFlush(t, e)

	// Clear MemTable by reopening (after flush, MemTable is reset).
	// Insert different key so the flushed one isn't in the new MemTable.
	mustPut(t, e, "user:2", "bob")

	tr, val, found, err := e.ExplainGet([]byte("user:1"))
	if err != nil {
		t.Fatalf("ExplainGet: %v", err)
	}
	if !found || string(val) != "alice" {
		t.Errorf("result: found=%v val=%q, want true/alice", found, val)
	}

	// MemTable miss must be recorded (user:1 not in new MemTable).
	memMisses := countStepType(tr, trace.StepTypeMemTableMiss)
	if memMisses < 1 {
		t.Errorf("want ≥1 MEMTABLE_MISS, got %d", memMisses)
	}

	// SSTable hit or Bloom check must appear.
	bloomChecks := countStepType(tr, trace.StepTypeBloomCheck)
	bloomSkips := countStepType(tr, trace.StepTypeBloomSkip)
	sstHits := countStepType(tr, trace.StepTypeSSTableHit)
	if bloomChecks+bloomSkips+sstHits == 0 {
		t.Error("expected SSTable/Bloom steps after flush, got none")
	}
}

func TestExplainGet_MissingKey_RecordsNotFound(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	tr, val, found, err := e.ExplainGet([]byte("no-such-key"))
	if err != nil {
		t.Fatalf("ExplainGet: %v", err)
	}
	if found || val != nil {
		t.Errorf("expected not-found, got found=%v val=%v", found, val)
	}

	notFoundSteps := countStepType(tr, trace.StepTypeNotFound)
	if notFoundSteps != 1 {
		t.Errorf("want 1 NOT_FOUND step, got %d", notFoundSteps)
	}
}

func TestExplainGet_EmptyKey_ReturnsTrace(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	tr, val, found, err := e.ExplainGet([]byte{})
	if err != nil {
		t.Fatalf("ExplainGet empty key: %v", err)
	}
	if found || val != nil {
		t.Error("expected (nil, false, nil) for empty key")
	}
	if tr == nil {
		t.Fatal("nil trace for empty key")
	}
	// Must have a validation step.
	engineSteps := tr.StepsForComponent(trace.ComponentEngine)
	if len(engineSteps) == 0 {
		t.Error("expected at least one engine step for empty key")
	}
}

func TestExplainGet_Tombstone_RecordsHit(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	mustPut(t, e, "k", "v")
	mustDelete(t, e, "k")

	tr, val, found, err := e.ExplainGet([]byte("k"))
	if err != nil {
		t.Fatalf("ExplainGet: %v", err)
	}
	if found || val != nil {
		t.Errorf("expected not-found (tombstone), got found=%v val=%v", found, val)
	}
	// The tombstone is in MemTable — should see a MEMTABLE_HIT (with tombstone detail).
	memHits := countStepType(tr, trace.StepTypeMemTableHit)
	if memHits != 1 {
		t.Errorf("want 1 MEMTABLE_HIT for tombstone, got %d", memHits)
	}
}

func TestExplainGet_BloomSkip_Recorded(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	// Write and flush "a", then search for "z" (definitely not present).
	mustPut(t, e, "a", "val")
	mustFlush(t, e)

	tr, _, found, err := e.ExplainGet([]byte("z"))
	if err != nil {
		t.Fatalf("ExplainGet: %v", err)
	}
	if found {
		t.Error("should not find key z")
	}

	// Either BLOOM_SKIP or BOUNDS_SKIP must appear (key is outside range or
	// Bloom says absent).
	bloomSkips := countStepType(tr, trace.StepTypeBloomSkip)
	boundsSkips := countStepType(tr, trace.StepTypeBoundsSkip)
	if bloomSkips+boundsSkips == 0 {
		t.Error("expected at least one BLOOM_SKIP or BOUNDS_SKIP step")
	}
}

func TestExplainGet_ResultMatchesGet(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	mustPut(t, e, "key", "value")

	trVal, ok, _ := e.Get([]byte("key"))
	_, expVal, expFound, _ := e.ExplainGet([]byte("key"))
	if ok != expFound || string(trVal) != string(expVal) {
		t.Errorf("ExplainGet result differs from Get: (%v,%q) vs (%v,%q)",
			ok, trVal, expFound, expVal)
	}
}

// ── ExplainDelete ─────────────────────────────────────────────────────────────

func TestExplainDelete_RecordsTombstonePath(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	mustPut(t, e, "k", "v")

	tr, err := e.ExplainDelete([]byte("k"))
	if err != nil {
		t.Fatalf("ExplainDelete: %v", err)
	}

	walSteps := tr.StepsForComponent(trace.ComponentWAL)
	memSteps := tr.StepsForComponent(trace.ComponentMemTable)

	if len(walSteps) != 1 || walSteps[0].StepType != trace.StepTypeWALAppend {
		t.Errorf("want 1 WAL_APPEND, got %d steps", len(walSteps))
	}
	if len(memSteps) != 1 || memSteps[0].StepType != trace.StepTypeMemTableDelete {
		t.Errorf("want 1 MEMTABLE_DELETE, got %d steps", len(memSteps))
	}
	// Detail should mention tombstone.
	if walSteps[0].Status != trace.StatusOK {
		t.Errorf("WAL step status = %q, want OK", walSteps[0].Status)
	}
}

func TestExplainDelete_EmptyKey_ReturnsError(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	tr, err := e.ExplainDelete([]byte{})
	if err == nil {
		t.Fatal("expected error for empty key")
	}
	if !errors.Is(err, ErrInvalidKey) {
		t.Errorf("error = %v, want ErrInvalidKey", err)
	}
	if tr == nil || tr.Err == "" {
		t.Error("trace should record the error")
	}
}

func TestExplainDelete_ResultMatchesDelete(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	mustPut(t, e, "del-key", "v")
	_, err := e.ExplainDelete([]byte("del-key"))
	if err != nil {
		t.Fatalf("ExplainDelete: %v", err)
	}
	_, found, _ := e.Get([]byte("del-key"))
	if found {
		t.Error("key should be gone after ExplainDelete")
	}
}

// ── ExplainScan ───────────────────────────────────────────────────────────────

func TestExplainScan_RecordsSourceAndMerge(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	mustPut(t, e, "a", "1")
	mustPut(t, e, "b", "2")
	mustFlush(t, e)
	mustPut(t, e, "c", "3")

	tr, entries, err := e.ExplainScan(nil, nil)
	if err != nil {
		t.Fatalf("ExplainScan: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("want 3 entries, got %d", len(entries))
	}

	// Must have a MemTable scan source step.
	memSources := countStepTypeInComponent(tr, trace.ComponentMemTable, trace.StepTypeScanSource)
	if memSources < 1 {
		t.Errorf("want ≥1 SCAN_SOURCE from MEMTABLE, got %d", memSources)
	}

	// Must have an SSTable scan source step (we flushed one).
	sstSources := countStepTypeInComponent(tr, trace.ComponentSSTable, trace.StepTypeScanSource)
	if sstSources < 1 {
		t.Errorf("want ≥1 SCAN_SOURCE from SSTABLE, got %d", sstSources)
	}

	// Must have a merge step.
	mergeSteps := countStepType(tr, trace.StepTypeScanMerge)
	if mergeSteps != 1 {
		t.Errorf("want 1 SCAN_MERGE step, got %d", mergeSteps)
	}
}

func TestExplainScan_TombstoneSuppression(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	mustPut(t, e, "keep", "v")
	mustPut(t, e, "drop", "v")
	mustDelete(t, e, "drop")

	tr, entries, err := e.ExplainScan(nil, nil)
	if err != nil {
		t.Fatalf("ExplainScan: %v", err)
	}
	if len(entries) != 1 || string(entries[0].Key) != "keep" {
		t.Errorf("expected [keep], got %v entries", len(entries))
	}

	// Merge step detail should mention tombstones_suppressed.
	mergeStep := firstStepOfType(tr, trace.StepTypeScanMerge)
	if mergeStep == nil {
		t.Fatal("missing SCAN_MERGE step")
	}
	// Just verify it was recorded with OK status.
	if mergeStep.Status != trace.StatusOK {
		t.Errorf("merge step status = %q, want OK", mergeStep.Status)
	}
}

func TestExplainScan_ResultMatchesScan(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	mustPut(t, e, "x", "1")
	mustPut(t, e, "y", "2")

	regularEntries, _ := e.Scan(nil, nil)
	_, explainEntries, _ := e.ExplainScan(nil, nil)

	if len(regularEntries) != len(explainEntries) {
		t.Errorf("Scan=%d entries, ExplainScan=%d entries", len(regularEntries), len(explainEntries))
	}
	for i := range regularEntries {
		if string(regularEntries[i].Key) != string(explainEntries[i].Key) {
			t.Errorf("entry[%d] key mismatch: %q vs %q", i, regularEntries[i].Key, explainEntries[i].Key)
		}
	}
}

// ── Trace metadata ────────────────────────────────────────────────────────────

func TestExplainPut_TraceHasOperation(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	tr, _ := e.ExplainPut([]byte("k"), []byte("v"))
	if tr.Operation != trace.OpPut {
		t.Errorf("operation = %q, want PUT", tr.Operation)
	}
	if tr.FinishedAt.IsZero() {
		t.Error("FinishedAt not set")
	}
}

func TestExplainGet_TraceHasOperation(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	tr, _, _, _ := e.ExplainGet([]byte("k"))
	if tr.Operation != trace.OpGet {
		t.Errorf("operation = %q, want GET", tr.Operation)
	}
}

func TestExplainDelete_TraceHasOperation(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()
	mustPut(t, e, "k", "v")

	tr, _ := e.ExplainDelete([]byte("k"))
	if tr.Operation != trace.OpDelete {
		t.Errorf("operation = %q, want DELETE", tr.Operation)
	}
}

func TestExplainScan_TraceHasOperation(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	tr, _, _ := e.ExplainScan(nil, nil)
	if tr.Operation != trace.OpScan {
		t.Errorf("operation = %q, want SCAN", tr.Operation)
	}
}

// ── Existing API unchanged ────────────────────────────────────────────────────

func TestExistingPut_UnchangedByPhase22(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	if err := e.Put([]byte("key"), []byte("val")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	v, ok, _ := e.Get([]byte("key"))
	if !ok || string(v) != "val" {
		t.Errorf("Get after Put: (%q, %v)", v, ok)
	}
}

func TestExistingGet_UnchangedByPhase22(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	mustPut(t, e, "k", "v")
	v, ok, err := e.Get([]byte("k"))
	if err != nil || !ok || string(v) != "v" {
		t.Errorf("Get: (%q, %v, %v)", v, ok, err)
	}
}

func TestExistingDelete_UnchangedByPhase22(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	mustPut(t, e, "k", "v")
	if err := e.Delete([]byte("k")); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, ok, _ := e.Get([]byte("k"))
	if ok {
		t.Error("key should be gone after Delete")
	}
}

func TestExistingScan_UnchangedByPhase22(t *testing.T) {
	e := openEngine(t, tmpDir(t))
	defer e.Close()

	mustPut(t, e, "a", "1")
	mustPut(t, e, "b", "2")
	entries, err := e.Scan(nil, nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("want 2 entries, got %d", len(entries))
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func countStepType(tr *trace.Trace, st trace.StepType) int {
	n := 0
	for _, s := range tr.Steps {
		if s.StepType == st {
			n++
		}
	}
	return n
}

func countStepTypeInComponent(tr *trace.Trace, c trace.Component, st trace.StepType) int {
	n := 0
	for _, s := range tr.Steps {
		if s.Component == c && s.StepType == st {
			n++
		}
	}
	return n
}

func firstStepOfType(tr *trace.Trace, st trace.StepType) *trace.TraceStep {
	for i := range tr.Steps {
		if tr.Steps[i].StepType == st {
			return &tr.Steps[i]
		}
	}
	return nil
}
