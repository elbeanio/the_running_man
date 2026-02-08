package wrapper

import (
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
		{Name: "success", Command: "sh", Args: []string{"-c", "exit 0"}},
		{Name: "fail", Command: "sh", Args: []string{"-c", "exit 42"}},
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

	// Give processes time to start
	time.Sleep(100 * time.Millisecond)

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

	// Wait for restart to complete
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	secondCount := restartCount
	mu.Unlock()

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

	// Start should fail
	err := manager.Start()
	if err == nil {
		t.Error("Start should fail for invalid command")
	}

	if !strings.Contains(err.Error(), "failed to start") {
		t.Errorf("Error should mention 'failed to start', got: %v", err)
	}
}

func TestManager_PartialStartFailure(t *testing.T) {
	configs := []ProcessConfig{
		{Name: "good", Command: "echo", Args: []string{"test"}},
		{Name: "bad", Command: "this-command-does-not-exist", Args: []string{}},
	}

	manager := NewManager(configs, nil)

	// Start should fail
	err := manager.Start()
	if err == nil {
		t.Error("Start should fail when any process fails")
	}

	// No processes should be running (all should be stopped on failure)
	codes := manager.ExitCodes()
	if len(codes) > 0 {
		t.Errorf("No processes should be running after start failure, got %d", len(codes))
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
