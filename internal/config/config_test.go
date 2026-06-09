package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefault(t *testing.T) {
	cfg := Default()

	if cfg.Server.Host != "127.0.0.1" {
		t.Errorf("Server.Host = %q, want 127.0.0.1", cfg.Server.Host)
	}
	if cfg.Server.Port != 7890 {
		t.Errorf("Server.Port = %d, want 7890", cfg.Server.Port)
	}
	if cfg.Storage.DataDir != "./data" {
		t.Errorf("Storage.DataDir = %q, want ./data", cfg.Storage.DataDir)
	}
	if cfg.Log.Level != "info" {
		t.Errorf("Log.Level = %q, want info", cfg.Log.Level)
	}
	if cfg.Log.Format != "json" {
		t.Errorf("Log.Format = %q, want json", cfg.Log.Format)
	}
}

func TestLoad_ValidFile(t *testing.T) {
	yaml := `
server:
  host: "0.0.0.0"
  port: 9000
storage:
  data_dir: "/tmp/shardforge"
log:
  level: "debug"
  format: "text"
`
	path := writeTempFile(t, yaml)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("Server.Host = %q, want 0.0.0.0", cfg.Server.Host)
	}
	if cfg.Server.Port != 9000 {
		t.Errorf("Server.Port = %d, want 9000", cfg.Server.Port)
	}
	if cfg.Storage.DataDir != "/tmp/shardforge" {
		t.Errorf("Storage.DataDir = %q, want /tmp/shardforge", cfg.Storage.DataDir)
	}
	if cfg.Log.Level != "debug" {
		t.Errorf("Log.Level = %q, want debug", cfg.Log.Level)
	}
	if cfg.Log.Format != "text" {
		t.Errorf("Log.Format = %q, want text", cfg.Log.Format)
	}
}

func TestLoad_PartialFile_UsesDefaults(t *testing.T) {
	// Only override the port; everything else should fall back to defaults.
	yaml := `
server:
  port: 8080
`
	path := writeTempFile(t, yaml)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("Server.Port = %d, want 8080", cfg.Server.Port)
	}
	// Defaults should be preserved for fields not in the file.
	if cfg.Server.Host != "127.0.0.1" {
		t.Errorf("Server.Host = %q, want default 127.0.0.1", cfg.Server.Host)
	}
	if cfg.Log.Level != "info" {
		t.Errorf("Log.Level = %q, want default info", cfg.Log.Level)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	// An unclosed flow mapping is structurally invalid YAML.
	path := writeTempFile(t, "server: {host: unclosed")

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestLoad_InvalidPort(t *testing.T) {
	yaml := `
server:
  port: 99999
`
	path := writeTempFile(t, yaml)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected validation error for port 99999, got nil")
	}
}

func TestLoad_InvalidLogLevel(t *testing.T) {
	yaml := `
log:
  level: "verbose"
`
	path := writeTempFile(t, yaml)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected validation error for unknown log level, got nil")
	}
}

func TestLoad_InvalidLogFormat(t *testing.T) {
	yaml := `
log:
  format: "xml"
`
	path := writeTempFile(t, yaml)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected validation error for unknown log format, got nil")
	}
}

// writeTempFile writes content to a temporary file and returns its path.
// The file is removed automatically when the test completes.
func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writeTempFile: %v", err)
	}
	return path
}
