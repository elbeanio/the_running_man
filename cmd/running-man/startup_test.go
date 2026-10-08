package main

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elbeanio/the_running_man/internal/api"
	"github.com/elbeanio/the_running_man/internal/process"
)

// Each API state becomes the row the screen shows, with its own name stripped
// from its failure: the row already says whose it is.
func TestBuildStartupRows(t *testing.T) {
	pv := processesView{
		Dependencies: []dependencyView{
			{Name: "db", State: "ready", Sources: []string{"proj-db-1"}},
			{Name: "denodo", State: "failed",
				Detail: "the Compose service denodo was not ready: its healthcheck (port 9996) did not pass within 3m"},
		},
		Processes: []processView{
			{Name: "api", Status: "running", Healthcheck: "port 8000", DependsOn: []string{"db"}},
			{Name: "worker", Status: "starting", Healthcheck: `log "ready"`},
			{Name: "web", Status: "pending", DependsOn: []string{"api", "worker"}},
			{Name: "backend", Status: "blocked", DependsOn: []string{"denodo"},
				StartupError: "the Compose service denodo was not ready: ..."},
			{Name: "cache", Status: "running", StartupError: "cache was not ready: its healthcheck (port 6379) did not pass within 60s",
				Healthcheck: "port 6379"},
			{Name: "frontend", Status: "running"},
			{Name: "migrate", Status: "failed", ExitCode: 2},
		},
	}
	lines := map[string][]string{"proj-db-1": {"ready to accept connections"}, "api": {"listening"}}

	want := map[string]struct{ state, detail string }{
		"db":       {"ready", "its healthcheck passed"},
		"denodo":   {"failed", "its healthcheck (port 9996) did not pass within 3m"},
		"api":      {"ready", "its healthcheck (port 8000) passed"},
		"worker":   {"starting", `waiting for its healthcheck (log "ready")`},
		"web":      {"pending", "waiting for api, worker"},
		"backend":  {"blocked", "the Compose service denodo was not ready: ..."},
		"cache":    {"failed", "its healthcheck (port 6379) did not pass within 60s"},
		"frontend": {"running", "no dependencies"},
		"migrate":  {"exited", "exited with code 2"},
	}
	rows := buildStartupRows(pv, lines)
	if len(rows) != len(want) {
		t.Fatalf("%d rows, want %d", len(rows), len(want))
	}
	if rows[0].Name != "db" || rows[1].Name != "denodo" || rows[2].Name != "api" {
		t.Errorf("order = %s, %s, %s...: want services first, then processes", rows[0].Name, rows[1].Name, rows[2].Name)
	}
	for _, r := range rows {
		w := want[r.Name]
		if r.State != w.state || r.Detail != w.detail {
			t.Errorf("%s = %s / %q, want %s / %q", r.Name, r.State, r.Detail, w.state, w.detail)
		}
	}
	if got := rows[0].Lines; len(got) != 1 || got[0] != "ready to accept connections" {
		t.Errorf("a service's lines come from its sources: %v", got)
	}
	if got := rows[2].Lines; len(got) != 1 || got[0] != "listening" {
		t.Errorf("a process's lines come from its own source: %v", got)
	}
}

func TestLastLines(t *testing.T) {
	got := lastLines([]logEntry{
		{Source: "a", Message: "1"}, {Source: "b", Message: "x"},
		{Source: "a", Message: "2"}, {Source: "a", Message: "3\nmore"},
	}, 2)
	if a := got["a"]; len(a) != 2 || a[0] != "2" || a[1] != "3" {
		t.Errorf("a = %v, want the last two, first lines only", a)
	}
	if b := got["b"]; len(b) != 1 {
		t.Errorf("b = %v", b)
	}
}

func liveModel() model {
	m := initialModel("http://localhost", nil)
	m.sources = []string{"running-man", "frontend", "backend", "Traces"}
	m.sourceTypes = map[string]string{"running-man": "system", "frontend": "process", "backend": "process", "Traces": "traces"}
	m.selectedSource = 0
	return m
}

var (
	rowsWaiting = []startupRow{{Name: "db", State: "checking"}, {Name: "backend", State: "pending"}}
	rowsUp      = []startupRow{{Name: "db", State: "ready"}, {Name: "backend", State: "ready"}}
	rowsStopped = []startupRow{{Name: "db", State: "failed"}, {Name: "backend", State: "blocked"}}
)

// The first time there is something to show, the startup tab appears first and
// is selected. With no dependencies configured it never appears.
func TestStartup_TabAppearsAndIsSelected(t *testing.T) {
	m, _ := liveModel().applyStartup(startupMsg{})
	if m.sources[0] == startupTab {
		t.Fatal("startup tab shown with nothing configured")
	}

	m, _ = liveModel().applyStartup(startupMsg{active: true, rows: rowsWaiting})
	if m.currentSource() != startupTab || m.sources[0] != startupTab {
		t.Errorf("current = %q, sources = %v", m.currentSource(), m.sources)
	}
}

// Once everything is up it hands over to the first process's logs, once.
func TestStartup_HandsOverOnceWhenUp(t *testing.T) {
	m, _ := liveModel().applyStartup(startupMsg{active: true, rows: rowsWaiting})
	m, _ = m.applyStartup(startupMsg{active: true, rows: rowsUp})
	if got := m.currentSource(); got != "frontend" {
		t.Fatalf("after everything came up, showing %q, want the first process", got)
	}

	// Going back to look is allowed, and does not bounce.
	m.selectedSource = 0
	m, _ = m.applyStartup(startupMsg{active: true, rows: rowsUp})
	if got := m.currentSource(); got != startupTab {
		t.Errorf("handed over a second time, to %q", got)
	}
}

// A stopped startup never hands over: the screen is the explanation.
func TestStartup_StoppedStaysOnScreen(t *testing.T) {
	m, _ := liveModel().applyStartup(startupMsg{active: true, rows: rowsStopped})
	if got := m.currentSource(); got != startupTab {
		t.Errorf("left the startup screen for %q while startup was stopped", got)
	}
}

// Moving off the screen before everything is up means no jump later.
func TestStartup_NoHandOverIfAlreadyElsewhere(t *testing.T) {
	m, _ := liveModel().applyStartup(startupMsg{active: true, rows: rowsWaiting})
	m.selectedSource = 3 // backend
	m, _ = m.applyStartup(startupMsg{active: true, rows: rowsUp})
	if got := m.currentSource(); got != "backend" {
		t.Errorf("jumped to %q from a tab the user chose", got)
	}
}

// End to end over the socket, with responses encoded from the API's own
// types, so a field renamed on one side and not the other is caught.
func TestFetchStartup_OverTheSocket(t *testing.T) {
	dir, err := os.MkdirTemp("", "rm") // short: t.TempDir() can exceed sun_path
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "api.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}

	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/processes":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"processes": []process.ProcessInfo{
					{Name: "backend", Status: process.StatusPending, DependsOn: []string{"db"}},
				},
				"dependencies": []process.DependencyInfo{
					{Name: "db", State: process.DependencyChecking, Sources: []string{"proj-db-1"}},
				},
			})
		case "/logs":
			_ = json.NewEncoder(w).Encode(map[string]any{"logs": []logEntry{
				{Source: "proj-db-1", Message: "database system is starting up"},
			}})
		}
	})}
	go srv.Serve(ln)
	defer srv.Close()

	saved := apiClient
	apiClient = api.NewSocketClient(sock)
	defer func() { apiClient = saved }()

	msg, ok := fetchStartup(apiBaseURL)().(startupMsg)
	if !ok || !msg.active || len(msg.rows) != 2 {
		t.Fatalf("got %#v", msg)
	}
	db, backend := msg.rows[0], msg.rows[1]
	if db.State != "checking" || len(db.Lines) != 1 || !strings.Contains(db.Lines[0], "starting up") {
		t.Errorf("db row = %+v", db)
	}
	if backend.State != "pending" || backend.Detail != "waiting for db" {
		t.Errorf("backend row = %+v", backend)
	}
}
