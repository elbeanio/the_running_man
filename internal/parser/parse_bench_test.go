package parser

import (
	"testing"
	"time"
)

// Every line a process or container writes goes through MultiParser, so this
// is the hottest path in the program. Plain info lines are the common case and
// the most expensive: they fall through every level pattern.
var benchLines = []string{
	"GET /api/users 200 12ms",
	"Compiled successfully in 1432ms",
	"INFO server listening on :8080",
	"WARN slow query took 1.2s",
	"ERROR: connection refused to db:5432",
	`{"level":"info","msg":"request handled","status":200}`,
	"webpack 5.88.2 compiled with 2 warnings",
	"  at Object.<anonymous> (/app/index.js:10:5)",
}

func BenchmarkMultiParser_Parse(b *testing.B) {
	mp := NewMultiParser()
	now := time.Now()
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		mp.ParseLineWithType("bench", "process", benchLines[i%len(benchLines)], now)
	}
}
