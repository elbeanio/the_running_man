package config

import (
	"os"
	"path/filepath"
	"strings"
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
		DockerCompose: DockerComposeConfig{Files: []string{"docker-compose.yml"}},
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

	if got := cfg.DockerCompose.PrimaryFile(); got != "docker-compose.yml" {
		t.Errorf("Expected docker_compose 'docker-compose.yml', got '%s'", got)
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

func TestConfig_ProcessConfig_NewFields(t *testing.T) {
	// Test YAML parsing with new fields
	yamlData := `
processes:
  - name: frontend
    type: web
    description: "React frontend with Vite"
    url: http://localhost:5173
    command: npm run dev
  - name: backend
    type: api
    description: "Go API server"
    command: go run main.go
    restart_on_crash: true
  - name: worker
    type: worker
    command: python worker.py
`

	var cfg Config
	if err := yaml.Unmarshal([]byte(yamlData), &cfg); err != nil {
		t.Fatalf("Failed to parse YAML with new fields: %v", err)
	}

	if len(cfg.Processes) != 3 {
		t.Fatalf("Expected 3 processes, got %d", len(cfg.Processes))
	}

	// Check frontend process
	frontend := cfg.Processes[0]
	if frontend.Name != "frontend" {
		t.Errorf("Expected name 'frontend', got %s", frontend.Name)
	}
	if frontend.Type != "web" {
		t.Errorf("Expected type 'web', got %s", frontend.Type)
	}
	if frontend.Description != "React frontend with Vite" {
		t.Errorf("Expected description 'React frontend with Vite', got %s", frontend.Description)
	}
	if frontend.URL != "http://localhost:5173" {
		t.Errorf("Expected URL 'http://localhost:5173', got %s", frontend.URL)
	}

	// Check backend process
	backend := cfg.Processes[1]
	if backend.Type != "api" {
		t.Errorf("Expected type 'api', got %s", backend.Type)
	}
	if !backend.RestartOnCrash {
		t.Error("Expected restart_on_crash to be true for backend")
	}

	// Check worker process (has type but no description/url)
	worker := cfg.Processes[2]
	if worker.Type != "worker" {
		t.Errorf("Expected type 'worker', got %s", worker.Type)
	}
	if worker.Description != "" {
		t.Errorf("Expected empty description for worker, got %s", worker.Description)
	}
	if worker.URL != "" {
		t.Errorf("Expected empty URL for worker, got %s", worker.URL)
	}

	// Test conversion to process.ProcessConfig
	processConfigs := cfg.ToProcessConfigs()
	if len(processConfigs) != 3 {
		t.Fatalf("Expected 3 process configs, got %d", len(processConfigs))
	}

	// Verify conversion preserved fields
	if processConfigs[0].Type != "web" {
		t.Errorf("Conversion failed: expected type 'web', got %s", processConfigs[0].Type)
	}
	if processConfigs[0].Description != "React frontend with Vite" {
		t.Errorf("Conversion failed: expected description 'React frontend with Vite', got %s", processConfigs[0].Description)
	}
}

// Review findings R6 and R12, plus the same defect in max_span_age.
//
// Validate() checked only that these durations *parsed*. time.ParseDuration
// accepts negatives, so each of these got through and failed later:
//
//   - interval: -1m   -> time.NewTicker panics ("non-positive interval for
//     NewTicker"), crashing the tool after startup, once it was already
//     supervising processes.
//   - retention: -5m  -> eviction cutoff at or after "now", so every entry is
//     discarded on arrival and the buffer is permanently empty, silently.
//   - max_span_age: -5m -> the same, for spans.

func TestConfig_Validate_RejectsNonPositiveInterval(t *testing.T) {
	for _, interval := range []string{"-1m", "0s", "0", "-1ns"} {
		cfg := &Config{
			Processes: []ProcessConfig{{Name: "ticker", Command: "echo hi", Interval: interval}},
		}
		err := cfg.Validate()
		if err == nil {
			t.Errorf("interval %q should be rejected: time.NewTicker panics on non-positive durations", interval)
			continue
		}
		if !strings.Contains(err.Error(), "non-positive interval") {
			t.Errorf("interval %q: unhelpful error %q", interval, err)
		}
	}
}

func TestConfig_Validate_AcceptsPositiveInterval(t *testing.T) {
	for _, interval := range []string{"30s", "1m", "1h", "500ms"} {
		cfg := &Config{
			Processes: []ProcessConfig{{Name: "ticker", Command: "echo hi", Interval: interval}},
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("interval %q should be accepted, got: %v", interval, err)
		}
	}
}

func TestConfig_Validate_RejectsNonPositiveRetention(t *testing.T) {
	for _, retention := range []string{"-5m", "0s", "0"} {
		cfg := &Config{
			Processes: []ProcessConfig{{Name: "x", Command: "echo hi"}},
			Retention: retention,
		}
		err := cfg.Validate()
		if err == nil {
			t.Errorf("retention %q should be rejected: it would discard every entry on arrival", retention)
			continue
		}
		if !strings.Contains(err.Error(), "retention must be positive") {
			t.Errorf("retention %q: unhelpful error %q", retention, err)
		}
	}
}

func TestTracingConfig_Validate_RejectsNonPositiveMaxSpanAge(t *testing.T) {
	for _, age := range []string{"-5m", "0s"} {
		tc := &TracingConfig{MaxSpanAge: age}
		err := tc.Validate()
		if err == nil {
			t.Errorf("max_span_age %q should be rejected: it would discard every span on arrival", age)
			continue
		}
		if !strings.Contains(err.Error(), "max_span_age must be positive") {
			t.Errorf("max_span_age %q: unhelpful error %q", age, err)
		}
	}
}

// Review finding R13: the shell was validated against a hardcoded allowlist of
// five absolute paths, which rejected most shells people actually have --
// Homebrew bash at /opt/homebrew/bin/bash or /usr/local/bin/bash, /bin/dash,
// fish, anything under Nix -- while the README promised "any shell". It also
// ACCEPTED /usr/bin/bash and /usr/bin/zsh, which do not exist on macOS, so it
// permitted shells that could not run.
//
// Now validated on the property that matters: an absolute path to something
// executable.
func TestConfig_Validate_ShellMustBeExecutable(t *testing.T) {
	base := []ProcessConfig{{Name: "x", Command: "echo hi"}}

	// /bin/sh exists and is executable everywhere this runs.
	cfg := &Config{Processes: base, Shell: "/bin/sh"}
	if err := cfg.Validate(); err != nil {
		t.Errorf("/bin/sh should be accepted: %v", err)
	}

	// A real executable outside the old allowlist.
	tmp := filepath.Join(t.TempDir(), "myshell")
	if err := os.WriteFile(tmp, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	cfg = &Config{Processes: base, Shell: tmp}
	if err := cfg.Validate(); err != nil {
		t.Errorf("an executable outside the old allowlist should be accepted: %v", err)
	}

	// Not executable.
	notExec := filepath.Join(t.TempDir(), "notexec")
	if err := os.WriteFile(notExec, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	cfg = &Config{Processes: base, Shell: notExec}
	if err := cfg.Validate(); err == nil {
		t.Error("a non-executable file should be rejected")
	}

	// Does not exist -- the old allowlist would have accepted /usr/bin/bash on
	// macOS despite it being absent.
	cfg = &Config{Processes: base, Shell: "/definitely/not/here/bash"}
	if err := cfg.Validate(); err == nil {
		t.Error("a non-existent shell should be rejected")
	}

	// A directory.
	cfg = &Config{Processes: base, Shell: t.TempDir()}
	if err := cfg.Validate(); err == nil {
		t.Error("a directory should be rejected")
	}

	// Relative path.
	cfg = &Config{Processes: base, Shell: "bash"}
	if err := cfg.Validate(); err == nil {
		t.Error("a relative path should be rejected")
	}
}
