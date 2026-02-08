package process

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

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
