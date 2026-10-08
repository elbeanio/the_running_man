package docker

import (
	"fmt"
	"os"
	"sort"

	"gopkg.in/yaml.v3"
)

// ComposeFile represents a parsed docker-compose.yml file
type ComposeFile struct {
	Version string `yaml:"version"`

	// Name is the top-level `name:` key, which sets the Compose project name.
	// Without it Compose falls back to the directory name -- so a project that
	// declares one is not findable by directory name at all.
	Name string `yaml:"name"`

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

	// Healthcheck is parsed only to know whether one is defined: Docker runs it
	// and reports the result, which is what depends_on waits for.
	Healthcheck *ComposeHealthcheck `yaml:"healthcheck"`

	// We only care about enough fields to identify services
}

// ComposeHealthcheck is the part of a Compose healthcheck that says whether
// there is one.
type ComposeHealthcheck struct {
	Test    interface{} `yaml:"test"` // a string, or a list such as ["CMD", ...]
	Disable bool        `yaml:"disable"`
}

// HasHealthcheck reports whether Docker will run a healthcheck for the
// service, as defined in its Compose file. A healthcheck defined only in the
// image (a Dockerfile HEALTHCHECK) is not visible here.
func (s ComposeService) HasHealthcheck() bool {
	hc := s.Healthcheck
	if hc == nil || hc.Disable {
		return false
	}
	switch test := hc.Test.(type) {
	case string:
		return test != ""
	case []interface{}:
		// ["NONE"] is Compose's other way of disabling a healthcheck.
		return len(test) > 0 && test[0] != "NONE"
	}
	return false
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
		// Last non-empty name wins, matching Compose: when several files set
		// `name:`, the last one specified is the project name.
		if cf.Name != "" {
			merged.Name = cf.Name
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
