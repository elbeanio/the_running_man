package tracing

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/elbeanio/the_running_man/internal/parser"
)

// Disk keeps spans in segment files, so traces outlive a restart and can be
// compared with one from hours later to confirm a fix. Logs are not kept this
// way: they serve the app that is running now, where a trace is the record of
// one request and is useful after the fact.
//
// A segment is a JSON-lines file: a header naming the format and the start of
// the Running Man run that wrote it, then one line per span. Segments are
// expired whole -- deleting lines from a file means rewriting it -- so a span can
// outlive max_span_age by up to one segment's lifetime, an hour.
//
// Disk has no lock of its own. Every call comes from SpanStorage with its lock
// held, which keeps spans on disk in arrival order and leaves no second lock to
// take in the wrong order.
type Disk struct {
	dir      string
	run      time.Time
	maxAge   time.Duration
	maxBytes int64
	logs     LogSink
	now      func() time.Time

	cur       *os.File
	curName   string
	curOpened time.Time
	curSize   int64
	failing   bool

	// earlier lists the segments that existed when the disk was opened: what
	// earlier runs left, for LoadFromDisk to read back.
	earlier []string

	// newest is each segment's latest span arrival, which is what expiry
	// judges a segment by and what memory drops through. Recorded as spans are
	// written, and for earlier runs' segments as they are loaded; a segment not
	// in it falls back to its modification time.
	newest map[string]time.Time
}

const (
	diskFormat = 1
	// segmentLife and segmentMaxBytes bound how much expiry can overshoot:
	// a segment is deleted only when all of it can go.
	segmentLife     = time.Hour
	segmentMaxBytes = 16 << 20
	segmentExt      = ".jsonl"
	// segmentTime names segments so that a plain sort is arrival order.
	segmentTime = "20060102T150405.000000000Z"
)

type segmentHeader struct {
	Format            int       `json:"format"`
	RunningManStarted time.Time `json:"running_man_started"`
}

type diskLine struct {
	ReceivedAt time.Time  `json:"received_at"`
	Span       *SpanEntry `json:"span"`
}

// OpenDisk prepares dir for the run that started at run, and writes that run's
// first segment straight away: a run that receives no spans still has to be on
// disk, or its restart could never be shown.
func OpenDisk(dir string, run time.Time, maxAge time.Duration, maxBytes int64, logs LogSink) (*Disk, error) {
	return openDisk(dir, run, maxAge, maxBytes, logs, time.Now)
}

// openDisk is OpenDisk with the clock given, since it is read before OpenDisk
// returns: the first segment is named and dated by it.
func openDisk(dir string, run time.Time, maxAge time.Duration, maxBytes int64, logs LogSink, now func() time.Time) (*Disk, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("creating %s: %w", dir, err)
	}
	earlier, err := listSegments(dir)
	if err != nil {
		return nil, err
	}
	d := &Disk{dir: dir, run: run, maxAge: maxAge, maxBytes: maxBytes, logs: logs, now: now,
		earlier: earlier, newest: map[string]time.Time{}}
	if err := d.roll(); err != nil {
		return nil, err
	}
	return d, nil
}

func listSegments(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", dir, err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), segmentExt) {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	return names, nil
}

// roll closes the current segment and starts the next, header first.
//
// A failed close of the finished segment is reported but does not stop the
// next one: its last spans may not have reached the disk, which the developer
// should know, and the spans to come can still be saved.
func (d *Disk) roll() error {
	if d.cur != nil {
		if err := d.cur.Close(); err != nil {
			d.report(parser.LevelError, fmt.Sprintf(
				"Error closing %s; its last spans may not survive a restart: %v", filepath.Join(d.dir, d.curName), err))
		}
		d.cur = nil
	}
	now := d.now()
	name := now.UTC().Format(segmentTime) + segmentExt
	f, err := os.OpenFile(filepath.Join(d.dir, name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	header, _ := json.Marshal(segmentHeader{Format: diskFormat, RunningManStarted: d.run})
	n, err := f.Write(append(header, '\n'))
	if err != nil {
		return errors.Join(err, f.Close())
	}
	d.cur, d.curName, d.curOpened, d.curSize = f, name, now, int64(n)
	return nil
}

// append writes one span, starting a new segment first when the current one is
// an hour old or full. It reports whether it did, since that is when expiry
// runs. A failure is reported in the running-man tab once, not per span, and
// capture carries on in memory.
func (d *Disk) append(sp *SpanEntry) (rolled bool) {
	line, err := json.Marshal(diskLine{ReceivedAt: sp.receivedAt, Span: sp})
	if err == nil && (d.cur == nil || d.now().Sub(d.curOpened) >= segmentLife || d.curSize >= segmentMaxBytes) {
		err = d.roll()
		rolled = err == nil
	}
	if err == nil {
		var n int
		n, err = d.cur.Write(append(line, '\n'))
		d.curSize += int64(n)
		if err == nil {
			d.newest[d.curName] = sp.receivedAt
		}
	}
	if err != nil {
		if d.cur != nil {
			// Joined, so the one report carries both failures.
			err = errors.Join(err, d.cur.Close())
			d.cur = nil // retried by a roll on the next span
		}
		if !d.failing {
			d.failing = true
			d.report(parser.LevelError, fmt.Sprintf(
				"Could not save spans to %s, so traces from now on will not survive a restart: %v", d.dir, err))
		}
		return rolled
	}
	if d.failing {
		d.failing = false
		d.report(parser.LevelInfo, "Saving spans to "+d.dir+" again")
	}
	return rolled
}

func (d *Disk) report(level parser.LogLevel, msg string) {
	if d.logs == nil {
		return
	}
	d.logs.Append(&parser.LogEntry{Timestamp: d.now(), Level: level, Source: "running-man",
		SourceType: "system", Message: msg, Raw: msg})
}

// expire deletes segments past max_span_age, then the oldest while the total
// exceeds max_span_bytes, never the one being written. It returns the newest
// arrival time among what it deleted, so memory can drop the same spans.
func (d *Disk) expire() (through time.Time, deleted bool) {
	names, err := listSegments(d.dir)
	if err != nil {
		return through, false
	}
	type seg struct {
		name string
		mod  time.Time
		size int64
	}
	var segs []seg
	var total int64
	for _, name := range names {
		info, err := os.Stat(filepath.Join(d.dir, name))
		if err != nil {
			continue
		}
		mod, ok := d.newest[name]
		if !ok {
			mod = info.ModTime()
		}
		segs = append(segs, seg{name, mod, info.Size()})
		total += info.Size()
	}
	cutoff := d.now().Add(-d.maxAge)
	for _, s := range segs {
		if s.name == d.curName {
			break // the newest; everything after it is too
		}
		if !s.mod.Before(cutoff) && total <= d.maxBytes {
			break
		}
		if err := os.Remove(filepath.Join(d.dir, s.name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			break
		}
		total -= s.size
		d.earlier = slices.DeleteFunc(d.earlier, func(n string) bool { return n == s.name })
		delete(d.newest, s.name)
		if s.mod.After(through) {
			through = s.mod
		}
		deleted = true
	}
	return through, deleted
}

// size is the bytes the segments hold on disk.
func (d *Disk) size() int64 {
	names, _ := listSegments(d.dir)
	var total int64
	for _, name := range names {
		if info, err := os.Stat(filepath.Join(d.dir, name)); err == nil {
			total += info.Size()
		}
	}
	return total
}

// loaded is what load read back from earlier runs' segments.
type loaded struct {
	spans   []*SpanEntry         // in arrival order
	runs    []time.Time          // the start of each run that wrote a segment
	newest  map[string]time.Time // each segment's latest arrival
	skipped int                  // lines that could not be read
}

// load reads the named segments back. It touches only files, not the Disk's
// state, so it runs without the storage lock. A segment deleted since it was
// listed is skipped quietly: it expired, it is not broken.
func (d *Disk) load(names []string) loaded {
	l := loaded{newest: map[string]time.Time{}}
	for _, name := range names {
		f, err := os.Open(filepath.Join(d.dir, name))
		if err != nil {
			continue
		}
		spans, run, bad := readSegment(f)
		f.Close()
		l.spans = append(l.spans, spans...)
		if n := len(spans); n > 0 {
			l.newest[name] = spans[n-1].receivedAt
		}
		if !run.IsZero() {
			l.runs = append(l.runs, run)
		}
		l.skipped += bad
	}
	return l
}

// readSegment reads one segment. Lines are read whole however long they are:
// a span carrying a conversation and its images runs to megabytes.
func readSegment(r io.Reader) (spans []*SpanEntry, run time.Time, skipped int) {
	br := bufio.NewReader(r)
	first := true
	known := false
	for {
		line, err := br.ReadBytes('\n')
		if len(strings.TrimSpace(string(line))) > 0 {
			switch {
			case first:
				var h segmentHeader
				if json.Unmarshal(line, &h) == nil && h.Format == diskFormat {
					known, run = true, h.RunningManStarted
				}
			case !known:
				skipped++
			default:
				var l diskLine
				if json.Unmarshal(line, &l) != nil || l.Span == nil {
					skipped++
				} else {
					l.Span.receivedAt = l.ReceivedAt
					spans = append(spans, l.Span)
				}
			}
			first = false
		}
		if err != nil {
			return spans, run, skipped
		}
	}
}
