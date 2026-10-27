// Package runtime defines the Container-Runtime interface and the canonical
// data types shared by every supported runtime implementation.
//
// The Running Man supervisor talks to one runtime per instance. Today Docker
// is the default; podman is opt-in via the `runtime:` configuration field.
// The interface intentionally mirrors only what the supervisor reads: list,
// inspect, wait, logs, events, and the runtime's preferred compose CLI.
// Internal abstractions (the moby client, podman's REST adapter) live in
// sibling files. New runtimes plug in by adding one Go file in this package
// and registering it in `cmd/running-man/main.go`.
package runtime

import (
	"context"
	"io"
)

// Runtime is the contract every supported container runtime implements.
type Runtime interface {
	// Endpoint returns the Unix-domain socket URL the runtime talks to.
	Endpoint() string

	// List returns every container the runtime knows about, regardless of
	// state. The runtime may return all containers running under it, or
	// only those matching an idle-time filter; callers must not assume
	// either.
	List(ctx context.Context) ([]Container, error)

	// Inspect returns a single container as the runtime sees it today.
	// Container state should reflect the current moment, not a cached
	// snapshot.
	Inspect(ctx context.Context, id string) (Container, error)

	// Wait blocks until the named container reaches the desired
	// condition (typically "not-running"). Errors fast on timeout or
	// runtime failure.
	Wait(ctx context.Context, id string, condition string) error

	// Logs opens a live log stream for the named container. The caller
	// owns the returned ReadCloser and must close it.
	Logs(ctx context.Context, id string, opts LogOpts) (io.ReadCloser, error)

	// Events streams the runtime's container-event feed to dst until
	// ctx is cancelled or the runtime closes the stream.
	Events(ctx context.Context, opts EventOpts, dst chan<- Event) error

	// Composer exposes the runtime's preferred compose CLI.
	Composer() Composer
}

// Container is the runtime-agnostic view of a single container.
type Container struct {
	ID      string
	Image   string
	State   string
	Exit    int
	Created int64
}

// LogOpts parameterises a Logs call.
type LogOpts struct {
	Follow bool
	Since  string
	Until  string
	Tail   int
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

// Composer locates the runtime's preferred compose CLI.
type Composer interface {
	Find() (ComposeCommand, error)
}

// ComposeCommand is a resolved compose CLI entry point.
//
// It exists here in `runtime` so callers do not need to depend on a
// runtime-specific subpackage just to discover the CLI. Each runtime's
// own FindCommand implementation returns a ComposeCommand populated for
// its preferred binary.
type ComposeCommand struct {
	// Name is the executable, e.g. "docker" or "podman".
	Name string

	// Base are the leading arguments, e.g. []string{"compose"} for the
	// docker v2 plugin shape.
	Base []string
}
