package docker

import (
	"bytes"
	"encoding/binary"
	"strings"
	"sync"
	"testing"
	"time"
)

// frame builds one frame of Docker's multiplexed log format: a stream byte
// (1 stdout, 2 stderr), three zero bytes, a big-endian payload length, then the
// payload. Built by hand because the moby/api stdcopy package exports only the
// decoder -- which also keeps the format spelled out where the test can see it.
func frame(stream byte, payload string) []byte {
	header := make([]byte, 8)
	header[0] = stream
	binary.BigEndian.PutUint32(header[4:], uint32(len(payload)))
	return append(header, payload...)
}

const (
	streamStdout = 1
	streamStderr = 2
)

type captured struct {
	line     string
	at       time.Time
	isStderr bool
}

type recorder struct {
	mu    sync.Mutex
	lines []captured
}

func (r *recorder) handle(_ string, line string, at time.Time, isStderr bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, captured{line: line, at: at, isStderr: isStderr})
}

func newTestStreamer(r *recorder) *ContainerStreamer {
	return NewContainerStreamer(nil, "c0ffee", "svc", r.handle, 0)
}

// A container with tty: true has no multiplex headers: its log stream is the
// raw output. Parsed as multiplexed, the first 8 bytes of real output were read
// as a header -- bytes 4-7 of "hello world" decode to a frame size near 1.8 GB,
// which was allocated, and the read then blocked forever waiting for it.
// Reproduced against a real container: zero lines in six seconds.
func TestStreamer_TTYStreamIsRaw(t *testing.T) {
	r := &recorder{}
	raw := "2026-10-04T12:00:01.000000001Z hello world\n" +
		"2026-10-04T12:00:02.000000002Z second line\n"

	newTestStreamer(r).consume(strings.NewReader(raw), true)

	if len(r.lines) != 2 {
		t.Fatalf("got %d lines, want 2: %+v", len(r.lines), r.lines)
	}
	if r.lines[0].line != "hello world" || r.lines[1].line != "second line" {
		t.Errorf("lines = %q, %q", r.lines[0].line, r.lines[1].line)
	}
}

// Multiplexed streams carry stdout and stderr as separate frames.
func TestStreamer_MultiplexedSeparatesStreams(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(frame(streamStdout, "2026-10-04T12:00:01Z to stdout\n"))
	buf.Write(frame(streamStderr, "2026-10-04T12:00:02Z to stderr\n"))

	r := &recorder{}
	newTestStreamer(r).consume(&buf, false)

	if len(r.lines) != 2 {
		t.Fatalf("got %d lines, want 2: %+v", len(r.lines), r.lines)
	}
	byText := map[string]bool{}
	for _, c := range r.lines {
		byText[c.line] = c.isStderr
	}
	if byText["to stdout"] != false || byText["to stderr"] != true {
		t.Errorf("streams not separated: %+v", r.lines)
	}
}

// Replayed history was stamped with time.Now(), so lines written before Running
// Man existed were reported as having just happened -- /errors?since=10m
// returned days-old errors from a long-running container. Docker's own
// timestamp is used instead, and stripped from the line.
func TestStreamer_UsesDockersTimestamp(t *testing.T) {
	r := &recorder{}
	newTestStreamer(r).consume(strings.NewReader("2020-01-02T03:04:05.123456789Z an old line\n"), true)

	if len(r.lines) != 1 {
		t.Fatalf("got %d lines", len(r.lines))
	}
	want := time.Date(2020, 1, 2, 3, 4, 5, 123456789, time.UTC)
	if !r.lines[0].at.Equal(want) {
		t.Errorf("timestamp = %v, want %v", r.lines[0].at, want)
	}
	if r.lines[0].line != "an old line" {
		t.Errorf("line = %q, want the prefix stripped", r.lines[0].line)
	}
}

// A line Docker did not timestamp, or a prefix that does not parse, is kept
// whole and stamped on arrival rather than mangled.
func TestStreamer_ToleratesMissingTimestamp(t *testing.T) {
	r := &recorder{}
	newTestStreamer(r).consume(strings.NewReader("no timestamp here\n"), true)

	if len(r.lines) != 1 || r.lines[0].line != "no timestamp here" {
		t.Fatalf("got %+v", r.lines)
	}
	if time.Since(r.lines[0].at) > time.Minute {
		t.Errorf("fallback timestamp %v is not arrival time", r.lines[0].at)
	}
}

// A line can be split across multiplex frames; it must be reassembled, not
// emitted as two half-lines.
func TestStreamer_ReassemblesLinesAcrossFrames(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(frame(streamStdout, "2026-10-04T12:00:01Z first half "))
	buf.Write(frame(streamStdout, "and second half\n"))

	r := &recorder{}
	newTestStreamer(r).consume(&buf, false)

	if len(r.lines) != 1 || r.lines[0].line != "first half and second half" {
		t.Fatalf("got %+v", r.lines)
	}
}

// Output without a trailing newline is not lost when the stream ends.
func TestStreamer_FlushesTheLastPartialLine(t *testing.T) {
	r := &recorder{}
	newTestStreamer(r).consume(strings.NewReader("2026-10-04T12:00:01Z no newline at the end"), true)

	if len(r.lines) != 1 || r.lines[0].line != "no newline at the end" {
		t.Fatalf("got %+v", r.lines)
	}
}
