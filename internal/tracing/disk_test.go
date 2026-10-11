package tracing

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/elbeanio/the_running_man/internal/parser"
)

// clock is a settable time source, so roll and expiry are tested without
// sleeping through an hour.
type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

type sink struct{ entries []*parser.LogEntry }

func (s *sink) Append(e *parser.LogEntry) { s.entries = append(s.entries, e) }

// run opens span storage on dir as one Running Man run starting at c's time,
// loads what earlier runs left, and returns it.
func run(t *testing.T, dir string, c *clock, maxBytes int64, logs LogSink) *SpanStorage {
	t.Helper()
	d, err := openDisk(dir, c.now(), 24*time.Hour, maxBytes, logs, c.now)
	if err != nil {
		t.Fatalf("openDisk: %v", err)
	}
	s := NewSpanStorage(1000, 24*time.Hour, 1<<30)
	s.now = c.now
	s.AttachDisk(d, c.now())
	s.LoadFromDisk()
	return s
}

func heldIDs(s *SpanStorage) string {
	var out []string
	for _, sp := range allSpans(s) {
		out = append(out, sp.TraceID)
	}
	return strings.Join(out, " ")
}

// The point of the change: a trace from before a restart is still there after
// it, with everything the API serves.
func TestDisk_TracesSurviveARestart(t *testing.T) {
	dir, c := t.TempDir(), &clock{time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)}

	first := run(t, dir, c, 1<<30, nil)
	first.Add(&SpanEntry{TraceID: "before", SpanID: "s1", Name: "chat.turn", Duration: 1500 * time.Millisecond,
		StartTime: c.now(), Attributes: map[string]string{"input.value": "why is it slow?"},
		Events: []SpanEvent{{Name: "exception", Attributes: map[string]string{"exception.message": "boom"}}}})

	c.advance(2 * time.Hour)
	second := run(t, dir, c, 1<<30, nil)
	second.Add(&SpanEntry{TraceID: "after", SpanID: "s2"})

	if got := heldIDs(second); got != "before after" {
		t.Fatalf("spans after restart: %q, want the earlier run's first", got)
	}
	sp := second.Span("before", "s1")
	if sp == nil || sp.Duration != 1500*time.Millisecond || sp.Attributes["input.value"] != "why is it slow?" ||
		len(sp.Events) != 1 || sp.Events[0].Attributes["exception.message"] != "boom" {
		t.Errorf("span did not round-trip: %+v", sp)
	}
	if !sp.receivedAt.Equal(time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("received at %v, want its original arrival: age limits are measured from it", sp.receivedAt)
	}
}

// Age expiry deletes whole segments once their newest span is past the limit,
// and drops those spans from memory too, so the API never serves a trace that
// would be gone after a restart.
func TestDisk_ExpiresSegmentsByAge(t *testing.T) {
	dir, c := t.TempDir(), &clock{time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)}
	s := run(t, dir, c, 1<<30, nil)

	s.Add(&SpanEntry{TraceID: "old"})
	c.advance(90 * time.Minute) // past the hourly roll: a new segment
	s.Add(&SpanEntry{TraceID: "new"})
	if n := len(segments(t, dir)); n != 2 {
		t.Fatalf("%d segments, want 2 after an hourly roll", n)
	}

	c.advance(24 * time.Hour) // "old" is past max_span_age; "new" is not yet
	s.ExpireDisk()

	if n := len(segments(t, dir)); n != 1 {
		t.Errorf("%d segments after expiry, want 1", n)
	}
	if got := heldIDs(s); got != "new" {
		t.Errorf("memory holds %q after its segment expired, want only \"new\"", got)
	}
}

// Past max_span_bytes on disk, the oldest segments go first. The segment being
// written is never deleted, or the newest spans would vanish on arrival.
func TestDisk_ExpiresOldestSegmentsBySize(t *testing.T) {
	dir, c := t.TempDir(), &clock{time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)}
	s := run(t, dir, c, 5_000, nil)

	for _, id := range []string{"a", "b", "c"} {
		s.Add(bigSpan(id, 2_000))
		c.advance(61 * time.Minute) // each in its own segment
	}
	s.Add(bigSpan("d", 10)) // rolls, which runs expiry

	if got := heldIDs(s); got != "b c d" {
		t.Errorf("after size expiry memory holds %q, want \"b c d\"", got)
	}
	if n := len(segments(t, dir)); n != 3 {
		t.Errorf("%d segments, want 3", n)
	}
}

// A segment that alone exceeds max_span_bytes is the one being written when a
// big span arrives. Deleting it would lose that span the moment it was saved.
func TestDisk_NeverDeletesTheSegmentBeingWritten(t *testing.T) {
	dir, c := t.TempDir(), &clock{time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)}
	s := run(t, dir, c, 1_000, nil)

	s.Add(bigSpan("a", 5_000))
	c.advance(61 * time.Minute)
	s.Add(bigSpan("b", 5_000)) // rolls; "a"'s segment goes, "b"'s must not

	if got := heldIDs(s); got != "b" {
		t.Errorf("memory holds %q, want \"b\"", got)
	}
	if n := len(segments(t, dir)); n != 1 {
		t.Errorf("%d segments, want the one being written", n)
	}
}

// A crash mid-write leaves half a line; a later format leaves lines this build
// cannot read. Neither may stop startup or vanish silently: they are skipped
// and counted.
func TestDisk_SkipsAndCountsUnreadableLines(t *testing.T) {
	dir, c := t.TempDir(), &clock{time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)}
	first := run(t, dir, c, 1<<30, nil)
	first.Add(&SpanEntry{TraceID: "kept"})

	seg := filepath.Join(dir, segments(t, dir)[0])
	f, err := os.OpenFile(seg, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"received_at":"2026-10-11T09:00:01Z","span":{"trace_id":"tru`)
	f.Close()
	os.WriteFile(filepath.Join(dir, "20261011T080000.000000000Z.jsonl"),
		[]byte(`{"format":99}`+"\n"+`{"anything":1}`+"\n"), 0o600)

	c.advance(time.Minute)
	second := run(t, dir, c, 1<<30, nil)
	if got := heldIDs(second); got != "kept" {
		t.Errorf("loaded %q, want only the readable span", got)
	}
	if st := second.Stats(); st.SkippedLines != 2 {
		t.Errorf("skipped %d lines, want 2: the torn line and the unknown format's span line", st.SkippedLines)
	}
}

// Restarts are reported only where they fall among the traces still held, so
// the TUI can draw a line at each.
func TestDisk_ReportsRestarts(t *testing.T) {
	dir, c := t.TempDir(), &clock{time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)}
	s := run(t, dir, c, 1<<30, nil)
	if r := s.Restarts(); len(r) != 0 {
		t.Errorf("a first run reports restarts %v", r)
	}
	s.Add(&SpanEntry{TraceID: "one"})

	c.advance(time.Hour)
	second := c.now()
	run(t, dir, c, 1<<30, nil) // receives nothing, but is still a restart
	c.advance(time.Hour)
	third := c.now()
	s = run(t, dir, c, 1<<30, nil)

	r := s.Restarts()
	if len(r) != 2 || !r[0].Equal(third) || !r[1].Equal(second) {
		t.Errorf("restarts = %v, want [%v %v], newest first", r, third, second)
	}
	if !s.FromEarlierRun("one") || s.FromEarlierRun("missing") {
		t.Error("FromEarlierRun should be true only for a trace held from an earlier run")
	}
}

// A failing disk must not stop capture: spans stay in memory, and the
// developer is told once, not once per span.
func TestDisk_WriteFailureIsReportedOnceAndCaptureContinues(t *testing.T) {
	dir, c := t.TempDir(), &clock{time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)}
	logs := &sink{}
	s := run(t, dir, c, 1<<30, logs)

	os.RemoveAll(dir)
	os.WriteFile(dir, nil, 0o600) // a file where the directory was: every write fails
	for _, id := range []string{"a", "b", "c"} {
		s.Add(&SpanEntry{TraceID: id})
		c.advance(61 * time.Minute)
	}

	if got := heldIDs(s); got != "a b c" {
		t.Errorf("memory holds %q; capture must continue when the disk fails", got)
	}
	if len(logs.entries) != 1 || logs.entries[0].Source != "running-man" || logs.entries[0].Level != parser.LevelError {
		t.Errorf("reported %d entries, want one error in the running-man tab: %+v", len(logs.entries), logs.entries)
	}
}

// Closing a finished segment can fail, and then its last spans may not have
// reached the disk. That was ignored; it is reported now.
func TestDisk_FailedCloseOfAFinishedSegmentIsReported(t *testing.T) {
	dir, c := t.TempDir(), &clock{time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)}
	logs := &sink{}
	s := run(t, dir, c, 1<<30, logs)
	s.Add(&SpanEntry{TraceID: "a"})

	s.disk.cur.Close() // so the roll's own Close fails
	c.advance(61 * time.Minute)
	s.Add(&SpanEntry{TraceID: "b"})

	if len(logs.entries) != 1 || !strings.Contains(logs.entries[0].Message, "closing") {
		t.Fatalf("want one report of the failed close, got %+v", logs.entries)
	}
	if n := len(segments(t, dir)); n != 2 {
		t.Errorf("%d segments; a failed close must not stop the next segment", n)
	}
	if got := heldIDs(s); got != "a b" {
		t.Errorf("memory holds %q", got)
	}
}

// Spans that arrive while earlier runs' spans are loading are newer than all
// of them: the loaded spans go before, or age eviction, which stops at the
// first span that is not old, would stop early.
func TestDisk_LoadedSpansGoBeforeLiveOnes(t *testing.T) {
	dir, c := t.TempDir(), &clock{time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)}
	run(t, dir, c, 1<<30, nil).Add(&SpanEntry{TraceID: "earlier"})

	c.advance(time.Hour)
	d, err := openDisk(dir, c.now(), 24*time.Hour, 1<<30, nil, c.now)
	if err != nil {
		t.Fatal(err)
	}
	s := NewSpanStorage(1000, 24*time.Hour, 1<<30)
	s.now = c.now
	s.AttachDisk(d, c.now())
	if !s.Stats().Loading {
		t.Error("Stats().Loading is false before the load has run")
	}
	s.Add(&SpanEntry{TraceID: "live"})
	s.LoadFromDisk()

	if got := heldIDs(s); got != "earlier live" {
		t.Errorf("order %q, want loaded spans before live ones", got)
	}
	if s.Stats().Loading {
		t.Error("Stats().Loading still true after the load")
	}
}

func segments(t *testing.T, dir string) []string {
	t.Helper()
	names, err := listSegments(dir)
	if err != nil {
		t.Fatal(err)
	}
	return names
}
