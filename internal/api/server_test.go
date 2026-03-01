package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iangeorge/the_running_man/internal/parser"
	"github.com/iangeorge/the_running_man/internal/process"
	"github.com/iangeorge/the_running_man/internal/storage"
)

func setupTestServer() (*Server, *storage.RingBuffer) {
	buffer := storage.NewRingBuffer(100, 30*time.Minute, 50*1024*1024)
	server := NewServer(buffer, 9000, nil, nil) // nil lineHandler and manager for tests
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

	server := NewServer(buffer, 9000, lineHandler, nil)

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
	server := NewServer(buffer, 9000, nil, nil)

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

	server := NewServer(buffer, 9000, lineHandler, nil)

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

func TestMCP_ToolRegistration(t *testing.T) {
	server, _ := setupTestServer()

	// This test validates that all MCP tools can be registered without panicking.
	// It catches issues with malformed jsonschema tags or incorrect handler signatures.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("MCP tool registration panicked: %v", r)
		}
	}()

	// Create MCP handler - this registers all tools
	_ = server.createMCPHandler()
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

	server := NewServer(buffer, 9000, lineHandler, nil)

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
	server := NewServer(buffer, 9000, nil, nil)

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

	if errMsg, ok := response["error"].(string); !ok || !strings.Contains(errMsg, "Invalid process name") {
		t.Errorf("Expected 'Invalid process name' error, got: %v", response["error"])
	}
}

func TestHandleProcessDetail_InvalidName_DotDot(t *testing.T) {
	buffer := storage.NewRingBuffer(100, 30*time.Minute, 50*1024*1024)
	server := NewServer(buffer, 9000, nil, nil)

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

	if errMsg, ok := response["error"].(string); !ok || !strings.Contains(errMsg, "Invalid process name") {
		t.Errorf("Expected 'Invalid process name' error, got: %v", response["error"])
	}
}

func TestHandleProcessDetail_TooLong(t *testing.T) {
	buffer := storage.NewRingBuffer(100, 30*time.Minute, 50*1024*1024)
	server := NewServer(buffer, 9000, nil, nil)

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

	if errMsg, ok := response["error"].(string); !ok || errMsg != "Process name too long" {
		t.Errorf("Expected 'Process name too long' error, got: %v", response["error"])
	}
}

func TestHandleProcessDetail_EmptyName(t *testing.T) {
	buffer := storage.NewRingBuffer(100, 30*time.Minute, 50*1024*1024)
	server := NewServer(buffer, 9000, nil, nil)

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

	if errMsg, ok := response["error"].(string); !ok || errMsg != "Process name required" {
		t.Errorf("Expected 'Process name required' error, got: %v", response["error"])
	}
}

func TestHandleProcessDetail_NoManager(t *testing.T) {
	buffer := storage.NewRingBuffer(100, 30*time.Minute, 50*1024*1024)
	server := NewServer(buffer, 9000, nil, nil) // nil manager

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

	if errMsg, ok := response["error"].(string); !ok || errMsg != "Process manager not available" {
		t.Errorf("Expected 'Process manager not available' error, got: %v", response["error"])
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

	server := NewServer(buffer, 9000, nil, manager)

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

	server := NewServer(buffer, 9000, nil, manager)

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

	server := NewServer(buffer, 9000, nil, manager)

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
	server := NewServer(buffer, 9000, nil, nil)

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

	if errMsg, ok := response["error"].(string); !ok || errMsg != "Process name required" {
		t.Errorf("Expected 'Process name required' error, got: %v", response["error"])
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

	server := NewServer(buffer, 9000, nil, manager)

	// Get original PID
	info1, _ := manager.GetProcess("test-echo")
	originalPID := info1.PID

	// Restart the process
	req := httptest.NewRequest("POST", "/processes/test-echo/restart", nil)
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

	server := NewServer(buffer, 9000, nil, manager)

	req := httptest.NewRequest("POST", "/processes/nonexistent/restart", nil)
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

	server := NewServer(buffer, 9000, nil, manager)

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
	server := NewServer(buffer, 9000, nil, nil) // nil manager

	req := httptest.NewRequest("POST", "/processes/any-proc/restart", nil)
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

	server := NewServer(buffer, 9000, nil, manager)

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

	server := NewServer(buffer, 9000, nil, manager)

	req := httptest.NewRequest("POST", "/processes/stop-all", nil)
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

	server := NewServer(buffer, 9000, nil, manager)

	// Stop-all when some processes are stopped and some running
	req := httptest.NewRequest("POST", "/processes/stop-all", nil)
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

	server := NewServer(buffer, 9000, nil, manager)

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
	if !ok || !strings.Contains(errMsg, "Method not allowed") {
		t.Errorf("Expected 'Method not allowed' error, got: %v", response["error"])
	}
}

func TestHandleStopAll_NoManager(t *testing.T) {
	buffer := storage.NewRingBuffer(100, 30*time.Minute, 50*1024*1024)
	server := NewServer(buffer, 9000, nil, nil) // nil manager

	req := httptest.NewRequest("POST", "/processes/stop-all", nil)
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

	if errMsg, ok := response["error"].(string); !ok || !strings.Contains(errMsg, "Not found") {
		t.Errorf("Expected 'Not found' error, got: %v", response["error"])
	}
}
