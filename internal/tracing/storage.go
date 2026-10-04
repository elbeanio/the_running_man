package tracing

import (
	"strings"
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
	span.receivedAt = time.Now()
	s.spans = append(s.spans, span)

	// Trim if over size limit
	if over := len(s.spans) - s.maxSize; over > 0 {
		s.dropOldest(over)
	}
}

// evictOldSpans removes spans older than maxAge
// evictOldSpans drops spans held for longer than maxAge.
//
// Measured from arrival, which is monotonic in this slice, so stopping at the
// first span that is not old is correct. It used to compare StartTime, which is
// not: one span from a client with a fast clock stopped all span eviction, and
// a span that started before the cutoff was discarded on arrival.
func (s *SpanStorage) evictOldSpans() {
	cutoff := time.Now().Add(-s.maxAge)
	i := 0
	for i < len(s.spans) && s.spans[i].receivedAt.Before(cutoff) {
		i++
	}
	s.dropOldest(i)
}

// dropOldest removes the n oldest spans, clearing their slots so the backing
// array does not keep them reachable after they are gone.
func (s *SpanStorage) dropOldest(n int) {
	if n <= 0 {
		return
	}
	clear(s.spans[:n])
	s.spans = s.spans[n:]
}

// Query retrieves spans matching the given filters
func (s *SpanStorage) Query(filters SpanQueryFilters) []*SpanEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Non-nil, so an empty result serialises as [] rather than null.
	result := []*SpanEntry{}
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
		if filters.SpanName != "" && !strings.Contains(span.Name, filters.SpanName) {
			continue
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

// SpanQueryFilters defines filters for querying spans
type SpanQueryFilters struct {
	Since       time.Duration
	ServiceName string
	TraceID     string
	SpanName    string
	Status      string
	Limit       int
}
