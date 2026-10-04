package storage

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/elbeanio/the_running_man/internal/parser"
)

// Age eviction stopped at the first entry that was not old, which assumes
// entries arrive in timestamp order. OTLP logs keep the sender's timestamp, so
// they do not: one record from a client whose clock was an hour fast sat at the
// head and froze retention for everything behind it. Reproduced: retention 50ms,
// 100ms later the buffer still held 1,002 entries where 2 belonged.
func TestRetentionIgnoresReportedTimestamps(t *testing.T) {
	rb := NewRingBuffer(100000, 50*time.Millisecond, 1<<30)

	rb.Append(&parser.LogEntry{Timestamp: time.Now().Add(time.Hour), Source: "browser", Raw: "x"})
	for i := 0; i < 1000; i++ {
		rb.Append(&parser.LogEntry{Timestamp: time.Now(), Source: "backend", Raw: "x"})
	}

	time.Sleep(100 * time.Millisecond)
	rb.Append(&parser.LogEntry{Timestamp: time.Now(), Source: "backend", Raw: "x"})

	if got := rb.Stats().TotalEntries; got > 1 {
		t.Errorf("buffer holds %d entries after retention elapsed; want only the latest", got)
	}
}

// The reverse case: an entry stamped in the past by its sender was received
// just now, and retention is a promise about how long Running Man keeps what it
// received.
func TestRetentionKeepsRecentlyReceivedEntries(t *testing.T) {
	rb := NewRingBuffer(100000, time.Minute, 1<<30)
	rb.Append(&parser.LogEntry{Timestamp: time.Now().Add(-2 * time.Hour), Source: "browser", Raw: "x"})
	rb.Append(&parser.LogEntry{Timestamp: time.Now(), Source: "backend", Raw: "x"})

	if got := rb.Stats().TotalEntries; got != 2 {
		t.Errorf("buffer holds %d entries; an old reported timestamp evicted something received a moment ago", got)
	}
}

// /health listed sources in map order, so the same buffer gave a different
// order on every call -- noise in any diff or comparison of two responses.
func TestRingBuffer_GetSourcesIsSortedByName(t *testing.T) {
	rb := NewRingBuffer(1000, time.Hour, 1<<20)
	for i := 20; i > 0; i-- {
		rb.Append(&parser.LogEntry{Source: fmt.Sprintf("svc-%02d", i), Timestamp: time.Now(), Raw: "x", Message: "x"})
	}

	got := rb.GetSources()
	if !slices.IsSortedFunc(got, func(a, b SourceInfo) int { return strings.Compare(a.Name, b.Name) }) {
		names := make([]string, len(got))
		for i, s := range got {
			names[i] = s.Name
		}
		t.Errorf("sources not sorted by name: %v", names)
	}
}
