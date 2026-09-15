package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Start modes for offering to bring a Compose stack up.
const (
	// ComposeStartAsk offers to start the stack when nothing is running, but
	// only when there is a terminal to answer the question. In CI it behaves as
	// never.
	ComposeStartAsk = "ask"
	// ComposeStartNever never offers; the stack must already be running.
	ComposeStartNever = "never"
	// ComposeStartAlways starts without asking.
	ComposeStartAlways = "always"
)

// DockerComposeConfig describes a Compose project to watch.
//
// Accepts both the original string form and a structured form:
//
//	docker_compose: ./docker-compose.yml
//
//	docker_compose:
//	  files: [docker-compose.yml, docker-compose.override.yml]
//	  profiles: [api, workers]
//	  project_name: myproject
//	  env_file: .env
//	  start: ask
//
// The string form is kept working because it is in the README and the shipped
// example config.
type DockerComposeConfig struct {
	// Files are the Compose files, in order, equivalent to repeated -f. Real
	// projects routinely layer a base file and an override.
	Files []string `yaml:"files,omitempty"`

	// Profiles are the active Compose profiles. Services gated behind a profile
	// that is not listed here are not expected to be running, and are not
	// reported as missing.
	Profiles []string `yaml:"profiles,omitempty"`

	// ProjectName overrides the project name. Compose defaults it to the
	// directory name, but `docker compose -p` and COMPOSE_PROJECT_NAME override
	// that -- and container discovery filters on the project label, so getting
	// this wrong means finding nothing at all.
	ProjectName string `yaml:"project_name,omitempty"`

	// EnvFile is passed to Compose as --env-file.
	EnvFile string `yaml:"env_file,omitempty"`

	// Start controls whether to offer to bring the stack up when nothing is
	// running: ask (default), never, always.
	Start string `yaml:"start,omitempty"`
}

// UnmarshalYAML accepts either a plain path string or the structured mapping.
func (d *DockerComposeConfig) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		var path string
		if err := value.Decode(&path); err != nil {
			return fmt.Errorf("docker_compose: %w", err)
		}
		if path != "" {
			d.Files = []string{path}
		}
		return nil

	case yaml.MappingNode:
		// A distinct type avoids recursing into this method.
		type raw DockerComposeConfig
		var r raw
		if err := value.Decode(&r); err != nil {
			return fmt.Errorf("docker_compose: %w", err)
		}
		*d = DockerComposeConfig(r)
		return nil

	default:
		return fmt.Errorf("docker_compose must be a path or a mapping, got %v", value.Kind)
	}
}

// IsSet reports whether any Compose file was configured.
func (d *DockerComposeConfig) IsSet() bool {
	return d != nil && len(d.Files) > 0
}

// PrimaryFile is the first Compose file, used where a single path is needed
// (deriving the default project name, and reporting).
func (d *DockerComposeConfig) PrimaryFile() string {
	if !d.IsSet() {
		return ""
	}
	return d.Files[0]
}

// GetStart returns the start mode, defaulting to ask.
func (d *DockerComposeConfig) GetStart() string {
	if d == nil || d.Start == "" {
		return ComposeStartAsk
	}
	return d.Start
}

// Validate checks the Compose configuration.
func (d *DockerComposeConfig) Validate() error {
	if !d.IsSet() {
		return nil
	}

	// Deliberately does NOT check that the files exist. Validate is about the
	// shape of the configuration; the files are read moments later by
	// ParseComposeFiles, which reports a missing file with better context. A
	// second check here would only duplicate that error with less information,
	// and would make config validation depend on the filesystem.
	for _, f := range d.Files {
		if f == "" {
			return fmt.Errorf("docker_compose files must not contain empty paths")
		}
	}

	switch d.GetStart() {
	case ComposeStartAsk, ComposeStartNever, ComposeStartAlways:
	default:
		return fmt.Errorf("docker_compose start must be one of %q, %q or %q, got %q",
			ComposeStartAsk, ComposeStartNever, ComposeStartAlways, d.Start)
	}

	for _, p := range d.Profiles {
		if p == "" {
			return fmt.Errorf("docker_compose profiles must not contain empty names")
		}
	}

	return nil
}

// ExpandEnv expands environment variables in the Compose paths and names.
func (d *DockerComposeConfig) ExpandEnv() {
	if d == nil {
		return
	}
	for i, f := range d.Files {
		d.Files[i] = os.ExpandEnv(f)
	}
	d.ProjectName = os.ExpandEnv(d.ProjectName)
	d.EnvFile = os.ExpandEnv(d.EnvFile)
}
