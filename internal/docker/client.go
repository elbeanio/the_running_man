package docker

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

// Client wraps the Docker client for container management
type Client struct {
	cli *client.Client
}

// NewClient creates a new Docker client
func NewClient() (*Client, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
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

	// Sanitize project name (Docker Compose converts to lowercase and replaces _ with -)
	projectName = strings.ToLower(projectName)
	projectName = strings.ReplaceAll(projectName, "_", "-")

	return projectName
}
