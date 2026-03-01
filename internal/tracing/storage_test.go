package tracing

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSpanStorage_AddAndQuery(t *testing.T) {
	storage := NewSpanStorage(100, time.Hour)

	// Create test spans
	span1 := &SpanEntry{
		TraceID:     "trace1",
		SpanID:      "span1",
		Name:        "operation1",
		ServiceName: "service1",
		StartTime:   time.Now().Add(-30 * time.Minute),
		Status:      "ok",
	}

	span2 := &SpanEntry{
		TraceID:     "trace2",
		SpanID:      "span2",
		Name:        "operation2",
		ServiceName: "service2",
		StartTime:   time.Now().Add(-15 * time.Minute),
		Status:      "error",
	}

	// Add spans
	storage.Add(span1)
	storage.Add(span2)

	// Query all spans
	allSpans := storage.Query(SpanQueryFilters{})
	assert.Len(t, allSpans, 2)

	// Query by service name
	service1Spans := storage.Query(SpanQueryFilters{ServiceName: "service1"})
	assert.Len(t, service1Spans, 1)
	assert.Equal(t, "trace1", service1Spans[0].TraceID)

	// Query by trace ID
	trace2Spans := storage.Query(SpanQueryFilters{TraceID: "trace2"})
	assert.Len(t, trace2Spans, 1)
	assert.Equal(t, "span2", trace2Spans[0].SpanID)

	// Query by status
	errorSpans := storage.Query(SpanQueryFilters{Status: "error"})
	assert.Len(t, errorSpans, 1)
	assert.Equal(t, "trace2", errorSpans[0].TraceID)

	// Query by time (should get both spans)
	recentSpans := storage.Query(SpanQueryFilters{Since: time.Hour})
	assert.Len(t, recentSpans, 2)

	// Query by older time (should get none)
	oldSpans := storage.Query(SpanQueryFilters{Since: 5 * time.Minute})
	assert.Len(t, oldSpans, 0)
}

func TestSpanStorage_GetTrace(t *testing.T) {
	storage := NewSpanStorage(100, time.Hour)

	// Create spans for the same trace
	span1 := &SpanEntry{
		TraceID:     "trace1",
		SpanID:      "span1",
		Name:        "operation1",
		ServiceName: "service1",
		StartTime:   time.Now(),
	}

	span2 := &SpanEntry{
		TraceID:     "trace1",
		SpanID:      "span2",
		Name:        "operation2",
		ServiceName: "service1",
		StartTime:   time.Now(),
	}

	span3 := &SpanEntry{
		TraceID:     "trace2",
		SpanID:      "span3",
		Name:        "operation3",
		ServiceName: "service2",
		StartTime:   time.Now(),
	}

	storage.Add(span1)
	storage.Add(span2)
	storage.Add(span3)

	// Get trace1 spans
	trace1Spans := storage.GetTrace("trace1")
	assert.Len(t, trace1Spans, 2)

	// Get trace2 spans
	trace2Spans := storage.GetTrace("trace2")
	assert.Len(t, trace2Spans, 1)

	// Get non-existent trace
	trace3Spans := storage.GetTrace("trace3")
	assert.Len(t, trace3Spans, 0)
}

func TestSpanStorage_EvictionByAge(t *testing.T) {
	storage := NewSpanStorage(100, 30*time.Minute)

	// Create old span
	oldSpan := &SpanEntry{
		TraceID:     "old",
		SpanID:      "span1",
		Name:        "old-operation",
		ServiceName: "service1",
		StartTime:   time.Now().Add(-1 * time.Hour), // 1 hour old
	}

	// Create new span
	newSpan := &SpanEntry{
		TraceID:     "new",
		SpanID:      "span2",
		Name:        "new-operation",
		ServiceName: "service1",
		StartTime:   time.Now(), // current time
	}

	storage.Add(oldSpan)
	storage.Add(newSpan)

	// Should only have the new span (old one evicted)
	spans := storage.Query(SpanQueryFilters{})
	assert.Len(t, spans, 1)
	assert.Equal(t, "new", spans[0].TraceID)
}

func TestSpanStorage_EvictionBySize(t *testing.T) {
	storage := NewSpanStorage(2, time.Hour) // Max 2 spans

	// Add 3 spans
	for i := 0; i < 3; i++ {
		span := &SpanEntry{
			TraceID:     string(rune('a' + i)),
			SpanID:      string(rune('1' + i)),
			Name:        "operation",
			ServiceName: "service1",
			StartTime:   time.Now(),
		}
		storage.Add(span)
	}

	// Should only have the last 2 spans
	spans := storage.Query(SpanQueryFilters{})
	assert.Len(t, spans, 2)
	assert.Equal(t, "b", spans[0].TraceID) // Second span
	assert.Equal(t, "c", spans[1].TraceID) // Third span (first was evicted)
}

func TestSpanStorage_Stats(t *testing.T) {
	storage := NewSpanStorage(100, time.Hour)

	// Add spans at different times
	now := time.Now()
	span1 := &SpanEntry{
		TraceID:     "trace1",
		SpanID:      "span1",
		Name:        "operation1",
		ServiceName: "service1",
		StartTime:   now.Add(-30 * time.Minute),
	}

	span2 := &SpanEntry{
		TraceID:     "trace2",
		SpanID:      "span2",
		Name:        "operation2",
		ServiceName: "service2",
		StartTime:   now.Add(-15 * time.Minute),
	}

	storage.Add(span1)
	storage.Add(span2)

	stats := storage.Stats()
	assert.Equal(t, 2, stats.TotalSpans)
	assert.True(t, stats.OldestSpan.Before(stats.NewestSpan))
}

func TestSpanStorage_QueryWithSpanName(t *testing.T) {
	storage := NewSpanStorage(100, time.Hour)

	span1 := &SpanEntry{
		TraceID:     "trace1",
		SpanID:      "span1",
		Name:        "GET /api/users",
		ServiceName: "service1",
		StartTime:   time.Now(),
	}

	span2 := &SpanEntry{
		TraceID:     "trace2",
		SpanID:      "span2",
		Name:        "POST /api/users",
		ServiceName: "service1",
		StartTime:   time.Now(),
	}

	span3 := &SpanEntry{
		TraceID:     "trace3",
		SpanID:      "span3",
		Name:        "GET /api/products",
		ServiceName: "service2",
		StartTime:   time.Now(),
	}

	storage.Add(span1)
	storage.Add(span2)
	storage.Add(span3)

	// Query by partial span name
	apiSpans := storage.Query(SpanQueryFilters{SpanName: "/api/"})
	assert.Len(t, apiSpans, 3)

	usersSpans := storage.Query(SpanQueryFilters{SpanName: "users"})
	assert.Len(t, usersSpans, 2)

	postSpans := storage.Query(SpanQueryFilters{SpanName: "POST"})
	assert.Len(t, postSpans, 1)
	assert.Equal(t, "trace2", postSpans[0].TraceID)

	productsSpans := storage.Query(SpanQueryFilters{SpanName: "products"})
	assert.Len(t, productsSpans, 1)
	assert.Equal(t, "trace3", productsSpans[0].TraceID)
}
