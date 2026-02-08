package process

import (
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
