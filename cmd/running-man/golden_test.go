package main

// Golden frames: what the TUI actually draws, recorded and compared.
//
// tui_test.go had 33 tests and not one of them called View(). Every test
// exercised a component in isolation -- renderLogs, renderHeader,
// buildMatchLineIndex -- which is why the bugs that survived were the ones in
// the assembly: the footer wrapping and taking the content window with it, the
// page size disagreeing with the visible height, borders not meeting their tabs.
// None of those are visible from any single function.
//
// Frames are captured from View() on a model built by hand rather than by
// driving a tea.Program. A Program needs a pty, a size negotiation and a
// settling period, none of which make the frame more truthful, and all of which
// make it less reproducible. plans/ideas.md suggested teatest for this;
// RequireEqualOutput is the same comparison with a program's worth of timing
// attached, so it is not used here.
//
// The colour profile is forced, because it has to be. In a test run stdout is
// not a terminal, so lipgloss resolves to Ascii and emits no escape sequences
// at all -- goldens captured that way would pass through any colour regression
// without noticing. Layout frames are therefore captured under Ascii, where the
// bytes are the layout, and colour is checked separately under TrueColor.

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

var updateGolden = flag.Bool("update", false, "rewrite golden frames from current output")

// withProfile forces a colour profile for the duration of a test.
//
// SetColorProfile acts on the default renderer, which every package-level style
// holds a pointer to, so it takes effect even though the styles were built at
// init long before this runs.
func withProfile(t *testing.T, p termenv.Profile) {
	t.Helper()
	lipgloss.SetColorProfile(p)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })
}

// assertGolden compares a frame against its recorded form, or rewrites it under
// -update.
func assertGolden(t *testing.T, name, got string) {
	t.Helper()

	path := filepath.Join("testdata", name+".golden")
	if *updateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("creating testdata: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v\n\nRun `go test ./cmd/running-man -run Golden -update` to record it.", path, err)
	}
	if got == string(want) {
		return
	}

	t.Errorf("frame %q differs from its golden.\n%s", name, frameDiff(string(want), got))
}

// frameDiff reports the first differing row with both versions, which is more
// use for a frame than a character-level diff: the eye finds a layout fault in
// the row, not in the byte.
func frameDiff(want, got string) string {
	wantRows := strings.Split(want, "\n")
	gotRows := strings.Split(got, "\n")

	var b strings.Builder
	if len(wantRows) != len(gotRows) {
		b.WriteString(fmt.Sprintf("row count: got %d, want %d\n", len(gotRows), len(wantRows)))
	}
	for i := 0; i < max(len(wantRows), len(gotRows)); i++ {
		w, g := rowAt(wantRows, i), rowAt(gotRows, i)
		if w == g {
			continue
		}
		b.WriteString(fmt.Sprintf("first difference at row %d:\n  want %q (%d cols)\n  got  %q (%d cols)\n",
			i, w, displayWidth(w), g, displayWidth(g)))
		break
	}
	b.WriteString("\n--- got ---\n")
	b.WriteString(got)
	return b.String()
}

func rowAt(rows []string, i int) string {
	if i < len(rows) {
		return rows[i]
	}
	return "<missing>"
}

// --- fixtures -------------------------------------------------------------
//
// Fixed content and fixed sizes. Timestamps are literals rather than formatted
// from a clock, and nothing in the render path reads one, so a frame is a pure
// function of the model.

func fixtureLogs() []logEntry {
	return []logEntry{
		{Timestamp: "2026-10-04T09:15:01Z", Level: "info", Source: "backend",
			Message: "listening on http://localhost:8080"},
		{Timestamp: "2026-10-04T09:15:02Z", Level: "warn", Source: "backend",
			Message: "deprecated config key 'legacy_mode'"},
		{Timestamp: "2026-10-04T09:15:03Z", Level: "error", Source: "backend",
			Message: "could not connect to database: connection refused", IsError: true,
			TraceID: "4bf92f3577b34da6a3ce929d0e0e4736"},
		{Timestamp: "2026-10-04T09:15:04Z", Level: "info", Source: "backend",
			Message: "retrying in 5s"},
		{Timestamp: "2026-10-04T09:15:05Z", Level: "error", Source: "backend",
			Message: "Traceback (most recent call last):\n  File \"app.py\", line 42\n    connect()\nConnectionError: refused",
			IsError: true},
	}
}

func fixtureTraces() []traceSummary {
	return []traceSummary{
		{TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", Status: "error",
			Services: []string{"backend", "postgres"}},
		{TraceID: "00f067aa0ba902b7a3ce929d0e0e4736", Status: "ok",
			Services: []string{"backend"}},
	}
}

// fixtureModel builds a model at a fixed size with fixed content.
func fixtureModel(width, height int) model {
	m := initialModel("http://localhost", nil)
	m.width, m.height = width, height
	m.sources = []string{"running-man", "backend", "Traces"}
	m.sourceTypes = map[string]string{
		"running-man": "system",
		"backend":     "process",
		"Traces":      "traces",
	}
	m.selectedSource = 1 // backend
	m.logs = fixtureLogs()
	m.traces = fixtureTraces()
	m.showTraceIDs = true
	return m
}

// --- the frames -----------------------------------------------------------

func TestGoldenFrames(t *testing.T) {
	cases := []struct {
		name  string
		build func() model
	}{
		{"logs-120x24", func() model { return fixtureModel(120, 24) }},
		{"logs-80x24", func() model { return fixtureModel(80, 24) }},
		// 100 columns is where the footer was found to wrap, taking the content
		// window with it.
		{"logs-100x24", func() model { return fixtureModel(100, 24) }},
		{"logs-40x16", func() model { return fixtureModel(40, 16) }},
		// Below 20 columns with trace IDs shown used to be a panic.
		{"logs-18x12", func() model { return fixtureModel(18, 12) }},
		{"logs-empty", func() model {
			m := fixtureModel(80, 16)
			m.logs = nil
			return m
		}},
		{"logs-no-trace-ids", func() model {
			m := fixtureModel(80, 20)
			m.showTraceIDs = false
			return m
		}},
		{"logs-scrolled", func() model {
			m := fixtureModel(80, 14)
			m.scrollOffset = 3
			m.autoScroll = false
			return m
		}},
		{"search-active", func() model {
			m := fixtureModel(100, 20)
			m.mode = ModeSearch
			m.searchQuery = "connect"
			m.searchInput = textinput.New()
			m.searchInput.SetValue("connect")
			return m
		}},
		{"search-results", func() model {
			m := fixtureModel(100, 20)
			m.searchQuery = "connect"
			m.searchMatchIdx = 1
			return m
		}},
		{"traces-list", func() model {
			m := fixtureModel(100, 20)
			m.selectedSource = 2 // Traces
			return m
		}},
		{"trace-detail", func() model {
			m := fixtureModel(100, 20)
			m.selectedSource = 2
			m.mode = ModeTraceDetail
			m.selectedTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
			m.traceSpans = []spanDetail{
				{SpanID: "a1", Name: "GET /things", ServiceName: "backend", Status: "error"},
				{SpanID: "b2", ParentSpanID: "a1", Name: "SELECT things", ServiceName: "postgres", Status: "ok"},
			}
			m.traceLogs = fixtureLogs()[:2]
			return m
		}},
		{"error-state", func() model {
			m := fixtureModel(80, 16)
			m.err = errFixture{}
			return m
		}},
		{"recovered-panic-warning", func() model {
			m := fixtureModel(120, 20)
			m.lastPanic = "View panicked: runtime error: index out of range [7]"
			return m
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Ascii: no escape sequences, so the bytes of the frame are the
			// layout and nothing else.
			withProfile(t, termenv.Ascii)
			assertGolden(t, tc.name, tc.build().View())
		})
	}
}

// Colour is checked separately and on one frame only. Under Ascii the escape
// sequences are absent entirely, so a layout golden cannot speak to colour; a
// full set of coloured goldens would meanwhile break on every styling tweak for
// no extra coverage.
func TestGoldenColour(t *testing.T) {
	withProfile(t, termenv.TrueColor)
	assertGolden(t, "colour-100x20", fixtureModel(100, 20).View())
}

type errFixture struct{}

func (errFixture) Error() string { return "could not reach the instance socket" }

// Every row of a frame must fit the terminal it was drawn for.
//
// This is the invariant the golden frames exist to protect, and the one that was
// broken: at 100 columns every row came out 108 wide, because the help line is
// assembled at its natural length and lipgloss pads the rest of the frame to
// match the widest row. The terminal then wraps it, which pushes the layout down
// a line and desynchronises everything measured against the height.
//
// Measured with ansi.StringWidth, the same accounting lipgloss uses, so the test
// agrees with the renderer about what a wide character costs rather than
// imposing a second opinion.
func TestFramesFitTheirWidth(t *testing.T) {
	withProfile(t, termenv.Ascii)

	sizes := []struct{ w, h int }{
		{18, 12}, {40, 16}, {60, 20}, {80, 24}, {100, 24}, {120, 24}, {200, 30},
	}
	views := map[string]func(model) string{
		"logs":         func(m model) string { return m.View() },
		"traces":       func(m model) string { m.selectedSource = 2; return m.View() },
		"search":       func(m model) string { m.mode = ModeSearch; m.searchQuery = "connect"; return m.View() },
		"trace-detail": func(m model) string { m.mode = ModeTraceDetail; m.selectedSource = 2; return m.View() },
	}

	for _, size := range sizes {
		for name, render := range views {
			t.Run(fmt.Sprintf("%s-%dx%d", name, size.w, size.h), func(t *testing.T) {
				frame := render(fixtureModel(size.w, size.h))
				for i, row := range strings.Split(frame, "\n") {
					if got := displayWidth(row); got > size.w {
						t.Errorf("row %d is %d columns wide, terminal is %d:\n  %q",
							i, got, size.w, row)
					}
				}
			})
		}
	}
}

// The content window and the page size must be the same number.
//
// View derives the window from lipgloss.Height of the header, search bar and
// help, while PgUp/PgDn paged by the constant uiHeaderFooterHeight. Whenever
// those differed -- most easily by the help line wrapping -- a page moved by a
// different amount than the window showed, so scrolling skipped or repeated
// rows.
func TestPageSizeMatchesTheVisibleWindow(t *testing.T) {
	withProfile(t, termenv.Ascii)

	for _, size := range []struct{ w, h int }{{60, 20}, {80, 24}, {100, 24}, {120, 40}} {
		m := fixtureModel(size.w, size.h)
		window := m.contentHeight()
		page := m.pageSize()
		if window != page {
			t.Errorf("%dx%d: window shows %d rows but a page moves %d",
				size.w, size.h, window, page)
		}
		if window <= 0 {
			t.Errorf("%dx%d: content window is %d rows", size.w, size.h, window)
		}
	}
}
