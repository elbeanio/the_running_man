package parser

// Captured output is arbitrary bytes from other people's programs, and some of
// those bytes are instructions rather than text.
//
// Dev servers emit colour codes, carriage returns for spinners and progress
// bars, and occasionally a clear-screen sequence. Stored raw and later drawn
// into a full-screen TUI frame, a single ESC[2J wipes the display -- and Bubble
// Tea's renderer, which diffs against what it believes is on screen, has no
// reason to repaint it. Escape sequences also make every width calculation wrong,
// because they occupy bytes but no columns.
//
// So lines are cleaned once, here, on the way in. The cleaned form is what gets
// stored in both Message and Raw: Raw backs the `contains` filter and the
// buffer's byte accounting, and neither is improved by counting escape bytes
// nobody can see.

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// SanitiseLine makes a captured line safe to store and to draw.
//
// Escape sequences go first, then the control characters ansi.Strip leaves
// behind -- it removes ESC-introduced sequences but not a bare carriage return,
// which is what every spinner and progress bar in the ecosystem is built from.
// Tabs become spaces because a tab's width depends on the terminal's stops,
// which no width calculation here can know.
func SanitiseLine(s string) string {
	s = ansi.Strip(s)
	s = applyCarriageReturns(s)

	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\t':
			b.WriteString("    ")
		case r == '\n':
			// Kept: callers split on newlines to render multi-line entries such
			// as tracebacks.
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			// Everything else in the C0 range, and DEL. Dropped rather than
			// escaped: this is a log viewer, not a hex editor.
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// applyCarriageReturns resolves a carriage return the way a terminal does: it
// returns the cursor to the start of the line, so what follows overwrites what
// came before.
//
// This is how every spinner and progress bar in the ecosystem works -- npm,
// pip, vite, docker pull. Dropping the control character and keeping all the
// text turned "building... 0", "building... 1", "build done" into the single
// run-on line "building... 0building... 1build done". Keeping only the last
// segment gives what the developer would actually have seen.
//
// A terminal would leave any trailing remnant of a longer earlier write
// visible; that is not emulated, because reconstructing it needs the column
// state of a screen this does not have.
func applyCarriageReturns(s string) string {
	if !strings.ContainsRune(s, '\r') {
		return s
	}
	// Applied per line, not across the whole string: a carriage return returns
	// to the start of *its* line, so resolving it globally would let one line's
	// spinner swallow every line above it. That also keeps CRLF endings intact.
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = lastWrite(line)
	}
	return strings.Join(lines, "\n")
}

// lastWrite returns the final thing written to a line, given that each carriage
// return restarted it.
func lastWrite(line string) string {
	if !strings.ContainsRune(line, '\r') {
		return line
	}
	segments := strings.Split(line, "\r")
	// A line ending in a carriage return has an empty last segment; the content
	// is whatever was written before it.
	for i := len(segments) - 1; i >= 0; i-- {
		if strings.TrimSpace(segments[i]) != "" {
			return segments[i]
		}
	}
	return ""
}
