package tracing

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSpanStorage_AddAndTraces(t *testing.T) {
	storage := NewSpanStorage(100, time.Hour, 1<<30)

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

	// Every span is stored.
	assert.Len(t, allSpans(storage), 2)

	// The same selections, now made per trace.
	ids := func(f TraceFilters) []string {
		var out []string
		for _, tr := range storage.Traces(f) {
			out = append(out, tr.TraceID)
		}
		return out
	}
	assert.Equal(t, []string{"trace1"}, ids(TraceFilters{ServiceName: "service1"}))
	assert.Equal(t, []string{"trace2"}, ids(TraceFilters{Status: "error"}))
	assert.Equal(t, []string{"trace2", "trace1"}, ids(TraceFilters{Since: time.Hour}))
	assert.Empty(t, ids(TraceFilters{Since: 5 * time.Minute}))
	// span_name is a substring match.
	assert.Equal(t, []string{"trace2", "trace1"}, ids(TraceFilters{SpanName: "operation"}))
	assert.Equal(t, []string{"trace2"}, ids(TraceFilters{SpanName: "ation2"}))
}

// allSpans is every stored span, for tests that count what eviction kept.
func allSpans(s *SpanStorage) []*SpanEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]*SpanEntry(nil), s.spans...)
}

func TestSpanStorage_GetTrace(t *testing.T) {
	storage := NewSpanStorage(100, time.Hour, 1<<30)

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
	// Age is measured from arrival. This used to backdate StartTime and expect
	// the span gone on the next Add -- which is how any span longer than the
	// retention window was discarded the moment it was exported. See
	// retention_test.go.
	storage := NewSpanStorage(100, 50*time.Millisecond, 1<<30)

	storage.Add(&SpanEntry{TraceID: "old", SpanID: "span1", Name: "old-operation",
		ServiceName: "service1", StartTime: time.Now()})

	time.Sleep(80 * time.Millisecond) // past retention
	storage.Add(&SpanEntry{TraceID: "new", SpanID: "span2", Name: "new-operation",
		ServiceName: "service1", StartTime: time.Now()})

	spans := allSpans(storage)
	assert.Len(t, spans, 1)
	assert.Equal(t, "new", spans[0].TraceID)
}

func TestSpanStorage_EvictionBySize(t *testing.T) {
	storage := NewSpanStorage(2, time.Hour, 1<<30) // Max 2 spans

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
	spans := allSpans(storage)
	assert.Len(t, spans, 2)
	assert.Equal(t, "b", spans[0].TraceID) // Second span
	assert.Equal(t, "c", spans[1].TraceID) // Third span (first was evicted)
}
