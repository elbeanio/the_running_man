package parser

import (
	"encoding/json"
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

	// Check if it's an error
	if entry.Level == LevelError {
		entry.IsError = true
	}

	// Extract stacktrace if present
	if stack, ok := data["stacktrace"].(string); ok {
		entry.Stacktrace = stack
		entry.IsError = true
	} else if stack, ok := data["stack"].(string); ok {
		entry.Stacktrace = stack
		entry.IsError = true
	} else if stack, ok := data["error"].(string); ok && len(stack) > 100 {
		// If error field is long, it might be a stack trace
		entry.Stacktrace = stack
		entry.IsError = true
	}

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
