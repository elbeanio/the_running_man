package main

// Text that is safe to draw.
//
// Log lines are arbitrary bytes from other people's programs. Three assumptions
// the original rendering made are all wrong for real dev-server output:
//
//   - that len(s) is the width of s. It is the byte count. A line of emoji or
//     CJK is far wider than its length suggests; a line full of colour escapes
//     is far narrower. Every truncation that compared len(line) against a
//     terminal width was therefore cutting in the wrong place.
//   - that a width minus a constant is still positive. It is not in a narrow
//     window, and slicing to a negative bound panics. renderLogs crashed at any
//     width of 19 or less whenever a log line carried a trace ID, which is the
//     default.
//   - that the bytes are printable. They are not: dev servers emit colour
//     codes, carriage returns for spinners, and clear-screen sequences. Drawn
//     into an alt-screen frame those are instructions, not text, and a single
//     ESC[2J wipes the display -- which Bubble Tea's line-diffing renderer then
//     has no reason to repaint.
//
// Everything that reaches the screen goes through here.

import (
	"github.com/charmbracelet/x/ansi"
)

// truncate shortens s to fit width display columns, appending an ellipsis when
// it had to cut.
//
// Width-aware, escape-aware and total: a width of zero or less yields "" rather
// than panicking, which is the whole point of routing every call site through
// one function.
func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= width {
		return s
	}
	// ansi.Truncate accounts for the tail itself and never cuts mid-escape or
	// mid-rune, so a truncated coloured line still closes its own colour.
	return ansi.Truncate(s, width, "...")
}

// displayWidth is the column width of s, ignoring escape sequences.
func displayWidth(s string) int { return ansi.StringWidth(s) }

// clockTime extracts HH:MM:SS from a timestamp for display.
//
// Timestamps arrive as strings over the API, and an OTLP sender picks its own,
// so "" and "oops" are both reachable. The original code sliced [11:19]
// unconditionally in one place and behind a length check in others; the
// unchecked one panicked during a search whenever any entry had a short
// timestamp.
func clockTime(ts string) string {
	// RFC3339: 2026-10-04T18:00:00Z -- the clock starts at 11 and runs 8 wide.
	const start, end = 11, 19
	if len(ts) >= end {
		return ts[start:end]
	}
	// Anything too short to hold a clock is returned as it came. Showing the
	// odd value is more use than showing a blank, and the caller's truncation
	// keeps it inside its column either way.
	return ts
}
