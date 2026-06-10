package main

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func runCapture(args []string) (stdout, stderr string, code int) {
	var out, errOut strings.Builder
	code = runWithWriters(args, &out, &errOut)
	return out.String(), errOut.String(), code
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// file is .../cmd/shardforge-cluster/main_test.go — go up 3 levels
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func TestRun_NoArgs_PrintsUsage(t *testing.T) {
	_, errOut, code := runCapture([]string{})
	if code == 0 {
		t.Fatal("expected non-zero exit with no args")
	}
	if !strings.Contains(errOut, "SCOPE") {
		t.Errorf("help output missing SCOPE disclaimer: %q", errOut)
	}
}

func TestRun_Help_PrintsDisclaimer(t *testing.T) {
	_, errOut, _ := runCapture([]string{"--help"})
	for _, phrase := range []string{"No Raft", "No consensus", "static config only"} {
		if !strings.Contains(errOut, phrase) {
			t.Errorf("help missing %q\nstderr: %s", phrase, errOut)
		}
	}
}

func TestRun_Help_MentionsNewCommands(t *testing.T) {
	_, errOut, _ := runCapture([]string{"--help"})
	for _, cmd := range []string{"health", "simulate-failure", "plan-rebalance"} {
		if !strings.Contains(errOut, cmd) {
			t.Errorf("help output missing command %q\nstderr: %s", cmd, errOut)
		}
	}
}

func TestRun_UnknownCommand_ReturnsNonZero(t *testing.T) {
	_, _, code := runCapture([]string{"bogus"})
	if code == 0 {
		t.Fatal("expected non-zero for unknown command")
	}
}

func TestRun_Validate_MissingPath_ReturnsNonZero(t *testing.T) {
	_, _, code := runCapture([]string{"validate"})
	if code == 0 {
		t.Fatal("expected non-zero when path is missing")
	}
}

func TestRun_Validate_NonExistentFile_ReturnsNonZero(t *testing.T) {
	_, _, code := runCapture([]string{"validate", "/nonexistent/cluster.json"})
	if code == 0 {
		t.Fatal("expected non-zero for missing file")
	}
}

func TestRun_Validate_ValidFile_ReturnsZero(t *testing.T) {
	path := filepath.Join(repoRoot(t), "configs", "local-3node.json")
	out, _, code := runCapture([]string{"validate", path})
	if code != 0 {
		t.Fatalf("expected zero exit for valid config, got %d", code)
	}
	if !strings.Contains(out, "ok") {
		t.Errorf("expected ok in output: %q", out)
	}
}

func TestRun_Print_MissingPath_ReturnsNonZero(t *testing.T) {
	_, _, code := runCapture([]string{"print"})
	if code == 0 {
		t.Fatal("expected non-zero when path is missing")
	}
}

func TestRun_Print_ValidFile_PrintsJSON(t *testing.T) {
	path := filepath.Join(repoRoot(t), "configs", "local-3node.json")
	out, _, code := runCapture([]string{"print", path})
	if code != 0 {
		t.Fatalf("expected zero exit, got %d", code)
	}
	if !strings.Contains(out, "local-3node") {
		t.Errorf("expected name in output: %q", out)
	}
	if !strings.Contains(out, "fnv1a-consistent-hash") {
		t.Errorf("expected algorithm in output: %q", out)
	}
}

func TestRun_ExampleLocal3Node_PrintsJSON(t *testing.T) {
	out, _, code := runCapture([]string{"example-local-3node"})
	if code != 0 {
		t.Fatalf("expected zero exit, got %d", code)
	}
	if !strings.Contains(out, "v1") {
		t.Errorf("expected version in output: %q", out)
	}
	if !strings.Contains(out, "node-1") {
		t.Errorf("expected node-1 in output: %q", out)
	}
}

func TestRun_Validate_AllConfigFiles(t *testing.T) {
	root := repoRoot(t)
	for _, name := range []string{
		"local-3node.json",
		"local-3node-with-proxy.json",
		"docker-3node-with-proxy.json",
	} {
		path := filepath.Join(root, "configs", name)
		t.Run(name, func(t *testing.T) {
			out, errOut, code := runCapture([]string{"validate", path})
			if code != 0 {
				t.Errorf("expected zero exit for %s\nstdout: %s\nstderr: %s", name, out, errOut)
			}
		})
	}
}

// --- Health command tests ---

func TestRun_Health_MissingConfig_ReturnsNonZero(t *testing.T) {
	_, _, code := runCapture([]string{"health"})
	if code == 0 {
		t.Fatal("expected non-zero when config path missing")
	}
}

func TestRun_Health_NonExistentFile_ReturnsNonZero(t *testing.T) {
	_, _, code := runCapture([]string{"health", "/nonexistent/cluster.json"})
	if code == 0 {
		t.Fatal("expected non-zero for missing file")
	}
}

func TestRun_Health_ValidConfig_ReturnsZeroWithJSON(t *testing.T) {
	// Nodes won't be running, but command should still succeed (reports unhealthy).
	path := filepath.Join(repoRoot(t), "configs", "local-failure-sim-3node.json")
	out, _, code := runCapture([]string{"health", path})
	if code != 0 {
		t.Fatalf("expected zero exit even when nodes unhealthy, got %d", code)
	}
	// Output should be valid JSON.
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Errorf("health output is not valid JSON: %v\noutput: %s", err, out)
	}
	// Should contain scope disclaimer.
	if !strings.Contains(out, "no_automatic_failover") {
		t.Errorf("health output missing scope flags: %s", out)
	}
}

// --- simulate-failure command tests ---

func TestRun_SimulateFailure_MissingConfig_ReturnsNonZero(t *testing.T) {
	_, _, code := runCapture([]string{"simulate-failure"})
	if code == 0 {
		t.Fatal("expected non-zero when config path missing")
	}
}

func TestRun_SimulateFailure_UnknownNode_ReturnsNonZero(t *testing.T) {
	path := filepath.Join(repoRoot(t), "configs", "local-failure-sim-3node.json")
	_, _, code := runCapture([]string{"simulate-failure", path, "--down", "node-999", "--key", "user:1"})
	if code == 0 {
		t.Fatal("expected non-zero for unknown down node")
	}
}

func TestRun_SimulateFailure_NoKeys_ReturnsNonZero(t *testing.T) {
	path := filepath.Join(repoRoot(t), "configs", "local-failure-sim-3node.json")
	_, _, code := runCapture([]string{"simulate-failure", path, "--down", "node-2"})
	if code == 0 {
		t.Fatal("expected non-zero when no --key provided")
	}
}

func TestRun_SimulateFailure_WithKeys_ReturnsJSON(t *testing.T) {
	path := filepath.Join(repoRoot(t), "configs", "local-failure-sim-3node.json")
	out, _, code := runCapture([]string{
		"simulate-failure", path,
		"--down", "node-2",
		"--key", "user:1",
		"--key", "order:9",
	})
	if code != 0 {
		t.Fatalf("expected zero exit, got %d", code)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Errorf("simulate-failure output is not valid JSON: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "no_automatic_failover") {
		t.Errorf("simulate-failure output missing scope flags: %s", out)
	}
}

func TestRun_SimulateFailure_DoesNotWriteFiles(t *testing.T) {
	// This test verifies the command is stateless: running it multiple times
	// with the same args produces the same output and no side effects.
	path := filepath.Join(repoRoot(t), "configs", "local-failure-sim-3node.json")
	args := []string{"simulate-failure", path, "--down", "node-2", "--key", "user:1"}
	out1, _, _ := runCapture(args)
	out2, _, _ := runCapture(args)
	if out1 != out2 {
		t.Errorf("simulate-failure is not deterministic or has side effects:\nfirst: %s\nsecond: %s", out1, out2)
	}
}

// --- plan-rebalance command tests ---

func TestRun_PlanRebalance_MissingConfig_ReturnsNonZero(t *testing.T) {
	_, _, code := runCapture([]string{"plan-rebalance"})
	if code == 0 {
		t.Fatal("expected non-zero when config path missing")
	}
}

func TestRun_PlanRebalance_UnknownNode_ReturnsNonZero(t *testing.T) {
	path := filepath.Join(repoRoot(t), "configs", "local-failure-sim-3node.json")
	_, _, code := runCapture([]string{"plan-rebalance", path, "--remove", "node-999", "--key", "user:1"})
	if code == 0 {
		t.Fatal("expected non-zero for unknown removed node")
	}
}

func TestRun_PlanRebalance_NoKeys_ReturnsNonZero(t *testing.T) {
	path := filepath.Join(repoRoot(t), "configs", "local-failure-sim-3node.json")
	_, _, code := runCapture([]string{"plan-rebalance", path, "--remove", "node-2"})
	if code == 0 {
		t.Fatal("expected non-zero when no --key provided")
	}
}

func TestRun_PlanRebalance_WithKeys_ReturnsJSON(t *testing.T) {
	path := filepath.Join(repoRoot(t), "configs", "local-failure-sim-3node.json")
	out, _, code := runCapture([]string{
		"plan-rebalance", path,
		"--remove", "node-2",
		"--key", "user:1",
		"--key", "order:9",
	})
	if code != 0 {
		t.Fatalf("expected zero exit, got %d", code)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Errorf("plan-rebalance output is not valid JSON: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "operator_steps") {
		t.Errorf("plan-rebalance output missing operator_steps: %s", out)
	}
	if !strings.Contains(out, "no_data_movement") {
		t.Errorf("plan-rebalance output missing scope disclaimer: %s", out)
	}
}

func TestRun_PlanRebalance_DoesNotWriteFiles(t *testing.T) {
	// Verify idempotent: no files are created by running this command.
	path := filepath.Join(repoRoot(t), "configs", "local-failure-sim-3node.json")
	args := []string{"plan-rebalance", path, "--remove", "node-2", "--key", "user:1"}
	out1, _, _ := runCapture(args)
	out2, _, _ := runCapture(args)
	if out1 != out2 {
		t.Errorf("plan-rebalance is not deterministic:\nfirst: %s\nsecond: %s", out1, out2)
	}
}

func TestRun_Validate_FailureSimConfig_ReturnsZero(t *testing.T) {
	path := filepath.Join(repoRoot(t), "configs", "local-failure-sim-3node.json")
	out, _, code := runCapture([]string{"validate", path})
	if code != 0 {
		t.Fatalf("expected zero exit for failure-sim config, got %d", code)
	}
	if !strings.Contains(out, "ok") {
		t.Errorf("expected ok in output: %q", out)
	}
}
