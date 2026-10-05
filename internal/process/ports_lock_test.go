package process

import (
	"sync/atomic"
	"testing"
	"time"
)

// startSleepers starts n long-running processes and returns their manager.
func startSleepers(t *testing.T, n int) *Manager {
	t.Helper()
	var configs []ProcessConfig
	for i := range n {
		configs = append(configs, ProcessConfig{Name: string(rune('a' + i)), Command: "sleep 30"})
	}
	m := NewManager(configs, nil)
	if err := m.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = m.Stop() })
	return m
}

// clearPortCache empties the cache so a lookup has to run the commands.
func clearPortCache() {
	portCacheMu.Lock()
	clear(portCache)
	portCacheMu.Unlock()
}

// /processes looked ports up one process at a time: a snapshot of the whole
// process table and an lsof for each, 2N subprocesses in sequence for N
// processes, fetching the same table N times. One of each is enough.
func TestListProcesses_OneSnapshotAndOneLsof(t *testing.T) {
	m := startSleepers(t, 3)

	var snapshots, lsofs atomic.Int32
	realSnapshot, realLsof := snapshotProcesses, runLsof
	snapshotProcesses = func() ([]procEntry, error) { snapshots.Add(1); return realSnapshot() }
	runLsof = func(args ...string) []byte { lsofs.Add(1); return realLsof(args...) }
	t.Cleanup(func() { snapshotProcesses, runLsof = realSnapshot, realLsof })

	clearPortCache()
	m.ListProcesses()

	if s, l := snapshots.Load(), lsofs.Load(); s != 1 || l != 1 {
		t.Errorf("3 processes took %d process-table snapshots and %d lsof runs, want 1 and 1", s, l)
	}
}

// And the lookup ran with the manager's lock held, so Restart and Stop, which
// need it for writing, waited for every one of those subprocesses.
func TestListProcesses_LockNotHeldDuringLsof(t *testing.T) {
	m := startSleepers(t, 1)

	realLsof := runLsof
	locked := make(chan bool, 1)
	runLsof = func(args ...string) []byte {
		ok := m.mu.TryLock()
		if ok {
			m.mu.Unlock()
		}
		select {
		case locked <- !ok:
		default:
		}
		return realLsof(args...)
	}
	t.Cleanup(func() { runLsof = realLsof })

	clearPortCache()
	m.ListProcesses()

	select {
	case held := <-locked:
		if held {
			t.Error("the manager's lock was held while lsof ran")
		}
	case <-time.After(time.Second):
		t.Fatal("lsof was never run")
	}
}
