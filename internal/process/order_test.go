package process

import (
	"fmt"
	"slices"
	"testing"
	"time"
)

// Processes were kept only in a map, so they started, and were listed, in a
// different random order on every run, and the order written in
// running-man.yml was thrown away. That order is the only statement of intent
// there is until depends_on exists: a database listed before the API that uses
// it should start first.
func TestManager_KeepsConfigOrder(t *testing.T) {
	var configs []ProcessConfig
	var want []string
	for i := range 12 {
		name := fmt.Sprintf("p%02d", 12-i) // deliberately not alphabetical
		configs = append(configs, ProcessConfig{Name: name, Command: "sleep 5"})
		want = append(want, name)
	}

	m := NewManager(configs, nil)
	if err := m.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = m.Stop() }()

	if got := m.ProcessNames(); !slices.Equal(got, want) {
		t.Errorf("ProcessNames = %v, want config order %v", got, want)
	}

	infos := m.ListProcesses()
	var listed []string
	for _, info := range infos {
		listed = append(listed, info.Name)
	}
	if !slices.Equal(listed, want) {
		t.Errorf("ListProcesses order = %v, want config order %v", listed, want)
	}

	// Started in order: each start time is no earlier than the one before.
	var prev time.Time
	for _, info := range infos {
		if info.StartTime.Before(prev) {
			t.Errorf("%s started before the process listed ahead of it", info.Name)
		}
		prev = info.StartTime
	}
}

// Stopping iterated the process map, so shutdown order was random. It is now
// the reverse of config order: the last thing started is the first stopped.
func TestManager_StopsInReverseConfigOrder(t *testing.T) {
	m := NewManager([]ProcessConfig{
		{Name: "db", Command: "true"},
		{Name: "api", Command: "true"},
		{Name: "web", Command: "true"},
	}, nil)
	for _, name := range m.order {
		m.processes[name] = m.newWrapper(name, m.configs[name])
	}

	if got, want := m.stopOrder(), []string{"web", "api", "db"}; !slices.Equal(got, want) {
		t.Errorf("stop order = %v, want %v", got, want)
	}
}
