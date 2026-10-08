package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/elbeanio/the_running_man/internal/parser"
)

// startupTab is the name of the startup status tab. Like "Traces", it is a
// view rather than a log source.
const startupTab = "Startup"

// startupRow is one process or Compose service on the startup screen.
type startupRow struct {
	Name   string
	Kind   string   // "process" or "service"
	State  string   // pending, starting, ready, running, blocked, failed
	Detail string   // what it waits for, or why it failed
	Lines  []string // its last log lines, oldest first
}

// startupGlyphs carry the state without colour, so the screen reads on a
// terminal without it.
var startupGlyphs = map[string]string{
	"ready":    "✓",
	"running":  "●",
	"waiting":  "●",
	"starting": "◐",
	"checking": "◐",
	"pending":  "○",
	"stopped":  "■",
	"blocked":  "✗",
	"failed":   "✗",
	"exited":   "✗",
}

var (
	startupReadyStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("70"))  // green
	startupWaitStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // amber
	startupPendingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245")) // grey
	startupFailStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196")) // red
	startupLineStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("244")) // dim
	startupTitleStyle   = lipgloss.NewStyle().Bold(true)
)

func startupStateStyle(state string) lipgloss.Style {
	switch state {
	case "ready", "running":
		return startupReadyStyle
	case "starting", "checking":
		return startupWaitStyle
	case "blocked", "failed", "exited":
		return startupFailStyle
	default:
		return startupPendingStyle
	}
}

// startupSummary is the line above the rows: progress, or why startup stopped.
func startupSummary(rows []startupRow) (string, lipgloss.Style) {
	ready, failed := 0, ""
	for _, r := range rows {
		switch r.State {
		case "ready", "running":
			ready++
		case "failed":
			if failed == "" {
				failed = r.Name + ": " + r.Detail
			}
		}
	}
	switch {
	case failed != "":
		return "Startup stopped: " + failed, startupFailStyle
	case ready == len(rows):
		return "Everything is up. Tab to the logs.", startupReadyStyle
	default:
		return fmt.Sprintf("Starting: %d of %d ready", ready, len(rows)), startupTitleStyle
	}
}

// renderStartup draws the startup screen: a summary line, then one row per
// process and Compose service with its state and its last log lines.
func renderStartup(rows []startupRow, height, width int) string {
	if height <= 0 || width <= 0 {
		return ""
	}
	var out []string
	add := func(s string) {
		if len(out) < height {
			out = append(out, ansi.Truncate(s, width, "…"))
		}
	}

	summary, style := startupSummary(rows)
	add(style.Render(" " + summary))
	add("")

	nameWidth := 0
	for _, r := range rows {
		nameWidth = max(nameWidth, lipgloss.Width(r.Name))
	}
	const stateWidth = 8 // "starting"

	// Each row is a status line and up to two log lines. Rows that do not fit
	// are counted rather than cut off silently.
	for i, r := range rows {
		need := 1 + min(len(r.Lines), 2)
		if len(out)+need > height && i < len(rows) {
			add(startupPendingStyle.Render(fmt.Sprintf(" … %d more", len(rows)-i)))
			break
		}

		st := startupStateStyle(r.State)
		// Sanitised at the point of drawing, as the log view is: the detail
		// and lines come from the API, and one escape sequence reaching the
		// terminal can clear the screen.
		head := fmt.Sprintf(" %s %-*s  %-*s  %s", startupGlyphs[r.State], nameWidth,
			parser.SanitiseLine(r.Name), stateWidth, r.State, parser.SanitiseLine(r.Detail))
		add(st.Render(head))

		lines := r.Lines
		if len(lines) > 2 {
			lines = lines[len(lines)-2:]
		}
		for _, l := range lines {
			add(startupLineStyle.Render("     " + parser.SanitiseLine(l)))
		}
	}

	for len(out) < height {
		out = append(out, "")
	}
	return strings.Join(out, "\n")
}

// --- live data --------------------------------------------------------------

// processView and dependencyView are the parts of /processes the startup
// screen reads.
type processView struct {
	Name         string   `json:"name"`
	Status       string   `json:"status"`
	ExitCode     int      `json:"exit_code"`
	StartupError string   `json:"startup_error"`
	DependsOn    []string `json:"depends_on"`
	Healthcheck  string   `json:"healthcheck"`
}

type dependencyView struct {
	Name    string   `json:"name"`
	State   string   `json:"state"`
	Detail  string   `json:"detail"`
	Sources []string `json:"sources"`
}

type processesView struct {
	Processes    []processView    `json:"processes"`
	Dependencies []dependencyView `json:"dependencies"`
}

// startupMsg carries a fresh set of startup rows. active is false when nothing
// is configured with dependencies, and the screen is not shown at all.
type startupMsg struct {
	active bool
	rows   []startupRow
}

// recentLogLimit is how much of the newest log one startup refresh reads, to
// find each source's last two lines in a single request.
const recentLogLimit = 500

// fetchStartup reads /processes and the newest log lines, and builds the rows.
func fetchStartup(apiURL string) tea.Cmd {
	return func() tea.Msg {
		var pv processesView
		if err := getJSON(apiURL+"/processes", &pv); err != nil {
			return errMsg{err}
		}
		active := len(pv.Dependencies) > 0
		for _, p := range pv.Processes {
			active = active || len(p.DependsOn) > 0
		}
		if !active {
			return startupMsg{}
		}

		var logs logsResponse
		query := url.Values{"limit": {strconv.Itoa(recentLogLimit)}}
		if err := getJSON(apiURL+"/logs?"+query.Encode(), &logs); err != nil {
			return errMsg{err}
		}
		return startupMsg{active: true, rows: buildStartupRows(pv, lastLines(logs.Logs, 2))}
	}
}

// getJSON fetches url from the API and decodes the response into v.
func getJSON(url string, v any) error {
	resp, err := apiClient.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(v)
}

// lastLines returns each source's newest n lines, oldest first, from logs in
// arrival order. A multi-line entry counts by its first line.
func lastLines(logs []logEntry, n int) map[string][]string {
	out := map[string][]string{}
	for _, e := range logs {
		line, _, _ := strings.Cut(e.Message, "\n")
		lines := append(out[e.Source], line)
		if len(lines) > n {
			lines = lines[len(lines)-n:]
		}
		out[e.Source] = lines
	}
	return out
}

// buildStartupRows turns /processes into rows: the Compose services depended
// on first, then the processes, in their configured order.
func buildStartupRows(pv processesView, lines map[string][]string) []startupRow {
	rows := make([]startupRow, 0, len(pv.Dependencies)+len(pv.Processes))

	for _, d := range pv.Dependencies {
		r := startupRow{Name: d.Name, Kind: "service", State: d.State}
		switch d.State {
		case "pending":
			r.Detail = "not checked yet"
		case "checking":
			r.Detail = "waiting for its healthcheck"
		case "ready":
			r.Detail = "its healthcheck passed"
		case "failed":
			r.Detail = strings.TrimPrefix(d.Detail, "the Compose service "+d.Name+" was not ready: ")
		}
		for _, src := range d.Sources {
			r.Lines = append(r.Lines, lines[src]...)
		}
		if len(r.Lines) > 2 {
			r.Lines = r.Lines[len(r.Lines)-2:]
		}
		rows = append(rows, r)
	}

	for _, p := range pv.Processes {
		r := startupRow{Name: p.Name, Kind: "process", Lines: lines[p.Name]}
		ownFailure := strings.TrimPrefix(p.StartupError, p.Name+" was not ready: ")
		switch {
		case p.Status == "pending":
			r.State, r.Detail = "pending", "waiting for "+strings.Join(p.DependsOn, ", ")
		case p.Status == "starting":
			r.State, r.Detail = "starting", "waiting for its healthcheck ("+p.Healthcheck+")"
		case p.Status == "blocked":
			r.State, r.Detail = "blocked", p.StartupError
		case p.Status == "running" && p.StartupError != "":
			r.State, r.Detail = "failed", ownFailure
		case p.Status == "running" && p.Healthcheck != "":
			r.State, r.Detail = "ready", "its healthcheck ("+p.Healthcheck+") passed"
		case p.Status == "running" && len(p.DependsOn) > 0:
			r.State, r.Detail = "running", "dependencies ready"
		case p.Status == "running":
			r.State, r.Detail = "running", "no dependencies"
		case p.Status == "failed":
			r.State, r.Detail = "exited", fmt.Sprintf("exited with code %d", p.ExitCode)
		default:
			r.State, r.Detail = p.Status, ""
		}
		rows = append(rows, r)
	}
	return rows
}

// startupUp reports whether everything has started: nothing waiting, nothing
// failed.
func startupUp(rows []startupRow) bool {
	for _, r := range rows {
		switch r.State {
		case "ready", "running", "waiting", "stopped":
		default:
			return false
		}
	}
	return true
}

// withStartupTab puts the startup tab first, once.
func withStartupTab(sources []string) []string {
	if len(sources) > 0 && sources[0] == startupTab {
		return sources
	}
	return append([]string{startupTab}, sources...)
}

// firstProcessTab is where the screen hands over once everything is up: the
// first process's logs, else the first tab after the startup screen.
func firstProcessTab(sources []string, types map[string]string) int {
	for i, s := range sources {
		if types[s] == "process" {
			return i
		}
	}
	return min(1, len(sources)-1)
}

// applyStartup takes a fresh set of rows: shows the startup tab the first time
// there is anything to show, and hands over to the logs once, when everything
// is up and the screen is still the one being looked at.
func (m model) applyStartup(msg startupMsg) (model, tea.Cmd) {
	if !msg.active {
		return m, nil
	}
	first := !m.startupActive
	m.startupActive = true
	m.startupRows = msg.rows
	if first {
		m.sources = withStartupTab(m.sources)
		m.selectedSource = 0
	}
	if !m.startupHandedOver && m.currentSource() == startupTab && startupUp(msg.rows) {
		m.startupHandedOver = true
		m.selectedSource = firstProcessTab(m.sources, m.sourceTypes)
		return m.fetchForSelectedSource()
	}
	return m, nil
}
