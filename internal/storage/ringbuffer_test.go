package storage

import (
	"testing"
	"time"

	"github.com/iangeorge/the_running_man/internal/parser"
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
