// Package process provides process wrapping and lifecycle management.
//
// SECURITY MODEL: All processes are executed via /bin/sh -c to enable full shell features
// (cd, &&, ||, pipes, redirections, variable expansion, etc.). This means shell metacharacters
// and command substitution will be interpreted.
//
// This is a local development tool. All commands come from sources the developer controls:
//   - CLI flags they type themselves
//   - Config files on their local filesystem
//   - Docker Compose files they created
//
// If an attacker can modify these sources, they already have full access to the system.
// There is no security boundary to defend - the user is intentionally running arbitrary commands.
//
// NOTE: Currently only supports Unix-like systems (macOS, Linux). Uses /bin/sh.
package process

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

// LineHandler is called for each line of output
type LineHandler func(source string, line string, timestamp time.Time, isStderr bool)

// ProcessWrapper wraps a child process and captures its output
type ProcessWrapper struct {
	cmd       *exec.Cmd
	name      string
	command   string
	args      []string
	stdout    io.ReadCloser
	stderr    io.ReadCloser
	handler   LineHandler
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	killTimer *time.Timer
	timerMu   sync.Mutex
	startTime time.Time
	stateMu   sync.RWMutex // Protects ProcessState reads
}

// New creates a new ProcessWrapper for the given command.
//
// Commands are executed via /bin/sh -c to enable shell features like cd, &&, pipes, etc.
// The command and args are joined with spaces to form the full shell command string.
//
// Example:
//
//	New("frontend", "cd", []string{"frontend", "&&", "npm", "start"}, handler)
//	Executes: /bin/sh -c "cd frontend && npm start"
//
// Or more simply:
//
//	New("frontend", "cd frontend && npm start", []string{}, handler)
//	Executes: /bin/sh -c "cd frontend && npm start"
func New(name string, command string, args []string, handler LineHandler) *ProcessWrapper {
	ctx, cancel := context.WithCancel(context.Background())

	// Build the full command string for shell execution
	var fullCommand string
	if len(args) == 0 {
		fullCommand = command
	} else {
		fullCommand = command + " " + strings.Join(args, " ")
	}

	// Execute command in shell to support cd, &&, pipes, etc.
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", fullCommand)
	cmd.Env = os.Environ() // Inherit environment

	return &ProcessWrapper{
		name:    name,
		command: command,
		args:    args,
		cmd:     cmd,
		handler: handler,
		ctx:     ctx,
		cancel:  cancel,
	}
}

// Start starts the wrapped process and begins capturing output
func (w *ProcessWrapper) Start() error {
	// Setup stdout pipe
	stdout, err := w.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	w.stdout = stdout

	// Setup stderr pipe
	stderr, err := w.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}
	w.stderr = stderr

	// Start the process
	if err := w.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start process: %w", err)
	}

	// Record start time
	w.startTime = time.Now()

	// Start goroutines to read stdout and stderr
	w.wg.Add(2)
	go w.captureStream(stdout, false)
	go w.captureStream(stderr, true)

	return nil
}

// captureStream reads lines from a stream and forwards them to the handler
func (w *ProcessWrapper) captureStream(stream io.ReadCloser, isStderr bool) {
	defer w.wg.Done()

	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024) // Support up to 1MB lines

	for scanner.Scan() {
		line := scanner.Text()
		timestamp := time.Now()

		// Pass-through to terminal with process name prefix
		if isStderr {
			fmt.Fprintf(os.Stderr, "[%s] %s\n", w.name, line)
		} else {
			fmt.Printf("[%s] %s\n", w.name, line)
		}

		// Call handler if provided
		if w.handler != nil {
			w.handler(w.name, line, timestamp, isStderr)
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "[running-man] Error reading %s stream: %v\n", w.name, err)
	}
}

// setupSignalHandlers configures graceful shutdown on SIGINT/SIGTERM
func (w *ProcessWrapper) setupSignalHandlers() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		fmt.Fprintf(os.Stderr, "\n[running-man] Received %v, stopping process...\n", sig)
		w.Stop()
	}()
}

// Wait waits for the process to complete and all output to be captured
func (w *ProcessWrapper) Wait() error {
	// Wait for process to exit
	err := w.cmd.Wait()

	// Cancel kill timer if process exited gracefully
	w.timerMu.Lock()
	if w.killTimer != nil {
		w.killTimer.Stop()
		w.killTimer = nil
	}
	w.timerMu.Unlock()

	// Wait for output streams to finish
	w.wg.Wait()

	return err
}

// Stop gracefully stops the process
func (w *ProcessWrapper) Stop() error {
	if w.cmd.Process != nil {
		// Send SIGINT first (graceful)
		if err := w.cmd.Process.Signal(os.Interrupt); err != nil {
			// If SIGINT fails, send SIGTERM
			return w.cmd.Process.Signal(syscall.SIGTERM)
		}

		// Give it 5 seconds to shut down gracefully
		w.timerMu.Lock()
		w.killTimer = time.AfterFunc(5*time.Second, func() {
			if w.cmd.Process != nil {
				fmt.Fprintf(os.Stderr, "[running-man] Process didn't stop gracefully, killing...\n")
				w.cmd.Process.Kill()
			}
		})
		w.timerMu.Unlock()
	}

	// Cancel context
	w.cancel()

	return nil
}

// ExitCode returns the exit code of the process, or -1 if still running
func (w *ProcessWrapper) ExitCode() int {
	w.stateMu.RLock()
	state := w.cmd.ProcessState
	w.stateMu.RUnlock()

	if state == nil {
		return -1
	}
	return state.ExitCode()
}

// PID returns the process ID, or -1 if not started.
// This method is safe to call concurrently.
func (w *ProcessWrapper) PID() int {
	w.stateMu.RLock()
	proc := w.cmd.Process
	w.stateMu.RUnlock()

	if proc == nil {
		return -1
	}
	return proc.Pid
}

// IsRunning returns true if the process is still running.
// Returns true for processes that haven't been started yet.
// This method is safe to call concurrently.
func (w *ProcessWrapper) IsRunning() bool {
	w.stateMu.RLock()
	state := w.cmd.ProcessState
	w.stateMu.RUnlock()

	return state == nil
}

// GetStatus returns the process status: "running", "stopped", or "failed".
// A process that exited with code 0 is "stopped", non-zero is "failed".
// This method is safe to call concurrently.
func (w *ProcessWrapper) GetStatus() string {
	w.stateMu.RLock()
	state := w.cmd.ProcessState
	w.stateMu.RUnlock()

	if state == nil {
		return "running"
	}
	if state.ExitCode() == 0 {
		return "stopped"
	}
	return "failed"
}

// StartTime returns when the process was started.
// Returns zero time if the process hasn't been started yet.
func (w *ProcessWrapper) StartTime() time.Time {
	return w.startTime
}

// CommandString returns the full command string including arguments.
// Returns just the command if no arguments were provided.
func (w *ProcessWrapper) CommandString() string {
	if len(w.args) == 0 {
		return w.command
	}
	return w.command + " " + strings.Join(w.args, " ")
}
