package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// apiTraces is what /traces returns: fixtureTraces, newest first, with extra
// traces newer still.
func apiTraces(newer ...traceSummary) tracesMsg {
	return tracesMsg{traces: append(newer, fixtureTraces()...)}
}

func newerTrace(id string, minutes int) traceSummary {
	return traceSummary{TraceID: id, RootSpan: id, StartTime: fixtureTraces()[0].StartTime.Add(time.Duration(minutes) * time.Minute)}
}

func update(m model, msg tracesMsg) model {
	next, _ := m.Update(msg)
	return next.(model)
}

// Every other tab reads oldest to newest, down the screen; the trace list ran
// the other way, because the API does. It now matches the tabs, and opens on
// the newest trace, at the bottom, as a log tab opens on its latest line.
func TestTraceList_OldestFirstOpeningOnTheNewest(t *testing.T) {
	m := update(initialModel("http://localhost", nil), apiTraces())

	if m.traces[0].RootSpan != "GET /things" || m.traces[1].RootSpan != "chat.turn" {
		t.Errorf("order %q, %q; want oldest first", m.traces[0].RootSpan, m.traces[1].RootSpan)
	}
	if got, _ := m.selectedTrace(); got.RootSpan != "chat.turn" {
		t.Errorf("opened on %q, want the newest", got.RootSpan)
	}
}

// On the newest trace, the selection follows new ones down, as a log tab
// follows its tail.
func TestTraceList_FollowsNewTracesFromTheNewest(t *testing.T) {
	m := update(initialModel("http://localhost", nil), apiTraces())
	m = update(m, apiTraces(newerTrace("later", 1)))
	if got, _ := m.selectedTrace(); got.TraceID != "later" {
		t.Errorf("selection on %q, want the new newest trace", got.TraceID)
	}
}

// Moved off the newest, the selection stays on its trace, and the window stays
// where it was, as new traces arrive. A refresh used to put the window back at
// the top.
func TestTraceList_StaysPutOffTheNewest(t *testing.T) {
	m := update(initialModel("http://localhost", nil), apiTraces())
	m.selectedTraceIdx, m.traceScrollOffset = 0, 0 // GET /things, the oldest
	m = update(m, apiTraces(newerTrace("later", 1)))
	if got, _ := m.selectedTrace(); got.RootSpan != "GET /things" {
		t.Errorf("selection moved to %q", got.RootSpan)
	}

	m.traceScrollOffset = 1
	m.selectedTraceIdx = 1
	m = update(m, apiTraces(newerTrace("later", 1), newerTrace("later still", 2)))
	if m.traceScrollOffset != 1 {
		t.Errorf("scroll offset %d after a refresh, want it kept at 1", m.traceScrollOffset)
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

// oldestFirst is fixtureTraces as the TUI holds them.
func oldestFirst() []traceSummary {
	traces := fixtureTraces()
	slices.Reverse(traces)
	return traces
}

// Traces now survive a restart; a line where it happened shows which are from
// before it, whose logs are gone.
func TestTraceList_DrawsALineAtEachRestart(t *testing.T) {
	traces := oldestFirst() // 09:15:03, then 09:16:10
	lines := listLines(traces, []time.Time{at("2026-10-04T09:15:30Z")}, 10, 0, 0)

	older, divider, newer := indexOf(lines, "GET /things"), indexOf(lines, "Running Man restarted 09:15:30"), indexOf(lines, "chat.turn")
	if !(older >= 0 && older < divider && divider < newer) {
		t.Errorf("want the line between the two traces:\n%s", strings.Join(lines, "\n"))
	}
}

// A restart newer than every trace is drawn below them all: none of the traces
// listed is from this run.
func TestTraceList_RestartAfterEveryTrace(t *testing.T) {
	lines := listLines(oldestFirst(), []time.Time{at("2026-10-04T10:00:00Z")}, 10, 1, 0)
	if d, last := indexOf(lines, "Running Man restarted 10:00:00"), indexOf(lines, "chat.turn"); d < last {
		t.Errorf("want the line below the last trace:\n%s", strings.Join(lines, "\n"))
	}
}

// Divider lines take rows, so the selected trace must still be kept in view.
func TestTraceList_SelectionStaysVisibleBelowRestarts(t *testing.T) {
	var traces []traceSummary
	var restarts []time.Time
	base := at("2026-10-04T12:00:00Z")
	for i := range 10 {
		start := base.Add(time.Duration(i) * time.Minute)
		traces = append(traces, traceSummary{TraceID: fmt.Sprint(i), RootSpan: fmt.Sprintf("trace-%d", i), StartTime: start})
		restarts = append(restarts, start.Add(-30*time.Second)) // a restart above every trace
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
	traces := oldestFirst()
	if lines := listLines(traces, nil, 10, 0, 0); indexOf(lines, "Oct") >= 0 {
		t.Errorf("date shown for traces all from one day:\n%s", strings.Join(lines, "\n"))
	}
	traces[0].StartTime = traces[0].StartTime.Add(-24 * time.Hour)
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

// The list opens on the newest trace, at the bottom, so with more traces than
// fit it opens scrolled. The column header scrolled with the rows, and was
// gone whenever the list was opened.
func TestTraceList_HeaderStaysWhenScrolled(t *testing.T) {
	var traces []traceSummary
	for i := range 20 {
		traces = append(traces, traceSummary{TraceID: fmt.Sprint(i), RootSpan: fmt.Sprintf("trace-%d", i),
			StartTime: at("2026-10-04T12:00:00Z").Add(time.Duration(i) * time.Minute)})
	}
	lines := listLines(traces, nil, 6, 19, 14)
	if !strings.Contains(lines[0], "Root span") || indexOf(lines, "trace-19") != 5 {
		t.Errorf("want the header on top and the newest at the bottom:\n%s", strings.Join(lines, "\n"))
	}
}
