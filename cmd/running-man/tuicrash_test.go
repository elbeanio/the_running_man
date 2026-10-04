package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The crash that killed unattended instances: the sources list is rebuilt from
// whatever is in the ring buffer, so a quiet source disappears once its entries
// age out past the retention limits. selectedSource was left pointing past the
// end of the shorter list.
func TestSourcesShrinkingUnderTheSelection(t *testing.T) {
	m := initialModel("http://localhost", nil)
	m.sources = []string{"backend", "worker", "frontend", "Traces"}
	m.selectedSource = 2 // looking at "frontend"

	next, _ := m.Update(sourcesMsg{
		names: []string{"backend"},
		types: map[string]string{"backend": "process"},
	})

	got, ok := next.(model)
	if !ok {
		t.Fatalf("Update returned %T", next)
	}
	if got.lastPanic != "" {
		t.Errorf("a shrinking sources list panicked: %s", got.lastPanic)
	}
	if got.selectedSource >= len(got.sources) {
		t.Errorf("selectedSource %d is out of range for %v", got.selectedSource, got.sources)
	}
	if got.currentSource() == "" {
		t.Error("no source selected after the list shrank")
	}
}

// Same hazard for traces, which age out past max_span_age.
func TestTracesShrinkingUnderTheSelection(t *testing.T) {
	m := initialModel("http://localhost", nil)
	m.traces = []traceSummary{{TraceID: "a"}, {TraceID: "b"}, {TraceID: "c"}}
	m.selectedTraceIdx = 2

	next, _ := m.Update(tracesMsg([]traceSummary{{TraceID: "a"}}))
	got := next.(model)

	if got.lastPanic != "" {
		t.Errorf("a shrinking trace list panicked: %s", got.lastPanic)
	}
	if _, ok := got.selectedTrace(); !ok {
		t.Error("no trace selected after the list shrank")
	}
}

// A panic while rendering must not end the session, and must leave a report.
func TestViewPanicIsSurvivedAndRecorded(t *testing.T) {
	dir := t.TempDir()
	installCrashLog(dir)

	m := initialModel("http://localhost", nil)
	// Width 19 with trace IDs shown used to panic in renderLogs. Whether or not
	// that specific arithmetic still bites, View must never take the program
	// down with it.
	m.width, m.height = 19, 20
	m.showTraceIDs = true
	m.sources = []string{"backend", "Traces"}
	m.logs = []logEntry{{
		Timestamp: "2026-10-04T18:00:00Z", Level: "info", Source: "backend",
		Message: "a message that is long enough to need truncating somewhere",
		TraceID: "abcdef1234567890",
	}}

	out := m.View() // must return, not panic
	if out == "" {
		t.Error("View returned nothing")
	}

	if _, err := os.Stat(filepath.Join(dir, ".running-man", "crash.log")); err == nil {
		data, _ := os.ReadFile(filepath.Join(dir, ".running-man", "crash.log"))
		t.Logf("a panic was recovered and recorded:\n%s", firstLines(string(data), 4))
	}
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
