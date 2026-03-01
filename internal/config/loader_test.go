package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_ValidFile(t *testing.T) {
	// Create temp config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test-config.yml")

	content := `
processes:
  - name: web
    command: npm start
  - name: api
    command: go run main.go
api_port: 8080
retention: 1h
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	// Load config
	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify contents
	if len(cfg.Processes) != 2 {
		t.Errorf("expected 2 processes, got %d", len(cfg.Processes))
	}
	if cfg.Processes[0].Name != "web" {
		t.Errorf("expected process name 'web', got '%s'", cfg.Processes[0].Name)
	}
	if cfg.GetAPIPort() != 8080 {
		t.Errorf("expected api_port 8080, got %d", cfg.GetAPIPort())
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	_, err := LoadConfig("/nonexistent/path/config.yml")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
	if !os.IsNotExist(err) && err.Error() == "" {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid.yml")

	content := `
processes:
  - name: web
    command: npm start
    invalid_indent
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	_, err := LoadConfig(configPath)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestLoadConfig_InvalidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid-config.yml")

	// Config with duplicate process names (invalid)
	content := `
processes:
  - name: web
    command: npm start
  - name: web
    command: npm run dev
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	_, err := LoadConfig(configPath)
	if err == nil {
		t.Fatal("expected validation error for duplicate process names")
	}
}

func TestLoadConfig_EmptyPath(t *testing.T) {
	// Create running-man.yml in temp dir
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, DefaultConfigFileName)

	content := `
processes:
  - name: test
    command: echo hello
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	// Change to temp dir
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	defer os.Chdir(oldDir)

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}

	// Load config with empty path (should find running-man.yml)
	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if len(cfg.Processes) != 1 {
		t.Errorf("expected 1 process, got %d", len(cfg.Processes))
	}
}

func TestFindConfig_InCurrentDir(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, DefaultConfigFileName)

	// Create config file
	if err := os.WriteFile(configPath, []byte("processes: []"), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	// Change to temp dir
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	defer os.Chdir(oldDir)

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}

	// Find config
	found := FindConfig()
	if found == "" {
		t.Fatal("expected to find config in current directory")
	}

	// Resolve symlinks for comparison (macOS /var -> /private/var)
	foundResolved, _ := filepath.EvalSymlinks(found)
	configResolved, _ := filepath.EvalSymlinks(configPath)
	if foundResolved != configResolved {
		t.Errorf("expected path %s, got %s", configResolved, foundResolved)
	}
}

func TestFindConfig_InParentDir(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, DefaultConfigFileName)
	subDir := filepath.Join(tmpDir, "subdir")

	// Create config in parent dir
	if err := os.WriteFile(configPath, []byte("processes: []"), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	// Create subdirectory
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("failed to create subdirectory: %v", err)
	}

	// Change to subdirectory
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	defer os.Chdir(oldDir)

	if err := os.Chdir(subDir); err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}

	// Find config (should find in parent)
	found := FindConfig()
	if found == "" {
		t.Fatal("expected to find config in parent directory")
	}

	// Resolve symlinks for comparison (macOS /var -> /private/var)
	foundResolved, _ := filepath.EvalSymlinks(found)
	configResolved, _ := filepath.EvalSymlinks(configPath)
	if foundResolved != configResolved {
		t.Errorf("expected path %s, got %s", configResolved, foundResolved)
	}
}

func TestFindConfig_NotFound(t *testing.T) {
	tmpDir := t.TempDir()

	// Change to temp dir with no config
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	defer os.Chdir(oldDir)

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}

	// Find config (should not find anything)
	found := FindConfig()
	if found != "" {
		t.Errorf("expected empty string, got %s", found)
	}
}

func TestLoadConfigOrDefault_ExplicitPath(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "custom-config.yml")

	content := `
processes:
  - name: test
    command: echo test
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	// Load with explicit path
	cfg, err := LoadConfigOrDefault(configPath)
	if err != nil {
		t.Fatalf("LoadConfigOrDefault failed: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected config, got nil")
	}
	if len(cfg.Processes) != 1 {
		t.Errorf("expected 1 process, got %d", len(cfg.Processes))
	}
}

func TestLoadConfigOrDefault_ExplicitPath_NotFound(t *testing.T) {
	// Explicit path that doesn't exist should return error
	_, err := LoadConfigOrDefault("/nonexistent/config.yml")
	if err == nil {
		t.Fatal("expected error for nonexistent explicit path")
	}
}

func TestLoadConfigOrDefault_Search_Found(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, DefaultConfigFileName)

	content := `
processes:
  - name: test
    command: echo test
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	// Change to temp dir
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	defer os.Chdir(oldDir)

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}

	// Load with empty path (should search and find)
	cfg, err := LoadConfigOrDefault("")
	if err != nil {
		t.Fatalf("LoadConfigOrDefault failed: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected config, got nil")
	}
}

func TestLoadConfigOrDefault_Search_NotFound(t *testing.T) {
	tmpDir := t.TempDir()

	// Change to temp dir with no config
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	defer os.Chdir(oldDir)

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}

	// Load with empty path (should return nil, no error)
	cfg, err := LoadConfigOrDefault("")
	if err != nil {
		t.Fatalf("LoadConfigOrDefault returned error: %v", err)
	}
	if cfg != nil {
		t.Errorf("expected nil config, got %+v", cfg)
	}
}

func TestLoadConfig_WithDockerCompose(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yml")

	content := `
docker_compose: docker-compose.yml
api_port: 9000
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.DockerCompose != "docker-compose.yml" {
		t.Errorf("expected docker_compose 'docker-compose.yml', got '%s'", cfg.DockerCompose)
	}
}

func TestLoadConfig_EnvVarExpansion(t *testing.T) {
	// Set up environment variables for testing
	t.Setenv("BUILD_MODE", "development")
	t.Setenv("PORT", "3000")
	t.Setenv("SHELL_PATH", "/bin/bash")
	t.Setenv("COMPOSE_FILE", "docker-compose.dev.yml")

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yml")

	content := `
processes:
  - name: web
    command: npm run ${BUILD_MODE}
    args:
      - "--port"
      - "${PORT}"
  - name: api
    command: go run main.go --env $BUILD_MODE  # $VAR syntax also works
docker_compose: ${COMPOSE_FILE}
shell: ${SHELL_PATH}
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify environment variable expansion
	if cfg.Processes[0].Command != "npm run development" {
		t.Errorf("expected command 'npm run development', got '%s'", cfg.Processes[0].Command)
	}

	if len(cfg.Processes[0].Args) != 2 || cfg.Processes[0].Args[1] != "3000" {
		t.Errorf("expected args ['--port', '3000'], got %v", cfg.Processes[0].Args)
	}

	// $BUILD_MODE should also expand
	if cfg.Processes[1].Command != "go run main.go --env development" {
		t.Errorf("expected command 'go run main.go --env development', got '%s'", cfg.Processes[1].Command)
	}

	if cfg.DockerCompose != "docker-compose.dev.yml" {
		t.Errorf("expected docker_compose 'docker-compose.dev.yml', got '%s'", cfg.DockerCompose)
	}

	if cfg.Shell != "/bin/bash" {
		t.Errorf("expected shell '/bin/bash', got '%s'", cfg.Shell)
	}
}

func TestLoadConfig_EnvVarExpansion_Undefined(t *testing.T) {
	// Test with undefined environment variables (should expand to empty string)
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yml")

	content := `
processes:
  - name: test
    command: echo ${UNDEFINED_VAR}
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Undefined environment variables should expand to empty string
	if cfg.Processes[0].Command != "echo " {
		t.Errorf("expected command 'echo ', got '%s'", cfg.Processes[0].Command)
	}
}
