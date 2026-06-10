package cluster

import (
	"encoding/json"
	"fmt"
	"os"
)

// Parse decodes JSON data into a Config, normalises defaults, and validates.
// Unknown JSON fields are silently ignored (permissive decoding).
func Parse(data []byte) (Config, error) {
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("%w: json decode: %v", ErrInvalidConfig, err)
	}
	cfg = Normalize(cfg)
	if err := Validate(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Marshal serialises cfg to indented JSON.
// It does not validate cfg before marshalling.
func Marshal(cfg Config) ([]byte, error) {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("cluster: marshal: %w", err)
	}
	return data, nil
}

// Load reads a JSON cluster config from path, normalises it, and validates it.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("cluster: load %q: %w", path, err)
	}
	cfg, err := Parse(data)
	if err != nil {
		return Config{}, fmt.Errorf("cluster: load %q: %w", path, err)
	}
	return cfg, nil
}

// Save writes cfg to path as indented JSON, creating or truncating the file.
// It does not validate cfg before writing.
func Save(path string, cfg Config) error {
	data, err := Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("cluster: save %q: %w", path, err)
	}
	return nil
}
