package parser

import (
	"sync"
	"time"
)

// LogLevel represents the severity of a log entry
type LogLevel string

const (
	LevelDebug LogLevel = "debug"
	LevelInfo  LogLevel = "info"
	LevelWarn  LogLevel = "warn"
	LevelError LogLevel = "error"
)

// LogEntry represents a parsed log entry.
//
// The json tags are load-bearing. Without them these fields serialised under
// their Go names ("Timestamp", "IsError"), while docs/openapi.yaml documented
// snake_case and every other endpoint -- /processes, /health -- used
// snake_case. An agent following /docs got every field name wrong on the
// endpoint it uses most.
type LogEntry struct {
	Timestamp  time.Time `json:"timestamp"`
	Level      LogLevel  `json:"level"`
	Source     string    `json:"source"`
	SourceType string    `json:"source_type,omitempty"` // "process", "docker", "system", "traces", "otlp"
	Message    string    `json:"message"`
	Raw        string    `json:"raw"`
	IsError    bool      `json:"is_error"`
	Stacktrace string    `json:"stacktrace,omitempty"`
	TraceID    string    `json:"trace_id,omitempty"`
}

// sourceState holds the per-source parsing state.
//
// Only the Python traceback parser is stateful; the JSON and plain-text
// parsers are pure. Each source gets its own mutex so that a noisy source
// cannot serialise every other one, and because a single source has two
// concurrent readers (stdout and stderr) that must not interleave into the
// same traceback accumulator.
type sourceState struct {
	mu     sync.Mutex
	python *PythonParser
}

// MultiParser coordinates the individual parsers and holds the multi-line
// state needed to group Python tracebacks.
//
// State is kept PER SOURCE. It previously shared one PythonParser across
// every source, which meant a single instance was mutated from one goroutine
// per stream per process (plus one per container). That produced three
// distinct failures:
//
//   - a data race on the traceback accumulator;
//   - a traceback open on one source swallowing indented lines from another,
//     so those lines never reached the buffer at all and reappeared inside an
//     unrelated stack trace;
//   - the completed entry being attributed to whichever source happened to
//     emit the terminating line.
//
// A MultiParser is safe for concurrent use.
type MultiParser struct {
	jsonParser      *JSONParser
	plainTextParser *PlainTextParser

	mu     sync.Mutex
	states map[string]*sourceState
}

// NewMultiParser creates a new multi-format parser
func NewMultiParser() *MultiParser {
	return &MultiParser{
		jsonParser:      NewJSONParser(),
		plainTextParser: NewPlainTextParser(),
		states:          make(map[string]*sourceState),
	}
}

// stateFor returns the parsing state for a source, creating it on first use.
// The number of sources is bounded by the processes and containers being
// watched, so this map does not grow without limit in practice.
func (m *MultiParser) stateFor(source string) *sourceState {
	m.mu.Lock()
	defer m.mu.Unlock()

	st, ok := m.states[source]
	if !ok {
		st = &sourceState{python: NewPythonParser()}
		m.states[source] = st
	}
	return st
}

// ParseLine attempts to parse a log line using available parsers.
//
// It returns zero, one or two entries: zero while a multi-line traceback is
// still accumulating, and two when a line both terminates a traceback and is
// itself a log line.
func (m *MultiParser) ParseLine(source string, line string, timestamp time.Time) []*LogEntry {
	return m.ParseLineWithType(source, "", line, timestamp)
}

// ParseLineWithType parses a log line with source type information.
//
// See ParseLine for the return contract.
func (m *MultiParser) ParseLineWithType(source string, sourceType string, line string, timestamp time.Time) []*LogEntry {
	st := m.stateFor(source)
	st.mu.Lock()
	defer st.mu.Unlock()

	var entries []*LogEntry

	// If a traceback is open for this source, the line belongs to it until it
	// says otherwise. Checking this first matters: trying JSON first would let
	// a structured line arrive mid-traceback, be emitted on its own, and leave
	// the traceback open to swallow later lines.
	if st.python.IsInProgress() {
		entry, consumed := st.python.Parse(source, line, timestamp)
		if entry != nil {
			entry.SourceType = sourceType
			entries = append(entries, entry)
		}
		if consumed {
			return entries
		}
		// Not consumed: this line ended the traceback but is a log line in its
		// own right, so fall through and parse it normally. Previously it was
		// dropped -- the comment said "this line should be parsed separately"
		// and nothing ever did.
	}

	// Try JSON parser first (most structured)
	if entry, ok := m.jsonParser.Parse(source, line, timestamp); ok {
		entry.SourceType = sourceType
		return append(entries, entry)
	}

	// Try the Python parser, which may start a new traceback.
	entry, consumed := st.python.Parse(source, line, timestamp)
	if entry != nil {
		entry.SourceType = sourceType
		entries = append(entries, entry)
	}
	if consumed {
		// Line became part of a traceback (possibly starting one), so there is
		// nothing to emit as plain text.
		return entries
	}

	// Fall back to plain text parser
	plain := m.plainTextParser.Parse(source, line, timestamp)
	plain.SourceType = sourceType
	return append(entries, plain)
}

// Flush returns any in-progress multi-line entry for a source, or nil.
func (m *MultiParser) Flush(source string) *LogEntry {
	st := m.stateFor(source)
	st.mu.Lock()
	defer st.mu.Unlock()

	return st.python.Flush(source)
}

// ParseLine is a convenience function that creates a parser and parses a
// single line. It returns the entries produced; see MultiParser.ParseLine.
func ParseLine(source string, line string) []*LogEntry {
	parser := NewMultiParser()
	return parser.ParseLine(source, line, time.Now())
}
