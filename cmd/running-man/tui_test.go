package main

import (
	"fmt"
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
			result := renderLogs(tt.logs, tt.height, tt.width, 0, "")

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

	result := renderLogs(logs, 10, 80, 0, "")
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
	result := renderLogs(logs, 4, 80, 0, "")
	lines := strings.Split(strings.TrimSpace(result), "\n")

	// Should show only the last 4 lines
	if len(lines) != 4 {
		t.Errorf("Expected 4 lines (height limit), got %d", len(lines))
	}
}

func TestRenderLogs_EmptyLogs(t *testing.T) {
	result := renderLogs([]logEntry{}, 10, 80, 0, "")

	if !strings.Contains(result, "No logs yet") {
		t.Errorf("Expected 'No logs yet' message, got: %s", result)
	}
}

func TestRenderLogs_ScrollOffset(t *testing.T) {
	// Create logs that produce 10 lines total
	logs := []logEntry{}
	for i := 1; i <= 10; i++ {
		logs = append(logs, logEntry{
			Timestamp: fmt.Sprintf("2026-02-08T12:34:%02dZ", i),
			Level:     "INFO",
			Message:   fmt.Sprintf("Line %d", i),
			IsError:   false,
		})
	}

	height := 5 // Show only 5 lines at a time

	// Test showing most recent (scrollOffset = 0)
	result := renderLogs(logs, height, 80, 0, "")
	lines := strings.Split(strings.TrimSpace(result), "\n")
	if len(lines) != 5 {
		t.Errorf("Expected 5 lines with scrollOffset=0, got %d", len(lines))
	}
	// Should contain "Line 10" (most recent)
	stripped := stripANSI(result)
	if !strings.Contains(stripped, "Line 10") {
		t.Errorf("scrollOffset=0 should show most recent log (Line 10)")
	}

	// Test scrolling up 2 lines (scrollOffset = 2)
	result = renderLogs(logs, height, 80, 2, "")
	lines = strings.Split(strings.TrimSpace(result), "\n")
	if len(lines) != 5 {
		t.Errorf("Expected 5 lines with scrollOffset=2, got %d", len(lines))
	}
	// Should contain "Line 8" (2 lines before the end)
	stripped = stripANSI(result)
	if !strings.Contains(stripped, "Line 8") {
		t.Errorf("scrollOffset=2 should show Line 8 at bottom, got: %s", stripped)
	}
	if strings.Contains(stripped, "Line 9") || strings.Contains(stripped, "Line 10") {
		t.Errorf("scrollOffset=2 should not show Line 9 or 10")
	}

	// Test scrolling to oldest logs (large scrollOffset)
	result = renderLogs(logs, height, 80, 1000, "")
	lines = strings.Split(strings.TrimSpace(result), "\n")
	if len(lines) != 5 {
		t.Errorf("Expected 5 lines with large scrollOffset, got %d", len(lines))
	}
	// Should contain "Line 1" (oldest)
	stripped = stripANSI(result)
	if !strings.Contains(stripped, "Line 1") {
		t.Errorf("Large scrollOffset should show oldest logs (Line 1)")
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

func TestSortSources(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name: "all groups present",
			input: []string{
				"my-process",
				"the_running_man-web-1",
				"running-man",
				"the_running_man-db-1",
				"another-process",
			},
			expected: []string{
				"running-man",
				"the_running_man-db-1",
				"the_running_man-web-1",
				"another-process",
				"my-process",
			},
		},
		{
			name: "only processes",
			input: []string{
				"worker",
				"api",
				"frontend",
			},
			expected: []string{
				"api",
				"frontend",
				"worker",
			},
		},
		{
			name: "only docker containers",
			input: []string{
				"myapp-redis-1",
				"myapp-postgres-1",
				"myapp-nginx-1",
			},
			expected: []string{
				"myapp-nginx-1",
				"myapp-postgres-1",
				"myapp-redis-1",
			},
		},
		{
			name: "running-man only",
			input: []string{
				"running-man",
			},
			expected: []string{
				"running-man",
			},
		},
		{
			name: "mixed with underscores",
			input: []string{
				"simple",
				"my_app_web_1",
				"running-man",
				"my_app_db_1",
			},
			expected: []string{
				"running-man",
				"my_app_db_1",
				"my_app_web_1",
				"simple",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sortSources(tt.input)

			if len(result) != len(tt.expected) {
				t.Fatalf("Expected %d sources, got %d", len(tt.expected), len(result))
			}

			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("Position %d: expected %q, got %q\nFull result: %v\nExpected: %v",
						i, tt.expected[i], result[i], result, tt.expected)
				}
			}
		})
	}
}

func TestIsDockerContainer(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		// Docker Compose containers (should be true)
		{"compose with dash", "myapp-web-1", true},
		{"compose with underscore", "my_app_web_1", true},
		{"compose multiple services", "project-redis-server-1", true},
		{"compose with hash", "myapp-web-a1b2c3d4e5f6", true},

		// Not Docker containers (should be false)
		{"simple name", "web", false},
		{"single dash", "my-process", false},
		{"two parts", "app-server", false},
		{"running-man", "running-man", false},
		{"process name", "python-server", false},
		{"short name", "api", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isDockerContainer(tt.input)
			if result != tt.expected {
				t.Errorf("isDockerContainer(%q) = %v, expected %v", tt.input, result, tt.expected)
			}
		})
	}
}
