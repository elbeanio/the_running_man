package config

import (
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestConfig_Validate_ValidConfig(t *testing.T) {
	cfg := &Config{
		Processes: []ProcessConfig{
			{Name: "web", Command: "npm start"},
			{Name: "worker", Command: "python", Args: []string{"worker.py"}},
		},
		APIPort:    8080,
		Retention:  "1h",
		MaxEntries: 5000,
		MaxBytes:   10000000,
		Shell:      "/bin/bash",
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("Valid config should not error: %v", err)
	}
}

func TestConfig_Validate_WithDockerCompose(t *testing.T) {
	cfg := &Config{
		DockerCompose: "docker-compose.yml",
		APIPort:       9000,
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("Config with docker_compose should be valid: %v", err)
	}
}

func TestConfig_Validate_EmptyConfig(t *testing.T) {
	cfg := &Config{}

	err := cfg.Validate()
	if err == nil {
		t.Error("Empty config should error")
	}
	if err.Error() != "config must specify at least one process or docker_compose file" {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestConfig_Validate_DuplicateProcessNames(t *testing.T) {
	cfg := &Config{
		Processes: []ProcessConfig{
			{Name: "web", Command: "npm start"},
			{Name: "web", Command: "python app.py"},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("Duplicate process names should error")
	}
	if err.Error() != "duplicate process name: 'web'" {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestConfig_Validate_EmptyProcessName(t *testing.T) {
	cfg := &Config{
		Processes: []ProcessConfig{
			{Name: "", Command: "npm start"},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("Empty process name should error")
	}
	if err.Error() != "process name cannot be empty" {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestConfig_Validate_EmptyProcessCommand(t *testing.T) {
	cfg := &Config{
		Processes: []ProcessConfig{
			{Name: "web", Command: ""},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("Empty process command should error")
	}
	if err.Error() != "process 'web' must have a command" {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestConfig_Validate_InvalidAPIPort(t *testing.T) {
	tests := []struct {
		port int
		name string
	}{
		{-1, "negative port"},
		{65536, "port too high"},
		{99999, "port way too high"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Processes: []ProcessConfig{{Name: "test", Command: "echo"}},
				APIPort:   tt.port,
			}

			err := cfg.Validate()
			if err == nil {
				t.Errorf("Invalid API port %d should error", tt.port)
			}
		})
	}
}

func TestConfig_Validate_InvalidRetention(t *testing.T) {
	cfg := &Config{
		Processes: []ProcessConfig{{Name: "test", Command: "echo"}},
		Retention: "not-a-duration",
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("Invalid retention duration should error")
	}
}

func TestConfig_Validate_NegativeMaxEntries(t *testing.T) {
	cfg := &Config{
		Processes:  []ProcessConfig{{Name: "test", Command: "echo"}},
		MaxEntries: -100,
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("Negative max_entries should error")
	}
}

func TestConfig_Validate_NegativeMaxBytes(t *testing.T) {
	cfg := &Config{
		Processes: []ProcessConfig{{Name: "test", Command: "echo"}},
		MaxBytes:  -100,
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("Negative max_bytes should error")
	}
}

func TestConfig_ToProcessConfigs(t *testing.T) {
	cfg := &Config{
		Processes: []ProcessConfig{
			{Name: "web", Command: "npm", Args: []string{"start"}},
			{Name: "worker", Command: "python worker.py"},
		},
	}

	procs := cfg.ToProcessConfigs()

	if len(procs) != 2 {
		t.Fatalf("Expected 2 processes, got %d", len(procs))
	}

	if procs[0].Name != "web" || procs[0].Command != "npm" || len(procs[0].Args) != 1 {
		t.Errorf("Process 0 not converted correctly: %+v", procs[0])
	}

	if procs[1].Name != "worker" || procs[1].Command != "python worker.py" {
		t.Errorf("Process 1 not converted correctly: %+v", procs[1])
	}
}

func TestConfig_GetRetentionDuration(t *testing.T) {
	tests := []struct {
		name      string
		retention string
		expected  time.Duration
	}{
		{"default", "", 30 * time.Minute},
		{"1 hour", "1h", 1 * time.Hour},
		{"30 seconds", "30s", 30 * time.Second},
		{"24 hours", "24h", 24 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Retention: tt.retention}
			got := cfg.GetRetentionDuration()
			if got != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, got)
			}
		})
	}
}

func TestConfig_GetAPIPort(t *testing.T) {
	tests := []struct {
		name     string
		port     int
		expected int
	}{
		{"default", 0, 9000},
		{"custom", 8080, 8080},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{APIPort: tt.port}
			got := cfg.GetAPIPort()
			if got != tt.expected {
				t.Errorf("Expected %d, got %d", tt.expected, got)
			}
		})
	}
}

func TestConfig_GetMaxEntries(t *testing.T) {
	tests := []struct {
		name     string
		entries  int
		expected int
	}{
		{"default", 0, 10000},
		{"custom", 5000, 5000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{MaxEntries: tt.entries}
			got := cfg.GetMaxEntries()
			if got != tt.expected {
				t.Errorf("Expected %d, got %d", tt.expected, got)
			}
		})
	}
}

func TestConfig_GetMaxBytes(t *testing.T) {
	tests := []struct {
		name     string
		bytes    int64
		expected int64
	}{
		{"default", 0, 50 * 1024 * 1024},
		{"custom", 100000000, 100000000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{MaxBytes: tt.bytes}
			got := cfg.GetMaxBytes()
			if got != tt.expected {
				t.Errorf("Expected %d, got %d", tt.expected, got)
			}
		})
	}
}

func TestConfig_GetShell(t *testing.T) {
	tests := []struct {
		name     string
		shell    string
		expected string
	}{
		{"default", "", "/bin/sh"},
		{"bash", "/bin/bash", "/bin/bash"},
		{"zsh", "/bin/zsh", "/bin/zsh"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Shell: tt.shell}
			got := cfg.GetShell()
			if got != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, got)
			}
		})
	}
}

func TestConfig_YAMLUnmarshal(t *testing.T) {
	yamlData := `
processes:
  - name: web
    command: npm start
  - name: worker
    command: python
    args:
      - worker.py
      - --verbose
docker_compose: docker-compose.yml
api_port: 8080
retention: 1h
max_entries: 5000
max_bytes: 10000000
shell: /bin/bash
`

	var cfg Config
	if err := yaml.Unmarshal([]byte(yamlData), &cfg); err != nil {
		t.Fatalf("Failed to unmarshal YAML: %v", err)
	}

	// Verify all fields
	if len(cfg.Processes) != 2 {
		t.Errorf("Expected 2 processes, got %d", len(cfg.Processes))
	}

	if cfg.Processes[0].Name != "web" {
		t.Errorf("Expected process name 'web', got '%s'", cfg.Processes[0].Name)
	}

	if cfg.Processes[1].Command != "python" {
		t.Errorf("Expected command 'python', got '%s'", cfg.Processes[1].Command)
	}

	if len(cfg.Processes[1].Args) != 2 {
		t.Errorf("Expected 2 args, got %d", len(cfg.Processes[1].Args))
	}

	if cfg.DockerCompose != "docker-compose.yml" {
		t.Errorf("Expected docker_compose 'docker-compose.yml', got '%s'", cfg.DockerCompose)
	}

	if cfg.APIPort != 8080 {
		t.Errorf("Expected api_port 8080, got %d", cfg.APIPort)
	}

	if cfg.Retention != "1h" {
		t.Errorf("Expected retention '1h', got '%s'", cfg.Retention)
	}

	if cfg.MaxEntries != 5000 {
		t.Errorf("Expected max_entries 5000, got %d", cfg.MaxEntries)
	}

	if cfg.MaxBytes != 10000000 {
		t.Errorf("Expected max_bytes 10000000, got %d", cfg.MaxBytes)
	}

	if cfg.Shell != "/bin/bash" {
		t.Errorf("Expected shell '/bin/bash', got '%s'", cfg.Shell)
	}

	// Verify validation passes
	if err := cfg.Validate(); err != nil {
		t.Errorf("Valid config should not error: %v", err)
	}
}

func TestConfig_YAMLUnmarshal_MinimalConfig(t *testing.T) {
	yamlData := `
processes:
  - name: app
    command: ./app
`

	var cfg Config
	if err := yaml.Unmarshal([]byte(yamlData), &cfg); err != nil {
		t.Fatalf("Failed to unmarshal YAML: %v", err)
	}

	if len(cfg.Processes) != 1 {
		t.Errorf("Expected 1 process, got %d", len(cfg.Processes))
	}

	// Check defaults are applied via getters
	if cfg.GetAPIPort() != 9000 {
		t.Errorf("Expected default api_port 9000, got %d", cfg.GetAPIPort())
	}

	if cfg.GetRetentionDuration() != 30*time.Minute {
		t.Errorf("Expected default retention 30m, got %v", cfg.GetRetentionDuration())
	}

	if cfg.GetMaxEntries() != 10000 {
		t.Errorf("Expected default max_entries 10000, got %d", cfg.GetMaxEntries())
	}

	if cfg.GetMaxBytes() != 50*1024*1024 {
		t.Errorf("Expected default max_bytes 50MB, got %d", cfg.GetMaxBytes())
	}

	if cfg.GetShell() != "/bin/sh" {
		t.Errorf("Expected default shell /bin/sh, got %s", cfg.GetShell())
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("Minimal config should be valid: %v", err)
	}
}
