package docker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

const (
	colimaSocketPath = "/Users/%s/.colima/default/docker.sock"
)

// Client wraps the Docker client for container management
type Client struct {
	cli *client.Client
}

// NewClient creates a new Docker client
func NewClient() (*Client, error) {
	var opts []client.Opt

	// Check for DOCKER_HOST environment variable (used by Docker Desktop, etc.)
	if dockerHost := os.Getenv("DOCKER_HOST"); dockerHost != "" {
		opts = append(opts, client.WithHost(dockerHost))
	} else if _, err := os.Stat("/var/run/docker.sock"); err != nil {
		// Standard socket not found, try Colima socket
		homeDir, err := os.UserHomeDir()
		if err == nil {
			colimaSocket := fmt.Sprintf(colimaSocketPath, homeDir)
			if _, err := os.Stat(colimaSocket); err == nil {
				opts = append(opts, client.WithHost("unix://"+colimaSocket))
			}
		}
	}

	opts = append(opts, client.FromEnv, client.WithAPIVersionNegotiation())

	cli, err := client.NewClientWithOpts(opts...)
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
	_, err := c.cli.Ping(ctx)
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

// DiscoverContainers finds running containers for the given compose file
func (c *Client) DiscoverContainers(ctx context.Context, composePath string, serviceNames []string) ([]Container, error) {
	// Extract project name from compose file path
	projectName := GetProjectNameFromPath(composePath)

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
	filterArgs := filters.NewArgs()
	filterArgs.Add("label", fmt.Sprintf("com.docker.compose.project=%s", projectName))
	filterArgs.Add("label", fmt.Sprintf("com.docker.compose.service=%s", serviceName))

	// List containers
	dockerContainers, err := c.cli.ContainerList(ctx, container.ListOptions{
		Filters: filterArgs,
		All:     false, // Only running containers
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	var containers []Container
	for _, dc := range dockerContainers {
		// Extract container name (remove leading /)
		name := strings.TrimPrefix(dc.Names[0], "/")

		containers = append(containers, Container{
			ID:          dc.ID[:12], // Short ID
			Name:        name,
			ServiceName: serviceName,
			ProjectName: projectName,
			Image:       dc.Image,
			State:       dc.State,
		})
	}

	return containers, nil
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
	filterArgs := filters.NewArgs()
	filterArgs.Add("type", "container")
	filterArgs.Add("label", fmt.Sprintf("com.docker.compose.project=%s", projectName))

	eventOptions := events.ListOptions{
		Filters: filterArgs,
	}

	eventsChan, errsChan := c.cli.Events(ctx, eventOptions)

	for {
		select {
		case event := <-eventsChan:
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
		case err := <-errsChan:
			if err != nil && ctx.Err() == nil {
				return fmt.Errorf("error watching events: %w", err)
			}
			return nil
		case <-ctx.Done():
			return nil
		}
	}
}
