//go:build !windows

package process

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/elbeanio/the_running_man/internal/health"
	"github.com/elbeanio/the_running_man/internal/termout"
)

// Healthcheck says when a process is ready, for whatever depends on it.
// Exactly one of Port, HTTP and Log is set; config validation ensures it.
type Healthcheck struct {
	Port    int
	HTTP    string
	Log     string
	Timeout time.Duration
}

// String describes the check for messages: `port 8000`, `log "ready"`.
func (h *Healthcheck) String() string {
	switch {
	case h.Port != 0:
		return fmt.Sprintf("port %d", h.Port)
	case h.HTTP != "":
		return "http " + h.HTTP
	default:
		return fmt.Sprintf("log %q", h.Log)
	}
}

// Startup statuses, alongside the wrapper's running/stopped/failed.
const (
	// StatusPending is a process not started yet because a dependency is
	// not ready.
	StatusPending = "pending"
	// StatusStarting is a running process whose healthcheck has not passed.
	StatusStarting = "starting"
	// StatusBlocked is a process that will not start, because a dependency
	// never became ready and startup stopped.
	StatusBlocked = "blocked"
)

// ServiceReadiness blocks until the named Compose service is ready, or
// returns why it is not. It applies its own timeout. Supplied by the caller,
// which knows how to reach Docker.
type ServiceReadiness func(ctx context.Context, service string) error

// readiness is the outcome of one dependency's healthcheck, decided once.
type readiness struct {
	done chan struct{} // closed when decided
	err  error         // nil when ready; read only after done
}

func newReadiness() *readiness { return &readiness{done: make(chan struct{})} }

// startup holds the dependency state: which processes are waiting, which
// dependencies are ready, and whether startup has been stopped.
type startup struct {
	mu sync.Mutex

	// ready is each dependency's readiness, by process or service name.
	ready map[string]*readiness
	// checked records which processes' healthchecks have been launched; a
	// healthcheck runs for the first run only.
	checked map[string]bool
	// matchers feed a process's output to its log healthcheck.
	matchers map[string]*health.LineMatcher

	// gated are the processes with dependencies, closed when each is
	// decided: started, or blocked.
	gated map[string]chan struct{}
	// pending and blocked are the gated processes' current startup status.
	pending map[string]bool
	blocked map[string]string // name → why it will not start

	// halted is closed when startup stops; haltErr says why.
	halted   chan struct{}
	haltOnce sync.Once
	haltErr  error
}

func newStartup(configs map[string]ProcessConfig) *startup {
	s := &startup{
		ready:    map[string]*readiness{},
		checked:  map[string]bool{},
		matchers: map[string]*health.LineMatcher{},
		gated:    map[string]chan struct{}{},
		pending:  map[string]bool{},
		blocked:  map[string]string{},
		halted:   make(chan struct{}),
	}
	for name, cfg := range configs {
		if hc := cfg.Healthcheck; hc != nil && hc.Log != "" {
			s.matchers[name] = health.NewLineMatcher(hc.Log)
		}
		if len(cfg.DependsOn) > 0 {
			s.gated[name] = make(chan struct{})
			s.pending[name] = true
		}
	}
	return s
}

// readinessOf returns the readiness for a dependency, creating it if needed.
func (s *startup) readinessOf(name string) *readiness {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.ready[name]
	if !ok {
		r = newReadiness()
		s.ready[name] = r
	}
	return r
}

// decide records a readiness outcome. The first decision stands.
func (s *startup) decide(name string, err error) {
	r := s.readinessOf(name)
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-r.done:
		return
	default:
	}
	r.err = err
	close(r.done)
}

// settle marks a gated process as decided: started, or blocked with a reason.
func (s *startup) settle(name, blockedBecause string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.pending[name] {
		return
	}
	delete(s.pending, name)
	if blockedBecause != "" {
		s.blocked[name] = blockedBecause
	}
	close(s.gated[name])
}

// started records that a process was started outside its gate, by Restart:
// it is no longer pending or blocked.
func (s *startup) started(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.blocked, name)
	if s.pending[name] {
		delete(s.pending, name)
		close(s.gated[name])
	}
}

// status returns a startup status for the process and the reason it is
// blocked, or "" when startup has nothing to say about it.
func (s *startup) status(name string) (status, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pending[name] {
		return StatusPending, ""
	}
	if why, ok := s.blocked[name]; ok {
		return StatusBlocked, why
	}
	return "", ""
}

// healthcheckState reports whether a process's healthcheck is still running,
// and why it failed if it did.
func (s *startup) healthcheckState(name string) (undecided bool, failure error) {
	s.mu.Lock()
	r, ok := s.ready[name]
	checked := s.checked[name]
	s.mu.Unlock()
	if !ok || !checked {
		return false, nil
	}
	select {
	case <-r.done:
		return false, r.err
	default:
		return true, nil
	}
}

// --- Manager side ---

// note reports a startup state change: on the terminal in headless mode
// (termout is silent under the TUI), and in the logs as Running Man's own
// output, where agents and the TUI can read it.
func (m *Manager) note(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	termout.Printf("[running-man] %s\n", msg)
	if m.handler != nil {
		m.handler("running-man", msg, time.Now(), false)
	}
}

// SetServiceReadiness supplies the readiness check for Compose services that
// processes depend on. It must be called before Start.
func (m *Manager) SetServiceReadiness(fn ServiceReadiness) {
	m.serviceReady = fn
}

// feedHandler returns the line handler for a run of name: the manager's own,
// with the process's log healthcheck fed first when it has one. Fed from the
// wrapper, so the check sees the very first line of the run.
func (m *Manager) feedHandler(name string) LineHandler {
	matcher := m.startup.matchers[name]
	if matcher == nil {
		return m.handler
	}
	return func(source, line string, ts time.Time, isStderr bool) {
		matcher.Feed(line)
		if m.handler != nil {
			m.handler(source, line, ts, isStderr)
		}
	}
}

// checkHealth launches a process's healthcheck after its first run starts.
// A process without one is ready as soon as it starts.
func (m *Manager) checkHealth(name string, cfg ProcessConfig) {
	s := m.startup
	s.mu.Lock()
	if s.checked[name] {
		s.mu.Unlock()
		return
	}
	s.checked[name] = true
	s.mu.Unlock()

	hc := cfg.Healthcheck
	if hc == nil {
		s.decide(name, nil)
		return
	}
	// Created now, not lazily by the first dependent to ask: until then a
	// status query would find no check and report running, not starting.
	s.readinessOf(name)
	go func() {
		timeout := hc.Timeout
		if timeout <= 0 {
			timeout = 60 * time.Second
		}
		ctx, cancel := context.WithTimeout(m.ctx, timeout)
		defer cancel()

		var err error
		switch {
		case hc.Port != 0:
			err = health.Port(ctx, hc.Port)
		case hc.HTTP != "":
			err = health.HTTP(ctx, hc.HTTP)
		default:
			err = s.matchers[name].Wait(ctx)
		}
		if err != nil && m.ctx.Err() == nil {
			err = fmt.Errorf("%s was not ready: its healthcheck (%s) did not pass within %s",
				name, hc, timeout)
		}
		if err == nil {
			m.note("%s is ready (%s)", name, hc)
		}
		s.decide(name, err)
	}()
}

// awaitService runs the Compose readiness check for a service, once.
func (m *Manager) awaitService(name string) {
	s := m.startup
	s.mu.Lock()
	if s.checked[name] {
		s.mu.Unlock()
		return
	}
	s.checked[name] = true
	s.mu.Unlock()

	go func() {
		var err error
		if m.serviceReady == nil {
			err = fmt.Errorf("cannot check Compose service %s: no Compose stack is configured", name)
		} else if err = m.serviceReady(m.ctx, name); err != nil && m.ctx.Err() == nil {
			err = fmt.Errorf("the Compose service %s was not ready: %w", name, err)
		}
		if err == nil {
			m.note("Compose service %s is ready", name)
		}
		s.decide(name, err)
	}()
}

// awaitDependencies blocks until every dependency of name is ready. It
// returns nil when they all are, or the reason startup cannot continue.
func (m *Manager) awaitDependencies(name string, cfg ProcessConfig) error {
	for _, dep := range cfg.DependsOn {
		if _, isProc := m.configs[dep]; !isProc {
			m.awaitService(dep)
		}
		r := m.startup.readinessOf(dep)
		select {
		case <-r.done:
			if r.err != nil {
				return r.err
			}
		case <-m.startup.halted:
			return m.startup.haltErr
		case <-m.ctx.Done():
			return m.ctx.Err()
		}
	}
	return nil
}

// halt stops startup: nothing waiting is started, and each is marked blocked
// with the reason. What is already running keeps running -- its output is
// usually the explanation.
func (m *Manager) halt(cause error) {
	s := m.startup
	s.haltOnce.Do(func() {
		s.haltErr = cause
		close(s.halted)

		msg := fmt.Sprintf("Startup failed and went no further: %v", cause)
		termout.Errorf("[running-man] %s\n", msg)
		if m.handler != nil {
			m.handler("running-man", msg, time.Now(), true)
		}

		s.mu.Lock()
		waiting := make([]string, 0, len(s.pending))
		for _, n := range m.order {
			if s.pending[n] {
				waiting = append(waiting, n)
			}
		}
		s.mu.Unlock()
		for _, n := range waiting {
			s.settle(n, cause.Error())
		}
	})
}

// gate waits for a process's dependencies, then starts it, or blocks it if
// startup stops. Runs in its own goroutine from Start.
func (m *Manager) gate(name string, cfg ProcessConfig) {
	m.note("%s is waiting for %s", name, strings.Join(cfg.DependsOn, ", "))
	if err := m.awaitDependencies(name, cfg); err != nil {
		if m.ctx.Err() != nil {
			return
		}
		m.halt(err)
		return
	}

	// Restart can have started it already.
	m.mu.RLock()
	_, running := m.processes[name]
	m.mu.RUnlock()
	if running {
		m.startup.settle(name, "")
		return
	}

	m.note("%s: dependencies ready, starting", name)
	w := m.newWrapper(name, cfg)
	if err := w.Start(); err != nil {
		err = fmt.Errorf("%s failed to start: %w", name, err)
		m.startup.settle(name, err.Error())
		m.startup.decide(name, err)
		m.halt(err)
		return
	}
	if !m.adopt(name, w) {
		discard(w)
		return
	}
	m.startup.settle(name, "")
	m.checkHealth(name, cfg)
}

// awaitStarted blocks a waiter until a gated process has started or been
// blocked. It returns the reason when blocked, and an error when the manager
// stops first.
func (m *Manager) awaitStarted(name string) (blocked error) {
	ch, gated := m.startup.gated[name]
	if !gated {
		return nil
	}
	select {
	case <-ch:
	case <-m.ctx.Done():
		return m.ctx.Err()
	}
	if status, why := m.startup.status(name); status == StatusBlocked {
		return errors.New(why)
	}
	return nil
}

// applyStartup overlays the startup state on a process's reported status.
func (m *Manager) applyStartup(info *ProcessInfo) {
	if status, why := m.startup.status(info.Name); status != "" {
		info.Status = status
		info.StartupError = why
		if status == StatusPending || status == StatusBlocked {
			info.PID = -1
			info.ExitCode = -1
		}
		return
	}
	undecided, failure := m.startup.healthcheckState(info.Name)
	switch {
	case undecided && info.Status == "running":
		info.Status = StatusStarting
	case failure != nil:
		info.StartupError = failure.Error()
	}
}

// DependencyInfo is a Compose service that a process depends on, as reported
// alongside the processes. Processes report their own state; services are not
// processes, so they are listed here.
type DependencyInfo struct {
	Name string `json:"name"`
	// State is pending (not checked yet), starting (being checked), ready or
	// failed.
	State string `json:"state"`
	// Detail says why it failed.
	Detail string `json:"detail,omitempty"`
	// Sources are the log sources of the service's containers.
	Sources []string `json:"sources,omitempty"`
}

// SetServiceSources supplies the log sources belonging to a Compose service,
// so its output can be shown next to its state.
func (m *Manager) SetServiceSources(fn func(service string) []string) {
	m.serviceSources = fn
}

// Dependencies reports each Compose service a process depends on, in the
// order first named.
func (m *Manager) Dependencies() []DependencyInfo {
	var names []string
	seen := map[string]bool{}
	for _, p := range m.order {
		for _, dep := range m.configs[p].DependsOn {
			if _, isProc := m.configs[dep]; !isProc && !seen[dep] {
				seen[dep] = true
				names = append(names, dep)
			}
		}
	}

	deps := make([]DependencyInfo, 0, len(names))
	for _, name := range names {
		d := DependencyInfo{Name: name, State: StatusPending}
		s := m.startup
		s.mu.Lock()
		r, hasReadiness := s.ready[name]
		checked := s.checked[name]
		s.mu.Unlock()
		if checked && hasReadiness {
			select {
			case <-r.done:
				if r.err != nil {
					d.State, d.Detail = "failed", r.err.Error()
				} else {
					d.State = "ready"
				}
			default:
				d.State = StatusStarting
			}
		}
		if m.serviceSources != nil {
			d.Sources = m.serviceSources(name)
		}
		deps = append(deps, d)
	}
	return deps
}
