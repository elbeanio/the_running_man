//go:build !windows
// +build !windows

package process

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// ProcessConfig represents a process configuration
type ProcessConfig struct {
	Name           string
	Command        string
	Args           []string
	Shell          string // Shell to use (default: /bin/sh)
	RestartOnCrash bool   // Whether to restart process on crash (default: false)
}

// ProcessInfo contains runtime information about a process
type ProcessInfo struct {
	Name      string    `json:"name"`
	Command   string    `json:"command"`
	PID       int       `json:"pid"`       // -1 if not started
	Status    string    `json:"status"`    // "running", "stopped", "failed"
	ExitCode  int       `json:"exit_code"` // -1 for running processes
	StartTime time.Time `json:"start_time"`
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
		wrapper := NewWithOTEL(name, cfg.Command, cfg.Args, cfg.Shell, m.handler, m.otelEndpoint, m.otelPort, m.otelEnabled, m.silent)
		if err := wrapper.Start(); err != nil {
			// If any process fails to start, stop all started processes
			m.stopAllLocked()
			// Clear the processes map since all have been stopped
			m.processes = make(map[string]*ProcessWrapper)
			return fmt.Errorf("failed to start process %s: %w", name, err)
		}
		m.processes[name] = wrapper
	}

	// Setup signal handlers after all processes are started
	m.setupSignalHandlers()

	return nil
}

// Wait waits for all processes to complete
// Returns an error if any process exits with an error
// Automatically restarts crashed processes if restart_on_crash is enabled
func (m *Manager) Wait() error {
	m.mu.RLock()
	processNames := make([]string, 0, len(m.processes))
	for name := range m.processes {
		processNames = append(processNames, name)
	}
	m.mu.RUnlock()

	var firstErr error
	var mu sync.Mutex

	// Wait for each process and handle restarts
	var wg sync.WaitGroup
	for _, name := range processNames {
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
		if err := existing.Stop(); err != nil {
			fmt.Fprintf(os.Stderr, "[running-man] Warning: error stopping process %s: %v\n", processName, err)
		}
		existing.Wait()
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
		info := ProcessInfo{
			Name:      name,
			Command:   p.CommandString(),
			PID:       p.PID(),
			Status:    p.GetStatus(),
			StartTime: p.StartTime(),
			ExitCode:  p.ExitCode(), // Always include exit code (-1 for running processes)
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

	info := &ProcessInfo{
		Name:      name,
		Command:   p.CommandString(),
		PID:       p.PID(),
		Status:    p.GetStatus(),
		StartTime: p.StartTime(),
		ExitCode:  p.ExitCode(), // Always include exit code (-1 for running processes)
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
			m.Stop()
		case <-m.ctx.Done():
			// Cleanup on manager shutdown
			signal.Stop(m.sigChan)
			return
		}
	}()
}
