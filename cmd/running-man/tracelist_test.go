package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// Traces come newest first. A new one arriving moved every index down, and the
// highlight silently landed on a different trace; it follows the trace now.
func TestTraceList_SelectionFollowsTheTrace(t *testing.T) {
	m := fixtureModel(100, 20)
	m.traces = fixtureTraces()
	m.selectedTraceIdx = 1 // GET /things
	newest := traceSummary{TraceID: "ffff", RootSpan: "chat.turn", StartTime: m.traces[0].StartTime.Add(time.Minute)}

	next, _ := m.Update(tracesMsg{traces: append([]traceSummary{newest}, fixtureTraces()...)})
	m = next.(model)
	if got, _ := m.selectedTrace(); got.RootSpan != "GET /things" {
		t.Errorf("selection moved to %q", got.RootSpan)
	}
}

// listLines renders the trace list and strips it to plain text, one line each.
func listLines(traces []traceSummary, restarts []time.Time, height, selected, offset int) []string {
	return strings.Split(ansi.Strip(renderTraceList(traces, restarts, height, 100, offset, selected)), "\n")
}

func at(s string) time.Time { t, _ := time.Parse(time.RFC3339, s); return t }

func indexOf(lines []string, substr string) int {
	for i, l := range lines {
		if strings.Contains(l, substr) {
			return i
		}
	}
	return -1
}

// Traces now survive a restart; a line where it happened shows which are from
// before it, whose logs are gone.
func TestTraceList_DrawsALineAtEachRestart(t *testing.T) {
	traces := fixtureTraces() // 09:16:10, then 09:15:03
	lines := listLines(traces, []time.Time{at("2026-10-04T09:15:30Z")}, 10, 0, 0)

	newer, divider, older := indexOf(lines, "chat.turn"), indexOf(lines, "Running Man restarted 09:15"), indexOf(lines, "GET /things")
	if !(newer >= 0 && newer < divider && divider < older) {
		t.Errorf("want the line between the two traces:\n%s", strings.Join(lines, "\n"))
	}
}

// A restart newer than every trace is drawn above them all: none of the traces
// listed is from this run.
func TestTraceList_RestartAfterEveryTrace(t *testing.T) {
	lines := listLines(fixtureTraces(), []time.Time{at("2026-10-04T10:00:00Z")}, 10, 0, 0)
	if d, first := indexOf(lines, "Running Man restarted 10:00"), indexOf(lines, "chat.turn"); d < 0 || d > first {
		t.Errorf("want the line above the first trace:\n%s", strings.Join(lines, "\n"))
	}
}

// Divider lines take rows, so the selected trace must still be kept in view.
func TestTraceList_SelectionStaysVisibleBelowRestarts(t *testing.T) {
	var traces []traceSummary
	var restarts []time.Time
	base := at("2026-10-04T12:00:00Z")
	for i := range 10 {
		start := base.Add(-time.Duration(i) * time.Minute)
		traces = append(traces, traceSummary{TraceID: fmt.Sprint(i), RootSpan: fmt.Sprintf("trace-%d", i), StartTime: start})
		restarts = append(restarts, start.Add(-30*time.Second)) // a restart below every trace
	}
	// The key handlers set the offset counting traces, not lines: offset 2 keeps
	// trace 5 in view without restart lines, and not with them.
	lines := listLines(traces, restarts, 6, 5, 2)
	if indexOf(lines, "trace-5") < 0 {
		t.Errorf("selected trace-5 is off screen:\n%s", strings.Join(lines, "\n"))
	}
}

// With a day of traces, 10:00 yesterday and 10:00 today look identical; the
// date is shown once the list spans more than one day.
func TestTraceList_ShowsTheDateAcrossDays(t *testing.T) {
	traces := fixtureTraces()
	if lines := listLines(traces, nil, 10, 0, 0); indexOf(lines, "Oct") >= 0 {
		t.Errorf("date shown for traces all from one day:\n%s", strings.Join(lines, "\n"))
	}
	traces[1].StartTime = traces[1].StartTime.Add(-24 * time.Hour)
	lines := listLines(traces, nil, 10, 0, 0)
	if indexOf(lines, "Oct 04 09:16:10") < 0 || indexOf(lines, "Oct 03 09:15:03") < 0 {
		t.Errorf("want dates across days:\n%s", strings.Join(lines, "\n"))
	}
}

// The API says when a trace's logs were lost to a restart; the detail view
// shows that rather than "No correlated logs", which reads as "it logged
// nothing".
func TestTraceDetail_ShowsLogsLostToARestart(t *testing.T) {
	note := "This trace is from before Running Man restarted."
	out := ansi.Strip(renderTraceDetail("abc", fixtureSpans(), nil, note, 40, 100, 0, -1))
	if !strings.Contains(out, note) || strings.Contains(out, "No correlated logs") {
		t.Errorf("want the note in place of \"No correlated logs\":\n%s", out)
	}
}
