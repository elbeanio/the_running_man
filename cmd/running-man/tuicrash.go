package main

// Keeping the TUI alive, and leaving evidence when it is not.
//
// Bubble Tea catches panics in Update, View and command goroutines by default,
// and its handling is to shut the program down and print the panic to stdout
// with the stack to stderr (bubbletea@v1.3.10 tea.go:840). In an alt-screen TUI
// that someone has walked away from, the practical result is a dead program and
// no record: the stack went to a screen that has since been restored, scrolled
// or closed. That is exactly how the crashes reported here arrived -- "came back
// and it had quit", with nothing to go on.
//
// Two things change that. A recover of our own inside Update and View runs
// *before* Bubble Tea's, so a render bug degrades into a visible message instead
// of ending the session. And everything that still gets through lands in a file
// under the project directory rather than on the terminal.

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elbeanio/the_running_man/internal/instance"
	"github.com/elbeanio/the_running_man/internal/termout"
)

// crashLogName is the file panics are recorded in, beside the instance marker.
const crashLogName = "crash.log"

// debugLogName receives Bubble Tea's own trace when debugging is on.
const debugLogName = "tui-debug.log"

// debugEnvVar is shared with internal/termout, so one switch turns on both the
// TUI's own trace and the high-frequency diagnostics elsewhere.
const debugEnvVar = termout.DebugEnvVar

var (
	crashLogMu   sync.Mutex
	crashLogPath string

	// A panic in View repeats on every frame, so the same report would be
	// written many times a second. Identical consecutive reports are collapsed
	// and re-stated periodically, which keeps the log readable and bounded while
	// still showing that it is ongoing.
	lastSummary   string
	lastWrite     time.Time
	suppressed    int
	resummariseAt = 30 * time.Second
)

// CrashLogPath returns where crash reports are written for a project.
func CrashLogPath(projectDir string) string {
	return filepath.Join(projectDir, instance.DirName, crashLogName)
}

// installCrashLog points the Go runtime's crash output at a file and records
// where recovered panics should be appended.
//
// debug.SetCrashOutput covers what a recover cannot: an unrecovered panic on
// some other goroutine, a fatal runtime error such as a concurrent map write,
// and a fatal signal. Those kill the process outright, and on a restored
// terminal their output is routinely lost.
func installCrashLog(projectDir string) {
	path := CrashLogPath(projectDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return // Not worth failing a TUI launch over.
	}

	crashLogMu.Lock()
	crashLogPath = path
	crashLogMu.Unlock()

	// O_APPEND, because the interesting case is the second crash.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	// Deliberately not closed: the runtime holds this for the life of the
	// process and writes to it as it dies.
	_ = debug.SetCrashOutput(f, debug.CrashOptions{})
}

// installDebugLog turns on Bubble Tea's logging when RUNNING_MAN_DEBUG is set,
// writing it beside the crash log.
//
// Returns a closer, which is a no-op when debugging is off. Logging to a file is
// the only option for a TUI: stdout belongs to the interface.
func installDebugLog(projectDir string) func() {
	if os.Getenv(debugEnvVar) == "" {
		return func() {}
	}
	path := filepath.Join(projectDir, instance.DirName, debugLogName)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return func() {}
	}
	f, err := tea.LogToFile(path, "running-man")
	if err != nil {
		return func() {}
	}
	log.Printf("debug logging on; %s=%s", debugEnvVar, os.Getenv(debugEnvVar))
	return func() { _ = f.Close() }
}

// removeCrashLogIfEmpty deletes the crash log when nothing was ever written to
// it.
//
// debug.SetCrashOutput needs an open file up front, so the log is created on
// every launch whether or not anything crashes. Left behind, an empty crash.log
// says the opposite of the truth -- the troubleshooting guide tells people that
// an absent file means the TUI did not crash -- and it keeps .running-man from
// being removed on a clean exit.
//
// Called at the end of shutdown rather than when the TUI returns: unlinking the
// file while the runtime still holds it open means a fatal panic after this
// point would write to a file nobody can find, so the window is kept as small
// as possible.
func removeCrashLogIfEmpty() {
	crashLogMu.Lock()
	path := crashLogPath
	crashLogMu.Unlock()
	if path == "" {
		return
	}

	info, err := os.Stat(path)
	if err != nil || info.Size() > 0 {
		return
	}
	_ = os.Remove(path)
}

// recordPanic appends a recovered panic and its stack to the crash log, and
// returns a one-line summary to show the user.
//
// The stack is captured here rather than left to Bubble Tea, because by the time
// Bubble Tea sees a panic it is already shutting the program down.
func recordPanic(where string, r any, stack []byte) string {
	summary := fmt.Sprintf("%s panicked: %v", where, r)

	crashLogMu.Lock()
	path := crashLogPath
	crashLogMu.Unlock()
	if path == "" {
		return summary
	}

	crashLogMu.Lock()
	repeat := summary == lastSummary
	due := time.Since(lastWrite) >= resummariseAt
	if repeat && !due {
		suppressed++
		crashLogMu.Unlock()
		return summary
	}
	alsoSuppressed := suppressed
	lastSummary = summary
	lastWrite = time.Now()
	suppressed = 0
	crashLogMu.Unlock()

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return summary
	}
	defer f.Close()

	var b strings.Builder
	fmt.Fprintf(&b, "\n=== %s: recovered panic in %s ===\n",
		time.Now().Format(time.RFC3339), where)
	if alsoSuppressed > 0 {
		fmt.Fprintf(&b, "(the previous identical report repeated %d more times)\n", alsoSuppressed)
	}
	fmt.Fprintf(&b, "%v\n\n%s\n", r, stack)
	_, _ = f.WriteString(b.String())

	return summary
}
