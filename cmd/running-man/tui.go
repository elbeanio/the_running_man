package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/elbeanio/the_running_man/internal/api"
	"github.com/elbeanio/the_running_man/internal/instance"
	"github.com/elbeanio/the_running_man/internal/parser"
	"github.com/elbeanio/the_running_man/internal/process"
	"github.com/elbeanio/the_running_man/internal/termout"
)

const (
	defaultPollInterval = 2 * time.Second

	// minMessageWidth is how much room the message needs before a trace
	// indicator is worth adding alongside it.
	minMessageWidth = 30

	// maxTraceIDDisplayLength is the trace ID's share of a log line, excluding
	// the "[trace:" prefix and "]" suffix, so the whole indicator is this plus 8.
	maxTraceIDDisplayLength = 12
)

// Mode represents the current operating mode of the TUI
type Mode int

const (
	ModeNormal Mode = iota
	ModeSearch
	ModeTraceList
	ModeTraceDetail
)

// Model holds the TUI state
type model struct {
	apiURL         string
	sources        []string
	sourceTypes    map[string]string // source name -> type ("process", "docker", "system", "traces")
	selectedSource int
	logs           []logEntry
	err            error
	width          int
	height         int
	manager        *process.Manager // Process manager to stop on quit
	scrollOffset   int              // Number of lines scrolled from bottom (0 = showing latest)
	autoScroll     bool             // Whether to auto-scroll to bottom on new logs

	// pinnedTop holds the view at the oldest line, the way autoScroll holds it
	// at the newest. Set by Home. Without it, the full history that arrives
	// after leaving the live tail landed below the view, and Home stopped at
	// the top of the live window rather than the top of the log.
	pinnedTop      bool
	mode           Mode            // Current mode (normal or search)
	searchInput    textinput.Model // Text input for search mode
	searchQuery    string          // Current search query (mirrored from searchInput)
	searchMatchIdx int             // Current match index when navigating with n/N
	showTraceIDs   bool            // Whether to show trace indicators (toggled with 't')

	// Trace view state
	traces            []traceSummary // List of trace summaries
	traceScrollOffset int            // Scroll offset for trace list
	selectedTraceIdx  int            // Selected trace index in list

	// Trace detail view state
	selectedTraceID         string       // ID of selected trace for detail view
	traceSpans              []spanDetail // Spans for selected trace
	traceLogs               []logEntry   // Logs correlated with selected trace
	traceDetailScrollOffset int          // Scroll offset for trace detail view

	// Internal state
	tickCount int // Count of tick messages received

	// matchIndex is the display-line index of every search match, built once
	// when the logs or the query change rather than on every frame. It used to
	// be rebuilt by each of the help line, the search bar and the highlighting
	// pass: three or four full lowercase scans of every log per frame, which
	// made a frame cost 26ms at a full buffer while searching.
	//
	// Read through matches(), never directly: matchFor records what it was
	// built from, so a stale index is rebuilt rather than trusted. Forgetting
	// refreshMatches after changing the logs or the query then costs speed, not
	// correctness -- which matters, because the first draft of this missed it
	// in nine places.
	matchIndex []int
	matchFor   matchKey

	// notice is a one-line status shown in the footer, such as the outcome of a
	// restart. Empty when there is nothing to say.
	notice string

	// lastPanic holds the summary of a recovered panic, shown in the footer so a
	// render bug is visible rather than silent. Empty when nothing has gone
	// wrong.
	lastPanic string

	// startupRows are the rows of the startup screen; see tuistartup.go.
	startupRows []startupRow
	// startupActive is set once anything is configured with dependencies,
	// which is when the startup tab exists.
	startupActive bool
	// startupHandedOver is set once the screen has switched to the logs, so
	// it does so only once.
	startupHandedOver bool
}

type logEntry struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Source    string `json:"source"`
	Message   string `json:"message"`
	TraceID   string `json:"trace_id,omitempty"` // Optional trace ID for correlation
}

type traceSummary struct {
	TraceID   string
	Duration  time.Duration
	Status    string
	Services  []string
	StartTime time.Time
	SpanCount int
}

type spanDetail struct {
	SpanID       string
	ParentSpanID string
	Name         string
	Kind         string
	StartTime    time.Time
	EndTime      time.Time
	Duration     time.Duration
	Status       string
	StatusCode   string
	ServiceName  string
	Attributes   map[string]string
}

type logsResponse struct {
	Logs  []logEntry `json:"logs"`
	Count int        `json:"count"`
}

type healthResponse struct {
	Sources []sourceInfo `json:"sources"`
}

type sourceInfo struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	EntryCount int    `json:"entry_count"`
}

// Messages for async operations
type sourcesMsg struct {
	names []string
	types map[string]string
}

// logsMsg carries one /logs response and what it was a response to. A response
// used not to say, so a slow one could land after a newer one and overwrite
// it: a reply for the previous tab replaced the current tab's logs, and a
// live-window reply replaced the full history fetched on scrolling up.
type logsMsg struct {
	source  string
	limited bool
	logs    []logEntry
}
type tracesMsg []traceSummary
type traceSpansMsg []spanDetail
type traceLogsMsg []logEntry
type errMsg struct{ err error }
type tickMsg time.Time

func (e errMsg) Error() string { return e.err.Error() }

// matchKey identifies the logs and query a match index was built from.
//
// The logs are identified by their backing array and length: the TUI replaces
// m.logs wholesale on every fetch, so a new fetch is a new array even when the
// length is unchanged, as it is with a full buffer.
type matchKey struct {
	query string
	first *logEntry
	n     int
}

func keyFor(logs []logEntry, query string) matchKey {
	k := matchKey{query: query, n: len(logs)}
	if len(logs) > 0 {
		k.first = &logs[0]
	}
	return k
}

// maxScroll is the largest useful scroll offset: the oldest line at the top of
// the window.
func (m model) maxScroll() int {
	total := 0
	for i := range m.logs {
		total += entryLines(m.logs[i])
	}
	return max(0, total-m.contentHeight())
}

// scrollBy moves the log view; positive is up, toward older lines.
//
// Clamped to what exists. Scrolling used to be unbounded: Home set the offset
// to math.MaxInt, so PgUp afterwards overflowed it to a large negative number,
// which renders as the bottom -- Home then PgUp jumped to the newest output --
// while PgDn from Home subtracted a page from MaxInt and appeared to do
// nothing. And up past the top kept counting invisibly, so it took as many
// downs to come back.
func (m *model) scrollBy(delta int) {
	m.pinnedTop = false
	m.scrollOffset = max(0, min(m.scrollOffset+delta, m.maxScroll()))
	m.autoScroll = m.scrollOffset == 0
}

// scrollToTop shows the oldest lines and stays there as history loads.
func (m *model) scrollToTop() {
	m.scrollOffset = m.maxScroll()
	m.autoScroll = m.scrollOffset == 0
	m.pinnedTop = !m.autoScroll
}

// scrollToBottom follows the newest output.
func (m *model) scrollToBottom() {
	m.scrollOffset = 0
	m.autoScroll = true
	m.pinnedTop = false
}

// liveFetchLimit is how many entries the TUI asks for while following the live
// tail. Comfortably more than any screen, so the view never comes up short, and
// the same as the API's default -- so tailing costs exactly what it always did.
const liveFetchLimit = 1000

// tailing reports whether the view is following the newest output with no
// search, the case where only the most recent entries can be on screen.
func (m model) tailing() bool {
	return m.autoScroll && m.searchQuery == ""
}

// logLimit is how many entries to fetch: the live window while tailing, and
// everything (zero) otherwise.
//
// The TUI never asked for more than /logs gives by default, which since #23 has
// been 1,000. So with a buffer of up to 10,000, scrolling back stopped at the
// newest 1,000 entries and search counted matches only among them -- both
// silently. Scrolled up, or searching, it now fetches the whole buffer. While
// tailing only the newest entries can be on screen, so it keeps the cheaper
// request it always made.
func (m model) logLimit() int {
	if m.tailing() {
		return liveFetchLimit
	}
	return 0
}

// refreshMatches rebuilds the search match index. Called wherever the logs or
// the query change -- which is the only time the answer can.
func (m *model) refreshMatches() {
	m.matchIndex = buildMatchLineIndex(m.logs, m.searchQuery)
	m.matchFor = keyFor(m.logs, m.searchQuery)
}

// matches returns the search match index, rebuilding it if it is stale.
func (m model) matches() []int {
	if m.matchFor == keyFor(m.logs, m.searchQuery) {
		return m.matchIndex
	}
	return buildMatchLineIndex(m.logs, m.searchQuery)
}

// currentSource returns the selected source name, or "" when there is nothing
// to select.
//
// Every read of m.sources goes through here. The direct indexing it replaces was
// the cause of the crash that hit an unattended instance: the sources list is
// rebuilt from whatever is in the ring buffer, so a quiet source disappears once
// its entries age out past the retention limits, the list gets shorter, and
// selectedSource is left pointing past the end. A `len(m.sources) > 0` test does
// not help -- it was present at two of the call sites and guarded nothing, since
// the problem is the index, not emptiness.
func (m model) currentSource() string {
	if m.selectedSource < 0 || m.selectedSource >= len(m.sources) {
		return ""
	}
	return m.sources[m.selectedSource]
}

// clampSelectedSource brings the selection back into range after the sources
// list changes. Called wherever m.sources is assigned.
func (m *model) clampSelectedSource() {
	if len(m.sources) == 0 {
		m.selectedSource = 0
		return
	}
	if m.selectedSource >= len(m.sources) {
		m.selectedSource = len(m.sources) - 1
	}
	if m.selectedSource < 0 {
		m.selectedSource = 0
	}
}

// clampSelectedTrace brings the trace selection back into range after the trace
// list changes.
func (m *model) clampSelectedTrace() {
	if len(m.traces) == 0 {
		m.selectedTraceIdx = 0
		return
	}
	if m.selectedTraceIdx >= len(m.traces) {
		m.selectedTraceIdx = len(m.traces) - 1
	}
	if m.selectedTraceIdx < 0 {
		m.selectedTraceIdx = 0
	}
}

// selectedTrace returns the highlighted trace, or false when there is none.
func (m model) selectedTrace() (traceSummary, bool) {
	if m.selectedTraceIdx < 0 || m.selectedTraceIdx >= len(m.traces) {
		return traceSummary{}, false
	}
	return m.traces[m.selectedTraceIdx], true
}

// fetchForSelectedSource returns the appropriate command based on selected tab
func (m model) fetchForSelectedSource() (model, tea.Cmd) {
	source := m.currentSource()
	if source == "" {
		return m, nil
	}
	if source == "Traces" {
		return m, fetchTraces(m.apiURL)
	}
	if source == startupTab {
		return m, fetchStartup(m.apiURL)
	}
	return m, fetchLogs(m.apiURL, source, m.logLimit())
}

// isTraceView returns true if the currently selected tab is "Traces"
func (m model) isTraceView() bool {
	return m.currentSource() == "Traces"
}

func fetchTraces(apiURL string) tea.Cmd {
	return func() tea.Msg {
		resp, err := apiClient.Get(apiURL + "/traces")
		if err != nil {
			return errMsg{err}
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return errMsg{err}
		}

		// Parse the response
		var response struct {
			Traces []struct {
				TraceID      string            `json:"trace_id"`
				SpanID       string            `json:"span_id"`
				ParentSpanID string            `json:"parent_span_id"`
				Name         string            `json:"name"`
				Kind         string            `json:"kind"`
				StartTime    time.Time         `json:"start_time"`
				EndTime      time.Time         `json:"end_time"`
				Duration     string            `json:"duration"` // Duration as string like "1.23456789s"
				Status       string            `json:"status"`
				StatusCode   string            `json:"status_code"`
				ServiceName  string            `json:"service_name"`
				Attributes   map[string]string `json:"attributes"`
			} `json:"traces"`
			Count int `json:"count"`
		}

		if err := json.Unmarshal(body, &response); err != nil {
			return errMsg{err}
		}

		// Aggregate spans by trace ID
		traceMap := make(map[string]*traceSummary)
		for _, span := range response.Traces {
			summary, exists := traceMap[span.TraceID]
			if !exists {
				// Parse duration from string
				duration, _ := time.ParseDuration(span.Duration)

				summary = &traceSummary{
					TraceID:   span.TraceID,
					Duration:  duration,
					Status:    span.Status,
					Services:  []string{span.ServiceName},
					StartTime: span.StartTime,
					SpanCount: 1,
				}
				traceMap[span.TraceID] = summary
			} else {
				// Update existing summary
				summary.SpanCount++

				// Update duration if this span is longer
				duration, _ := time.ParseDuration(span.Duration)
				if duration > summary.Duration {
					summary.Duration = duration
				}

				// Update status if this span has error
				if span.Status == "ERROR" {
					summary.Status = "ERROR"
				}

				// Add service if not already in list
				found := false
				for _, s := range summary.Services {
					if s == span.ServiceName {
						found = true
						break
					}
				}
				if !found && span.ServiceName != "" {
					summary.Services = append(summary.Services, span.ServiceName)
				}

				// Update start time if earlier
				if span.StartTime.Before(summary.StartTime) {
					summary.StartTime = span.StartTime
				}
			}
		}

		// Convert map to slice
		summaries := make([]traceSummary, 0, len(traceMap))
		for _, summary := range traceMap {
			summaries = append(summaries, *summary)
		}

		// Sort by start time (newest first)
		sort.Slice(summaries, func(i, j int) bool {
			return summaries[i].StartTime.After(summaries[j].StartTime)
		})

		return tracesMsg(summaries)
	}
}

func fetchTraceSpans(apiURL, traceID string) tea.Cmd {
	return func() tea.Msg {
		resp, err := apiClient.Get(apiURL + "/traces?" + url.Values{"trace_id": {traceID}}.Encode())
		if err != nil {
			return errMsg{err}
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return errMsg{err}
		}

		// Parse the response
		var response struct {
			Traces []struct {
				TraceID      string            `json:"trace_id"`
				SpanID       string            `json:"span_id"`
				ParentSpanID string            `json:"parent_span_id"`
				Name         string            `json:"name"`
				Kind         string            `json:"kind"`
				StartTime    time.Time         `json:"start_time"`
				EndTime      time.Time         `json:"end_time"`
				Duration     string            `json:"duration"`
				Status       string            `json:"status"`
				StatusCode   string            `json:"status_code"`
				ServiceName  string            `json:"service_name"`
				Attributes   map[string]string `json:"attributes"`
			} `json:"traces"`
			Count int `json:"count"`
		}

		if err := json.Unmarshal(body, &response); err != nil {
			return errMsg{err}
		}

		// Convert to spanDetail
		spans := make([]spanDetail, len(response.Traces))
		for i, span := range response.Traces {
			duration, _ := time.ParseDuration(span.Duration)
			spans[i] = spanDetail{
				SpanID:       span.SpanID,
				ParentSpanID: span.ParentSpanID,
				Name:         span.Name,
				Kind:         span.Kind,
				StartTime:    span.StartTime,
				EndTime:      span.EndTime,
				Duration:     duration,
				Status:       span.Status,
				StatusCode:   span.StatusCode,
				ServiceName:  span.ServiceName,
				Attributes:   span.Attributes,
			}
		}

		return traceSpansMsg(spans)
	}
}

func fetchTraceLogs(apiURL, traceID string) tea.Cmd {
	return func() tea.Msg {
		resp, err := apiClient.Get(apiURL + "/traces/" + url.PathEscape(traceID) + "/logs")
		if err != nil {
			return errMsg{err}
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return errMsg{err}
		}

		// Parse the response
		var response struct {
			TraceID string     `json:"trace_id"`
			Logs    []logEntry `json:"logs"`
			Count   int        `json:"count"`
		}

		if err := json.Unmarshal(body, &response); err != nil {
			return errMsg{err}
		}

		return traceLogsMsg(response.Logs)
	}
}

func initialModel(apiURL string, manager *process.Manager) model {
	ti := textinput.New()
	ti.Placeholder = "search..."
	ti.Focus()

	return model{
		apiURL:         apiURL,
		sources:        []string{},
		sourceTypes:    make(map[string]string),
		selectedSource: 0,
		logs:           []logEntry{},
		width:          80,
		height:         24,
		manager:        manager,
		scrollOffset:   0,
		autoScroll:     true,
		mode:           ModeNormal,
		searchInput:    ti,
		searchQuery:    "",
		searchMatchIdx: 0,
		showTraceIDs:   true, // Show trace indicators by default

		// Trace view state
		traces:            []traceSummary{},
		traceScrollOffset: 0,
		selectedTraceIdx:  0,

		// Trace detail view state
		selectedTraceID:         "",
		traceSpans:              []spanDetail{},
		traceLogs:               []logEntry{},
		traceDetailScrollOffset: 0,
		tickCount:               0,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		fetchSources(m.apiURL),
		fetchStartup(m.apiURL),
		tickCmd(),
	)
}

// Update is a recovering wrapper around updateModel.
//
// Bubble Tea would otherwise catch a panic here and end the session; recovering
// first means a bad message degrades into a footer warning and a crash-log
// entry. The model returned on panic is the one from *before* the message, so a
// message that corrupts state is discarded rather than kept.
func (m model) Update(msg tea.Msg) (next tea.Model, cmd tea.Cmd) {
	defer func() {
		if r := recover(); r != nil {
			m.lastPanic = recordPanic("Update", r, debug.Stack())
			next, cmd = m, nil
		}
	}()
	return m.updateModel(msg)
}

func (m model) updateModel(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Global quit works in any mode
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

		// Route to mode-specific handler
		wasTailing := m.tailing()
		var next tea.Model
		var cmd tea.Cmd
		switch m.mode {
		case ModeSearch:
			next, cmd = m.updateSearchMode(msg)
		case ModeNormal, ModeTraceDetail:
			next, cmd = m.updateNormalMode(msg)
		default:
			return m, nil
		}

		// Leaving the live tail -- scrolling up, or starting a search -- needs
		// the whole buffer, and now rather than on the next tick: until it
		// arrives, history stops at the live window and search counts only
		// what is loaded.
		if nm, ok := next.(model); ok && wasTailing && !nm.tailing() {
			if source := nm.currentSource(); source != "" && source != "Traces" && source != startupTab {
				cmd = tea.Batch(cmd, fetchLogs(nm.apiURL, source, 0))
			}
		}
		return next, cmd

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case sourcesMsg:
		m.sourceTypes = msg.types
		m.sources = sortSourcesWithTypes(msg.names, msg.types)
		// Add "Traces" as a special tab at the end
		m.sources = append(m.sources, "Traces")
		if m.sourceTypes == nil {
			m.sourceTypes = make(map[string]string)
		}
		m.sourceTypes["Traces"] = "traces"
		// And the startup screen first, once there is one. Always first, so
		// the selected index means the same tab from one refresh to the next.
		if m.startupActive {
			m.sources = withStartupTab(m.sources)
			m.sourceTypes[startupTab] = "startup"
		}
		// The list was just replaced and may be shorter than before.
		m.clampSelectedSource()
		return m.fetchForSelectedSource()

	case startupMsg:
		return m.applyStartup(msg)

	case logsMsg:
		// Stale replies are dropped rather than applied: one for a tab that is
		// no longer selected, or a live-window one when the view now wants the
		// whole buffer.
		if msg.source != m.currentSource() || (msg.limited && !m.tailing()) {
			return m, nil
		}
		m.logs = msg.logs
		m.refreshMatches()
		// More history may have loaded above. Pinned to the top stays at the
		// top; otherwise the offset from the bottom is kept, and clamped.
		if m.pinnedTop && !m.autoScroll {
			m.scrollOffset = m.maxScroll()
		}
		m.scrollOffset = min(m.scrollOffset, m.maxScroll())
		// Reset scroll offset when auto-scroll is enabled (user is at bottom)
		if m.autoScroll {
			m.scrollOffset = 0
		}

	case restartMsg:
		if msg.err != nil {
			m.notice = fmt.Sprintf("restart %s failed: %v", msg.process, msg.err)
		} else {
			m.notice = fmt.Sprintf("restarted %s", msg.process)
		}

	case tracesMsg:
		m.traces = msg
		// Reset trace scroll offset
		m.traceScrollOffset = 0
		// Same hazard as the sources list: spans age out past max_span_age, so
		// this list shrinks on its own and the selection can be left past the
		// end.
		m.clampSelectedTrace()

	case traceSpansMsg:
		m.traceSpans = msg

	case traceLogsMsg:
		m.traceLogs = msg

	case tickMsg:
		m.tickCount++

		// Fetch sources on every tick to detect new processes quickly
		// This ensures tabs appear as soon as processes start logging
		cmds := []tea.Cmd{tickCmd(), fetchSources(m.apiURL)}

		if len(m.sources) > 0 {
			_, cmd := m.fetchForSelectedSource()
			cmds = append(cmds, cmd)
		}

		return m, tea.Batch(cmds...)

	case errMsg:
		m.err = msg.err
	}

	return m, nil
}

// View is a recovering wrapper around viewModel.
//
// A panic while rendering is the most likely kind here -- it is where the width
// and height arithmetic lives -- and it would otherwise kill the program on the
// next frame after a resize. A fallback frame keeps the session alive and says
// where to look.
func (m model) View() (out string) {
	defer func() {
		if r := recover(); r != nil {
			summary := recordPanic("View", r, debug.Stack())
			out = errorStyle.Render(fmt.Sprintf(
				"Could not draw this frame: %s\n\nThe details are in %s\n"+
					"Try resizing the window, or press q to quit.",
				summary, crashLogName))
		}
	}()
	return m.viewModel()
}

func (m model) viewModel() string {
	if m.err != nil {
		return errorStyle.Render(fmt.Sprintf("Error: %v\n\nPress q to quit", m.err))
	}

	// Built once and measured once; the scroll handlers use the same numbers.
	c := m.chrome()
	header, searchBar, help := c.header, c.searchBar, c.help
	contentWidth := m.contentWidth()
	// Measured from the chrome just built. contentHeight() would build it a
	// second time -- a regression from #38 that doubled the cost of the header
	// and footer on every frame. The paging handlers still use contentHeight(),
	// and both come from chromeRows.height, so they cannot disagree.
	contentHeight := max(0, m.height-c.height())

	// Render content based on mode
	var content string
	switch m.mode {
	case ModeTraceDetail:
		// Render trace detail view
		content = renderTraceDetail(m.selectedTraceID, m.traceSpans, m.traceLogs, contentHeight, contentWidth, m.traceDetailScrollOffset)
	case ModeNormal:
		// Check if we're in Traces tab
		if m.currentSource() == "Traces" {
			// Render trace list
			content = renderTraceList(m.traces, contentHeight, contentWidth, m.traceScrollOffset, m.selectedTraceIdx)
		} else if m.currentSource() == startupTab {
			content = renderStartup(m.startupRows, contentHeight, contentWidth)
		} else {
			// Render logs with search highlighting and current match index
			content = renderLogWindow(m.logs, contentHeight, contentWidth, m.scrollOffset, m.searchQuery, m.searchMatchIdx, m.showTraceIDs, m.matches())
		}
	default:
		// For search mode or others, render logs
		content = renderLogWindow(m.logs, contentHeight, contentWidth, m.scrollOffset, m.searchQuery, m.searchMatchIdx, m.showTraceIDs, m.matches())
	}

	// Add neutral grey border around content (matches header borders)
	// No top border to connect with active tab
	if len(m.sources) > 0 {
		contentStyle := lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderTop(false).                        // No top border to connect with active tab
			BorderForeground(lipgloss.Color("240")). // Neutral grey (matches header)
			Width(m.width - 2)                       // Borders are added OUTSIDE width

		content = contentStyle.Render(content)
	}

	// Only include searchBar when it's not empty
	components := []string{header}
	if searchBar != "" {
		components = append(components, searchBar)
	}
	components = append(components, content, help)
	return lipgloss.JoinVertical(lipgloss.Left, components...)
}

func renderTraceList(traces []traceSummary, height, width, scrollOffset, selectedIdx int) string {
	if height <= 0 || width <= 0 {
		return logStyle.Render("Invalid terminal dimensions")
	}

	if len(traces) == 0 {
		return logStyle.Render("No traces yet...")
	}

	// Calculate column widths
	// Trace ID: 20 chars max (same as in log view)
	// Duration: 12 chars (e.g., "1.23456789s")
	// Status: 8 chars
	// Services: remaining width
	traceIDWidth := 20
	durationWidth := 12
	statusWidth := 8
	servicesWidth := width - traceIDWidth - durationWidth - statusWidth - 6 // 6 for spacing

	if servicesWidth < 10 {
		servicesWidth = 10
		traceIDWidth = width - durationWidth - statusWidth - servicesWidth - 6
		if traceIDWidth < 10 {
			traceIDWidth = 10
		}
	}

	// Create header
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("15")).
		Background(lipgloss.Color("236"))

	header := headerStyle.Render(fmt.Sprintf("%-*s  %-*s  %-*s  %-*s",
		traceIDWidth, "Trace ID",
		durationWidth, "Duration",
		statusWidth, "Status",
		servicesWidth, "Services"))

	// Collect all lines
	allLines := []string{header}

	for i, trace := range traces {
		// Truncate trace ID if needed
		displayTraceID := truncate(trace.TraceID, traceIDWidth)

		// Format duration
		durationStr := trace.Duration.String()
		durationStr = truncate(durationStr, durationWidth)

		// Format status
		statusStr := trace.Status
		statusStr = truncate(statusStr, statusWidth)

		// Format services (comma-separated)
		servicesStr := strings.Join(trace.Services, ", ")
		servicesStr = truncate(servicesStr, servicesWidth)

		// Apply selection style
		lineStyle := logStyle
		if i == selectedIdx {
			lineStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("15")). // White
				Background(lipgloss.Color("57"))  // Purple
		} else if trace.Status == "ERROR" {
			lineStyle = errorLogStyle
		}

		line := fmt.Sprintf("%-*s  %-*s  %-*s  %-*s",
			traceIDWidth, displayTraceID,
			durationWidth, durationStr,
			statusWidth, statusStr,
			servicesWidth, servicesStr)

		allLines = append(allLines, lineStyle.Render(line))
	}

	// Handle scrolling with padding
	totalLines := len(allLines)

	// Helper function to pad lines to exact height
	padLines := func(lines []string, height int) string {
		if len(lines) >= height {
			return lipgloss.JoinVertical(lipgloss.Left, lines[:height]...)
		}
		// Pad with empty lines
		paddedLines := make([]string, height)
		copy(paddedLines, lines)
		for i := len(lines); i < height; i++ {
			paddedLines[i] = logStyle.Render("")
		}
		return lipgloss.JoinVertical(lipgloss.Left, paddedLines...)
	}

	if totalLines <= height {
		return padLines(allLines, height)
	}

	// Calculate start index based on scrollOffset
	// scrollOffset = 0 means show top
	startIdx := scrollOffset
	endIdx := startIdx + height

	// Clamp to valid ranges
	if startIdx < 0 {
		startIdx = 0
		endIdx = height
	}
	if endIdx > totalLines {
		endIdx = totalLines
		startIdx = endIdx - height
		if startIdx < 0 {
			startIdx = 0
		}
	}

	return padLines(allLines[startIdx:endIdx], height)
}

func renderTraceDetail(traceID string, spans []spanDetail, logs []logEntry, height, width, scrollOffset int) string {
	if height <= 0 || width <= 0 {
		return logStyle.Render("Invalid terminal dimensions")
	}

	if traceID == "" {
		return logStyle.Render("No trace selected")
	}

	// Build trace summary from spans
	var traceDuration time.Duration
	var traceStatus string
	services := make(map[string]bool)
	var startTime time.Time

	for _, span := range spans {
		if span.Duration > traceDuration {
			traceDuration = span.Duration
		}
		if span.Status == "ERROR" {
			traceStatus = "ERROR"
		}
		if span.ServiceName != "" {
			services[span.ServiceName] = true
		}
		if startTime.IsZero() || span.StartTime.Before(startTime) {
			startTime = span.StartTime
		}
	}

	if traceStatus == "" {
		traceStatus = "OK"
	}

	// Build services list
	serviceList := make([]string, 0, len(services))
	for service := range services {
		serviceList = append(serviceList, service)
	}
	sort.Strings(serviceList)

	// Create header
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("15")).
		Background(lipgloss.Color("57"))

	header := headerStyle.Render(fmt.Sprintf(" Trace: %s ", traceID))

	// Create trace info
	infoStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("250"))

	infoLines := []string{
		fmt.Sprintf("Duration: %s | Status: %s | Spans: %d | Services: %s",
			traceDuration, traceStatus, len(spans), strings.Join(serviceList, ", ")),
		"",
	}

	// Build span tree
	if len(spans) > 0 {
		infoLines = append(infoLines, "Spans:")
		infoLines = append(infoLines, renderSpanTree(spans, width))
		infoLines = append(infoLines, "")
	}

	// Add correlated logs
	if len(logs) > 0 {
		infoLines = append(infoLines, fmt.Sprintf("Correlated Logs (%d):", len(logs)))
		for _, log := range logs {
			line := fmt.Sprintf("[%s] [%s] %s",
				clockTime(log.Timestamp), log.Level, parser.SanitiseLine(log.Message))
			line = truncate(line, width-2)
			infoLines = append(infoLines, line)
		}
	} else {
		infoLines = append(infoLines, "No correlated logs")
	}

	// Apply styles to all lines
	allLines := []string{header}
	for _, line := range infoLines {
		allLines = append(allLines, infoStyle.Render(line))
	}

	// Handle scrolling with padding
	totalLines := len(allLines)

	// Helper function to pad lines to exact height
	padLines := func(lines []string, height int) string {
		if len(lines) >= height {
			return lipgloss.JoinVertical(lipgloss.Left, lines[:height]...)
		}
		// Pad with empty lines
		paddedLines := make([]string, height)
		copy(paddedLines, lines)
		for i := len(lines); i < height; i++ {
			paddedLines[i] = infoStyle.Render("")
		}
		return lipgloss.JoinVertical(lipgloss.Left, paddedLines...)
	}

	if totalLines <= height {
		return padLines(allLines, height)
	}

	// Calculate start index based on scrollOffset
	startIdx := scrollOffset
	endIdx := startIdx + height

	// Clamp to valid ranges
	if startIdx < 0 {
		startIdx = 0
		endIdx = height
	}
	if endIdx > totalLines {
		endIdx = totalLines
		startIdx = endIdx - height
		if startIdx < 0 {
			startIdx = 0
		}
	}

	return padLines(allLines[startIdx:endIdx], height)
}

// renderSpanTree builds and renders an ASCII tree of spans
func renderSpanTree(spans []spanDetail, width int) string {
	// Build parent-child relationships
	children := make(map[string][]spanDetail)
	rootSpans := []spanDetail{}

	for _, span := range spans {
		if span.ParentSpanID == "" {
			rootSpans = append(rootSpans, span)
		} else {
			children[span.ParentSpanID] = append(children[span.ParentSpanID], span)
		}
	}

	// Sort root spans by start time
	sort.Slice(rootSpans, func(i, j int) bool {
		return rootSpans[i].StartTime.Before(rootSpans[j].StartTime)
	})

	// Render tree recursively
	var lines []string
	for i, span := range rootSpans {
		isLast := i == len(rootSpans)-1
		renderSpanNode(span, children, "", isLast, &lines, width)
	}

	return strings.Join(lines, "\n")
}

// renderSpanNode renders a span and its children recursively
func renderSpanNode(span spanDetail, children map[string][]spanDetail, prefix string, isLast bool, lines *[]string, width int) {
	// Current node prefix
	var nodePrefix string
	if prefix == "" {
		nodePrefix = ""
	} else if isLast {
		nodePrefix = prefix + "└── "
	} else {
		nodePrefix = prefix + "├── "
	}

	// Format span info
	statusSymbol := "✓"
	if span.Status == "ERROR" {
		statusSymbol = "✗"
	}

	spanInfo := fmt.Sprintf("%s %s (%s) %s", statusSymbol, span.Name, span.Duration, span.ServiceName)

	// Truncate if needed
	maxLineWidth := width - displayWidth(nodePrefix) - 2
	spanInfo = truncate(spanInfo, maxLineWidth)

	*lines = append(*lines, nodePrefix+spanInfo)

	// Render children
	childSpans := children[span.SpanID]
	if len(childSpans) > 0 {
		// Sort children by start time
		sort.Slice(childSpans, func(i, j int) bool {
			return childSpans[i].StartTime.Before(childSpans[j].StartTime)
		})

		// New prefix for children
		newPrefix := prefix
		if isLast {
			newPrefix += "    "
		} else {
			newPrefix += "│   "
		}

		for i, child := range childSpans {
			isChildLast := i == len(childSpans)-1
			renderSpanNode(child, children, newPrefix, isChildLast, lines, width)
		}
	}
}

// sortSourcesWithTypes sorts sources by type (running-man, docker, process) then alphabetically.
func sortSourcesWithTypes(sources []string, sourceTypes map[string]string) []string {
	// Group by type
	runningMan := []string{}
	docker := []string{}
	processes := []string{}
	unknown := []string{}

	for _, source := range sources {
		sourceType := sourceTypes[source]
		switch sourceType {
		case "system":
			runningMan = append(runningMan, source)
		case "docker":
			docker = append(docker, source)
		case "process", "otlp":
			// otlp sources are log records POSTed to the OTLP receiver (e.g. from a
			// browser, which has no stdout). They are application output like any
			// process, so group them alongside processes rather than as unknown.
			processes = append(processes, source)
		default:
			unknown = append(unknown, source)
		}
	}

	// Sort each group alphabetically
	sort.Strings(runningMan)
	sort.Strings(docker)
	sort.Strings(processes)
	sort.Strings(unknown)

	// Combine in order: running-man, docker, processes, unknown
	result := []string{}
	result = append(result, runningMan...)
	result = append(result, docker...)
	result = append(result, processes...)
	result = append(result, unknown...)

	return result
}

// isDockerContainer checks if a source is a Docker container using source type information.
func isDockerContainer(name string, sourceTypes map[string]string) bool {
	if sourceType, ok := sourceTypes[name]; ok {
		return sourceType == "docker"
	}
	return false
}

func renderHeader(sources []string, selected int, width int, sourceTypes map[string]string) string {
	if len(sources) == 0 {
		return headerStyle.Render("Loading sources...")
	}

	// Determine active tab color (distinct, readable colors)
	var activeTabColor lipgloss.Color
	if selected < len(sources) {
		source := sources[selected]
		if source == "running-man" {
			activeTabColor = lipgloss.Color("33") // Dodger blue
		} else if source == startupTab {
			activeTabColor = lipgloss.Color("172") // Amber
		} else if source == "Traces" {
			activeTabColor = lipgloss.Color("127") // Medium purple
		} else if isDockerContainer(source, sourceTypes) {
			activeTabColor = lipgloss.Color("70") // Medium sea green (docker)
		} else {
			activeTabColor = lipgloss.Color("208") // Orange (processes - distinct from docker)
		}
	} else {
		activeTabColor = lipgloss.Color("33") // Default blue
	}

	// All borders use neutral grey color
	neutralBorderColor := lipgloss.Color("240") // Dark grey

	// Build tabs with neutral borders, colored content
	tabs := []string{}
	for i, source := range sources {
		// Determine style based on source group
		var normalStyle, selectedStyle lipgloss.Style

		if source == "running-man" {
			normalStyle = runningManTabStyle
			selectedStyle = runningManSelectedTabStyle
		} else if source == "Traces" || source == startupTab {
			normalStyle = tracesTabStyle
			selectedStyle = tracesSelectedTabStyle
		} else if isDockerContainer(source, sourceTypes) {
			normalStyle = dockerTabStyle
			selectedStyle = dockerSelectedTabStyle
		} else {
			normalStyle = processTabStyle
			selectedStyle = processSelectedTabStyle
		}

		// Use selected style if this is the active tab
		style := normalStyle
		if i == selected {
			style = selectedStyle
			// DEBUG: Try bright white text instead of black for better contrast
			style = style.
				Background(activeTabColor).
				Foreground(lipgloss.Color("15")). // Bright white (instead of black)
				BorderForeground(neutralBorderColor)
		} else {
			// Inactive tab: background = black, text = bright white, borders = neutral
			style = style.
				Background(lipgloss.Color("0")).  // Black
				Foreground(lipgloss.Color("15")). // Bright white
				BorderForeground(neutralBorderColor)
		}

		// Add emoji prefix - use variation selector for gear
		var displayName string
		if source == "running-man" {
			displayName = "🏃‍➡️  " + source // Running man facing right
		} else if source == "Traces" {
			displayName = "🔍  " + source // 2 spaces after 2-column emoji
		} else if source == startupTab {
			displayName = "🚦  " + source // 2 spaces after 2-column emoji
		} else if isDockerContainer(source, sourceTypes) {
			displayName = "🐳  " + source // 2 spaces after 2-column emoji
		} else {
			displayName = "🔧 " + source // Wrench emoji (2 columns, consistent)
		}

		tabs = append(tabs, style.Render(fmt.Sprintf(" %s ", displayName)))
	}

	// Shift tabs right by 2 spaces
	leftMargin := 2

	// Only the tabs that fit. The row was previously built from all of them at
	// full size, so three tabs came to 57 columns whatever the terminal was --
	// and lipgloss then padded every other row of the frame to match, which is
	// how a narrow window ended up with every row wider than the screen.
	const cornerAndGap = 4 // "┌──" on the left, "┐" on the right
	tabs = tabsThatFit(tabs, selected, width-cornerAndGap)

	// Join tabs horizontally at top
	row := lipgloss.JoinHorizontal(lipgloss.Top, tabs...)

	// Create gap style with neutral border color
	gapStyle := lipgloss.NewStyle().
		Border(tabBorder, false, false, true, false). // Only bottom border
		BorderForeground(neutralBorderColor)

	// Left gap: corner + border line "┌──"
	// This shows where content box corner would be
	leftGapStyle := lipgloss.NewStyle().
		Foreground(neutralBorderColor)
	leftGap := leftGapStyle.Render("┌" + strings.Repeat("─", leftMargin))

	// Right gap (after tabs) - fills remaining space with bottom border
	rightGapWidth := max(0, width-lipgloss.Width(row)-lipgloss.Width(leftGap)-1) // -1 for corner
	rightGap := gapStyle.Render(strings.Repeat(" ", rightGapWidth))

	// Add corner at the end
	cornerStyle := lipgloss.NewStyle().
		Foreground(neutralBorderColor)
	corner := cornerStyle.Render("┐")

	// Join left gap + tabs + right gap + corner with Bottom alignment
	header := lipgloss.JoinHorizontal(lipgloss.Bottom, leftGap, row, rightGap, corner)

	// Clamp every row as a backstop. The arithmetic above should already fit,
	// but a header one column too wide makes lipgloss pad the whole frame to
	// match and the terminal wrap all of it, so this is worth not relying on
	// arithmetic for.
	return clampRows(header, width)
}

// tabsThatFit returns the tabs that will fit in the budget, keeping the selected
// one and preferring its neighbours, in their original order.
//
// The selected tab is kept even when it alone does not fit, truncated instead:
// knowing which source is being shown matters more than the border drawing
// correctly at a width nobody can read anyway.
func tabsThatFit(tabs []string, selected, budget int) []string {
	if len(tabs) == 0 || budget <= 0 {
		return nil
	}
	if selected < 0 || selected >= len(tabs) {
		selected = 0
	}

	total := 0
	for _, t := range tabs {
		total += lipgloss.Width(t)
	}
	if total <= budget {
		return tabs
	}

	used := lipgloss.Width(tabs[selected])
	if used > budget {
		return []string{clampRows(tabs[selected], budget)}
	}

	// Grow outwards from the selection so context on both sides is kept where
	// there is room for it.
	lo, hi := selected, selected
	for {
		grew := false
		if hi+1 < len(tabs) && used+lipgloss.Width(tabs[hi+1]) <= budget {
			hi++
			used += lipgloss.Width(tabs[hi])
			grew = true
		}
		if lo-1 >= 0 && used+lipgloss.Width(tabs[lo-1]) <= budget {
			lo--
			used += lipgloss.Width(tabs[lo])
			grew = true
		}
		if !grew {
			break
		}
	}
	return tabs[lo : hi+1]
}

// clampRows truncates every row of a multi-row block to width.
func clampRows(block string, width int) string {
	rows := strings.Split(block, "\n")
	for i, row := range rows {
		rows[i] = truncate(row, width)
	}
	return strings.Join(rows, "\n")
}

func renderLogs(logs []logEntry, height, width, scrollOffset int, searchQuery string, currentMatchIdx int, showTraceIDs bool) string {
	return renderLogWindow(logs, height, width, scrollOffset, searchQuery, currentMatchIdx,
		showTraceIDs, buildMatchLineIndex(logs, searchQuery))
}

// renderLogWindow draws the visible window of the log view.
//
// It formats only the lines it will show. It used to format and style every
// line of every entry and then slice the window out at the end, so a frame cost
// grew with the whole buffer rather than the screen: 11.9ms and 123k
// allocations at the default 10,000 entries to draw about 45 rows, and Bubble
// Tea calls View after every keypress, tick and fetch.
//
// Line structure comes from the raw message, split on newlines, with each line
// sanitised on its own. That keeps the count here identical to the one
// buildMatchLineIndex uses: sanitising a whole message could swallow a newline
// inside a malformed escape sequence, and the window and the match positions
// would then disagree about which line is which.
//
// matchIndex is the model's stored index, so the match offset for a line is a
// binary search rather than a running count over everything above it.
func renderLogWindow(logs []logEntry, height, width, scrollOffset int, searchQuery string, currentMatchIdx int, showTraceIDs bool, matchIndex []int) string {
	if height <= 0 || width <= 0 {
		return logStyle.Render("Invalid terminal dimensions")
	}
	if scrollOffset < 0 {
		scrollOffset = 0
	}
	if len(logs) == 0 {
		return logStyle.Render("No logs yet...")
	}

	// Pass 1: how many display lines there are. A newline count per entry, no
	// formatting.
	totalLines := 0
	for i := range logs {
		totalLines += entryLines(logs[i])
	}
	startIdx, endIdx := logWindow(totalLines, height, scrollOffset)

	// Pass 2: format only what is inside the window.
	lines := make([]string, 0, endIdx-startIdx)
	lineIdx := 0
	for _, log := range logs {
		n := entryLines(log)
		if lineIdx+n <= startIdx {
			lineIdx += n
			continue
		}
		if lineIdx >= endIdx {
			break
		}

		style := logStyle
		if log.Level == "error" {
			style = errorLogStyle
		}
		for i, raw := range strings.Split(log.Message, "\n") {
			if lineIdx >= startIdx && lineIdx < endIdx {
				// Sanitised again at the point of drawing, a line at a time.
				// Entries are cleaned on capture; this is the last line of
				// defence for the symptom that started this, one escape
				// sequence reaching the terminal and clearing the screen.
				line := formatLogLine(log, i, parser.SanitiseLine(raw), width, showTraceIDs)
				if searchQuery != "" {
					line = highlightMatchesWithCurrent(line, searchQuery,
						sort.SearchInts(matchIndex, lineIdx), currentMatchIdx)
				}
				lines = append(lines, style.Render(line))
			}
			lineIdx++
		}
	}

	// Bottom-aligned: fewer lines than the window are padded above, so the
	// newest output sits at the bottom and stale content is cleared.
	if pad := height - len(lines); pad > 0 {
		padded := make([]string, height)
		for i := 0; i < pad; i++ {
			padded[i] = logStyle.Render("")
		}
		copy(padded[pad:], lines)
		lines = padded
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// entryLines is how many display lines an entry occupies.
func entryLines(log logEntry) int {
	return 1 + strings.Count(log.Message, "\n")
}

// logWindow is the [start, end) range of display lines to show, given how far
// the view is scrolled up from the bottom. Scrolled past the top, it shows the
// oldest lines rather than fewer.
func logWindow(totalLines, height, scrollOffset int) (int, int) {
	if totalLines <= height {
		return 0, totalLines
	}
	end := totalLines - scrollOffset
	if end < height {
		return 0, height
	}
	if end > totalLines {
		end = totalLines
	}
	return end - height, end
}

// formatLogLine renders one display line of an entry: the timestamp, level and
// optional trace indicator on its first line, an indent on the rest, fitted to
// the width.
func formatLogLine(log logEntry, i int, msgLine string, width int, showTraceIDs bool) string {
	if i > 0 {
		// Continuation lines get indented. -2 for the box's borders.
		return truncate("                    "+msgLine, width-2)
	}

	baseLine := fmt.Sprintf("[%s] [%s] %s", clockTime(log.Timestamp), log.Level, msgLine)

	if showTraceIDs && log.TraceID != "" {
		displayTraceID := truncate(log.TraceID, maxTraceIDDisplayLength)
		// The styled indicator's own width, since the style may add escape
		// sequences that occupy no columns.
		indicator := traceIndicatorStyle.Render(fmt.Sprintf("[trace:%s]", displayTraceID))

		// The indicator only earns its space if the message still has some. A
		// 40-column window was spending 22 of them on a truncated trace ID and
		// showing "[09:15:03] [er..." for the message, which is the wrong way
		// round: the trace ID is recoverable from the entry, the message is why
		// anyone is looking.
		if width-displayWidth(indicator)-1 >= minMessageWidth {
			return truncate(baseLine, width-displayWidth(indicator)-1) + " " + indicator
		}
	}

	// -2 for the box's left and right borders.
	return truncate(baseLine, width-2)
}

// buildMatchLineIndex returns a slice where each element is the rendered-line index
// (in the flat allLines array that renderLogs would produce) for each global match
// occurrence of query across all logs. Used to compute scrollOffset for n/p navigation.
// buildMatchLineIndex returns the display-line index of every search match.
//
// Deliberately takes no width. It used to truncate each line to the terminal
// width before searching it, which had three consequences: a match beyond the
// cut was never found, the result disagreed with countMatches (which passed a
// huge width to disable truncation) and with the renderer (which was given
// width-2), and the truncation itself panicked on a narrow window. Truncation
// cannot change how many display lines an entry occupies -- only the newlines in
// its message can -- so the width was never relevant to the answer.
//
// A match past the visible edge of a long line is therefore counted and can be
// jumped to, but will not be visibly highlighted. That is the better way round:
// the alternative was not finding it at all.
func buildMatchLineIndex(logs []logEntry, query string) []int {
	if query == "" {
		return nil
	}
	lowerQuery := strings.ToLower(query)
	var result []int
	lineIdx := 0

	for _, log := range logs {
		messageLines := strings.Split(log.Message, "\n")

		for i, msgLine := range messageLines {
			var line string
			if i == 0 {
				line = fmt.Sprintf("[%s] [%s] %s", clockTime(log.Timestamp), log.Level, msgLine)
			} else {
				line = fmt.Sprintf("                    %s", msgLine)
			}

			lowerLine := strings.ToLower(line)
			count := strings.Count(lowerLine, lowerQuery)
			for k := 0; k < count; k++ {
				result = append(result, lineIdx)
			}
			lineIdx++
		}
	}

	return result
}

// scrollOffsetForMatch computes the scrollOffset needed to center rendered line
// targetLineIdx within a viewport of the given height, given totalLines total
// rendered lines.
func scrollOffsetForMatch(targetLineIdx, totalLines, height int) int {
	// Place the target line in the middle of the viewport.
	// The viewport's last visible line is at: totalLines - scrollOffset - 1
	// so: scrollOffset = totalLines - targetLineIdx - 1 - height/2
	offset := totalLines - targetLineIdx - 1 - height/2
	if offset < 0 {
		offset = 0
	}
	return offset
}

// countAllLines returns the total number of rendered lines that logs would produce,
// using the same logic as renderLogs / buildMatchLineIndex.
func countAllLines(logs []logEntry, width int) int {
	count := 0
	for _, log := range logs {
		count += len(strings.Split(log.Message, "\n"))
	}
	return count
}

// highlightMatchesWithCurrent highlights all occurrences of query in line.
// The occurrence whose global index equals currentMatchIdx gets a bold+inverted style
// (the "current" match); all others get a dim style.
// lineMatchOffset is the global index of the first match on this line.
// Pass currentMatchIdx = -1 to use uniform dim highlighting for all occurrences.
func highlightMatchesWithCurrent(line, query string, lineMatchOffset, currentMatchIdx int) string {
	if query == "" || len(line) == 0 {
		return line
	}

	lowerQuery := strings.ToLower(query)
	lowerLine := strings.ToLower(line)

	if len(lowerQuery) > len(lowerLine) {
		return line
	}

	currentStyle := lipgloss.NewStyle().
		Bold(true).
		Reverse(true) // inverted fg/bg — stands out clearly

	otherStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("240")). // dark gray
		Foreground(lipgloss.Color("15"))   // white

	idx := 0
	pos := 0
	result := ""
	matchOnLine := 0

	for {
		matchIdx := strings.Index(lowerLine[idx:], lowerQuery)
		if matchIdx == -1 {
			result += line[pos:]
			break
		}

		beforeMatch := pos + matchIdx
		if beforeMatch > len(line) {
			result += line[pos:]
			break
		}
		result += line[pos:beforeMatch]

		matchStart := beforeMatch
		matchEnd := matchStart + len(lowerQuery)
		if matchEnd > len(line) {
			matchEnd = len(line)
		}

		globalIdx := lineMatchOffset + matchOnLine
		var style lipgloss.Style
		if currentMatchIdx >= 0 && globalIdx == currentMatchIdx {
			style = currentStyle
		} else {
			style = otherStyle
		}
		result += style.Render(line[matchStart:matchEnd])

		pos = matchEnd
		idx = matchIdx + len(lowerQuery)
		matchOnLine++
		if pos >= len(line) {
			break
		}
	}

	return result
}

// Commands
func fetchSources(apiURL string) tea.Cmd {
	return func() tea.Msg {
		resp, err := apiClient.Get(apiURL + "/health")
		if err != nil {
			return errMsg{err}
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return errMsg{err}
		}

		var health healthResponse
		if err := json.Unmarshal(body, &health); err != nil {
			return errMsg{err}
		}

		sources := make([]string, len(health.Sources))
		sourceTypes := make(map[string]string)
		for i, s := range health.Sources {
			sources[i] = s.Name
			sourceTypes[s.Name] = s.Type
		}

		return sourcesMsg{names: sources, types: sourceTypes}
	}
}

// fetchLogs requests one source's logs. A limit of zero fetches everything.
//
// The limit is always sent, zero included. /logs caps an unqualified request
// at DefaultLogLimit (1,000), so leaving it off does not mean "everything" --
// which is how the TUI's scroll-back and search were silently confined to the
// newest 1,000 entries of a 10,000-entry buffer.
func fetchLogs(apiURL, source string, limit int) tea.Cmd {
	return func() tea.Msg {
		// Escaped: process names are validated, but an OTLP service name is
		// whatever the sender chose, and "my app" or a stray & or # used to
		// produce a broken request -- that source's tab simply never loaded.
		query := url.Values{"source": {source}, "limit": {strconv.Itoa(limit)}}
		endpoint := apiURL + "/logs?" + query.Encode()
		resp, err := apiClient.Get(endpoint)
		if err != nil {
			return errMsg{err}
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return errMsg{err}
		}

		var logsResp logsResponse
		if err := json.Unmarshal(body, &logsResp); err != nil {
			return errMsg{err}
		}

		return logsMsg{source: source, limited: limit > 0, logs: logsResp.Logs}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(defaultPollInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func countMatches(logs []logEntry, query string) int {
	// Delegates to buildMatchLineIndex so the count and the positions can never
	// disagree -- they used to, by being given different widths.
	return len(buildMatchLineIndex(logs, query))
}

// Styles
var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("57"))

	// Tab border definitions from lipgloss example (using NormalBorder characters)
	activeTabBorder = lipgloss.Border{
		Top:         "─",
		Bottom:      " ", // Space to connect with content box
		Left:        "│",
		Right:       "│",
		TopLeft:     "┌",
		TopRight:    "┐",
		BottomLeft:  "┘",
		BottomRight: "└",
	}

	tabBorder = lipgloss.Border{
		Top:         "─",
		Bottom:      "─",
		Left:        "│",
		Right:       "│",
		TopLeft:     "┌",
		TopRight:    "┐",
		BottomLeft:  "┴",
		BottomRight: "┴",
	}

	// Base tab style
	baseTabStyle = lipgloss.NewStyle().
			Border(tabBorder, true).
			Padding(0, 1)

	// Tab styles with different border colors
	runningManTabStyle = baseTabStyle.
				BorderForeground(lipgloss.Color("39")). // Blue
				Foreground(lipgloss.Color("15")).       // White
				Background(lipgloss.Color("236"))       // Dark gray

	runningManSelectedTabStyle = runningManTabStyle.
					Bold(true).
					Background(lipgloss.Color("#000000")). // True black
					Border(activeTabBorder, true)

	// Docker tabs - Green
	dockerTabStyle = baseTabStyle.
			BorderForeground(lipgloss.Color("42")). // Green
			Foreground(lipgloss.Color("15")).       // White
			Background(lipgloss.Color("236"))       // Dark gray

	dockerSelectedTabStyle = dockerTabStyle.
				Bold(true).
				Background(lipgloss.Color("0")). // Black
				Border(activeTabBorder, true)

	// Process tabs - Orange (distinct from docker green)
	processTabStyle = baseTabStyle.
			BorderForeground(lipgloss.Color("208")). // Orange
			Foreground(lipgloss.Color("15")).        // White
			Background(lipgloss.Color("236"))        // Dark gray

	processSelectedTabStyle = processTabStyle.
				Bold(true).
				Background(lipgloss.Color("0")). // Black
				Border(activeTabBorder, true)

	// Traces tabs - Purple
	tracesTabStyle = baseTabStyle.
			BorderForeground(lipgloss.Color("93")). // Purple
			Foreground(lipgloss.Color("15")).       // White
			Background(lipgloss.Color("236"))       // Dark gray

	tracesSelectedTabStyle = tracesTabStyle.
				Bold(true).
				Background(lipgloss.Color("0")). // Black
				Border(activeTabBorder, true)

	logStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	errorLogStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("203"))

	traceIndicatorStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("33")). // Bright blue
				Bold(true)

	traceStatusHighlightStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("15")). // White text
					Background(lipgloss.Color("34")). // Green background for "on"
					Bold(true)

	traceStatusOffStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("15")).  // White text
				Background(lipgloss.Color("124")). // Red background for "off"
				Bold(true)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Italic(true)

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("203")).
			Bold(true)
)

// updateSearchMode handles key events in search mode
func (m model) updateSearchMode(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Delegate to textinput for all key handling
	ti, cmd := m.searchInput.Update(msg)
	m.searchInput = ti
	m.searchQuery = m.searchInput.Value()
	m.refreshMatches()
	m.searchMatchIdx = 0

	// Check for escape or enter to exit search mode
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "esc", "escape": // Bubble Tea returns "esc" for escape key
			m.mode = ModeNormal
			m.searchInput.SetValue("")
			m.searchQuery = ""
			m.refreshMatches()
			m.searchMatchIdx = 0
		case "enter":
			m.mode = ModeNormal
			// Jump to first match (less-style: Enter confirms and navigates to match 0)
			m.searchMatchIdx = 0
			total := len(m.matches())
			if total > 0 {
				m = scrollToMatch(m)
			}
		}
	}

	return m, cmd
}

// updateNormalMode handles key events in normal mode
func (m model) updateNormalMode(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	key := keyMsg.String()

	switch key {
	case "esc", "escape": // Bubble Tea returns "esc" for escape key
		// Back to trace list from trace detail view
		if m.mode == ModeTraceDetail {
			m.mode = ModeNormal
			return m, nil
		}

	case "/", "ctrl+f":
		// Nothing to search on the startup screen.
		if m.currentSource() == startupTab {
			return m, nil
		}
		// Enter search mode
		m.mode = ModeSearch
		m.searchQuery = ""
		m.refreshMatches()
		m.searchMatchIdx = 0
		m.searchInput.Focus()
		m.searchInput.SetValue("")

	case "tab", "right":
		if len(m.sources) > 0 {
			m.selectedSource = (m.selectedSource + 1) % len(m.sources)
			return m.fetchForSelectedSource()
		}

	case "shift+tab", "left":
		if len(m.sources) > 0 {
			m.selectedSource--
			if m.selectedSource < 0 {
				m.selectedSource = len(m.sources) - 1
			}
			return m.fetchForSelectedSource()
		}

	case "up":
		if m.mode == ModeTraceDetail {
			// Scroll up in trace detail view
			m.traceDetailScrollOffset--
			if m.traceDetailScrollOffset < 0 {
				m.traceDetailScrollOffset = 0
			}
		} else if m.isTraceView() {
			// Navigate up in trace list
			if m.selectedTraceIdx > 0 {
				m.selectedTraceIdx--
				// Adjust scroll offset to keep selected item visible
				if m.selectedTraceIdx < m.traceScrollOffset {
					m.traceScrollOffset = m.selectedTraceIdx
				}
			}
		} else {
			m.scrollBy(1)
		}

	case "down":
		if m.mode == ModeTraceDetail {
			// Scroll down in trace detail view. Saturating: End sets this to
			// math.MaxInt, and incrementing that wrapped to the minimum, which
			// renders as the top.
			if m.traceDetailScrollOffset < math.MaxInt {
				m.traceDetailScrollOffset++
			}
		} else if m.isTraceView() {
			// Navigate down in trace list
			if m.selectedTraceIdx < len(m.traces)-1 {
				m.selectedTraceIdx++
				// Adjust scroll offset to keep selected item visible
				availableHeight := m.pageSize()
				if m.selectedTraceIdx >= m.traceScrollOffset+availableHeight {
					m.traceScrollOffset = m.selectedTraceIdx - availableHeight + 1
				}
			}
		} else {
			m.scrollBy(-1)
		}

	case "pgup":
		if m.mode == ModeTraceDetail {
			// Page up in trace detail view
			availableHeight := m.pageSize()
			m.traceDetailScrollOffset -= availableHeight
			if m.traceDetailScrollOffset < 0 {
				m.traceDetailScrollOffset = 0
			}
		} else if m.isTraceView() {
			// Page up in trace list
			availableHeight := m.pageSize()
			m.selectedTraceIdx -= availableHeight
			if m.selectedTraceIdx < 0 {
				m.selectedTraceIdx = 0
			}
			m.traceScrollOffset = m.selectedTraceIdx
		} else {
			m.scrollBy(m.pageSize())
		}

	case "pgdown":
		if m.mode == ModeTraceDetail {
			// Page down in trace detail view
			availableHeight := m.pageSize()
			m.traceDetailScrollOffset += availableHeight
		} else if m.isTraceView() {
			// Page down in trace list
			availableHeight := m.pageSize()
			m.selectedTraceIdx += availableHeight
			if m.selectedTraceIdx >= len(m.traces) {
				m.selectedTraceIdx = len(m.traces) - 1
			}
			// Adjust scroll offset
			if m.selectedTraceIdx >= m.traceScrollOffset+availableHeight {
				m.traceScrollOffset = m.selectedTraceIdx - availableHeight + 1
			}
		} else {
			m.scrollBy(-m.pageSize())
		}

	case "home":
		if m.mode == ModeTraceDetail {
			// Go to top of trace detail view
			m.traceDetailScrollOffset = 0
		} else if m.isTraceView() {
			// Go to first trace
			m.selectedTraceIdx = 0
			m.traceScrollOffset = 0
		} else {
			m.scrollToTop()
		}

	case "end":
		if m.mode == ModeTraceDetail {
			// Go to bottom of trace detail view (we don't know total height, so just set a large number)
			m.traceDetailScrollOffset = math.MaxInt
		} else if m.isTraceView() {
			// Go to last trace
			m.selectedTraceIdx = len(m.traces) - 1
			availableHeight := m.pageSize()
			if m.selectedTraceIdx >= availableHeight {
				m.traceScrollOffset = m.selectedTraceIdx - availableHeight + 1
			}
		} else {
			m.scrollToBottom()
		}

	case "n":
		if m.searchQuery != "" {
			total := len(m.matches())
			if total > 0 {
				m.searchMatchIdx = (m.searchMatchIdx + 1) % total
				m = scrollToMatch(m)
			}
		}

	case "p":
		if m.searchQuery != "" {
			total := len(m.matches())
			if total > 0 {
				m.searchMatchIdx = (m.searchMatchIdx - 1 + total) % total
				m = scrollToMatch(m)
			}
		}

	case "enter":
		// Select trace in trace view (for Phase 3)
		if trace, ok := m.selectedTrace(); ok && m.isTraceView() {
			// Switch to trace detail view
			m.mode = ModeTraceDetail
			m.selectedTraceID = trace.TraceID
			m.traceDetailScrollOffset = 0
			// Clear previous trace data
			m.traceSpans = []spanDetail{}
			m.traceLogs = []logEntry{}
			// Fetch trace details
			return m, tea.Batch(
				fetchTraceSpans(m.apiURL, m.selectedTraceID),
				fetchTraceLogs(m.apiURL, m.selectedTraceID),
			)
		}

	case "t":
		// Toggle trace indicator visibility (only in log view)
		if !m.isTraceView() {
			m.showTraceIDs = !m.showTraceIDs
		}

	case "r":
		// Restart current process (only in log view, not trace view)
		if source := m.currentSource(); source != "" && !m.isTraceView() && source != startupTab {
			return m, restartProcess(m.apiURL, source)
		}
	}

	return m, nil
}

// restartMsg reports how a restart request went.
type restartMsg struct {
	process string
	err     error
}

// restartProcess asks the instance to restart a process.
//
// A command rather than the goroutine it replaced, for three reasons. The
// goroutine used http.Post -- the default client -- which since the API moved
// to a Unix socket went to TCP localhost:80 and reached nothing. It dropped the
// error under a comment claiming it "will appear in logs via normal logging",
// which nothing did, so pressing r silently did nothing. And a goroutine
// spawned from Update is outside Bubble Tea's panic recovery, and ours, so a
// panic in it would have ended the program.
func restartProcess(apiURL, process string) tea.Cmd {
	return func() tea.Msg {
		endpoint := apiURL + "/processes/" + url.PathEscape(process) + "/restart"
		resp, err := apiClient.Post(endpoint, "", nil)
		if err != nil {
			return restartMsg{process: process, err: err}
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			return restartMsg{process: process, err: fmt.Errorf("HTTP %d", resp.StatusCode)}
		}
		return restartMsg{process: process}
	}
}

// scrollToMatch sets m.scrollOffset so that the line containing the current
// searchMatchIdx is centered in the viewport. Disables autoScroll.
func scrollToMatch(m model) model {
	matchLineIndices := m.matches()
	if m.searchMatchIdx < 0 || m.searchMatchIdx >= len(matchLineIndices) {
		return m
	}
	targetLineIdx := matchLineIndices[m.searchMatchIdx]
	totalLines := countAllLines(m.logs, m.width)

	// availableHeight is the same number View() uses, so a jump lands where
	// accounts for header, help, searchBar, padding)
	availableHeight := m.pageSize()
	if availableHeight < 1 {
		availableHeight = 1
	}

	m.scrollOffset = scrollOffsetForMatch(targetLineIdx, totalLines, availableHeight)
	m.autoScroll = false
	return m
}

func TuiCommand(args []string) {
	TuiCommandWithManager(args, nil)
}

// apiClient reaches the instance's API over its Unix socket.
//
// Package-level because there is exactly one TUI per process, and the fetch
// commands are closures built deep in the update loop -- threading a client
// through every tea.Cmd would be noise for no gain.
var apiClient = http.DefaultClient

// apiBaseURL is a placeholder host. The socket dialler ignores it; curl's
// --unix-socket behaves the same way.
const apiBaseURL = "http://localhost"

func TuiCommandWithManager(args []string, manager *process.Manager) {
	// Parse flags
	fs := flag.NewFlagSet("tui", flag.ExitOnError)
	socketPath := fs.String("socket", "",
		"Path to the instance's API socket (default: "+instance.DirName+"/"+instance.SocketName+" here)")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing flags: %v\n", err)
		os.Exit(1)
	}

	projectDir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not determine working directory: %v\n", err)
		os.Exit(1)
	}

	// Default to this project's socket, which is the only one a TUI launched
	// here should be looking at.
	if *socketPath == "" {
		// instance.SocketPath resolves the path itself, so a symlinked route to
		// the project finds the same socket `running-man run` created.
		*socketPath = instance.SocketPath(projectDir)
	}

	if _, err := os.Stat(*socketPath); err != nil {
		fmt.Fprintf(os.Stderr, "No Running Man instance is serving %s\n", *socketPath)
		fmt.Fprintf(os.Stderr, "Start one with `running-man run`, or pass --socket PATH.\n")
		os.Exit(1)
	}

	apiClient = api.NewSocketClient(*socketPath)
	apiURL := apiBaseURL

	// Crash reports go in the working directory's .running-man, beside the
	// marker, so they are found where someone is already looking. Deliberately
	// not derived from the socket path: a long project path puts the socket
	// under the temp directory, which is not where anyone would look for it.
	installCrashLog(projectDir)
	defer installDebugLog(projectDir)()

	// The TUI owns the screen from here. Everything else that writes to the
	// terminal stops, because a diagnostic printed over a full-height frame
	// scrolls it, and an escape sequence in one can clear it outright.
	// Restored on the way out so shutdown messages are visible again.
	defer termout.Silence()()

	// Create and run the TUI
	p := tea.NewProgram(initialModel(apiURL, manager), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		fmt.Fprintf(os.Stderr, "Any crash report is in %s\n", crashLogName)
		os.Exit(1)
	}
}
