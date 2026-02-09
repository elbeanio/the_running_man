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

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/iangeorge/the_running_man/internal/process"
)

const (
	defaultPollInterval = 2 * time.Second

	// Approximate UI element heights for scroll calculations
	uiHeaderFooterHeight = 5 // Tab bar + help text + padding
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
	return model{
		apiURL:         apiURL,
		sources:        []string{},
		selectedSource: 0,
		logs:           []logEntry{},
		width:          80,
		height:         24,
		manager:        manager,
		scrollOffset:   0,
		autoScroll:     true, // Start with auto-scroll enabled
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
		switch msg.String() {
		case "q", "ctrl+c":
			// Just quit - main.go will handle cleanup
			return m, tea.Quit

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
			// Scroll up one line
			m.autoScroll = false
			m.scrollOffset++

		case "down":
			// Scroll down one line
			m.scrollOffset--
			if m.scrollOffset <= 0 {
				m.scrollOffset = 0
				m.autoScroll = true
			}

		case "pgup":
			// Scroll up one page
			m.autoScroll = false
			availableHeight := m.height - uiHeaderFooterHeight
			m.scrollOffset += availableHeight

		case "pgdown":
			// Scroll down one page
			availableHeight := m.height - uiHeaderFooterHeight
			m.scrollOffset -= availableHeight
			if m.scrollOffset <= 0 {
				m.scrollOffset = 0
				m.autoScroll = true
			}

		case "home":
			// Jump to oldest logs
			m.autoScroll = false
			m.scrollOffset = math.MaxInt // Scroll to top, renderLogs will clamp to actual max

		case "end":
			// Jump to newest logs
			m.scrollOffset = 0
			m.autoScroll = true
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

	// Help footer
	help := helpStyle.Render("\n←/→ Tab: Switch source | ↑/↓ PgUp/PgDn Home/End: Scroll | q: Quit")

	// Calculate available height for logs
	availableHeight := m.height - lipgloss.Height(header) - lipgloss.Height(help) - 2

	// Render logs
	logsView := renderLogs(m.logs, availableHeight, m.width, m.scrollOffset)

	return lipgloss.JoinVertical(lipgloss.Left, header, logsView, help)
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

func renderLogs(logs []logEntry, height, width, scrollOffset int) string {
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
