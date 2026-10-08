//go:build integration

// Against a real Docker daemon: go test -tags integration ./internal/health
package health

import (
	"context"
	"fmt"
	"os/exec"
	"testing"
	"time"
)

// The false positive this check was changed for, against Docker's own port
// forwarder: a published port with nothing listening inside must not pass,
// and must pass once something does.
func TestPort_ThroughDockersForwarder(t *testing.T) {
	if err := exec.Command("docker", "image", "inspect", "alpine:latest").Run(); err != nil {
		t.Skip("Docker or alpine:latest not available")
	}
	pollInterval = PollInterval
	defer func() { pollInterval = 10 * time.Millisecond }()

	port := 18000 + int(time.Now().UnixNano()%1000)
	name := fmt.Sprintf("rmport%d", time.Now().UnixNano())
	// Listens only after 3 seconds.
	if err := exec.Command("docker", "run", "-d", "--name", name, "-p", fmt.Sprintf("%d:9000", port),
		"alpine:latest", "sh", "-c", "sleep 3; nc -lk -p 9000 -e cat").Run(); err != nil {
		t.Fatalf("docker run: %v", err)
	}
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", name).Run() })
	start := time.Now()

	early, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	if err := Port(early, port); err == nil {
		t.Fatalf("passed %v after start, with nothing listening inside", time.Since(start).Round(time.Millisecond))
	}

	later, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel2()
	if err := Port(later, port); err != nil {
		t.Fatalf("never passed once the listener was up: %v", err)
	}
	if took := time.Since(start); took < 3*time.Second {
		t.Errorf("passed after %v, before the listener started at 3s", took)
	}
}
