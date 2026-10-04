// Package termout is the one way the rest of Running Man writes to the
// terminal, so that it can stop.
//
// While the TUI is running it owns the screen: it draws a full-height frame and
// repaints only the lines it believes have changed. Anything else writing to
// stdout or stderr in the meantime scrolls that frame, moves the cursor out from
// under it, or -- with an unlucky escape sequence -- clears it, and the renderer
// has no reason to repaint what it did not know was lost.
//
// This was not a rare event. internal/tracing printed a line for every OTLP
// request received, so an instrumented application corrupted the display
// continuously; the API server, the process manager and the container streamer
// all had unconditional error paths doing the same thing.
//
// Process output is a separate mechanism and already gated: the wrapper and the
// streamer take a `silent` flag, set when the TUI is in use. This is for
// everything else -- the diagnostics that had no gate at all.
package termout

import (
	"fmt"
	"io"
	"os"
	"sync/atomic"
)

// DebugEnvVar turns on per-request and other high-frequency diagnostics.
//
// They are off by default because they are noise: the OTLP receiver used to
// print a line for every request it handled, which is one line per span batch
// from every instrumented process. Useful when chasing something, intolerable
// the rest of the time, and the largest single source of TUI corruption.
const DebugEnvVar = "RUNNING_MAN_DEBUG"

// debug is read once: the environment does not change under a running process,
// and this is consulted on a hot path.
var debug = os.Getenv(DebugEnvVar) != ""

// quiet suppresses terminal writes. Atomic because the writers are the API
// server's handlers, the OTLP receiver's handlers and the process manager's
// goroutines, none of which share a lock with the TUI.
var quiet atomic.Bool

// Silence suppresses terminal writes and returns a function that restores them.
//
// Call it before handing the screen to a TUI. The returned restore makes
// shutdown messages visible again, which matters because they are the last thing
// a user sees.
func Silence() (restore func()) {
	quiet.Store(true)
	return func() { quiet.Store(false) }
}

// Quiet reports whether terminal writes are currently suppressed, for callers
// that want to skip assembling an expensive message.
func Quiet() bool { return quiet.Load() }

// Printf writes to stdout unless the terminal is in use by something else.
func Printf(format string, a ...any) {
	if quiet.Load() {
		return
	}
	fmt.Fprintf(os.Stdout, format, a...)
}

// Errorf writes to stderr unless the terminal is in use by something else.
//
// Suppressed on the same terms as Printf: stderr and stdout are the same
// terminal, and a diagnostic on stderr corrupts a frame just as effectively.
func Errorf(format string, a ...any) {
	if quiet.Load() {
		return
	}
	fmt.Fprintf(os.Stderr, format, a...)
}

// Debugf writes a high-frequency diagnostic, but only when DebugEnvVar is set.
func Debugf(format string, a ...any) {
	if !debug || quiet.Load() {
		return
	}
	fmt.Fprintf(os.Stdout, format, a...)
}

// Debug reports whether high-frequency diagnostics are enabled.
func Debug() bool { return debug }

// Writer returns stdout, or io.Discard while writes are suppressed, for the few
// callers that need an io.Writer rather than a print call.
func Writer() io.Writer {
	if quiet.Load() {
		return io.Discard
	}
	return os.Stdout
}
