package config

import (
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// The string form is in the README and the shipped example config, so it must
// keep working unchanged.
func TestDockerComposeConfig_AcceptsStringForm(t *testing.T) {
	var cfg Config
	if err := yaml.Unmarshal([]byte("docker_compose: ./docker-compose.yml\n"), &cfg); err != nil {
		t.Fatalf("string form should parse: %v", err)
	}

	if !cfg.DockerCompose.IsSet() {
		t.Fatal("compose config should be set")
	}
	if got := cfg.DockerCompose.PrimaryFile(); got != "./docker-compose.yml" {
		t.Errorf("primary file = %q", got)
	}
	if len(cfg.DockerCompose.Files) != 1 {
		t.Errorf("files = %v, want one entry", cfg.DockerCompose.Files)
	}
	// Unspecified start must default to ask, not the zero value.
	if got := cfg.DockerCompose.GetStart(); got != ComposeStartAsk {
		t.Errorf("start = %q, want %q", got, ComposeStartAsk)
	}
	if got := cfg.DockerCompose.GetStartTimeout(); got != DefaultComposeStartTimeout {
		t.Errorf("start_timeout = %s, want %s", got, DefaultComposeStartTimeout)
	}
}

func TestDockerComposeConfig_AcceptsStructuredForm(t *testing.T) {
	in := `
docker_compose:
  files:
    - docker-compose.yml
    - docker-compose.override.yml
  profiles: [api, workers]
  project_name: myproject
  env_file: .env
  start: always
  start_timeout: 5m
`
	var cfg Config
	if err := yaml.Unmarshal([]byte(in), &cfg); err != nil {
		t.Fatalf("structured form should parse: %v", err)
	}

	d := cfg.DockerCompose
	if len(d.Files) != 2 || d.Files[1] != "docker-compose.override.yml" {
		t.Errorf("files = %v", d.Files)
	}
	if len(d.Profiles) != 2 || d.Profiles[0] != "api" {
		t.Errorf("profiles = %v", d.Profiles)
	}
	if d.ProjectName != "myproject" {
		t.Errorf("project_name = %q", d.ProjectName)
	}
	if d.EnvFile != ".env" {
		t.Errorf("env_file = %q", d.EnvFile)
	}
	if d.GetStart() != ComposeStartAlways {
		t.Errorf("start = %q", d.GetStart())
	}
	if got := d.GetStartTimeout(); got != 5*time.Minute {
		t.Errorf("start_timeout = %s, want 5m", got)
	}
}

func TestDockerComposeConfig_Validate(t *testing.T) {
	base := []ProcessConfig{{Name: "x", Command: "echo hi"}}

	bad := &Config{Processes: base, DockerCompose: DockerComposeConfig{
		Files: []string{"a.yml"}, Start: "maybe",
	}}
	if err := bad.Validate(); err == nil {
		t.Error("an unknown start mode should be rejected")
	}

	empty := &Config{Processes: base, DockerCompose: DockerComposeConfig{Files: []string{""}}}
	if err := empty.Validate(); err == nil {
		t.Error("an empty compose file path should be rejected")
	}

	emptyProfile := &Config{Processes: base, DockerCompose: DockerComposeConfig{
		Files: []string{"a.yml"}, Profiles: []string{""},
	}}
	if err := emptyProfile.Validate(); err == nil {
		t.Error("an empty profile name should be rejected")
	}

	unparseable := &Config{Processes: base, DockerCompose: DockerComposeConfig{
		Files: []string{"a.yml"}, StartTimeout: "5 minutes",
	}}
	if err := unparseable.Validate(); err == nil {
		t.Error("an unparseable start_timeout should be rejected")
	}

	// A non-positive deadline has already passed, so the wait would give up
	// before any container could appear.
	zeroTimeout := &Config{Processes: base, DockerCompose: DockerComposeConfig{
		Files: []string{"a.yml"}, StartTimeout: "0s",
	}}
	if err := zeroTimeout.Validate(); err == nil {
		t.Error("a zero start_timeout should be rejected")
	}

	negativeTimeout := &Config{Processes: base, DockerCompose: DockerComposeConfig{
		Files: []string{"a.yml"}, StartTimeout: "-1m",
	}}
	if err := negativeTimeout.Validate(); err == nil {
		t.Error("a negative start_timeout should be rejected")
	}

	ok := &Config{Processes: base, DockerCompose: DockerComposeConfig{
		Files:        []string{"a.yml"},
		Profiles:     []string{"api"},
		Start:        ComposeStartNever,
		StartTimeout: "10m",
	}}
	if err := ok.Validate(); err != nil {
		t.Errorf("valid compose config rejected: %v", err)
	}

	// Validate must not require the file to exist: it is about config shape,
	// and ParseComposeFiles reports a missing file with better context.
	missing := &Config{Processes: base, DockerCompose: DockerComposeConfig{
		Files: []string{"/definitely/not/here/docker-compose.yml"},
	}}
	if err := missing.Validate(); err != nil {
		t.Errorf("Validate should not check file existence: %v", err)
	}
}

// docker_compose alone must satisfy the "at least one process or compose file"
// requirement, in both forms.
func TestDockerComposeConfig_SatisfiesMinimumConfig(t *testing.T) {
	structured := &Config{DockerCompose: DockerComposeConfig{Files: []string{"a.yml"}}}
	if err := structured.Validate(); err != nil {
		t.Errorf("compose-only config should be valid: %v", err)
	}

	none := &Config{}
	if err := none.Validate(); err == nil {
		t.Error("a config with neither processes nor compose should be rejected")
	}
}

func TestDockerComposeConfig_ExpandEnv(t *testing.T) {
	t.Setenv("STACK_DIR", "/srv/stack")
	t.Setenv("PROJ", "myproj")

	d := DockerComposeConfig{
		Files:       []string{"$STACK_DIR/docker-compose.yml"},
		ProjectName: "$PROJ",
		EnvFile:     "$STACK_DIR/.env",
	}
	d.ExpandEnv()

	if d.Files[0] != "/srv/stack/docker-compose.yml" {
		t.Errorf("files = %v", d.Files)
	}
	if d.ProjectName != "myproj" {
		t.Errorf("project_name = %q", d.ProjectName)
	}
	if d.EnvFile != "/srv/stack/.env" {
		t.Errorf("env_file = %q", d.EnvFile)
	}
}
