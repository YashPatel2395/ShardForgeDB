package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// runCapture calls runWithWriters and returns stdout, stderr, and exit code.
func runCapture(args []string) (stdout, stderr string, code int) {
	var out, errOut strings.Builder
	code = runWithWriters(args, &out, &errOut)
	return out.String(), errOut.String(), code
}

// TestRun_HelpIncludesScopeDisclaimer verifies that --help output contains all
// required scope-honesty phrases.
func TestRun_HelpIncludesScopeDisclaimer(t *testing.T) {
	_, errOut, _ := runCapture([]string{"--help"})
	// Note: flag.ContinueOnError returns code 2 for --help; we only check content.
	for _, phrase := range []string{
		"stateless HTTP routing proxy",
		"No Raft",
		"No consensus",
		"No replication",
		"No failover",
		"No retry to another node",
	} {
		if !strings.Contains(errOut, phrase) {
			t.Errorf("help output missing phrase %q\nfull stderr:\n%s", phrase, errOut)
		}
	}
}

// TestRun_MissingNodes_ReturnsNonZero verifies that omitting --nodes is an error.
func TestRun_MissingNodes_ReturnsNonZero(t *testing.T) {
	_, _, code := runCapture([]string{})
	if code == 0 {
		t.Fatal("expected non-zero exit when --nodes is missing")
	}
}

// TestRun_InvalidTimeout_ReturnsNonZero verifies that a bad --timeout value fails.
func TestRun_InvalidTimeout_ReturnsNonZero(t *testing.T) {
	_, _, code := runCapture([]string{
		"--nodes", "http://127.0.0.1:9101",
		"--timeout", "not-a-duration",
	})
	if code == 0 {
		t.Fatal("expected non-zero exit for invalid --timeout")
	}
}

// TestRun_InvalidFlag_ReturnsNonZero verifies that an unknown flag fails.
func TestRun_InvalidFlag_ReturnsNonZero(t *testing.T) {
	_, _, code := runCapture([]string{"--unknown-flag"})
	if code == 0 {
		t.Fatal("expected non-zero exit for unknown flag")
	}
}

// TestBuildNodeConfigs_AssignsDeterministicIDs verifies that valid URLs get
// compact sequential IDs node-1, node-2, node-3.
func TestBuildNodeConfigs_AssignsDeterministicIDs(t *testing.T) {
	cfgs := buildNodeConfigs("http://a,http://b,http://c")
	if len(cfgs) != 3 {
		t.Fatalf("expected 3 configs, got %d", len(cfgs))
	}
	for i, want := range []string{"node-1", "node-2", "node-3"} {
		if cfgs[i].ID != want {
			t.Errorf("cfgs[%d].ID = %q, want %q", i, cfgs[i].ID, want)
		}
	}
}

// TestBuildNodeConfigs_SkipsEmptyEntries verifies that empty/whitespace-only
// comma entries are skipped and IDs remain compact (no gaps).
func TestBuildNodeConfigs_SkipsEmptyEntries(t *testing.T) {
	cfgs := buildNodeConfigs("http://a,,  ,http://b")
	if len(cfgs) != 2 {
		t.Fatalf("expected 2 configs, got %d", len(cfgs))
	}
	if cfgs[0].ID != "node-1" {
		t.Errorf("cfgs[0].ID = %q, want node-1", cfgs[0].ID)
	}
	if cfgs[1].ID != "node-2" {
		t.Errorf("cfgs[1].ID = %q, want node-2", cfgs[1].ID)
	}
}

// TestBuildNodeConfigs_SingleNode verifies single URL produces node-1.
func TestBuildNodeConfigs_SingleNode(t *testing.T) {
	cfgs := buildNodeConfigs("http://127.0.0.1:9101")
	if len(cfgs) != 1 {
		t.Fatalf("expected 1 config, got %d", len(cfgs))
	}
	if cfgs[0].ID != "node-1" {
		t.Errorf("ID = %q, want node-1", cfgs[0].ID)
	}
	if cfgs[0].BaseURL != "http://127.0.0.1:9101" {
		t.Errorf("BaseURL = %q, want http://127.0.0.1:9101", cfgs[0].BaseURL)
	}
}

// TestBuildNodeConfigs_EmptyString returns empty slice for empty input.
func TestBuildNodeConfigs_EmptyString(t *testing.T) {
	cfgs := buildNodeConfigs("")
	if len(cfgs) != 0 {
		t.Errorf("expected 0 configs for empty input, got %d", len(cfgs))
	}
}

// TestBuildNodeConfigs_TrimSpaces verifies that URLs with surrounding spaces are trimmed.
func TestBuildNodeConfigs_TrimSpaces(t *testing.T) {
	cfgs := buildNodeConfigs("  http://a  ,  http://b  ")
	if len(cfgs) != 2 {
		t.Fatalf("expected 2 configs, got %d", len(cfgs))
	}
	if cfgs[0].BaseURL != "http://a" {
		t.Errorf("BaseURL[0] = %q, want http://a", cfgs[0].BaseURL)
	}
	if cfgs[1].BaseURL != "http://b" {
		t.Errorf("BaseURL[1] = %q, want http://b", cfgs[1].BaseURL)
	}
}

// TestRun_HelpIncludesConfig verifies --help mentions --config flag.
func TestRun_HelpIncludesConfig(t *testing.T) {
	_, errOut, _ := runCapture([]string{"--help"})
	if !strings.Contains(errOut, "--config") {
		t.Errorf("help output missing --config flag\nstderr: %s", errOut)
	}
}

// TestRun_ConfigAndNodes_ReturnsNonZero verifies that providing both --config and
// --nodes is rejected with a clear error.
func TestRun_ConfigAndNodes_ReturnsNonZero(t *testing.T) {
	_, errOut, code := runCapture([]string{
		"--config", "configs/local-3node-with-proxy.json",
		"--nodes", "http://127.0.0.1:9101",
	})
	if code == 0 {
		t.Fatal("expected non-zero exit when both --config and --nodes are provided")
	}
	if !strings.Contains(errOut, "not both") {
		t.Errorf("error message should say 'not both': %q", errOut)
	}
}

// TestRun_Config_MissingFile_ReturnsNonZero verifies that a missing config file fails.
func TestRun_Config_MissingFile_ReturnsNonZero(t *testing.T) {
	_, _, code := runCapture([]string{"--config", "/nonexistent/cluster.json"})
	if code == 0 {
		t.Fatal("expected non-zero exit for missing config file")
	}
}

// TestRun_Config_InvalidJSON_ReturnsNonZero verifies that --config with an
// invalid JSON file returns non-zero with a load error.
// Note: Do NOT test --config with a valid file in CLI tests — proxy.Open succeeds
// (ring creation is ring-only, no network calls) and the server blocks indefinitely
// waiting for a signal. Use internal/cluster/loader_test.go for full proxy integration.
func TestRun_Config_InvalidJSON_ReturnsNonZero(t *testing.T) {
	// Write a temp file with invalid JSON.
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, errOut, code := runCapture([]string{"--config", path})
	if code == 0 {
		t.Fatal("expected non-zero exit for invalid JSON config")
	}
	if !strings.Contains(errOut, "load config") {
		t.Errorf("expected 'load config' in error: %q", errOut)
	}
}
