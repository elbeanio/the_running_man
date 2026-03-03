package parser

import (
	"strings"
	"testing"
	"time"
)

func TestJSONParser(t *testing.T) {
	parser := NewJSONParser()
	ts := time.Now()

	tests := []struct {
		name        string
		input       string
		wantLevel   LogLevel
		wantMessage string
		wantParsed  bool
		wantTraceID string
	}{
		{
			name:        "basic json log",
			input:       `{"level":"info","message":"server started","timestamp":"2024-01-01T12:00:00Z"}`,
			wantLevel:   LevelInfo,
			wantMessage: "server started",
			wantParsed:  true,
			wantTraceID: "",
		},
		{
			name:        "json error with stack",
			input:       `{"level":"error","msg":"database failed","stacktrace":"at function1()\\nat function2()"}`,
			wantLevel:   LevelError,
			wantMessage: "database failed",
			wantParsed:  true,
			wantTraceID: "",
		},
		{
			name:        "not json",
			input:       "plain text log line",
			wantParsed:  false,
			wantTraceID: "",
		},
		{
			name:        "json with trace_id",
			input:       `{"level":"info","message":"processing request","trace_id":"abc123","span_id":"def456"}`,
			wantLevel:   LevelInfo,
			wantMessage: "processing request",
			wantParsed:  true,
			wantTraceID: "abc123",
		},
		{
			name:        "json with traceId (camelCase)",
			input:       `{"level":"error","msg":"database error","traceId":"xyz789"}`,
			wantLevel:   LevelError,
			wantMessage: "database error",
			wantParsed:  true,
			wantTraceID: "xyz789",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry, ok := parser.Parse("test", tt.input, ts)

			if ok != tt.wantParsed {
				t.Errorf("Parse() ok = %v, want %v", ok, tt.wantParsed)
			}

			if ok && entry != nil {
				if entry.Level != tt.wantLevel {
					t.Errorf("Level = %v, want %v", entry.Level, tt.wantLevel)
				}
				if entry.Message != tt.wantMessage {
					t.Errorf("Message = %v, want %v", entry.Message, tt.wantMessage)
				}
				if entry.Level == LevelError && !entry.IsError {
					t.Error("Expected IsError = true for error level")
				}
				if entry.TraceID != tt.wantTraceID {
					t.Errorf("TraceID = %v, want %v", entry.TraceID, tt.wantTraceID)
				}
			}
		})
	}
}

func TestPythonParser(t *testing.T) {
	parser := NewPythonParser()
	ts := time.Now()

	lines := []string{
		"Traceback (most recent call last):",
		`  File "/app/main.py", line 42, in main`,
		"    result = divide(10, 0)",
		`  File "/app/math.py", line 5, in divide`,
		"    return a / b",
		"ZeroDivisionError: division by zero",
	}

	var entry *LogEntry
	for i, line := range lines {
		e, ok := parser.Parse("test", line, ts)
		if i < len(lines)-1 {
			// Should be accumulating
			if ok {
				t.Errorf("Line %d: Expected ok=false (accumulating), got true", i)
			}
		} else {
			// Last line should complete the traceback
			if !ok {
				t.Error("Expected ok=true on last line")
			}
			entry = e
		}
	}

	if entry == nil {
		t.Fatal("Expected entry to be returned")
	}

	if entry.Level != LevelError {
		t.Errorf("Level = %v, want %v", entry.Level, LevelError)
	}

	if !entry.IsError {
		t.Error("Expected IsError = true")
	}

	if !strings.Contains(entry.Stacktrace, "Traceback") {
		t.Error("Stacktrace should contain 'Traceback'")
	}

	if !strings.Contains(entry.Stacktrace, "ZeroDivisionError") {
		t.Error("Stacktrace should contain 'ZeroDivisionError'")
	}

	if !strings.Contains(entry.Message, "ZeroDivisionError") {
		t.Error("Message should contain error type")
	}
}

func TestPythonParser_PartialTraceback(t *testing.T) {
	parser := NewPythonParser()
	ts := time.Now()

	// Start a traceback but don't finish it
	parser.Parse("test", "Traceback (most recent call last):", ts)
	parser.Parse("test", `  File "test.py", line 1, in main`, ts)

	if !parser.IsInProgress() {
		t.Error("Expected parser to be in progress")
	}

	// Flush should return the partial traceback
	entry := parser.Flush("test")
	if entry == nil {
		t.Fatal("Expected flush to return entry")
	}

	// After flush, should be reset
	if parser.IsInProgress() {
		t.Error("Parser should not be in progress after flush")
	}
}

func TestPlainTextParser(t *testing.T) {
	parser := NewPlainTextParser()
	ts := time.Now()

	tests := []struct {
		name        string
		input       string
		wantLevel   LogLevel
		wantIsError bool
	}{
		{
			name:        "error keyword",
			input:       "ERROR: Connection failed to database",
			wantLevel:   LevelError,
			wantIsError: true,
		},
		{
			name:        "warning keyword",
			input:       "WARNING: Deprecated API usage",
			wantLevel:   LevelWarn,
			wantIsError: false,
		},
		{
			name:        "debug keyword",
			input:       "DEBUG: Processing request",
			wantLevel:   LevelDebug,
			wantIsError: false,
		},
		{
			name:        "info explicit",
			input:       "[INFO] Server started on port 8080",
			wantLevel:   LevelInfo,
			wantIsError: false,
		},
		{
			name:        "plain text default",
			input:       "Some regular log message",
			wantLevel:   LevelInfo,
			wantIsError: false,
		},
		{
			name:        "exception keyword",
			input:       "Caught exception while processing",
			wantLevel:   LevelError,
			wantIsError: true,
		},
		{
			name:        "failed keyword",
			input:       "Failed to load configuration",
			wantLevel:   LevelError,
			wantIsError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := parser.Parse("test", tt.input, ts)

			if entry.Level != tt.wantLevel {
				t.Errorf("Level = %v, want %v", entry.Level, tt.wantLevel)
			}

			if entry.IsError != tt.wantIsError {
				t.Errorf("IsError = %v, want %v", entry.IsError, tt.wantIsError)
			}

			if entry.Message != tt.input {
				t.Errorf("Message = %v, want %v", entry.Message, tt.input)
			}
		})
	}
}

func TestMultiParser(t *testing.T) {
	parser := NewMultiParser()
	ts := time.Now()

	tests := []struct {
		name      string
		input     string
		wantLevel LogLevel
		wantNil   bool
	}{
		{
			name:      "json takes precedence",
			input:     `{"level":"error","message":"test"}`,
			wantLevel: LevelError,
		},
		{
			name:      "plain text fallback",
			input:     "ERROR: something went wrong",
			wantLevel: LevelError,
		},
		{
			name:    "python traceback start",
			input:   "Traceback (most recent call last):",
			wantNil: true, // Should return nil while accumulating
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := parser.ParseLine("test", tt.input, ts)

			if tt.wantNil {
				if entry != nil {
					t.Error("Expected nil entry while accumulating")
				}
			} else {
				if entry == nil {
					t.Fatal("Expected non-nil entry")
				}
				if entry.Level != tt.wantLevel {
					t.Errorf("Level = %v, want %v", entry.Level, tt.wantLevel)
				}
			}
		})
	}
}

func TestMultiParser_PythonTracebackComplete(t *testing.T) {
	parser := NewMultiParser()
	ts := time.Now()

	lines := []string{
		"Traceback (most recent call last):",
		`  File "test.py", line 1`,
		"ValueError: bad value",
	}

	var entries []*LogEntry
	for _, line := range lines {
		entry := parser.ParseLine("test", line, ts)
		if entry != nil {
			entries = append(entries, entry)
		}
	}

	// Should have exactly one entry (the completed traceback)
	if len(entries) != 1 {
		t.Errorf("Expected 1 entry, got %d", len(entries))
	}

	if len(entries) > 0 {
		if entries[0].Level != LevelError {
			t.Errorf("Level = %v, want %v", entries[0].Level, LevelError)
		}
		if !strings.Contains(entries[0].Stacktrace, "ValueError") {
			t.Error("Expected ValueError in stacktrace")
		}
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input string
		want  LogLevel
	}{
		{"debug", LevelDebug},
		{"DEBUG", LevelDebug},
		{"trace", LevelDebug},
		{"info", LevelInfo},
		{"INFO", LevelInfo},
		{"information", LevelInfo},
		{"warn", LevelWarn},
		{"warning", LevelWarn},
		{"error", LevelError},
		{"ERROR", LevelError},
		{"fatal", LevelError},
		{"panic", LevelError},
		{"unknown", LevelInfo},
		{"", LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseLevel(tt.input)
			if got != tt.want {
				t.Errorf("parseLevel(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
