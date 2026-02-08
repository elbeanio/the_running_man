package main

import (
	"strings"
	"testing"
)

func TestRenderLogs_MultilineMessages(t *testing.T) {
	tests := []struct {
		name      string
		logs      []logEntry
		height    int
		width     int
		wantLines int // Expected number of rendered lines
	}{
		{
			name: "single line message",
			logs: []logEntry{
				{
					Timestamp: "2026-02-08T12:34:56Z",
					Level:     "INFO",
					Message:   "Single line",
					IsError:   false,
				},
			},
			height:    10,
			width:     80,
			wantLines: 1,
		},
		{
			name: "multiline message",
			logs: []logEntry{
				{
					Timestamp: "2026-02-08T12:34:56Z",
					Level:     "ERROR",
					Message:   "Line 1\nLine 2\nLine 3",
					IsError:   true,
				},
			},
			height:    10,
			width:     80,
			wantLines: 3,
		},
		{
			name: "multiple logs with multiline",
			logs: []logEntry{
				{
					Timestamp: "2026-02-08T12:34:56Z",
					Level:     "INFO",
					Message:   "First log",
					IsError:   false,
				},
				{
					Timestamp: "2026-02-08T12:34:57Z",
					Level:     "ERROR",
					Message:   "Error line 1\nError line 2",
					IsError:   true,
				},
			},
			height:    10,
			width:     80,
			wantLines: 3, // 1 from first log + 2 from second log
		},
		{
			name: "empty message",
			logs: []logEntry{
				{
					Timestamp: "2026-02-08T12:34:56Z",
					Level:     "INFO",
					Message:   "",
					IsError:   false,
				},
			},
			height:    10,
			width:     80,
			wantLines: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := renderLogs(tt.logs, tt.height, tt.width)

			// Count lines in the rendered output
			// lipgloss uses \n to join lines
			lines := strings.Split(strings.TrimSpace(result), "\n")

			if len(lines) != tt.wantLines {
				t.Errorf("renderLogs() produced %d lines, want %d\nOutput:\n%s",
					len(lines), tt.wantLines, result)
			}
		})
	}
}

func TestRenderLogs_ContinuationIndentation(t *testing.T) {
	logs := []logEntry{
		{
			Timestamp: "2026-02-08T12:34:56Z",
			Level:     "ERROR",
			Message:   "First line\nSecond line\nThird line",
			IsError:   true,
		},
	}

	result := renderLogs(logs, 10, 80)
	lines := strings.Split(strings.TrimSpace(result), "\n")

	if len(lines) != 3 {
		t.Fatalf("Expected 3 lines, got %d", len(lines))
	}

	// First line should have timestamp and level
	if !strings.Contains(lines[0], "12:34:56") || !strings.Contains(lines[0], "ERROR") {
		t.Errorf("First line missing timestamp/level: %s", lines[0])
	}

	// Continuation lines should be indented (20 spaces)
	// Note: lipgloss might add ANSI codes, so we check for spaces at the start
	for i := 1; i < len(lines); i++ {
		// Strip ANSI codes for checking
		stripped := stripANSI(lines[i])
		if !strings.HasPrefix(stripped, "                    ") {
			t.Errorf("Line %d not indented: %s", i+1, stripped)
		}
	}
}

func TestRenderLogs_HeightLimit(t *testing.T) {
	// Create logs that exceed height
	logs := []logEntry{
		{
			Timestamp: "2026-02-08T12:34:56Z",
			Level:     "INFO",
			Message:   "Line 1\nLine 2\nLine 3",
			IsError:   false,
		},
		{
			Timestamp: "2026-02-08T12:34:57Z",
			Level:     "INFO",
			Message:   "Line 4\nLine 5\nLine 6",
			IsError:   false,
		},
	}

	// Total lines: 6, but height limit is 4
	result := renderLogs(logs, 4, 80)
	lines := strings.Split(strings.TrimSpace(result), "\n")

	// Should show only the last 4 lines
	if len(lines) != 4 {
		t.Errorf("Expected 4 lines (height limit), got %d", len(lines))
	}
}

func TestRenderLogs_EmptyLogs(t *testing.T) {
	result := renderLogs([]logEntry{}, 10, 80)

	if !strings.Contains(result, "No logs yet") {
		t.Errorf("Expected 'No logs yet' message, got: %s", result)
	}
}

// stripANSI removes ANSI escape codes from a string
func stripANSI(s string) string {
	// Simple ANSI stripper - matches ESC[...m sequences
	result := ""
	inEscape := false
	for i := 0; i < len(s); i++ {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			inEscape = true
			i++ // skip the '['
			continue
		}
		if inEscape {
			if s[i] == 'm' {
				inEscape = false
			}
			continue
		}
		result += string(s[i])
	}
	return result
}
