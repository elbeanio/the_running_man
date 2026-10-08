package main

import (
	"testing"
	"time"
)

// Traces come newest first. A new one arriving moved every index down, and the
// highlight silently landed on a different trace; it follows the trace now.
func TestTraceList_SelectionFollowsTheTrace(t *testing.T) {
	m := fixtureModel(100, 20)
	m.traces = fixtureTraces()
	m.selectedTraceIdx = 1 // GET /things
	newest := traceSummary{TraceID: "ffff", RootSpan: "chat.turn", StartTime: m.traces[0].StartTime.Add(time.Minute)}

	next, _ := m.Update(tracesMsg(append([]traceSummary{newest}, fixtureTraces()...)))
	m = next.(model)
	if got, _ := m.selectedTrace(); got.RootSpan != "GET /things" {
		t.Errorf("selection moved to %q", got.RootSpan)
	}
}
