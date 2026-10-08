package api

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/elbeanio/the_running_man/internal/parser"
	"github.com/elbeanio/the_running_man/internal/storage"
)

func callLogServer(t *testing.T) (http.Handler, *storage.RingBuffer) {
	t.Helper()
	buf := storage.NewRingBuffer(100, time.Hour, 1<<20)
	return NewServer(buf, testProjectDir, nil, nil, nil).routes(), buf
}

func apiCalls(buf *storage.RingBuffer) []*parser.LogEntry {
	var calls []*parser.LogEntry
	for _, e := range buf.Query(storage.QueryFilters{Sources: []string{"running-man"}}) {
		if strings.HasPrefix(e.Message, "api ") {
			calls = append(calls, e)
		}
	}
	return calls
}

// Each request an agent makes is one line in Running Man's own source: method,
// path and query, status, response size and time. The size is the cost to the
// agent's context, which is what the trace discovery levels are judged by.
func TestAPICallLog_LogsExternalCalls(t *testing.T) {
	h, buf := callLogServer(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/health?x=1", nil))

	calls := apiCalls(buf)
	if len(calls) != 1 {
		t.Fatalf("logged %d calls, want 1", len(calls))
	}
	c := calls[0]
	want := "api GET /health?x=1 200 "
	if !strings.HasPrefix(c.Message, want) {
		t.Errorf("message %q, want it to start %q", c.Message, want)
	}
	if !strings.Contains(c.Message, formatBytes(rec.Body.Len())) {
		t.Errorf("message %q does not give the response size %s", c.Message, formatBytes(rec.Body.Len()))
	}
	if c.Level != parser.LevelInfo || c.SourceType != "system" {
		t.Errorf("level %s, source type %s; want info, system", c.Level, c.SourceType)
	}
}

// The TUI polls several endpoints every second; logged, it would bury
// everything else in the tab.
func TestAPICallLog_SkipsRunningMansOwnClient(t *testing.T) {
	h, buf := callLogServer(t)
	req := httptest.NewRequest("GET", "/health", nil)
	req.Header.Set("User-Agent", InternalUserAgent)
	h.ServeHTTP(httptest.NewRecorder(), req)

	if n := len(apiCalls(buf)); n != 0 {
		t.Errorf("logged %d of Running Man's own calls", n)
	}
}

// An agent getting errors back is worth noticing: warn. And the line is not
// parsed -- a query of level=error must not make the call itself an error,
// which the parser's explicit-level rule would.
func TestAPICallLog_Levels(t *testing.T) {
	h, buf := callLogServer(t)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/logs?level=error", nil))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/traces/nope", nil))

	calls := apiCalls(buf)
	if len(calls) != 2 {
		t.Fatalf("logged %d calls, want 2", len(calls))
	}
	if calls[0].Level != parser.LevelInfo {
		t.Errorf("%q logged at %s, want info", calls[0].Message, calls[0].Level)
	}
	if calls[1].Level != parser.LevelWarn || !strings.Contains(calls[1].Message, " 503 ") && !strings.Contains(calls[1].Message, " 404 ") {
		t.Errorf("%q logged at %s, want warn with its 4xx/5xx status", calls[1].Message, calls[1].Level)
	}
}

func TestFormatBytes(t *testing.T) {
	for n, want := range map[int]string{0: "0 B", 512: "512 B", 1536: "1.5 KB", 139673: "136.4 KB", 3 << 20: "3.0 MB"} {
		if got := formatBytes(n); got != want {
			t.Errorf("formatBytes(%d) = %q, want %q", n, got, want)
		}
	}
}

// End to end over the real socket: Running Man's own client is not logged, and
// anything else is. If the client stopped identifying itself, the TUI's polling
// would fill the tab.
func TestAPICallLog_OverTheSocket(t *testing.T) {
	dir, err := os.MkdirTemp("", "rmlog") // short: sun_path is small
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "api.sock")
	ln, err := Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	buf := storage.NewRingBuffer(100, time.Hour, 1<<20)
	srv := NewServer(buf, testProjectDir, nil, nil, nil)
	go func() { _ = srv.Serve(ln) }()
	defer ln.Close()

	own := NewSocketClient(sock)
	resp, err := own.Get("http://localhost/health")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if n := len(apiCalls(buf)); n != 0 {
		t.Fatalf("Running Man's own client was logged (%d lines)", n)
	}

	agent := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", sock)
	}}}
	resp, err = agent.Get("http://localhost/health")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if n := len(apiCalls(buf)); n != 1 {
		t.Errorf("an agent's call logged %d times, want 1", n)
	}
}
