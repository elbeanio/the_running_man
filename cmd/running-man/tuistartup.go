package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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
	"starting": "◐",
	"pending":  "○",
	"blocked":  "✗",
	"failed":   "✗",
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
	case "starting":
		return startupWaitStyle
	case "blocked", "failed":
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
		kind := ""
		if r.Kind == "service" {
			kind = " (Compose)"
		}
		head := fmt.Sprintf(" %s %-*s  %-*s  %s",
			startupGlyphs[r.State], nameWidth, r.Name, stateWidth, r.State, r.Detail+kind)
		add(st.Render(head))

		lines := r.Lines
		if len(lines) > 2 {
			lines = lines[len(lines)-2:]
		}
		for _, l := range lines {
			add(startupLineStyle.Render("     " + l))
		}
	}

	for len(out) < height {
		out = append(out, "")
	}
	return strings.Join(out, "\n")
}
