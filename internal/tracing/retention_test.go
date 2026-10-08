package tracing

import (
	"testing"
	"time"
)

// Span eviction stopped at the first span whose StartTime was not old, the same
// shape as the ring buffer's bug. StartTime comes from the sender, so one span
// from a client with a fast clock froze span retention, and spans are out of
// order legitimately anyway: a long span starts well before it is exported.
func TestSpanRetentionIgnoresReportedStartTimes(t *testing.T) {
	s := NewSpanStorage(100000, 50*time.Millisecond)

	s.Add(&SpanEntry{TraceID: "future", StartTime: time.Now().Add(time.Hour)})
	for i := 0; i < 500; i++ {
		s.Add(&SpanEntry{TraceID: "t", StartTime: time.Now()})
	}

	time.Sleep(100 * time.Millisecond)
	s.Add(&SpanEntry{TraceID: "latest", StartTime: time.Now()})

	if got := len(allSpans(s)); got > 1 {
		t.Errorf("storage holds %d spans after retention elapsed; want only the latest", got)
	}
}

// A long-running span's StartTime is its real start, which can be minutes before
// it is received. It must not be discarded on arrival for being "old".
func TestSpanRetentionKeepsRecentlyReceivedSpans(t *testing.T) {
	s := NewSpanStorage(100000, time.Minute)
	s.Add(&SpanEntry{TraceID: "long", StartTime: time.Now().Add(-10 * time.Minute)})
	s.Add(&SpanEntry{TraceID: "new", StartTime: time.Now()})

	if got := len(allSpans(s)); got != 2 {
		t.Errorf("storage holds %d spans; a long span was discarded on arrival", got)
	}
}
