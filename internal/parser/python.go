package parser

import (
	"regexp"
	"strings"
	"time"
)

// PythonParser detects and groups Python tracebacks
type PythonParser struct {
	inTraceback bool
	lines       []string
	firstLine   string
	firstTime   time.Time
}

var (
	// Matches "Traceback (most recent call last):"
	tracebackStartRegex = regexp.MustCompile(`^Traceback \(most recent call last\):`)

	// Matches "  File "/path/to/file.py", line 123, in function_name"
	tracebackFileRegex = regexp.MustCompile(`^\s+File ".*", line \d+`)

	// Matches the exception line that terminates a traceback.
	//
	// The previous pattern was `^(\w+Error|Exception|Warning):\s+(.*)`, which
	// missed a lot of real Python: exceptions not ending in "Error"
	// (KeyboardInterrupt, SystemExit, StopIteration), dotted names
	// (requests.exceptions.HTTPError), and — because of the `\s+` — any bare
	// exception with no message at all ("ValueError:"). Those tracebacks never
	// terminated cleanly.
	//
	// Now: an optional dotted module prefix, an exception-shaped CapitalisedName,
	// and an OPTIONAL colon and message -- because Python prints a bare
	// "KeyboardInterrupt" with no colon when the exception has no message, which
	// is exactly what Ctrl-C during a hang produces.
	//
	// The name must end in a recognised exception suffix rather than being any
	// capitalised word, so an ordinary log line like "Starting" or "Done" does
	// not terminate a traceback and get mislabelled as its error.
	//
	// Known limitation: lowercase exception names such as socket.timeout are not
	// matched.
	errorLineRegex = regexp.MustCompile(`^([A-Za-z_][\w.]*\.)?([A-Z]\w*(?:Error|Exception|Warning|Interrupt|Exit|Iteration|Timeout|Overflow)|Exception|Warning)(:\s*.*)?$`)

	// traceIDRegex extracts a trace id embedded in traceback text. Package-level
	// so it is compiled once rather than on every traceback, matching its three
	// neighbours above.
	traceIDRegex = regexp.MustCompile(`(?i)(?:trace[_-]?id|trace)[=:]\s*([a-zA-Z0-9\-_.]+)`)
)

// NewPythonParser creates a new Python traceback parser
func NewPythonParser() *PythonParser {
	return &PythonParser{}
}

// Parse offers a line to the traceback accumulator.
//
// It returns the completed entry, if this line finished one, and whether the
// line was CONSUMED. Those are independent:
//
//	(nil, true)    line became part of a traceback; nothing to emit yet
//	(entry, true)  line completed a traceback and belonged to it
//	(entry, false) line completed a traceback but is a log line in its own
//	               right, and the caller must parse it separately
//	(nil, false)   nothing to do with this line
//
// The (entry, false) case is the one that used to lose data: the line was
// reported as consumed, the caller moved on, and it never reached the buffer.
func (p *PythonParser) Parse(source string, line string, timestamp time.Time) (*LogEntry, bool) {
	// Check if this line starts a traceback
	if tracebackStartRegex.MatchString(line) {
		p.inTraceback = true
		p.lines = []string{line}
		p.firstLine = line
		p.firstTime = timestamp
		return nil, true // consumed: accumulating
	}

	// If we're in a traceback, check if this line continues it
	if p.inTraceback {
		// Lines starting with spaces are part of the traceback
		if strings.HasPrefix(line, "  ") || tracebackFileRegex.MatchString(line) {
			p.lines = append(p.lines, line)
			return nil, true // consumed
		}

		// Error line ends the traceback, and belongs to it
		if errorLineRegex.MatchString(line) {
			p.lines = append(p.lines, line)
			entry := p.buildEntry(source)
			p.reset()
			return entry, true
		}

		// A blank line ends it. Nothing worth emitting for the blank itself.
		if strings.TrimSpace(line) == "" && len(p.lines) > 2 {
			entry := p.buildEntry(source)
			p.reset()
			return entry, true
		}

		// Any other line ends the traceback WITHOUT being part of it. Report it
		// as not consumed so the caller parses it on its own merits.
		entry := p.buildEntry(source)
		p.reset()
		return entry, false
	}

	return nil, false
}

// IsInProgress returns true if we're currently parsing a multi-line traceback
func (p *PythonParser) IsInProgress() bool {
	return p.inTraceback
}

// Flush returns any in-progress traceback entry
func (p *PythonParser) Flush(source string) *LogEntry {
	if p.inTraceback && len(p.lines) > 0 {
		entry := p.buildEntry(source)
		p.reset()
		return entry
	}
	return nil
}

func (p *PythonParser) buildEntry(source string) *LogEntry {
	stacktrace := strings.Join(p.lines, "\n")

	// Extract error message from last line
	message := p.firstLine
	if len(p.lines) > 0 {
		lastLine := p.lines[len(p.lines)-1]
		if errorLineRegex.MatchString(lastLine) {
			message = lastLine
		}
	}

	entry := &LogEntry{
		Timestamp:  p.firstTime,
		Level:      LevelError,
		Source:     source,
		Message:    message,
		Raw:        stacktrace,
		IsError:    true,
		Stacktrace: stacktrace,
	}

	// Try to extract trace_id from the traceback
	for _, line := range p.lines {
		if matches := traceIDRegex.FindStringSubmatch(line); len(matches) > 1 {
			entry.TraceID = matches[1]
			break
		}
	}

	return entry
}

func (p *PythonParser) reset() {
	p.inTraceback = false
	p.lines = nil
	p.firstLine = ""
}
