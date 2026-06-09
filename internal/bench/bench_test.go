package bench

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// ── Key / value generation ────────────────────────────────────────────────────

func TestGenKey_IsDeterministic(t *testing.T) {
	k1 := GenKey(0)
	k2 := GenKey(0)
	if string(k1) != string(k2) {
		t.Errorf("GenKey(0) not deterministic: %q vs %q", k1, k2)
	}
}

func TestGenKey_LexicographicOrder(t *testing.T) {
	// Keys must sort lexicographically in the same order as their indices.
	for i := 0; i < 999; i++ {
		a := string(GenKey(i))
		b := string(GenKey(i + 1))
		if a >= b {
			t.Fatalf("GenKey(%d)=%q >= GenKey(%d)=%q — not lexicographic", i, a, i+1, b)
		}
	}
}

func TestGenKey_Format(t *testing.T) {
	if got := string(GenKey(0)); got != "key-0000000000" {
		t.Errorf("GenKey(0) = %q, want %q", got, "key-0000000000")
	}
	if got := string(GenKey(42)); got != "key-0000000042" {
		t.Errorf("GenKey(42) = %q, want %q", got, "key-0000000042")
	}
}

func TestGenValue_IsDeterministic(t *testing.T) {
	v1 := GenValue(42, 7, 32)
	v2 := GenValue(42, 7, 32)
	if string(v1) != string(v2) {
		t.Error("GenValue(42, 7, 32) not deterministic")
	}
}

func TestGenValue_DifferentSeedProducesDifferentOutput(t *testing.T) {
	v1 := GenValue(1, 0, 32)
	v2 := GenValue(2, 0, 32)
	if string(v1) == string(v2) {
		t.Error("GenValue with different seeds produced identical output")
	}
}

func TestGenValue_DifferentIndexProducesDifferentOutput(t *testing.T) {
	v1 := GenValue(42, 0, 32)
	v2 := GenValue(42, 1, 32)
	if string(v1) == string(v2) {
		t.Error("GenValue with different indices produced identical output")
	}
}

func TestGenValue_CorrectLength(t *testing.T) {
	for _, size := range []int{1, 16, 128, 256} {
		if got := len(GenValue(42, 0, size)); got != size {
			t.Errorf("GenValue size=%d: got %d bytes", size, got)
		}
	}
}

// ── Percentile ────────────────────────────────────────────────────────────────

func TestPercentile_Empty(t *testing.T) {
	if got := Percentile(nil, 50); got != 0 {
		t.Errorf("Percentile(nil, 50) = %v, want 0", got)
	}
}

func TestPercentile_SingleElement(t *testing.T) {
	durations := []time.Duration{100 * time.Nanosecond}
	for _, p := range []int{0, 50, 99, 100} {
		if got := Percentile(durations, p); got != 100 {
			t.Errorf("Percentile([100ns], %d) = %v, want 100ns", p, got)
		}
	}
}

func TestPercentile_KnownValues(t *testing.T) {
	// 100 elements: 1ns, 2ns, ..., 100ns.
	durations := make([]time.Duration, 100)
	for i := range durations {
		durations[i] = time.Duration(i+1) * time.Nanosecond
	}
	// P50 = index 49 (0-based) = 50ns.
	if got := Percentile(durations, 50); got != 50*time.Nanosecond {
		t.Errorf("P50 = %v, want 50ns", got)
	}
	// P95 = index 94 = 95ns.
	if got := Percentile(durations, 95); got != 95*time.Nanosecond {
		t.Errorf("P95 = %v, want 95ns", got)
	}
	// P99 = index 98 = 99ns.
	if got := Percentile(durations, 99); got != 99*time.Nanosecond {
		t.Errorf("P99 = %v, want 99ns", got)
	}
}

func TestPercentile_DoesNotModifyInput(t *testing.T) {
	durations := []time.Duration{5, 3, 1, 4, 2}
	orig := make([]time.Duration, len(durations))
	copy(orig, durations)
	Percentile(durations, 50)
	for i, d := range durations {
		if d != orig[i] {
			t.Errorf("Percentile modified input at index %d", i)
		}
	}
}

// ── Config validation ─────────────────────────────────────────────────────────

func TestConfig_Validate_Valid(t *testing.T) {
	cfg := Config{Scale: Scales["small"], Seed: 42, EngineDir: t.TempDir()}
	if err := cfg.Validate(); err != nil {
		t.Errorf("valid config failed: %v", err)
	}
}

func TestConfig_Validate_ZeroKeyCount(t *testing.T) {
	sc := Scales["small"]
	sc.KeyCount = 0
	cfg := Config{Scale: sc}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for KeyCount=0, got nil")
	}
}

func TestConfig_Validate_ZeroValueSize(t *testing.T) {
	sc := Scales["small"]
	sc.ValueSize = 0
	cfg := Config{Scale: sc}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for ValueSize=0, got nil")
	}
}

func TestConfig_Validate_ZeroFlushInterval(t *testing.T) {
	sc := Scales["small"]
	sc.FlushInterval = 0
	cfg := Config{Scale: sc}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for FlushInterval=0, got nil")
	}
}

func TestConfig_Validate_ZeroRangeSize(t *testing.T) {
	sc := Scales["small"]
	sc.RangeSize = 0
	cfg := Config{Scale: sc}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for RangeSize=0, got nil")
	}
}

func TestConfig_Validate_ZeroScanCount(t *testing.T) {
	sc := Scales["small"]
	sc.ScanCount = 0
	cfg := Config{Scale: sc}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for ScanCount=0, got nil")
	}
}

// ── Runner: scale and workload validation ─────────────────────────────────────

func TestRunner_InvalidScale_ReturnsError(t *testing.T) {
	r := &Runner{ScaleName: "huge"}
	_, err := r.Run()
	if err == nil {
		t.Fatal("expected error for invalid scale, got nil")
	}
	if !errors.Is(err, ErrUnknownScale) {
		t.Errorf("expected ErrUnknownScale, got %v", err)
	}
}

func TestRunner_InvalidWorkload_ReturnsError(t *testing.T) {
	r := &Runner{ScaleName: "small", WorkloadNames: []string{"no-such-workload"}}
	_, err := r.Run()
	if err == nil {
		t.Fatal("expected error for invalid workload, got nil")
	}
	if !errors.Is(err, ErrUnknownWorkload) {
		t.Errorf("expected ErrUnknownWorkload, got %v", err)
	}
}

// ── Workload execution (small scale) ─────────────────────────────────────────

func runWorkload(t *testing.T, name string) Result {
	t.Helper()
	r := &Runner{
		ScaleName:     "small",
		WorkloadNames: []string{name},
		Seed:          42,
		BaseDir:       t.TempDir(),
	}
	results, err := r.Run()
	if err != nil {
		t.Fatalf("%s: Run failed: %v", name, err)
	}
	if len(results) != 1 {
		t.Fatalf("%s: expected 1 result, got %d", name, len(results))
	}
	res := results[0]
	if res.Name != name {
		t.Errorf("result name = %q, want %q", res.Name, name)
	}
	if res.Operations <= 0 {
		t.Errorf("Operations = %d, want > 0", res.Operations)
	}
	if res.Duration <= 0 {
		t.Errorf("Duration = %v, want > 0", res.Duration)
	}
	return res
}

func TestWorkload_WriteHeavy_SmallScale(t *testing.T) {
	res := runWorkload(t, "write-heavy")
	if res.BytesWritten == 0 {
		t.Error("BytesWritten = 0, want > 0")
	}
	if res.FlushCount == 0 {
		t.Error("FlushCount = 0, want > 0")
	}
}

func TestWorkload_ReadHeavy_SmallScale(t *testing.T) {
	res := runWorkload(t, "read-heavy")
	if res.BytesRead == 0 {
		t.Error("BytesRead = 0, want > 0")
	}
	// Bloom filter negative skips: some reads target missing keys.
	if res.BloomChecks == 0 {
		t.Error("BloomChecks = 0, want > 0")
	}
}

func TestWorkload_Mixed_SmallScale(t *testing.T) {
	res := runWorkload(t, "mixed")
	sc := Scales["small"]
	if res.Operations != sc.KeyCount {
		t.Errorf("Operations = %d, want %d", res.Operations, sc.KeyCount)
	}
}

func TestWorkload_Scan_SmallScale(t *testing.T) {
	res := runWorkload(t, "scan")
	sc := Scales["small"]
	if res.Operations != sc.ScanCount {
		t.Errorf("Operations = %d, want %d (ScanCount)", res.Operations, sc.ScanCount)
	}
	if res.BytesRead == 0 {
		t.Error("BytesRead = 0, want > 0")
	}
}

func TestWorkload_Compaction_SmallScale(t *testing.T) {
	res := runWorkload(t, "compaction")
	if res.PreCompactSSTableCount < 2 {
		t.Errorf("PreCompactSSTableCount = %d, want >= 2", res.PreCompactSSTableCount)
	}
	if res.PostCompactSSTableCount != 1 {
		t.Errorf("PostCompactSSTableCount = %d, want 1", res.PostCompactSSTableCount)
	}
	if res.CompactDuration <= 0 {
		t.Error("CompactDuration = 0, want > 0")
	}
	if res.CompactionCount != 1 {
		t.Errorf("CompactionCount = %d, want 1", res.CompactionCount)
	}
}

func TestWorkload_Compaction_IncludesGetAndScanMeasurements(t *testing.T) {
	res := runWorkload(t, "compaction")

	// SSTable state around compaction.
	if res.PreCompactSSTableCount < 2 {
		t.Errorf("PreCompactSSTableCount = %d, want >= 2", res.PreCompactSSTableCount)
	}
	if res.PostCompactSSTableCount != 1 {
		t.Errorf("PostCompactSSTableCount = %d, want 1", res.PostCompactSSTableCount)
	}
	if res.CompactDuration <= 0 {
		t.Error("CompactDuration = 0, want > 0")
	}

	// Per-phase Get op counts.
	if res.PreCompactGetOps <= 0 {
		t.Errorf("PreCompactGetOps = %d, want > 0", res.PreCompactGetOps)
	}
	if res.PostCompactGetOps <= 0 {
		t.Errorf("PostCompactGetOps = %d, want > 0", res.PostCompactGetOps)
	}

	// Per-phase Scan op counts.
	if res.PreCompactScanOps <= 0 {
		t.Errorf("PreCompactScanOps = %d, want > 0", res.PreCompactScanOps)
	}
	if res.PostCompactScanOps <= 0 {
		t.Errorf("PostCompactScanOps = %d, want > 0", res.PostCompactScanOps)
	}

	// Total ops = Gets before + Scans before + Gets after + Scans after.
	expectedOps := res.PreCompactGetOps + res.PreCompactScanOps +
		res.PostCompactGetOps + res.PostCompactScanOps
	if res.Operations != expectedOps {
		t.Errorf("Operations = %d, want %d (PreGet+PreScan+PostGet+PostScan)",
			res.Operations, expectedOps)
	}

	// BytesRead must exceed what Get-only sampling would produce.
	// Get-only minimum: sampleOps*2 * ValueSize. Scans read key+value per entry,
	// covering RangeSize keys per scan, so BytesRead must be strictly larger.
	sc := Scales["small"]
	getOnlyMinBytes := uint64(res.PreCompactGetOps+res.PostCompactGetOps) * uint64(sc.ValueSize)
	if res.BytesRead <= getOnlyMinBytes {
		t.Errorf("BytesRead = %d, want > %d (Get-only minimum — Scans should contribute additional bytes)",
			res.BytesRead, getOnlyMinBytes)
	}
}

func TestWorkload_Restart_SmallScale(t *testing.T) {
	res := runWorkload(t, "restart")
	// Restart measures exactly 1 operation (the reopen).
	if res.Operations != 1 {
		t.Errorf("Operations = %d, want 1", res.Operations)
	}
	// All three percentiles should be the same single measurement.
	if res.P50Latency != res.P99Latency {
		t.Errorf("P50=%v P99=%v should be equal for 1-op workload", res.P50Latency, res.P99Latency)
	}
}

// ── Runner cleanup ────────────────────────────────────────────────────────────

func TestRunner_CleansUpTempDirs(t *testing.T) {
	baseDir := t.TempDir()
	r := &Runner{
		ScaleName:     "small",
		WorkloadNames: []string{"write-heavy"},
		Seed:          42,
		BaseDir:       baseDir,
	}
	if _, err := r.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("temp dirs not cleaned up after Run: %v", names)
	}
}

// ── Report generation ─────────────────────────────────────────────────────────

func sampleResults() []Result {
	return []Result{
		{
			Name:               "write-heavy",
			Operations:         1000,
			Duration:           500 * time.Millisecond,
			OpsPerSec:          2000,
			P50Latency:         250 * time.Microsecond,
			P95Latency:         1 * time.Millisecond,
			P99Latency:         5 * time.Millisecond,
			BytesWritten:       131072,
			FinalSSTableCount:  10,
			FlushCount:         10,
			BloomChecks:        500,
			BloomNegativeSkips: 25,
		},
	}
}

func sampleCompactionResult() Result {
	return Result{
		Name:                    "compaction",
		Operations:              240,
		Duration:                200 * time.Millisecond,
		OpsPerSec:               1200,
		P50Latency:              2 * time.Microsecond,
		P95Latency:              4 * time.Microsecond,
		P99Latency:              8 * time.Microsecond,
		BytesRead:               512000,
		FinalSSTableCount:       1,
		CompactionCount:         1,
		PreCompactSSTableCount:  5,
		PostCompactSSTableCount: 1,
		CompactDuration:         30 * time.Millisecond,
		PreCompactGetOps:        100,
		PostCompactGetOps:       100,
		PreCompactScanOps:       20,
		PostCompactScanOps:      20,
	}
}

func TestReport_ContainsRequiredSections(t *testing.T) {
	cfg := Config{Scale: Scales["small"], Seed: 42}
	var sb strings.Builder
	if err := WriteReport(&sb, "small", cfg, sampleResults()); err != nil {
		t.Fatalf("WriteReport: %v", err)
	}
	out := sb.String()

	required := []string{
		"## Environment",
		"## Configuration",
		"## Commands",
		"## Results",
		"## Interpretation",
		"## Known Limitations",
		"## How to Reproduce",
	}
	for _, section := range required {
		if !strings.Contains(out, section) {
			t.Errorf("report missing required section: %q", section)
		}
	}
}

func TestReport_IsDeterministic(t *testing.T) {
	cfg := Config{Scale: Scales["small"], Seed: 42}
	results := sampleResults()

	var sb1, sb2 strings.Builder
	if err := WriteReport(&sb1, "small", cfg, results); err != nil {
		t.Fatalf("WriteReport 1: %v", err)
	}
	if err := WriteReport(&sb2, "small", cfg, results); err != nil {
		t.Fatalf("WriteReport 2: %v", err)
	}

	if sb1.String() != sb2.String() {
		t.Error("WriteReport output is not deterministic for identical inputs")
	}
}

func TestReport_ContainsWorkloadName(t *testing.T) {
	cfg := Config{Scale: Scales["small"], Seed: 42}
	var sb strings.Builder
	if err := WriteReport(&sb, "small", cfg, sampleResults()); err != nil {
		t.Fatalf("WriteReport: %v", err)
	}
	if !strings.Contains(sb.String(), "write-heavy") {
		t.Error("report does not contain workload name 'write-heavy'")
	}
}

func TestReport_ContainsScaleName(t *testing.T) {
	cfg := Config{Scale: Scales["small"], Seed: 42}
	var sb strings.Builder
	if err := WriteReport(&sb, "small", cfg, sampleResults()); err != nil {
		t.Fatalf("WriteReport: %v", err)
	}
	if !strings.Contains(sb.String(), "small") {
		t.Error("report does not contain scale name 'small'")
	}
}

func TestReport_CompactionDetail_IncludesGetAndScanCounts(t *testing.T) {
	cfg := Config{Scale: Scales["small"], Seed: 42}
	results := []Result{sampleCompactionResult()}
	var sb strings.Builder
	if err := WriteReport(&sb, "small", cfg, results); err != nil {
		t.Fatalf("WriteReport: %v", err)
	}
	out := sb.String()

	required := []string{
		"## Compaction Detail",
		"Gets before compact",
		"Gets after compact",
		"Scans before compact",
		"Scans after compact",
		"Total measured ops",
	}
	for _, s := range required {
		if !strings.Contains(out, s) {
			t.Errorf("compaction detail section missing: %q", s)
		}
	}
}

func TestReport_WriteReportFile(t *testing.T) {
	path := t.TempDir() + "/report.md"
	cfg := Config{Scale: Scales["small"], Seed: 42}
	if err := WriteReportFile(path, "small", cfg, sampleResults()); err != nil {
		t.Fatalf("WriteReportFile: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(data) == 0 {
		t.Error("report file is empty")
	}
}

// ── Go benchmark wrappers ─────────────────────────────────────────────────────

func BenchmarkGenKey(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = GenKey(i)
	}
}

func BenchmarkGenValue_128(b *testing.B) {
	b.SetBytes(128)
	for i := 0; i < b.N; i++ {
		_ = GenValue(42, i, 128)
	}
}

func BenchmarkPercentile_1k(b *testing.B) {
	durations := make([]time.Duration, 1000)
	for i := range durations {
		durations[i] = time.Duration(i) * time.Nanosecond
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Percentile(durations, 99)
	}
}

func BenchmarkWorkload_WriteHeavy_Small(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		r := &Runner{
			ScaleName:     "small",
			WorkloadNames: []string{"write-heavy"},
			Seed:          42,
			BaseDir:       b.TempDir(),
		}
		b.StartTimer()
		if _, err := r.Run(); err != nil {
			b.Fatalf("Run: %v", err)
		}
	}
}

func BenchmarkWorkload_ReadHeavy_Small(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		r := &Runner{
			ScaleName:     "small",
			WorkloadNames: []string{"read-heavy"},
			Seed:          42,
			BaseDir:       b.TempDir(),
		}
		b.StartTimer()
		if _, err := r.Run(); err != nil {
			b.Fatalf("Run: %v", err)
		}
	}
}
