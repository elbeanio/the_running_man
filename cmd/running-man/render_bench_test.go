package main

import (
	"fmt"
	"testing"
)

// benchLogs is a full buffer: 10,000 entries is the default max_entries.
func benchLogs(n int) []logEntry {
	logs := make([]logEntry, n)
	for i := range logs {
		logs[i] = logEntry{
			Timestamp: "2026-10-04T09:15:01Z", Level: "info", Source: "backend",
			Message: fmt.Sprintf("request %d handled in 12ms path=/api/things/%d status=200", i, i),
		}
	}
	return logs
}

func benchModel(query string) model {
	m := fixtureModel(160, 50)
	m.logs = benchLogs(10000)
	m.searchQuery = query
	m.refreshMatches() // as Update does when logs arrive or the query changes
	return m
}

// A frame at a full buffer. Bubble Tea calls View after every message --
// keypress, tick, fetch -- so this is the cost of holding down an arrow key.
func BenchmarkViewFullBuffer(b *testing.B) {
	m := benchModel("")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

func BenchmarkViewFullBufferSearching(b *testing.B) {
	m := benchModel("status")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}
