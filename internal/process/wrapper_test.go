package process

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestProcessWrapper_BasicExecution(t *testing.T) {
	var mu sync.Mutex
	var lines []string

	handler := func(source string, line string, timestamp time.Time, isStderr bool) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, line)
	}

	// Use echo to output some text
	wrapper := New("test-echo", "echo", []string{"hello", "world"}, handler)

	if err := wrapper.Start(); err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	if err := wrapper.Wait(); err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Check we captured output
	mu.Lock()
	defer mu.Unlock()

	if len(lines) == 0 {
		t.Fatal("Expected to capture output, got none")
	}

	output := strings.Join(lines, " ")
	if !strings.Contains(output, "hello") || !strings.Contains(output, "world") {
		t.Errorf("Expected output to contain 'hello world', got: %s", output)
	}

	// Check exit code
	if code := wrapper.ExitCode(); code != 0 {
		t.Errorf("Expected exit code 0, got %d", code)
	}
}

func TestProcessWrapper_ErrorOutput(t *testing.T) {
	var mu sync.Mutex
	var stderrLines []string

	handler := func(source string, line string, timestamp time.Time, isStderr bool) {
		mu.Lock()
		defer mu.Unlock()
		if isStderr {
			stderrLines = append(stderrLines, line)
		}
	}

	// Use a shell command that writes to stderr
	wrapper := New("test-stderr", "sh", []string{"-c", "echo 'error message' >&2"}, handler)

	if err := wrapper.Start(); err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	if err := wrapper.Wait(); err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Check we captured stderr
	mu.Lock()
	defer mu.Unlock()

	if len(stderrLines) == 0 {
		t.Fatal("Expected to capture stderr output, got none")
	}

	output := strings.Join(stderrLines, " ")
	if !strings.Contains(output, "error message") {
		t.Errorf("Expected stderr to contain 'error message', got: %s", output)
	}
}

func TestProcessWrapper_EnvironmentInheritance(t *testing.T) {
	var mu sync.Mutex
	var lines []string

	handler := func(source string, line string, timestamp time.Time, isStderr bool) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, line)
	}

	// Set a test environment variable
	t.Setenv("TEST_VAR", "test_value")

	// Use printenv to check the variable
	wrapper := New("test-env", "printenv", []string{"TEST_VAR"}, handler)

	if err := wrapper.Start(); err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	if err := wrapper.Wait(); err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Check environment variable was inherited
	mu.Lock()
	defer mu.Unlock()

	if len(lines) == 0 {
		t.Fatal("Expected to capture output, got none")
	}

	output := strings.Join(lines, " ")
	if !strings.Contains(output, "test_value") {
		t.Errorf("Expected output to contain 'test_value', got: %s", output)
	}
}

func TestProcessWrapper_NonZeroExit(t *testing.T) {
	wrapper := New("test-fail", "sh", []string{"-c", "exit 42"}, nil)

	if err := wrapper.Start(); err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Wait should return an error for non-zero exit
	err := wrapper.Wait()
	if err == nil {
		t.Error("Expected error for non-zero exit code")
	}

	// Check exit code
	if code := wrapper.ExitCode(); code != 42 {
		t.Errorf("Expected exit code 42, got %d", code)
	}
}

// Batch 4: Helper method tests

func TestPID_NotStarted(t *testing.T) {
	wrapper := New("test", "echo", []string{"hi"}, nil)
	// Don't call Start()

	pid := wrapper.PID()
	if pid != -1 {
		t.Errorf("Expected PID=-1 before start, got %d", pid)
	}
}

func TestPID_AfterStart(t *testing.T) {
	wrapper := New("test", "sleep", []string{"2"}, nil)

	if err := wrapper.Start(); err != nil {
		t.Fatalf("Failed to start: %v", err)
	}
	defer wrapper.Stop()

	pid := wrapper.PID()
	if pid <= 0 {
		t.Errorf("Expected positive PID after start, got %d", pid)
	}
}

func TestGetStatus_Running(t *testing.T) {
	wrapper := New("test", "sleep", []string{"2"}, nil)

	if err := wrapper.Start(); err != nil {
		t.Fatalf("Failed to start: %v", err)
	}
	defer wrapper.Stop()

	if err := waitForPID(wrapper, 1*time.Second); err != nil {
		t.Fatalf("Process didn't start: %v", err)
	}

	status := wrapper.GetStatus()
	if status != "running" {
		t.Errorf("Expected status 'running', got '%s'", status)
	}
}

func TestGetStatus_Stopped(t *testing.T) {
	wrapper := New("test", "echo", []string{"done"}, nil)

	if err := wrapper.Start(); err != nil {
		t.Fatalf("Failed to start: %v", err)
	}

	wrapper.Wait() // Wait for completion

	status := wrapper.GetStatus()
	if status != "stopped" {
		t.Errorf("Expected status 'stopped', got '%s'", status)
	}
}

func TestGetStatus_Failed(t *testing.T) {
	wrapper := New("test", "sh", []string{"-c", "exit 1"}, nil)

	if err := wrapper.Start(); err != nil {
		t.Fatalf("Failed to start: %v", err)
	}

	wrapper.Wait() // Will error but that's expected

	status := wrapper.GetStatus()
	if status != "failed" {
		t.Errorf("Expected status 'failed', got '%s'", status)
	}
}

func TestIsRunning_NotStarted(t *testing.T) {
	wrapper := New("test", "echo", []string{"hi"}, nil)
	// Don't start

	running := wrapper.IsRunning()
	// Process not started yet, so ProcessState is nil -> returns true
	if !running {
		t.Error("Expected IsRunning=true for not-yet-started process")
	}
}

func TestIsRunning_AfterExit(t *testing.T) {
	wrapper := New("test", "echo", []string{"done"}, nil)

	if err := wrapper.Start(); err != nil {
		t.Fatalf("Failed to start: %v", err)
	}

	wrapper.Wait()

	running := wrapper.IsRunning()
	if running {
		t.Error("Expected IsRunning=false after process exits")
	}
}

func TestStartTime(t *testing.T) {
	before := time.Now()

	wrapper := New("test", "echo", []string{"test"}, nil)

	if err := wrapper.Start(); err != nil {
		t.Fatalf("Failed to start: %v", err)
	}
	defer wrapper.Stop()

	after := time.Now()
	startTime := wrapper.StartTime()

	if startTime.Before(before) || startTime.After(after) {
		t.Errorf("Start time %v not between %v and %v", startTime, before, after)
	}
}

func TestCommandString_NoArgs(t *testing.T) {
	wrapper := New("test", "echo", nil, nil)

	cmd := wrapper.CommandString()
	if cmd != "echo" {
		t.Errorf("Expected 'echo', got '%s'", cmd)
	}
}

func TestCommandString_WithArgs(t *testing.T) {
	wrapper := New("test", "echo", []string{"hello", "world"}, nil)

	cmd := wrapper.CommandString()
	expected := "echo hello world"
	if cmd != expected {
		t.Errorf("Expected '%s', got '%s'", expected, cmd)
	}
}

// Test helper functions for polling instead of sleeping

// waitForPID polls until the process has a valid PID or times out
func waitForPID(w *ProcessWrapper, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if w.PID() > 0 {
			return nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for process to start (PID still -1)")
}

// waitForExit polls until the process exits or times out
func waitForExit(w *ProcessWrapper, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !w.IsRunning() {
			return nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for process to exit")
}
