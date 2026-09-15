package parser

import (
	"testing"
	"time"
)

// Observed during phase 3 verification: a process died with
// "nc: Address already in use" and exit code 1, but the line was classified
// level=info, is_error=false -- so /errors returned ZERO while a process sat
// there failed. An agent asking "what went wrong?" got nothing back.
//
// These are failures whose text contains none of error/err/exception/fatal/
// panic/failed/failure, so the original patterns could not see them.
func TestPlainTextParser_RecognisesFailuresWithoutErrorWords(t *testing.T) {
	failures := []string{
		"nc: Address already in use",
		"bind: address already in use",
		"listen tcp :8080: bind: address already in use",
		"sh: /usr/local/bin/thing: Permission denied",
		"dial tcp 127.0.0.1:5432: connect: connection refused",
		"bash: pytest: command not found",
		"python: can't open file 'app.py': [Errno 2] No such file or directory",
		"cannot bind to port 80",
		"Segmentation fault",
		"fatal: out of memory",
		"Traceback (most recent call last):",
		"UnhandledPromiseRejection: something",
		"Error: listen EADDRINUSE: address already in use :::3000",
		"touch: /thing: Read-only file system",
		"write: no space left on device",
		"accept: too many open files",
	}
	for _, line := range failures {
		entry := NewPlainTextParser().Parse("app", line, time.Now(), false)
		if entry.Level != LevelError {
			t.Errorf("level = %q for %q, want error", entry.Level, line)
		}
		if !entry.IsError {
			t.Errorf("is_error = false for %q; it would be missing from /errors", line)
		}
	}
}

// False positives are worse than misses here: they fill /errors with noise and
// train the reader to ignore it. A web server logging a 404 is not an error.
func TestPlainTextParser_DoesNotOverReport(t *testing.T) {
	benign := []string{
		"GET /missing 404 not found",
		"Listening on http://localhost:3000",
		"Compiled successfully",
		"200 GET /health",
		"Found 3 matching records",
		"user not found in cache, fetching",
		"Starting server",
	}
	for _, line := range benign {
		entry := NewPlainTextParser().Parse("app", line, time.Now(), false)
		if entry.IsError {
			t.Errorf("%q was classified as an error; false positives make /errors useless", line)
		}
	}
}

// stderr raises the floor to warn, not error. Many well-behaved tools write
// ordinary progress to stderr, so treating it as an error would flood /errors.
func TestPlainTextParser_StderrRaisesFloorToWarnOnly(t *testing.T) {
	line := "Resolving dependencies"

	stdout := NewPlainTextParser().Parse("app", line, time.Now(), false)
	if stdout.Level != LevelInfo {
		t.Errorf("stdout level = %q, want info", stdout.Level)
	}

	stderr := NewPlainTextParser().Parse("app", line, time.Now(), true)
	if stderr.Level != LevelWarn {
		t.Errorf("stderr level = %q, want warn", stderr.Level)
	}
	if stderr.IsError {
		t.Error("stderr alone must not mark an entry as an error; npm, pip, webpack and git all use it for progress")
	}
}

// An explicit level in the text still wins over the stderr floor.
func TestPlainTextParser_StderrDoesNotDowngrade(t *testing.T) {
	entry := NewPlainTextParser().Parse("app", "ERROR something broke", time.Now(), true)
	if entry.Level != LevelError || !entry.IsError {
		t.Errorf("an error on stderr must stay an error, got level=%q is_error=%v", entry.Level, entry.IsError)
	}

	debug := NewPlainTextParser().Parse("app", "DEBUG fine", time.Now(), false)
	if debug.Level != LevelDebug {
		t.Errorf("debug level = %q, want debug", debug.Level)
	}
}
