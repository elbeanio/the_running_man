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

		// Failures that contain none of the words above. Without these, a
		// process could die with a perfectly clear message and still be
		// classified info -- observed with "nc: Address already in use", which
		// left /errors empty while the process sat there with exit code 1.
		//
		// Kept to phrases that are unambiguously failures. Tempting additions
		// like "not found" are excluded: a web server logging a 404 is not an
		// error, and a false positive is worse than a miss here, because it
		// fills /errors with noise and trains the reader to ignore it.
		regexp.MustCompile(`(?i)address (already )?in use`),
		regexp.MustCompile(`(?i)permission denied`),
		regexp.MustCompile(`(?i)connection refused`),
		regexp.MustCompile(`(?i)command not found`),
		regexp.MustCompile(`(?i)no such file or directory`),
		regexp.MustCompile(`(?i)cannot (bind|allocate|access|open)`),
		regexp.MustCompile(`(?i)segmentation fault`),
		regexp.MustCompile(`(?i)out of memory`),
		regexp.MustCompile(`(?i)read-only file system`),
		regexp.MustCompile(`(?i)no space left on device`),
		regexp.MustCompile(`(?i)too many open files`),
		regexp.MustCompile(`(?i)\btraceback\b`),
		regexp.MustCompile(`(?i)unhandled ?(exception|rejection|promise)`),
		regexp.MustCompile(`(?i)\bEADDRINUSE|EACCES|ECONNREFUSED|ENOENT\b`),
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

// Parse parses a plain text log line with heuristic level detection.
//
// isStderr raises the floor to warn when nothing else matches. It deliberately
// does NOT force an error: plenty of well-behaved tools write ordinary progress
// to stderr -- npm, pip, webpack, git, curl -- so treating stderr as an error
// would fill /errors with noise and make it useless. Warn means "visible if you
// look, not shouted about", which is the honest reading of "this came from
// stderr and said nothing else identifiable".
func (p *PlainTextParser) Parse(source string, line string, timestamp time.Time, isStderr bool) *LogEntry {
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

	if isStderr {
		entry.Level = LevelWarn
		return entry
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
	if matches := traceIDRegex.FindStringSubmatch(line); len(matches) > 1 {
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
