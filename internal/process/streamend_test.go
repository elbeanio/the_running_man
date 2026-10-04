package process

import (
	"slices"
	"sync"
	"testing"
	"time"
)

// streamEvents records lines and stream ends in the order they happen.
type streamEvents struct {
	mu     sync.Mutex
	events []string
}

func (r *streamEvents) line(_ string, line string, _ time.Time, isStderr bool) {
	r.add(streamName(isStderr) + ":" + line)
}

func (r *streamEvents) end(_ string, isStderr bool) {
	r.add(streamName(isStderr) + ":END")
}

func (r *streamEvents) add(e string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *streamEvents) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.events)
}

func streamName(isStderr bool) string {
	if isStderr {
		return "stderr"
	}
	return "stdout"
}

// The parser holds a Python traceback open until a later line closes it. With
// nothing telling it a stream had ended, a traceback that was the last thing a
// process wrote -- one whose exception line the parser does not recognise, or
// one cut short by the process dying -- sat in the parser and never reached the
// buffer. Each stream's end is reported after its last line, and before Wait
// returns, so the flush lands ahead of anything reported about the exit.
func TestWrapper_ReportsStreamEndAfterTheLastLine(t *testing.T) {
	r := &streamEvents{}
	w := New("p", "echo out; echo err >&2", nil, "", r.line)
	w.OnStreamEnd(r.end)

	if err := w.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	_ = w.Wait()

	got := r.snapshot()
	for _, stream := range []string{"stdout", "stderr"} {
		last := slices.Index(got, stream+":"+map[string]string{"stdout": "out", "stderr": "err"}[stream])
		end := slices.Index(got, stream+":END")
		if end < 0 {
			t.Fatalf("%s end not reported before Wait returned: %v", stream, got)
		}
		if last < 0 || last > end {
			t.Errorf("%s end reported before its last line: %v", stream, got)
		}
	}
}

// Every wrapper the manager creates must carry the hook, including the ones it
// creates later: a restart after a crash is exactly when a traceback is likely
// to be the last output.
func TestManager_ReportsStreamEndForRestartedProcesses(t *testing.T) {
	r := &streamEvents{}
	m := NewManager([]ProcessConfig{{
		Name:           "crashy",
		Command:        "echo boom >&2; exit 1",
		RestartOnCrash: true,
	}}, r.line)
	m.OnStreamEnd(r.end)

	if err := m.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	// Restarts happen inside Wait.
	done := make(chan error, 1)
	go func() { done <- m.Wait() }()
	defer func() {
		_ = m.Stop()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("Wait did not return after Stop")
		}
	}()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ends := 0
		for _, e := range r.snapshot() {
			if e == "stderr:END" {
				ends++
			}
		}
		if ends >= 2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	got := r.snapshot()
	t.Fatalf("expected a stderr end for the first run and the restart; got %d events, first %v",
		len(got), got[:min(len(got), 4)])
}
