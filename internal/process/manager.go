//go:build !windows
// +build !windows

package process

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ProcessConfig represents a process configuration
type ProcessConfig struct {
	Name           string
	Type           string // web, api, worker, database, cache, etc.
	Description    string // Free-text description of the process
	URL            string // Optional URL for web applications
	Command        string
	Args           []string
	Shell          string // Shell to use (default: /bin/sh)
	RestartOnCrash bool   // Whether to restart process on crash (default: false)
	Interval       string // Interval for recurring execution (e.g., "1m", "30s", "5h")
}

// ProcessInfo contains runtime information about a process
type ProcessInfo struct {
	Name        string    `json:"name"`
	Type        string    `json:"type,omitempty"`        // web, api, worker, database, cache, etc.
	Description string    `json:"description,omitempty"` // Free-text description of the process
	URL         string    `json:"url,omitempty"`         // Optional URL for web applications
	Command     string    `json:"command"`
	PID         int       `json:"pid"`       // -1 if not started
	Status      string    `json:"status"`    // "running", "stopped", "failed"
	ExitCode    int       `json:"exit_code"` // -1 for running processes
	StartTime   time.Time `json:"start_time"`
	Interval    string    `json:"interval,omitempty"` // Interval for recurring execution (e.g., "1m", "30s")
}

// Manager manages multiple ProcessWrappers
type Manager struct {
	processes map[string]*ProcessWrapper
	configs   map[string]ProcessConfig
	handler   LineHandler
	mu        sync.RWMutex
	sigChan   chan os.Signal
	ctx       context.Context
	cancel    context.CancelFunc

	// OTEL configuration
	otelEndpoint string
	otelPort     int
	otelEnabled  bool
	silent       bool // When true, wrappers won't print to stdout/stderr (for TUI mode)
}

// NewManager creates a new Manager for multiple processes
func NewManager(configs []ProcessConfig, handler LineHandler) *Manager {
	return NewManagerWithOTEL(configs, handler, "", 0, false, false)
}

// NewManagerWithOTEL creates a new Manager with OpenTelemetry support
// If silent is true, process wrappers won't print to stdout/stderr (for TUI mode)
func NewManagerWithOTEL(configs []ProcessConfig, handler LineHandler, otelEndpoint string, otelPort int, otelEnabled bool, silent bool) *Manager {
	ctx, cancel := context.WithCancel(context.Background())

	m := &Manager{
		processes:    make(map[string]*ProcessWrapper),
		configs:      make(map[string]ProcessConfig),
		handler:      handler,
		sigChan:      make(chan os.Signal, 1),
		ctx:          ctx,
		cancel:       cancel,
		otelEndpoint: otelEndpoint,
		otelPort:     otelPort,
		otelEnabled:  otelEnabled,
		silent:       silent,
	}

	// Store configs for later restart
	for _, cfg := range configs {
		m.configs[cfg.Name] = cfg
	}

	return m
}

// Start starts all managed processes
func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Start each process
	for name, cfg := range m.configs {
		// Check if this is a recurring process
		if cfg.Interval != "" {
			// Parse interval duration
			interval, err := time.ParseDuration(cfg.Interval)
			if err != nil {
				// Should not happen if Validate() was called
				return fmt.Errorf("invalid interval for process %s: %w", name, err)
			}

			// Start recurring process
			wrapper := NewWithOTEL(name, cfg.Command, cfg.Args, cfg.Shell, m.handler, m.otelEndpoint, m.otelPort, m.otelEnabled, m.silent)
			m.processes[name] = wrapper

			// Start the recurring execution in a goroutine
			go m.startRecurringProcess(name, cfg, interval)
		} else {
			// Start regular (non-recurring) process
			wrapper := NewWithOTEL(name, cfg.Command, cfg.Args, cfg.Shell, m.handler, m.otelEndpoint, m.otelPort, m.otelEnabled, m.silent)
			if err := wrapper.Start(); err != nil {
				// If any process fails to start, stop all started processes
				if err := m.stopAllLocked(); err != nil {
					fmt.Printf("[running-man] Failed to stop processes during cleanup: %v\n", err)
				}
				// Clear the processes map since all have been stopped
				m.processes = make(map[string]*ProcessWrapper)
				return fmt.Errorf("failed to start process %s: %w", name, err)
			}
			m.processes[name] = wrapper
		}
	}

	// Setup signal handlers after all processes are started
	m.setupSignalHandlers()

	return nil
}

// startRecurringProcess starts a process that runs at regular intervals
func (m *Manager) startRecurringProcess(name string, cfg ProcessConfig, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run immediately on start
	m.runRecurringProcess(name, cfg)

	for {
		select {
		case <-m.ctx.Done():
			// Context cancelled, stop the recurring process
			return
		case <-ticker.C:
			// Interval elapsed, run the process again
			m.runRecurringProcess(name, cfg)
		}
	}
}

// runRecurringProcess executes a single instance of a recurring process
func (m *Manager) runRecurringProcess(name string, cfg ProcessConfig) {
	// Create a new wrapper for this execution
	wrapper := NewWithOTEL(name, cfg.Command, cfg.Args, cfg.Shell, m.handler, m.otelEndpoint, m.otelPort, m.otelEnabled, m.silent)

	// Start the process
	if err := wrapper.Start(); err != nil {
		fmt.Printf("[running-man] Failed to start recurring process %s: %v\n", name, err)
		return
	}

	// Wait for the process to complete
	err := wrapper.Wait()
	if err != nil {
		fmt.Printf("[running-man] Recurring process %s exited with error: %v\n", name, err)
	}
}

// Wait waits for all processes to complete
// Returns an error if any process exits with an error
// Automatically restarts crashed processes if restart_on_crash is enabled
// Note: Recurring processes (with interval) run forever until context is cancelled
func (m *Manager) Wait() error {
	m.mu.RLock()
	// Filter out recurring processes
	nonRecurringProcesses := make([]string, 0)
	for name := range m.processes {
		cfg, hasCfg := m.configs[name]
		if hasCfg && cfg.Interval != "" {
			// This is a recurring process, skip it (runs forever)
			continue
		}
		nonRecurringProcesses = append(nonRecurringProcesses, name)
	}
	m.mu.RUnlock()

	var firstErr error
	var mu sync.Mutex

	// Wait for each non-recurring process and handle restarts
	var wg sync.WaitGroup
	for _, name := range nonRecurringProcesses {
		wg.Add(1)
		go func(processName string) {
			defer wg.Done()

			for {
				// Get current wrapper
				m.mu.RLock()
				wrapper, exists := m.processes[processName]
				cfg, hasCfg := m.configs[processName]
				m.mu.RUnlock()

				if !exists || !hasCfg {
					return
				}

				// Wait for process to exit
				err := wrapper.Wait()
				exitCode := wrapper.ExitCode()

				// Check if we should restart
				shouldRestart := cfg.RestartOnCrash && exitCode != 0

				// Check if we're shutting down (context cancelled)
				select {
				case <-m.ctx.Done():
					// Manager is shutting down, don't restart
					return
				default:
				}

				if !shouldRestart {
					// Process exited cleanly or restart not enabled
					if err != nil {
						mu.Lock()
						if firstErr == nil {
							firstErr = err
						}
						mu.Unlock()
					}
					return
				}

				// Log restart
				m.handler(processName, fmt.Sprintf("Process crashed with exit code %d, restarting...", exitCode), time.Now(), true)

				// Create new wrapper and restart
				newWrapper := NewWithOTEL(processName, cfg.Command, cfg.Args, cfg.Shell, m.handler, m.otelEndpoint, m.otelPort, m.otelEnabled, m.silent)
				if err := newWrapper.Start(); err != nil {
					m.handler(processName, fmt.Sprintf("Failed to restart: %v", err), time.Now(), true)
					mu.Lock()
					if firstErr == nil {
						firstErr = fmt.Errorf("failed to restart process %s: %w", processName, err)
					}
					mu.Unlock()
					return
				}

				// Update wrapper in map
				m.mu.Lock()
				m.processes[processName] = newWrapper
				m.mu.Unlock()

				// Continue loop to wait for the restarted process
			}
		}(name)
	}

	wg.Wait()

	// Check if we have any recurring processes (with Interval)
	hasRecurring := false
	m.mu.RLock()
	for _, cfg := range m.configs {
		if cfg.Interval != "" {
			hasRecurring = true
			break
		}
	}
	m.mu.RUnlock()

	// Only wait for context cancellation if we have recurring processes
	if hasRecurring {
		<-m.ctx.Done()
	}

	return firstErr
}

// Stop stops all managed processes
func (m *Manager) Stop() error {
	// Cleanup signal handler
	signal.Stop(m.sigChan)
	m.cancel()

	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopAllLocked()
}

// stopAllLocked stops all processes (must be called with lock held)
func (m *Manager) stopAllLocked() error {
	var firstErr error

	for name, p := range m.processes {
		if err := p.Stop(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("failed to stop process %s: %w", name, err)
		}
	}

	return firstErr
}

// Restart stops and restarts a specific process by name
func (m *Manager) Restart(processName string) error {
	m.mu.Lock()
	cfg, exists := m.configs[processName]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("process %s not found", processName)
	}

	var existing *ProcessWrapper
	if proc, ok := m.processes[processName]; ok {
		existing = proc
		delete(m.processes, processName) // Remove from map while locked
	}
	m.mu.Unlock()

	// Wait outside the lock to avoid blocking other operations
	if existing != nil {
		// Log restart message if handler is available
		if m.handler != nil {
			m.handler(processName, "Restarting process...", time.Now(), false)
		}

		if err := existing.Stop(); err != nil {
			// Log warning through handler if available
			if m.handler != nil {
				m.handler("running-man", fmt.Sprintf("Warning: error stopping process %s: %v", processName, err), time.Now(), true)
			}
		}
		if err := existing.Wait(); err != nil {
			// Ignore "Wait was already called" errors - process already exited
			// Also ignore expected exit statuses when killing processes
			if !strings.Contains(err.Error(), "Wait was already called") &&
				!strings.Contains(err.Error(), "signal: killed") &&
				!strings.Contains(err.Error(), "signal: terminated") &&
				!strings.Contains(err.Error(), "exit status 1") && // Common when killed
				!strings.Contains(err.Error(), "exit status 137") { // SIGKILL
				// Log warning through handler if available
				if m.handler != nil {
					m.handler("running-man", fmt.Sprintf("Warning: error waiting for process %s: %v", processName, err), time.Now(), true)
				}
			}
		}
	}

	// Start new instance
	wrapper := NewWithOTEL(cfg.Name, cfg.Command, cfg.Args, cfg.Shell, m.handler, m.otelEndpoint, m.otelPort, m.otelEnabled, m.silent)
	if err := wrapper.Start(); err != nil {
		return fmt.Errorf("failed to restart process %s: %w", processName, err)
	}

	m.mu.Lock()
	m.processes[processName] = wrapper
	m.mu.Unlock()

	return nil
}

// ExitCodes returns a map of process names to their exit codes
func (m *Manager) ExitCodes() map[string]int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	codes := make(map[string]int)
	for name, p := range m.processes {
		codes[name] = p.ExitCode()
	}
	return codes
}

// ListProcesses returns information about all managed processes
func (m *Manager) ListProcesses() []ProcessInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	infos := make([]ProcessInfo, 0, len(m.processes))
	for name, p := range m.processes {
		config, hasConfig := m.configs[name]
		info := ProcessInfo{
			Name:      name,
			Command:   p.CommandString(),
			PID:       p.PID(),
			Status:    p.GetStatus(),
			StartTime: p.StartTime(),
			ExitCode:  p.ExitCode(), // Always include exit code (-1 for running processes)
		}
		// Add config fields if available
		if hasConfig {
			info.Type = config.Type
			info.Description = config.Description
			info.URL = config.URL
			info.Interval = config.Interval
		}
		infos = append(infos, info)
	}
	return infos
}

// ProcessNames returns a list of all managed process names.
// This is more efficient than ListProcesses when only names are needed.
// This method is safe to call concurrently.
func (m *Manager) ProcessNames() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.processes))
	for name := range m.processes {
		names = append(names, name)
	}
	return names
}

// GetProcess returns information about a specific process
func (m *Manager) GetProcess(name string) (*ProcessInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	p, exists := m.processes[name]
	if !exists {
		return nil, fmt.Errorf("process %s not found", name)
	}

	config, hasConfig := m.configs[name]
	info := &ProcessInfo{
		Name:      name,
		Command:   p.CommandString(),
		PID:       p.PID(),
		Status:    p.GetStatus(),
		StartTime: p.StartTime(),
		ExitCode:  p.ExitCode(), // Always include exit code (-1 for running processes)
	}
	// Add config fields if available
	if hasConfig {
		info.Type = config.Type
		info.Description = config.Description
		info.URL = config.URL
		info.Interval = config.Interval
	}
	return info, nil
}

// setupSignalHandlers configures graceful shutdown on SIGINT/SIGTERM
func (m *Manager) setupSignalHandlers() {
	signal.Notify(m.sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		select {
		case sig := <-m.sigChan:
			fmt.Fprintf(os.Stderr, "\n[running-man] Received %v, stopping all processes...\n", sig)
			if err := m.Stop(); err != nil {
				fmt.Fprintf(os.Stderr, "[running-man] Error stopping processes on signal: %v\n", err)
			}
		case <-m.ctx.Done():
			// Cleanup on manager shutdown
			signal.Stop(m.sigChan)
			return
		}
	}()
}
