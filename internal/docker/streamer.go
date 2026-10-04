package docker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"

	"github.com/elbeanio/the_running_man/internal/termout"
)

// LineHandler is called for each line of output from a container
type LineHandler func(source string, line string, timestamp time.Time, isStderr bool)

// StreamEndHandler is called when a container's log stream ends, once per
// output stream, after its last line. It lets the parser release a traceback
// it is still holding: a container gets no exit report, so nothing else would
// ever close it.
type StreamEndHandler func(source string, isStderr bool)

// maxContainerLineBytes caps a single line, matching the cap the process
// wrapper applies. A line longer than this is cut and marked rather than held
// in memory indefinitely.
const maxContainerLineBytes = 1024 * 1024

// ContainerStreamer streams logs from a Docker container
type ContainerStreamer struct {
	client      *Client
	containerID string
	name        string
	handler     LineHandler
	onEnd       StreamEndHandler
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup

	// history is how far back to replay when attaching. Zero replays nothing.
	history time.Duration
}

// NewContainerStreamer creates a new streamer for the given container.
//
// history is how much of the container's existing log to replay on attach.
// Replaying is useful -- what happened just before attaching is often the
// point -- but it is bounded to the retention window, because replayed lines
// arrive now and would otherwise be held for a full retention period however
// old they were.
func NewContainerStreamer(client *Client, containerID, name string, handler LineHandler, history time.Duration) *ContainerStreamer {
	ctx, cancel := context.WithCancel(context.Background())

	return &ContainerStreamer{
		client:      client,
		containerID: containerID,
		name:        name,
		handler:     handler,
		ctx:         ctx,
		cancel:      cancel,
		history:     history,
	}
}

// OnStreamEnd registers fn to be called as the log stream ends. It must be
// called before Start.
func (s *ContainerStreamer) OnStreamEnd(fn StreamEndHandler) {
	s.onEnd = fn
}

// Start begins streaming logs from the container
func (s *ContainerStreamer) Start() error {
	// A TTY container's log stream is the raw output, with none of the
	// multiplex headers. Treating it as multiplexed read the first 8 bytes of
	// real output as a header: "hello world" decodes to a frame of about
	// 1.8 GB, which was allocated, and the read then waited forever for it. So
	// the container is inspected first. If inspection fails it is assumed not to
	// be a TTY, which is Compose's default.
	tty := false
	if info, err := s.client.cli.ContainerInspect(s.ctx, s.containerID, client.ContainerInspectOptions{}); err == nil &&
		info.Container.Config != nil {
		tty = info.Container.Config.Tty
	}

	options := client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		// Docker's own timestamps, so a replayed line carries when it was
		// written. Without them every line was stamped on arrival, and history
		// from before Running Man attached was reported as having just happened.
		Timestamps: true,
		Since:      replaySince(s.history),
	}

	logStream, err := s.client.cli.ContainerLogs(s.ctx, s.containerID, options)
	if err != nil {
		return fmt.Errorf("failed to attach to container logs: %w", err)
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer logStream.Close()
		s.consume(logStream, tty)
	}()

	return nil
}

// replaySince renders the Since option for a replay window. It was "0" under
// the comment "Stream from now", but "0" is the Unix epoch: it replayed the
// container's entire history.
func replaySince(history time.Duration) string {
	if history <= 0 {
		// Now: replay nothing.
		return strconv.FormatInt(time.Now().Unix(), 10)
	}
	return strconv.FormatInt(time.Now().Add(-history).Unix(), 10)
}

// consume reads a container log stream until it ends, emitting one call to the
// handler per line.
//
// Split out from Start so the format handling is testable without a daemon.
func (s *ContainerStreamer) consume(stream io.Reader, tty bool) {
	stdout := &lineWriter{emit: func(line string) { s.emit(line, false) }}
	stderr := &lineWriter{emit: func(line string) { s.emit(line, true) }}
	// Deferred first so it runs last, after the partial lines are flushed.
	defer func() {
		if s.onEnd == nil {
			return
		}
		s.onEnd(s.name, false)
		if !tty {
			s.onEnd(s.name, true)
		}
	}()
	defer stdout.flush()
	defer stderr.flush()

	var err error
	if tty {
		// One raw stream. A TTY has no separate stderr.
		_, err = io.Copy(stdout, stream)
	} else {
		// stdcopy rather than the hand-rolled demultiplexer this replaced: it is
		// the Docker client's own implementation of the format, and it is
		// already in the module graph.
		_, err = stdcopy.StdCopy(stdout, stderr, stream)
	}
	if err != nil && !s.stopping(err) {
		termout.Errorf("[running-man] Error reading container logs for %s: %v\n", s.name, err)
	}
}

// emit passes one line to the handler, taking its time from the timestamp
// Docker prefixed it with.
func (s *ContainerStreamer) emit(line string, isStderr bool) {
	at, text := splitDockerTimestamp(line)

	// Pass-through to terminal with container name prefix. Suppressed by
	// termout while the TUI owns the screen.
	if isStderr {
		termout.Errorf("[%s] %s\n", s.name, text)
	} else {
		termout.Printf("[%s] %s\n", s.name, text)
	}

	if s.handler != nil {
		s.handler(s.name, text, at, isStderr)
	}
}

// splitDockerTimestamp separates the RFC3339Nano timestamp Docker prefixes to
// each line when Timestamps is requested. A line without one -- or with a
// prefix that does not parse -- is returned whole and stamped on arrival rather
// than mangled.
func splitDockerTimestamp(line string) (time.Time, string) {
	if i := strings.IndexByte(line, ' '); i > 0 {
		if at, err := time.Parse(time.RFC3339Nano, line[:i]); err == nil {
			return at, line[i+1:]
		}
	}
	return time.Now(), line
}

// lineWriter turns a byte stream into lines, holding a partial line until the
// rest of it arrives.
//
// Needed because a multiplexed frame boundary is not a line boundary: one line
// can span frames, and one frame can hold several lines.
type lineWriter struct {
	buf  []byte
	emit func(string)
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		w.emit(string(bytes.TrimSuffix(w.buf[:i], []byte{'\r'})))
		w.buf = w.buf[i+1:]
	}
	// A line with no end in sight is cut and marked, so one runaway writer
	// cannot hold an unbounded buffer.
	if len(w.buf) > maxContainerLineBytes {
		w.emit(string(w.buf[:maxContainerLineBytes]) +
			fmt.Sprintf(" [running-man: line truncated at %d bytes]", maxContainerLineBytes))
		w.buf = w.buf[:0]
	}
	return len(p), nil
}

// flush emits whatever is left: output that ended without a newline.
func (w *lineWriter) flush() {
	if len(w.buf) > 0 {
		w.emit(string(w.buf))
		w.buf = nil
	}
}

// Stop stops streaming logs
func (s *ContainerStreamer) Stop() error {
	s.cancel()
	return nil
}

// Wait waits for the streaming to complete
func (s *ContainerStreamer) Wait() error {
	s.wg.Wait()
	return nil
}

// stopping reports whether a read error is just the stream being shut down.
//
// On quit the context is cancelled under a blocking read and the HTTP body
// fails with context.Canceled. Reported as an error, that printed a line per
// container after "Shutting down processes..." -- seven of them on a Compose
// stack, all saying nothing had gone wrong.
func (s *ContainerStreamer) stopping(err error) bool {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, net.ErrClosed) {
		return true
	}
	// The context going away during the read is the same situation even when
	// the error does not say so: a cancelled request can surface as a transport
	// error of its own.
	return s.ctx.Err() != nil
}
