//go:build integration

// Tests against a real Docker daemon. Tagged, because they take seconds and
// need Docker: run them before review with
//
//	go test -tags integration ./internal/docker
package docker

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// The bug, end to end: a container's log stream ended when it stopped, and
// nothing attached a new one when it started again, so everything written
// after a restart was lost. Also exercises WatchEvents, which had no test
// that ever received an event.
func TestWatcher_ReattachesARestartedContainer(t *testing.T) {
	if !IsAvailable() {
		t.Skip("Docker daemon not available")
	}
	if err := exec.Command("docker", "image", "inspect", "alpine:latest").Run(); err != nil {
		t.Skip("alpine:latest not available locally")
	}

	project := fmt.Sprintf("rmtest%d", time.Now().UnixNano())
	// Each run prints its own marker, so lines from the second run are
	// distinguishable from the first.
	script := `run=$(cat /proc/sys/kernel/random/uuid); i=0; ` +
		`while true; do i=$((i+1)); echo "run=$run n=$i"; sleep 0.1; done`
	out, err := exec.Command("docker", "run", "-d",
		"--label", "com.docker.compose.project="+project,
		"--label", "com.docker.compose.service=ticker",
		"--name", project+"-ticker-1",
		"alpine:latest", "sh", "-c", script).Output()
	if err != nil {
		t.Fatalf("docker run: %v", err)
	}
	id := strings.TrimSpace(string(out))[:12]
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", id).Run() })

	client, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	var mu sync.Mutex
	runs := map[string]int{}
	seen := map[string]int{}
	handler := func(_ string, line string, _ time.Time, _ bool) {
		if f := strings.Fields(line); len(f) > 0 && strings.HasPrefix(f[0], "run=") {
			mu.Lock()
			runs[f[0]]++
			seen[line]++
			mu.Unlock()
		}
	}
	countRuns := func() int {
		mu.Lock()
		defer mu.Unlock()
		return len(runs)
	}

	discover := func() ([]Container, error) {
		return client.DiscoverContainersInProject(context.Background(), project, []string{"ticker"})
	}
	w := NewWatcher(client, project, []string{"ticker"}, time.Minute, handler, nil)
	defer func() { w.Stop(); w.Wait() }()

	containers, err := discover()
	if err != nil || len(containers) != 1 {
		t.Fatalf("discover: %v, %d containers", err, len(containers))
	}
	if err := w.Attach(containers[0]); err != nil {
		t.Fatal(err)
	}
	w.Watch(discover)

	waitUntil := func(cond func() bool) bool {
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if cond() {
				return true
			}
			time.Sleep(50 * time.Millisecond)
		}
		return false
	}
	if !waitUntil(func() bool { return countRuns() == 1 }) {
		t.Fatal("no output from the first run")
	}

	// -t 0: sh as PID 1 ignores SIGTERM, so the default would wait 10s.
	if err := exec.Command("docker", "restart", "-t", "0", id).Run(); err != nil {
		t.Fatalf("docker restart: %v", err)
	}
	if !waitUntil(func() bool { return countRuns() == 2 }) {
		t.Fatalf("no output captured from the run after the restart; runs seen: %v", runs)
	}

	// Reattaching replays from the new run's start, so no line of the first
	// run arrives a second time.
	mu.Lock()
	defer mu.Unlock()
	for line, n := range seen {
		if n > 1 {
			t.Errorf("%q captured %d times", line, n)
		}
	}
	t.Logf("lines per run: %v", runs)
}

// A one-shot service that ran to completion is done, not missing; one that
// failed, or one still running, is not "completed".
func TestCompletedServices(t *testing.T) {
	if !IsAvailable() {
		t.Skip("Docker daemon not available")
	}
	if err := exec.Command("docker", "image", "inspect", "alpine:latest").Run(); err != nil {
		t.Skip("alpine:latest not available locally")
	}
	c, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	project := fmt.Sprintf("rmdone%d", time.Now().UnixNano())
	run := func(service, script string) {
		out, err := exec.Command("docker", "run", "-d",
			"--label", "com.docker.compose.project="+project,
			"--label", "com.docker.compose.service="+service,
			"alpine:latest", "sh", "-c", script).Output()
		if err != nil {
			t.Fatalf("docker run: %v", err)
		}
		id := strings.TrimSpace(string(out))
		t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", id).Run() })
	}
	run("migrate", "exit 0")
	run("seed", "exit 1")
	run("server", "sleep 30")
	time.Sleep(time.Second)

	done, err := c.CompletedServices(context.Background(), project)
	if err != nil {
		t.Fatal(err)
	}
	if !done["migrate"] || done["seed"] || done["server"] {
		t.Errorf("completed = %v, want only migrate", done)
	}
}

// runService starts a container labelled as a Compose service, and removes it
// when the test ends.
func runService(t *testing.T, project string, extra []string, script string) string {
	t.Helper()
	args := append([]string{"run", "-d",
		"--label", "com.docker.compose.project=" + project,
		"--label", "com.docker.compose.service=svc",
		"--name", project + "-svc-1"}, extra...)
	args = append(args, "alpine:latest", "sh", "-c", script)
	out, err := exec.Command("docker", args...).Output()
	if err != nil {
		t.Fatalf("docker run: %v", err)
	}
	id := strings.TrimSpace(string(out))[:12]
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", id).Run() })
	return id
}

func requireDocker(t *testing.T) *Client {
	t.Helper()
	if !IsAvailable() {
		t.Skip("Docker daemon not available")
	}
	if err := exec.Command("docker", "image", "inspect", "alpine:latest").Run(); err != nil {
		t.Skip("alpine:latest not available locally")
	}
	c, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// A Compose healthcheck is run by Docker; readiness is Docker saying healthy.
func TestWaitServiceHealthy(t *testing.T) {
	c := requireDocker(t)
	project := fmt.Sprintf("rmhealth%d", time.Now().UnixNano())
	runService(t, project, []string{
		"--health-cmd", "test -f /tmp/ready", "--health-interval", "200ms", "--health-retries", "1",
	}, "sleep 1; touch /tmp/ready; sleep 30")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	start := time.Now()
	if err := c.WaitServiceHealthy(ctx, project, "svc"); err != nil {
		t.Fatalf("WaitServiceHealthy: %v", err)
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Error("passed before the healthcheck could have passed")
	}
}

// A log check passes on a line of the container's current run.
func TestWaitServiceLog(t *testing.T) {
	c := requireDocker(t)
	project := fmt.Sprintf("rmlog%d", time.Now().UnixNano())
	runService(t, project, nil, "sleep 0.5; echo 'Ready to accept connections'; sleep 30")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := c.WaitServiceLog(ctx, project, "svc", "Ready to accept"); err != nil {
		t.Fatalf("WaitServiceLog: %v", err)
	}

	never, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	if err := c.WaitServiceLog(never, project, "svc", "not in the log"); err == nil {
		t.Error("WaitServiceLog passed on text the container never wrote")
	}
}

// A line from before the current run started does not count: a restarted
// container's previous run said it was ready, and this one has not yet.
func TestWaitServiceLog_IgnoresThePreviousRun(t *testing.T) {
	c := requireDocker(t)
	project := fmt.Sprintf("rmlogrun%d", time.Now().UnixNano())
	// Ready only on the first run: the marker file survives a restart.
	id := runService(t, project, nil,
		"if [ ! -f /tmp/ran ]; then touch /tmp/ran; echo ready; fi; sleep 30")
	time.Sleep(500 * time.Millisecond)
	if err := exec.Command("docker", "restart", "-t", "0", id).Run(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	if err := c.WaitServiceLog(ctx, project, "svc", "ready"); err == nil {
		t.Error("passed on a line from the previous run")
	}
}
