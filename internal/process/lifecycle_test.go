package process

import (
	"strings"
	"sync"
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
