package parser

import (
	"regexp"
	"strings"
	"time"
)

var (
	// Patterns to detect log levels in plain text.
	//
	// Written in lower case and matched against the lower-cased line, rather
	// than each carrying (?i). Case-insensitive matching defeats the regexp
	// package's literal-prefix scan and folds every rune it compares, and this
	// runs on every line captured: lowering the line once made classification
	// about 40% faster. Joining each level into one alternation was tried too,
	// and was slower -- an alternation has no literal prefix to scan for.
	errorPatterns = []*regexp.Regexp{
		regexp.MustCompile(`\b(error|err|exception|fatal|panic|failed|failure)\b`),
		regexp.MustCompile(`\[error\]`),
		regexp.MustCompile(`\berror:`),

		// Failures that contain none of the words above. Without these, a
		// process could die with a perfectly clear message and still be
		// classified info -- observed with "nc: Address already in use", which
		// left /errors empty while the process sat there with exit code 1.
		//
		// Kept to phrases that are unambiguously failures. Tempting additions
		// like "not found" are excluded: a web server logging a 404 is not an
		// error, and a false positive is worse than a miss here, because it
		// fills /errors with noise and trains the reader to ignore it.
		regexp.MustCompile(`address (already )?in use`),
		regexp.MustCompile(`permission denied`),
		regexp.MustCompile(`connection refused`),
		regexp.MustCompile(`command not found`),
		regexp.MustCompile(`no such file or directory`),
		regexp.MustCompile(`cannot (bind|allocate|access|open)`),
		regexp.MustCompile(`segmentation fault`),
		regexp.MustCompile(`out of memory`),
		regexp.MustCompile(`read-only file system`),
		regexp.MustCompile(`no space left on device`),
		regexp.MustCompile(`too many open files`),
		regexp.MustCompile(`\btraceback\b`),
		regexp.MustCompile(`unhandled ?(exception|rejection|promise)`),
	}

	// errnoPattern is apart from errorPatterns because it is matched against
	// the line as written, not lower-cased.
	//
	// Grouped, and case-sensitive. Written as
	// (?i)\bEADDRINUSE|EACCES|ECONNREFUSED|ENOENT\b, the word boundaries bound
	// only the first and last arms, so EACCES and ECONNREFUSED matched mid-word
	// -- "reaccessing the cache" was an error. And errno names are always upper
	// case, so matching them case-insensitively only ever added false positives:
	// a path component spelled "eacces" is not a permission error.
	errnoPattern = regexp.MustCompile(`\b(EADDRINUSE|EACCES|ECONNREFUSED|ENOENT)\b`)

	warnPatterns = []*regexp.Regexp{
		regexp.MustCompile(`\b(warn|warning|deprecated)\b`),
		regexp.MustCompile(`\[warn\]`),
		regexp.MustCompile(`\bwarning:`),
	}

	// traceIDPattern finds a trace ID annotation: trace_id=, trace-id=, traceId:,
	// or a bare trace: followed by something ID-shaped.
	//
	// Compiled once. It used to be compiled inside Parse, on every info line,
	// which is the most common line there is.
	//
	// The leading \b stops it matching inside "backtrace", and the value must be
	// at least 8 characters and contain a digit (checked in extractTraceID): the
	// bare "trace:" form otherwise turns "backtrace: disabled" into the trace ID
	// "disabled". Hex IDs and UUIDs always pass both tests.
	traceIDPattern = regexp.MustCompile(`(?i)\btrace(?:[_-]?id)?\s*[=:]\s*([a-z0-9][a-z0-9_.\-]{7,})`)

	// zeroFailurePattern matches a test runner reporting that nothing failed.
	// "0 failed" contains the word failed, and every green run prints it --
	// pytest, go test, jest, cargo -- so without these /errors filled up with
	// successes, which is the false positive the error patterns' own comment
	// ranks worse than a miss. The spans are removed before classifying, so a
	// line reporting both "0 failed" and a real error is still an error.
	zeroFailurePattern = regexp.MustCompile(`(?i)\b0\s+(failed|failures?|errors?)\b` +
		`|\b(failed|failures?|errors?)\s*[:=]\s*0\b`)

	debugPatterns = []*regexp.Regexp{
		regexp.MustCompile(`\b(debug|trace|verbose)\b`),
		regexp.MustCompile(`\[debug\]`),
		regexp.MustCompile(`\bdebug:`),
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

	// The trace ID is taken before the level is decided, because deciding the
	// level returns early. Extraction used to sit at the bottom, so only lines
	// that fell through to info ever got one -- errors and warnings, the lines
	// anyone wants to correlate, never did.
	var traceSpan []int
	entry.TraceID, traceSpan = extractTraceID(line)

	// Classify what the line says, not its annotations. A "trace-id=..." token
	// otherwise matched the debug pattern's \btrace\b, and a test summary's
	// "0 failed" matched the error patterns.
	line = classificationText(line, traceSpan)

	// Lowered after the annotations are cut out, not before: the trace span
	// indexes the original bytes, and lowering can change a line's length.
	lineLower := strings.ToLower(line)

	if errnoPattern.MatchString(line) {
		entry.Level = LevelError
		entry.IsError = true
		return entry
	}

	// Check for error patterns
	for _, pattern := range errorPatterns {
		if pattern.MatchString(lineLower) {
			entry.Level = LevelError
			entry.IsError = true
			return entry
		}
	}

	// Check for warning patterns
	for _, pattern := range warnPatterns {
		if pattern.MatchString(lineLower) {
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
		if pattern.MatchString(lineLower) {
			entry.Level = LevelDebug
			return entry
		}
	}

	// Default to info. An explicit "[info]"/"info:" check that sat here is
	// gone: it returned the same answer as falling through.
	entry.Level = LevelInfo
	return entry
}

// extractTraceID returns the trace ID annotated on a line and the span of the
// whole annotation, or "" and nil.
func extractTraceID(line string) (string, []int) {
	m := traceIDPattern.FindStringSubmatchIndex(line)
	if m == nil {
		return "", nil
	}
	id := line[m[2]:m[3]]
	if !strings.ContainsAny(id, "0123456789") {
		return "", nil
	}
	return id, m[:2]
}

// classificationText is the line with its trace annotation and any zero-count
// failure summaries removed, which is what the level patterns should see.
func classificationText(line string, traceSpan []int) string {
	if traceSpan != nil {
		line = line[:traceSpan[0]] + line[traceSpan[1]:]
	}
	return zeroFailurePattern.ReplaceAllString(line, "")
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
