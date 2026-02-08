package process

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
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
	stdout    io.ReadCloser
	stderr    io.ReadCloser
	handler   LineHandler
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	killTimer *time.Timer
	timerMu   sync.Mutex
}

// New creates a new ProcessWrapper for the given command
func New(name string, command string, args []string, handler LineHandler) *ProcessWrapper {
	ctx, cancel := context.WithCancel(context.Background())

	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Env = os.Environ() // Inherit environment

	return &ProcessWrapper{
		name:    name,
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

// ExitCode returns the exit code of the process
func (w *ProcessWrapper) ExitCode() int {
	if w.cmd.ProcessState == nil {
		return -1
	}
	return w.cmd.ProcessState.ExitCode()
}
