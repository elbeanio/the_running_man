package docker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseComposeFile(t *testing.T) {
	// Create a temporary test docker-compose.yml
	tmpDir := t.TempDir()
	composePath := filepath.Join(tmpDir, "docker-compose.yml")

	composeContent := `version: "3.8"
services:
  web:
    image: nginx:latest
    ports:
      - "80:80"
  db:
    image: postgres:13
    environment:
      POSTGRES_PASSWORD: secret
  redis:
    image: redis:alpine
`

	err := os.WriteFile(composePath, []byte(composeContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test compose file: %v", err)
	}

	// Parse the file
	compose, err := ParseComposeFile(composePath)
	if err != nil {
		t.Fatalf("Failed to parse compose file: %v", err)
	}

	// Verify version
	if compose.Version != "3.8" {
		t.Errorf("Expected version 3.8, got %s", compose.Version)
	}

	// Verify services
	if len(compose.Services) != 3 {
		t.Errorf("Expected 3 services, got %d", len(compose.Services))
	}

	// Check specific services exist
	if _, ok := compose.Services["web"]; !ok {
		t.Error("web service not found")
	}
	if _, ok := compose.Services["db"]; !ok {
		t.Error("db service not found")
	}
	if _, ok := compose.Services["redis"]; !ok {
		t.Error("redis service not found")
	}

	// Check service details
	if compose.Services["web"].Image != "nginx:latest" {
		t.Errorf("Expected nginx:latest image, got %s", compose.Services["web"].Image)
	}
}

func TestGetServiceNames(t *testing.T) {
	compose := &ComposeFile{
		Services: map[string]ComposeService{
			"web":   {Image: "nginx"},
			"db":    {Image: "postgres"},
			"cache": {Image: "redis"},
		},
	}

	names := compose.GetServiceNames()

	if len(names) != 3 {
		t.Errorf("Expected 3 service names, got %d", len(names))
	}

	// Check that all names are present (order doesn't matter)
	nameMap := make(map[string]bool)
	for _, name := range names {
		nameMap[name] = true
	}

	if !nameMap["web"] || !nameMap["db"] || !nameMap["cache"] {
		t.Errorf("Missing expected service names, got: %v", names)
	}
}

func TestParseComposeFile_InvalidPath(t *testing.T) {
	_, err := ParseComposeFile("/nonexistent/docker-compose.yml")
	if err == nil {
		t.Error("Expected error for nonexistent file, got nil")
	}
}

func TestParseComposeFile_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	composePath := filepath.Join(tmpDir, "invalid.yml")

	invalidContent := `this is not valid yaml: [[[`
	err := os.WriteFile(composePath, []byte(invalidContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	_, err = ParseComposeFile(composePath)
	if err == nil {
		t.Error("Expected error for invalid YAML, got nil")
	}
}

func TestParseComposeFile_NoServices(t *testing.T) {
	tmpDir := t.TempDir()
	composePath := filepath.Join(tmpDir, "empty.yml")

	emptyContent := `version: "3.8"
services: {}
`
	err := os.WriteFile(composePath, []byte(emptyContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	_, err = ParseComposeFile(composePath)
	if err == nil {
		t.Error("Expected error for compose file with no services")
	}
}

func TestParseComposeFile_WithContainerName(t *testing.T) {
	tmpDir := t.TempDir()
	composePath := filepath.Join(tmpDir, "docker-compose.yml")

	composeContent := `version: "3.8"
services:
  web:
    image: nginx
    container_name: my-nginx
`

	err := os.WriteFile(composePath, []byte(composeContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test compose file: %v", err)
	}

	compose, err := ParseComposeFile(composePath)
	if err != nil {
		t.Fatalf("Failed to parse compose file: %v", err)
	}

	if compose.Services["web"].ContainerName != "my-nginx" {
		t.Errorf("Expected container_name my-nginx, got %s", compose.Services["web"].ContainerName)
	}
}
