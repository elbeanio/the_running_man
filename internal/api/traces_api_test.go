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
	st := tracing.NewSpanStorage(100, time.Hour, 1<<30)
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

// /health reported the log buffer against its limits but said nothing about
// spans, so nobody could see how close traces were to being evicted.
func TestHealthReportsSpanStorage(t *testing.T) {
	s, _ := traceServer(t)
	_, body := get(t, s, "/health")

	var traces struct {
		TotalSpans int    `json:"total_spans"`
		TotalBytes int64  `json:"total_bytes"`
		MaxSpans   int    `json:"max_spans"`
		MaxBytes   int64  `json:"max_bytes"`
		MaxAge     string `json:"max_age"`
		OldestSpan string `json:"oldest_span"`
	}
	if err := json.Unmarshal(body["traces"], &traces); err != nil {
		t.Fatalf("no traces block in /health: %v", err)
	}
	if traces.TotalSpans != 2 || traces.TotalBytes < 5000 || traces.MaxSpans != 100 ||
		traces.MaxBytes != 1<<30 || traces.MaxAge != "1h0m0s" || traces.OldestSpan == "" {
		t.Errorf("traces block = %+v", traces)
	}
}

// Tracing disabled: no traces block rather than one of zeroes, which would read
// as "tracing is on and nothing has arrived".
func TestHealthOmitsSpanStorageWithoutTracing(t *testing.T) {
	s := NewServer(storage.NewRingBuffer(10, time.Minute, 1<<20), testProjectDir, nil, nil, nil)
	if _, body := get(t, s, "/health"); body["traces"] != nil {
		t.Errorf("traces block present with tracing disabled: %s", body["traces"])
	}
}

// restartedServer is a server over span storage that has been through a
// restart: one trace from the earlier run, loaded back from disk, and one from
// this run.
func restartedServer(t *testing.T) (*Server, time.Time) {
	t.Helper()
	dir := t.TempDir()
	open := func() *tracing.SpanStorage {
		started := time.Now()
		d, err := tracing.OpenDisk(dir, started, time.Hour, 1<<30, nil)
		if err != nil {
			t.Fatal(err)
		}
		st := tracing.NewSpanStorage(100, time.Hour, 1<<30)
		st.AttachDisk(d, started)
		st.LoadFromDisk()
		return st
	}
	open().Add(&tracing.SpanEntry{TraceID: "earlier", SpanID: "a", Name: "chat.turn", StartTime: time.Now()})
	time.Sleep(time.Millisecond)
	restart := time.Now()
	st := open()
	st.Add(&tracing.SpanEntry{TraceID: "now", SpanID: "b", Name: "chat.turn", StartTime: time.Now()})
	return NewServer(storage.NewRingBuffer(10, time.Minute, 1<<20), testProjectDir, nil, nil, st), restart
}

// The TUI draws a line in the trace list where Running Man restarted, and an
// agent can see which traces predate it.
func TestTracesListsRestarts(t *testing.T) {
	s, restart := restartedServer(t)
	_, body := get(t, s, "/traces")

	var restarts []time.Time
	if err := json.Unmarshal(body["restarts"], &restarts); err != nil {
		t.Fatalf("no restarts in /traces: %v", err)
	}
	if len(restarts) != 1 || restarts[0].Before(restart) {
		t.Errorf("restarts = %v, want one, at or after %v", restarts, restart)
	}

	s, _ = traceServer(t)
	if _, body := get(t, s, "/traces"); string(body["restarts"]) != "[]" {
		t.Errorf("without persistence restarts = %s, want []", body["restarts"])
	}
}

// Logs are not kept across a restart, so a trace from before one has none.
// Saying so beats an empty list that reads as "this trace logged nothing".
func TestTraceLogsExplainsLogsLostToARestart(t *testing.T) {
	s, _ := restartedServer(t)

	_, body := get(t, s, "/traces/earlier/logs")
	var note string
	json.Unmarshal(body["note"], &note)
	if !strings.Contains(note, "restart") {
		t.Errorf("note for a trace from before the restart = %q", note)
	}
	if _, body := get(t, s, "/traces/now/logs"); body["note"] != nil {
		t.Errorf("note on a trace from this run: %s", body["note"])
	}
}

func TestHealthReportsSpanPersistence(t *testing.T) {
	s, _ := restartedServer(t)
	_, body := get(t, s, "/health")
	var traces struct {
		Persisted    bool   `json:"persisted"`
		Loading      *bool  `json:"loading"`
		SkippedLines *int   `json:"skipped_lines"`
		DiskBytes    int64  `json:"disk_bytes"`
		Dir          string `json:"dir"`
	}
	json.Unmarshal(body["traces"], &traces)
	if !traces.Persisted || traces.Loading == nil || *traces.Loading || traces.SkippedLines == nil ||
		traces.DiskBytes == 0 || traces.Dir == "" {
		t.Errorf("traces block = %s", body["traces"])
	}

	s, _ = traceServer(t)
	_, body = get(t, s, "/health")
	json.Unmarshal(body["traces"], &traces)
	if traces.Persisted {
		t.Error("persisted reported without a disk")
	}
}
