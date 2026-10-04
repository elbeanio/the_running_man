package docker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/moby/moby/client"
)

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

// Container represents a discovered Docker container
type Container struct {
	ID          string
	Name        string
	ServiceName string
	ProjectName string
	Image       string
	State       string
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

// ContainerEvent represents a Docker container lifecycle event
type ContainerEvent struct {
	Type        string // start, stop, die, restart, kill
	ContainerID string
	Name        string
	Image       string
}

// EventHandler is called when a container lifecycle event occurs
type EventHandler func(event ContainerEvent)

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
