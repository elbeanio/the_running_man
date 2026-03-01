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
	"os"
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

	// Tracing defaults
	DefaultTracingEnabled = true
	DefaultTracingPort    = 4318
	DefaultMaxSpans       = 10000
	DefaultMaxSpanAge     = 30 * time.Minute

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

	// Tracing configuration
	Tracing TracingConfig `yaml:"tracing,omitempty"`
}

// TracingConfig represents OpenTelemetry tracing configuration
type TracingConfig struct {
	// Enable OTLP trace ingestion (default: true)
	Enabled bool `yaml:"enabled,omitempty"`

	// OTLP HTTP receiver port (default: 4318)
	Port int `yaml:"port,omitempty"`

	// Maximum number of spans to keep in memory (default: 10000)
	MaxSpans int `yaml:"max_spans,omitempty"`

	// Maximum age of spans to keep (e.g., "30m", "1h", "24h")
	// Default: 30m
	MaxSpanAge string `yaml:"max_span_age,omitempty"`
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

	// Validate tracing configuration
	if err := c.Tracing.Validate(); err != nil {
		return fmt.Errorf("tracing configuration error: %w", err)
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

// Validate validates tracing configuration
func (tc *TracingConfig) Validate() error {
	// Validate tracing port
	if tc.Port != 0 && (tc.Port < MinPort || tc.Port > MaxPort) {
		return fmt.Errorf("tracing port must be between %d and %d, got %d", MinPort, MaxPort, tc.Port)
	}

	// Validate max_spans
	if tc.MaxSpans < 0 {
		return fmt.Errorf("max_spans cannot be negative, got %d", tc.MaxSpans)
	}

	// Validate max_span_age duration if specified
	if tc.MaxSpanAge != "" {
		if _, err := time.ParseDuration(tc.MaxSpanAge); err != nil {
			return fmt.Errorf("invalid max_span_age duration '%s': %w", tc.MaxSpanAge, err)
		}
	}

	return nil
}

// GetTracingPort returns the tracing port or the default.
func (tc *TracingConfig) GetTracingPort() int {
	if tc.Port == 0 {
		return DefaultTracingPort
	}
	return tc.Port
}

// GetMaxSpans returns the max spans or the default.
func (tc *TracingConfig) GetMaxSpans() int {
	if tc.MaxSpans == 0 {
		return DefaultMaxSpans
	}
	return tc.MaxSpans
}

// GetMaxSpanAgeDuration returns the max span age duration or the default.
func (tc *TracingConfig) GetMaxSpanAgeDuration() time.Duration {
	if tc.MaxSpanAge == "" {
		return DefaultMaxSpanAge
	}
	duration, err := time.ParseDuration(tc.MaxSpanAge)
	if err != nil {
		// Should never happen if Validate() was called
		return DefaultMaxSpanAge
	}
	return duration
}

// IsEnabled returns whether tracing is enabled.
func (tc *TracingConfig) IsEnabled() bool {
	// DefaultTracingEnabled is true, so if Enabled is not explicitly set to false,
	// tracing is enabled
	return !(tc.Enabled == false) // Only false if explicitly set to false
}

// expandEnvVars expands environment variables in command fields.
// Supports ${VAR} and $VAR syntax using os.ExpandEnv().
// References to undefined variables are replaced by empty string.
func (c *Config) expandEnvVars() {
	for i := range c.Processes {
		// Expand environment variables in command
		c.Processes[i].Command = os.ExpandEnv(c.Processes[i].Command)

		// Also expand in args if they exist
		for j := range c.Processes[i].Args {
			c.Processes[i].Args[j] = os.ExpandEnv(c.Processes[i].Args[j])
		}
	}

	// Expand in shell if specified
	if c.Shell != "" {
		c.Shell = os.ExpandEnv(c.Shell)
	}

	// Expand in docker_compose path if specified
	if c.DockerCompose != "" {
		c.DockerCompose = os.ExpandEnv(c.DockerCompose)
	}
}
