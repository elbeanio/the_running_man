package docker

import (
	"sync"
	"testing"
	"time"
)

// fakeStream records what the watcher did to it.
type fakeStream struct {
	mu      sync.Mutex
	from    time.Time
	stopped bool
}

func (f *fakeStream) Stop() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped = true
	return nil
}

func (f *fakeStream) Wait() error { return nil }

func (f *fakeStream) wasStopped() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stopped
}

// fakeDaemon stands in for the two daemon calls the watcher makes.
type fakeDaemon struct {
	mu        sync.Mutex
	startedAt map[string]time.Time
	streams   []*fakeStream
}

func (d *fakeDaemon) setStarted(id string, at time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.startedAt[id] = at
}

func newTestWatcher() (*Watcher, *fakeDaemon) {
	d := &fakeDaemon{startedAt: map[string]time.Time{}}
	w := NewWatcher(nil, "proj", []string{"api"}, time.Hour, nil, nil)
	w.startedAt = func(id string) (time.Time, error) {
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.startedAt[id], nil
	}
	w.open = func(c Container, from time.Time) (stream, error) {
		d.mu.Lock()
		defer d.mu.Unlock()
		s := &fakeStream{from: from}
		d.streams = append(d.streams, s)
		return s, nil
	}
	return w, d
}

var api = Container{ID: "c0ffee", Name: "proj-api-1", ServiceName: "api"}

// Startup discovery and the event feed can both report the same running
// container; it must be streamed once, or every line arrives twice.
func TestWatcher_SameRunAttachedOnce(t *testing.T) {
	w, d := newTestWatcher()
	d.setStarted(api.ID, time.Unix(1000, 0))

	for range 3 {
		if err := w.Attach(api); err != nil {
			t.Fatal(err)
		}
	}
	if len(d.streams) != 1 {
		t.Errorf("one run of one container opened %d streams, want 1", len(d.streams))
	}
}

// The bug: a container's log stream ends when it stops, and nothing reattached
// when it started again, so everything it wrote after a restart was lost.
// A restart keeps the container ID, so the new run is told apart by its start
// time, and streamed from that time -- replaying the retention window instead
// would capture the previous run's lines a second time.
func TestWatcher_RestartIsReattachedFromItsStart(t *testing.T) {
	w, d := newTestWatcher()
	first := time.Now().Add(-10 * time.Minute)
	d.setStarted(api.ID, first)
	if err := w.Attach(api); err != nil {
		t.Fatal(err)
	}

	second := time.Now().Add(-time.Second)
	d.setStarted(api.ID, second)
	if err := w.Attach(api); err != nil {
		t.Fatal(err)
	}

	if len(d.streams) != 2 {
		t.Fatalf("a restarted container opened %d streams in total, want 2", len(d.streams))
	}
	if !d.streams[0].from.IsZero() {
		t.Errorf("first attach replayed from %v; it should use the retention window", d.streams[0].from)
	}
	if !d.streams[1].from.Equal(second) {
		t.Errorf("reattach replayed from %v, want the new run's start %v", d.streams[1].from, second)
	}
	if !d.streams[0].wasStopped() {
		t.Error("the previous run's stream was not stopped")
	}
}

// A run that started longer ago than the retention window is replayed only
// as far back as retention would keep.
func TestWatcher_ReattachReplayIsBoundedByRetention(t *testing.T) {
	w, d := newTestWatcher()
	d.setStarted(api.ID, time.Now().Add(-48*time.Hour))
	_ = w.Attach(api)
	d.setStarted(api.ID, time.Now().Add(-24*time.Hour))
	_ = w.Attach(api)

	if from := d.streams[1].from; time.Since(from) > time.Hour+time.Minute {
		t.Errorf("replayed from %v ago; retention is an hour", time.Since(from).Round(time.Minute))
	}
}

// Events keep arriving while Running Man shuts down. A stream opened after
// Stop would never be stopped.
func TestWatcher_NothingAttachedAfterStop(t *testing.T) {
	w, d := newTestWatcher()
	d.setStarted(api.ID, time.Unix(1000, 0))
	w.Stop()

	if err := w.Attach(api); err == nil {
		t.Error("Attach after Stop reported success")
	}
	if len(d.streams) != 0 {
		t.Errorf("Attach after Stop opened %d streams", len(d.streams))
	}
}

// Stop stops every stream the watcher opened.
func TestWatcher_StopStopsStreams(t *testing.T) {
	w, d := newTestWatcher()
	d.setStarted("a", time.Unix(1000, 0))
	d.setStarted("b", time.Unix(1000, 0))
	_ = w.Attach(Container{ID: "a", ServiceName: "api"})
	_ = w.Attach(Container{ID: "b", ServiceName: "api"})

	w.Stop()
	for i, s := range d.streams {
		if !s.wasStopped() {
			t.Errorf("stream %d not stopped", i)
		}
	}
}

// Only the configured services are followed: a start event for a service
// the active profiles leave out is not this instance's business.
func TestWatcher_IgnoresOtherServices(t *testing.T) {
	w, d := newTestWatcher()
	d.setStarted("x", time.Unix(1000, 0))
	w.onEvent(ContainerEvent{Type: "start", ContainerID: "x", ServiceName: "worker"})
	w.onEvent(ContainerEvent{Type: "die", ContainerID: "x", ServiceName: "api"})
	if len(d.streams) != 0 {
		t.Errorf("opened %d streams for events that should be ignored", len(d.streams))
	}

	w.onEvent(ContainerEvent{Type: "start", ContainerID: "x", Name: "proj-api-1", ServiceName: "api"})
	if len(d.streams) != 1 {
		t.Errorf("a start event for a watched service opened %d streams, want 1", len(d.streams))
	}
}
