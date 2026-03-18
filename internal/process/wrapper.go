//go:build !windows
// +build !windows

// Package process provides process wrapping and lifecycle management.
//
// SECURITY MODEL: All processes are executed via shell -c to enable full shell features
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
// NOTE: Currently only supports Unix-like systems (macOS, Linux). Default shell: /bin/sh.
package process

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
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
	shell     string
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
	silent    bool         // When true, don't print to stdout/stderr (for TUI mode)
}

// New creates a new ProcessWrapper for the given command.
//
// Commands are executed via shell -c to enable shell features like cd, &&, pipes, etc.
// The command and args are joined with spaces to form the full shell command string.
// If shell is empty, defaults to /bin/sh.
//
// Example:
//
//	New("frontend", "cd", []string{"frontend", "&&", "npm", "start"}, "/bin/bash", handler)
//	Executes: /bin/bash -c "cd frontend && npm start"
//
// Or more simply:
//
//	New("frontend", "cd frontend && npm start", []string{}, "/bin/bash", handler)
//	Executes: /bin/bash -c "cd frontend && npm start"
func New(name string, command string, args []string, shell string, handler LineHandler) *ProcessWrapper {
	return NewWithOTEL(name, command, args, shell, handler, "", 0, false, false)
}

// NewWithOTEL creates a new ProcessWrapper with OpenTelemetry environment variable injection.
// If otelEndpoint is not empty and otelEnabled is true, OTEL environment variables will be injected.
// If silent is true, the wrapper will not print to stdout/stderr (for TUI mode).
func NewWithOTEL(name string, command string, args []string, shell string, handler LineHandler, otelEndpoint string, otelPort int, otelEnabled bool, silent bool) *ProcessWrapper {
	ctx, cancel := context.WithCancel(context.Background())

	// Default to /bin/sh if no shell specified
	if shell == "" {
		shell = "/bin/sh"
	}

	// Build the full command string for shell execution
	var fullCommand string
	if len(args) == 0 {
		fullCommand = command
	} else {
		fullCommand = command + " " + strings.Join(args, " ")
	}

	// Execute command in shell to support cd, &&, pipes, etc.
	cmd := exec.CommandContext(ctx, shell, "-c", fullCommand)

	// Set up session for proper termination of shell and all child processes
	// Using Setsid instead of Setpgid creates a new session which is better for
	// killing entire process trees (all descendants are in same session)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true, // Create new session
	}

	// Start with inherited environment
	env := os.Environ()

	// Inject OTEL environment variables if enabled
	if otelEnabled && otelEndpoint != "" {
		// Build full endpoint URL with port if specified
		endpoint := otelEndpoint
		if otelPort > 0 {
			endpoint = fmt.Sprintf("%s:%d", otelEndpoint, otelPort)
		}

		// Create OTEL env vars and inject
		otelVars := map[string]string{
			"OTEL_EXPORTER_OTLP_ENDPOINT": endpoint,
			"OTEL_SERVICE_NAME":           name,
			"OTEL_PROPAGATORS":            "tracecontext,baggage",
			"OTEL_EXPORTER_OTLP_PROTOCOL": "http/protobuf",
			"OTEL_RESOURCE_ATTRIBUTES":    "deployment.environment=local",
			"OTEL_TRACES_SAMPLER":         "always_on",
			"OTEL_METRICS_SAMPLER":        "always_on",
			"OTEL_LOGS_SAMPLER":           "always_on",
		}

		// Remove any existing OTEL vars first
		var filteredEnv []string
		for _, e := range env {
			if !strings.HasPrefix(e, "OTEL_") {
				filteredEnv = append(filteredEnv, e)
			}
		}

		// Add OTEL vars (prepend so they take precedence)
		for key, value := range otelVars {
			filteredEnv = append([]string{fmt.Sprintf("%s=%s", key, value)}, filteredEnv...)
		}

		cmd.Env = filteredEnv
	} else {
		cmd.Env = env
	}

	return &ProcessWrapper{
		name:    name,
		command: command,
		args:    args,
		shell:   shell,
		cmd:     cmd,
		handler: handler,
		ctx:     ctx,
		cancel:  cancel,
		silent:  silent,
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

		// Pass-through to terminal with process name prefix (only when not silent)
		if !w.silent {
			if isStderr {
				fmt.Fprintf(os.Stderr, "[%s] %s\n", w.name, line)
			} else {
				fmt.Printf("[%s] %s\n", w.name, line)
			}
		}

		// Call handler if provided
		if w.handler != nil {
			w.handler(w.name, line, timestamp, isStderr)
		}
	}

	if err := scanner.Err(); err != nil {
		if !w.silent {
			fmt.Fprintf(os.Stderr, "[running-man] Error reading %s stream: %v\n", w.name, err)
		}
	}
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

// Stop gracefully stops the process and all its children
func (w *ProcessWrapper) Stop() error {
	if w.cmd.Process != nil {
		pid := w.cmd.Process.Pid
		// Try to get session ID first (most effective for killing entire tree)
		sid, err := syscall.Getsid(pid)
		hasSid := err == nil && sid > 0

		if hasSid {
			// We have SID, kill entire session (all processes in session)
			// Send SIGTERM to entire session
			if err := syscall.Kill(-sid, syscall.SIGTERM); err != nil && !isNoSuchProcess(err) {
				fmt.Fprintf(os.Stderr, "[running-man] Failed to send SIGTERM to session %d: %v\n", -sid, err)
			}
		} else {
			// Fall back to process group
			pgid, err := syscall.Getpgid(pid)
			hasPgid := err == nil && pgid > 0

			if hasPgid {
				// We have PGID, kill entire process group
				// Send SIGTERM to entire process group
				if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil && !isNoSuchProcess(err) {
					fmt.Fprintf(os.Stderr, "[running-man] Failed to send SIGTERM to process group %d: %v\n", -pgid, err)
				}
			} else {
				// If we can't get PGID, kill just the process
				// Send SIGTERM to process
				if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !isNoSuchProcess(err) {
					fmt.Fprintf(os.Stderr, "[running-man] Failed to send SIGTERM to process %d: %v\n", pid, err)
				}
			}
		}

		// Also proactively find and kill any child processes
		// This helps with processes like Uvicorn that create child processes
		// Run in goroutine so it doesn't block
		go func() {
			// Give processes a moment to start shutting down
			time.Sleep(100 * time.Millisecond)
			findAndKillChildProcesses(pid)
		}()

		// Give it 2 seconds to shut down gracefully (reduced from 5 for faster restart)
		w.timerMu.Lock()
		w.killTimer = time.AfterFunc(2*time.Second, func() {
			w.timerMu.Lock()
			defer w.timerMu.Unlock()

			// Check if process is still running
			if w.cmd.Process != nil && w.IsRunning() {
				fmt.Fprintf(os.Stderr, "[running-man] Process didn't stop gracefully, sending SIGKILL...\n")
				// Re-get PID and SID/PGID since they might have changed
				currentPid := w.cmd.Process.Pid
				currentSid, err := syscall.Getsid(currentPid)
				currentHasSid := err == nil && currentSid > 0

				// Kill forcefully with SIGKILL
				if currentHasSid {
					// Try to kill session if we have it
					if err := syscall.Kill(-currentSid, syscall.SIGKILL); err != nil && !isNoSuchProcess(err) {
						fmt.Fprintf(os.Stderr, "[running-man] Failed to send SIGKILL to session %d: %v\n", -currentSid, err)
					}
				} else {
					// Fall back to process group
					currentPgid, err := syscall.Getpgid(currentPid)
					currentHasPgid := err == nil && currentPgid > 0

					if currentHasPgid {
						// Try to kill process group if we have it
						if err := syscall.Kill(-currentPgid, syscall.SIGKILL); err != nil && !isNoSuchProcess(err) {
							fmt.Fprintf(os.Stderr, "[running-man] Failed to send SIGKILL to process group %d: %v\n", -currentPgid, err)
						}
					} else {
						// Kill just the process
						if err := syscall.Kill(currentPid, syscall.SIGKILL); err != nil && !isNoSuchProcess(err) {
							fmt.Fprintf(os.Stderr, "[running-man] Failed to send SIGKILL to process %d: %v\n", currentPid, err)
						}
					}
				}
				// Also kill any remaining child processes
				findAndKillChildProcesses(currentPid)
			}
		})
		w.timerMu.Unlock()
	}

	// Cancel context
	w.cancel()

	return nil
}

// isNoSuchProcess checks if a kill error is because the process doesn't exist
func isNoSuchProcess(err error) bool {
	// Check for "no such process" errors which are harmless when stopping
	return err != nil && (err.Error() == "no such process" ||
		err.Error() == "process already finished" ||
		err.Error() == "os: process already finished")
}

// killProcessTree kills a process and all its children
func killProcessTree(pid int) {
	// Try to get session ID (SID)
	sid, err := syscall.Getsid(pid)
	if err == nil && sid > 0 {
		// Kill entire session (all processes in session)
		syscall.Kill(-sid, syscall.SIGTERM)
		time.Sleep(100 * time.Millisecond)
		syscall.Kill(-sid, syscall.SIGKILL)
	} else {
		// Fall back to process group
		pgid, err := syscall.Getpgid(pid)
		if err == nil && pgid > 0 {
			// Kill entire process group
			syscall.Kill(-pgid, syscall.SIGTERM)
			time.Sleep(100 * time.Millisecond)
			syscall.Kill(-pgid, syscall.SIGKILL)
		} else {
			// Fall back to killing just the process
			syscall.Kill(pid, syscall.SIGTERM)
			time.Sleep(100 * time.Millisecond)
			syscall.Kill(pid, syscall.SIGKILL)
		}
	}
}

// findAndKillChildProcesses finds and kills all child processes of the given PID
func findAndKillChildProcesses(parentPid int) {
	// Use ps to find all processes with PPID = parentPid
	cmd := exec.Command("ps", "-o", "pid=", "-o", "ppid=", "-A")
	output, err := cmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[running-man] Warning: failed to list processes: %v\n", err)
		return
	}

	// Parse output to build parent-child relationships
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	childPids := make(map[int]bool)

	// First pass: find direct children
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			pid, _ := strconv.Atoi(fields[0])
			ppid, _ := strconv.Atoi(fields[1])
			if ppid == parentPid {
				childPids[pid] = true
			}
		}
	}

	// Second pass: find grandchildren (children of children)
	// Keep looping until no new children are found
	changed := true
	for changed {
		changed = false
		for _, line := range lines {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				pid, _ := strconv.Atoi(fields[0])
				ppid, _ := strconv.Atoi(fields[1])
				if childPids[ppid] && !childPids[pid] {
					childPids[pid] = true
					changed = true
				}
			}
		}
	}

	// Kill all child processes
	for childPid := range childPids {
		// Try SIGTERM first, then SIGKILL
		syscall.Kill(childPid, syscall.SIGTERM)
		time.Sleep(50 * time.Millisecond)
		syscall.Kill(childPid, syscall.SIGKILL)
	}

	if len(childPids) > 0 {
		fmt.Fprintf(os.Stderr, "[running-man] Killed %d child process(es) of PID %d\n", len(childPids), parentPid)
	}
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
