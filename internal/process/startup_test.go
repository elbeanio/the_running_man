package process

import (
	"context"
	"errors"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// lineLog records every line the manager reports, for asserting on messages.
type lineLog struct {
	mu    sync.Mutex
	lines []string
}

func (l *lineLog) handle(source, line string, _ time.Time, _ bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, source+": "+line)
}

func (l *lineLog) contains(text string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, line := range l.lines {
		if strings.Contains(line, text) {
			return true
		}
	}
	return false
}

func statusOf(t *testing.T, m *Manager, name string) ProcessInfo {
	t.Helper()
	info, err := m.GetProcess(name)
	if err != nil {
		t.Fatalf("GetProcess(%s): %v", name, err)
	}
	return *info
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	if !waitFor(5*time.Second, cond) {
		t.Fatalf("timed out waiting for: %s", what)
	}
}

func startManager(t *testing.T, configs []ProcessConfig, handler LineHandler) *Manager {
	t.Helper()
	m := NewManager(configs, handler)
	if err := m.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = m.Stop() })
	return m
}

// The feature: a dependent does not start until its dependency's healthcheck
// passes. Until then it is pending, and the dependency is starting.
func TestStartup_DependentWaitsForHealthcheck(t *testing.T) {
	t.Parallel()
	m := startManager(t, []ProcessConfig{
		{Name: "backend", Command: "sleep 0.1; echo ready; sleep 30",
			Healthcheck: &Healthcheck{Log: "ready", Timeout: 5 * time.Second}},
		{Name: "frontend", Command: "sleep 30", DependsOn: []string{"backend"}},
	}, nil)

	if got := statusOf(t, m, "frontend").Status; got != StatusPending {
		t.Errorf("frontend before backend is ready: %s, want pending", got)
	}
	if got := statusOf(t, m, "backend").Status; got != StatusStarting {
		t.Errorf("backend before its healthcheck passes: %s, want starting", got)
	}

	eventually(t, "frontend running", func() bool { return statusOf(t, m, "frontend").Status == "running" })
	if got := statusOf(t, m, "backend").Status; got != "running" {
		t.Errorf("backend after its healthcheck passed: %s, want running", got)
	}
}

// A fast process prints its ready line at once. The log check is fed from the
// wrapper, so the line cannot arrive before anyone is looking.
func TestStartup_LogCheckSeesTheFirstLine(t *testing.T) {
	t.Parallel()
	m := startManager(t, []ProcessConfig{
		{Name: "backend", Command: "echo ready; sleep 30",
			Healthcheck: &Healthcheck{Log: "ready", Timeout: 2 * time.Second}},
		{Name: "frontend", Command: "sleep 30", DependsOn: []string{"backend"}},
	}, nil)
	eventually(t, "frontend running", func() bool { return statusOf(t, m, "frontend").Status == "running" })
}

// The hard stop: a dependency that never becomes ready stops startup. Nothing
// waiting is started, each says why, and the failure reaches the logs as an
// error. What is running keeps running.
func TestStartup_TimeoutIsAHardStop(t *testing.T) {
	t.Parallel()
	log := &lineLog{}
	m := startManager(t, []ProcessConfig{
		{Name: "db", Command: "sleep 30",
			Healthcheck: &Healthcheck{Log: "never printed", Timeout: 200 * time.Millisecond}},
		{Name: "backend", Command: "sleep 30", DependsOn: []string{"db"},
			Healthcheck: &Healthcheck{Port: 1, Timeout: time.Second}},
		{Name: "frontend", Command: "sleep 30", DependsOn: []string{"backend"}},
		{Name: "unrelated", Command: "sleep 30"},
	}, log.handle)

	eventually(t, "backend blocked", func() bool { return statusOf(t, m, "backend").Status == StatusBlocked })
	eventually(t, "frontend blocked", func() bool { return statusOf(t, m, "frontend").Status == StatusBlocked })

	for _, name := range []string{"backend", "frontend"} {
		info := statusOf(t, m, name)
		if !strings.Contains(info.StartupError, "db was not ready") {
			t.Errorf("%s startup_error = %q, want the reason", name, info.StartupError)
		}
		if info.PID != -1 {
			t.Errorf("%s was started (pid %d)", name, info.PID)
		}
	}
	if !strings.Contains(statusOf(t, m, "db").StartupError, `log "never printed"`) {
		t.Errorf("db startup_error = %q, want its healthcheck named", statusOf(t, m, "db").StartupError)
	}
	if got := statusOf(t, m, "unrelated").Status; got != "running" {
		t.Errorf("an unrelated process was affected: %s", got)
	}
	if !log.contains("Startup failed") {
		t.Error("the hard stop was not reported in the logs")
	}
	if code, ok := m.ExitCodes()["frontend"]; !ok || code == 0 {
		t.Errorf("a blocked process must count as failed; exit codes %v", m.ExitCodes())
	}
}

// Wait must not hang on a process that will never start.
func TestStartup_WaitReturnsForABlockedProcess(t *testing.T) {
	t.Parallel()
	m := startManager(t, []ProcessConfig{
		{Name: "db", Command: "true",
			Healthcheck: &Healthcheck{Log: "never", Timeout: 100 * time.Millisecond}},
		{Name: "app", Command: "true", DependsOn: []string{"db"}},
	}, nil)

	returned, err := waitAsync(m, 5*time.Second)
	if !returned {
		t.Fatal("Wait did not return with a blocked process")
	}
	if err == nil || !strings.Contains(err.Error(), "app did not start") {
		t.Errorf("Wait error = %v, want the blocked process reported", err)
	}
}

// Compose services are checked by the caller's readiness function.
func TestStartup_ComposeServiceDependency(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		result error
		want   string
	}{
		{"ready", nil, "running"},
		{"never ready", errors.New("not healthy within 60s"), StatusBlocked},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewManager([]ProcessConfig{
				{Name: "app", Command: "sleep 30", DependsOn: []string{"db"}},
			}, nil)
			var asked []string
			var mu sync.Mutex
			m.SetServiceReadiness(func(ctx context.Context, service string) error {
				mu.Lock()
				asked = append(asked, service)
				mu.Unlock()
				time.Sleep(50 * time.Millisecond)
				return tc.result
			})
			if err := m.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = m.Stop() }()

			eventually(t, "app "+tc.want, func() bool { return statusOf(t, m, "app").Status == tc.want })
			mu.Lock()
			defer mu.Unlock()
			if len(asked) != 1 || asked[0] != "db" {
				t.Errorf("readiness asked about %v, want [db] once", asked)
			}
		})
	}
}

// A dependency shared by two processes is checked once, not per dependent.
func TestStartup_SharedDependencyCheckedOnce(t *testing.T) {
	t.Parallel()
	var calls int
	var mu sync.Mutex
	m := NewManager([]ProcessConfig{
		{Name: "a", Command: "sleep 30", DependsOn: []string{"db"}},
		{Name: "b", Command: "sleep 30", DependsOn: []string{"db"}},
	}, nil)
	m.SetServiceReadiness(func(context.Context, string) error {
		mu.Lock()
		calls++
		mu.Unlock()
		return nil
	})
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m.Stop() }()

	eventually(t, "both running", func() bool {
		return statusOf(t, m, "a").Status == "running" && statusOf(t, m, "b").Status == "running"
	})
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Errorf("shared dependency checked %d times, want 1", calls)
	}
}

// Stopping while a process waits must leave it unstarted.
func TestStartup_StopWhilePendingStartsNothing(t *testing.T) {
	t.Parallel()
	r := &pidReporter{}
	m := NewManager([]ProcessConfig{
		{Name: "db", Command: "sleep 0.1; echo ready; sleep 30",
			Healthcheck: &Healthcheck{Log: "ready", Timeout: 5 * time.Second}},
		{Name: "app", Command: "echo pid=$$; exec sleep 30", DependsOn: []string{"db"}},
	}, r.handle)
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	_ = m.Stop()

	time.Sleep(300 * time.Millisecond)
	if pid := r.get(); pid != 0 {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Errorf("app started after Stop (pid %d)", pid)
	}
}

// Restart of a waiting process starts it at once, and the gate does not start
// it a second time when the dependency becomes ready.
func TestStartup_RestartOfPendingProcessStartsItOnce(t *testing.T) {
	t.Parallel()
	c := &lineCounter{marker: "app-started"}
	m := startManager(t, []ProcessConfig{
		{Name: "db", Command: "sleep 0.1; echo ready; sleep 30",
			Healthcheck: &Healthcheck{Log: "ready", Timeout: 5 * time.Second}},
		{Name: "app", Command: "echo app-started; sleep 30", DependsOn: []string{"db"}},
	}, c.handle)

	if err := m.Restart("app"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "db ready", func() bool { return statusOf(t, m, "db").Status == "running" })
	time.Sleep(200 * time.Millisecond)
	if n := c.count(); n != 1 {
		t.Errorf("app started %d times, want 1", n)
	}
}

// A client drawing the startup screen needs each process's dependencies and
// healthcheck, to tell ready from merely running, and the Compose services
// depended on, which are not processes.
func TestStartup_ReportsDependenciesAndHealthchecks(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	m := NewManager([]ProcessConfig{
		{Name: "backend", Command: "sleep 30", DependsOn: []string{"db"},
			Healthcheck: &Healthcheck{Port: 1, Timeout: 5 * time.Second}},
		{Name: "frontend", Command: "sleep 30"},
	}, nil)
	m.SetServiceReadiness(func(ctx context.Context, service string) error {
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	m.SetServiceSources(func(service string) []string { return []string{"proj-" + service + "-1"} })
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m.Stop() }()

	backend := statusOf(t, m, "backend")
	if len(backend.DependsOn) != 1 || backend.DependsOn[0] != "db" || backend.Healthcheck != "port 1" {
		t.Errorf("backend = depends_on %v, healthcheck %q", backend.DependsOn, backend.Healthcheck)
	}
	if fe := statusOf(t, m, "frontend"); len(fe.DependsOn) != 0 || fe.Healthcheck != "" {
		t.Errorf("frontend = depends_on %v, healthcheck %q", fe.DependsOn, fe.Healthcheck)
	}

	eventually(t, "db being checked", func() bool {
		deps := m.Dependencies()
		return len(deps) == 1 && deps[0].State == DependencyChecking
	})
	d := m.Dependencies()[0]
	if d.Name != "db" || len(d.Sources) != 1 || d.Sources[0] != "proj-db-1" {
		t.Errorf("dependency = %+v", d)
	}

	close(release)
	eventually(t, "db ready", func() bool { return m.Dependencies()[0].State == "ready" })
}

func TestStartup_FailedDependencyIsReported(t *testing.T) {
	t.Parallel()
	m := startManagerWithService(t, errors.New("its Compose healthcheck did not pass within 1s"))
	eventually(t, "db failed", func() bool {
		deps := m.Dependencies()
		return len(deps) == 1 && deps[0].State == "failed"
	})
	if d := m.Dependencies()[0]; !strings.Contains(d.Detail, "did not pass within 1s") {
		t.Errorf("detail = %q", d.Detail)
	}
}

func startManagerWithService(t *testing.T, result error) *Manager {
	t.Helper()
	m := NewManager([]ProcessConfig{{Name: "app", Command: "sleep 30", DependsOn: []string{"db"}}}, nil)
	m.SetServiceReadiness(func(context.Context, string) error { return result })
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Stop() })
	return m
}
