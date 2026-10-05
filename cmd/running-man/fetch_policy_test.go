package main

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// limitRecorder is a stub API that records the limit on each /logs request.
type limitRecorder struct {
	mu     sync.Mutex
	limits []string
}

func newLimitRecorder(t *testing.T) (*limitRecorder, *httptest.Server) {
	t.Helper()
	rec := &limitRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/logs" {
			rec.mu.Lock()
			rec.limits = append(rec.limits, r.URL.Query().Get("limit"))
			rec.mu.Unlock()
		}
		_, _ = w.Write([]byte(`{"logs":[],"count":0}`))
	}))
	saved := apiClient
	apiClient = srv.Client()
	t.Cleanup(func() { apiClient = saved; srv.Close() })
	return rec, srv
}

// run executes a command and everything it batches, as the runtime would.
func run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			run(c)
		}
	}
}

// While following the live tail only the newest entries can be on screen, so
// it keeps the bounded request the TUI always made.
func TestTailingFetchesALimitedWindow(t *testing.T) {
	rec, srv := newLimitRecorder(t)

	m := fixtureModel(100, 30)
	m.apiURL = srv.URL
	m.autoScroll = true
	m.searchQuery = ""

	_, cmd := m.fetchForSelectedSource()
	run(cmd)

	if len(rec.limits) != 1 || rec.limits[0] != "1000" {
		t.Errorf("tailing fetched with limit %v, want [1000]", rec.limits)
	}
}

// Scrolled up, history must not end at the live window, and search must count
// everything rather than whatever happened to be loaded. "Everything" has to be
// an explicit limit=0: /logs caps an unqualified request at 1,000, which is how
// the TUI's scroll-back and search were confined to the newest 1,000 entries.
func TestNotTailingFetchesEverything(t *testing.T) {
	for name, setup := range map[string]func(*model){
		"scrolled up": func(m *model) { m.autoScroll = false },
		"searching":   func(m *model) { m.searchQuery = "connect" },
	} {
		t.Run(name, func(t *testing.T) {
			rec, srv := newLimitRecorder(t)
			m := fixtureModel(100, 30)
			m.apiURL = srv.URL
			m.autoScroll = true
			setup(&m)

			_, cmd := m.fetchForSelectedSource()
			run(cmd)

			if len(rec.limits) != 1 || rec.limits[0] != "0" {
				t.Errorf("fetched with limit %v, want an explicit 0", rec.limits)
			}
		})
	}
}

// Leaving the tail fetches everything at once, rather than leaving history cut
// off at the live window until the next tick.
func TestLeavingTheTailRefetchesImmediately(t *testing.T) {
	rec, srv := newLimitRecorder(t)
	m := fixtureModel(100, 30)
	m.apiURL = srv.URL
	m.autoScroll = true
	// More than a screen, or there is nowhere to scroll and the view rightly
	// stays on the tail.
	m.logs = benchLogs(200)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	run(cmd)

	full := false
	for _, l := range rec.limits {
		if l == "0" {
			full = true
		}
	}
	if !full {
		t.Errorf("scrolling up did not trigger a full fetch; requests: %v", rec.limits)
	}
}
