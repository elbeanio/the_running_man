package storage

import (
	"testing"
	"time"

	"github.com/elbeanio/the_running_man/internal/parser"
)

func TestRingBuffer_BasicAppendAndQuery(t *testing.T) {
	rb := NewRingBuffer(100, 30*time.Minute, 50*1024*1024)

	// Add some entries
	entries := []*parser.LogEntry{
		{
			Timestamp: time.Now(),
			Level:     parser.LevelInfo,
			Source:    "test",
			Message:   "info message",
			Raw:       "info message",
		},
		{
			Timestamp: time.Now(),
			Level:     parser.LevelError,
			Source:    "test",
			Message:   "error message",
			Raw:       "error message",
			IsError:   true,
		},
	}

	for _, entry := range entries {
		rb.Append(entry)
	}

	// Query all entries
	result := rb.Query(QueryFilters{})
	if len(result) != 2 {
		t.Errorf("Expected 2 entries, got %d", len(result))
	}
}

func TestRingBuffer_LevelFiltering(t *testing.T) {
	rb := NewRingBuffer(100, 30*time.Minute, 50*1024*1024)

	// Add entries with different levels
	rb.Append(&parser.LogEntry{
		Timestamp: time.Now(),
		Level:     parser.LevelInfo,
		Message:   "info",
		Raw:       "info",
	})
	rb.Append(&parser.LogEntry{
		Timestamp: time.Now(),
		Level:     parser.LevelError,
		Message:   "error",
		Raw:       "error",
		IsError:   true,
	})
	rb.Append(&parser.LogEntry{
		Timestamp: time.Now(),
		Level:     parser.LevelWarn,
		Message:   "warn",
		Raw:       "warn",
	})

	// Query only errors
	result := rb.Query(QueryFilters{
		Levels: []parser.LogLevel{parser.LevelError},
	})

	if len(result) != 1 {
		t.Errorf("Expected 1 error entry, got %d", len(result))
	}

	if result[0].Level != parser.LevelError {
		t.Errorf("Expected error level, got %s", result[0].Level)
	}
}

func TestRingBuffer_TimeFiltering(t *testing.T) {
	rb := NewRingBuffer(100, 30*time.Minute, 50*1024*1024)

	// Add old entry
	rb.Append(&parser.LogEntry{
		Timestamp: time.Now().Add(-10 * time.Minute),
		Level:     parser.LevelInfo,
		Message:   "old",
		Raw:       "old",
	})

	// Add recent entry
	rb.Append(&parser.LogEntry{
		Timestamp: time.Now(),
		Level:     parser.LevelInfo,
		Message:   "recent",
		Raw:       "recent",
	})

	// Query last 5 minutes
	result := rb.Query(QueryFilters{
		Since: 5 * time.Minute,
	})

	if len(result) != 1 {
		t.Errorf("Expected 1 recent entry, got %d", len(result))
	}

	if result[0].Message != "recent" {
		t.Errorf("Expected 'recent' message, got %s", result[0].Message)
	}
}

func TestRingBuffer_ContainsFiltering(t *testing.T) {
	rb := NewRingBuffer(100, 30*time.Minute, 50*1024*1024)

	rb.Append(&parser.LogEntry{
		Timestamp: time.Now(),
		Message:   "database connection failed",
		Raw:       "database connection failed",
	})
	rb.Append(&parser.LogEntry{
		Timestamp: time.Now(),
		Message:   "server started",
		Raw:       "server started",
	})

	// Query for database errors
	result := rb.Query(QueryFilters{
		Contains: "database",
	})

	if len(result) != 1 {
		t.Errorf("Expected 1 entry containing 'database', got %d", len(result))
	}
}

func TestRingBuffer_ErrorsOnlyFiltering(t *testing.T) {
	rb := NewRingBuffer(100, 30*time.Minute, 50*1024*1024)

	rb.Append(&parser.LogEntry{
		Timestamp: time.Now(),
		Level:     parser.LevelInfo,
		Message:   "info",
		Raw:       "info",
		IsError:   false,
	})
	rb.Append(&parser.LogEntry{
		Timestamp: time.Now(),
		Level:     parser.LevelError,
		Message:   "error",
		Raw:       "error",
		IsError:   true,
	})

	result := rb.Query(QueryFilters{
		ErrorsOnly: true,
	})

	if len(result) != 1 {
		t.Errorf("Expected 1 error entry, got %d", len(result))
	}

	if !result[0].IsError {
		t.Error("Expected IsError to be true")
	}
}

func TestRingBuffer_SizeEviction(t *testing.T) {
	// Small buffer: max 3 entries
	rb := NewRingBuffer(3, 30*time.Minute, 50*1024*1024)

	// Add 5 entries
	for i := 1; i <= 5; i++ {
		rb.Append(&parser.LogEntry{
			Timestamp: time.Now(),
			Message:   string(rune('0' + i)),
			Raw:       string(rune('0' + i)),
		})
	}

	// Should only keep last 3
	result := rb.Query(QueryFilters{})
	if len(result) != 3 {
		t.Errorf("Expected 3 entries after eviction, got %d", len(result))
	}

	// Should have entries 3, 4, 5
	if result[0].Message != "3" {
		t.Errorf("Expected oldest entry to be '3', got %s", result[0].Message)
	}
}

func TestRingBuffer_TimeEviction(t *testing.T) {
	// Short retention: 1 second
	rb := NewRingBuffer(100, 1*time.Second, 50*1024*1024)

	// Add old entry
	rb.Append(&parser.LogEntry{
		Timestamp: time.Now().Add(-2 * time.Second),
		Message:   "old",
		Raw:       "old",
	})

	// Add recent entry
	time.Sleep(10 * time.Millisecond)
	rb.Append(&parser.LogEntry{
		Timestamp: time.Now(),
		Message:   "recent",
		Raw:       "recent",
	})

	// Old entry should be evicted
	result := rb.Query(QueryFilters{})
	if len(result) != 1 {
		t.Errorf("Expected 1 entry after time eviction, got %d", len(result))
	}

	if result[0].Message != "recent" {
		t.Errorf("Expected 'recent' entry, got %s", result[0].Message)
	}
}

func TestRingBuffer_ByteSizeEviction(t *testing.T) {
	// Small byte limit: 100 bytes
	rb := NewRingBuffer(1000, 30*time.Minute, 100)

	// Add entries that total >100 bytes
	for i := 0; i < 10; i++ {
		rb.Append(&parser.LogEntry{
			Timestamp: time.Now(),
			Message:   "this is a message that takes up some bytes",
			Raw:       "this is a message that takes up some bytes",
		})
	}

	// Should have evicted old entries
	stats := rb.Stats()
	if stats.TotalBytes > 100 {
		t.Errorf("Expected total bytes <= 100, got %d", stats.TotalBytes)
	}

	if stats.TotalEntries >= 10 {
		t.Errorf("Expected fewer than 10 entries after eviction, got %d", stats.TotalEntries)
	}
}

func TestRingBuffer_Stats(t *testing.T) {
	rb := NewRingBuffer(100, 30*time.Minute, 50*1024*1024)

	rb.Append(&parser.LogEntry{
		Timestamp: time.Now(),
		Message:   "test message",
		Raw:       "test message",
	})

	stats := rb.Stats()

	if stats.TotalEntries != 1 {
		t.Errorf("Expected 1 entry, got %d", stats.TotalEntries)
	}

	if stats.TotalBytes == 0 {
		t.Error("Expected non-zero bytes")
	}

	if stats.MaxEntries != 100 {
		t.Errorf("Expected max entries 100, got %d", stats.MaxEntries)
	}
}

func TestRingBuffer_Clear(t *testing.T) {
	rb := NewRingBuffer(100, 30*time.Minute, 50*1024*1024)

	rb.Append(&parser.LogEntry{
		Timestamp: time.Now(),
		Message:   "test",
		Raw:       "test",
	})

	rb.Clear()

	stats := rb.Stats()
	if stats.TotalEntries != 0 {
		t.Errorf("Expected 0 entries after clear, got %d", stats.TotalEntries)
	}

	if stats.TotalBytes != 0 {
		t.Errorf("Expected 0 bytes after clear, got %d", stats.TotalBytes)
	}
}

func TestRingBuffer_ThreadSafety(t *testing.T) {
	rb := NewRingBuffer(1000, 30*time.Minute, 50*1024*1024)

	// Concurrent writes and reads
	done := make(chan bool)

	// Writer goroutine
	go func() {
		for i := 0; i < 100; i++ {
			rb.Append(&parser.LogEntry{
				Timestamp: time.Now(),
				Message:   "concurrent",
				Raw:       "concurrent",
			})
		}
		done <- true
	}()

	// Reader goroutine
	go func() {
		for i := 0; i < 100; i++ {
			rb.Query(QueryFilters{})
			rb.Stats()
		}
		done <- true
	}()

	// Wait for both to complete
	<-done
	<-done

	// If we get here without panicking, thread safety works
}

func TestRingBuffer_SourceGlobFiltering(t *testing.T) {
	rb := NewRingBuffer(100, 30*time.Minute, 50*1024*1024)

	// Add entries from different sources
	sources := []string{"python-server", "python-worker", "node-api", "go-service", "test-runner"}
	for _, source := range sources {
		rb.Append(&parser.LogEntry{
			Timestamp: time.Now(),
			Source:    source,
			Message:   "log from " + source,
			Raw:       "log from " + source,
		})
	}

	tests := []struct {
		name     string
		pattern  string
		expected int
	}{
		{"exact match", "python-server", 1},
		{"glob all python", "python-*", 2},
		{"glob all with wildcard", "*-server", 1},
		{"glob prefix", "go-*", 1},
		{"glob suffix", "*-runner", 1},
		{"glob all", "*", 5},
		{"no match", "java-*", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := rb.Query(QueryFilters{
				Sources: []string{tt.pattern},
			})

			if len(result) != tt.expected {
				t.Errorf("Pattern '%s': expected %d entries, got %d", tt.pattern, tt.expected, len(result))
			}
		})
	}
}

func TestRingBuffer_MultipleSourceGlobFiltering(t *testing.T) {
	rb := NewRingBuffer(100, 30*time.Minute, 50*1024*1024)

	// Add entries from different sources
	sources := []string{"python-server", "python-worker", "node-api", "go-service"}
	for _, source := range sources {
		rb.Append(&parser.LogEntry{
			Timestamp: time.Now(),
			Source:    source,
			Message:   "log from " + source,
			Raw:       "log from " + source,
		})
	}

	// Query with multiple patterns
	result := rb.Query(QueryFilters{
		Sources: []string{"python-*", "go-*"},
	})

	if len(result) != 3 {
		t.Errorf("Expected 3 entries (2 python + 1 go), got %d", len(result))
	}
}

func TestRingBuffer_ExcludeFiltering(t *testing.T) {
	rb := NewRingBuffer(100, 30*time.Minute, 50*1024*1024)

	// Add entries from different sources
	sources := []string{"python-server", "python-worker", "test-runner", "test-integration"}
	for _, source := range sources {
		rb.Append(&parser.LogEntry{
			Timestamp: time.Now(),
			Source:    source,
			Message:   "log from " + source,
			Raw:       "log from " + source,
		})
	}

	tests := []struct {
		name     string
		exclude  []string
		expected int
	}{
		{"exclude exact", []string{"python-server"}, 3},
		{"exclude glob test", []string{"test-*"}, 2},
		{"exclude multiple", []string{"test-*", "python-worker"}, 1},
		{"exclude all", []string{"*"}, 0},
		{"exclude none matching", []string{"java-*"}, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := rb.Query(QueryFilters{
				Exclude: tt.exclude,
			})

			if len(result) != tt.expected {
				t.Errorf("Exclude %v: expected %d entries, got %d", tt.exclude, tt.expected, len(result))
			}
		})
	}
}

func TestRingBuffer_CombinedSourceAndExclude(t *testing.T) {
	rb := NewRingBuffer(100, 30*time.Minute, 50*1024*1024)

	// Add entries from different sources
	sources := []string{"python-server", "python-test", "python-worker", "node-api"}
	for _, source := range sources {
		rb.Append(&parser.LogEntry{
			Timestamp: time.Now(),
			Source:    source,
			Message:   "log from " + source,
			Raw:       "log from " + source,
		})
	}

	// Query for python-* but exclude test
	result := rb.Query(QueryFilters{
		Sources: []string{"python-*"},
		Exclude: []string{"*-test"},
	})

	if len(result) != 2 {
		t.Errorf("Expected 2 entries (python-server, python-worker), got %d", len(result))
	}

	// Verify the correct entries
	for _, entry := range result {
		if entry.Source == "python-test" {
			t.Errorf("Unexpected entry from python-test (should be excluded)")
		}
	}
}

func TestRingBuffer_GetSources(t *testing.T) {
	rb := NewRingBuffer(100, 30*time.Minute, 50*1024*1024)

	// Add entries from different sources
	now := time.Now()
	rb.Append(&parser.LogEntry{
		Timestamp: now.Add(-2 * time.Minute),
		Source:    "python-server",
		Message:   "log 1",
		Raw:       "log 1",
	})
	rb.Append(&parser.LogEntry{
		Timestamp: now.Add(-1 * time.Minute),
		Source:    "python-server",
		Message:   "log 2",
		Raw:       "log 2",
	})
	rb.Append(&parser.LogEntry{
		Timestamp: now,
		Source:    "node-api",
		Message:   "log 3",
		Raw:       "log 3",
	})

	sources := rb.GetSources()

	if len(sources) != 2 {
		t.Errorf("Expected 2 unique sources, got %d", len(sources))
	}

	// Find python-server source
	var pythonSource *SourceInfo
	for i := range sources {
		if sources[i].Name == "python-server" {
			pythonSource = &sources[i]
			break
		}
	}

	if pythonSource == nil {
		t.Fatal("python-server source not found")
	}

	if pythonSource.EntryCount != 2 {
		t.Errorf("Expected python-server to have 2 entries, got %d", pythonSource.EntryCount)
	}

	// LastSeen should be the most recent timestamp
	if pythonSource.LastSeen.Before(now.Add(-1 * time.Minute)) {
		t.Errorf("Expected LastSeen to be recent, got %v", pythonSource.LastSeen)
	}
}

func TestRingBuffer_InvalidGlobPattern(t *testing.T) {
	rb := NewRingBuffer(100, 30*time.Minute, 50*1024*1024)

	rb.Append(&parser.LogEntry{
		Timestamp: time.Now(),
		Source:    "test-source",
		Message:   "log",
		Raw:       "log",
	})

	// Invalid glob pattern (malformed)
	result := rb.Query(QueryFilters{
		Sources: []string{"[invalid"},
	})

	// Should return 0 results (pattern doesn't match anything and is invalid)
	if len(result) != 0 {
		t.Errorf("Expected 0 entries for invalid glob pattern, got %d", len(result))
	}
}

func TestRingBuffer_TraceCorrelation(t *testing.T) {
	rb := NewRingBuffer(100, 30*time.Minute, 50*1024*1024)

	// Add entries with trace IDs
	entries := []*parser.LogEntry{
		{
			Timestamp: time.Now(),
			Level:     parser.LevelInfo,
			Source:    "service-a",
			Message:   "Processing request",
			Raw:       "Processing request",
			TraceID:   "trace-123",
		},
		{
			Timestamp: time.Now(),
			Level:     parser.LevelError,
			Source:    "service-a",
			Message:   "Database error",
			Raw:       "Database error",
			IsError:   true,
			TraceID:   "trace-123",
		},
		{
			Timestamp: time.Now(),
			Level:     parser.LevelInfo,
			Source:    "service-b",
			Message:   "Cache hit",
			Raw:       "Cache hit",
			TraceID:   "trace-456",
		},
		{
			Timestamp: time.Now(),
			Level:     parser.LevelInfo,
			Source:    "service-c",
			Message:   "No trace ID",
			Raw:       "No trace ID",
			TraceID:   "", // Empty trace ID
		},
	}

	for _, entry := range entries {
		rb.Append(entry)
	}

	// Test GetLogsByTraceID for trace-123
	logs1 := rb.GetLogsByTraceID("trace-123")
	if len(logs1) != 2 {
		t.Errorf("Expected 2 logs for trace-123, got %d", len(logs1))
	}
	for _, log := range logs1 {
		if log.TraceID != "trace-123" {
			t.Errorf("Expected trace ID 'trace-123', got '%s'", log.TraceID)
		}
	}

	// Test GetLogsByTraceID for trace-456
	logs2 := rb.GetLogsByTraceID("trace-456")
	if len(logs2) != 1 {
		t.Errorf("Expected 1 log for trace-456, got %d", len(logs2))
	}
	if len(logs2) > 0 && logs2[0].TraceID != "trace-456" {
		t.Errorf("Expected trace ID 'trace-456', got '%s'", logs2[0].TraceID)
	}

	// Test GetLogsByTraceID for non-existent trace
	logs3 := rb.GetLogsByTraceID("nonexistent")
	if len(logs3) != 0 {
		t.Errorf("Expected 0 logs for nonexistent trace, got %d", len(logs3))
	}

	// Test GetLogsByTraceID for empty trace ID
	logs4 := rb.GetLogsByTraceID("")
	if len(logs4) != 0 {
		t.Errorf("Expected 0 logs for empty trace ID, got %d", len(logs4))
	}

	// Test that eviction cleans up trace index
	// Create a small buffer and add many entries to trigger eviction
	smallRb := NewRingBuffer(2, 30*time.Minute, 50*1024*1024)

	// Add 3 entries with same trace ID (buffer size is 2, so first should be evicted)
	smallRb.Append(&parser.LogEntry{
		Timestamp: time.Now(),
		Level:     parser.LevelInfo,
		Source:    "test",
		Message:   "First",
		Raw:       "First",
		TraceID:   "test-trace",
	})

	smallRb.Append(&parser.LogEntry{
		Timestamp: time.Now(),
		Level:     parser.LevelInfo,
		Source:    "test",
		Message:   "Second",
		Raw:       "Second",
		TraceID:   "test-trace",
	})

	// This should evict the first entry
	smallRb.Append(&parser.LogEntry{
		Timestamp: time.Now(),
		Level:     parser.LevelInfo,
		Source:    "test",
		Message:   "Third",
		Raw:       "Third",
		TraceID:   "test-trace",
	})

	// Should have 2 entries for test-trace (second and third)
	logs5 := smallRb.GetLogsByTraceID("test-trace")
	if len(logs5) != 2 {
		t.Errorf("After eviction, expected 2 logs for test-trace, got %d", len(logs5))
	}
}
