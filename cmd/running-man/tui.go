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
)

// Mode represents the current operating mode of the TUI
type Mode int

const (
	ModeNormal Mode = iota
	ModeSearch
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
}

type logEntry struct {
	Timestamp string `json:"Timestamp"`
	Level     string `json:"Level"`
	Source    string `json:"Source"`
	Message   string `json:"Message"`
	IsError   bool   `json:"IsError"`
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
type errMsg struct{ err error }
type tickMsg time.Time

func (e errMsg) Error() string { return e.err.Error() }

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
		case ModeNormal:
			return m.updateNormalMode(msg)
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case sourcesMsg:
		m.sources = sortSources(msg)
		if len(m.sources) > 0 {
			return m, fetchLogs(m.apiURL, m.sources[m.selectedSource])
		}

	case logsMsg:
		m.logs = msg
		// Reset scroll offset when auto-scroll is enabled (user is at bottom)
		if m.autoScroll {
			m.scrollOffset = 0
		}

	case tickMsg:
		if len(m.sources) > 0 {
			return m, tea.Batch(
				fetchLogs(m.apiURL, m.sources[m.selectedSource]),
				tickCmd(),
			)
		}
		return m, tickCmd()

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
	var helpText string
	switch m.mode {
	case ModeSearch:
		helpText = "\nEsc: Exit search | Enter: Jump to first match"
	case ModeNormal:
		if m.searchQuery != "" {
			matchCount := countMatches(m.logs, m.searchQuery)
			var matchStatus string
			if matchCount == 0 {
				matchStatus = "no matches"
			} else {
				matchStatus = fmt.Sprintf("%d of %d", m.searchMatchIdx+1, matchCount)
			}
			helpText = fmt.Sprintf("\n←/→ Tab: Switch source | ↑/↓ PgUp/PgDn Home/End: Scroll | n/p: Match nav (%s) | /: Search | q: Quit", matchStatus)
		} else {
			helpText = "\n←/→ Tab: Switch source | ↑/↓ PgUp/PgDn Home/End: Scroll | /: Search | q: Quit"
		}
	}
	help := helpStyle.Render(helpText)

	// Calculate available height for logs
	availableHeight := m.height - lipgloss.Height(header) - lipgloss.Height(searchBar) - lipgloss.Height(help) - 2

	// Render logs with search highlighting and current match index
	logsView := renderLogs(m.logs, availableHeight, m.width, m.scrollOffset, m.searchQuery, m.searchMatchIdx)

	return lipgloss.JoinVertical(lipgloss.Left, header, searchBar, logsView, help)
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

func renderLogs(logs []logEntry, height, width, scrollOffset int, searchQuery string, currentMatchIdx int) string {
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
				line = fmt.Sprintf("[%s] [%s] %s", timestamp[11:19], log.Level, msgLine)
			} else {
				// Continuation lines get indented
				line = fmt.Sprintf("                    %s", msgLine)
			}

			// Truncate long lines
			if len(line) > width-2 {
				line = line[:width-5] + "..."
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
		// All lines fit, no scrolling needed
		return lipgloss.JoinVertical(lipgloss.Left, allLines...)
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
	} else {
		// Normal scrolling case
		if startIdx < 0 {
			startIdx = 0
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, allLines[startIdx:endIdx]...)
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
	if query == "" {
		return 0
	}
	lowerQuery := strings.ToLower(query)
	count := 0
	for _, log := range logs {
		lowerMsg := strings.ToLower(log.Message)
		count += strings.Count(lowerMsg, lowerQuery)
	}
	return count
}

func isPrintableKey(msg tea.Msg) bool {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return false
	}
	return len(keyMsg.String()) == 1 && keyMsg.String() != "escape"
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
		case "escape":
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
			return m, fetchLogs(m.apiURL, m.sources[m.selectedSource])
		}

	case "shift+tab", "left":
		if len(m.sources) > 0 {
			m.selectedSource--
			if m.selectedSource < 0 {
				m.selectedSource = len(m.sources) - 1
			}
			return m, fetchLogs(m.apiURL, m.sources[m.selectedSource])
		}

	case "up":
		m.autoScroll = false
		m.scrollOffset++

	case "down":
		m.scrollOffset--
		if m.scrollOffset <= 0 {
			m.scrollOffset = 0
			m.autoScroll = true
		}

	case "pgup":
		m.autoScroll = false
		availableHeight := m.height - uiHeaderFooterHeight
		m.scrollOffset += availableHeight

	case "pgdown":
		availableHeight := m.height - uiHeaderFooterHeight
		m.scrollOffset -= availableHeight
		if m.scrollOffset <= 0 {
			m.scrollOffset = 0
			m.autoScroll = true
		}

	case "home":
		m.autoScroll = false
		m.scrollOffset = math.MaxInt

	case "end":
		m.scrollOffset = 0
		m.autoScroll = true

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
