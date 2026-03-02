package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/iangeorge/the_running_man/internal/process"
)

const (
	defaultPollInterval = 2 * time.Second

	// Approximate UI element heights for scroll calculations
	uiHeaderFooterHeight = 5 // Tab bar + help text + padding

	// Maximum length for trace ID part in UI display (truncated if longer)
	// Does not include "[trace:" prefix and "]" suffix
	// Total indicator max length is this + 8 (for "[trace:" and "]")
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
	selectedSource int
	logs           []logEntry
	err            error
	width          int
	height         int
	manager        *process.Manager // Process manager to stop on quit
	scrollOffset   int              // Number of lines scrolled from bottom (0 = showing latest)
	autoScroll     bool             // Whether to auto-scroll to bottom on new logs
	mode           Mode             // Current mode (normal or search)
	searchInput    textinput.Model  // Text input for search mode
	searchQuery    string           // Current search query (mirrored from searchInput)
	searchMatchIdx int              // Current match index when navigating with n/N
	showTraceIDs   bool             // Whether to show trace indicators (toggled with 't')

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
}

type logEntry struct {
	Timestamp string `json:"Timestamp"`
	Level     string `json:"Level"`
	Source    string `json:"Source"`
	Message   string `json:"Message"`
	IsError   bool   `json:"IsError"`
	TraceID   string `json:"TraceID,omitempty"` // Optional trace ID for correlation
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
	EntryCount int    `json:"entry_count"`
}

// Messages for async operations
type sourcesMsg []string
type logsMsg []logEntry
type tracesMsg []traceSummary
type traceSpansMsg []spanDetail
type traceLogsMsg []logEntry
type errMsg struct{ err error }
type tickMsg time.Time

func (e errMsg) Error() string { return e.err.Error() }

// fetchForSelectedSource returns the appropriate command based on selected tab
func (m model) fetchForSelectedSource() (model, tea.Cmd) {
	if len(m.sources) == 0 {
		return m, nil
	}

	source := m.sources[m.selectedSource]
	if source == "Traces" {
		return m, fetchTraces(m.apiURL)
	}
	return m, fetchLogs(m.apiURL, source)
}

// isTraceView returns true if the currently selected tab is "Traces"
func (m model) isTraceView() bool {
	return len(m.sources) > 0 && m.sources[m.selectedSource] == "Traces"
}

func fetchTraces(apiURL string) tea.Cmd {
	return func() tea.Msg {
		resp, err := http.Get(apiURL + "/traces")
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
				TraceID      string            `json:"TraceID"`
				SpanID       string            `json:"SpanID"`
				ParentSpanID string            `json:"ParentSpanID"`
				Name         string            `json:"Name"`
				Kind         string            `json:"Kind"`
				StartTime    time.Time         `json:"StartTime"`
				EndTime      time.Time         `json:"EndTime"`
				Duration     string            `json:"Duration"` // Duration as string like "1.23456789s"
				Status       string            `json:"Status"`
				StatusCode   string            `json:"StatusCode"`
				ServiceName  string            `json:"ServiceName"`
				Attributes   map[string]string `json:"Attributes"`
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
		resp, err := http.Get(fmt.Sprintf("%s/traces?trace_id=%s", apiURL, traceID))
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
				TraceID      string            `json:"TraceID"`
				SpanID       string            `json:"SpanID"`
				ParentSpanID string            `json:"ParentSpanID"`
				Name         string            `json:"Name"`
				Kind         string            `json:"Kind"`
				StartTime    time.Time         `json:"StartTime"`
				EndTime      time.Time         `json:"EndTime"`
				Duration     string            `json:"Duration"`
				Status       string            `json:"Status"`
				StatusCode   string            `json:"StatusCode"`
				ServiceName  string            `json:"ServiceName"`
				Attributes   map[string]string `json:"Attributes"`
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
		resp, err := http.Get(fmt.Sprintf("%s/traces/%s/logs", apiURL, traceID))
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
		tickCmd(),
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Global quit works in any mode
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

		// Route to mode-specific handler
		switch m.mode {
		case ModeSearch:
			return m.updateSearchMode(msg)
		case ModeNormal, ModeTraceDetail:
			return m.updateNormalMode(msg)
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case sourcesMsg:
		m.sources = sortSources(msg)
		// Add "Traces" as a special tab at the end
		m.sources = append(m.sources, "Traces")
		if len(m.sources) > 0 {
			return m, fetchLogs(m.apiURL, m.sources[m.selectedSource])
		}

	case logsMsg:
		m.logs = msg
		// Reset scroll offset when auto-scroll is enabled (user is at bottom)
		if m.autoScroll {
			m.scrollOffset = 0
		}

	case tracesMsg:
		m.traces = msg
		// Reset trace scroll offset
		m.traceScrollOffset = 0

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
			source := m.sources[m.selectedSource]
			if source == "Traces" {
				cmds = append(cmds, fetchTraces(m.apiURL))
			} else {
				cmds = append(cmds, fetchLogs(m.apiURL, source))
			}
		}

		return m, tea.Batch(cmds...)

	case errMsg:
		m.err = msg.err
	}

	return m, nil
}

func (m model) View() string {
	if m.err != nil {
		return errorStyle.Render(fmt.Sprintf("Error: %v\n\nPress q to quit", m.err))
	}

	// Header with source tabs
	header := renderHeader(m.sources, m.selectedSource)

	// Search bar - use textinput when in search mode
	var searchBar string
	if m.mode == ModeSearch {
		searchBarStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("235")).
			Padding(0, 1)

		matchCount := countMatches(m.logs, m.searchQuery)
		var status string
		if m.searchQuery == "" {
			status = "Type to search..."
		} else if matchCount == 0 {
			status = "No matches"
		} else {
			status = fmt.Sprintf("%d of %d matches", m.searchMatchIdx+1, matchCount)
		}

		searchInput := m.searchInput.View()
		searchBar = searchBarStyle.Render(" "+searchInput+" ") +
			searchBarStyle.Width(40).Render(" "+status)
	}

	// Calculate help text based on mode
	var help string
	switch m.mode {
	case ModeSearch:
		help = helpStyle.Render("\nEsc: Exit search | Enter: Jump to first match")
	case ModeTraceDetail:
		// Trace detail view help
		baseHelp := "ESC: Back to trace list | ↑/↓ PgUp/PgDn Home/End: Scroll"
		baseHelp += " | q: Quit"
		help = helpStyle.Render("\n" + baseHelp)
	case ModeNormal:
		// Check if we're in trace view
		isTraceView := len(m.sources) > 0 && m.sources[m.selectedSource] == "Traces"

		// Build help text with styled components
		baseHelp := "←/→ Tab: Switch source"

		if isTraceView {
			// Trace view specific help
			baseHelp += " | ↑/↓ PgUp/PgDn Home/End: Navigate traces"
			if len(m.traces) > 0 {
				baseHelp += fmt.Sprintf(" | Enter: View trace (%d of %d)", m.selectedTraceIdx+1, len(m.traces))
			}
		} else {
			// Log view help
			baseHelp += " | ↑/↓ PgUp/PgDn Home/End: Scroll"

			// Add search navigation if there's a search query
			if m.searchQuery != "" {
				matchCount := countMatches(m.logs, m.searchQuery)
				var matchStatus string
				if matchCount == 0 {
					matchStatus = "no matches"
				} else {
					matchStatus = fmt.Sprintf("%d of %d", m.searchMatchIdx+1, matchCount)
				}
				baseHelp += fmt.Sprintf(" | n/p: Match nav (%s)", matchStatus)
			}

			// Add search command
			baseHelp += " | /: Search"

			// Add trace toggle with highlighted status
			var traceStatusStyled string
			if m.showTraceIDs {
				traceStatusStyled = traceStatusHighlightStyle.Render(" trace:on ")
			} else {
				traceStatusStyled = traceStatusOffStyle.Render(" trace:off ")
			}
			baseHelp += fmt.Sprintf(" | t: Toggle trace (%s)", traceStatusStyled)
		}

		// Add quit command
		baseHelp += " | q: Quit"

		help = helpStyle.Render("\n" + baseHelp)
	}

	// Calculate available height for content
	availableHeight := m.height - lipgloss.Height(header) - lipgloss.Height(searchBar) - lipgloss.Height(help) - 2

	// Render content based on mode
	var content string
	switch m.mode {
	case ModeTraceDetail:
		// Render trace detail view
		content = renderTraceDetail(m.selectedTraceID, m.traceSpans, m.traceLogs, availableHeight, m.width, m.traceDetailScrollOffset)
	case ModeNormal:
		// Check if we're in Traces tab
		if len(m.sources) > 0 && m.sources[m.selectedSource] == "Traces" {
			// Render trace list
			content = renderTraceList(m.traces, availableHeight, m.width, m.traceScrollOffset, m.selectedTraceIdx)
		} else {
			// Render logs with search highlighting and current match index
			content = renderLogs(m.logs, availableHeight, m.width, m.scrollOffset, m.searchQuery, m.searchMatchIdx, m.showTraceIDs)
		}
	default:
		// For search mode or others, render logs
		content = renderLogs(m.logs, availableHeight, m.width, m.scrollOffset, m.searchQuery, m.searchMatchIdx, m.showTraceIDs)
	}

	return lipgloss.JoinVertical(lipgloss.Left, header, searchBar, content, help)
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
		displayTraceID := trace.TraceID
		if len(displayTraceID) > traceIDWidth {
			displayTraceID = displayTraceID[:traceIDWidth-3] + "..."
		}

		// Format duration
		durationStr := trace.Duration.String()
		if len(durationStr) > durationWidth {
			durationStr = durationStr[:durationWidth-3] + "..."
		}

		// Format status
		statusStr := trace.Status
		if len(statusStr) > statusWidth {
			statusStr = statusStr[:statusWidth-3] + "..."
		}

		// Format services (comma-separated)
		servicesStr := strings.Join(trace.Services, ", ")
		if len(servicesStr) > servicesWidth {
			servicesStr = servicesStr[:servicesWidth-3] + "..."
		}

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
			timestamp := log.Timestamp
			if len(timestamp) > 19 {
				timestamp = timestamp[11:19] // HH:MM:SS
			}
			line := fmt.Sprintf("[%s] [%s] %s", timestamp, log.Level, log.Message)
			if len(line) > width-2 {
				line = line[:width-5] + "..."
			}
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
	maxLineWidth := width - len(nodePrefix) - 2
	if len(spanInfo) > maxLineWidth {
		spanInfo = spanInfo[:maxLineWidth-3] + "..."
	}

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

// sortSources organizes sources into logical groups:
// Group 1: running-man (internal logs)
// Group 2: Docker containers (alphabetical)
// Group 3: Processes (alphabetical)
func sortSources(sources []string) []string {
	runningMan := []string{}
	docker := []string{}
	processes := []string{}

	for _, source := range sources {
		if source == "running-man" {
			runningMan = append(runningMan, source)
		} else if isDockerContainer(source) {
			docker = append(docker, source)
		} else {
			processes = append(processes, source)
		}
	}

	// Sort each group alphabetically
	sort.Strings(docker)
	sort.Strings(processes)

	// Combine groups
	result := []string{}
	result = append(result, runningMan...)
	result = append(result, docker...)
	result = append(result, processes...)

	return result
}

// isDockerContainer attempts to identify if a source name is a Docker container.
// Docker Compose containers typically follow the pattern: projectname-servicename-N
// or projectname_servicename_N (depending on compose version)
func isDockerContainer(name string) bool {
	// Look for patterns like "project-service-1" or "project_service_1"
	// This is a heuristic - containers have multiple segments separated by - or _
	// and typically end with a number

	parts := strings.FieldsFunc(name, func(r rune) bool {
		return r == '-' || r == '_'
	})

	// Docker containers typically have at least 3 parts: project-service-replica
	if len(parts) < 3 {
		return false
	}

	// Last part is often a number (replica index)
	lastPart := parts[len(parts)-1]
	if len(lastPart) > 0 {
		// Check if it's a hex ID (first 12 chars of container ID)
		// or a replica number
		if _, err := fmt.Sscanf(lastPart, "%d", new(int)); err == nil {
			return true
		}
		// Could also be a hash - if it's all hex digits
		if len(lastPart) == 12 {
			return true
		}
	}

	return false
}

func renderHeader(sources []string, selected int) string {
	if len(sources) == 0 {
		return headerStyle.Render("Loading sources...")
	}

	tabs := []string{}
	for i, source := range sources {
		// Determine style based on source group
		var normalStyle, selectedStyle lipgloss.Style

		if source == "running-man" {
			normalStyle = runningManTabStyle
			selectedStyle = runningManSelectedTabStyle
		} else if source == "Traces" {
			normalStyle = tracesTabStyle
			selectedStyle = tracesSelectedTabStyle
		} else if isDockerContainer(source) {
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
		}

		tabs = append(tabs, style.Render(fmt.Sprintf(" %s ", source)))
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, tabs...)
}

func renderSearchBar(active bool, query string, logs []logEntry, matchIdx int) string {
	if !active {
		return ""
	}

	matchCount := countMatches(logs, query)
	var status string
	if query == "" {
		status = "Type to search..."
	} else if matchCount == 0 {
		status = "No matches"
	} else {
		status = fmt.Sprintf("%d of %d matches", matchIdx+1, matchCount)
	}

	searchBarStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("15")).
		Background(lipgloss.Color("235")).
		Padding(0, 1)

	inputStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("86"))

	return searchBarStyle.Render(fmt.Sprintf(" Search: %s%s ", query, inputStyle.Render("_"))) +
		searchBarStyle.Width(40).Render(" "+status)
}

func renderLogs(logs []logEntry, height, width, scrollOffset int, searchQuery string, currentMatchIdx int, showTraceIDs bool) string {
	// Validate inputs
	if height <= 0 || width <= 0 {
		return logStyle.Render("Invalid terminal dimensions")
	}
	if scrollOffset < 0 {
		scrollOffset = 0
	}

	if len(logs) == 0 {
		return logStyle.Render("No logs yet...")
	}

	highlight := searchQuery != ""
	lowerQuery := strings.ToLower(searchQuery)

	// runningMatchCount tracks which global match index we're at as we iterate lines.
	runningMatchCount := 0

	// Collect all lines (splitting multiline messages)
	allLines := []string{}
	for _, log := range logs {
		style := logStyle
		if log.IsError {
			style = errorLogStyle
		}

		// Format: [timestamp] [level] message
		timestamp := log.Timestamp
		if len(timestamp) > 19 {
			timestamp = timestamp[:19] // Trim to HH:MM:SS
		}

		// Split message on newlines to handle multiline output
		messageLines := strings.Split(log.Message, "\n")

		for i, msgLine := range messageLines {
			var line string
			if i == 0 {
				// First line gets full prefix
				baseLine := fmt.Sprintf("[%s] [%s] %s", timestamp[11:19], log.Level, msgLine)

				// Add trace indicator if enabled and trace_id exists
				if showTraceIDs && log.TraceID != "" {
					// Truncate trace ID if too long
					displayTraceID := log.TraceID
					if len(displayTraceID) > maxTraceIDDisplayLength {
						displayTraceID = displayTraceID[:maxTraceIDDisplayLength-3] + "..."
					}
					traceIndicator := fmt.Sprintf("[trace:%s]", displayTraceID)

					// Calculate available space for message after trace indicator
					// We need to account for the styled width, not just string length
					traceIndicatorStyled := traceIndicatorStyle.Render(traceIndicator)
					indicatorWidth := lipgloss.Width(traceIndicatorStyled) + 1 // +1 for space

					// Available width for the base line (message + timestamp + level)
					// width is terminal width, we need to leave room for indicator
					maxBaseLineWidth := width - indicatorWidth

					if len(baseLine) > maxBaseLineWidth {
						// Truncate message to make room for trace indicator
						baseLine = baseLine[:maxBaseLineWidth-3] + "..."
					}

					line = fmt.Sprintf("%s %s", baseLine, traceIndicatorStyled)
				} else {
					line = baseLine
				}
			} else {
				// Continuation lines get indented
				line = fmt.Sprintf("                    %s", msgLine)
			}

			// Truncate long lines (only if trace indicator wasn't added above)
			// When trace indicator is added, we've already handled truncation
			if !(i == 0 && showTraceIDs && log.TraceID != "") && lipgloss.Width(line) > width-2 {
				// Need to truncate the unstyled string, not the styled one
				// Find how many characters to keep
				charsToKeep := width - 5 // Leave room for "..."
				if charsToKeep < 0 {
					charsToKeep = 0
				}
				if len(line) > charsToKeep {
					line = line[:charsToKeep] + "..."
				}
			}

			// Apply highlighting if search query exists
			if highlight {
				lineMatchOffset := runningMatchCount
				occurrences := strings.Count(strings.ToLower(line), lowerQuery)
				line = highlightMatchesWithCurrent(line, searchQuery, lineMatchOffset, currentMatchIdx)
				runningMatchCount += occurrences
			}

			allLines = append(allLines, style.Render(line))
		}
	}

	// Calculate start index based on scroll offset
	// scrollOffset = 0 means show most recent (bottom)
	// scrollOffset > 0 means scroll up from bottom
	totalLines := len(allLines)
	if totalLines <= height {
		// All lines fit, but we need to pad with empty lines to fill height
		// This ensures old content is cleared when we have fewer lines
		paddedLines := make([]string, height)
		copy(paddedLines[height-totalLines:], allLines)
		// Fill beginning with empty styled lines
		for i := 0; i < height-totalLines; i++ {
			paddedLines[i] = logStyle.Render("")
		}
		return lipgloss.JoinVertical(lipgloss.Left, paddedLines...)
	}

	// Start from the bottom and move up by scrollOffset
	endIdx := totalLines - scrollOffset
	startIdx := endIdx - height

	// Clamp to valid ranges
	if endIdx < height {
		// Scrolled too far up, show the oldest logs
		startIdx = 0
		endIdx = height
	} else if endIdx > totalLines {
		// Should never happen with valid scrollOffset, but clamp anyway
		endIdx = totalLines
		startIdx = totalLines - height
		if startIdx < 0 {
			startIdx = 0
		}
	} else {
		// Normal scrolling case
		if startIdx < 0 {
			startIdx = 0
		}
	}

	// Ensure we return exactly height lines
	lines := allLines[startIdx:endIdx]
	if len(lines) < height {
		// Pad with empty lines
		paddedLines := make([]string, height)
		copy(paddedLines[height-len(lines):], lines)
		for i := 0; i < height-len(lines); i++ {
			paddedLines[i] = logStyle.Render("")
		}
		return lipgloss.JoinVertical(lipgloss.Left, paddedLines...)
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// buildMatchLineIndex returns a slice where each element is the rendered-line index
// (in the flat allLines array that renderLogs would produce) for each global match
// occurrence of query across all logs. Used to compute scrollOffset for n/p navigation.
func buildMatchLineIndex(logs []logEntry, width int, query string) []int {
	if query == "" {
		return nil
	}
	lowerQuery := strings.ToLower(query)
	var result []int
	lineIdx := 0

	for _, log := range logs {
		timestamp := log.Timestamp
		if len(timestamp) > 19 {
			timestamp = timestamp[:19]
		}
		messageLines := strings.Split(log.Message, "\n")

		for i, msgLine := range messageLines {
			var line string
			if i == 0 {
				line = fmt.Sprintf("[%s] [%s] %s", timestamp[11:19], log.Level, msgLine)
			} else {
				line = fmt.Sprintf("                    %s", msgLine)
			}
			if len(line) > width-2 {
				line = line[:width-5] + "..."
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

// highlightMatches is the original uniform-highlight version, kept for compatibility.
// It delegates to highlightMatchesWithCurrent with no current match.
func highlightMatches(line, query string) string {
	return highlightMatchesWithCurrent(line, query, 0, -1)
}

// Commands
func fetchSources(apiURL string) tea.Cmd {
	return func() tea.Msg {
		resp, err := http.Get(apiURL + "/health")
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
		for i, s := range health.Sources {
			sources[i] = s.Name
		}

		return sourcesMsg(sources)
	}
}

func fetchLogs(apiURL, source string) tea.Cmd {
	return func() tea.Msg {
		url := fmt.Sprintf("%s/logs?source=%s", apiURL, source)
		resp, err := http.Get(url)
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

		return logsMsg(logsResp.Logs)
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(defaultPollInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func countMatches(logs []logEntry, query string) int {
	// Delegate to buildMatchLineIndex so the count matches exactly what is
	// highlighted in the rendered output (full line including timestamp/level prefix).
	// Use a large width so truncation never fires and no matches are cut off.
	return len(buildMatchLineIndex(logs, 1<<20, query))
}

func isPrintableKey(msg tea.Msg) bool {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return false
	}
	keyStr := keyMsg.String()
	return len(keyStr) == 1 && keyStr != "esc" && keyStr != "escape"
}

// Styles
var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("57"))

	// Tab styles for running-man (system logs) - Cyan/Blue
	runningManTabStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("250")).
				Background(lipgloss.Color("24")). // Dark blue
				Padding(0, 1)

	runningManSelectedTabStyle = lipgloss.NewStyle().
					Bold(true).
					Foreground(lipgloss.Color("15")).
					Background(lipgloss.Color("39")). // Bright blue
					Padding(0, 1)

	// Tab styles for Docker containers - Green
	dockerTabStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("250")).
			Background(lipgloss.Color("22")). // Dark green
			Padding(0, 1)

	dockerSelectedTabStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("15")).
				Background(lipgloss.Color("34")). // Bright green
				Padding(0, 1)

	// Tab styles for processes - Yellow/Orange
	processTabStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("250")).
			Background(lipgloss.Color("94")). // Dark orange
			Padding(0, 1)

	processSelectedTabStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("15")).
				Background(lipgloss.Color("214")). // Bright orange
				Padding(0, 1)

	// Tab styles for Traces view - Purple
	tracesTabStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("250")).
			Background(lipgloss.Color("54")). // Dark purple
			Padding(0, 1)

	tracesSelectedTabStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("15")).
				Background(lipgloss.Color("93")). // Bright purple
				Padding(0, 1)

	// Legacy generic tab styles (kept for compatibility)
	tabStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("250")).
			Background(lipgloss.Color("235")).
			Padding(0, 1)

	selectedTabStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("15")).
				Background(lipgloss.Color("57")).
				Padding(0, 1)

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
	m.searchMatchIdx = 0

	// Check for escape or enter to exit search mode
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "esc", "escape": // Bubble Tea returns "esc" for escape key
			m.mode = ModeNormal
			m.searchInput.SetValue("")
			m.searchQuery = ""
			m.searchMatchIdx = 0
		case "enter":
			m.mode = ModeNormal
			// Jump to first match (less-style: Enter confirms and navigates to match 0)
			m.searchMatchIdx = 0
			total := countMatches(m.logs, m.searchQuery)
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
		// Enter search mode
		m.mode = ModeSearch
		m.searchQuery = ""
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
			m.autoScroll = false
			m.scrollOffset++
		}

	case "down":
		if m.mode == ModeTraceDetail {
			// Scroll down in trace detail view
			m.traceDetailScrollOffset++
		} else if m.isTraceView() {
			// Navigate down in trace list
			if m.selectedTraceIdx < len(m.traces)-1 {
				m.selectedTraceIdx++
				// Adjust scroll offset to keep selected item visible
				availableHeight := m.height - uiHeaderFooterHeight
				if m.selectedTraceIdx >= m.traceScrollOffset+availableHeight {
					m.traceScrollOffset = m.selectedTraceIdx - availableHeight + 1
				}
			}
		} else {
			m.scrollOffset--
			if m.scrollOffset <= 0 {
				m.scrollOffset = 0
				m.autoScroll = true
			}
		}

	case "pgup":
		if m.mode == ModeTraceDetail {
			// Page up in trace detail view
			availableHeight := m.height - uiHeaderFooterHeight
			m.traceDetailScrollOffset -= availableHeight
			if m.traceDetailScrollOffset < 0 {
				m.traceDetailScrollOffset = 0
			}
		} else if m.isTraceView() {
			// Page up in trace list
			availableHeight := m.height - uiHeaderFooterHeight
			m.selectedTraceIdx -= availableHeight
			if m.selectedTraceIdx < 0 {
				m.selectedTraceIdx = 0
			}
			m.traceScrollOffset = m.selectedTraceIdx
		} else {
			m.autoScroll = false
			availableHeight := m.height - uiHeaderFooterHeight
			m.scrollOffset += availableHeight
		}

	case "pgdown":
		if m.mode == ModeTraceDetail {
			// Page down in trace detail view
			availableHeight := m.height - uiHeaderFooterHeight
			m.traceDetailScrollOffset += availableHeight
		} else if m.isTraceView() {
			// Page down in trace list
			availableHeight := m.height - uiHeaderFooterHeight
			m.selectedTraceIdx += availableHeight
			if m.selectedTraceIdx >= len(m.traces) {
				m.selectedTraceIdx = len(m.traces) - 1
			}
			// Adjust scroll offset
			if m.selectedTraceIdx >= m.traceScrollOffset+availableHeight {
				m.traceScrollOffset = m.selectedTraceIdx - availableHeight + 1
			}
		} else {
			availableHeight := m.height - uiHeaderFooterHeight
			m.scrollOffset -= availableHeight
			if m.scrollOffset <= 0 {
				m.scrollOffset = 0
				m.autoScroll = true
			}
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
			m.autoScroll = false
			m.scrollOffset = math.MaxInt
		}

	case "end":
		if m.mode == ModeTraceDetail {
			// Go to bottom of trace detail view (we don't know total height, so just set a large number)
			m.traceDetailScrollOffset = math.MaxInt
		} else if m.isTraceView() {
			// Go to last trace
			m.selectedTraceIdx = len(m.traces) - 1
			availableHeight := m.height - uiHeaderFooterHeight
			if m.selectedTraceIdx >= availableHeight {
				m.traceScrollOffset = m.selectedTraceIdx - availableHeight + 1
			}
		} else {
			m.scrollOffset = 0
			m.autoScroll = true
		}

	case "n":
		if m.searchQuery != "" {
			total := countMatches(m.logs, m.searchQuery)
			if total > 0 {
				m.searchMatchIdx = (m.searchMatchIdx + 1) % total
				m = scrollToMatch(m)
			}
		}

	case "p":
		if m.searchQuery != "" {
			total := countMatches(m.logs, m.searchQuery)
			if total > 0 {
				m.searchMatchIdx = (m.searchMatchIdx - 1 + total) % total
				m = scrollToMatch(m)
			}
		}

	case "enter":
		// Select trace in trace view (for Phase 3)
		if m.isTraceView() && len(m.traces) > 0 {
			// Switch to trace detail view
			m.mode = ModeTraceDetail
			m.selectedTraceID = m.traces[m.selectedTraceIdx].TraceID
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
	}

	return m, nil
}

// scrollToMatch sets m.scrollOffset so that the line containing the current
// searchMatchIdx is centered in the viewport. Disables autoScroll.
func scrollToMatch(m model) model {
	matchLineIndices := buildMatchLineIndex(m.logs, m.width, m.searchQuery)
	if m.searchMatchIdx >= len(matchLineIndices) {
		return m
	}
	targetLineIdx := matchLineIndices[m.searchMatchIdx]
	totalLines := countAllLines(m.logs, m.width)

	// availableHeight mirrors the View() calculation (approx — uiHeaderFooterHeight
	// accounts for header, help, searchBar, padding)
	availableHeight := m.height - uiHeaderFooterHeight
	if availableHeight < 1 {
		availableHeight = 1
	}

	m.scrollOffset = scrollOffsetForMatch(targetLineIdx, totalLines, availableHeight)
	m.autoScroll = false
	return m
}

func tuiCommand(args []string) {
	tuiCommandWithManager(args, nil)
}

func tuiCommandWithManager(args []string, manager *process.Manager) {
	// Parse flags
	fs := flag.NewFlagSet("tui", flag.ExitOnError)
	apiPort := fs.Int("api-port", defaultAPIPort, "API server port")
	fs.Parse(args)

	apiURL := fmt.Sprintf("http://localhost:%d", *apiPort)

	// Create and run the TUI
	p := tea.NewProgram(initialModel(apiURL, manager), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		os.Exit(1)
	}
}
