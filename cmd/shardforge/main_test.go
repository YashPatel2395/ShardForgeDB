package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// executeCmd runs newRootCmd() with the given args and returns combined output
// and any error returned by Execute(). Errors and usage are silenced on the
// command so the caller can inspect them cleanly via the returned error.
func executeCmd(args []string) (string, error) {
	var buf bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestHelp(t *testing.T) {
	out, err := executeCmd([]string{"--help"})
	if err != nil {
		t.Fatalf("--help returned error: %v", err)
	}
	for _, want := range []string{"shardforge", "Usage:", "version"} {
		if !strings.Contains(out, want) {
			t.Errorf("help output missing %q\nfull output:\n%s", want, out)
		}
	}
}

func TestVersion(t *testing.T) {
	out, err := executeCmd([]string{"version"})
	if err != nil {
		t.Fatalf("version returned error: %v", err)
	}
	want := fmt.Sprintf("ShardForgeDB %s", version)
	if !strings.Contains(out, want) {
		t.Errorf("version output = %q, want to contain %q", out, want)
	}
}

func TestUnknownCommand(t *testing.T) {
	_, err := executeCmd([]string{"totally-unknown-command"})
	if err == nil {
		t.Fatal("expected error for unknown command, got nil")
	}
}
