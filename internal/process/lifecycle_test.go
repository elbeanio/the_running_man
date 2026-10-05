package process

import (
	"fmt"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// lineCounter counts lines containing a marker.
type lineCounter struct {
	mu     sync.Mutex
	marker string
	n      int
}

func (c *lineCounter) handle(_ string, line string, _ time.Time, _ bool) {
	if strings.Contains(line, c.marker) {
		c.mu.Lock()
		c.n++
		c.mu.Unlock()
	}
}

func (c *lineCounter) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

// When one process failed to start, Start stopped the others and returned the
// error, but left the context alive -- so a recurring process launched earlier
// in the loop kept firing on its interval, with nothing left to stop it.
func TestManager_FailedStartStopsRecurringProcesses(t *testing.T) {
	c := &lineCounter{marker: "tick"}
	m := NewManager([]ProcessConfig{
		{Name: "cron", Command: "echo tick", Interval: "20ms"},
		{Name: "broken", Command: "true", Shell: "/nonexistent/shell"},
	}, c.handle)

	if err := m.Start(); err == nil {
		_ = m.Stop()
		t.Fatal("Start succeeded with a process whose shell does not exist")
	}

	// Let anything already in flight finish, then check nothing new runs.
	time.Sleep(100 * time.Millisecond)
	before := c.count()
	time.Sleep(200 * time.Millisecond)
	if after := c.count(); after != before {
		t.Errorf("recurring process kept running after Start failed: %d more runs", after-before)
	}
}

// pidReporter captures the pid a test command prints as "pid=<n>".
type pidReporter struct {
	mu  sync.Mutex
	pid int
}

func (r *pidReporter) handle(_ string, line string, _ time.Time, _ bool) {
	if rest, ok := strings.CutPrefix(line, "pid="); ok {
		var pid int
		if _, err := fmt.Sscan(rest, &pid); err == nil {
			r.mu.Lock()
			r.pid = pid
			r.mu.Unlock()
		}
	}
}

func (r *pidReporter) get() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pid
}

// waitFor polls cond until it holds or the timeout passes.
func waitFor(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}

// alive reports whether pid exists. Reaped children are gone; the runs under
// test are always waited for, so a zombie is not mistaken for a survivor.
func alive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// The recurring loop selects between the ticker and cancellation, and when both
// are ready Go picks at random: a tick could start a run after Stop. A run must
// not start once the manager is stopped.
func TestManager_NoRecurringRunAfterStop(t *testing.T) {
	c := &lineCounter{marker: "tick"}
	cfg := ProcessConfig{Name: "cron", Command: "echo tick", Interval: "1h"}
	m := NewManager([]ProcessConfig{cfg}, c.handle)
	_ = m.Stop()

	// What the loop does when the tick wins the select.
	m.runRecurringProcess("cron", cfg)

	if n := c.count(); n != 0 {
		t.Errorf("a run started after Stop (%d lines of output)", n)
	}
}

// Restart after Stop started a new process that nothing would ever stop: Stop
// had already swept the process table.
func TestManager_RestartAfterStopStartsNothing(t *testing.T) {
	r := &pidReporter{}
	m := NewManager([]ProcessConfig{{Name: "srv", Command: "echo pid=$$; exec sleep 30"}}, r.handle)
	if err := m.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	_ = m.Stop()

	err := m.Restart("srv")
	if pid := r.get(); waitFor(200*time.Millisecond, func() bool { return r.get() != pid }) {
		newPid := r.get()
		defer func() { _ = syscall.Kill(newPid, syscall.SIGKILL) }()
		if waitFor(3*time.Second, func() bool { return !alive(newPid) }) {
			return // started, but stopped again: acceptable
		}
		t.Fatalf("Restart after Stop left pid %d running (err=%v)", newPid, err)
	}
	if err == nil {
		t.Error("Restart after Stop reported success without starting anything")
	}
}

// The race itself. A recurring run forks, then takes the lock to register.
// Stop cancels, then takes the lock and stops everything registered. A run
// that forked before Stop but registered after its sweep was never stopped,
// and outlived Running Man. Here the lock is held across the fork to force
// that interleaving.
func TestManager_RunRegisteredAfterStopIsStopped(t *testing.T) {
	r := &pidReporter{}
	cfg := ProcessConfig{Name: "cron", Command: "echo pid=$$; exec sleep 30", Interval: "1h"}
	m := NewManager([]ProcessConfig{cfg}, r.handle)

	m.mu.Lock() // Stop is "in progress"
	done := make(chan struct{})
	go func() {
		defer close(done)
		m.runRecurringProcess("cron", cfg)
	}()

	if !waitFor(3*time.Second, func() bool { return r.get() != 0 }) {
		m.mu.Unlock()
		t.Fatal("the run never started")
	}
	pid := r.get()
	defer func() { _ = syscall.Kill(pid, syscall.SIGKILL) }()

	// What Stop does, with the run blocked waiting to register.
	m.cancel()
	_ = m.stopAllLocked()
	m.mu.Unlock()

	if !waitFor(5*time.Second, func() bool { return !alive(pid) }) {
		t.Fatalf("pid %d, registered after Stop's sweep, is still running", pid)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("the run did not finish after its process was stopped")
	}
}
