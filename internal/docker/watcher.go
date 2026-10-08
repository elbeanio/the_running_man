package docker

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/elbeanio/the_running_man/internal/termout"
)

// stream is what the watcher needs from a log stream: *ContainerStreamer, or a
// fake in tests.
type stream interface {
	Stop() error
	Wait() error
}

// Watcher keeps a log stream attached to every running container of the
// watched Compose services, including containers that start after Running Man
// attached.
//
// Without it, streams were attached once, at startup. A container's log stream
// ends when the container stops, and nothing attached a new one when it
// started again -- so a restart, a crash with a restart policy, or a
// `docker compose up` that recreated a container silently ended capture for
// it. Reproduced against a real container: after `docker compose restart`,
// Docker logged the new run and Running Man held none of it. A service that was
// not running at startup was never picked up either.
type Watcher struct {
	client   *Client
	project  string
	services map[string]bool
	history  time.Duration
	handler  LineHandler
	onEnd    StreamEndHandler

	// The two daemon calls, as fields so tests can stand in for them.
	startedAt func(id string) (time.Time, error)
	open      func(c Container, from time.Time) (stream, error)

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu       sync.Mutex
	attached map[string]attachment // by container ID
}

// attachment is the stream for one run of one container.
type attachment struct {
	stream    stream
	startedAt time.Time
	container Container
}

// errStopped is returned by Attach once the watcher has been stopped.
var errStopped = errors.New("the container watcher is stopped")

// NewWatcher creates a watcher for the given Compose project and services.
// history is the replay window for a container seen for the first time; see
// NewContainerStreamer.
func NewWatcher(client *Client, project string, services []string, history time.Duration, handler LineHandler, onEnd StreamEndHandler) *Watcher {
	ctx, cancel := context.WithCancel(context.Background())
	w := &Watcher{
		client:   client,
		project:  project,
		services: make(map[string]bool, len(services)),
		history:  history,
		handler:  handler,
		onEnd:    onEnd,
		ctx:      ctx,
		cancel:   cancel,
		attached: map[string]attachment{},
	}
	for _, s := range services {
		w.services[s] = true
	}
	w.startedAt = func(id string) (time.Time, error) {
		return w.client.containerStartedAt(w.ctx, id)
	}
	w.open = func(c Container, from time.Time) (stream, error) {
		s := NewContainerStreamer(w.client, c.ID, c.Name, w.handler, w.history)
		s.OnStreamEnd(w.onEnd)
		if !from.IsZero() {
			s.ReplayFrom(from)
		}
		if err := s.Start(); err != nil {
			return nil, err
		}
		return s, nil
	}
	return w
}

// Attach streams the container's logs, unless its current run is already
// being streamed.
//
// A container seen before is a restart -- the ID survives one -- so it is
// streamed from the new run's start, bounded by the retention window. The
// history window would replay the previous run, which was captured already.
func (w *Watcher) Attach(c Container) error {
	startedAt, err := w.startedAt(c.ID)
	if err != nil {
		return err
	}

	// Held across opening the stream, so two reports of the same run (startup
	// discovery and a start event) cannot both open one, and so Stop cannot
	// sweep between the check and the registration.
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.ctx.Err() != nil {
		return errStopped
	}
	prev, seen := w.attached[c.ID]
	if seen && prev.startedAt.Equal(startedAt) {
		return nil
	}

	var from time.Time
	if seen {
		from = startedAt
		if floor := time.Now().Add(-w.history); from.Before(floor) {
			from = floor
		}
		// Normally over already: the stream ends when the container stops.
		_ = prev.stream.Stop()
	}

	s, err := w.open(c, from)
	if err != nil {
		return err
	}
	w.attached[c.ID] = attachment{stream: s, startedAt: startedAt, container: c}
	return nil
}

// onEvent attaches a watched service's container when it starts.
func (w *Watcher) onEvent(e ContainerEvent) {
	if e.Type != "start" || !w.services[e.ServiceName] {
		return
	}
	c := Container{ID: e.ContainerID, Name: e.Name, ServiceName: e.ServiceName, ProjectName: w.project}
	if err := w.Attach(c); err != nil && !errors.Is(err, errStopped) {
		termout.Errorf("[running-man] Failed to attach to %s after it started: %v\n", e.Name, err)
	}
}

// Watch follows the project's container events until Stop, attaching each
// watched container that starts.
//
// If the event stream drops -- the daemon restarting, say -- it reconnects,
// then runs discover and attaches whatever is running, since starts during
// the gap were missed.
func (w *Watcher) Watch(discover func() ([]Container, error)) {
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		for {
			err := w.client.WatchEvents(w.ctx, w.project, w.onEvent)
			if w.ctx.Err() != nil {
				return
			}
			termout.Errorf("[running-man] Lost the Docker event stream (%v); reconnecting\n", err)
			select {
			case <-time.After(2 * time.Second):
			case <-w.ctx.Done():
				return
			}
			containers, err := discover()
			if err != nil {
				termout.Errorf("[running-man] Failed to rediscover containers: %v\n", err)
				continue
			}
			for _, c := range containers {
				if err := w.Attach(c); err != nil && !errors.Is(err, errStopped) {
					termout.Errorf("[running-man] Failed to attach to %s: %v\n", c.Name, err)
				}
			}
		}
	}()
}

// SourcesFor returns the log sources of the service's attached containers, in
// name order.
func (w *Watcher) SourcesFor(service string) []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	var names []string
	for _, a := range w.attached {
		if a.container.ServiceName == service {
			names = append(names, a.container.Name)
		}
	}
	slices.Sort(names)
	return names
}

// Stop stops watching and stops every stream. Nothing is attached after it.
func (w *Watcher) Stop() {
	w.cancel()
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, a := range w.attached {
		_ = a.stream.Stop()
	}
}

// Wait waits for the event loop and every stream to finish.
func (w *Watcher) Wait() {
	w.wg.Wait()
	w.mu.Lock()
	streams := make([]stream, 0, len(w.attached))
	for _, a := range w.attached {
		streams = append(streams, a.stream)
	}
	w.mu.Unlock()
	for _, s := range streams {
		_ = s.Wait()
	}
}
