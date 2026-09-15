package process

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// waitAsync runs manager.Wait() in a goroutine and reports whether it returned
// within the timeout. Wait() blocking forever is a real failure mode (see
// TestManager_WaitReturnsWhenNoRecurringProcesses), so tests must never call it
// directly on the test goroutine without a deadline.
func waitAsync(m *Manager, timeout time.Duration) (returned bool, err error) {
	done := make(chan error, 1)
	go func() { done <- m.Wait() }()

	select {
	case err := <-done:
		return true, err
	case <-time.After(timeout):
		return false, nil
	}
}

// Regression test for Wait() hanging forever.
//
// Wait() filters recurring processes out of its wait set, waits for the rest,
// then blocks on <-m.ctx.Done() "for recurring processes" -- but originally did
// so unconditionally, without checking that any recurring process existed. That
// made Wait() never return for any configuration, hanging both this package's
// tests and internal/api's until the Go test timeout panicked.
func TestManager_WaitReturnsWhenNoRecurringProcesses(t *testing.T) {
	handler := func(source string, line string, timestamp time.Time, isStderr bool) {}

	configs := []ProcessConfig{
		{Name: "echo1", Command: "echo", Args: []string{"hello"}},
		{Name: "echo2", Command: "echo", Args: []string{"world"}},
	}

	manager := NewManager(configs, handler)
	if err := manager.Start(); err != nil {
		t.Fatalf("Failed to start processes: %v", err)
	}
	defer func() { _ = manager.Stop() }()

	returned, err := waitAsync(manager, 5*time.Second)
	if !returned {
		t.Fatal("Wait() did not return after all non-recurring processes exited; it must not block on ctx.Done() when there are no recurring processes")
	}
	if err != nil {
		t.Fatalf("Wait() returned an error: %v", err)
	}
}

// The other half of the contract: a recurring process runs until the manager is
// stopped, so Wait() must keep blocking while one exists. Guards against
// "fixing" the hang by deleting the ctx.Done() wait outright.
func TestManager_WaitBlocksWhileRecurringProcessExists(t *testing.T) {
	handler := func(source string, line string, timestamp time.Time, isStderr bool) {}

	configs := []ProcessConfig{
		{Name: "oneshot", Command: "echo", Args: []string{"hello"}},
		// Long interval: runs once immediately, then not again during the test.
		{Name: "ticker", Command: "echo", Args: []string{"tick"}, Interval: "1h"},
	}

	manager := NewManager(configs, handler)
	if err := manager.Start(); err != nil {
		t.Fatalf("Failed to start processes: %v", err)
	}

	// Exactly one Wait() call for the whole test. Concurrent calls each spawn a
	// waiter per process, which race on os/exec.Cmd.Wait() -- see the doc comment
	// on Manager.Wait.
	done := make(chan error, 1)
	go func() { done <- manager.Wait() }()

	// Must still be blocked: the recurring process has not been stopped.
	select {
	case <-done:
		t.Fatal("Wait() returned while a recurring process was still scheduled; it should block until the manager is stopped")
	case <-time.After(500 * time.Millisecond):
	}

	// Stopping the manager cancels the context, which should release Wait().
	if err := manager.Stop(); err != nil {
		t.Fatalf("Stop() failed: %v", err)
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Wait() did not return after Stop() cancelled the context")
	}
}

func TestManager_MultipleProcesses(t *testing.T) {
	var mu sync.Mutex
	capturedLines := make(map[string][]string)

	handler := func(source string, line string, timestamp time.Time, isStderr bool) {
		mu.Lock()
		defer mu.Unlock()
		capturedLines[source] = append(capturedLines[source], line)
	}

	configs := []ProcessConfig{
		{Name: "echo1", Command: "echo", Args: []string{"hello"}},
		{Name: "echo2", Command: "echo", Args: []string{"world"}},
		{Name: "echo3", Command: "echo", Args: []string{"test"}},
	}

	manager := NewManager(configs, handler)

	// Start all processes
	if err := manager.Start(); err != nil {
		t.Fatalf("Failed to start processes: %v", err)
	}

	// Wait for all to complete
	if err := manager.Wait(); err != nil {
		t.Fatalf("Wait failed: %v", err)
	}

	// Verify all processes captured output
	mu.Lock()
	defer mu.Unlock()

	if len(capturedLines["echo1"]) == 0 {
		t.Error("echo1 should have captured output")
	}
	if len(capturedLines["echo2"]) == 0 {
		t.Error("echo2 should have captured output")
	}
	if len(capturedLines["echo3"]) == 0 {
		t.Error("echo3 should have captured output")
	}

	// Verify content
	echo1Output := strings.Join(capturedLines["echo1"], " ")
	if !strings.Contains(echo1Output, "hello") {
		t.Errorf("echo1 output should contain 'hello', got: %s", echo1Output)
	}

	echo2Output := strings.Join(capturedLines["echo2"], " ")
	if !strings.Contains(echo2Output, "world") {
		t.Errorf("echo2 output should contain 'world', got: %s", echo2Output)
	}
}

func TestManager_ExitCodes(t *testing.T) {
	configs := []ProcessConfig{
		{Name: "success", Command: "exit 0", Args: []string{}},
		{Name: "fail", Command: "exit 42", Args: []string{}},
	}

	manager := NewManager(configs, nil)

	if err := manager.Start(); err != nil {
		t.Fatalf("Failed to start processes: %v", err)
	}

	// Wait for all to complete (will error because one fails)
	_ = manager.Wait()

	// Check exit codes
	codes := manager.ExitCodes()

	if codes["success"] != 0 {
		t.Errorf("success process should have exit code 0, got %d", codes["success"])
	}

	if codes["fail"] != 42 {
		t.Errorf("fail process should have exit code 42, got %d", codes["fail"])
	}
}

func TestManager_Stop(t *testing.T) {
	configs := []ProcessConfig{
		{Name: "sleep1", Command: "sleep", Args: []string{"10"}},
		{Name: "sleep2", Command: "sleep", Args: []string{"10"}},
	}

	manager := NewManager(configs, nil)

	if err := manager.Start(); err != nil {
		t.Fatalf("Failed to start processes: %v", err)
	}

	if err := waitForManagerPIDs(manager, 1*time.Second); err != nil {
		t.Fatalf("Processes didn't start: %v", err)
	}

	// Stop all processes
	if err := manager.Stop(); err != nil {
		t.Errorf("Stop failed: %v", err)
	}

	// Wait should return quickly since processes are stopped
	done := make(chan bool)
	go func() {
		manager.Wait()
		done <- true
	}()

	select {
	case <-done:
		// Success - wait completed
	case <-time.After(2 * time.Second):
		t.Error("Wait did not complete after Stop within timeout")
	}
}

func TestManager_Restart(t *testing.T) {
	var mu sync.Mutex
	restartCount := 0

	handler := func(source string, line string, timestamp time.Time, isStderr bool) {
		mu.Lock()
		defer mu.Unlock()
		if source == "echo-restart" {
			restartCount++
		}
	}

	configs := []ProcessConfig{
		{Name: "echo-restart", Command: "echo", Args: []string{"test"}},
	}

	manager := NewManager(configs, handler)

	// Start process
	if err := manager.Start(); err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Wait for first run to complete
	if err := manager.Wait(); err != nil {
		t.Fatalf("Wait failed: %v", err)
	}

	mu.Lock()
	firstCount := restartCount
	mu.Unlock()

	if firstCount == 0 {
		t.Fatal("Process should have run at least once")
	}

	// Restart the process
	if err := manager.Restart("echo-restart"); err != nil {
		t.Fatalf("Restart failed: %v", err)
	}

	// Wait for restart to complete (poll for count to increase)
	deadline := time.Now().Add(1 * time.Second)
	var secondCount int
	for time.Now().Before(deadline) {
		mu.Lock()
		secondCount = restartCount
		mu.Unlock()
		if secondCount > firstCount {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if secondCount <= firstCount {
		t.Errorf("Restart should have increased count from %d to %d", firstCount, secondCount)
	}
}

func TestManager_RestartNonExistent(t *testing.T) {
	configs := []ProcessConfig{
		{Name: "echo1", Command: "echo", Args: []string{"test"}},
	}

	manager := NewManager(configs, nil)

	if err := manager.Start(); err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Try to restart a process that doesn't exist
	err := manager.Restart("nonexistent")
	if err == nil {
		t.Error("Restart should fail for non-existent process")
	}

	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("Error should mention 'not found', got: %v", err)
	}
}

func TestManager_StartFailure(t *testing.T) {
	configs := []ProcessConfig{
		{Name: "invalid", Command: "this-command-does-not-exist", Args: []string{}},
	}

	manager := NewManager(configs, nil)

	// With shell execution, Start() succeeds (shell starts), but the command fails
	err := manager.Start()
	if err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}

	// Wait should fail because the command doesn't exist
	err = manager.Wait()
	if err == nil {
		t.Error("Wait should fail for invalid command")
	}

	// Check exit code is 127 (command not found)
	codes := manager.ExitCodes()
	if codes["invalid"] != 127 {
		t.Errorf("Expected exit code 127 for command not found, got %d", codes["invalid"])
	}
}

func TestManager_PartialStartFailure(t *testing.T) {
	configs := []ProcessConfig{
		{Name: "good", Command: "echo", Args: []string{"test"}},
		{Name: "bad", Command: "this-command-does-not-exist", Args: []string{}},
	}

	manager := NewManager(configs, nil)

	// With shell execution, Start() succeeds (shells start)
	err := manager.Start()
	if err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}

	// Wait for processes to complete
	_ = manager.Wait()

	// Check that bad command has exit code 127 (command not found)
	codes := manager.ExitCodes()
	if codes["bad"] != 127 {
		t.Errorf("Expected exit code 127 for bad command, got %d", codes["bad"])
	}

	// Good command should have succeeded
	if codes["good"] != 0 {
		t.Errorf("Expected exit code 0 for good command, got %d", codes["good"])
	}
}

func TestManager_ConcurrentAccess(t *testing.T) {
	configs := []ProcessConfig{
		{Name: "echo1", Command: "echo", Args: []string{"test1"}},
		{Name: "echo2", Command: "echo", Args: []string{"test2"}},
	}

	manager := NewManager(configs, nil)

	if err := manager.Start(); err != nil {
		t.Fatalf("Failed to start processes: %v", err)
	}

	// Try concurrent access to ExitCodes while processes are running/finishing
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes := manager.ExitCodes()
			// Just access the codes, don't care about values during this test
			_ = codes
		}()
	}

	wg.Wait()
	manager.Wait()
}

// Batch 3: GetProcess and ListProcesses tests

func TestGetProcess_Found_Running(t *testing.T) {
	configs := []ProcessConfig{
		{Name: "test-sleep", Command: "sleep", Args: []string{"5"}},
	}
	manager := NewManager(configs, nil)

	if err := manager.Start(); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}
	defer manager.Stop()

	if err := waitForManagerPIDs(manager, 1*time.Second); err != nil {
		t.Fatalf("Process didn't start: %v", err)
	}

	info, err := manager.GetProcess("test-sleep")
	if err != nil {
		t.Fatalf("GetProcess failed: %v", err)
	}

	if info.Name != "test-sleep" {
		t.Errorf("Expected name 'test-sleep', got '%s'", info.Name)
	}
	if info.Status != "running" {
		t.Errorf("Expected status 'running', got '%s'", info.Status)
	}
	if info.PID <= 0 {
		t.Errorf("Expected positive PID, got %d", info.PID)
	}
	if info.ExitCode != -1 {
		t.Errorf("Expected exit_code -1 for running process, got %d", info.ExitCode)
	}
}

func TestGetProcess_Found_Stopped(t *testing.T) {
	configs := []ProcessConfig{
		{Name: "test-echo", Command: "echo", Args: []string{"test"}},
	}
	manager := NewManager(configs, nil)

	if err := manager.Start(); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}
	defer manager.Stop()

	manager.Wait() // Wait for completion

	info, err := manager.GetProcess("test-echo")
	if err != nil {
		t.Fatalf("GetProcess failed: %v", err)
	}

	if info.Status != "stopped" {
		t.Errorf("Expected status 'stopped', got '%s'", info.Status)
	}
	if info.ExitCode != 0 {
		t.Errorf("Expected exit_code 0 for stopped process, got %d", info.ExitCode)
	}
}

func TestGetProcess_NotFound(t *testing.T) {
	configs := []ProcessConfig{
		{Name: "existing", Command: "echo", Args: []string{"test"}},
	}
	manager := NewManager(configs, nil)

	if err := manager.Start(); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}
	defer manager.Stop()

	_, err := manager.GetProcess("nonexistent")
	if err == nil {
		t.Fatal("Expected error for nonexistent process, got nil")
	}

	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("Expected 'not found' in error, got: %v", err)
	}
}

func TestListProcesses_ExitCodeAlwaysPresent(t *testing.T) {
	configs := []ProcessConfig{
		{Name: "running-proc", Command: "sleep", Args: []string{"5"}},
		{Name: "stopped-proc", Command: "echo", Args: []string{"done"}},
	}
	manager := NewManager(configs, nil)

	if err := manager.Start(); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}
	defer manager.Stop()

	if err := waitForManagerPIDs(manager, 1*time.Second); err != nil {
		t.Fatalf("Processes didn't start: %v", err)
	}

	// Give echo time to complete (it exits immediately)
	time.Sleep(50 * time.Millisecond)

	infos := manager.ListProcesses()

	if len(infos) != 2 {
		t.Fatalf("Expected 2 processes, got %d", len(infos))
	}

	// Verify all have exit_code field (even if running)
	for _, info := range infos {
		// Running processes should have exit_code=-1
		// Stopped processes should have exit_code=0
		if info.Status == "running" && info.ExitCode != -1 {
			t.Errorf("Process %s is running but exit_code is %d, expected -1", info.Name, info.ExitCode)
		}
		if info.Status == "stopped" && info.ExitCode != 0 {
			t.Errorf("Process %s is stopped but exit_code is %d, expected 0", info.Name, info.ExitCode)
		}
	}
}

// Test helper functions for polling instead of sleeping

// waitForPID polls until the process has a valid PID or times out

// Test helper functions for polling instead of sleeping

// waitForManagerPIDs waits for all processes in manager to have PIDs
func waitForManagerPIDs(m *Manager, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		infos := m.ListProcesses()
		allStarted := true
		for _, info := range infos {
			if info.PID <= 0 {
				allStarted = false
				break
			}
		}
		if allStarted {
			return nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for all processes to start")
}

func TestManager_RestartOnCrash(t *testing.T) {
	handler := func(source string, line string, timestamp time.Time, isStderr bool) {
		// Log output for debugging
		t.Logf("[%s] %s", source, line)
	}

	// Create a process that exits with code 1
	configs := []ProcessConfig{
		{
			Name:           "crasher",
			Command:        "sh",
			Args:           []string{"-c", "exit 1"},
			RestartOnCrash: true,
		},
	}

	manager := NewManager(configs, handler)

	// Start process
	if err := manager.Start(); err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Start Wait() in background (it will loop restarting the crashed process)
	done := make(chan error, 1)
	go func() {
		done <- manager.Wait()
	}()

	// Let it crash and restart a few times
	time.Sleep(200 * time.Millisecond)

	// Stop the manager (this should break the restart loop)
	// Note: Stop may fail if process already exited, which is expected
	manager.Stop()

	// Wait should complete quickly after Stop
	select {
	case <-done:
		// Success - Wait completed
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not complete after Stop within timeout")
	}

	// If we got here, the restart loop worked and was properly stopped
}

func TestManager_NoRestartOnCleanExit(t *testing.T) {
	var mu sync.Mutex
	lines := []string{}
	runCount := 0

	handler := func(source string, line string, timestamp time.Time, isStderr bool) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, line)
		if source == "clean-exit" && strings.Contains(line, "run") {
			runCount++
		}
	}

	// Create a process that exits cleanly (exit code 0)
	configs := []ProcessConfig{
		{
			Name:           "clean-exit",
			Command:        "echo",
			Args:           []string{"clean run"},
			RestartOnCrash: true, // Enabled, but shouldn't restart on clean exit
		},
	}

	manager := NewManager(configs, handler)

	// Start process
	if err := manager.Start(); err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Wait for process to complete
	if err := manager.Wait(); err != nil {
		t.Fatalf("Wait failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	// Should have run once, not restarted
	if runCount != 1 {
		t.Errorf("Expected exactly 1 run, got %d", runCount)
	}

	// Should not see restart message
	for _, line := range lines {
		if strings.Contains(line, "restarting") {
			t.Errorf("Should not restart on clean exit, but saw: %s", line)
		}
	}
}

func TestManager_NoRestartWhenDisabled(t *testing.T) {
	var mu sync.Mutex
	lines := []string{}

	handler := func(source string, line string, timestamp time.Time, isStderr bool) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, line)
	}

	// Create a process that crashes but has restart disabled
	configs := []ProcessConfig{
		{
			Name:           "no-restart",
			Command:        "sh",
			Args:           []string{"-c", "exit 1"},
			RestartOnCrash: false, // Disabled
		},
	}

	manager := NewManager(configs, handler)

	// Start process
	if err := manager.Start(); err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Wait for process to exit (should not restart)
	manager.Wait()

	mu.Lock()
	defer mu.Unlock()

	// Should not see restart message
	for _, line := range lines {
		if strings.Contains(line, "restarting") {
			t.Errorf("Should not restart when disabled, but saw: %s", line)
		}
	}
}

// Review finding R7: recurring processes reported permanently wrong status.
//
// Start() created a wrapper for a recurring process and registered it without
// ever calling Start() on it, then launched a goroutine that created a
// DIFFERENT wrapper per run and never registered it. So the registered wrapper
// had no ProcessState forever: pid -1, status "running", exit code -1,
// regardless of whether the runs were succeeding or failing.
func TestManager_RecurringProcessReportsRealState(t *testing.T) {
	handler := func(source, line string, ts time.Time, isStderr bool) {}
	configs := []ProcessConfig{
		{Name: "ticker", Command: "echo tick", Interval: "1h"},
	}
	m := NewManager(configs, handler)
	if err := m.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = m.Stop() }()

	// Give the immediate first run time to start, finish, and be recorded.
	deadline := time.Now().Add(5 * time.Second)
	var info ProcessInfo
	for time.Now().Before(deadline) {
		infos := m.ListProcesses()
		if len(infos) == 1 && infos[0].PID != -1 {
			info = infos[0]
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if info.PID == -1 || info.PID == 0 {
		t.Fatalf("recurring process still reports pid %d; the running wrapper is not registered", info.PID)
	}
	// Between runs a healthy recurring process is "waiting", not "running"
	// (which would be a lie) and not "stopped" (which reads as down).
	if info.Status != "waiting" && info.Status != "running" {
		t.Errorf("status = %q, want \"waiting\" or \"running\"", info.Status)
	}
	if info.Interval != "1h" {
		t.Errorf("interval = %q, want \"1h\"", info.Interval)
	}
}

// A recurring process whose last run failed must say so, not be smoothed over
// into "waiting".
func TestManager_RecurringProcessFailureIsVisible(t *testing.T) {
	handler := func(source, line string, ts time.Time, isStderr bool) {}
	configs := []ProcessConfig{
		{Name: "broken", Command: "exit 4", Interval: "1h"},
	}
	m := NewManager(configs, handler)
	if err := m.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = m.Stop() }()

	deadline := time.Now().Add(5 * time.Second)
	var info ProcessInfo
	for time.Now().Before(deadline) {
		infos := m.ListProcesses()
		if len(infos) == 1 && infos[0].ExitCode > 0 {
			info = infos[0]
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if info.ExitCode != 4 {
		t.Errorf("exit code = %d, want 4", info.ExitCode)
	}
	if info.Status != "failed" {
		t.Errorf("status = %q, want \"failed\": a failing recurring process must not report as healthy", info.Status)
	}
}

// A process that exits non-zero must leave a trace in the log buffer.
//
// Failures were previously only printed to running-man's own stdout, so
// nothing reached the buffer: /logs had no record that a process had died, and
// /errors could return zero while a process sat there failed. Verified during
// phase 3 -- a process exited 1 having printed a clear message, and /errors
// was empty.
func TestManager_NonZeroExitIsReportedToTheBuffer(t *testing.T) {
	var mu sync.Mutex
	var lines []string
	handler := func(source, line string, ts time.Time, isStderr bool) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, line)
	}

	m := NewManager([]ProcessConfig{{Name: "doomed", Command: "exit 3"}}, handler)
	if err := m.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = m.Stop() }()

	if returned, _ := waitAsync(m, 5*time.Second); !returned {
		t.Fatal("Wait() did not return")
	}

	mu.Lock()
	defer mu.Unlock()

	var found string
	for _, l := range lines {
		if strings.Contains(l, "doomed") && strings.Contains(l, "exited with code 3") {
			found = l
		}
	}
	if found == "" {
		t.Fatalf("no exit report reached the handler; got %v", lines)
	}
	// Must contain a word the plain-text parser recognises as an error, or the
	// entry lands as info and /errors stays empty -- the whole point of this.
	if !strings.Contains(found, "failed") {
		t.Errorf("exit report %q contains no word the parser treats as an error", found)
	}
}

// A clean exit must not manufacture an error entry.
func TestManager_ZeroExitIsNotReportedAsFailure(t *testing.T) {
	var mu sync.Mutex
	var lines []string
	handler := func(source, line string, ts time.Time, isStderr bool) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, line)
	}

	m := NewManager([]ProcessConfig{{Name: "fine", Command: "echo ok"}}, handler)
	if err := m.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = m.Stop() }()

	if returned, _ := waitAsync(m, 5*time.Second); !returned {
		t.Fatal("Wait() did not return")
	}

	mu.Lock()
	defer mu.Unlock()
	for _, l := range lines {
		if strings.Contains(l, "failed") {
			t.Errorf("a clean exit produced a failure line: %q", l)
		}
	}
}

// A recurring process failing every run would otherwise be invisible: each run
// exits and is replaced by the next.
func TestManager_RecurringFailureIsReportedToTheBuffer(t *testing.T) {
	var mu sync.Mutex
	var lines []string
	handler := func(source, line string, ts time.Time, isStderr bool) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, line)
	}

	m := NewManager([]ProcessConfig{
		{Name: "brokenticker", Command: "exit 5", Interval: "1h"},
	}, handler)
	if err := m.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = m.Stop() }()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		var found bool
		for _, l := range lines {
			if strings.Contains(l, "brokenticker") && strings.Contains(l, "exited with code 5") {
				found = true
			}
		}
		mu.Unlock()
		if found {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	t.Errorf("recurring failure never reached the handler; got %v", lines)
}
