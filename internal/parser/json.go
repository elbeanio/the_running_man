package parser

import (
	"encoding/json"
	"strings"
	"time"
)

// JSONParser parses structured JSON logs
type JSONParser struct{}

// NewJSONParser creates a new JSON parser
func NewJSONParser() *JSONParser {
	return &JSONParser{}
}

// Parse attempts to parse a JSON log line
func (p *JSONParser) Parse(source string, line string, timestamp time.Time) (*LogEntry, bool) {
	// Only an object can be a structured log line. Checked first because this
	// runs on every line, and for the plain-text majority Unmarshal did nothing
	// but build a SyntaxError to throw away.
	if t := strings.TrimLeft(line, " \t"); t == "" || t[0] != '{' {
		return nil, false
	}

	var data map[string]interface{}

	if err := json.Unmarshal([]byte(line), &data); err != nil {
		return nil, false
	}

	entry := &LogEntry{
		Timestamp: timestamp,
		Source:    source,
		Raw:       line,
	}

	// Extract message field (common names)
	if msg, ok := data["message"].(string); ok {
		entry.Message = msg
	} else if msg, ok := data["msg"].(string); ok {
		entry.Message = msg
	} else if msg, ok := data["text"].(string); ok {
		entry.Message = msg
	} else {
		entry.Message = line
	}

	// Extract level (common names and formats)
	levelStr := ""
	if lvl, ok := data["level"].(string); ok {
		levelStr = lvl
	} else if lvl, ok := data["severity"].(string); ok {
		levelStr = lvl
	} else if lvl, ok := data["lvl"].(string); ok {
		levelStr = lvl
	}

	entry.Level = parseLevel(levelStr)

	// Extract stacktrace if present
	if stack, ok := data["stacktrace"].(string); ok {
		entry.Stacktrace = stack
	} else if stack, ok := data["stack"].(string); ok {
		entry.Stacktrace = stack
	} else if stack, ok := data["error"].(string); ok && len(stack) > 100 {
		// If error field is long, it might be a stack trace
		entry.Stacktrace = stack
	}

	// A stack trace makes the entry an error whatever level it was logged at.
	// This used to set a separate IsError flag and leave the level alone, so
	// /errors returned the entry and /logs?level=error did not.
	if entry.Stacktrace != "" {
		entry.Level = LevelError
	}

	// Extract trace_id if present (common OTEL field names)
	if traceID, ok := data["trace_id"].(string); ok && traceID != "" {
		entry.TraceID = traceID
	} else if traceID, ok := data["traceId"].(string); ok && traceID != "" {
		entry.TraceID = traceID
	} else if traceID, ok := data["traceID"].(string); ok && traceID != "" {
		entry.TraceID = traceID
	} else if traceID, ok := data["trace"].(string); ok && traceID != "" {
		entry.TraceID = traceID
	}
	// Deliberately NOT falling back to span_id. It used to be stored in
	// TraceID when no trace_id was present, with a comment conceding that
	// deriving the real trace id "if available" was unimplemented. The effect
	// was that span ids became keys in the ring buffer's trace index, so
	// /traces/{id}/logs silently returned wrong or empty results. A missing
	// trace id is better than a wrong one.

	// Extract timestamp if present
	if ts, ok := data["timestamp"].(string); ok {
		if parsed, err := time.Parse(time.RFC3339, ts); err == nil {
			entry.Timestamp = parsed
		}
	} else if ts, ok := data["ts"].(string); ok {
		if parsed, err := time.Parse(time.RFC3339, ts); err == nil {
			entry.Timestamp = parsed
		}
	} else if ts, ok := data["time"].(string); ok {
		if parsed, err := time.Parse(time.RFC3339, ts); err == nil {
			entry.Timestamp = parsed
		}
	}

	return entry, true
}
