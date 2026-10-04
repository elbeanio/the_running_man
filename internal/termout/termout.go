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
	"os"
	"sync"
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

// silences counts active Silence calls rather than holding a flag, because they
// nest: `running-man run` silences as soon as it knows a TUI is coming, and the
// TUI silences again when it takes the screen. With a flag, the inner restore
// would un-silence while the outer caller still expected quiet.
//
// Atomic because the writers are the API server's handlers, the OTLP receiver's
// handlers and the process and container readers, none of which share a lock
// with the TUI.
var silences atomic.Int64

// Silence suppresses terminal writes and returns a function that restores them.
//
// Call it before handing the screen to a TUI. The returned restore makes
// shutdown messages visible again, which matters because they are the last thing
// a user sees. Restoring more than once is harmless.
func Silence() (restore func()) {
	silences.Add(1)
	var once sync.Once
	return func() {
		once.Do(func() {
			if silences.Add(-1) < 0 {
				silences.Store(0)
			}
		})
	}
}

// Quiet reports whether terminal writes are currently suppressed, for callers
// that want to skip assembling an expensive message.
func Quiet() bool { return silences.Load() > 0 }

// Printf writes to stdout unless the terminal is in use by something else.
func Printf(format string, a ...any) {
	if Quiet() {
		return
	}
	fmt.Fprintf(os.Stdout, format, a...)
}

// Errorf writes to stderr unless the terminal is in use by something else.
//
// Suppressed on the same terms as Printf: stderr and stdout are the same
// terminal, and a diagnostic on stderr corrupts a frame just as effectively.
func Errorf(format string, a ...any) {
	if Quiet() {
		return
	}
	fmt.Fprintf(os.Stderr, format, a...)
}

// Debugf writes a high-frequency diagnostic, but only when DebugEnvVar is set.
func Debugf(format string, a ...any) {
	if !debug || Quiet() {
		return
	}
	fmt.Fprintf(os.Stdout, format, a...)
}
