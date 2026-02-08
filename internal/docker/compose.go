package docker

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ComposeFile represents a parsed docker-compose.yml file
type ComposeFile struct {
	Version  string                    `yaml:"version"`
	Services map[string]ComposeService `yaml:"services"`
}

// ComposeService represents a service definition in docker-compose.yml
type ComposeService struct {
	Image         string        `yaml:"image"`
	Build         interface{}   `yaml:"build"` // Can be string or object
	ContainerName string        `yaml:"container_name"`
	Environment   interface{}   `yaml:"environment"` // Can be array or map
	Ports         []interface{} `yaml:"ports"`
	// We only care about enough fields to identify services
}

// ParseComposeFile reads and parses a docker-compose.yml file
func ParseComposeFile(path string) (*ComposeFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read compose file: %w", err)
	}

	var compose ComposeFile
	if err := yaml.Unmarshal(data, &compose); err != nil {
		return nil, fmt.Errorf("failed to parse compose file: %w", err)
	}

	if len(compose.Services) == 0 {
		return nil, fmt.Errorf("no services found in compose file")
	}

	return &compose, nil
}

// GetServiceNames returns a list of service names from the compose file
func (c *ComposeFile) GetServiceNames() []string {
	names := make([]string, 0, len(c.Services))
	for name := range c.Services {
		names = append(names, name)
	}
	return names
}

// GetProjectName extracts the project name from the compose file path
// Docker Compose uses the directory name as the default project name
func GetProjectName(composePath string) string {
	// For now, we'll implement this in the next task when we integrate with Docker API
	// Docker Compose typically uses the directory name as project name
	return ""
}
