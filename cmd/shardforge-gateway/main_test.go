package main

import (
	"strings"
	"testing"
)

// runCapture calls runWithWriters and returns stdout, stderr, and exit code.
func runCapture(args []string) (stdout, stderr string, code int) {
	var out, errOut strings.Builder
	code = runWithWriters(args, &out, &errOut)
	return out.String(), errOut.String(), code
}

// TestRun_HelpIncludesScopeDisclaimer verifies the --help output contains the
// scope disclaimer text required by the Phase 15 spec.
func TestRun_HelpIncludesScopeDisclaimer(t *testing.T) {
	_, errOut, code := runCapture([]string{"--help"})
	// flag.ContinueOnError returns exit 2 for --help; we just check output.
	_ = code
	for _, phrase := range []string{
		"client-side routing only",
		"No Raft",
		"No consensus",
		"No failover",
	} {
		if !strings.Contains(errOut, phrase) {
			t.Errorf("help output missing phrase %q\nfull stderr:\n%s", phrase, errOut)
		}
	}
}

// TestRun_MissingNodes_ReturnsNonZero verifies that omitting --nodes is an error.
func TestRun_MissingNodes_ReturnsNonZero(t *testing.T) {
	_, _, code := runCapture([]string{"route", "user:1"})
	if code == 0 {
		t.Fatal("expected non-zero exit when --nodes is missing")
	}
}

// TestRun_UnknownCommand_ReturnsNonZero verifies that an unrecognised command fails.
func TestRun_UnknownCommand_ReturnsNonZero(t *testing.T) {
	_, _, code := runCapture([]string{
		"--nodes", "http://127.0.0.1:9101",
		"bogus-command",
	})
	if code == 0 {
		t.Fatal("expected non-zero exit for unknown command")
	}
}

// TestRun_Route_PrintsSelectedNode verifies that `route <key>` prints the
// node_id and base_url fields without making any network calls.
// `route` is a pure ring computation, so no live node is required.
func TestRun_Route_PrintsSelectedNode(t *testing.T) {
	out, _, code := runCapture([]string{
		"--nodes", "http://127.0.0.1:9101,http://127.0.0.1:9102,http://127.0.0.1:9103",
		"route", "user:1",
	})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !strings.Contains(out, "node_id=") {
		t.Errorf("output missing node_id: %q", out)
	}
	if !strings.Contains(out, "base_url=") {
		t.Errorf("output missing base_url: %q", out)
	}
}

// TestRun_InvalidTimeout_ReturnsNonZero verifies that a bad --timeout value fails.
func TestRun_InvalidTimeout_ReturnsNonZero(t *testing.T) {
	_, _, code := runCapture([]string{
		"--nodes", "http://127.0.0.1:9101",
		"--timeout", "not-a-duration",
		"route", "user:1",
	})
	if code == 0 {
		t.Fatal("expected non-zero exit for invalid --timeout")
	}
}

// TestBuildNodeConfigs_AssignsDeterministicIDs checks that valid URLs get
// compact sequential IDs (node-1, node-2, node-3).
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
	if cfgs[0].BaseURL != "http://a" || cfgs[1].BaseURL != "http://b" || cfgs[2].BaseURL != "http://c" {
		t.Errorf("unexpected BaseURLs: %v", cfgs)
	}
}

// TestBuildNodeConfigs_SkipsEmptyEntries checks that empty or whitespace-only
// comma entries are skipped and IDs remain compact (no gaps).
func TestBuildNodeConfigs_SkipsEmptyEntries(t *testing.T) {
	// Two empty entries between valid URLs — IDs must still be node-1, node-2.
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

// TestRun_Route_MissingKeyArg verifies that `route` without a key argument fails.
func TestRun_Route_MissingKeyArg(t *testing.T) {
	_, _, code := runCapture([]string{
		"--nodes", "http://127.0.0.1:9101",
		"route",
	})
	if code == 0 {
		t.Fatal("expected non-zero exit when route key is missing")
	}
}

// TestRun_Route_DeterministicRouting verifies that the same key always routes
// to the same node, confirming ring determinism across two calls.
func TestRun_Route_DeterministicRouting(t *testing.T) {
	args := []string{
		"--nodes", "http://127.0.0.1:9101,http://127.0.0.1:9102,http://127.0.0.1:9103",
		"route", "stable-key",
	}
	out1, _, code1 := runCapture(args)
	out2, _, code2 := runCapture(args)
	if code1 != 0 || code2 != 0 {
		t.Fatalf("expected exit 0 for both calls, got %d and %d", code1, code2)
	}
	if out1 != out2 {
		t.Errorf("route is not deterministic:\nfirst:  %q\nsecond: %q", out1, out2)
	}
}
