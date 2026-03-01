package storage

import (
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/iangeorge/the_running_man/internal/parser"
)

// RingBuffer stores log entries with size and time limits
type RingBuffer struct {
	mu          sync.RWMutex
	entries     []*parser.LogEntry
	maxSize     int
	maxAge      time.Duration
	currentSize int64
	maxBytes    int64
	startTime   time.Time
	// Trace index for fast lookup of logs by trace_id
	traceIndex map[string][]*parser.LogEntry
}

// NewRingBuffer creates a new ring buffer
func NewRingBuffer(maxSize int, maxAge time.Duration, maxBytes int64) *RingBuffer {
	return &RingBuffer{
		entries:    make([]*parser.LogEntry, 0, maxSize),
		maxSize:    maxSize,
		maxAge:     maxAge,
		maxBytes:   maxBytes,
		startTime:  time.Now(),
		traceIndex: make(map[string][]*parser.LogEntry),
	}
}

// Append adds a new log entry to the buffer
func (rb *RingBuffer) Append(entry *parser.LogEntry) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	entrySize := int64(len(entry.Raw))

	// Evict old entries if needed
	rb.evictIfNeeded(entrySize)

	// Add the new entry
	rb.entries = append(rb.entries, entry)
	rb.currentSize += entrySize

	// Update trace index if entry has a trace_id
	if entry.TraceID != "" {
		rb.traceIndex[entry.TraceID] = append(rb.traceIndex[entry.TraceID], entry)
	}
}

// evictIfNeeded removes old entries to make room for new ones
func (rb *RingBuffer) evictIfNeeded(newEntrySize int64) {
	now := time.Now()

	// Remove entries that are too old
	cutoffTime := now.Add(-rb.maxAge)
	for len(rb.entries) > 0 && rb.entries[0].Timestamp.Before(cutoffTime) {
		removed := rb.entries[0]
		rb.entries = rb.entries[1:]
		rb.currentSize -= int64(len(removed.Raw))
		// Clean up trace index for removed entry
		if removed.TraceID != "" {
			rb.removeFromTraceIndex(removed.TraceID, removed)
		}
	}

	// Remove oldest entries if we're over size limit
	for len(rb.entries) > 0 && rb.currentSize+newEntrySize > rb.maxBytes {
		removed := rb.entries[0]
		rb.entries = rb.entries[1:]
		rb.currentSize -= int64(len(removed.Raw))
		// Clean up trace index for removed entry
		if removed.TraceID != "" {
			rb.removeFromTraceIndex(removed.TraceID, removed)
		}
	}

	// Remove oldest entries if we're over count limit
	for len(rb.entries) >= rb.maxSize {
		removed := rb.entries[0]
		rb.entries = rb.entries[1:]
		rb.currentSize -= int64(len(removed.Raw))
		// Clean up trace index for removed entry
		if removed.TraceID != "" {
			rb.removeFromTraceIndex(removed.TraceID, removed)
		}
	}
}

// Query retrieves log entries matching the given filters
func (rb *RingBuffer) Query(filters QueryFilters) []*parser.LogEntry {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	var result []*parser.LogEntry
	cutoffTime := time.Now().Add(-filters.Since)

	for _, entry := range rb.entries {
		// Filter by time
		if filters.Since > 0 && entry.Timestamp.Before(cutoffTime) {
			continue
		}

		// Filter by level
		if len(filters.Levels) > 0 {
			found := false
			for _, level := range filters.Levels {
				if entry.Level == level {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		// Filter by source (supports glob patterns)
		if len(filters.Sources) > 0 {
			found := false
			for _, source := range filters.Sources {
				// Try exact match first (faster)
				if entry.Source == source {
					found = true
					break
				}
				// Try glob pattern match
				matched, err := filepath.Match(source, entry.Source)
				if err == nil && matched {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		// Filter by exclude patterns (supports glob)
		if len(filters.Exclude) > 0 {
			excluded := false
			for _, pattern := range filters.Exclude {
				// Try exact match first (faster)
				if entry.Source == pattern {
					excluded = true
					break
				}
				// Try glob pattern match
				matched, err := filepath.Match(pattern, entry.Source)
				if err == nil && matched {
					excluded = true
					break
				}
			}
			if excluded {
				continue
			}
		}

		// Filter by content
		if filters.Contains != "" {
			if !strings.Contains(entry.Message, filters.Contains) &&
				!strings.Contains(entry.Raw, filters.Contains) {
				continue
			}
		}

		// Filter errors only
		if filters.ErrorsOnly && !entry.IsError {
			continue
		}

		result = append(result, entry)
	}

	return result
}

// Stats returns statistics about the buffer
func (rb *RingBuffer) Stats() BufferStats {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	return BufferStats{
		TotalEntries: len(rb.entries),
		TotalBytes:   rb.currentSize,
		OldestEntry:  rb.oldestEntry(),
		NewestEntry:  rb.newestEntry(),
		Uptime:       time.Since(rb.startTime),
		MaxEntries:   rb.maxSize,
		MaxBytes:     rb.maxBytes,
		MaxAge:       rb.maxAge,
	}
}

// SourceInfo contains information about a log source
type SourceInfo struct {
	Name       string    `json:"name"`
	EntryCount int       `json:"entry_count"`
	LastSeen   time.Time `json:"last_seen"`
}

// GetSources returns all unique sources with their statistics
func (rb *RingBuffer) GetSources() []SourceInfo {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	sourceMap := make(map[string]*SourceInfo)

	for _, entry := range rb.entries {
		if info, exists := sourceMap[entry.Source]; exists {
			info.EntryCount++
			if entry.Timestamp.After(info.LastSeen) {
				info.LastSeen = entry.Timestamp
			}
		} else {
			sourceMap[entry.Source] = &SourceInfo{
				Name:       entry.Source,
				EntryCount: 1,
				LastSeen:   entry.Timestamp,
			}
		}
	}

	// Convert map to slice
	sources := make([]SourceInfo, 0, len(sourceMap))
	for _, info := range sourceMap {
		sources = append(sources, *info)
	}

	return sources
}

func (rb *RingBuffer) oldestEntry() time.Time {
	if len(rb.entries) == 0 {
		return time.Time{}
	}
	return rb.entries[0].Timestamp
}

func (rb *RingBuffer) newestEntry() time.Time {
	if len(rb.entries) == 0 {
		return time.Time{}
	}
	return rb.entries[len(rb.entries)-1].Timestamp
}

// removeFromTraceIndex removes a specific entry from the trace index
func (rb *RingBuffer) removeFromTraceIndex(traceID string, entry *parser.LogEntry) {
	if entries, exists := rb.traceIndex[traceID]; exists {
		// Find and remove the entry
		for i, e := range entries {
			if e == entry {
				// Remove the entry from slice
				rb.traceIndex[traceID] = append(entries[:i], entries[i+1:]...)
				// If slice is empty, delete the traceID from map
				if len(rb.traceIndex[traceID]) == 0 {
					delete(rb.traceIndex, traceID)
				}
				break
			}
		}
	}
}

// Clear removes all entries from the buffer
func (rb *RingBuffer) Clear() {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.entries = make([]*parser.LogEntry, 0, rb.maxSize)
	rb.currentSize = 0
	rb.traceIndex = make(map[string][]*parser.LogEntry)
}

// GetLogsByTraceID returns all log entries for a specific trace ID
func (rb *RingBuffer) GetLogsByTraceID(traceID string) []*parser.LogEntry {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if entries, exists := rb.traceIndex[traceID]; exists {
		// Return a copy to avoid concurrent modification issues
		result := make([]*parser.LogEntry, len(entries))
		copy(result, entries)
		return result
	}
	return nil
}

// QueryFilters specifies which logs to retrieve
type QueryFilters struct {
	Since      time.Duration
	Levels     []parser.LogLevel
	Sources    []string // Supports glob patterns (e.g., "python-*")
	Exclude    []string // Exclude patterns (supports glob)
	Contains   string
	ErrorsOnly bool
}

// BufferStats contains statistics about the ring buffer
type BufferStats struct {
	TotalEntries int
	TotalBytes   int64
	OldestEntry  time.Time
	NewestEntry  time.Time
	Uptime       time.Duration
	MaxEntries   int
	MaxBytes     int64
	MaxAge       time.Duration
}
