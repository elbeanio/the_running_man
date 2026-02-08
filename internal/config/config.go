// Package config defines the schema for running-man.yml configuration file.
//
// Example usage:
//
//	var cfg Config
//	if err := yaml.Unmarshal(data, &cfg); err != nil { ... }
//	if err := cfg.Validate(); err != nil { ... }
//	procs := cfg.ToProcessConfigs()
//	port := cfg.GetAPIPort()
package config

import (
	"fmt"
	"time"

	"github.com/iangeorge/the_running_man/internal/process"
)

const (
	// Default configuration values
	DefaultAPIPort    = 9000
	DefaultRetention  = 30 * time.Minute
	DefaultMaxEntries = 10000
	DefaultMaxBytes   = 50 * 1024 * 1024 // 50MB
	DefaultShell      = "/bin/sh"

	// Port validation constants
	MinPort = 1
	MaxPort = 65535
)

// Config represents the complete running-man.yml configuration.
type Config struct {
	// Processes to run and manage
	Processes []ProcessConfig `yaml:"processes,omitempty"`

	// Path to docker-compose.yml file (optional)
	DockerCompose string `yaml:"docker_compose,omitempty"`

	// API server port (default: 9000)
	APIPort int `yaml:"api_port,omitempty"`

	// Log retention duration (e.g., "30m", "1h", "24h")
	// Default: 30m
	Retention string `yaml:"retention,omitempty"`

	// Maximum number of log entries to keep
	// Default: 10000
	MaxEntries int `yaml:"max_entries,omitempty"`

	// Maximum total bytes of logs to keep (e.g., 50MB)
	// Default: 52428800 (50MB)
	MaxBytes int64 `yaml:"max_bytes,omitempty"`

	// Shell to use for process execution (default: /bin/sh)
	// Examples: /bin/bash, /bin/zsh
	Shell string `yaml:"shell,omitempty"`
}

// ProcessConfig represents a single process configuration in YAML.
// This matches internal/process.ProcessConfig but with YAML tags.
type ProcessConfig struct {
	Name           string   `yaml:"name"`
	Command        string   `yaml:"command"`
	Args           []string `yaml:"args,omitempty"`
	RestartOnCrash bool     `yaml:"restart_on_crash,omitempty"`
}

// Validate checks the config for errors and returns validation errors.
func (c *Config) Validate() error {
	// Validate processes
	if len(c.Processes) == 0 && c.DockerCompose == "" {
		return fmt.Errorf("config must specify at least one process or docker_compose file")
	}

	// Check for duplicate process names
	names := make(map[string]bool)
	for _, proc := range c.Processes {
		if proc.Name == "" {
			return fmt.Errorf("process name cannot be empty")
		}
		if proc.Command == "" {
			return fmt.Errorf("process '%s' must have a command", proc.Name)
		}
		if names[proc.Name] {
			return fmt.Errorf("duplicate process name: '%s'", proc.Name)
		}
		names[proc.Name] = true
	}

	// Validate API port (0 means use default, so skip validation)
	if c.APIPort != 0 && (c.APIPort < MinPort || c.APIPort > MaxPort) {
		return fmt.Errorf("api_port must be between %d and %d, got %d", MinPort, MaxPort, c.APIPort)
	}

	// Validate shell if specified
	if c.Shell != "" {
		validShells := []string{"/bin/sh", "/bin/bash", "/bin/zsh", "/usr/bin/bash", "/usr/bin/zsh"}
		valid := false
		for _, s := range validShells {
			if c.Shell == s {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("shell must be one of [%s, %s, %s, %s, %s], got '%s'",
				"/bin/sh", "/bin/bash", "/bin/zsh", "/usr/bin/bash", "/usr/bin/zsh", c.Shell)
		}
	}

	// Validate retention duration if specified
	if c.Retention != "" {
		if _, err := time.ParseDuration(c.Retention); err != nil {
			return fmt.Errorf("invalid retention duration '%s': %w", c.Retention, err)
		}
	}

	// Validate max_entries
	if c.MaxEntries < 0 {
		return fmt.Errorf("max_entries cannot be negative, got %d", c.MaxEntries)
	}

	// Validate max_bytes
	if c.MaxBytes < 0 {
		return fmt.Errorf("max_bytes cannot be negative, got %d", c.MaxBytes)
	}

	return nil
}

// ToProcessConfigs converts config ProcessConfigs to process.ProcessConfigs.
// The shell from the config is applied to all processes.
func (c *Config) ToProcessConfigs() []process.ProcessConfig {
	shell := c.GetShell()
	result := make([]process.ProcessConfig, len(c.Processes))
	for i, proc := range c.Processes {
		result[i] = process.ProcessConfig{
			Name:           proc.Name,
			Command:        proc.Command,
			Args:           proc.Args,
			Shell:          shell,
			RestartOnCrash: proc.RestartOnCrash,
		}
	}
	return result
}

// GetRetentionDuration returns the retention duration or the default.
// Call Validate() before using this method to ensure the duration is valid.
func (c *Config) GetRetentionDuration() time.Duration {
	if c.Retention == "" {
		return DefaultRetention
	}
	duration, err := time.ParseDuration(c.Retention)
	if err != nil {
		// Should never happen if Validate() was called
		// Return default as fallback
		return DefaultRetention
	}
	return duration
}

// GetAPIPort returns the API port or the default.
func (c *Config) GetAPIPort() int {
	if c.APIPort == 0 {
		return DefaultAPIPort
	}
	return c.APIPort
}

// GetMaxEntries returns the max entries or the default.
func (c *Config) GetMaxEntries() int {
	if c.MaxEntries == 0 {
		return DefaultMaxEntries
	}
	return c.MaxEntries
}

// GetMaxBytes returns the max bytes or the default.
func (c *Config) GetMaxBytes() int64 {
	if c.MaxBytes == 0 {
		return DefaultMaxBytes
	}
	return c.MaxBytes
}

// GetShell returns the shell to use or the default.
func (c *Config) GetShell() string {
	if c.Shell == "" {
		return DefaultShell
	}
	return c.Shell
}
