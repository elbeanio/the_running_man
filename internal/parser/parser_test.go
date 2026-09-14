package parser

import (
	"fmt"
	"strings"
	"sync"
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
		e, consumed := parser.Parse("test", line, ts)

		// Every line of a well-formed traceback belongs to it, so all of them
		// are consumed. Only the last produces an entry.
		if !consumed {
			t.Errorf("Line %d: expected the line to be consumed by the traceback", i)
		}
		if i < len(lines)-1 {
			if e != nil {
				t.Errorf("Line %d: expected no entry while accumulating, got one", i)
			}
		} else {
			if e == nil {
				t.Error("Expected the last line to complete the traceback")
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
			got := parser.ParseLine("test", tt.input, ts)

			if tt.wantNil {
				if len(got) != 0 {
					t.Errorf("Expected no entries while accumulating, got %d", len(got))
				}
			} else {
				if len(got) != 1 {
					t.Fatalf("Expected exactly 1 entry, got %d", len(got))
				}
				if got[0].Level != tt.wantLevel {
					t.Errorf("Level = %v, want %v", got[0].Level, tt.wantLevel)
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
		entries = append(entries, parser.ParseLine("test", line, ts)...)
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

// --- Review findings R2 and R8 ---

// R2(b): a traceback open on one source must not swallow another source's
// lines. MultiParser previously shared one PythonParser across every source,
// so an indented line from "frontend" was absorbed into "backend"'s traceback:
// it vanished from the buffer entirely and reappeared inside an unrelated
// stack trace.
func TestMultiParser_TracebackDoesNotSwallowOtherSources(t *testing.T) {
	p := NewMultiParser()
	ts := time.Now()

	p.ParseLineWithType("backend", "process", "Traceback (most recent call last):", ts)
	p.ParseLineWithType("backend", "process", `  File "app.py", line 10, in handler`, ts)

	// An indented line from a different source, while backend's traceback is open.
	got := p.ParseLineWithType("frontend", "process", "  webpack: compiled successfully", ts)
	if len(got) != 1 {
		t.Fatalf("frontend's line produced %d entries, want 1 (it must not be absorbed)", len(got))
	}
	if got[0].Source != "frontend" {
		t.Errorf("entry attributed to %q, want %q", got[0].Source, "frontend")
	}
	if !strings.Contains(got[0].Message, "webpack") {
		t.Errorf("frontend's line was lost; got message %q", got[0].Message)
	}

	// backend's traceback must still complete correctly, without the intruder.
	final := p.ParseLineWithType("backend", "process", "ValueError: boom", ts)
	if len(final) != 1 {
		t.Fatalf("backend traceback produced %d entries, want 1", len(final))
	}
	if final[0].Source != "backend" {
		t.Errorf("traceback attributed to %q, want %q", final[0].Source, "backend")
	}
	if strings.Contains(final[0].Stacktrace, "webpack") {
		t.Errorf("backend's stacktrace contains frontend's line:\n%s", final[0].Stacktrace)
	}
}

// R2(a): MultiParser is used from one goroutine per stream per process, plus
// one per container. It must be safe for concurrent use.
func TestMultiParser_ConcurrentSourcesAreSafe(t *testing.T) {
	p := NewMultiParser()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			source := fmt.Sprintf("src-%d", n)
			ts := time.Now()
			for j := 0; j < 100; j++ {
				p.ParseLineWithType(source, "process", "Traceback (most recent call last):", ts)
				p.ParseLineWithType(source, "process", `  File "x.py", line 1, in f`, ts)
				p.ParseLineWithType(source, "process", "ValueError: x", ts)
				p.ParseLineWithType(source, "process", "plain line", ts)
			}
		}(i)
	}
	wg.Wait()
}

// Two readers share one source name (stdout and stderr of the same process),
// so per-source state must be locked too, not just isolated.
func TestMultiParser_ConcurrentSameSourceIsSafe(t *testing.T) {
	p := NewMultiParser()

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ts := time.Now()
			for j := 0; j < 200; j++ {
				p.ParseLineWithType("shared", "process", "Traceback (most recent call last):", ts)
				p.ParseLineWithType("shared", "process", "ValueError: x", ts)
			}
		}()
	}
	wg.Wait()
}

// R8: the line that terminates a traceback without belonging to it was
// dropped. The parser reported it as consumed, the caller moved on, and it
// never reached the buffer -- so the first ordinary log line after any
// traceback disappeared.
func TestMultiParser_TracebackTerminatingLineIsKept(t *testing.T) {
	p := NewMultiParser()
	ts := time.Now()

	p.ParseLineWithType("app", "process", "Traceback (most recent call last):", ts)
	p.ParseLineWithType("app", "process", `  File "app.py", line 1, in <module>`, ts)

	// Neither indented nor an exception line: ends the traceback, and is a log
	// line in its own right.
	got := p.ParseLineWithType("app", "process", "INFO server still running", ts)

	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2 (the traceback, and the line that ended it)", len(got))
	}
	if got[0].Stacktrace == "" {
		t.Errorf("first entry should be the traceback, got %+v", got[0])
	}
	if !strings.Contains(got[1].Message, "server still running") {
		t.Errorf("terminating line was lost; second entry message = %q", got[1].Message)
	}
}

// A JSON line arriving mid-traceback must close the traceback rather than
// being emitted alone and leaving it open to swallow later lines.
func TestMultiParser_JSONLineEndsTraceback(t *testing.T) {
	p := NewMultiParser()
	ts := time.Now()

	p.ParseLineWithType("app", "process", "Traceback (most recent call last):", ts)
	p.ParseLineWithType("app", "process", `  File "app.py", line 1, in <module>`, ts)

	got := p.ParseLineWithType("app", "process", `{"level":"info","message":"still here"}`, ts)
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2 (traceback + the JSON line)", len(got))
	}

	// And the traceback must be closed, so a later indented line is ordinary.
	after := p.ParseLineWithType("app", "process", "  indented but unrelated", ts)
	if len(after) != 1 {
		t.Fatalf("got %d entries after the traceback closed, want 1", len(after))
	}
	if after[0].Stacktrace != "" {
		t.Error("traceback was still open; the later line was absorbed into it")
	}
}

// Review finding R22: errorLineRegex was `^(\w+Error|Exception|Warning):\s+(.*)`,
// which missed a lot of real Python. Those tracebacks never terminated
// cleanly and fell through to the "any other line ends it" path, losing their
// exception line as the message.
func TestPythonParser_RecognisesRealExceptionLines(t *testing.T) {
	shouldTerminate := []string{
		"ValueError: invalid literal",
		"ValueError:",                        // no message: \s+ used to require one
		"KeyboardInterrupt",                  // bare, no colon -- what Ctrl-C prints
		"KeyboardInterrupt: ",                //
		"SystemExit: 1",                      // not an *Error
		"StopIteration",                      // not an *Error, bare
		"requests.exceptions.HTTPError: 404", // dotted module path
		"AssertionError: failed",
		"RecursionError: maximum recursion depth exceeded",
		"GeneratorExit",
	}
	for _, line := range shouldTerminate {
		p := NewPythonParser()
		ts := time.Now()
		p.Parse("app", "Traceback (most recent call last):", ts)
		p.Parse("app", `  File "app.py", line 1, in <module>`, ts)

		entry, consumed := p.Parse("app", line, ts)
		if entry == nil {
			t.Errorf("%q did not terminate the traceback", line)
			continue
		}
		if !consumed {
			t.Errorf("%q should belong to the traceback it terminates", line)
		}
		if entry.Message != line {
			t.Errorf("message = %q, want %q", entry.Message, line)
		}
	}
}

// And ordinary log lines must NOT be mistaken for exception lines, or a
// traceback would end early with the wrong message attached.
func TestPythonParser_OrdinaryLinesAreNotExceptions(t *testing.T) {
	notExceptions := []string{
		"Starting",
		"Done",
		"Listening on port 8000",
		"INFO all good",
		"lowercase: thing",
	}
	for _, line := range notExceptions {
		p := NewPythonParser()
		ts := time.Now()
		p.Parse("app", "Traceback (most recent call last):", ts)
		p.Parse("app", `  File "app.py", line 1, in <module>`, ts)

		entry, consumed := p.Parse("app", line, ts)
		// These still END the traceback (any other line does), but must not be
		// absorbed into it as the exception line.
		if consumed {
			t.Errorf("%q was treated as part of the traceback; it is an ordinary log line", line)
		}
		if entry != nil && entry.Message == line {
			t.Errorf("%q was used as the traceback's error message", line)
		}
	}
}
