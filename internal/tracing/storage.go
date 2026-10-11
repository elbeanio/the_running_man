package tracing

import (
	"sync"
	"time"
)

// SpanStorage provides in-memory storage for trace spans
type SpanStorage struct {
	mu       sync.RWMutex
	spans    []*SpanEntry
	maxSize  int
	maxAge   time.Duration
	maxBytes int64

	// bytes is the total size() of spans, kept in step by Add and dropOldest.
	bytes int64
}

// NewSpanStorage creates a new span storage
func NewSpanStorage(maxSize int, maxAge time.Duration, maxBytes int64) *SpanStorage {
	return &SpanStorage{
		spans:    make([]*SpanEntry, 0, maxSize),
		maxSize:  maxSize,
		maxAge:   maxAge,
		maxBytes: maxBytes,
	}
}

// Add adds a span to storage
func (s *SpanStorage) Add(span *SpanEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove old spans
	s.evictOldSpans()

	// Make room by size. The len > 0 guard keeps a span larger than the whole
	// limit, alone, rather than discarding it on arrival: it is likely the
	// largest LLM call, which is the span most worth keeping.
	size := span.size()
	for len(s.spans) > 0 && s.bytes+size > s.maxBytes {
		s.dropOldest(1)
	}

	// Add new span
	span.receivedAt = time.Now()
	span.storedSize = size
	s.spans = append(s.spans, span)
	s.bytes += size

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
	for _, sp := range s.spans[:n] {
		s.bytes -= sp.storedSize
	}
	clear(s.spans[:n])
	s.spans = s.spans[n:]
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

// SpanStats describes what span storage holds against its retention limits.
type SpanStats struct {
	TotalSpans int
	TotalBytes int64
	MaxSpans   int
	MaxBytes   int64
	MaxAge     time.Duration
	// OldestSpan and NewestSpan are arrival times, which retention is measured
	// against; nil when nothing is stored.
	OldestSpan *time.Time
	NewestSpan *time.Time
}

// Stats reports the storage's size and limits.
func (s *SpanStorage) Stats() SpanStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st := SpanStats{TotalSpans: len(s.spans), TotalBytes: s.bytes,
		MaxSpans: s.maxSize, MaxBytes: s.maxBytes, MaxAge: s.maxAge}
	if n := len(s.spans); n > 0 {
		oldest, newest := s.spans[0].receivedAt, s.spans[n-1].receivedAt
		st.OldestSpan, st.NewestSpan = &oldest, &newest
	}
	return st
}
