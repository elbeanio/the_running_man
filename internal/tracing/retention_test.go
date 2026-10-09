package tracing

import (
	"strings"
	"testing"
	"time"
)

// Span eviction stopped at the first span whose StartTime was not old, the same
// shape as the ring buffer's bug. StartTime comes from the sender, so one span
// from a client with a fast clock froze span retention, and spans are out of
// order legitimately anyway: a long span starts well before it is exported.
func TestSpanRetentionIgnoresReportedStartTimes(t *testing.T) {
	s := NewSpanStorage(100000, 50*time.Millisecond, 1<<30)

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
	s := NewSpanStorage(100000, time.Minute, 1<<30)
	s.Add(&SpanEntry{TraceID: "long", StartTime: time.Now().Add(-10 * time.Minute)})
	s.Add(&SpanEntry{TraceID: "new", StartTime: time.Now()})

	if got := len(allSpans(s)); got != 2 {
		t.Errorf("storage holds %d spans; a long span was discarded on arrival", got)
	}
}

// bigSpan is a span whose size is dominated by one attribute of n bytes.
func bigSpan(traceID string, n int) *SpanEntry {
	return &SpanEntry{TraceID: traceID, Attributes: map[string]string{"v": strings.Repeat("x", n)}}
}

// Spans were bounded only by count and age, and one LLM span runs to 400 KB,
// so 10,000 of them could hold gigabytes. The byte limit evicts the oldest.
func TestSpanRetentionEvictsOldestPastMaxBytes(t *testing.T) {
	s := NewSpanStorage(1000, time.Hour, 10_000)
	for _, id := range []string{"a", "b", "c", "d"} {
		s.Add(bigSpan(id, 3_000))
	}

	var kept []string
	for _, sp := range allSpans(s) {
		kept = append(kept, sp.TraceID)
	}
	if strings.Join(kept, "") != "bcd" {
		t.Errorf("kept %v; want the newest three, which fit in 10,000 bytes", kept)
	}
	if st := s.Stats(); st.TotalBytes > 10_000 {
		t.Errorf("holding %d bytes, over the 10,000 limit", st.TotalBytes)
	}
}

// A span bigger than the whole limit is still kept, alone: dropping it on
// arrival would lose exactly the span most likely to matter, the largest LLM
// call. The ring buffer treats an oversized log line the same way.
func TestSpanRetentionKeepsASpanLargerThanMaxBytes(t *testing.T) {
	s := NewSpanStorage(1000, time.Hour, 1_000)
	s.Add(bigSpan("small", 10))
	s.Add(bigSpan("huge", 5_000))

	spans := allSpans(s)
	if len(spans) != 1 || spans[0].TraceID != "huge" {
		t.Errorf("kept %d spans; want only the oversized one", len(spans))
	}
}

// The byte count has to follow every eviction, not just byte eviction, or it
// drifts upwards until the byte limit evicts spans that would fit.
func TestSpanRetentionCountsBytesAcrossEvictions(t *testing.T) {
	s := NewSpanStorage(2, 30*time.Millisecond, 1<<30)
	for _, id := range []string{"a", "b", "c"} { // count evicts "a"
		s.Add(bigSpan(id, 100))
	}
	if got, want := s.Stats().TotalBytes, 2*bigSpan("x", 100).size(); got != want {
		t.Errorf("after count eviction: %d bytes, want %d", got, want)
	}

	time.Sleep(50 * time.Millisecond)
	s.Add(bigSpan("d", 100)) // age evicts "b" and "c"
	if got, want := s.Stats().TotalBytes, bigSpan("x", 100).size(); got != want {
		t.Errorf("after age eviction: %d bytes, want %d", got, want)
	}
}

func TestSpanSizeCountsEveryString(t *testing.T) {
	sp := &SpanEntry{
		TraceID: "t1", SpanID: "s1", ParentSpanID: "p1", Name: "name", Kind: "kind",
		Status: "ok", StatusCode: "code", ServiceName: "svc",
		Attributes: map[string]string{"key": "value"},
		Events:     []SpanEvent{{Name: "ev", Attributes: map[string]string{"ek": "evalue"}}},
		Links:      []SpanLink{{TraceID: "lt", SpanID: "ls", Attributes: map[string]string{"lk": "lv"}}},
	}
	// 2+2+2+4+4+2+4+3, 3+5, 2+2+6, 2+2+2+2
	if got := sp.size(); got != 23+8+10+8 {
		t.Errorf("size = %d, want 49", got)
	}
}
