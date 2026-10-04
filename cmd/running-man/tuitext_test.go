package main

import (
	"strings"
	"testing"
)

// Nine render sites hand-rolled byte slicing against a terminal width, and a
// narrow window made the bound negative. renderLogs crashed at any width of 19
// or less whenever a log line carried a trace ID, which is the default.
func TestTruncate_NeverPanicsAtAnyWidth(t *testing.T) {
	inputs := []string{
		"",
		"short",
		"a line long enough that it has to be cut somewhere sensible",
		"emoji 🚀🚀🚀 and CJK 日本語テキスト mixed with ascii",
		"\x1b[31mcoloured\x1b[0m text that needs truncating as well",
	}
	for _, in := range inputs {
		for w := -30; w <= 80; w++ {
			got := truncate(in, w)
			if w <= 0 && got != "" {
				t.Errorf("truncate(%q, %d) = %q, want empty", in, w, got)
			}
			if dw := displayWidth(got); dw > w && w > 0 {
				t.Errorf("truncate(%q, %d) is %d columns wide", in, w, dw)
			}
		}
	}
}

// len() is the byte count, not the column count. Comparing it against a
// terminal width is what put the cut in the wrong place for any line that was
// not plain ASCII.
func TestDisplayWidth_IsColumnsNotBytes(t *testing.T) {
	s := "\x1b[31mred\x1b[0m 🚀 日本"
	if w, b := displayWidth(s), len(s); w == b {
		t.Errorf("displayWidth and len agree (%d) on a string where they must not", w)
	}
	if got := displayWidth("abc"); got != 3 {
		t.Errorf("displayWidth(\"abc\") = %d, want 3", got)
	}
	if got := displayWidth("\x1b[31mabc\x1b[0m"); got != 3 {
		t.Errorf("escape sequences counted as width: got %d, want 3", got)
	}
}

// Timestamps arrive as strings over the API and an OTLP sender picks its own, so
// "" and "oops" are both reachable. buildMatchLineIndex sliced [11:19] with no
// length check, so one short timestamp in the buffer made search crash.
func TestClockTime_ToleratesAnyInput(t *testing.T) {
	tests := []struct{ in, want string }{
		{"2026-10-04T18:30:45Z", "18:30:45"},
		{"2026-10-04T18:30:45.123456+01:00", "18:30:45"},
		{"", ""},
		{"oops", "oops"},
		// Too short to hold a clock: returned unchanged rather than sliced.
		{"2026-10-04T", "2026-10-04T"},
		{"2026-10-04T18:30", "2026-10-04T18:30"},
	}
	for _, tt := range tests {
		if got := clockTime(tt.in); got != tt.want {
			t.Errorf("clockTime(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// The whole render path, at every width, with the content that used to crash it.
func TestRenderPathSurvivesEveryWidth(t *testing.T) {
	logs := []logEntry{
		{Timestamp: "2026-10-04T18:30:45Z", Level: "info", Source: "a",
			Message: "a reasonably long message that will need truncating", TraceID: "abcdef1234567890"},
		{Timestamp: "", Level: "error", Source: "b", Message: "no timestamp at all", IsError: true},
		{Timestamp: "oops", Level: "info", Source: "c", Message: "emoji 🚀 and 日本語"},
	}
	traces := []traceSummary{{TraceID: "abcdef1234567890", Status: "error", Services: []string{"api", "db"}}}
	spans := []spanDetail{{SpanID: "s1", Name: "GET /things", ServiceName: "api"}}

	for w := 1; w <= 60; w++ {
		for _, showTraceIDs := range []bool{true, false} {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("renderLogs(width=%d, traceIDs=%v) panicked: %v", w, showTraceIDs, r)
					}
				}()
				renderLogs(logs, 10, w, 0, "", -1, showTraceIDs)
				renderLogs(logs, 10, w, 0, "message", 0, showTraceIDs)
			}()
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("trace views (width=%d) panicked: %v", w, r)
				}
			}()
			renderTraceList(traces, 10, w, 0, 0)
			renderTraceDetail("abcdef1234567890", spans, logs, 10, w, 0)
		}()
	}

	// Search indexing must tolerate the same entries at any width, since it no
	// longer takes one.
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("buildMatchLineIndex panicked: %v", r)
			}
		}()
		if got := buildMatchLineIndex(logs, "message"); len(got) == 0 {
			t.Error("expected at least one match")
		}
		buildMatchLineIndex(logs, strings.Repeat("x", 200))
	}()
}
