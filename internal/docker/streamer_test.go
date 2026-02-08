package docker

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNewContainerStreamer(t *testing.T) {
	if !IsAvailable() {
		t.Skip("Docker daemon not available, skipping test")
	}

	client, err := NewClient()
	if err != nil {
		t.Fatalf("Failed to create Docker client: %v", err)
	}
	defer client.Close()

	streamer := NewContainerStreamer(client, "test-container", "test", nil)
	if streamer == nil {
		t.Fatal("NewContainerStreamer returned nil")
	}

	if streamer.containerID != "test-container" {
		t.Errorf("Expected containerID 'test-container', got '%s'", streamer.containerID)
	}

	if streamer.name != "test" {
		t.Errorf("Expected name 'test', got '%s'", streamer.name)
	}
}

func TestContainerStreamer_StartStop(t *testing.T) {
	if !IsAvailable() {
		t.Skip("Docker daemon not available, skipping test")
	}

	client, err := NewClient()
	if err != nil {
		t.Fatalf("Failed to create Docker client: %v", err)
	}
	defer client.Close()

	// Try to start a streamer (will fail if no container exists, which is expected)
	streamer := NewContainerStreamer(client, "nonexistent", "test", nil)

	// Start will return an error for nonexistent container
	err = streamer.Start()
	if err == nil {
		// If somehow it succeeded, stop it
		streamer.Stop()
		streamer.Wait()
	}
	// Either way, this tests that the API doesn't panic
}

func TestContainerStreamer_WithHandler(t *testing.T) {
	if !IsAvailable() {
		t.Skip("Docker daemon not available, skipping test")
	}

	client, err := NewClient()
	if err != nil {
		t.Fatalf("Failed to create Docker client: %v", err)
	}
	defer client.Close()

	// Test that handler is called (if we had a running container)
	var mu sync.Mutex
	var lines []string

	handler := func(source string, line string, timestamp time.Time, isStderr bool) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, line)
	}

	streamer := NewContainerStreamer(client, "test", "test-container", handler)
	if streamer.handler == nil {
		t.Error("Handler not set on streamer")
	}
}

// TestContainerStreamer_Integration is an integration test that requires:
// - Docker daemon running
// - A test container to be started
// This test is normally skipped but can be run manually for integration testing
func TestContainerStreamer_Integration(t *testing.T) {
	t.Skip("Integration test - requires manual setup")

	if !IsAvailable() {
		t.Skip("Docker daemon not available")
	}

	client, err := NewClient()
	if err != nil {
		t.Fatalf("Failed to create Docker client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// Start a simple test container
	// docker run --rm -d --name test-logger alpine sh -c "while true; do echo test-$(date +%s); sleep 1; done"
	// Note: This requires manual setup - we skip by default

	var mu sync.Mutex
	var lines []string

	handler := func(source string, line string, timestamp time.Time, isStderr bool) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, line)
		t.Logf("Received line: %s", line)
	}

	// Discover the test container
	containers, err := client.DiscoverContainers(ctx, "./docker-compose.yml", []string{"test-logger"})
	if err != nil || len(containers) == 0 {
		t.Skip("No test-logger container found - manual setup required")
	}

	container := containers[0]
	streamer := NewContainerStreamer(client, container.ID, container.Name, handler)

	err = streamer.Start()
	if err != nil {
		t.Fatalf("Failed to start streamer: %v", err)
	}

	// Let it run for a few seconds
	time.Sleep(3 * time.Second)

	// Stop the streamer
	err = streamer.Stop()
	if err != nil {
		t.Errorf("Failed to stop streamer: %v", err)
	}

	err = streamer.Wait()
	if err != nil {
		t.Errorf("Wait failed: %v", err)
	}

	// Check that we received some logs
	mu.Lock()
	defer mu.Unlock()

	if len(lines) == 0 {
		t.Error("No log lines received")
	}

	// Verify logs contain expected pattern
	found := false
	for _, line := range lines {
		if strings.Contains(line, "test-") {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("Expected log lines to contain 'test-', got: %v", lines)
	}
}
