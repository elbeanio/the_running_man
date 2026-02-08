package docker

import (
	"context"
	"fmt"

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
