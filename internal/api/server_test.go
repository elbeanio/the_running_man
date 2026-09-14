package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/elbeanio/the_running_man/internal/parser"
	"github.com/elbeanio/the_running_man/internal/process"
	"github.com/elbeanio/the_running_man/internal/storage"
)

func setupTestServer() (*Server, *storage.RingBuffer) {
	buffer := storage.NewRingBuffer(100, 30*time.Minute, 50*1024*1024)
	server := NewServer(buffer, 9000, nil, nil, nil) // nil lineHandler, manager, and traceStorage for tests
	return server, buffer
}

func TestHandleLogs_Empty(t *testing.T) {
	server, _ := setupTestServer()

	req := httptest.NewRequest("GET", "/logs", nil)
	w := httptest.NewRecorder()

	server.handleLogs(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	count := int(response["count"].(float64))
	if count != 0 {
		t.Errorf("Expected 0 logs, got %d", count)
	}
}

func TestHandleLogs_WithEntries(t *testing.T) {
	server, buffer := setupTestServer()

	// Add test entries
	buffer.Append(&parser.LogEntry{
		Timestamp: time.Now(),
		Level:     parser.LevelInfo,
		Source:    "test",
		Message:   "test message 1",
		Raw:       "test message 1",
	})
	buffer.Append(&parser.LogEntry{
		Timestamp: time.Now(),
		Level:     parser.LevelError,
		Source:    "test",
		Message:   "error message",
		Raw:       "error message",
		IsError:   true,
	})

	req := httptest.NewRequest("GET", "/logs", nil)
	w := httptest.NewRecorder()

	server.handleLogs(w, req)

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	count := int(response["count"].(float64))
	if count != 2 {
		t.Errorf("Expected 2 logs, got %d", count)
	}
}

func TestHandleLogs_LevelFilter(t *testing.T) {
	server, buffer := setupTestServer()

	buffer.Append(&parser.LogEntry{
		Timestamp: time.Now(),
		Level:     parser.LevelInfo,
		Message:   "info",
		Raw:       "info",
	})
	buffer.Append(&parser.LogEntry{
		Timestamp: time.Now(),
		Level:     parser.LevelError,
		Message:   "error",
		Raw:       "error",
		IsError:   true,
	})

	// Filter for errors only
	req := httptest.NewRequest("GET", "/logs?level=error", nil)
	w := httptest.NewRecorder()

	server.handleLogs(w, req)

	var response map[string]interface{}
	json.NewDecoder(w.Body).Decode(&response)

	count := int(response["count"].(float64))
	if count != 1 {
		t.Errorf("Expected 1 error log, got %d", count)
	}
}

func TestHandleLogs_MultipleFilters(t *testing.T) {
	server, buffer := setupTestServer()

	buffer.Append(&parser.LogEntry{
		Timestamp: time.Now(),
		Level:     parser.LevelWarn,
		Message:   "warning",
		Raw:       "warning",
	})
	buffer.Append(&parser.LogEntry{
		Timestamp: time.Now(),
		Level:     parser.LevelError,
		Message:   "error",
		Raw:       "error",
		IsError:   true,
	})
	buffer.Append(&parser.LogEntry{
		Timestamp: time.Now(),
		Level:     parser.LevelInfo,
		Message:   "info",
		Raw:       "info",
	})

	// Filter for warn and error
	req := httptest.NewRequest("GET", "/logs?level=warn,error", nil)
	w := httptest.NewRecorder()

	server.handleLogs(w, req)

	var response map[string]interface{}
	json.NewDecoder(w.Body).Decode(&response)

	count := int(response["count"].(float64))
	if count != 2 {
		t.Errorf("Expected 2 logs (warn+error), got %d", count)
	}
}

func TestHandleLogs_ContainsFilter(t *testing.T) {
	server, buffer := setupTestServer()

	buffer.Append(&parser.LogEntry{
		Timestamp: time.Now(),
		Message:   "database connection established",
		Raw:       "database connection established",
	})
	buffer.Append(&parser.LogEntry{
		Timestamp: time.Now(),
		Message:   "server started",
		Raw:       "server started",
	})

	req := httptest.NewRequest("GET", "/logs?contains=database", nil)
	w := httptest.NewRecorder()

	server.handleLogs(w, req)

	var response map[string]interface{}
	json.NewDecoder(w.Body).Decode(&response)

	count := int(response["count"].(float64))
	if count != 1 {
		t.Errorf("Expected 1 log containing 'database', got %d", count)
	}
}

func TestHandleLogs_SinceFilter(t *testing.T) {
	server, buffer := setupTestServer()

	// Old entry
	buffer.Append(&parser.LogEntry{
		Timestamp: time.Now().Add(-10 * time.Minute),
		Message:   "old",
		Raw:       "old",
	})

	// Recent entry
	buffer.Append(&parser.LogEntry{
		Timestamp: time.Now(),
		Message:   "recent",
		Raw:       "recent",
	})

	req := httptest.NewRequest("GET", "/logs?since=5m", nil)
	w := httptest.NewRecorder()

	server.handleLogs(w, req)

	var response map[string]interface{}
	json.NewDecoder(w.Body).Decode(&response)

	count := int(response["count"].(float64))
	if count != 1 {
		t.Errorf("Expected 1 recent log, got %d", count)
	}
}

func TestHandleErrors(t *testing.T) {
	server, buffer := setupTestServer()

	buffer.Append(&parser.LogEntry{
		Timestamp: time.Now(),
		Level:     parser.LevelInfo,
		Message:   "info",
		Raw:       "info",
		IsError:   false,
	})
	buffer.Append(&parser.LogEntry{
		Timestamp: time.Now(),
		Level:     parser.LevelError,
		Message:   "error",
		Raw:       "error",
		IsError:   true,
	})

	req := httptest.NewRequest("GET", "/errors", nil)
	w := httptest.NewRecorder()

	server.handleErrors(w, req)

	var response map[string]interface{}
	json.NewDecoder(w.Body).Decode(&response)

	count := int(response["count"].(float64))
	if count != 1 {
		t.Errorf("Expected 1 error, got %d", count)
	}
}

func TestHandleHealth(t *testing.T) {
	server, buffer := setupTestServer()

	buffer.Append(&parser.LogEntry{
		Timestamp: time.Now(),
		Message:   "test",
		Raw:       "test",
	})

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	server.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if status := response["status"].(string); status != "ok" {
		t.Errorf("Expected status 'ok', got %s", status)
	}

	if response["uptime"] == nil {
		t.Error("Expected uptime field")
	}

	if response["buffer"] == nil {
		t.Error("Expected buffer stats")
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
		wantErr  bool
	}{
		{"30s", 30 * time.Second, false},
		{"5m", 5 * time.Minute, false},
		{"1h", 1 * time.Hour, false},
		{"90", 90 * time.Second, false},
		{"invalid", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseDuration(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseDuration(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.expected {
				t.Errorf("parseDuration(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestCORSHeaders(t *testing.T) {
	server, _ := setupTestServer()

	req := httptest.NewRequest("GET", "/logs", nil)
	w := httptest.NewRecorder()

	handler := server.corsMiddleware(http.HandlerFunc(server.handleLogs))
	handler.ServeHTTP(w, req)

	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("Expected CORS header to be set")
	}
}

func TestSelfLogging(t *testing.T) {
	buffer := storage.NewRingBuffer(100, 30*time.Minute, 50*1024*1024)

	// Track calls to lineHandler
	var capturedLogs []struct {
		source  string
		message string
		isError bool
	}

	lineHandler := func(source string, line string, timestamp time.Time, isStderr bool) {
		capturedLogs = append(capturedLogs, struct {
			source  string
			message string
			isError bool
		}{source, line, isStderr})
	}

	server := NewServer(buffer, 9000, lineHandler, nil, nil)

	// Test normal log
	server.log("Test message", false)

	if len(capturedLogs) != 1 {
		t.Fatalf("Expected 1 captured log, got %d", len(capturedLogs))
	}

	if capturedLogs[0].source != "running-man" {
		t.Errorf("Expected source 'running-man', got '%s'", capturedLogs[0].source)
	}

	if capturedLogs[0].message != "Test message" {
		t.Errorf("Expected message 'Test message', got '%s'", capturedLogs[0].message)
	}

	if capturedLogs[0].isError {
		t.Error("Expected isError to be false")
	}

	// Test error log
	server.log("Error message", true)

	if len(capturedLogs) != 2 {
		t.Fatalf("Expected 2 captured logs, got %d", len(capturedLogs))
	}

	if !capturedLogs[1].isError {
		t.Error("Expected isError to be true for error log")
	}
}

func TestSelfLogging_NilHandler(t *testing.T) {
	buffer := storage.NewRingBuffer(100, 30*time.Minute, 50*1024*1024)
	server := NewServer(buffer, 9000, nil, nil, nil)

	// Should not panic with nil handler
	server.log("Test message", false)
	server.log("Error message", true)
}

func TestCheckPatternComplexity(t *testing.T) {
	buffer := storage.NewRingBuffer(100, 30*time.Minute, 50*1024*1024)

	var warnings []string
	lineHandler := func(source string, line string, timestamp time.Time, isStderr bool) {
		warnings = append(warnings, line)
	}

	server := NewServer(buffer, 9000, lineHandler, nil, nil)

	tests := []struct {
		name         string
		patterns     []string
		patternType  string
		wantWarnings int
		containsText string
	}{
		{
			name:         "few patterns - no warning",
			patterns:     []string{"test", "foo", "bar"},
			patternType:  "source",
			wantWarnings: 0,
		},
		{
			name:         "many patterns - warning",
			patterns:     make([]string, 25),
			patternType:  "source",
			wantWarnings: 1,
			containsText: "Large number",
		},
		{
			name:         "long pattern - warning",
			patterns:     []string{string(make([]byte, 250))},
			patternType:  "exclude",
			wantWarnings: 1,
			containsText: "Very long",
		},
		{
			name:         "many wildcards - warning",
			patterns:     []string{"************test"},
			patternType:  "source",
			wantWarnings: 1,
			containsText: "many wildcards",
		},
		{
			name:         "multiple issues - multiple warnings",
			patterns:     append(make([]string, 25), "************test"),
			patternType:  "source",
			wantWarnings: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warnings = nil // Reset
			server.checkPatternComplexity(tt.patterns, tt.patternType)

			if len(warnings) != tt.wantWarnings {
				t.Errorf("Expected %d warnings, got %d", tt.wantWarnings, len(warnings))
			}

			if tt.containsText != "" && len(warnings) > 0 {
				found := false
				for _, w := range warnings {
					if contains(w, tt.containsText) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected warning containing '%s', got: %v", tt.containsText, warnings)
				}
			}
		})
	}
}

func TestPatternWarnings_Integration(t *testing.T) {
	buffer := storage.NewRingBuffer(100, 30*time.Minute, 50*1024*1024)

	// Use actual lineHandler that appends to buffer
	lineHandler := func(source string, line string, timestamp time.Time, isStderr bool) {
		entry := &parser.LogEntry{
			Timestamp: timestamp,
			Level:     parser.LevelInfo,
			Source:    source,
			Message:   line,
			Raw:       line,
			IsError:   isStderr,
		}
		buffer.Append(entry)
	}

	server := NewServer(buffer, 9000, lineHandler, nil, nil)

	// Make a request with problematic patterns
	req := httptest.NewRequest("GET", "/logs?source=************test", nil)
	w := httptest.NewRecorder()

	server.handleLogs(w, req)

	// Check that warning was captured in buffer
	logs := buffer.Query(storage.QueryFilters{
		Sources: []string{"running-man"},
	})

	if len(logs) == 0 {
		t.Fatal("Expected warning to be captured in buffer")
	}

	found := false
	for _, log := range logs {
		if contains(log.Message, "Warning") && contains(log.Message, "wildcards") {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected to find wildcard warning in captured logs")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Batch 1: Security validation tests for GET /processes/{name}

func TestHandleProcessDetail_InvalidName_Slash(t *testing.T) {
	buffer := storage.NewRingBuffer(100, 30*time.Minute, 50*1024*1024)
	server := NewServer(buffer, 9000, nil, nil, nil)

	req := httptest.NewRequest("GET", "/processes/foo/bar", nil)
	w := httptest.NewRecorder()

	server.handleProcessOrRestart(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if errMsg, ok := response["error"].(string); !ok || !strings.Contains(errMsg, "invalid process name") {
		t.Errorf("Expected 'invalid process name' error, got: %v", response["error"])
	}
}

func TestHandleProcessDetail_InvalidName_DotDot(t *testing.T) {
	buffer := storage.NewRingBuffer(100, 30*time.Minute, 50*1024*1024)
	server := NewServer(buffer, 9000, nil, nil, nil)

	req := httptest.NewRequest("GET", "/processes/../etc/passwd", nil)
	w := httptest.NewRecorder()

	server.handleProcessOrRestart(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if errMsg, ok := response["error"].(string); !ok || !strings.Contains(errMsg, "invalid process name") {
		t.Errorf("Expected 'invalid process name' error, got: %v", response["error"])
	}
}

func TestHandleProcessDetail_TooLong(t *testing.T) {
	buffer := storage.NewRingBuffer(100, 30*time.Minute, 50*1024*1024)
	server := NewServer(buffer, 9000, nil, nil, nil)

	longName := strings.Repeat("a", 300)
	req := httptest.NewRequest("GET", "/processes/"+longName, nil)
	w := httptest.NewRecorder()

	server.handleProcessOrRestart(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if errMsg, ok := response["error"].(string); !ok || errMsg != "process name too long" {
		t.Errorf("Expected 'process name too long' error, got: %v", response["error"])
	}
}

func TestHandleProcessDetail_EmptyName(t *testing.T) {
	buffer := storage.NewRingBuffer(100, 30*time.Minute, 50*1024*1024)
	server := NewServer(buffer, 9000, nil, nil, nil)

	req := httptest.NewRequest("GET", "/processes/", nil)
	w := httptest.NewRecorder()

	server.handleProcessOrRestart(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if errMsg, ok := response["error"].(string); !ok || errMsg != "process name required" {
		t.Errorf("Expected 'process name required' error, got: %v", response["error"])
	}
}

func TestHandleProcessDetail_NoManager(t *testing.T) {
	buffer := storage.NewRingBuffer(100, 30*time.Minute, 50*1024*1024)
	server := NewServer(buffer, 9000, nil, nil, nil) // nil manager

	req := httptest.NewRequest("GET", "/processes/any-name", nil)
	w := httptest.NewRecorder()

	server.handleProcessOrRestart(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status 503, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if errMsg, ok := response["error"].(string); !ok || errMsg != "process manager not available" {
		t.Errorf("Expected 'process manager not available' error, got: %v", response["error"])
	}
}

// Batch 2: Happy path tests with real processes

func TestHandleProcessDetail_Success_Running(t *testing.T) {
	buffer := storage.NewRingBuffer(100, 30*time.Minute, 50*1024*1024)

	// Create manager with a long-running process
	configs := []process.ProcessConfig{
		{Name: "test-sleep", Command: "sleep", Args: []string{"10"}},
	}
	manager := process.NewManager(configs, nil)
	if err := manager.Start(); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}
	defer manager.Stop()

	if err := waitForManagerPIDs(manager, 1*time.Second); err != nil {
		t.Fatalf("Process didn't start: %v", err)
	}

	server := NewServer(buffer, 9000, nil, manager, nil)

	req := httptest.NewRequest("GET", "/processes/test-sleep", nil)
	w := httptest.NewRecorder()

	server.handleProcessOrRestart(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var info process.ProcessInfo
	if err := json.NewDecoder(w.Body).Decode(&info); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Verify fields
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
	if !strings.Contains(info.Command, "sleep") {
		t.Errorf("Expected command to contain 'sleep', got '%s'", info.Command)
	}
}

func TestHandleProcessDetail_Success_Stopped(t *testing.T) {
	buffer := storage.NewRingBuffer(100, 30*time.Minute, 50*1024*1024)

	// Create manager with a quick process
	configs := []process.ProcessConfig{
		{Name: "test-echo", Command: "echo", Args: []string{"done"}},
	}
	manager := process.NewManager(configs, nil)
	if err := manager.Start(); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}
	defer manager.Stop()

	// Wait for process to actually complete
	manager.Wait()

	server := NewServer(buffer, 9000, nil, manager, nil)

	req := httptest.NewRequest("GET", "/processes/test-echo", nil)
	w := httptest.NewRecorder()

	server.handleProcessOrRestart(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var info process.ProcessInfo
	if err := json.NewDecoder(w.Body).Decode(&info); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Verify fields
	if info.Name != "test-echo" {
		t.Errorf("Expected name 'test-echo', got '%s'", info.Name)
	}
	if info.Status != "stopped" {
		t.Errorf("Expected status 'stopped', got '%s'", info.Status)
	}
	if info.ExitCode != 0 {
		t.Errorf("Expected exit_code 0 for stopped process, got %d", info.ExitCode)
	}
}

func TestHandleProcessDetail_NotFound(t *testing.T) {
	buffer := storage.NewRingBuffer(100, 30*time.Minute, 50*1024*1024)

	// Create manager with known processes
	configs := []process.ProcessConfig{
		{Name: "proc1", Command: "sleep", Args: []string{"10"}},
		{Name: "proc2", Command: "sleep", Args: []string{"10"}},
	}
	manager := process.NewManager(configs, nil)
	if err := manager.Start(); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}
	defer manager.Stop()

	if err := waitForManagerPIDs(manager, 1*time.Second); err != nil {
		t.Fatalf("Processes didn't start: %v", err)
	}

	server := NewServer(buffer, 9000, nil, manager, nil)

	req := httptest.NewRequest("GET", "/processes/nonexistent", nil)
	w := httptest.NewRecorder()

	server.handleProcessOrRestart(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	errMsg, ok := response["error"].(string)
	if !ok {
		t.Fatal("Expected error message in response")
	}

	// Should mention the process and list available ones
	if !strings.Contains(errMsg, "nonexistent") {
		t.Errorf("Error should mention 'nonexistent', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "proc1") || !strings.Contains(errMsg, "proc2") {
		t.Errorf("Error should list available processes (proc1, proc2), got: %s", errMsg)
	}
}

func TestHandleProcessDetail_WhitespaceOnly(t *testing.T) {
	buffer := storage.NewRingBuffer(100, 30*time.Minute, 50*1024*1024)
	server := NewServer(buffer, 9000, nil, nil, nil)

	// URL encode spaces - %20 for space
	req := httptest.NewRequest("GET", "/processes/%20%20%20", nil)
	w := httptest.NewRecorder()

	server.handleProcessOrRestart(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if errMsg, ok := response["error"].(string); !ok || errMsg != "process name required" {
		t.Errorf("Expected 'process name required' error, got: %v", response["error"])
	}
}

// Test helper for polling

// waitForManagerPIDs waits for all processes in manager to have PIDs
func waitForManagerPIDs(m *process.Manager, timeout time.Duration) error {
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

// Tests for POST /processes/{name}/restart

func TestHandleProcessRestart_Success(t *testing.T) {
	buffer := storage.NewRingBuffer(100, 30*time.Minute, 50*1024*1024)

	configs := []process.ProcessConfig{
		{Name: "test-echo", Command: "echo", Args: []string{"test"}},
	}
	manager := process.NewManager(configs, nil)
	if err := manager.Start(); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}
	defer manager.Stop()

	if err := waitForManagerPIDs(manager, 1*time.Second); err != nil {
		t.Fatalf("Process didn't start: %v", err)
	}

	server := NewServer(buffer, 9000, nil, manager, nil)

	// Get original PID
	info1, _ := manager.GetProcess("test-echo")
	originalPID := info1.PID

	// Restart the process
	req := httptest.NewRequest("POST", "/processes/test-echo/restart", nil)
	req.RemoteAddr = "127.0.0.1:12345" // process control is loopback-only
	w := httptest.NewRecorder()

	server.handleProcessOrRestart(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Check response has message and process
	if msg, ok := response["message"].(string); !ok || !strings.Contains(msg, "restarted successfully") {
		t.Errorf("Expected success message, got: %v", response["message"])
	}

	processData, ok := response["process"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected 'process' field in response")
	}

	// Wait a bit for restart to complete
	time.Sleep(50 * time.Millisecond)

	// Verify PID changed
	info2, _ := manager.GetProcess("test-echo")
	newPID := info2.PID

	if newPID == originalPID {
		t.Errorf("PID should have changed after restart, was %d, still %d", originalPID, newPID)
	}

	// Verify process info in response
	if name, ok := processData["name"].(string); !ok || name != "test-echo" {
		t.Errorf("Expected process name 'test-echo', got %v", processData["name"])
	}
}

func TestHandleProcessRestart_NotFound(t *testing.T) {
	buffer := storage.NewRingBuffer(100, 30*time.Minute, 50*1024*1024)

	configs := []process.ProcessConfig{
		{Name: "existing", Command: "sleep", Args: []string{"10"}},
	}
	manager := process.NewManager(configs, nil)
	if err := manager.Start(); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}
	defer manager.Stop()

	server := NewServer(buffer, 9000, nil, manager, nil)

	req := httptest.NewRequest("POST", "/processes/nonexistent/restart", nil)
	req.RemoteAddr = "127.0.0.1:12345" // process control is loopback-only
	w := httptest.NewRecorder()

	server.handleProcessOrRestart(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	errMsg, ok := response["error"].(string)
	if !ok || !strings.Contains(errMsg, "not found") {
		t.Errorf("Expected 'not found' error, got: %v", response["error"])
	}
}

func TestHandleProcessRestart_WrongMethod(t *testing.T) {
	buffer := storage.NewRingBuffer(100, 30*time.Minute, 50*1024*1024)

	configs := []process.ProcessConfig{
		{Name: "test-proc", Command: "sleep", Args: []string{"10"}},
	}
	manager := process.NewManager(configs, nil)
	if err := manager.Start(); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}
	defer manager.Stop()

	server := NewServer(buffer, 9000, nil, manager, nil)

	// Try GET on restart endpoint
	req := httptest.NewRequest("GET", "/processes/test-proc/restart", nil)
	w := httptest.NewRecorder()

	server.handleProcessOrRestart(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", w.Code)
	}
}

func TestHandleProcessRestart_NoManager(t *testing.T) {
	buffer := storage.NewRingBuffer(100, 30*time.Minute, 50*1024*1024)
	server := NewServer(buffer, 9000, nil, nil, nil) // nil manager

	req := httptest.NewRequest("POST", "/processes/any-proc/restart", nil)
	req.RemoteAddr = "127.0.0.1:12345" // process control is loopback-only
	w := httptest.NewRecorder()

	server.handleProcessOrRestart(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status 503, got %d", w.Code)
	}
}

// Tests for POST /processes/stop-all

func TestHandleStopAll_Success(t *testing.T) {
	buffer := storage.NewRingBuffer(100, 30*time.Minute, 50*1024*1024)

	// Create manager with 3 long-running processes
	configs := []process.ProcessConfig{
		{Name: "proc1", Command: "sleep", Args: []string{"10"}},
		{Name: "proc2", Command: "sleep", Args: []string{"10"}},
		{Name: "proc3", Command: "sleep", Args: []string{"10"}},
	}
	manager := process.NewManager(configs, nil)
	if err := manager.Start(); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}
	defer manager.Stop()

	if err := waitForManagerPIDs(manager, 1*time.Second); err != nil {
		t.Fatalf("Processes didn't start: %v", err)
	}

	server := NewServer(buffer, 9000, nil, manager, nil)

	// Verify all processes are running
	infos := manager.ListProcesses()
	runningCount := 0
	for _, info := range infos {
		if info.Status == "running" {
			runningCount++
		}
	}
	if runningCount != 3 {
		t.Errorf("Expected 3 running processes, got %d", runningCount)
	}

	// Stop all processes
	req := httptest.NewRequest("POST", "/processes/stop-all", nil)
	req.RemoteAddr = "127.0.0.1:12345" // process control is loopback-only
	w := httptest.NewRecorder()

	server.handleStopAll(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Check response message and count
	if msg, ok := response["message"].(string); !ok || !strings.Contains(msg, "Stopped") {
		t.Errorf("Expected 'Stopped' message, got: %v", response["message"])
	}

	count := int(response["count"].(float64))
	if count != 3 {
		t.Errorf("Expected count=3, got %d", count)
	}

	// Wait for processes to actually terminate
	if err := manager.Wait(); err != nil {
		t.Logf("Warning: Wait returned error (may be expected): %v", err)
	}

	// Verify all processes have exited
	infos = manager.ListProcesses()
	for _, info := range infos {
		if info.Status == "running" {
			t.Errorf("Expected all processes to be stopped, but %s is still running", info.Name)
		}
	}
}

func TestHandleStopAll_NoProcesses(t *testing.T) {
	buffer := storage.NewRingBuffer(100, 30*time.Minute, 50*1024*1024)

	// Create manager with no processes
	configs := []process.ProcessConfig{}
	manager := process.NewManager(configs, nil)
	if err := manager.Start(); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}
	defer manager.Stop()

	server := NewServer(buffer, 9000, nil, manager, nil)

	req := httptest.NewRequest("POST", "/processes/stop-all", nil)
	req.RemoteAddr = "127.0.0.1:12345" // process control is loopback-only
	w := httptest.NewRecorder()

	server.handleStopAll(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	count := int(response["count"].(float64))
	if count != 0 {
		t.Errorf("Expected count=0 for no processes, got %d", count)
	}
}

func TestHandleStopAll_MixedStates(t *testing.T) {
	buffer := storage.NewRingBuffer(100, 30*time.Minute, 50*1024*1024)

	// Create manager with mix of quick and long-running processes
	configs := []process.ProcessConfig{
		{Name: "quick", Command: "echo", Args: []string{"done"}},
		{Name: "long", Command: "sleep", Args: []string{"10"}},
	}
	manager := process.NewManager(configs, nil)
	if err := manager.Start(); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}
	defer manager.Stop()

	// Wait for all PIDs to be assigned
	if err := waitForManagerPIDs(manager, 1*time.Second); err != nil {
		t.Fatalf("Processes didn't start: %v", err)
	}

	// Give quick process time to finish
	time.Sleep(100 * time.Millisecond)

	server := NewServer(buffer, 9000, nil, manager, nil)

	// Stop-all when some processes are stopped and some running
	req := httptest.NewRequest("POST", "/processes/stop-all", nil)
	req.RemoteAddr = "127.0.0.1:12345" // process control is loopback-only
	w := httptest.NewRecorder()

	server.handleStopAll(w, req)

	// Should succeed even with mixed states
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
		return
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Should report count of all processes
	count := int(response["count"].(float64))
	if count != 2 {
		t.Errorf("Expected count=2, got %d", count)
	}
}

func TestHandleStopAll_WrongMethod(t *testing.T) {
	buffer := storage.NewRingBuffer(100, 30*time.Minute, 50*1024*1024)

	configs := []process.ProcessConfig{
		{Name: "proc", Command: "sleep", Args: []string{"10"}},
	}
	manager := process.NewManager(configs, nil)
	if err := manager.Start(); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}
	defer manager.Stop()

	server := NewServer(buffer, 9000, nil, manager, nil)

	// Try GET instead of POST
	req := httptest.NewRequest("GET", "/processes/stop-all", nil)
	w := httptest.NewRecorder()

	server.handleStopAll(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	errMsg, ok := response["error"].(string)
	if !ok || !strings.Contains(errMsg, "method not allowed") {
		t.Errorf("Expected 'method not allowed' error, got: %v", response["error"])
	}
}

func TestHandleStopAll_NoManager(t *testing.T) {
	buffer := storage.NewRingBuffer(100, 30*time.Minute, 50*1024*1024)
	server := NewServer(buffer, 9000, nil, nil, nil) // nil manager

	req := httptest.NewRequest("POST", "/processes/stop-all", nil)
	req.RemoteAddr = "127.0.0.1:12345" // process control is loopback-only
	w := httptest.NewRecorder()

	server.handleStopAll(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status 503, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	errMsg, ok := response["error"].(string)
	if !ok || !strings.Contains(errMsg, "not available") {
		t.Errorf("Expected 'not available' error, got: %v", response["error"])
	}
}

// Tests for GET /

func TestHandleRoot_Success(t *testing.T) {
	server, _ := setupTestServer()

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	server.handleRoot(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Check response structure
	if name, ok := response["name"].(string); !ok || name == "" {
		t.Errorf("Expected 'name' field, got: %v", response["name"])
	}

	if version, ok := response["version"].(string); !ok || version == "" {
		t.Errorf("Expected 'version' field, got: %v", response["version"])
	}

	endpoints, ok := response["endpoints"].([]interface{})
	if !ok {
		t.Fatal("Expected 'endpoints' array in response")
	}

	if len(endpoints) < 5 {
		t.Errorf("Expected at least 5 endpoints, got %d", len(endpoints))
	}

	// Verify endpoint structure
	firstEndpoint := endpoints[0].(map[string]interface{})
	if _, ok := firstEndpoint["path"]; !ok {
		t.Error("Expected 'path' field in endpoint")
	}
	if _, ok := firstEndpoint["method"]; !ok {
		t.Error("Expected 'method' field in endpoint")
	}
	if _, ok := firstEndpoint["description"]; !ok {
		t.Error("Expected 'description' field in endpoint")
	}
}

func TestHandleRoot_NotFound(t *testing.T) {
	server, _ := setupTestServer()

	// Request a non-root path
	req := httptest.NewRequest("GET", "/nonexistent", nil)
	w := httptest.NewRecorder()

	server.handleRoot(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if errMsg, ok := response["error"].(string); !ok || !strings.Contains(errMsg, "not found") {
		t.Errorf("Expected 'not found' error, got: %v", response["error"])
	}
}

// --- Process control is restricted to loopback callers (review finding R1) ---
//
// Both servers bind all interfaces by default so containers, browsers and other
// devices can export telemetry. That also exposed POST /processes/stop-all and
// POST /processes/{name}/restart to anyone on the network, unauthenticated --
// verified exploitable from a LAN address before this guard existed.

func TestIsLoopbackRequest(t *testing.T) {
	tests := []struct {
		remoteAddr string
		want       bool
	}{
		{"127.0.0.1:12345", true},
		{"127.0.0.53:9999", true}, // all of 127.0.0.0/8 is loopback
		{"[::1]:12345", true},     // IPv6 loopback: an agent curling localhost may use either
		{"192.168.1.42:54321", false},
		{"10.0.0.5:1", false},
		{"[2001:db8::1]:443", false},
		{"", false},         // malformed: fail closed
		{"garbage", false},  // unparseable: fail closed
		{"127.0.0.1", true}, // no port at all
		{"not-an-ip:80", false},
	}
	for _, tt := range tests {
		req := httptest.NewRequest("POST", "/processes/stop-all", nil)
		req.RemoteAddr = tt.remoteAddr
		if got := isLoopbackRequest(req); got != tt.want {
			t.Errorf("isLoopbackRequest(%q) = %v, want %v", tt.remoteAddr, got, tt.want)
		}
	}
}

func TestStopAll_DeniedFromRemoteAddr(t *testing.T) {
	buffer := storage.NewRingBuffer(100, 30*time.Minute, 50*1024*1024)
	server := NewServer(buffer, 9000, nil, nil, nil)

	req := httptest.NewRequest("POST", "/processes/stop-all", nil)
	req.RemoteAddr = "192.168.1.42:54321"
	w := httptest.NewRecorder()

	server.handleStopAll(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a remote caller, got %d", w.Code)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("403 body is not JSON: %v", err)
	}
	// The denial must explain itself: these endpoints are documented and listed
	// by GET /, so a bare 403 would look like a bug.
	if body["remote_addr"] != "192.168.1.42" {
		t.Errorf("403 should name the caller's address, got %v", body["remote_addr"])
	}
	for _, k := range []string{"error", "detail", "allow", "docs"} {
		if v, ok := body[k].(string); !ok || v == "" {
			t.Errorf("403 body missing explanatory field %q", k)
		}
	}
	if !strings.Contains(body["allow"].(string), "--allow-remote-control") {
		t.Errorf("403 should say how to allow it, got %q", body["allow"])
	}
}

func TestProcessRestart_DeniedFromRemoteAddr(t *testing.T) {
	buffer := storage.NewRingBuffer(100, 30*time.Minute, 50*1024*1024)
	server := NewServer(buffer, 9000, nil, nil, nil)

	req := httptest.NewRequest("POST", "/processes/whatever/restart", nil)
	req.RemoteAddr = "10.1.2.3:4567"
	w := httptest.NewRecorder()

	server.handleProcessRestart(w, req, "whatever/restart")

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a remote caller, got %d", w.Code)
	}
	// Denial must happen before the manager is consulted: a nil manager would
	// otherwise return 503 and leak that the endpoint was reachable.
	if got := w.Body.String(); !strings.Contains(got, "restricted to local requests") {
		t.Errorf("unexpected 403 body: %s", got)
	}
}

func TestAllowRemoteControl_OpensStateChangingEndpoints(t *testing.T) {
	buffer := storage.NewRingBuffer(100, 30*time.Minute, 50*1024*1024)
	server := NewServer(buffer, 9000, nil, nil, nil)
	server.SetAllowRemoteControl(true)

	req := httptest.NewRequest("POST", "/processes/stop-all", nil)
	req.RemoteAddr = "192.168.1.42:54321"
	w := httptest.NewRecorder()

	server.handleStopAll(w, req)

	// With the escape hatch set, the guard must not fire. A nil manager means
	// 503 here, which is fine -- it proves we got past the 403.
	if w.Code == http.StatusForbidden {
		t.Fatalf("--allow-remote-control should bypass the loopback guard, got 403")
	}
}

func TestRoot_MarksLocalOnlyEndpoints(t *testing.T) {
	buffer := storage.NewRingBuffer(100, 30*time.Minute, 50*1024*1024)
	server := NewServer(buffer, 9000, nil, nil, nil)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	server.handleRoot(w, req)

	var body struct {
		Endpoints []struct {
			Path      string `json:"path"`
			LocalOnly bool   `json:"local_only"`
			Note      string `json:"note"`
		} `json:"endpoints"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("root response is not JSON: %v", err)
	}

	// GET / is how an agent discovers the surface, so the restriction has to be
	// visible there rather than only on refusal.
	want := map[string]bool{
		"/processes/{name}/restart": true,
		"/processes/stop-all":       true,
	}
	seen := map[string]bool{}
	for _, e := range body.Endpoints {
		if want[e.Path] {
			seen[e.Path] = true
			if !e.LocalOnly {
				t.Errorf("%s should be marked local_only", e.Path)
			}
			if e.Note == "" {
				t.Errorf("%s should carry an explanatory note", e.Path)
			}
		}
	}
	for p := range want {
		if !seen[p] {
			t.Errorf("root listing is missing %s", p)
		}
	}
}

// --- Review findings R4 and R5: the agent-facing API describing itself truthfully ---

// R4: parser.LogEntry had no json tags, so it serialised under Go field names
// ("Timestamp", "IsError") while docs/openapi.yaml documented snake_case and
// every other endpoint used snake_case. An agent following /docs -- the
// discovery mechanism the tool depends on -- got every field name wrong on the
// endpoint it uses most.
func TestLogsResponse_UsesSnakeCaseFieldNames(t *testing.T) {
	buffer := storage.NewRingBuffer(100, 30*time.Minute, 50*1024*1024)
	buffer.Append(&parser.LogEntry{
		Timestamp:  time.Now(),
		Level:      parser.LevelError,
		Source:     "backend",
		SourceType: "process",
		Message:    "boom",
		Raw:        "boom",
		IsError:    true,
		Stacktrace: "trace here",
		TraceID:    "abc123",
	})
	server := NewServer(buffer, 9000, nil, nil, nil)

	req := httptest.NewRequest("GET", "/logs", nil)
	w := httptest.NewRecorder()
	server.handleLogs(w, req)

	var body struct {
		Logs []map[string]interface{} `json:"logs"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if len(body.Logs) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(body.Logs))
	}
	entry := body.Logs[0]

	// These are the names docs/openapi.yaml promises.
	for _, key := range []string{
		"timestamp", "level", "source", "source_type",
		"message", "raw", "is_error", "stacktrace", "trace_id",
	} {
		if _, ok := entry[key]; !ok {
			t.Errorf("missing documented field %q; got keys %v", key, keysOf(entry))
		}
	}

	// And the old Go-name keys must be gone, so nothing depends on them.
	for _, key := range []string{"Timestamp", "Level", "Source", "Message", "IsError", "TraceID"} {
		if _, ok := entry[key]; ok {
			t.Errorf("field %q is still serialised under its Go name", key)
		}
	}
}

func keysOf(m map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// R5: `limit` was documented on /logs but never implemented -- the parameter
// was silently ignored, and a bare `curl /logs` returned the whole buffer (up
// to 10,000 entries by default). limit=notanumber returned 200 where /traces
// returned 400 for the same input.
func TestLogs_LimitIsApplied(t *testing.T) {
	buffer := storage.NewRingBuffer(1000, 30*time.Minute, 50*1024*1024)
	for i := 0; i < 50; i++ {
		buffer.Append(&parser.LogEntry{
			Timestamp: time.Now(),
			Level:     parser.LevelInfo,
			Source:    "app",
			Message:   fmt.Sprintf("line-%d", i),
			Raw:       fmt.Sprintf("line-%d", i),
		})
	}
	server := NewServer(buffer, 9000, nil, nil, nil)

	tests := []struct {
		query     string
		wantCount int
	}{
		{"/logs?limit=10", 10},
		{"/logs?limit=1", 1},
		{"/logs?limit=0", 50},   // 0 means no limit
		{"/logs?limit=999", 50}, // more than available
		{"/logs", 50},           // under the 1000 default
	}
	for _, tt := range tests {
		req := httptest.NewRequest("GET", tt.query, nil)
		w := httptest.NewRecorder()
		server.handleLogs(w, req)

		var body struct {
			Count int `json:"count"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: not JSON: %v", tt.query, err)
		}
		if body.Count != tt.wantCount {
			t.Errorf("%s: count = %d, want %d", tt.query, body.Count, tt.wantCount)
		}
	}
}

// The limit must keep the NEWEST matches: truncating to the oldest would be
// useless for debugging.
func TestLogs_LimitKeepsMostRecent(t *testing.T) {
	buffer := storage.NewRingBuffer(1000, 30*time.Minute, 50*1024*1024)
	for i := 0; i < 10; i++ {
		buffer.Append(&parser.LogEntry{
			Timestamp: time.Now(),
			Level:     parser.LevelInfo,
			Source:    "app",
			Message:   fmt.Sprintf("line-%d", i),
			Raw:       fmt.Sprintf("line-%d", i),
		})
	}
	server := NewServer(buffer, 9000, nil, nil, nil)

	req := httptest.NewRequest("GET", "/logs?limit=3", nil)
	w := httptest.NewRecorder()
	server.handleLogs(w, req)

	var body struct {
		Logs []struct {
			Message string `json:"message"`
		} `json:"logs"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if len(body.Logs) != 3 {
		t.Fatalf("got %d entries, want 3", len(body.Logs))
	}
	for i, want := range []string{"line-7", "line-8", "line-9"} {
		if body.Logs[i].Message != want {
			t.Errorf("entry %d = %q, want %q (the limit must keep the newest)", i, body.Logs[i].Message, want)
		}
	}
}

// The limit is applied after filtering, so "the 5 most recent errors" means
// that -- not "errors among the 5 most recent entries".
func TestLogs_LimitAppliesAfterFiltering(t *testing.T) {
	buffer := storage.NewRingBuffer(1000, 30*time.Minute, 50*1024*1024)
	// 20 info entries, then 3 errors, then 20 more info entries.
	add := func(level parser.LogLevel, msg string) {
		buffer.Append(&parser.LogEntry{
			Timestamp: time.Now(), Level: level, Source: "app",
			Message: msg, Raw: msg, IsError: level == parser.LevelError,
		})
	}
	for i := 0; i < 20; i++ {
		add(parser.LevelInfo, "noise")
	}
	for i := 0; i < 3; i++ {
		add(parser.LevelError, fmt.Sprintf("err-%d", i))
	}
	for i := 0; i < 20; i++ {
		add(parser.LevelInfo, "more noise")
	}

	server := NewServer(buffer, 9000, nil, nil, nil)
	req := httptest.NewRequest("GET", "/logs?level=error&limit=5", nil)
	w := httptest.NewRecorder()
	server.handleLogs(w, req)

	var body struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	// All 3 errors, despite being nowhere near the last 5 entries.
	if body.Count != 3 {
		t.Errorf("count = %d, want 3: the limit must apply after filtering", body.Count)
	}
}

func TestLogs_InvalidLimitIsRejected(t *testing.T) {
	buffer := storage.NewRingBuffer(100, 30*time.Minute, 50*1024*1024)
	server := NewServer(buffer, 9000, nil, nil, nil)

	// /traces already returned 400 for these; /logs returned 200 and ignored them.
	for _, query := range []string{"/logs?limit=notanumber", "/logs?limit=-5", "/logs?limit=1.5"} {
		req := httptest.NewRequest("GET", query, nil)
		w := httptest.NewRecorder()
		server.handleLogs(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: got HTTP %d, want 400", query, w.Code)
		}
	}
}

func TestErrors_LimitIsApplied(t *testing.T) {
	buffer := storage.NewRingBuffer(1000, 30*time.Minute, 50*1024*1024)
	for i := 0; i < 20; i++ {
		buffer.Append(&parser.LogEntry{
			Timestamp: time.Now(), Level: parser.LevelError, Source: "app",
			Message: fmt.Sprintf("err-%d", i), Raw: "e", IsError: true,
		})
	}
	server := NewServer(buffer, 9000, nil, nil, nil)

	req := httptest.NewRequest("GET", "/errors?limit=4", nil)
	w := httptest.NewRecorder()
	server.handleErrors(w, req)

	var body struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if body.Count != 4 {
		t.Errorf("count = %d, want 4", body.Count)
	}
}

// The spec served at /docs is an embedded copy of docs/openapi.yaml, synced by
// `go generate ./internal/api`. Nothing enforced that, so the two drifted: the
// embedded copy was months out of date, and /docs -- the mechanism an agent
// uses to discover the API -- was serving a spec that omitted several
// endpoints' documented behaviour.
//
// This test fails the moment they diverge again. Run `go generate ./internal/api`
// to fix it.
func TestEmbeddedOpenAPISpec_MatchesDocs(t *testing.T) {
	onDisk, err := os.ReadFile(filepath.Join("..", "..", "docs", "openapi.yaml"))
	if err != nil {
		t.Fatalf("cannot read docs/openapi.yaml: %v", err)
	}

	if !bytes.Equal(bytes.TrimSpace(onDisk), bytes.TrimSpace(openapiSpec)) {
		t.Errorf("the embedded OpenAPI spec is out of sync with docs/openapi.yaml "+
			"(embedded %d bytes, docs %d bytes).\n"+
			"Run: go generate ./internal/api",
			len(openapiSpec), len(onDisk))
	}
}
