package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/elbeanio/the_running_man/internal/storage"
	"github.com/elbeanio/the_running_man/internal/tracing"
)

func traceServer(t *testing.T) (*Server, string) {
	t.Helper()
	st := tracing.NewSpanStorage(100, time.Hour)
	at := time.Now().Add(-time.Minute)
	big := strings.Repeat("x", 5000)
	st.Add(&tracing.SpanEntry{TraceID: "t1", SpanID: "root", Name: "chat.turn", ServiceName: "duet",
		StartTime: at, EndTime: at.Add(time.Second), Duration: time.Second, Status: "ok",
		Attributes: map[string]string{"input.value": "what sold best?"}})
	st.Add(&tracing.SpanEntry{TraceID: "t1", SpanID: "llm", ParentSpanID: "root", Name: "ChatCompletion", ServiceName: "duet",
		StartTime: at, EndTime: at.Add(time.Second), Duration: time.Second, Status: "error",
		Attributes: map[string]string{"input.value": big}})
	return NewServer(storage.NewRingBuffer(10, time.Minute, 1<<20), testProjectDir, nil, nil, st), big
}

func get(t *testing.T, s *Server, path string) (int, map[string]json.RawMessage) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("%s: not JSON (%d): %s", path, rec.Code, rec.Body.String())
	}
	return rec.Code, body
}

// Level one: summaries, enough to pick a trace, and no span attributes -- a
// single trace can carry megabytes of them.
func TestTracesAPI_ListsSummaries(t *testing.T) {
	s, big := traceServer(t)
	code, body := get(t, s, "/traces")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	if strings.Contains(string(body["traces"]), big[:100]) {
		t.Error("the list carries span attributes")
	}
	var traces []tracing.TraceSummary
	_ = json.Unmarshal(body["traces"], &traces)
	if len(traces) != 1 {
		t.Fatalf("got %d traces in %d bytes", len(traces), len(body["traces"]))
	}
	tr := traces[0]
	if tr.TraceID != "t1" || tr.RootSpan != "chat.turn" || tr.Summary != "what sold best?" ||
		tr.SpanCount != 2 || tr.ErrorCount != 1 || tr.Status != "error" {
		t.Errorf("summary = %+v", tr)
	}

	if _, body := get(t, s, "/traces?status=ok"); string(body["count"]) != "0" {
		t.Errorf("status=ok matched a failing trace: %s", body["traces"])
	}
	if code, _ := get(t, s, "/traces?status=bogus"); code != 400 {
		t.Errorf("status=bogus: %d, want 400", code)
	}
}

// Level two: one trace, every span, values cut at 1 KB with their sizes.
func TestTracesAPI_OneTraceIsTrimmed(t *testing.T) {
	s, big := traceServer(t)
	code, body := get(t, s, "/traces/t1")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	var spans []map[string]any
	_ = json.Unmarshal(body["spans"], &spans)
	if len(spans) != 2 {
		t.Fatalf("spans = %s", body["spans"])
	}
	llm := spans[1]
	val := llm["attributes"].(map[string]any)["input.value"].(string)
	if len(val) > 1100 || strings.Contains(string(body["spans"]), big) {
		t.Errorf("value not cut: %d bytes", len(val))
	}
	if tr, _ := llm["truncated"].(map[string]any); tr["input.value"] != float64(len(big)) {
		t.Errorf("truncated = %v, want input.value: %d", llm["truncated"], len(big))
	}
	var sum tracing.TraceSummary
	_ = json.Unmarshal(body["trace"], &sum)
	if sum.RootSpan != "chat.turn" {
		t.Errorf("trace = %s", body["trace"])
	}
	if code, _ := get(t, s, "/traces/nope"); code != 404 {
		t.Errorf("unknown trace: %d, want 404", code)
	}
}

// Level three: one span, nothing cut -- how an agent reads the images in a
// model's input.
func TestTracesAPI_OneSpanInFull(t *testing.T) {
	s, big := traceServer(t)
	code, body := get(t, s, "/traces/t1/spans/llm")
	if code != 200 {
		t.Fatalf("status %d: %v", code, body)
	}
	var sp map[string]any
	_ = json.Unmarshal(body["span"], &sp)
	if sp["attributes"].(map[string]any)["input.value"] != big {
		t.Error("the full value is not returned")
	}
	if code, _ := get(t, s, "/traces/t1/spans/nope"); code != 404 {
		t.Errorf("unknown span: %d, want 404", code)
	}
	if code, _ := get(t, s, "/traces/nope/spans/llm"); code != 404 {
		t.Errorf("unknown trace: %d, want 404", code)
	}
}

// The correlated logs endpoint is unchanged.
func TestTracesAPI_LogsStillServed(t *testing.T) {
	s, _ := traceServer(t)
	if code, body := get(t, s, "/traces/t1/logs"); code != 200 || body["logs"] == nil {
		t.Errorf("logs: %d %v", code, body)
	}
}
