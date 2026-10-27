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
type Runtime interface {
	// Endpoint returns the Unix-domain socket URL the runtime talks to.
	Endpoint() string

	// List returns every container the runtime knows about, regardless of
	// state.
	List(ctx context.Context) ([]Container, error)

	// Inspect returns a single container as the runtime sees it today.
	Inspect(ctx context.Context, id string) (Container, error)

	// Wait blocks until the named container reaches the desired condition
	// (typically "not-running").
	Wait(ctx context.Context, id string, condition string) error

	// Logs opens a live log stream for the named container. The caller owns
	// the returned ReadCloser and must close it.
	Logs(ctx context.Context, id string, opts LogOpts) (io.ReadCloser, error)

	// Events streams the runtime's container-event feed to dst until ctx
	// is cancelled or the runtime closes the stream.
	Events(ctx context.Context, opts EventOpts, dst chan<- Event) error

	// FindCommand returns the runtime's preferred compose CLI as a
	// resolved command struct (Name, Base).
	FindCommand(ctx context.Context) (ComposeCommand, error)
}

// Container is the runtime-agnostic view of a single container. The field
// shape retains the docker-side Compose-aware names (ServiceName,
// ProjectName) so Compose discovery and healthcheck coordination continue
// to work as the docker package did before this refactor; runtime-agnostic
// fields (State, Exit, Created) sit alongside.
type Container struct {
	ID          string
	Name        string
	ServiceName string
	ProjectName string
	Image       string
	State       string
	Exit        int
	Created     int64
}

// LogOpts parameterises a Logs call.
type LogOpts struct {
	Follow bool
	Since  string
	Until  string
	Tail   int
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
