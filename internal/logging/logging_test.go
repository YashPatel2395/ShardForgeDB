package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestNew_ReturnsLogger(t *testing.T) {
	logger := New("info", "json")
	if logger == nil {
		t.Fatal("New returned nil logger")
	}
}

func TestNewWithWriter_JSONFormat(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter(&buf, "info", "json")
	logger.Info("hello", "key", "value")

	out := buf.String()
	if !strings.Contains(out, `"msg":"hello"`) {
		t.Errorf("expected JSON output with msg field, got: %s", out)
	}
	if !strings.Contains(out, `"key":"value"`) {
		t.Errorf("expected JSON output with key field, got: %s", out)
	}
}

func TestNewWithWriter_TextFormat(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter(&buf, "info", "text")
	logger.Info("hello world")

	out := buf.String()
	if !strings.Contains(out, "hello world") {
		t.Errorf("expected text output containing message, got: %s", out)
	}
}

func TestNew_DebugLevelFiltersInfo(t *testing.T) {
	// A logger at Debug level should emit Debug messages.
	var buf bytes.Buffer
	logger := NewWithWriter(&buf, "debug", "json")
	logger.Debug("debug message")

	if !strings.Contains(buf.String(), "debug message") {
		t.Errorf("expected debug message in output, got: %s", buf.String())
	}
}

func TestNew_WarnLevelSuppressesInfo(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter(&buf, "warn", "json")
	logger.Info("should be suppressed")

	if buf.Len() != 0 {
		t.Errorf("expected no output at warn level for Info message, got: %s", buf.String())
	}
}

func TestNew_UnknownLevelDefaultsToInfo(t *testing.T) {
	level := parseLevel("nonsense")
	if level != slog.LevelInfo {
		t.Errorf("parseLevel(nonsense) = %v, want Info", level)
	}
}

func TestNew_UnknownFormatDefaultsToJSON(t *testing.T) {
	// Should not panic; just uses JSON.
	var buf bytes.Buffer
	logger := NewWithWriter(&buf, "info", "unknown")
	logger.Info("test")

	if !strings.Contains(buf.String(), `"msg":"test"`) {
		t.Errorf("expected JSON fallback output, got: %s", buf.String())
	}
}
