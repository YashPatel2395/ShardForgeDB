// Package config handles loading and validating ShardForgeDB configuration.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration for ShardForgeDB.
type Config struct {
	Server  ServerConfig  `yaml:"server"`
	Storage StorageConfig `yaml:"storage"`
	Log     LogConfig     `yaml:"log"`
}

// ServerConfig controls network listener settings.
type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

// StorageConfig controls on-disk storage settings.
// Fields are reserved for future phases; no storage is implemented yet.
type StorageConfig struct {
	DataDir string `yaml:"data_dir"`
}

// LogConfig controls logging behaviour.
type LogConfig struct {
	// Level is one of: debug, info, warn, error.
	Level string `yaml:"level"`
	// Format is one of: json, text.
	Format string `yaml:"format"`
}

// Default returns a Config populated with sensible defaults.
func Default() *Config {
	return &Config{
		Server: ServerConfig{
			Host: "127.0.0.1",
			Port: 7890,
		},
		Storage: StorageConfig{
			DataDir: "./data",
		},
		Log: LogConfig{
			Level:  "info",
			Format: "json",
		},
	}
}

// Load reads a YAML config file at path and returns the parsed Config.
// Fields not present in the file retain their default values.
func Load(path string) (*Config, error) {
	cfg := Default()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: reading %q: %w", path, err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("config: parsing %q: %w", path, err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("config: validation: %w", err)
	}

	return cfg, nil
}

// validate checks that required fields hold acceptable values.
func (c *Config) validate() error {
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port must be between 1 and 65535, got %d", c.Server.Port)
	}
	switch c.Log.Level {
	case "debug", "info", "warn", "error":
		// valid
	default:
		return fmt.Errorf("log.level must be one of debug|info|warn|error, got %q", c.Log.Level)
	}
	switch c.Log.Format {
	case "json", "text":
		// valid
	default:
		return fmt.Errorf("log.format must be one of json|text, got %q", c.Log.Format)
	}
	return nil
}
