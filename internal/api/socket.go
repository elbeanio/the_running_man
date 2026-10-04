package api

// The API is served over a Unix socket rather than a TCP port.
//
// The reason is discovery, not security, though it helps both. With one
// instance per project and a TCP port, an agent working in project-b has no way
// to find project-b's instance: :9000 may well answer, from project-a, and the
// only fallback is scanning ports for something that may not be running at all.
// Reading the wrong project's logs is the worst kind of failure here, because
// the answer looks plausible.
//
// A socket path derived from the project directory dissolves the question. There
// is nothing to allocate, nothing to collide over, nothing to scan, and an agent
// in project-b physically cannot reach project-a's socket. It also keeps the
// documentation honest: a fixed project-relative path can be written in an
// example, where a discovered port never can.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// ErrInstanceLive reports that the socket already has something listening on it,
// so another instance owns this project.
var ErrInstanceLive = errors.New("another Running Man instance is serving this project")

// socketProbeTimeout bounds the liveness probe. Connecting to a Unix socket with
// a listener behind it is a local, immediate operation; anything slower than
// this is not a healthy instance worth deferring to.
const socketProbeTimeout = 250 * time.Millisecond

// Listen binds the API to a Unix socket, synchronously.
//
// Synchronously, and returning the error, because the previous TCP server was
// started in a goroutine that only printed its failure to stderr -- invisible
// under the TUI, which owns the screen. A second instance therefore came up with
// no API of its own and carried on as though it had one. Binding before anything
// else can depend on it is the fix, and #32 set the precedent for the OTLP
// receiver.
//
// Returns ErrInstanceLive when another instance holds the socket. A stale socket
// -- one with no listener, left behind by a SIGKILL, a panic or a lost terminal
// -- is taken over, because net.Listen refuses a path that already exists and
// one abrupt exit would otherwise make the project unstartable by hand.
func Listen(socketPath string) (net.Listener, error) {
	// 0700, not 0755: nothing outside this user needs to traverse into the
	// socket's directory, and when the socket falls back to a shared temp
	// directory the parent is the only thing standing between it and another
	// user's gaze. The marker inside stays 0644 for tools running as the owner.
	dir := filepath.Dir(socketPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("creating %s: %w", dir, err)
	}

	// Connecting is the only way to tell a live socket from an abandoned one:
	// both are a file at the same path, and os.Stat cannot distinguish them.
	if conn, err := net.DialTimeout("unix", socketPath, socketProbeTimeout); err == nil {
		conn.Close()
		return nil, ErrInstanceLive
	}

	if _, err := os.Lstat(socketPath); err == nil {
		if err := os.Remove(socketPath); err != nil {
			return nil, fmt.Errorf("removing stale socket %s: %w", socketPath, err)
		}
	}

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("listening on %s: %w", socketPath, err)
	}

	// 0600 is the entire access-control story now that there is no caller-IP
	// check: another user on this machine must not be able to read a project's
	// logs, which routinely contain tokens and connection strings.
	//
	// Set after binding rather than before, since the socket does not exist
	// until then. The window is between this process's own two syscalls, and
	// the containing directory is not writable by other users anyway.
	if err := os.Chmod(socketPath, 0o600); err != nil {
		ln.Close()
		return nil, fmt.Errorf("setting permissions on %s: %w", socketPath, err)
	}

	return ln, nil
}

// Serve serves the API on an already-bound listener until it is closed.
//
// Split from Listen so that binding can fail before anything else starts, while
// serving runs for the life of the instance.
func (s *Server) Serve(ln net.Listener) error {
	if err := http.Serve(ln, s.routes()); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// NewSocketClient returns an http.Client that talks to a Running Man socket.
//
// The TUI is a client of this API like any other -- it fetches traces and health
// over HTTP rather than reading the buffer in process -- so it needs the same
// transport. The URL's host is ignored, so callers use http://localhost and let
// the dialler decide where that actually goes.
func NewSocketClient(socketPath string) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socketPath)
			},
		},
	}
}
