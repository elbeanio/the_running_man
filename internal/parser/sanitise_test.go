package parser

import (
	"strings"
	"testing"
	"time"
)

// The reported symptom: a log line carrying a clear-screen sequence wiped the
// TUI's frame, and Bubble Tea's line-diffing renderer had no reason to repaint
// it. Verified against a live terminal before the fix: of a 20-row frame, three
// rows survived.
func TestSanitiseLine_RemovesTerminalInstructions(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"clear screen", "before\x1b[2Jafter", "beforeafter"},
		{"colour", "\x1b[31mred\x1b[0m and back", "red and back"},
		{"cursor move", "a\x1b[2Ab", "ab"},
		{"bell and backspace", "ding\aback\bspace", "dingbackspace"},
		{"del", "a\x7fb", "ab"},
		{"tab becomes spaces", "tab\there", "tab    here"},
		{"newlines survive", "line1\nline2", "line1\nline2"},
		{"plain text untouched", "nothing to do here", "nothing to do here"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SanitiseLine(tt.in); got != tt.want {
				t.Errorf("SanitiseLine(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// A carriage return means the following text overwrites what came before, which
// is how every spinner and progress bar works. Dropping the control character
// and keeping all of the text produced a run-on line.
func TestSanitiseLine_CarriageReturnOverwrites(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"spinner", "\rbuilding 0\rbuilding 1\rbuild done", "build done"},
		{"progress with clear-line", "progress: 50%\x1b[2K\rprogress: 100%", "progress: 100%"},
		{"trailing cr", "done\r", "done"},
		{"only cr", "\r", ""},
		{"crlf leftovers", "text\r\n", "text\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SanitiseLine(tt.in); got != tt.want {
				t.Errorf("SanitiseLine(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// Sanitising before the level patterns run is why a coloured "ERROR" is
// classified as one. Previously the escape sequences sat between the start of
// the line and the word, and the patterns did not match.
func TestMultiParser_ClassifiesColouredErrors(t *testing.T) {
	mp := NewMultiParser()
	entries := mp.ParseLine("backend", "\x1b[31mERROR\x1b[0m: could not connect", time.Now())

	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	e := entries[0]
	if e.Level != LevelError {
		t.Errorf("level = %q, want %q", e.Level, LevelError)
	}
	if strings.Contains(e.Message, "\x1b") || strings.Contains(e.Raw, "\x1b") {
		t.Errorf("escape sequences survived into the entry: message=%q raw=%q", e.Message, e.Raw)
	}
}
