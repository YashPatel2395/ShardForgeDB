package main

import (
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
