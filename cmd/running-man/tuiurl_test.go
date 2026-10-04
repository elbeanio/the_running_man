package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elbeanio/the_running_man/internal/api"
)

// Source names went into the query string unescaped. Process names are
// validated to [a-zA-Z0-9_-], but OTLP service names are chosen by the sender,
// so "my app" or anything with & or # produced a broken request and that tab
// never loaded.
func TestFetchLogsEscapesTheSourceName(t *testing.T) {
	var gotSource string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSource = r.URL.Query().Get("source")
		_, _ = w.Write([]byte(`{"logs":[],"count":0}`))
	}))
	defer srv.Close()

	saved := apiClient
	apiClient = srv.Client()
	defer func() { apiClient = saved }()

	const name = "my app & co #1"
	fetchLogs(srv.URL, name)()

	if gotSource != name {
		t.Errorf("server received source=%q, want %q", gotSource, name)
	}
}

// The restart key posted with http.Post -- the default client -- rather than
// apiClient. Since the API moved to a Unix socket in #35 that went to TCP
// localhost:80, reached nothing, and the error was dropped: pressing r did
// nothing at all, silently. Tested over a real socket, because a TCP stub would
// hide exactly this.
func TestRestartKeyReachesTheSocket(t *testing.T) {
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
	posted := make(chan string, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posted <- r.URL.Path
		}
	})}
	go srv.Serve(ln)
	defer srv.Close()

	saved := apiClient
	apiClient = api.NewSocketClient(sock)
	defer func() { apiClient = saved }()

	m := initialModel(apiBaseURL, nil) // http://localhost, exactly as in production
	m.sources = []string{"backend", "Traces"}
	m.selectedSource = 0

	_, cmd := m.updateNormalMode(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if cmd != nil {
		cmd()
	}

	select {
	case path := <-posted:
		if path != "/processes/backend/restart" {
			t.Errorf("posted to %q", path)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("restart never reached the socket")
	}
}
