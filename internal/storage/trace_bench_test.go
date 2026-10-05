package storage

import (
	"testing"
	"time"

	"github.com/elbeanio/the_running_man/internal/parser"
)

// A full buffer in which every line belongs to one trace: each append evicts
// that trace's oldest entry, which is the case removeFromTraceIndex must make
// cheap. Removing index 0 by shifting the slice made it O(entries) per append.
func BenchmarkRingBuffer_AppendEvictingOneBusyTrace(b *testing.B) {
	rb := NewRingBuffer(10000, time.Hour, 1<<30)
	entry := func() *parser.LogEntry {
		return &parser.LogEntry{Source: "s", TraceID: "busy", Timestamp: time.Now(), Raw: "x", Message: "x"}
	}
	for i := 0; i < 10000; i++ {
		rb.Append(entry())
	}
	b.ReportAllocs()
	for b.Loop() {
		rb.Append(entry())
	}
}
