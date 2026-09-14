package process

import (
	"fmt"
	"os"
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
	wrapper := New("test-echo", "echo", []string{"hello", "world"}, "", handler)

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
	wrapper := New("test-stderr", "echo 'error message' >&2", []string{}, "", handler)

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
	wrapper := New("test-env", "printenv", []string{"TEST_VAR"}, "", handler)

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
	wrapper := New("test-fail", "exit 42", []string{}, "", nil)

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
	wrapper := New("test", "echo", []string{"hi"}, "", nil)
	// Don't call Start()

	pid := wrapper.PID()
	if pid != -1 {
		t.Errorf("Expected PID=-1 before start, got %d", pid)
	}
}

func TestPID_AfterStart(t *testing.T) {
	wrapper := New("test", "sleep", []string{"2"}, "", nil)

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
	wrapper := New("test", "sleep", []string{"2"}, "", nil)

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
	wrapper := New("test", "echo", []string{"done"}, "", nil)

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
	wrapper := New("test", "exit 1", []string{}, "", nil)

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
	wrapper := New("test", "echo", []string{"hi"}, "", nil)
	// Don't start

	running := wrapper.IsRunning()
	// Process not started yet, so ProcessState is nil -> returns true
	if !running {
		t.Error("Expected IsRunning=true for not-yet-started process")
	}
}

func TestIsRunning_AfterExit(t *testing.T) {
	wrapper := New("test", "echo", []string{"done"}, "", nil)

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

	wrapper := New("test", "echo", []string{"test"}, "", nil)

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
	wrapper := New("test", "echo", nil, "", nil)

	cmd := wrapper.CommandString()
	if cmd != "echo" {
		t.Errorf("Expected 'echo', got '%s'", cmd)
	}
}

func TestCommandString_WithArgs(t *testing.T) {
	wrapper := New("test", "echo", []string{"hello", "world"}, "", nil)

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

// Tests for shell feature support

func TestShellFeature_ChangeDirectory(t *testing.T) {
	var mu sync.Mutex
	var lines []string

	handler := func(source string, line string, timestamp time.Time, isStderr bool) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, line)
	}

	// Use cd to change directory and print working directory
	// This tests that shell execution works
	wrapper := New("test-cd", "cd /tmp && pwd", []string{}, "", handler)

	if err := wrapper.Start(); err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	if err := wrapper.Wait(); err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Check output contains /tmp
	mu.Lock()
	defer mu.Unlock()

	if len(lines) == 0 {
		t.Fatal("Expected to capture output, got none")
	}

	output := strings.Join(lines, "\n")
	if !strings.Contains(output, "/tmp") {
		t.Errorf("Expected output to contain '/tmp', got: %s", output)
	}

	// Check exit code
	if code := wrapper.ExitCode(); code != 0 {
		t.Errorf("Expected exit code 0, got %d", code)
	}
}

func TestShellFeature_CommandChaining(t *testing.T) {
	var mu sync.Mutex
	var lines []string

	handler := func(source string, line string, timestamp time.Time, isStderr bool) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, line)
	}

	// Test && chaining - both commands should execute
	wrapper := New("test-chain", "echo first && echo second", []string{}, "", handler)

	if err := wrapper.Start(); err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	if err := wrapper.Wait(); err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Check both outputs appear
	mu.Lock()
	defer mu.Unlock()

	if len(lines) < 2 {
		t.Fatalf("Expected at least 2 lines of output, got %d", len(lines))
	}

	output := strings.Join(lines, "\n")
	if !strings.Contains(output, "first") {
		t.Errorf("Expected output to contain 'first', got: %s", output)
	}
	if !strings.Contains(output, "second") {
		t.Errorf("Expected output to contain 'second', got: %s", output)
	}

	// Check exit code
	if code := wrapper.ExitCode(); code != 0 {
		t.Errorf("Expected exit code 0, got %d", code)
	}
}

func TestShellFeature_Pipes(t *testing.T) {
	var mu sync.Mutex
	var lines []string

	handler := func(source string, line string, timestamp time.Time, isStderr bool) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, line)
	}

	// Test pipes - echo then grep
	wrapper := New("test-pipe", "echo hello world | grep hello", []string{}, "", handler)

	if err := wrapper.Start(); err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	if err := wrapper.Wait(); err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Check output contains hello (grep should filter it)
	mu.Lock()
	defer mu.Unlock()

	if len(lines) == 0 {
		t.Fatal("Expected to capture output, got none")
	}

	output := strings.Join(lines, "\n")
	if !strings.Contains(output, "hello") {
		t.Errorf("Expected output to contain 'hello', got: %s", output)
	}

	// Check exit code
	if code := wrapper.ExitCode(); code != 0 {
		t.Errorf("Expected exit code 0, got %d", code)
	}
}

func TestShellFeature_ExitCodePropagation(t *testing.T) {
	var mu sync.Mutex
	var lines []string

	handler := func(source string, line string, timestamp time.Time, isStderr bool) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, line)
	}

	// Test that exit codes propagate correctly from shell
	// false command returns exit code 1
	wrapper := New("test-exit", "false", []string{}, "", handler)

	if err := wrapper.Start(); err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Wait should return an error for non-zero exit
	err := wrapper.Wait()
	if err == nil {
		t.Fatal("Expected Wait to return error for non-zero exit code")
	}

	// Check exit code is 1
	if code := wrapper.ExitCode(); code != 1 {
		t.Errorf("Expected exit code 1, got %d", code)
	}
}

func TestShellFeature_CommandStringDisplay(t *testing.T) {
	// Verify that CommandString returns the original command, not the shell wrapper
	wrapper := New("test-display", "cd /tmp && pwd", []string{}, "", nil)

	cmdStr := wrapper.CommandString()
	expected := "cd /tmp && pwd"

	if cmdStr != expected {
		t.Errorf("Expected CommandString to return original command %q, got %q", expected, cmdStr)
	}

	// Should not contain /bin/sh
	if strings.Contains(cmdStr, "/bin/sh") {
		t.Errorf("CommandString should not expose shell wrapper, got: %s", cmdStr)
	}
}

// Regression test for output lost when a process exits.
//
// ProcessWrapper.Wait used to call cmd.Wait() (which closes the stdout/stderr
// pipes) before waiting for the capture goroutines to drain. os/exec's docs say
// that is incorrect, and in practice it discarded whatever had not been read
// yet -- the output you most want when a process dies.
//
// It surfaced as intermittent CI failures: a scanner error ("read |0: file
// already closed") alongside a test finding no captured output at all. An
// earlier attempt to fix it suppressed the error message, which hid the data
// loss rather than preventing it.
//
// Volume alone does not reproduce it reliably -- a few hundred short lines fit
// in the pipe buffer and get drained before the close. What makes it
// deterministic is a slow handler: while the handler is working, output stays
// queued in the pipe, so reaping the process at that moment discards it.
// Parsing and appending under a lock is real work, so this is not a contrived
// scenario, just an exaggerated one.
func TestProcessWrapper_CapturesAllOutputBeforeExit(t *testing.T) {
	const wantLines = 50

	var mu sync.Mutex
	var lines []string
	handler := func(source string, line string, timestamp time.Time, isStderr bool) {
		// Hold up the reader so output is still in flight when the process exits.
		time.Sleep(2 * time.Millisecond)
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, line)
	}

	// Emit a burst and exit immediately.
	cmd := fmt.Sprintf("i=1; while [ $i -le %d ]; do echo line-$i; i=$((i+1)); done", wantLines)
	wrapper := New("burst", cmd, []string{}, "", handler)

	if err := wrapper.Start(); err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}
	if err := wrapper.Wait(); err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Wait() returning must mean all output has reached the handler. No sleeping
	// or polling here: that guarantee is the thing under test.
	mu.Lock()
	got := len(lines)
	mu.Unlock()

	if got != wantLines {
		t.Errorf("captured %d of %d lines; Wait() must not return until output is drained", got, wantLines)
	}
}

// The drain in Wait() is bounded, because a grandchild that inherits the pipe
// and outlives its parent never produces EOF -- common in dev, where a server
// spawns workers. Without a bound, fixing the lost-output race would have
// traded intermittent data loss for an indefinite hang, which is worse.
func TestProcessWrapper_WaitDoesNotHangOnLingeringChild(t *testing.T) {
	original := outputDrainTimeout
	outputDrainTimeout = 250 * time.Millisecond
	defer func() { outputDrainTimeout = original }()

	handler := func(source string, line string, timestamp time.Time, isStderr bool) {}

	// The backgrounded sleep inherits stdout and outlives the shell, so the
	// read end never sees EOF even though the direct child has exited.
	wrapper := New("lingering", "sleep 30 & echo started", []string{}, "", handler)
	wrapper.silent = true

	if err := wrapper.Start(); err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- wrapper.Wait() }()

	select {
	case <-done:
		// Returned, as required -- via the drain timeout rather than EOF.
	case <-time.After(10 * time.Second):
		t.Fatal("Wait() hung waiting for output readers; the drain must be bounded")
	}

	_ = wrapper.Stop()
}

// Review finding R15: a single over-long line used to end capture for that
// stream permanently.
//
// captureStream used a bufio.Scanner with a 1MB limit. A longer line makes
// Scan() return false with bufio.ErrTooLong, and a Scanner cannot continue --
// so every line the process logged afterwards was lost. The error was only
// printed when not silent, and TUI mode is silent, so it happened invisibly.
func TestProcessWrapper_LongLineDoesNotEndCapture(t *testing.T) {
	var mu sync.Mutex
	var lines []string
	handler := func(source string, line string, timestamp time.Time, isStderr bool) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, line)
	}

	// A line well over MaxLineBytes, then ordinary lines that must survive it.
	cmd := fmt.Sprintf(
		"head -c %d /dev/zero | tr '\\0' 'x'; echo; echo after-one; echo after-two",
		MaxLineBytes+5000)

	wrapper := New("longline", cmd, []string{}, "", handler)
	wrapper.silent = true

	if err := wrapper.Start(); err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}
	if err := wrapper.Wait(); err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	var sawAfterOne, sawAfterTwo, sawTruncationNotice bool
	for _, l := range lines {
		switch {
		case strings.Contains(l, "after-one"):
			sawAfterOne = true
		case strings.Contains(l, "after-two"):
			sawAfterTwo = true
		case strings.Contains(l, "line truncated at"):
			sawTruncationNotice = true
		}
	}

	if !sawAfterOne || !sawAfterTwo {
		t.Errorf("capture stopped after an over-long line: after-one=%v after-two=%v (got %d lines)",
			sawAfterOne, sawAfterTwo, len(lines))
	}
	if !sawTruncationNotice {
		t.Error("an over-long line should be kept and marked as truncated, not dropped silently")
	}

	// The truncated line must be bounded, not unbounded.
	for _, l := range lines {
		if len(l) > MaxLineBytes+200 {
			t.Errorf("a captured line was %d bytes; it should be truncated near %d", len(l), MaxLineBytes)
		}
	}
}

// Lines split across read-buffer boundaries must not be mangled: readLine
// accumulates across reads, so a line longer than the 64KB read buffer but
// under MaxLineBytes has to arrive intact.
func TestProcessWrapper_LineLongerThanReadBufferIsIntact(t *testing.T) {
	const lineLen = 200 * 1024 // > 64KB read buffer, < 1MB max

	var mu sync.Mutex
	var longest int
	var sawTruncation bool
	handler := func(source string, line string, timestamp time.Time, isStderr bool) {
		mu.Lock()
		defer mu.Unlock()
		if len(line) > longest {
			longest = len(line)
		}
		if strings.Contains(line, "line truncated at") {
			sawTruncation = true
		}
	}

	cmd := fmt.Sprintf("head -c %d /dev/zero | tr '\\0' 'y'; echo", lineLen)
	wrapper := New("bigline", cmd, []string{}, "", handler)
	wrapper.silent = true

	if err := wrapper.Start(); err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}
	if err := wrapper.Wait(); err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if longest != lineLen {
		t.Errorf("captured longest line of %d bytes, want %d intact", longest, lineLen)
	}
	if sawTruncation {
		t.Error("a line under MaxLineBytes must not be marked truncated")
	}
}

// Review finding R14: the state accessors were documented "safe to call
// concurrently" and took stateMu.RLock, but nothing took stateMu for WRITING
// -- ProcessState is written inside (*exec.Cmd).Wait, which knows nothing
// about that mutex. The lock only serialised readers against each other, so
// the race against Wait remained and the comments gave false assurance.
// StartTime() took no lock at all.
//
// Run with -race; this fails there, not here, if the wrapper goes back to
// proxying cmd.ProcessState.
func TestProcessWrapper_StateAccessorsAreRaceFree(t *testing.T) {
	handler := func(source, line string, ts time.Time, isStderr bool) {}
	wrapper := New("racy", "echo hello; echo world", []string{}, "", handler)
	wrapper.silent = true

	if err := wrapper.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Hammer every accessor while Wait() is reaping the process.
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = wrapper.ExitCode()
					_ = wrapper.PID()
					_ = wrapper.IsRunning()
					_ = wrapper.GetStatus()
					_ = wrapper.StartTime()
				}
			}
		}()
	}

	if err := wrapper.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
	close(stop)
	wg.Wait()

	// And the recorded state must be correct after Wait.
	if wrapper.IsRunning() {
		t.Error("IsRunning() should be false after Wait()")
	}
	if got := wrapper.ExitCode(); got != 0 {
		t.Errorf("ExitCode() = %d, want 0", got)
	}
	if got := wrapper.GetStatus(); got != "stopped" {
		t.Errorf("GetStatus() = %q, want \"stopped\"", got)
	}
	if wrapper.PID() <= 0 {
		t.Errorf("PID() = %d, want a real pid", wrapper.PID())
	}
	if wrapper.StartTime().IsZero() {
		t.Error("StartTime() should be set after Start()")
	}
}

// Review finding R16: findAndKillChildProcesses snapshotted `ps` output and
// then signalled from it, so a pid recycled in between would be killed. It now
// re-reads each pid's start time immediately before signalling, since a
// process's start time cannot change. This checks the plumbing that makes that
// possible.
func TestStartTimeOf(t *testing.T) {
	// Our own pid has a start time, and it is stable across reads.
	own := startTimeOf(os.Getpid())
	if own == "" {
		t.Fatal("startTimeOf returned nothing for our own pid; ps -o lstart= is not working as assumed")
	}
	if again := startTimeOf(os.Getpid()); again != own {
		t.Errorf("start time is not stable: %q then %q", own, again)
	}

	// A pid that cannot exist yields no start time, so a kill would be skipped.
	if got := startTimeOf(0x7FFFFFFF); got != "" {
		t.Errorf("startTimeOf(impossible pid) = %q, want empty", got)
	}
}

func TestListProcesses_IncludesStartTimes(t *testing.T) {
	entries, err := listProcesses()
	if err != nil {
		t.Fatalf("listProcesses: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no processes listed")
	}

	var foundSelf bool
	for _, e := range entries {
		if e.lstart == "" {
			t.Errorf("pid %d has no start time; it would be skipped rather than verified", e.pid)
		}
		if e.pid == os.Getpid() {
			foundSelf = true
		}
	}
	if !foundSelf {
		t.Error("our own process was not in the listing")
	}
}
