package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type Config struct {
	// LogDirs contains directories that are scanned for log files.
	// Directories can include both application and infrastructure logs
	// (for example, PHP and nginx locations).
	LogDirs []string `json:"log_dirs"`
}

// defaultConfig returns a bootstrap configuration used when no user config exists.
func defaultConfig() Config {
	return Config{
		LogDirs: []string{
			"/home/yaonkey/Work/s/logs",
			"/home/yaonkey/Work/s/logs/nginx",
		},
	}
}

// LoadConfig reads JSON config from path.
// If the file does not exist, it writes and returns a default configuration.
func LoadConfig(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			cfg := defaultConfig()
			if writeErr := writeConfig(path, cfg); writeErr != nil {
				return Config{}, writeErr
			}
			return cfg, nil
		}
		return Config{}, err
	}

	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, err
	}

	if len(cfg.LogDirs) == 0 {
		cfg = defaultConfig()
	}

	normalized := make([]string, 0, len(cfg.LogDirs))
	for _, dir := range cfg.LogDirs {
		if dir == "" {
			continue
		}
		normalized = append(normalized, filepath.Clean(dir))
	}
	cfg.LogDirs = normalized

	return cfg, nil
}

// writeConfig persists config to disk in a stable, human-readable JSON format.
func writeConfig(path string, cfg Config) error {
	encoded, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	return os.WriteFile(path, encoded, 0o644)
}
