package tracing

import (
	"sync"
	"time"
)

// SpanStorage provides in-memory storage for trace spans
type SpanStorage struct {
	mu      sync.RWMutex
	spans   []*SpanEntry
	maxSize int
	maxAge  time.Duration
}

// NewSpanStorage creates a new span storage
func NewSpanStorage(maxSize int, maxAge time.Duration) *SpanStorage {
	return &SpanStorage{
		spans:   make([]*SpanEntry, 0, maxSize),
		maxSize: maxSize,
		maxAge:  maxAge,
	}
}

// Add adds a span to storage
func (s *SpanStorage) Add(span *SpanEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove old spans
	s.evictOldSpans()

	// Add new span
	s.spans = append(s.spans, span)

	// Trim if over size limit
	if len(s.spans) > s.maxSize {
		s.spans = s.spans[len(s.spans)-s.maxSize:]
	}
}

// evictOldSpans removes spans older than maxAge
func (s *SpanStorage) evictOldSpans() {
	cutoff := time.Now().Add(-s.maxAge)
	i := 0
	for i < len(s.spans) && s.spans[i].StartTime.Before(cutoff) {
		i++
	}
	if i > 0 {
		s.spans = s.spans[i:]
	}
}

// Query retrieves spans matching the given filters
func (s *SpanStorage) Query(filters SpanQueryFilters) []*SpanEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*SpanEntry
	cutoff := time.Now().Add(-filters.Since)

	for _, span := range s.spans {
		// Filter by time
		if filters.Since > 0 && span.StartTime.Before(cutoff) {
			continue
		}

		// Filter by service name
		if filters.ServiceName != "" && span.ServiceName != filters.ServiceName {
			continue
		}

		// Filter by trace ID
		if filters.TraceID != "" && span.TraceID != filters.TraceID {
			continue
		}

		// Filter by span name (supports partial match)
		if filters.SpanName != "" {
			matched := false
			// Simple contains check for now
			if contains(span.Name, filters.SpanName) {
				matched = true
			}
			if !matched {
				continue
			}
		}

		// Filter by status
		if filters.Status != "" && span.Status != filters.Status {
			continue
		}

		result = append(result, span)
	}

	return result
}

// GetTrace retrieves all spans for a specific trace ID
func (s *SpanStorage) GetTrace(traceID string) []*SpanEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*SpanEntry
	for _, span := range s.spans {
		if span.TraceID == traceID {
			result = append(result, span)
		}
	}
	return result
}

// Stats returns storage statistics
func (s *SpanStorage) Stats() SpanStorageStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	oldest := time.Now()
	newest := time.Time{}
	for _, span := range s.spans {
		if span.StartTime.Before(oldest) {
			oldest = span.StartTime
		}
		if span.StartTime.After(newest) {
			newest = span.StartTime
		}
	}

	return SpanStorageStats{
		TotalSpans: len(s.spans),
		OldestSpan: oldest,
		NewestSpan: newest,
	}
}

// SpanQueryFilters defines filters for querying spans
type SpanQueryFilters struct {
	Since       time.Duration
	ServiceName string
	TraceID     string
	SpanName    string
	Status      string
	Limit       int
}

// SpanStorageStats provides statistics about span storage
type SpanStorageStats struct {
	TotalSpans int
	OldestSpan time.Time
	NewestSpan time.Time
}

// contains is a helper function for string matching
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

// containsHelper performs case-sensitive substring search
func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
