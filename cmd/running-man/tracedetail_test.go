package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// A trace shaped like a real one: a root and fifteen children, each with a
// start and duration, plus correlated logs.
func bigTrace(failing bool) ([]spanDetail, []logEntry) {
	t0 := time.Date(2026, 10, 8, 19, 42, 12, 0, time.UTC)
	spans := []spanDetail{{SpanID: "root", Name: "chat.turn", ServiceName: "duet",
		StartTime: t0, Duration: 8 * time.Second, Status: "unset"}}
	for i := range 15 {
		s := spanDetail{SpanID: fmt.Sprintf("c%02d", i), ParentSpanID: "root",
			Name: fmt.Sprintf("data.get_relation_data %02d", i), ServiceName: "duet",
			StartTime: t0.Add(time.Duration(i) * 100 * time.Millisecond), Duration: 30 * time.Millisecond, Status: "ok"}
		if failing && i == 3 {
			s.Status = "error"
		}
		spans = append(spans, s)
	}
	var logs []logEntry
	for i := range 10 {
		logs = append(logs, logEntry{Timestamp: "2026-10-08T19:42:12Z", Level: "info", Message: fmt.Sprintf("log line %02d", i)})
	}
	return spans, logs
}

// The receiver records status in lower case -- "error", "ok", "unset" -- and
// the detail view compared against "ERROR", so it could not show a failure:
// every span had a tick, and every trace said OK.
func TestTraceDetail_ShowsErrors(t *testing.T) {
	spans, _ := bigTrace(true)
	out := ansi.Strip(renderTraceDetail("abc", spans, nil, 40, 100, 0))
	if !strings.Contains(out, "Status: ERROR") {
		t.Errorf("header does not report the error:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "data.get_relation_data 03") && !strings.Contains(line, "✗") {
			t.Errorf("failing span not marked: %q", line)
		}
		if strings.Contains(line, "data.get_relation_data 04") && !strings.Contains(line, "✓") {
			t.Errorf("passing span not marked ok: %q", line)
		}
	}
}

// A big trace overflowed its window and could not be scrolled. The span tree
// was added to the line list as one multi-line string, so a fifteen-span tree
// counted as a single line: the view thought everything fitted, skipped
// scrolling, and drew more rows than it had.
func TestTraceDetail_FitsItsWindowAndScrolls(t *testing.T) {
	spans, logs := bigTrace(false)
	const height = 20

	top := renderTraceDetail("abc", spans, logs, height, 100, 0)
	if n := strings.Count(top, "\n") + 1; n != height {
		t.Fatalf("rendered %d rows into a %d-row window", n, height)
	}
	if !strings.Contains(top, "chat.turn") {
		t.Error("top of the trace not shown at offset 0")
	}

	bottom := ansi.Strip(renderTraceDetail("abc", spans, logs, height, 100, 1<<30))
	if n := strings.Count(bottom, "\n") + 1; n != height {
		t.Fatalf("rendered %d rows at the end", n)
	}
	if !strings.Contains(bottom, "log line 09") {
		t.Errorf("scrolling to the end does not reach the last log:\n%s", bottom)
	}
	if strings.Contains(bottom, "chat.turn") {
		t.Error("scrolling did not move the view")
	}
}

// End set the offset to math.MaxInt and the view clamped it only when drawing,
// so Up after End decremented from MaxInt and appeared to do nothing, for
// practical purposes forever. The keys now stop at the real end.
func TestTraceDetail_ScrollKeysStopAtTheEnd(t *testing.T) {
	m := fixtureModel(100, 20)
	m.mode = ModeTraceDetail
	m.selectedTraceID = "abc"
	m.traceSpans, m.traceLogs = bigTrace(false)

	pressType := func(k tea.KeyType) {
		next, _ := m.updateNormalMode(tea.KeyMsg{Type: k})
		m = next.(model)
	}

	pressType(tea.KeyEnd)
	end := m.traceDetailScrollOffset
	if end != m.traceDetailMaxScroll() || end <= 0 {
		t.Fatalf("End set offset %d, want the real end %d", end, m.traceDetailMaxScroll())
	}
	pressType(tea.KeyDown)
	pressType(tea.KeyPgDown)
	if m.traceDetailScrollOffset != end {
		t.Errorf("scrolled past the end: %d, end %d", m.traceDetailScrollOffset, end)
	}
	pressType(tea.KeyUp)
	if m.traceDetailScrollOffset != end-1 {
		t.Errorf("Up after End: offset %d, want %d", m.traceDetailScrollOffset, end-1)
	}
}
