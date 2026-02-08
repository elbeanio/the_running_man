package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/iangeorge/the_running_man/internal/process"
)

const defaultPollInterval = 2 * time.Second

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
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case sourcesMsg:
		m.sources = msg
		if len(m.sources) > 0 {
			return m, fetchLogs(m.apiURL, m.sources[m.selectedSource])
		}

	case logsMsg:
		m.logs = msg

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
	help := helpStyle.Render("\n←/→ or Tab: Switch source | q: Quit")

	// Calculate available height for logs
	availableHeight := m.height - lipgloss.Height(header) - lipgloss.Height(help) - 2

	// Render logs
	logsView := renderLogs(m.logs, availableHeight, m.width)

	return lipgloss.JoinVertical(lipgloss.Left, header, logsView, help)
}

func renderHeader(sources []string, selected int) string {
	if len(sources) == 0 {
		return headerStyle.Render("Loading sources...")
	}

	tabs := []string{}
	for i, source := range sources {
		style := tabStyle
		if i == selected {
			style = selectedTabStyle
		}
		tabs = append(tabs, style.Render(fmt.Sprintf(" %s ", source)))
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, tabs...)
}

func renderLogs(logs []logEntry, height, width int) string {
	if len(logs) == 0 {
		return logStyle.Render("No logs yet...")
	}

	// Show most recent logs that fit in the available height
	startIdx := 0
	if len(logs) > height {
		startIdx = len(logs) - height
	}

	lines := []string{}
	for _, log := range logs[startIdx:] {
		style := logStyle
		if log.IsError {
			style = errorLogStyle
		}

		// Format: [timestamp] [level] message
		timestamp := log.Timestamp
		if len(timestamp) > 19 {
			timestamp = timestamp[:19] // Trim to HH:MM:SS
		}

		line := fmt.Sprintf("[%s] [%s] %s", timestamp[11:19], log.Level, log.Message)

		// Truncate long lines
		if len(line) > width-2 {
			line = line[:width-5] + "..."
		}

		lines = append(lines, style.Render(line))
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
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
		url := fmt.Sprintf("%s/logs?source=%s&since=5m", apiURL, source)
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
