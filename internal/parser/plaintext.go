package parser

import (
	"regexp"
	"strings"
	"time"
)

var (
	// Patterns to detect log levels in plain text
	errorPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(error|err|exception|fatal|panic|failed|failure)\b`),
		regexp.MustCompile(`(?i)\[error\]`),
		regexp.MustCompile(`(?i)\berror:`),
	}

	warnPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(warn|warning|deprecated)\b`),
		regexp.MustCompile(`(?i)\[warn\]`),
		regexp.MustCompile(`(?i)\bwarning:`),
	}

	debugPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(debug|trace|verbose)\b`),
		regexp.MustCompile(`(?i)\[debug\]`),
		regexp.MustCompile(`(?i)\bdebug:`),
	}
)

// PlainTextParser parses unstructured plain text logs
type PlainTextParser struct{}

// NewPlainTextParser creates a new plain text parser
func NewPlainTextParser() *PlainTextParser {
	return &PlainTextParser{}
}

// Parse parses a plain text log line with heuristic level detection
func (p *PlainTextParser) Parse(source string, line string, timestamp time.Time) *LogEntry {
	entry := &LogEntry{
		Timestamp: timestamp,
		Source:    source,
		Message:   line,
		Raw:       line,
	}

	// Detect level based on content
	lineLower := strings.ToLower(line)

	// Check for error patterns
	for _, pattern := range errorPatterns {
		if pattern.MatchString(line) {
			entry.Level = LevelError
			entry.IsError = true
			return entry
		}
	}

	// Check for warning patterns
	for _, pattern := range warnPatterns {
		if pattern.MatchString(line) {
			entry.Level = LevelWarn
			return entry
		}
	}

	// Check for debug patterns
	for _, pattern := range debugPatterns {
		if pattern.MatchString(line) {
			entry.Level = LevelDebug
			return entry
		}
	}

	// Check for common structured formats
	if strings.Contains(lineLower, "[info]") || strings.Contains(lineLower, "info:") {
		entry.Level = LevelInfo
		return entry
	}

	// Default to info
	entry.Level = LevelInfo

	// Try to extract trace_id from plain text (common patterns)
	// Look for patterns like "trace_id=abc123", "trace: abc123", "traceId=abc123"
	// Support alphanumeric strings, hex strings, and UUIDs (with hyphens)
	traceIDRegex := regexp.MustCompile(`(?i)(?:trace[_-]?id|trace)[=:]\s*([a-zA-Z0-9\-_\.]+)`)
	if matches := traceIDRegex.FindStringSubmatch(line); matches != nil && len(matches) > 1 {
		entry.TraceID = matches[1]
	}

	return entry
}

// parseLevel converts a string level to LogLevel
func parseLevel(levelStr string) LogLevel {
	switch strings.ToLower(strings.TrimSpace(levelStr)) {
	case "debug", "trace", "verbose":
		return LevelDebug
	case "info", "information":
		return LevelInfo
	case "warn", "warning":
		return LevelWarn
	case "error", "err", "fatal", "panic", "critical":
		return LevelError
	default:
		return LevelInfo
	}
}
