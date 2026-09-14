package parser

import (
	"fmt"
	"testing"
	"time"
)

// Review finding R10: the JSON parser stored a span_id in TraceID when no
// trace_id was present, with a comment conceding that deriving the real trace
// id was unimplemented. Span ids then became keys in the ring buffer's trace
// index, so /traces/{id}/logs silently returned wrong or empty results. A
// missing trace id is better than a wrong one.
func TestJSONParser_SpanIDIsNotUsedAsTraceID(t *testing.T) {
	p := NewJSONParser()
	ts := time.Now()

	entry, ok := p.Parse("app", `{"level":"info","message":"hi","span_id":"abc123span"}`, ts)
	if !ok {
		t.Fatal("expected the line to parse as JSON")
	}
	if entry.TraceID != "" {
		t.Errorf("TraceID = %q; a span_id must not be stored as a trace id", entry.TraceID)
	}
}

// A real trace_id must still be picked up, including the common aliases.
func TestJSONParser_TraceIDAliases(t *testing.T) {
	p := NewJSONParser()
	ts := time.Now()

	for _, field := range []string{"trace_id", "traceId", "traceID", "trace"} {
		line := fmt.Sprintf(`{"level":"info","message":"hi","%s":"deadbeef"}`, field)
		entry, ok := p.Parse("app", line, ts)
		if !ok {
			t.Fatalf("%s: expected JSON parse", field)
		}
		if entry.TraceID != "deadbeef" {
			t.Errorf("%s: TraceID = %q, want \"deadbeef\"", field, entry.TraceID)
		}
	}
}
