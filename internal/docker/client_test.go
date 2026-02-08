package docker

import (
	"context"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	if !IsAvailable() {
		t.Skip("Docker daemon not available, skipping test")
	}

	client, err := NewClient()
	if err != nil {
		t.Fatalf("Failed to create Docker client: %v", err)
	}
	defer client.Close()

	if client.cli == nil {
		t.Error("Client.cli should not be nil")
	}
}

func TestPing(t *testing.T) {
	if !IsAvailable() {
		t.Skip("Docker daemon not available, skipping test")
	}

	client, err := NewClient()
	if err != nil {
		t.Fatalf("Failed to create Docker client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = client.Ping(ctx)
	if err != nil {
		t.Errorf("Ping failed: %v", err)
	}
}

func TestIsAvailable(t *testing.T) {
	// This test just checks that IsAvailable doesn't panic
	// The actual result depends on whether Docker is running
	available := IsAvailable()
	t.Logf("Docker available: %v", available)
}

func TestClose(t *testing.T) {
	if !IsAvailable() {
		t.Skip("Docker daemon not available, skipping test")
	}

	client, err := NewClient()
	if err != nil {
		t.Fatalf("Failed to create Docker client: %v", err)
	}

	// Close should not error
	err = client.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// Calling Close again should not panic
	err = client.Close()
	if err != nil {
		t.Errorf("Second Close failed: %v", err)
	}
}

func TestGetProjectNameFromPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "simple path",
			path:     "/home/user/myproject/docker-compose.yml",
			expected: "myproject",
		},
		{
			name:     "nested path",
			path:     "/var/app/services/web/docker-compose.yml",
			expected: "web",
		},
		{
			name:     "path with underscores",
			path:     "/home/my_project/docker-compose.yml",
			expected: "my-project", // Docker converts _ to -
		},
		{
			name:     "path with uppercase",
			path:     "/home/MyProject/docker-compose.yml",
			expected: "myproject", // Docker converts to lowercase
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetProjectNameFromPath(tt.path)
			if result != tt.expected {
				t.Errorf("GetProjectNameFromPath(%q) = %q, want %q", tt.path, result, tt.expected)
			}
		})
	}
}

func TestDiscoverContainers(t *testing.T) {
	if !IsAvailable() {
		t.Skip("Docker daemon not available, skipping test")
	}

	client, err := NewClient()
	if err != nil {
		t.Fatalf("Failed to create Docker client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// This test will typically find no containers (unless a compose project is running)
	// We just verify the function doesn't error
	containers, err := client.DiscoverContainers(ctx, "/tmp/test/docker-compose.yml", []string{"web", "db"})
	if err != nil {
		t.Errorf("DiscoverContainers failed: %v", err)
	}

	// Log how many containers were found (could be 0 and that's ok)
	t.Logf("Found %d containers", len(containers))
}
