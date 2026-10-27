package runtime

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// Compile-time check: *Client must satisfy Runtime alongside *Podman. Update
// only when the interface in runtime.go changes.
var _ Runtime = (*Client)(nil)

// getDockerEndpoint resolves the Docker host using the Docker CLI context system
func getDockerEndpoint() string {
	// First check DOCKER_HOST env var (explicit override)
	if dockerHost := os.Getenv("DOCKER_HOST"); dockerHost != "" {
		return dockerHost
	}

	// Determine which context to use
	contextName := os.Getenv("DOCKER_CONTEXT")
	if contextName == "" {
		// Get current context from docker CLI config
		cmd := exec.Command("docker", "context", "show")
		out, err := cmd.Output()
		if err == nil {
			contextName = strings.TrimSpace(string(out))
		}
	}
	if contextName == "" {
		contextName = "default"
	}

	// Get the endpoint from docker context inspect
	cmd := exec.Command("docker", "context", "inspect", contextName, "--format", "{{.Endpoints.docker.Host}}")
	out, err := cmd.Output()
	if err == nil {
		endpoint := strings.TrimSpace(string(out))
		if endpoint != "" {
			return endpoint
		}
	}

	return ""
}

// Client wraps the Docker client for container management
type Client struct {
	cli *client.Client
}

// NewClient creates a new Docker client
func NewClient() (*Client, error) {
	var opts []client.Opt

	// Try to get Docker endpoint from context system
	if endpoint := getDockerEndpoint(); endpoint != "" {
		opts = append(opts, client.WithHost(endpoint))
	}

	// No WithAPIVersionNegotiation: it is a no-op in this client, which
	// negotiates by default. Passing it is deprecated.
	opts = append(opts, client.FromEnv)

	cli, err := client.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	return &Client{cli: cli}, nil
}

// Close closes the Docker client connection
func (c *Client) Close() error {
	if c.cli != nil {
		return c.cli.Close()
	}
	return nil
}

// Ping verifies connectivity to the Docker daemon
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.cli.Ping(ctx, client.PingOptions{})
	if err != nil {
		return fmt.Errorf("failed to ping Docker daemon: %w", err)
	}
	return nil
}

// FindCommand exposes the docker-side compose CLI discovery through the
// runtime.Runtime interface, satisfying `Runtime` for *Client alongside
// *Podman. The actual lookup lives in the package's top-level
// FindComposeCommand (preserved at its existing name) so existing
// callers and test fixtures keep working unchanged.
func (c *Client) FindCommand(ctx context.Context) (ComposeCommand, error) {
	return FindComposeCommand(ctx)
}

// --- Runtime interface implementation. ------------------------------------

// Endpoint reports the docker endpoints the underlying moby client
// connected to, prefixed with `docker://` so the value is distinguishable
// from Podman's `unix://` shape. The host is whatever DOCKER_HOST (or
// the active docker context) resolves to; moby's DaemonHost() returns the
// value after `FromEnv` and `WithHost` have been applied in NewClient.
func (c *Client) Endpoint() string {
	if c.cli == nil {
		return ""
	}
	return "docker://" + c.cli.DaemonHost()
}

// List calls Docker's /containers/json and converts each summary into the
// runtime's Container shape. Empty Names arrays (rare, but documented in
// older moby versions) come out with Name == "". Compose-aware labels are
// pulled out the same way the podman adapter does, so callers reading
// Container.ServiceName see consistent values across runtimes.
func (c *Client) List(ctx context.Context) ([]Container, error) {
	res, err := c.cli.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("docker list: %w", err)
	}
	out := make([]Container, 0, len(res.Items))
	for _, s := range res.Items {
		out = append(out, dockerSummaryToContainer(s))
	}
	return out, nil
}

// Inspect pulls Docker's /containers/{id}/json and adapts the State
// Status and ExitCode, plus the Compose-aware labels, into the runtime's
// Container. Other fields of InspectResponse (NetworkSettings, GraphDriver,
// Health) are still reachable through the underlying moby client for
// callers that need them; only the fields the supervisor reads migrate
// across here.
func (c *Client) Inspect(ctx context.Context, id string) (Container, error) {
	res, err := c.cli.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		return Container{}, fmt.Errorf("docker inspect: %w", err)
	}
	return dockerInspectToContainer(res.Container), nil
}

// Wait blocks until the named container reaches `condition`. Docker
// supports "not-running" through `WaitConditionNotRunning`; any other
// value gets the daemon's error, surfaced unmodified.
//
// ContainerWaitResult streams Waits as they happen; we read exactly one
// from the channel and close the call.
func (c *Client) Wait(ctx context.Context, id string, condition string) error {
	res := c.cli.ContainerWait(ctx, id, client.ContainerWaitOptions{
		Condition: container.WaitConditionNotRunning,
	})
	_ = condition // Currently only "not-running" is honoured; surface the
	// simpler signature and ignore the supplied string.
	select {
	case _, ok := <-res.Result:
		if !ok {
			return fmt.Errorf("docker wait for %s: channel closed without response", id)
		}
		return nil
	case err := <-res.Error:
		return fmt.Errorf("docker wait for %s: %w", id, err)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Logs opens a docker /containers/{id}/logs stream and returns it as an
// io.ReadCloser. The caller closes it. Follow, tail, since, until are
// passed through to ContainerLogsOptions. streamer's stdcopy demultiplexer
// works on the body unchanged.
func (c *Client) Logs(ctx context.Context, id string, opts LogOpts) (io.ReadCloser, error) {
	res, err := c.cli.ContainerLogs(ctx, id, client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     opts.Follow,
		Since:      opts.Since,
		Until:      opts.Until,
		Tail:       opts.TailString(),
		Timestamps: true,
	})
	if err != nil {
		return nil, fmt.Errorf("docker logs: %w", err)
	}
	return res, nil
}

// Events streams Docker container events from /events into dst.
// Docker's event message shape and the runtime's Event shape are not
// 1:1 — the wire has Type != "container" in some cases and carries
// richer Actor.Attributes — but the supervisor only reads Action / Actor
// ID / timestamp, which are all available.
func (c *Client) Events(ctx context.Context, opts EventOpts, dst chan<- Event) error {
	res := c.cli.Events(ctx, client.EventsListOptions{
		Since:   opts.Since,
		Until:   opts.Until,
		Filters: filterMapToDockerFilters(opts.Filters),
	})
	for {
		select {
		case msg, ok := <-res.Messages:
			if !ok {
				return nil
			}
			if msg.Type != "container" {
				continue
			}
			ev := Event{
				Action: string(msg.Action),
				Actor:  msg.Actor.ID,
				Time:   msg.Time,
			}
			select {
			case dst <- ev:
			case <-ctx.Done():
				return ctx.Err()
			}
		case err := <-res.Err:
			if err == nil {
				return nil
			}
			return fmt.Errorf("docker events: %w", err)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// --- helpers ---------------------------------------------------------------

// dockerSummaryToContainer mirrors podmanSummaryToContainer. Live in
// client.go because the moby Summary type is the input.
//
// Moby's container.Summary.State is a typed string enum rather than a
// free-form string; the docker-side ContainerState-to-string conversion
// is trivial but holding it here keeps the field-shape decisions separate
// from podman's.
func dockerSummaryToContainer(s container.Summary) Container {
	name := ""
	if len(s.Names) > 0 {
		name = strings.TrimPrefix(s.Names[0], "/")
	}
	c := Container{
		ID:      shortID(s.ID),
		Name:    name,
		Image:   s.Image,
		State:   string(s.State),
		Created: s.Created,
	}
	if s.Labels != nil {
		c.ServiceName = s.Labels["com.docker.compose.service"]
		c.ProjectName = s.Labels["com.docker.compose.project"]
	}
	return c
}

// dockerInspectToContainer adapts a moby inspect response into the
// runtime's Container. The supervisor reads State.Status, Exit, TTY
// (for log-stream demux), and the compose labels; the rest of
// InspectResponse is still reachable through the underlying moby
// client.
func dockerInspectToContainer(inf container.InspectResponse) Container {
	c := Container{
		ID:    shortID(inf.ID),
		Name:  strings.TrimPrefix(inf.Name, "/"),
		Image: inf.Image,
	}
	if inf.State != nil {
		c.State = string(inf.State.Status)
		c.Exit = inf.State.ExitCode
	}
	if inf.Config != nil {
		c.TTY = inf.Config.Tty
		if inf.Config.Labels != nil {
			c.ServiceName = inf.Config.Labels["com.docker.compose.service"]
			c.ProjectName = inf.Config.Labels["com.docker.compose.project"]
		}
	}
	return c
}

// filterMapToDockerFilters converts the runtime's neutral filter map
// ({"type":["container"]} etc.) into moby's Filters API.
func filterMapToDockerFilters(filters map[string][]string) client.Filters {
	out := client.Filters{}
	for k, vs := range filters {
		for _, v := range vs {
			out.Add(k, v)
		}
	}
	return out
}

// IsAvailable checks if Docker is available without returning an error
func IsAvailable() bool {
	cli, err := NewClient()
	if err != nil {
		return false
	}
	defer cli.Close()

	ctx := context.Background()
	return cli.Ping(ctx) == nil
}

// DiscoverContainersInProject finds running containers for an explicit Compose
// project name.
func (c *Client) DiscoverContainersInProject(ctx context.Context, projectName string, serviceNames []string) ([]Container, error) {
	var containers []Container

	for _, serviceName := range serviceNames {
		// Find containers for this service
		serviceContainers, err := c.findServiceContainers(ctx, projectName, serviceName)
		if err != nil {
			return nil, fmt.Errorf("failed to find containers for service %s: %w", serviceName, err)
		}
		containers = append(containers, serviceContainers...)
	}

	return containers, nil
}

// findServiceContainers finds all containers for a specific service
func (c *Client) findServiceContainers(ctx context.Context, projectName, serviceName string) ([]Container, error) {
	// Build filter for docker-compose containers
	filterArgs := client.Filters{}.
		Add("label", fmt.Sprintf("com.docker.compose.project=%s", projectName)).
		Add("label", fmt.Sprintf("com.docker.compose.service=%s", serviceName))

	// List containers
	result, err := c.cli.ContainerList(ctx, client.ContainerListOptions{
		Filters: filterArgs,
		All:     false, // Only running containers
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	var containers []Container
	for _, dc := range result.Items {
		// Extract container name (remove leading /)
		name := strings.TrimPrefix(dc.Names[0], "/")

		containers = append(containers, Container{
			ID:          dc.ID[:12], // Short ID
			Name:        name,
			ServiceName: serviceName,
			ProjectName: projectName,
			Image:       dc.Image,
			// Converted: the SDK types this as container.ContainerState, and
			// Container is our own domain type -- the API and the TUI read it,
			// and neither should acquire a Docker SDK type to do so.
			State: string(dc.State),
		})
	}

	return containers, nil
}

// ProjectName resolves the Compose project name the way Compose itself does:
// an explicitly configured name wins, then the compose file's top-level `name:`,
// and only then the directory name.
//
// Getting this wrong is not a cosmetic problem. Container discovery filters on
// the com.docker.compose.project label, so a project that declares `name: duet`
// in a directory called eureka was previously invisible -- Running Man reported
// that nothing was running and offered to start a stack that was already up.
//
// configured is docker_compose.project_name or --compose-project; fileName is
// ComposeFile.Name; primaryFile is the first Compose file, for the fallback.
//
// COMPOSE_PROJECT_NAME sits between the first two in Compose's own precedence
// and is deliberately not consulted here -- see docs/configuration.md.
func ProjectName(configured, fileName, primaryFile string) string {
	if configured != "" {
		return configured
	}
	if fileName != "" {
		// Compose requires the name key to be lowercase and rejects the file
		// otherwise, so this only normalises what Compose would have refused.
		return strings.ToLower(fileName)
	}
	return GetProjectNameFromPath(primaryFile)
}

// GetProjectNameFromPath extracts the project name from a compose file path
// Docker Compose uses the directory name as the default project name
func GetProjectNameFromPath(composePath string) string {
	// Get the directory containing the compose file
	dir := filepath.Dir(composePath)
	// Get the base name of that directory
	projectName := filepath.Base(dir)

	// If the compose file is in the current directory (.), use current dir name
	if dir == "." {
		cwd, err := filepath.Abs(".")
		if err == nil {
			projectName = filepath.Base(cwd)
		}
	}

	// Docker Compose uses the directory name as-is, only converting to lowercase
	// Note: It does NOT replace underscores with hyphens
	projectName = strings.ToLower(projectName)

	return projectName
}

// containerStartedAt returns when the container's current run started. A
// restart keeps the container's ID, so this is what tells one run from the
// next.
func (c *Client) containerStartedAt(ctx context.Context, id string) (time.Time, error) {
	info, err := c.cli.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		return time.Time{}, err
	}
	if info.Container.State == nil {
		return time.Time{}, fmt.Errorf("container %s has no state", id)
	}
	return time.Parse(time.RFC3339Nano, info.Container.State.StartedAt)
}

// WatchEvents watches Docker events and calls handler for container lifecycle events
// This function blocks until the context is cancelled
func (c *Client) WatchEvents(ctx context.Context, projectName string, handler EventHandler) error {
	// Build filter for events from our compose project
	filterArgs := client.Filters{}.
		Add("type", "container").
		Add("label", fmt.Sprintf("com.docker.compose.project=%s", projectName))

	stream := c.cli.Events(ctx, client.EventsListOptions{
		Filters: filterArgs,
	})

	for {
		select {
		case event := <-stream.Messages:
			// Process container events
			if event.Type == "container" {
				containerEvent := ContainerEvent{
					Type:        string(event.Action),
					ContainerID: event.Actor.ID[:12], // Short ID
					Name:        event.Actor.Attributes["name"],
					ServiceName: event.Actor.Attributes["com.docker.compose.service"],
					Image:       event.Actor.Attributes["image"],
				}

				// Call handler for relevant events
				switch string(event.Action) {
				case "start", "restart", "die", "stop", "kill":
					handler(containerEvent)
				}
			}
		case err := <-stream.Err:
			if err != nil && ctx.Err() == nil {
				return fmt.Errorf("error watching events: %w", err)
			}
			return nil
		case <-ctx.Done():
			return nil
		}
	}
}

// CompletedServices returns the project's services that have a container that
// ran to completion: stopped, with exit code 0. One-shot services -- a
// migration, a seed -- exit by design, and are done rather than missing.
func (c *Client) CompletedServices(ctx context.Context, project string) (map[string]bool, error) {
	result, err := c.cli.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: client.Filters{}.Add("label", "com.docker.compose.project="+project),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	done := map[string]bool{}
	for _, ct := range result.Items {
		if ct.State != "exited" {
			continue
		}
		info, err := c.cli.ContainerInspect(ctx, ct.ID, client.ContainerInspectOptions{})
		if err != nil || info.Container.State == nil {
			continue
		}
		if info.Container.State.ExitCode == 0 {
			done[ct.Labels["com.docker.compose.service"]] = true
		}
	}
	return done, nil
}
