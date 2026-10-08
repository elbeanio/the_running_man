package config

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
)

// DefaultHealthcheckTimeout is how long a healthcheck has to pass before the
// startup it gates is stopped.
const DefaultHealthcheckTimeout = 60 * time.Second

// HealthcheckConfig says how to tell that a process or Compose service is
// ready, for whatever depends on it. Exactly one of Port, HTTP and Log is set.
//
// One key per check type, rather than a `type:` and a `value:`, so it reads as
// ordinary YAML and the value's own type is checked by the decoder.
type HealthcheckConfig struct {
	// Port passes once a TCP connection to localhost:Port succeeds.
	Port int `yaml:"port,omitempty"`
	// HTTP passes once a GET of the URL returns any 2xx.
	HTTP string `yaml:"http,omitempty"`
	// Log passes once a line of the current run contains this substring.
	Log string `yaml:"log,omitempty"`
	// Timeout is how long the check has to pass, as a duration string.
	// Defaults to DefaultHealthcheckTimeout.
	Timeout string `yaml:"timeout,omitempty"`
}

// Validate checks the healthcheck's own shape. Whether anything depends on it,
// and whether it is allowed where it is, is ValidateDependencies' business.
func (h *HealthcheckConfig) Validate() error {
	set := 0
	if h.Port != 0 {
		set++
	}
	if h.HTTP != "" {
		set++
	}
	if h.Log != "" {
		set++
	}
	if set != 1 {
		return fmt.Errorf("a healthcheck needs exactly one of port, http or log")
	}

	if h.Port != 0 && (h.Port < 1 || h.Port > 65535) {
		return fmt.Errorf("healthcheck port %d is not a valid port", h.Port)
	}
	if h.HTTP != "" {
		u, err := url.Parse(h.HTTP)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("healthcheck http %q must be a full http:// or https:// URL", h.HTTP)
		}
	}
	if h.Timeout != "" {
		d, err := time.ParseDuration(h.Timeout)
		if err != nil {
			return fmt.Errorf("invalid healthcheck timeout %q: %w", h.Timeout, err)
		}
		if d <= 0 {
			return fmt.Errorf("healthcheck timeout must be positive, got %q", h.Timeout)
		}
	}
	return nil
}

// GetTimeout returns the timeout, or DefaultHealthcheckTimeout if none is set.
func (h *HealthcheckConfig) GetTimeout() time.Duration {
	if h.Timeout == "" {
		return DefaultHealthcheckTimeout
	}
	d, err := time.ParseDuration(h.Timeout)
	if err != nil || d <= 0 {
		return DefaultHealthcheckTimeout
	}
	return d
}

// ComposeServices is what dependency validation needs to know about the
// Compose stack. The caller builds it from the parsed Compose files, which
// this package does not read.
type ComposeServices struct {
	// Active are the services the active profiles start.
	Active map[string]bool
	// GatedOut are the services an inactive profile leaves out.
	GatedOut map[string]bool
	// WithHealthcheck are the services whose Compose file defines a
	// healthcheck, so Docker reports their health.
	WithHealthcheck map[string]bool
}

// ValidateDependencies refuses a dependency graph that cannot be resolved,
// before anything starts. Every problem is reported, not just the first.
//
// A dependency is a hard stop on startup, so anything that could leave a
// process waiting on something that will never be ready is refused here: an
// unknown or ambiguous name, a cycle, a recurring process (never "ready" for
// long), a service no active profile starts, and anything depended on without
// a healthcheck to say when it is ready.
func (c *Config) ValidateDependencies(compose ComposeServices) error {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}

	procs := make(map[string]*ProcessConfig, len(c.Processes))
	for i := range c.Processes {
		procs[c.Processes[i].Name] = &c.Processes[i]
	}

	for _, p := range c.Processes {
		if p.Interval != "" && p.Healthcheck != nil {
			fail("%s is a recurring process, so it cannot be depended on and its healthcheck "+
				"would never be used; remove the healthcheck", p.Name)
		}

		seen := map[string]bool{}
		for _, dep := range p.DependsOn {
			if seen[dep] {
				fail("%s lists %q in depends_on more than once", p.Name, dep)
				continue
			}
			seen[dep] = true

			if dep == p.Name {
				fail("%s depends on itself", p.Name)
				continue
			}

			target, isProc := procs[dep]
			isService := compose.Active[dep]

			switch {
			case isProc && isService:
				fail("%s depends on %q, but %q is both a process and a Compose service; "+
					"rename one so the dependency is unambiguous", p.Name, dep, dep)
			case isProc && target.Interval != "":
				fail("%s depends on %q, a recurring process, which is never ready for long "+
					"enough to depend on", p.Name, dep)
			case isProc && target.Healthcheck == nil:
				fail("%s depends on %q, which has no healthcheck; add one to %s so %s knows "+
					"when it is ready", p.Name, dep, dep, p.Name)
			case isProc:
			case isService:
				_, declared := c.DockerCompose.Healthchecks[dep]
				if !compose.WithHealthcheck[dep] && !declared {
					fail("%s depends on Compose service %q, which has no healthcheck; add one "+
						"to its Compose file, or under docker_compose.healthchecks", p.Name, dep)
				}
			case compose.GatedOut[dep]:
				fail("%s depends on Compose service %q, which no active profile starts",
					p.Name, dep)
			default:
				fail("%s depends on %q, which is neither a process nor a Compose service",
					p.Name, dep)
			}
		}
	}

	for _, name := range sortedKeys(c.DockerCompose.Healthchecks) {
		switch {
		case compose.WithHealthcheck[name]:
			fail("Compose service %q already has a healthcheck in its Compose file; remove "+
				"the one under docker_compose.healthchecks", name)
		case !compose.Active[name] && !compose.GatedOut[name]:
			fail("docker_compose.healthchecks names %q, which is not a Compose service", name)
		}
	}

	if cycle := findCycle(c.Processes); cycle != nil {
		fail("dependency cycle: %s", strings.Join(cycle, " -> "))
	}

	return errors.Join(errs...)
}

// findCycle returns one dependency cycle among the processes, as a path that
// starts and ends at the same process, or nil. Only processes can depend on
// anything, so a cycle can only run through processes.
func findCycle(processes []ProcessConfig) []string {
	deps := make(map[string][]string, len(processes))
	for _, p := range processes {
		deps[p.Name] = p.DependsOn
	}

	const (
		unvisited = iota
		visiting
		done
	)
	state := map[string]int{}
	var path []string

	var visit func(string) []string
	visit = func(name string) []string {
		switch state[name] {
		case visiting:
			start := slices.Index(path, name)
			return append(slices.Clone(path[start:]), name)
		case done:
			return nil
		}
		state[name] = visiting
		path = append(path, name)
		for _, d := range deps[name] {
			if _, isProc := deps[d]; !isProc || d == name {
				continue // not a process, or a self-dependency reported separately
			}
			if c := visit(d); c != nil {
				return c
			}
		}
		path = path[:len(path)-1]
		state[name] = done
		return nil
	}

	for _, p := range processes {
		if c := visit(p.Name); c != nil {
			return c
		}
	}
	return nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
