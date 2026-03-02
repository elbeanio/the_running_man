package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
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
			result := renderLogs(tt.logs, tt.height, tt.width, 0, "", -1, true)

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

	result := renderLogs(logs, 10, 80, 0, "", -1, true)
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
	result := renderLogs(logs, 4, 80, 0, "", -1, true)
	lines := strings.Split(strings.TrimSpace(result), "\n")

	// Should show only the last 4 lines
	if len(lines) != 4 {
		t.Errorf("Expected 4 lines (height limit), got %d", len(lines))
	}
}

func TestRenderLogs_EmptyLogs(t *testing.T) {
	result := renderLogs([]logEntry{}, 10, 80, 0, "", -1, true)

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
	result := renderLogs(logs, height, 80, 0, "", -1, true)
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
	result = renderLogs(logs, height, 80, 2, "", -1, true)
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
	result = renderLogs(logs, height, 80, 1000, "", -1, true)
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

func TestMode(t *testing.T) {
	if ModeNormal != 0 {
		t.Errorf("ModeNormal should be 0, got %d", ModeNormal)
	}
	if ModeSearch != 1 {
		t.Errorf("ModeSearch should be 1, got %d", ModeSearch)
	}
}

func TestSearchInput_InitialState(t *testing.T) {
	m := initialModel("http://localhost:9000", nil)

	// Should start in normal mode
	if m.mode != ModeNormal {
		t.Errorf("Expected ModeNormal initially, got %v", m.mode)
	}

	// Search query should be empty
	if m.searchQuery != "" {
		t.Errorf("Expected empty searchQuery initially, got %q", m.searchQuery)
	}

	// searchInput should be focused (textinput tracks this internally)
	// We can verify it exists and has empty value
	if m.searchInput.Value() != "" {
		t.Errorf("Expected textinput to have empty value initially, got %q", m.searchInput.Value())
	}
}

func TestSearchInput_ModelHasTextinput(t *testing.T) {
	m := initialModel("http://localhost:9000", nil)

	// Verify textinput field exists
	var _ textinput.Model = m.searchInput
}

// --- Tests for n/p navigation and match-line indexing ---

func makeTestLogs() []logEntry {
	return []logEntry{
		{Timestamp: "2026-03-01T10:00:01Z", Level: "INFO", Message: "apple banana cherry"},
		{Timestamp: "2026-03-01T10:00:02Z", Level: "INFO", Message: "banana split\nbanana bread"},
		{Timestamp: "2026-03-01T10:00:03Z", Level: "INFO", Message: "cherry pie"},
		{Timestamp: "2026-03-01T10:00:04Z", Level: "INFO", Message: "no fruit here"},
		{Timestamp: "2026-03-01T10:00:05Z", Level: "INFO", Message: "banana foster"},
	}
}

func TestBuildMatchLineIndex_SingleLineMatches(t *testing.T) {
	logs := makeTestLogs()
	// "banana" appears on:
	//   line 0: "apple banana cherry"      → 1 occurrence (global 0)
	//   line 1: "banana split"             → 1 occurrence (global 1)
	//   line 2: "banana bread"             → 1 occurrence (global 2)  [continuation line of log[1]]
	//   line 4: "banana foster"            → 1 occurrence (global 3)
	idx := buildMatchLineIndex(logs, 120, "banana")
	if len(idx) != 4 {
		t.Fatalf("expected 4 match positions, got %d: %v", len(idx), idx)
	}
	// Line 0 = log[0]
	if idx[0] != 0 {
		t.Errorf("match 0 should be on rendered line 0, got %d", idx[0])
	}
	// Line 1 = first rendered line of log[1] ("banana split")
	if idx[1] != 1 {
		t.Errorf("match 1 should be on rendered line 1, got %d", idx[1])
	}
	// Line 2 = continuation line of log[1] ("banana bread")
	if idx[2] != 2 {
		t.Errorf("match 2 should be on rendered line 2, got %d", idx[2])
	}
	// Line 4 = log[4] ("banana foster"); log[2]="cherry pie" is line 3, log[3]="no fruit here" is line 4... wait:
	// log[0] → line 0
	// log[1] → lines 1,2 (split on \n)
	// log[2] → line 3
	// log[3] → line 4
	// log[4] → line 5
	if idx[3] != 5 {
		t.Errorf("match 3 should be on rendered line 5, got %d", idx[3])
	}
}

func TestBuildMatchLineIndex_NoMatches(t *testing.T) {
	logs := makeTestLogs()
	idx := buildMatchLineIndex(logs, 120, "zzznomatch")
	if len(idx) != 0 {
		t.Errorf("expected 0 matches, got %d", len(idx))
	}
}

func TestBuildMatchLineIndex_EmptyQuery(t *testing.T) {
	logs := makeTestLogs()
	idx := buildMatchLineIndex(logs, 120, "")
	if len(idx) != 0 {
		t.Errorf("expected 0 matches for empty query, got %d", len(idx))
	}
}

func TestBuildMatchLineIndex_MultipleOccurrencesOnOneLine(t *testing.T) {
	logs := []logEntry{
		{Timestamp: "2026-03-01T10:00:01Z", Level: "INFO", Message: "foo foo foo"},
	}
	idx := buildMatchLineIndex(logs, 120, "foo")
	if len(idx) != 3 {
		t.Fatalf("expected 3 match positions, got %d: %v", len(idx), idx)
	}
	// All three on rendered line 0
	for i, lineIdx := range idx {
		if lineIdx != 0 {
			t.Errorf("match %d should be on line 0, got %d", i, lineIdx)
		}
	}
}

func TestBuildMatchLineIndex_CaseInsensitive(t *testing.T) {
	logs := []logEntry{
		{Timestamp: "2026-03-01T10:00:01Z", Level: "INFO", Message: "Hello HELLO hello"},
	}
	idx := buildMatchLineIndex(logs, 120, "hello")
	if len(idx) != 3 {
		t.Fatalf("expected 3 matches (case-insensitive), got %d", len(idx))
	}
}

func TestHighlightMatchesWithCurrent_CurrentMatchStyle(t *testing.T) {
	line := "foo bar foo"
	result := highlightMatchesWithCurrent(line, "foo", 0, 0) // lineMatchOffset=0, currentMatchIdx=0 → first "foo" is current
	// Text content must be preserved regardless of styling
	stripped := stripANSI(result)
	if stripped != line {
		t.Errorf("text content should be preserved, got %q", stripped)
	}
}

func TestHighlightMatchesWithCurrent_NonCurrentMatchStyle(t *testing.T) {
	line := "foo bar foo"
	// lineMatchOffset=0, currentMatchIdx=5 (some other match) → neither "foo" here is current
	result := highlightMatchesWithCurrent(line, "foo", 0, 5)
	// Text content must be preserved regardless of styling
	stripped := stripANSI(result)
	if stripped != line {
		t.Errorf("text content should be preserved, got %q", stripped)
	}
}

func TestHighlightMatchesWithCurrent_PreservesAllText(t *testing.T) {
	// Verify the full line text is preserved with multiple matches at various positions
	tests := []struct {
		line            string
		query           string
		lineMatchOffset int
		currentMatchIdx int
	}{
		{"hello world hello", "hello", 0, 0}, // first is current
		{"hello world hello", "hello", 0, 1}, // second is current
		{"hello world hello", "hello", 3, 3}, // offset=3, first on this line is current
		{"no match here", "zzz", 0, 0},       // no matches
		{"UPPER lower Upper", "upper", 0, 1}, // case-insensitive
	}
	for _, tt := range tests {
		result := highlightMatchesWithCurrent(tt.line, tt.query, tt.lineMatchOffset, tt.currentMatchIdx)
		stripped := stripANSI(result)
		if stripped != tt.line {
			t.Errorf("highlightMatchesWithCurrent(%q, %q, %d, %d): text content changed: got %q",
				tt.line, tt.query, tt.lineMatchOffset, tt.currentMatchIdx, stripped)
		}
	}
}

func TestHighlightMatchesWithCurrent_NoQuery(t *testing.T) {
	line := "hello world"
	result := highlightMatchesWithCurrent(line, "", 0, 0)
	if result != line {
		t.Errorf("empty query should return line unchanged, got %q", result)
	}
}

func TestNormalMode_N_IncreasesMatchIdx(t *testing.T) {
	m := initialModel("http://localhost:9000", nil)
	m.logs = makeTestLogs()
	m.searchQuery = "banana"
	m.searchMatchIdx = 0
	m.height = 40
	m.width = 120

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")}
	newModel, _ := m.updateNormalMode(msg)
	nm := newModel.(model)

	if nm.searchMatchIdx != 1 {
		t.Errorf("expected searchMatchIdx=1 after pressing n, got %d", nm.searchMatchIdx)
	}
}

func TestNormalMode_P_DecreasesMatchIdx(t *testing.T) {
	m := initialModel("http://localhost:9000", nil)
	m.logs = makeTestLogs()
	m.searchQuery = "banana"
	m.searchMatchIdx = 2
	m.height = 40
	m.width = 120

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")}
	newModel, _ := m.updateNormalMode(msg)
	nm := newModel.(model)

	if nm.searchMatchIdx != 1 {
		t.Errorf("expected searchMatchIdx=1 after pressing p, got %d", nm.searchMatchIdx)
	}
}

func TestNormalMode_N_WrapAround(t *testing.T) {
	m := initialModel("http://localhost:9000", nil)
	m.logs = makeTestLogs()
	m.searchQuery = "banana"
	m.searchMatchIdx = 3 // last match (0-indexed, 4 total)
	m.height = 40
	m.width = 120

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")}
	newModel, _ := m.updateNormalMode(msg)
	nm := newModel.(model)

	if nm.searchMatchIdx != 0 {
		t.Errorf("expected searchMatchIdx to wrap to 0, got %d", nm.searchMatchIdx)
	}
}

func TestNormalMode_P_WrapAround(t *testing.T) {
	m := initialModel("http://localhost:9000", nil)
	m.logs = makeTestLogs()
	m.searchQuery = "banana"
	m.searchMatchIdx = 0
	m.height = 40
	m.width = 120

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")}
	newModel, _ := m.updateNormalMode(msg)
	nm := newModel.(model)

	if nm.searchMatchIdx != 3 {
		t.Errorf("expected searchMatchIdx to wrap to 3 (last), got %d", nm.searchMatchIdx)
	}
}

func TestNormalMode_N_NoQuery_NoOp(t *testing.T) {
	m := initialModel("http://localhost:9000", nil)
	m.logs = makeTestLogs()
	m.searchQuery = "" // no active search
	m.searchMatchIdx = 0

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")}
	newModel, _ := m.updateNormalMode(msg)
	nm := newModel.(model)

	if nm.searchMatchIdx != 0 {
		t.Errorf("expected no change when no query, got %d", nm.searchMatchIdx)
	}
}

func TestNormalMode_N_SetsScrollOffset(t *testing.T) {
	m := initialModel("http://localhost:9000", nil)
	m.logs = makeTestLogs()
	m.searchQuery = "banana"
	m.searchMatchIdx = 0
	m.height = 10
	m.width = 120

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")}
	newModel, _ := m.updateNormalMode(msg)
	nm := newModel.(model)

	// scrollOffset should be non-zero (scrolled to show the match)
	// We don't assert the exact value here, just that it's been set to something reasonable
	// (not negative, and autoScroll is off)
	if nm.scrollOffset < 0 {
		t.Errorf("scrollOffset should not be negative, got %d", nm.scrollOffset)
	}
	if nm.autoScroll {
		t.Errorf("autoScroll should be disabled when navigating matches")
	}
}

func TestRenderLogs_WithCurrentMatchIdx(t *testing.T) {
	logs := []logEntry{
		{Timestamp: "2026-03-01T10:00:01Z", Level: "INFO", Message: "apple banana cherry"},
		{Timestamp: "2026-03-01T10:00:02Z", Level: "INFO", Message: "banana split"},
	}

	// Should not panic, and should contain text
	result := renderLogs(logs, 20, 120, 0, "banana", 0, true)
	stripped := stripANSI(result)
	if !strings.Contains(stripped, "banana") {
		t.Errorf("expected 'banana' in output, got: %s", stripped)
	}
}

func TestCountMatches_IncludesLevelAndTimestamp(t *testing.T) {
	// Regression: searching for a log level (e.g. "info") must count matches in the
	// rendered [HH:MM:SS] [INFO] prefix, not just the message body.
	logs := []logEntry{
		{Timestamp: "2026-03-01T10:00:01Z", Level: "INFO", Message: "startup complete"},
		{Timestamp: "2026-03-01T10:00:02Z", Level: "ERROR", Message: "something failed"},
		{Timestamp: "2026-03-01T10:00:03Z", Level: "INFO", Message: "request handled"},
	}

	count := countMatches(logs, "info")
	if count != 2 {
		t.Errorf("expected 2 matches for 'info' (in [INFO] prefix), got %d", count)
	}

	// And n/p navigation should be non-zero
	m := initialModel("http://localhost:9000", nil)
	m.logs = logs
	m.searchQuery = "info"
	m.searchMatchIdx = 0
	m.height = 20
	m.width = 120

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")}
	newModel, _ := m.updateNormalMode(msg)
	nm := newModel.(model)

	if nm.searchMatchIdx != 1 {
		t.Errorf("expected searchMatchIdx=1 after pressing n with 'info' query, got %d", nm.searchMatchIdx)
	}
}

func TestScrollToMatch_PositionsViewCorrectly(t *testing.T) {
	// Create enough logs that they exceed one screen
	logs := []logEntry{}
	for i := 0; i < 30; i++ {
		msg := fmt.Sprintf("line %d", i)
		if i == 25 {
			msg = "banana target line"
		}
		logs = append(logs, logEntry{
			Timestamp: fmt.Sprintf("2026-03-01T10:00:%02dZ", i%60),
			Level:     "INFO",
			Message:   msg,
		})
	}

	m := initialModel("http://localhost:9000", nil)
	m.logs = logs
	m.searchQuery = "banana"
	m.searchMatchIdx = 0
	m.height = 10
	m.width = 120

	// Navigate to the match
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")}
	newModel, _ := m.updateNormalMode(msg)
	nm := newModel.(model)

	// Render and check the target line is visible
	availableHeight := nm.height - uiHeaderFooterHeight
	result := renderLogs(nm.logs, availableHeight, nm.width, nm.scrollOffset, nm.searchQuery, nm.searchMatchIdx, nm.showTraceIDs)
	stripped := stripANSI(result)
	if !strings.Contains(stripped, "banana target line") {
		t.Errorf("after navigating to match, 'banana target line' should be visible in viewport\nscrollOffset=%d\noutput:\n%s",
			nm.scrollOffset, stripped)
	}
}

func TestRenderLogs_TraceIndicators(t *testing.T) {
	tests := []struct {
		name            string
		logs            []logEntry
		showTraceIDs    bool
		wantContains    string
		wantNotContains string
	}{
		{
			name: "trace indicator shown when enabled and trace_id exists",
			logs: []logEntry{
				{
					Timestamp: "2026-02-08T12:34:56Z",
					Level:     "INFO",
					Message:   "Test message",
					TraceID:   "abc123def456",
				},
			},
			showTraceIDs: true,
			wantContains: "[trace:abc123def456]",
		},
		{
			name: "trace indicator hidden when showTraceIDs is false",
			logs: []logEntry{
				{
					Timestamp: "2026-02-08T12:34:56Z",
					Level:     "INFO",
					Message:   "Test message",
					TraceID:   "abc123def456",
				},
			},
			showTraceIDs:    false,
			wantNotContains: "[trace:abc123def456]",
		},
		{
			name: "no trace indicator when trace_id is empty",
			logs: []logEntry{
				{
					Timestamp: "2026-02-08T12:34:56Z",
					Level:     "INFO",
					Message:   "Test message",
					TraceID:   "",
				},
			},
			showTraceIDs:    true,
			wantNotContains: "[trace:",
		},
		{
			name: "trace indicator truncated when too long",
			logs: []logEntry{
				{
					Timestamp: "2026-02-08T12:34:56Z",
					Level:     "INFO",
					Message:   "Test message",
					TraceID:   "verylongtraceidthatislongerthantwentycharacters",
				},
			},
			showTraceIDs: true,
			wantContains: "[trace:verylongt...]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := renderLogs(tt.logs, 10, 80, 0, "", -1, tt.showTraceIDs)
			stripped := stripANSI(result)

			if tt.wantContains != "" && !strings.Contains(stripped, tt.wantContains) {
				t.Errorf("renderLogs() output should contain %q, got:\n%s", tt.wantContains, stripped)
			}

			if tt.wantNotContains != "" && strings.Contains(stripped, tt.wantNotContains) {
				t.Errorf("renderLogs() output should not contain %q, got:\n%s", tt.wantNotContains, stripped)
			}
		})
	}
}

func TestRenderLogs_TraceIndicatorWidthHandling(t *testing.T) {
	// Test that trace indicators work correctly with width constraints
	logs := []logEntry{
		{
			Timestamp: "2026-02-08T12:34:56Z",
			Level:     "INFO",
			Message:   "A very long message that will need to be truncated when we have a trace indicator in a narrow terminal",
			TraceID:   "abc123",
		},
	}

	// Test with narrow width - message should be truncated to make room for trace indicator
	result := renderLogs(logs, 10, 60, 0, "", -1, true)
	stripped := stripANSI(result)

	// Should contain trace indicator
	if !strings.Contains(stripped, "[trace:abc123]") {
		t.Errorf("renderLogs() should show trace indicator even in narrow terminal, got:\n%s", stripped)
	}

	// Message should be truncated (have "...")
	if !strings.Contains(stripped, "...") {
		t.Errorf("renderLogs() should truncate long message to make room for trace indicator, got:\n%s", stripped)
	}
}

func TestModel_ToggleTraceIndicators(t *testing.T) {
	// Test that 't' key toggles trace indicator visibility
	m := initialModel("http://localhost:8080", nil)
	m.showTraceIDs = true

	// Press 't' to toggle off
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}}
	newModel, _ := m.updateNormalMode(msg)
	nm := newModel.(model)

	if nm.showTraceIDs {
		t.Errorf("Pressing 't' should toggle showTraceIDs from true to false, got %v", nm.showTraceIDs)
	}

	// Press 't' again to toggle back on
	msg2 := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}}
	newModel2, _ := nm.updateNormalMode(msg2)
	nm2 := newModel2.(model)

	if !nm2.showTraceIDs {
		t.Errorf("Pressing 't' again should toggle showTraceIDs from false to true, got %v", nm2.showTraceIDs)
	}
}
