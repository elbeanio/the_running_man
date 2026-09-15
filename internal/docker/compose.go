package docker

import (
	"fmt"
	"os"
	"sort"

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

	// Profiles gate a service: Compose only starts it when one of these
	// profiles is active. Previously unparsed, so every service was treated as
	// expected and profile-gated ones were reported as watched while never
	// appearing.
	Profiles []string `yaml:"profiles"`

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

// GetServiceNames returns every service name in the compose file, including
// services gated behind a profile.
//
// Prefer ServiceNamesForProfiles when the active profiles are known.
func (c *ComposeFile) GetServiceNames() []string {
	names := make([]string, 0, len(c.Services))
	for name := range c.Services {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ServiceNamesForProfiles returns the services Compose would start with the
// given profiles active.
//
// A service with no `profiles:` is always started. A service with profiles is
// started only when at least one of them is active. Mirrors Compose's own rule,
// so that what Running Man reports as "watched" matches what will actually run.
func (c *ComposeFile) ServiceNamesForProfiles(active []string) []string {
	activeSet := make(map[string]bool, len(active))
	for _, p := range active {
		activeSet[p] = true
	}

	names := make([]string, 0, len(c.Services))
	for name, svc := range c.Services {
		if len(svc.Profiles) == 0 {
			names = append(names, name)
			continue
		}
		for _, p := range svc.Profiles {
			if activeSet[p] {
				names = append(names, name)
				break
			}
		}
	}
	sort.Strings(names)
	return names
}

// ServicesGatedOut returns services excluded by the active profiles, so the
// difference can be reported rather than silently applied.
func (c *ComposeFile) ServicesGatedOut(active []string) []string {
	included := make(map[string]bool)
	for _, n := range c.ServiceNamesForProfiles(active) {
		included[n] = true
	}

	var out []string
	for name := range c.Services {
		if !included[name] {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// ParseComposeFiles parses several Compose files and merges their services, in
// the order given, later files overriding earlier ones by service name.
//
// This is a service-name-level merge, not Compose's full deep merge: Running
// Man only needs to know which services exist and which profiles gate them.
func ParseComposeFiles(paths []string) (*ComposeFile, error) {
	merged := &ComposeFile{Services: map[string]ComposeService{}}

	for _, path := range paths {
		cf, err := parseComposeFileAllowEmpty(path)
		if err != nil {
			return nil, err
		}
		if merged.Version == "" {
			merged.Version = cf.Version
		}
		for name, svc := range cf.Services {
			merged.Services[name] = svc
		}
	}

	if len(merged.Services) == 0 {
		return nil, fmt.Errorf("no services found in compose file(s)")
	}
	return merged, nil
}

// parseComposeFileAllowEmpty parses one file without requiring it to define
// services: an override file legitimately may not.
func parseComposeFileAllowEmpty(path string) (*ComposeFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read compose file %s: %w", path, err)
	}

	var compose ComposeFile
	if err := yaml.Unmarshal(data, &compose); err != nil {
		return nil, fmt.Errorf("failed to parse compose file %s: %w", path, err)
	}
	return &compose, nil
}

// GetProjectName extracts the project name from the compose file path
// Docker Compose uses the directory name as the default project name
func GetProjectName(composePath string) string {
	// For now, we'll implement this in the next task when we integrate with Docker API
	// Docker Compose typically uses the directory name as project name
	return ""
}
