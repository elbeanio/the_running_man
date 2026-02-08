package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/iangeorge/the_running_man/internal/parser"
	"github.com/iangeorge/the_running_man/internal/storage"
)

func setupTestServer() (*Server, *storage.RingBuffer) {
	buffer := storage.NewRingBuffer(100, 30*time.Minute, 50*1024*1024)
	server := NewServer(buffer, 9000, nil) // nil lineHandler for tests
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

	server := NewServer(buffer, 9000, lineHandler)

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
	server := NewServer(buffer, 9000, nil)

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

	server := NewServer(buffer, 9000, lineHandler)

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

	server := NewServer(buffer, 9000, lineHandler)

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
