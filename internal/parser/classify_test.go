package parser

import (
	"strings"
	"testing"
	"time"
)

// Trace-ID extraction ran only for lines that fell through to info: Parse
// returned as soon as a level matched, and the extraction sat below. Errors and
// warnings -- the lines anyone actually wants to correlate -- never got one.
func TestPlainText_TraceIDOnEveryLevel(t *testing.T) {
	p := NewPlainTextParser()
	for _, line := range []string{
		"ERROR: payment failed trace_id=4bf92f3577b34da6",
		"WARN slow query trace_id=4bf92f3577b34da6",
		"DEBUG cache miss trace_id=4bf92f3577b34da6",
		"INFO request ok trace_id=4bf92f3577b34da6",
	} {
		e := p.Parse("s", line, time.Now(), false)
		if e.TraceID != "4bf92f3577b34da6" {
			t.Errorf("%q: trace ID %q, want 4bf92f3577b34da6 (level %s)", line, e.TraceID, e.Level)
		}
	}
}

// The pattern accepted a bare "trace:" prefix with any word after it, and had no
// word boundary in front, so "backtrace: disabled" produced the trace ID
// "disabled".
func TestPlainText_TraceIDNotInventedFromWords(t *testing.T) {
	p := NewPlainTextParser()
	for _, line := range []string{
		"backtrace: disabled",
		"stack trace: enabled",
		"trace: on",
		"tracing=true",
	} {
		if e := p.Parse("s", line, time.Now(), false); e.TraceID != "" {
			t.Errorf("%q: invented trace ID %q", line, e.TraceID)
		}
	}
	// The forms that should still work.
	for line, want := range map[string]string{
		"trace_id=4bf92f3577b34da6":                     "4bf92f3577b34da6",
		"traceId: 00f067aa0ba902b7":                     "00f067aa0ba902b7",
		"trace-id=550e8400-e29b-41d4-a716-446655440000": "550e8400-e29b-41d4-a716-446655440000",
		"trace: 4bf92f3577b34da6":                       "4bf92f3577b34da6",
	} {
		if e := p.Parse("s", line, time.Now(), false); e.TraceID != want {
			t.Errorf("%q: trace ID %q, want %q", line, e.TraceID, want)
		}
	}
}

// False positives fill /errors with things that are not errors, which teaches
// the reader to ignore it -- the classifier's own comment ranks that worse than
// a miss.
func TestPlainText_NotErrors(t *testing.T) {
	p := NewPlainTextParser()
	for _, line := range []string{
		// Test runner summaries print a zero count of failures on every green run.
		"Tests: 42 passed, 0 failed",
		"42 passed, 0 failed in 1.23s",
		"=== 128 passed, 0 failures ===",
		"Test Suites: 12 passed, 0 failed, 12 total",
		"test result: ok. 15 passed; 0 failed; 0 ignored",
		"0 errors, 3 warnings",
		// The errno alternation had the wrong precedence, so two of the four
		// matched mid-word, case-insensitively.
		"reaccessing the cache",
		"loaded config from /etc/eacces.d",
		"reconnecting to beconnrefusedhost",
	} {
		if e := p.Parse("s", line, time.Now(), false); e.Level == LevelError {
			t.Errorf("%q classified as error", line)
		}
	}
}

// And the fix must not stop real failures being errors.
func TestPlainText_StillErrors(t *testing.T) {
	p := NewPlainTextParser()
	for _, line := range []string{
		"Tests: 41 passed, 1 failed",
		"3 failed, 39 passed",
		"FAILED tests/test_api.py::test_login",
		"connect: EACCES",
		"listen EADDRINUSE: address already in use :::3000",
		"Error: ECONNREFUSED 127.0.0.1:5432",
		"build failed",
		"ERROR: something broke",
	} {
		if e := p.Parse("s", line, time.Now(), false); e.Level != LevelError {
			t.Errorf("%q classified as %s, want error", line, e.Level)
		}
	}
}

// stdout and stderr are read by two goroutines into one per-source parser. The
// mutex made that race-free but not interleave-free: a stdout line arriving
// between two stderr traceback lines ended the traceback, and the rest of it was
// parsed as unrelated plain text. Reproduced against a real process before this
// fix.
func TestMultiParser_StdoutDoesNotSplitAStderrTraceback(t *testing.T) {
	mp := NewMultiParser()
	now := time.Now()

	var got []*LogEntry
	got = append(got, mp.ParseStderrLine("py", "", "Traceback (most recent call last):", now)...)
	got = append(got, mp.ParseLineWithType("py", "", "starting up", now)...) // stdout, mid-traceback
	got = append(got, mp.ParseStderrLine("py", "", `  File "app.py", line 3, in <module>`, now)...)
	got = append(got, mp.ParseStderrLine("py", "", "    connect()", now)...)
	got = append(got, mp.ParseStderrLine("py", "", "ConnectionError: refused", now)...)

	var traceback *LogEntry
	for _, e := range got {
		if e.Stacktrace != "" {
			if traceback != nil {
				t.Fatalf("traceback was split into more than one entry")
			}
			traceback = e
		}
	}
	if traceback == nil {
		t.Fatalf("no grouped traceback; got %d separate entries", len(got))
	}
	for _, want := range []string{`File "app.py"`, "connect()", "ConnectionError: refused"} {
		if !strings.Contains(traceback.Stacktrace, want) {
			t.Errorf("traceback missing %q:\n%s", want, traceback.Stacktrace)
		}
	}

	// The stdout line still arrives, on its own.
	var sawStdout bool
	for _, e := range got {
		if e.Message == "starting up" {
			sawStdout = true
		}
	}
	if !sawStdout {
		t.Error("the stdout line was lost")
	}
}

// The level patterns are matched against the lower-cased line rather than each
// carrying (?i), so every case a line might arrive in must still classify.
func TestPlainText_ClassificationIgnoresCase(t *testing.T) {
	p := NewPlainTextParser()
	for line, want := range map[string]LogLevel{
		"Permission Denied":              LevelError,
		"bind: ADDRESS ALREADY IN USE":   LevelError,
		"Unhandled Promise Rejection":    LevelError,
		"FATAL: out of disk":             LevelError,
		"Warning: config key deprecated": LevelWarn,
		"[WARN] slow":                    LevelWarn,
		"Debug: cache primed":            LevelDebug,
	} {
		if e := p.Parse("s", line, time.Now(), false); e.Level != want {
			t.Errorf("%q classified as %s, want %s", line, e.Level, want)
		}
	}
}

// The traceback parser had its own trace-ID pattern, with the faults the
// plain-text one had before #40: no word boundary and any value accepted, so a
// traceback mentioning "backtrace: disabled" got the trace ID "disabled".
func TestPython_TracebackTraceIDNotInventedFromWords(t *testing.T) {
	for _, tc := range []struct{ line, want string }{
		{"    log.info('backtrace: disabled')", ""},
		{"RuntimeError: request failed trace_id=4bf92f3577b34da6", "4bf92f3577b34da6"},
	} {
		p := NewPythonParser()
		now := time.Now()
		p.Parse("s", "Traceback (most recent call last):", now)
		p.Parse("s", `  File "app.py", line 3, in <module>`, now)
		// An exception line closes the traceback itself; anything else is
		// held until the stream ends.
		e, _ := p.Parse("s", tc.line, now)
		if e == nil {
			e = p.Flush("s")
		}
		if e == nil {
			t.Fatalf("%q: no traceback entry", tc.line)
		}
		if e.TraceID != tc.want {
			t.Errorf("%q: trace ID %q, want %q", tc.line, e.TraceID, tc.want)
		}
	}
}

// A JSON line carrying a stack trace was flagged an error without its level
// changing, so /errors returned it while /logs?level=error did not: two
// answers to the same question. GLOSSARY.md: "A stack trace found at a lower
// level promotes the entry to error."
func TestJSON_StackTracePromotesLevel(t *testing.T) {
	p := NewJSONParser()
	for _, line := range []string{
		`{"level":"info","msg":"handled","stacktrace":"at foo()\nat bar()"}`,
		`{"level":"warn","msg":"retrying","stack":"at foo()"}`,
	} {
		e, ok := p.Parse("s", line, time.Now())
		if !ok {
			t.Fatalf("%s: not parsed as JSON", line)
		}
		if e.Level != LevelError {
			t.Errorf("%s: level %s, want error", line, e.Level)
		}
	}
}
