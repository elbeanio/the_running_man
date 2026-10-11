package tracing

import (
	"slices"
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

	// now is the clock, settable so tests need not sleep through retention.
	now func() time.Time

	// disk, when set, keeps spans across restarts; see Disk. The rest are
	// meaningful only with it.
	disk *Disk
	// run is when this Running Man run started; runs adds the starts of the
	// earlier runs whose segments were loaded.
	run  time.Time
	runs []time.Time
	// loading is true until LoadFromDisk has finished, so a caller can tell
	// "not loaded yet" from "not there".
	loading bool
	skipped int
	// droppedThrough is the newest arrival among spans the disk has deleted.
	// A load still in flight must not bring them back.
	droppedThrough time.Time
}

// NewSpanStorage creates a new span storage
func NewSpanStorage(maxSize int, maxAge time.Duration, maxBytes int64) *SpanStorage {
	return &SpanStorage{
		spans:    make([]*SpanEntry, 0, maxSize),
		maxSize:  maxSize,
		maxAge:   maxAge,
		maxBytes: maxBytes,
		now:      time.Now,
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
	span.receivedAt = s.now()
	span.storedSize = size
	s.spans = append(s.spans, span)
	s.bytes += size

	// Trim if over size limit
	if over := len(s.spans) - s.maxSize; over > 0 {
		s.dropOldest(over)
	}

	if s.disk != nil && s.disk.append(span) {
		s.expireDisk()
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
	cutoff := s.now().Add(-s.maxAge)
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

// AttachDisk makes d the storage's disk, for the run that started at run.
// Spans the earlier runs left are not read until LoadFromDisk.
func (s *SpanStorage) AttachDisk(d *Disk, run time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.disk, s.run, s.runs = d, run, []time.Time{run}
	s.loading = len(d.earlier) > 0
}

// LoadFromDisk reads back what earlier runs left. It is slow -- the default
// limit is 256 MB of JSON -- so it reads without the lock, while new spans keep
// arriving, and then puts the loaded spans before them: they arrived earlier,
// and age eviction relies on arrival order.
func (s *SpanStorage) LoadFromDisk() {
	s.mu.RLock()
	d := s.disk
	var names []string
	if d != nil {
		names = slices.Clone(d.earlier)
	}
	s.mu.RUnlock()
	if d == nil {
		return
	}

	l := d.load(names)

	s.mu.Lock()
	defer s.mu.Unlock()
	for name, at := range l.newest {
		if _, gone := d.newest[name]; !gone && slices.Contains(d.earlier, name) {
			d.newest[name] = at
		}
	}
	keep := l.spans[:0]
	var size int64
	for _, sp := range l.spans {
		if sp.receivedAt.After(s.droppedThrough) {
			sp.storedSize = sp.size()
			size += sp.storedSize
			keep = append(keep, sp)
		}
	}
	s.spans = append(keep, s.spans...)
	s.bytes += size
	s.evictOldSpans()
	for len(s.spans) > 0 && (s.bytes > s.maxBytes || len(s.spans) > s.maxSize) {
		s.dropOldest(1)
	}
	s.runs = append(s.runs, l.runs...)
	slices.SortFunc(s.runs, func(a, b time.Time) int { return a.Compare(b) })
	s.runs = slices.CompactFunc(s.runs, func(a, b time.Time) bool { return a.Equal(b) })
	s.skipped = l.skipped
	s.loading = false
}

// ExpireDisk applies the retention limits to the segments on disk, and drops
// from memory whatever that deletes. Runs on every segment roll, and should
// also run periodically, so an idle instance still cleans up.
func (s *SpanStorage) ExpireDisk() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireDisk()
}

func (s *SpanStorage) expireDisk() {
	if s.disk == nil {
		return
	}
	through, deleted := s.disk.expire()
	if !deleted {
		return
	}
	if through.After(s.droppedThrough) {
		s.droppedThrough = through
	}
	i := 0
	for i < len(s.spans) && !s.spans[i].receivedAt.After(through) {
		i++
	}
	s.dropOldest(i)
}

// Restarts returns when Running Man restarted, newest first, among the spans
// held: each run start later than the oldest span's arrival.
func (s *SpanStorage) Restarts() []time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []time.Time{}
	if len(s.spans) == 0 {
		return out
	}
	oldest := s.spans[0].receivedAt
	for i := len(s.runs) - 1; i >= 0; i-- {
		if s.runs[i].After(oldest) {
			out = append(out, s.runs[i])
		}
	}
	return out
}

// FromEarlierRun reports whether a held trace arrived entirely before this run
// started -- so its correlated logs, which are not kept across a restart, are
// gone.
func (s *SpanStorage) FromEarlierRun(traceID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.disk == nil {
		return false
	}
	found := false
	for _, sp := range s.spans {
		if sp.TraceID == traceID {
			if !sp.receivedAt.Before(s.run) {
				return false
			}
			found = true
		}
	}
	return found
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

	// Persisted says spans are kept on disk; the rest describe the disk.
	Persisted    bool
	Dir          string
	DiskBytes    int64
	Loading      bool
	SkippedLines int
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
	if s.disk != nil {
		st.Persisted, st.Dir, st.DiskBytes = true, s.disk.dir, s.disk.size()
		st.Loading, st.SkippedLines = s.loading, s.skipped
	}
	return st
}
