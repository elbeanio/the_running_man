package main

// One place that decides how big things are.
//
// View derived the content window from the rendered heights of the header,
// search bar and help, while the scroll handlers paged by the constant
// uiHeaderFooterHeight -- whose own comment conceded it only "mirrors the View()
// calculation (approx)". Approximately is not a useful amount of agreement
// between the number of rows shown and the number a page moves: whenever they
// differed, scrolling skipped rows or repeated them. Most easily when the help
// line wrapped, which it did at anything under about 110 columns, because it was
// assembled at its natural length and never fitted to the terminal.
//
// So the chrome is built once and measured, and both the window and the page
// size come from that measurement.

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// chromeRows is everything drawn around the content.
type chromeRows struct {
	header    string
	searchBar string
	help      string
}

// height is the number of rows the chrome occupies, including the content box's
// bottom border and the blank row above the help.
func (c chromeRows) height() int {
	const (
		bottomBorder = 1 // the content box's bottom edge; it has no top edge, which
		// is where the active tab joins it
		gap = 1 // the blank row between content and help
	)
	return lipgloss.Height(c.header) +
		lipgloss.Height(c.searchBar) +
		lipgloss.Height(c.help) +
		bottomBorder + gap
}

// contentWidth is the width available inside the content box.
func (m model) contentWidth() int {
	const borders = 2 // left and right
	return max(0, m.width-borders)
}

// contentHeight is the number of content rows the frame will show.
func (m model) contentHeight() int {
	return max(0, m.height-m.chrome().height())
}

// pageSize is how far PgUp and PgDn move.
//
// Identical to contentHeight by construction rather than by coincidence: a page
// that is not a windowful is the bug this replaced.
func (m model) pageSize() int {
	return m.contentHeight()
}

// fitToWidth returns the first variant that fits, or the last one truncated.
//
// Variants run longest to shortest. A help line that does not fit is worse than
// a terse one: the terminal wraps it, which costs a row the layout has already
// accounted for and pushes the content window out of step.
func fitToWidth(width int, variants ...string) string {
	for _, v := range variants {
		if displayWidth(v) <= width {
			return v
		}
	}
	if len(variants) == 0 {
		return ""
	}
	return truncate(variants[len(variants)-1], width)
}

// helpText is the key hints, as the longest form that fits the terminal.
func (m model) helpText() string {
	switch m.mode {
	case ModeSearch:
		return fitToWidth(m.width,
			"Esc: Exit search | Enter: Jump to first match",
			"Esc: exit | Enter: jump",
			"Esc: exit")

	case ModeTraceDetail:
		return fitToWidth(m.width,
			"ESC: Back to trace list | ↑/↓ PgUp/PgDn Home/End: Scroll | q: Quit",
			"ESC: back | ↑/↓: scroll | q: quit",
			"ESC: back | q: quit",
			"q: quit")
	}

	if m.currentSource() == startupTab {
		return fitToWidth(m.width,
			"←/→ Tab: Switch source | q: Quit",
			"Tab: source | q: quit",
			"q: quit")
	}

	if m.currentSource() == "Traces" {
		long := "←/→ Tab: Switch source | ↑/↓ PgUp/PgDn Home/End: Navigate traces"
		if len(m.traces) > 0 {
			long += fmt.Sprintf(" | Enter: View trace (%d of %d)", m.selectedTraceIdx+1, len(m.traces))
		}
		long += " | q: Quit"
		return fitToWidth(m.width, long,
			"Tab: source | ↑/↓: traces | Enter: open | q: quit",
			"Tab | ↑/↓ | Enter | q",
			"q: quit")
	}

	long := "←/→ Tab: Switch source | ↑/↓ PgUp/PgDn Home/End: Scroll"
	medium := "Tab: source | ↑/↓: scroll"
	if m.searchQuery != "" {
		matchCount := len(m.matches())
		status := "no matches"
		if matchCount > 0 {
			status = fmt.Sprintf("%d of %d", m.searchMatchIdx+1, matchCount)
		}
		long += fmt.Sprintf(" | n/p: Match nav (%s)", status)
		medium += fmt.Sprintf(" | n/p: %s", status)
	}
	long += " | /: Search"
	medium += " | /: search"
	// The toggle state is styled, and fitToWidth measures with displayWidth, so
	// the escape sequences cost no columns and the fit is still correct.
	traceState, traceStyle := "off", traceStatusOffStyle
	if m.showTraceIDs {
		traceState, traceStyle = "on", traceStatusHighlightStyle
	}
	long += " | t: Toggle trace ( " + traceStyle.Render("trace:"+traceState) + " )"
	medium += " | t: " + traceStyle.Render("trace:"+traceState)
	long += " | q: Quit"
	medium += " | q: quit"

	return fitToWidth(m.width, long, medium, "Tab | ↑/↓ | / | t | q", "q: quit")
}

// chrome builds the rows around the content, each already fitted to the width.
func (m model) chrome() chromeRows {
	c := chromeRows{
		header: renderHeader(m.sources, m.selectedSource, m.width, m.sourceTypes),
	}

	if m.mode == ModeSearch {
		searchBarStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("235")).
			Padding(0, 1)

		matchCount := len(m.matches())
		var status string
		switch {
		case m.searchQuery == "":
			status = "Type to search..."
		case matchCount == 0:
			status = "No matches"
		default:
			status = fmt.Sprintf("%d of %d matches", m.searchMatchIdx+1, matchCount)
		}

		// The status pane takes a third of the width, down to nothing, rather
		// than a fixed 40 columns that a narrow terminal cannot afford.
		statusWidth := min(40, m.width/3)
		bar := searchBarStyle.Render(" " + m.searchInput.View() + " ")
		if statusWidth > 0 {
			bar += searchBarStyle.Width(statusWidth).Render(" " + status)
		}
		c.searchBar = truncate(bar, m.width)
	}

	// A recovered panic is stated rather than swallowed: the session survived,
	// but something is wrong and the user should know where to look.
	//
	// The warning outranks the key hints, so it gets the width first and the
	// hints take what is left. Appending it to the full help and truncating the
	// result instead left "| ⚠ View..." -- the warning present but unreadable,
	// which is the worst of both.
	help := m.helpText()

	// A notice, such as a restart's outcome, takes width the same way the panic
	// warning does, but ranks below it.
	status := ""
	if m.notice != "" {
		status = m.notice
	}
	if m.lastPanic != "" {
		status = fmt.Sprintf("⚠ %s (see %s)", m.lastPanic, crashLogName)
	}
	if status != "" {
		warning := truncate(status, m.width)

		// The help already built, not a second helpText(): building it can
		// scan every log for the search count.
		hints := fitToWidth(m.width-displayWidth(warning)-3, help, "q: quit")
		if hints == "" {
			help = warning
		} else {
			help = hints + " | " + warning
		}
	}
	c.help = helpStyle.Render("\n" + help)

	return c
}
