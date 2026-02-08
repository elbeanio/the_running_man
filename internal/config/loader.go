package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	// DefaultConfigFileName is the default name for the config file
	DefaultConfigFileName = "running-man.yml"
)

// LoadConfig loads and validates a configuration file.
// If configPath is empty, it searches for running-man.yml in the current directory.
// Returns an error if the file doesn't exist, can't be read, or is invalid.
func LoadConfig(configPath string) (*Config, error) {
	// Determine which file to load
	path := configPath
	if path == "" {
		// Look for default config in current directory
		path = DefaultConfigFileName
	}

	// Read the file
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("config file not found: %s", path)
		}
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	// Parse YAML
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", path, err)
	}

	// Validate
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config in %s: %w", path, err)
	}

	return &cfg, nil
}

// FindConfig searches for running-man.yml in the current directory and parent directories.
// Returns the absolute path to the config file, or empty string if not found.
func FindConfig() string {
	// Start from current directory
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}

	// Search up the directory tree
	for {
		configPath := filepath.Join(dir, DefaultConfigFileName)
		if _, err := os.Stat(configPath); err == nil {
			return configPath
		}

		// Move to parent directory
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached root directory
			break
		}
		dir = parent
	}

	return ""
}

// LoadConfigOrDefault attempts to load a config file.
// If configPath is specified, it loads that file (returns error if not found).
// If configPath is empty, it searches for running-man.yml (returns nil if not found).
// Returns (*Config, error) where Config may be nil if no config file is found.
func LoadConfigOrDefault(configPath string) (*Config, error) {
	// If explicit path provided, must exist
	if configPath != "" {
		return LoadConfig(configPath)
	}

	// Search for config file
	found := FindConfig()
	if found == "" {
		// No config file found, return nil (caller will use defaults)
		return nil, nil
	}

	// Load the found config
	return LoadConfig(found)
}
