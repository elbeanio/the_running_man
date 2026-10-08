//go:build integration

// Builds and runs the real binary: go test -tags integration ./cmd/running-man
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Closing the terminal Running Man runs in -- the window, or the tmux session
// -- sends it SIGHUP. Only SIGINT and SIGTERM were handled, so SIGHUP ended
// Running Man without stopping anything it had started: nine processes
// survived two runs in a tmux session that was killed. Each signal must stop
// the managed processes.
func TestSignalsStopManagedProcesses(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "running-man")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	for _, sig := range []syscall.Signal{syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT} {
		t.Run(sig.String(), func(t *testing.T) {
			// Short: the instance socket lives in here, and sun_path is small.
			dir, err := os.MkdirTemp("", "rmsig")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)

			// The child records its own pid; a script file, because Running Man
			// expands $ in config commands.
			script := filepath.Join(dir, "child.sh")
			pidFile := filepath.Join(dir, "child.pid")
			if err := os.WriteFile(script, []byte("#!/bin/sh\necho $$ > "+pidFile+"\nexec sleep 600\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			cfg := "processes:\n  - name: child\n    command: " + script + "\n"
			if err := os.WriteFile(filepath.Join(dir, "running-man.yml"), []byte(cfg), 0o644); err != nil {
				t.Fatal(err)
			}

			rm := exec.Command(bin, "run", "--no-tui", "--tracing=false")
			rm.Dir = dir
			if err := rm.Start(); err != nil {
				t.Fatal(err)
			}
			exited := make(chan struct{})
			go func() { _ = rm.Wait(); close(exited) }()
			defer func() { _ = rm.Process.Kill() }()

			var child int
			deadline := time.Now().Add(5 * time.Second)
			for child == 0 && time.Now().Before(deadline) {
				if b, err := os.ReadFile(pidFile); err == nil {
					child, _ = strconv.Atoi(strings.TrimSpace(string(b)))
				}
				time.Sleep(20 * time.Millisecond)
			}
			if child == 0 {
				t.Fatal("the managed process never started")
			}
			defer func() { _ = syscall.Kill(child, syscall.SIGKILL) }()

			if err := rm.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}

			select {
			case <-exited:
			case <-time.After(10 * time.Second):
				t.Fatalf("Running Man did not exit on %v", sig)
			}
			deadline = time.Now().Add(5 * time.Second)
			for syscall.Kill(child, 0) == nil {
				if time.Now().After(deadline) {
					t.Fatalf("pid %d outlived Running Man after %v", child, sig)
				}
				time.Sleep(50 * time.Millisecond)
			}
		})
	}
}
