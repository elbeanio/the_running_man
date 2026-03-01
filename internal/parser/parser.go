package parser

import (
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

// LogEntry represents a parsed log entry
type LogEntry struct {
	Timestamp  time.Time
	Level      LogLevel
	Source     string
	Message    string
	Raw        string
	IsError    bool
	Stacktrace string
	TraceID    string
}

// MultiParser manages multiple parsers and maintains state
type MultiParser struct {
	jsonParser      *JSONParser
	pythonParser    *PythonParser
	plainTextParser *PlainTextParser
}

// NewMultiParser creates a new multi-format parser
func NewMultiParser() *MultiParser {
	return &MultiParser{
		jsonParser:      NewJSONParser(),
		pythonParser:    NewPythonParser(),
		plainTextParser: NewPlainTextParser(),
	}
}

// ParseLine attempts to parse a log line using available parsers
func (m *MultiParser) ParseLine(source string, line string, timestamp time.Time) *LogEntry {
	// Try JSON parser first (most structured)
	if entry, ok := m.jsonParser.Parse(source, line, timestamp); ok {
		return entry
	}

	// Try Python traceback parser (stateful, multi-line)
	if entry, ok := m.pythonParser.Parse(source, line, timestamp); ok {
		return entry
	}

	// If Python parser is accumulating lines, don't return anything yet
	if m.pythonParser.IsInProgress() {
		return nil
	}

	// Fall back to plain text parser
	return m.plainTextParser.Parse(source, line, timestamp)
}

// Flush returns any in-progress multi-line entries
func (m *MultiParser) Flush(source string) *LogEntry {
	return m.pythonParser.Flush(source)
}

// ParseLine is a convenience function that creates a parser and parses a single line
func ParseLine(source string, line string) *LogEntry {
	parser := NewMultiParser()
	return parser.ParseLine(source, line, time.Now())
}
