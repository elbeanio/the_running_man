// Package runtime defines the Container-Runtime abstraction over Docker and
// Podman.
//
// The Running Man supervisor talks to one runtime per instance. Today Docker
// is the default; podman is opt-in via the `runtime:` configuration field.
// The interface intentionally mirrors what the supervisor actually reads:
// list, inspect, wait, logs, events, and the runtime's preferred compose
// CLI. Internal abstractions (the moby client in docker.go, podman's REST
// adapter in podman.go once it lands) sit alongside as siblings.
package runtime

import (
	"context"
	"fmt"
	"io"
)

// Runtime is the contract every supported container runtime implements.
//
// The interface is intentionally wide: it covers both the low-level calls
// (Endpoint, List, Inspect, Wait, Logs, Events, FindCommand) and the
// compose-aware helpers the supervisor uses (DiscoverContainersInProject,
// WatchEvents, CompletedServices, WaitServiceHealthy, WaitServiceLog).
// Implementations that need to defer one of the compose-aware helpers
// (e.g. podman's WaitServiceHealthy, which has no equivalent wire shape)
// return a documented error rather than crash.
type Runtime interface {
	// Endpoint returns the URL where the runtime exposes its REST API.
	// Docker-side: `docker://<host>`; podman-side: `unix://<path>`.
	Endpoint() string

	// Close releases any underlying resources. Idempotent.
	Close() error

	// Ping verifies the runtime is reachable. Called at supervisor start.
	Ping(ctx context.Context) error

	// List returns every container the runtime knows about, regardless of
	// state.
	List(ctx context.Context) ([]Container, error)

	// Inspect returns a single container as the runtime sees it today.
	Inspect(ctx context.Context, id string) (Container, error)

	// Wait blocks until the named container reaches the desired condition.
	Wait(ctx context.Context, id string, condition string) error

	// Logs opens a live log stream for the named container. Caller owns
	// the returned ReadCloser and must close it.
	Logs(ctx context.Context, id string, opts LogOpts) (io.ReadCloser, error)

	// Events streams the runtime's container-event feed to dst until ctx
	// is cancelled or the runtime closes the stream.
	Events(ctx context.Context, opts EventOpts, dst chan<- Event) error

	// FindCommand returns the runtime's preferred compose CLI as a
	// resolved command struct (Name, Base).
	FindCommand(ctx context.Context) (ComposeCommand, error)

	// DiscoverContainersInProject returns every running container
	// belonging to the named Compose project whose service appears in
	// services. Order matches the order of services; duplicates removed.
	DiscoverContainersInProject(ctx context.Context, project string, services []string) ([]Container, error)

	// WatchEvents streams Compose-aware container events into fn until
	// ctx is cancelled or the runtime closes its event feed. fn is called
	// for start / restart / die / stop / kill / create / destroy.
	WatchEvents(ctx context.Context, project string, fn EventHandler) error

	// CompletedServices reports which services have a container that ran
	// to completion (Stopped with ExitCode 0). One-shot services -- a
	// migration, a seed -- count as "done" not "missing".
	CompletedServices(ctx context.Context, project string) (map[string]bool, error)

	// WaitServiceHealthy passes once every running container of the
	// service reports healthy.
	WaitServiceHealthy(ctx context.Context, project string, service string) error

	// WaitServiceLog passes once every running container of the service
	// has written a line containing text in its current run.
	WaitServiceLog(ctx context.Context, project string, service string, text string) error
}

// Container is the runtime-agnostic view of a single container. The field
// shape retains the docker-side Compose-aware names (ServiceName,
// ProjectName) so Compose discovery and healthcheck coordination continue
// to work as the docker package did before this refactor; runtime-agnostic
// fields (State, Exit, Created, TTY) sit alongside.
//
// TTY is read from the inspect response (only available there, not in
// the list summary) and is used by the streamer to decide whether to
// demux the log frame. Both docker and podman carry a Tty field on the
// inspect response Config block, so the canonical field is supported
// across runtimes.
type Container struct {
	ID          string
	Name        string
	ServiceName string
	ProjectName string
	Image       string
	State       string
	Exit        int
	Created     int64
	TTY         bool
}

// ContainerEvent is one entry from the runtime's container event feed.
//
// Action is the lifecycle verb ("start", "die", "restart", "stop", "kill",
// "create", "destroy"). ContainerID is the short 12-char id used elsewhere
// in the supervisor. Name / ServiceName / Image come from the runtime's
// compose-aware label set; podman and docker both carry them, in slightly
// different places on the wire.
type ContainerEvent struct {
	Type        string // start, stop, die, restart, kill
	ContainerID string
	Name        string
	ServiceName string // the com.docker.compose.service label
	Image       string
}

// EventHandler is called once per ContainerEvent delivered to the runtime
// supervisor's WatchEvents path.
type EventHandler func(event ContainerEvent)

// LogOpts parameterises a Logs call.
type LogOpts struct {
	Follow     bool
	Since      string
	Until      string
	Tail       int
	Timestamps bool
}

// TailString returns Tail as a quoted-string Tail option; both Docker
// and podman use the same wire shape. Zero means "all", which the wire
// expresses as empty string.
func (o LogOpts) TailString() string {
	if o.Tail == 0 {
		return ""
	}
	return fmt.Sprintf("%d", o.Tail)
}

// EventOpts parameterises an Events stream.
type EventOpts struct {
	Since   string
	Until   string
	Filters map[string][]string
}

// Event is one runtime-event-feed entry.
type Event struct {
	Action string
	Actor  string
	Time   int64
}

// ComposeCommand is a resolved compose CLI entry point. It is the canonical
// shape every implementation's FindCommand returns; runtime-specific flags
// belong in ComposeOptions, declared in the per-runtime file.
type ComposeCommand struct {
	// Name is the executable, e.g. "docker" or "podman".
	Name string
	// Base are the leading arguments, e.g. []string{"compose"} for the v2
	// plugin shape, []string{} for the v1 standalone binary.
	Base []string
}
