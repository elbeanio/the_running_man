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
