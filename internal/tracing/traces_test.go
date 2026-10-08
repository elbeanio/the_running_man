package tracing

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

var t0 = time.Now().Add(-time.Minute).Truncate(time.Millisecond)

func span(trace, id, parent, name string, startMs, durMs int, status string, attrs map[string]string) *SpanEntry {
	start := t0.Add(time.Duration(startMs) * time.Millisecond)
	d := time.Duration(durMs) * time.Millisecond
	return &SpanEntry{TraceID: trace, SpanID: id, ParentSpanID: parent, Name: name,
		StartTime: start, EndTime: start.Add(d), Duration: d, Status: status,
		ServiceName: "duet", Attributes: attrs}
}

// A chat turn shaped like eureka's: a root carrying the user's message, an
// LLM call that outlasts its parent's own end, and a failing DB call.
func chatTurn() []*SpanEntry {
	return []*SpanEntry{
		span("t1", "root", "", "chat.turn", 0, 8000, "unset", map[string]string{"input.value": "Show me the revenue by region\nfor last quarter"}),
		span("t1", "llm", "root", "ChatCompletion", 100, 8500, "ok", nil),
		span("t1", "db", "root", "data.get_relation_data", 50, 30, "error", map[string]string{"db.statement": "SELECT 1"}),
	}
}

func TestSummarise(t *testing.T) {
	s := Summarise(chatTurn())
	if s.TraceID != "t1" || s.RootSpan != "chat.turn" || s.SpanCount != 3 || s.ErrorCount != 1 || s.Status != "error" {
		t.Errorf("summary = %+v", s)
	}
	// The user's message, first line only.
	if s.Summary != "Show me the revenue by region" {
		t.Errorf("summary text = %q", s.Summary)
	}
	// Earliest start to latest end -- the LLM call ends after the root.
	if s.Duration != "8.6s" || !s.StartTime.Equal(t0) {
		t.Errorf("duration %s from %v, want 8.6s from %v", s.Duration, s.StartTime, t0)
	}
	if len(s.Services) != 1 || s.Services[0] != "duet" {
		t.Errorf("services = %v", s.Services)
	}
}

// The summary rule, in order: input.value, http.route, http.target,
// db.statement on the root; first line; at most 120 characters.
func TestSummarise_SummaryRule(t *testing.T) {
	for _, tc := range []struct {
		attrs map[string]string
		want  string
	}{
		{map[string]string{"http.route": "/api/users/{id}", "http.target": "/api/users/7"}, "/api/users/{id}"},
		{map[string]string{"http.target": "/api/users/7"}, "/api/users/7"},
		{map[string]string{"db.statement": "SELECT * FROM t\nWHERE x"}, "SELECT * FROM t"},
		{map[string]string{"other": "x"}, ""},
		{map[string]string{"input.value": strings.Repeat("a", 200)}, strings.Repeat("a", 119) + "…"},
	} {
		got := Summarise([]*SpanEntry{span("t", "r", "", "op", 0, 1, "ok", tc.attrs)}).Summary
		if got != tc.want {
			t.Errorf("%v: summary %q, want %q", tc.attrs, got, tc.want)
		}
	}
}

// Spans can arrive without their parent -- an exporter that dropped it, or a
// trace still arriving. The root is a span whose parent is not in the trace,
// the earliest if there are several.
func TestSummarise_RootWithMissingParent(t *testing.T) {
	s := Summarise([]*SpanEntry{
		span("t", "b", "gone", "later", 500, 10, "ok", nil),
		span("t", "a", "gone", "earlier", 100, 10, "ok", nil),
		span("t", "c", "a", "child", 150, 10, "ok", nil),
	})
	if s.RootSpan != "earlier" {
		t.Errorf("root = %q, want the earliest span whose parent is missing", s.RootSpan)
	}
}

func TestTraces_FiltersAndOrder(t *testing.T) {
	st := NewSpanStorage(100, time.Hour)
	for _, sp := range chatTurn() {
		st.Add(sp)
	}
	st.Add(span("t2", "x", "", "data.get_metadata", 20000, 5, "ok", nil))
	st.Add(span("t3", "y", "", "proxy.chat.completions", 10000, 5, "unset", nil))

	all := st.Traces(TraceFilters{})
	if len(all) != 3 || all[0].TraceID != "t2" || all[2].TraceID != "t1" {
		t.Fatalf("order = %v, want newest first", ids(all))
	}
	if got := st.Traces(TraceFilters{Limit: 1}); len(got) != 1 || got[0].TraceID != "t2" {
		t.Errorf("limit 1 = %v, want the newest trace", ids(got))
	}
	if got := st.Traces(TraceFilters{Status: "error"}); len(got) != 1 || got[0].TraceID != "t1" {
		t.Errorf("status=error = %v, want t1", ids(got))
	}
	// span_name matches any span in the trace, and the whole trace is summarised.
	got := st.Traces(TraceFilters{SpanName: "ChatCompletion"})
	if len(got) != 1 || got[0].TraceID != "t1" || got[0].SpanCount != 3 {
		t.Errorf("span_name = %+v, want all of t1", got)
	}
	if got := st.Traces(TraceFilters{ServiceName: "nope"}); len(got) != 0 {
		t.Errorf("unknown service matched %v", ids(got))
	}
}

func ids(ss []TraceSummary) []string {
	var out []string
	for _, s := range ss {
		out = append(out, s.TraceID)
	}
	return out
}

// Values are cut at 1 KB on a character boundary, with the original size
// recorded, and the stored span is not touched.
func TestTrim(t *testing.T) {
	big := strings.Repeat("é", 1000) // 2000 bytes
	sp := span("t", "s", "", "op", 0, 1, "ok", map[string]string{"input.value": big, "small": "x"})
	sp.Events = []SpanEvent{{Name: "exception", Attributes: map[string]string{"exception.stacktrace": big}}}

	tr := Trim(sp, 1024)
	v := tr.Span.Attributes["input.value"]
	if len(v) > 1024+len("…") || !strings.HasSuffix(v, "…") || !json.Valid([]byte(`"`+v+`"`)) {
		t.Errorf("cut value is %d bytes, suffix %q", len(v), v[len(v)-5:])
	}
	if strings.ContainsRune(strings.TrimSuffix(v, "…"), '�') {
		t.Error("cut inside a character")
	}
	if tr.Truncated["input.value"] != 2000 || tr.Truncated["events.0.exception.stacktrace"] != 2000 {
		t.Errorf("truncated = %v", tr.Truncated)
	}
	if _, ok := tr.Truncated["small"]; ok {
		t.Error("a small value was reported as truncated")
	}
	if sp.Attributes["input.value"] != big || sp.Events[0].Attributes["exception.stacktrace"] != big {
		t.Error("trimming changed the stored span")
	}

	b, err := json.Marshal(tr)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if m["span_id"] != "s" || m["duration"] != "1ms" || m["truncated"] == nil {
		t.Errorf("JSON = %s", b)
	}
	if b, _ := json.Marshal(Trim(span("t", "s", "", "op", 0, 1, "ok", nil), 1024)); strings.Contains(string(b), "truncated") {
		t.Errorf("an untrimmed span says truncated: %s", b)
	}
}
