package docker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
	return path
}

// Profiles were not parsed at all, so every service was treated as expected and
// profile-gated services were reported as watched while never appearing.
func TestServiceNamesForProfiles(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "docker-compose.yml", `
services:
  web:
    image: nginx
  db:
    image: postgres
  api:
    image: api
    profiles: [backend]
  worker:
    image: worker
    profiles: [backend, jobs]
  debug:
    image: debug
    profiles: [debugging]
`)

	cf, err := ParseComposeFiles([]string{path})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	tests := []struct {
		profiles []string
		want     []string
	}{
		// No profiles: only ungated services, matching Compose's own rule.
		{nil, []string{"db", "web"}},
		{[]string{"backend"}, []string{"api", "db", "web", "worker"}},
		{[]string{"jobs"}, []string{"db", "web", "worker"}},
		{[]string{"backend", "debugging"}, []string{"api", "db", "debug", "web", "worker"}},
		{[]string{"nonexistent"}, []string{"db", "web"}},
	}
	for _, tt := range tests {
		got := cf.ServiceNamesForProfiles(tt.profiles)
		if strings.Join(got, ",") != strings.Join(tt.want, ",") {
			t.Errorf("profiles %v: got %v, want %v", tt.profiles, got, tt.want)
		}
	}

	// What is excluded must be reportable, not silently dropped.
	gated := cf.ServicesGatedOut(nil)
	if strings.Join(gated, ",") != "api,debug,worker" {
		t.Errorf("gated out = %v", gated)
	}
	if got := cf.ServicesGatedOut([]string{"backend", "debugging"}); len(got) != 0 {
		t.Errorf("nothing should be gated out with all profiles active, got %v", got)
	}
}

// Real projects layer a base file and an override.
func TestParseComposeFiles_Merges(t *testing.T) {
	dir := t.TempDir()
	base := writeFile(t, dir, "base.yml", `
services:
  web:
    image: nginx
  api:
    image: api:1
`)
	override := writeFile(t, dir, "override.yml", `
services:
  api:
    image: api:2
    profiles: [backend]
  extra:
    image: extra
`)

	cf, err := ParseComposeFiles([]string{base, override})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if len(cf.Services) != 3 {
		t.Errorf("services = %v, want web/api/extra", cf.GetServiceNames())
	}
	// Later files win.
	if cf.Services["api"].Image != "api:2" {
		t.Errorf("api image = %q, want api:2 (later file should win)", cf.Services["api"].Image)
	}
	// And the override's profile gating applies.
	if got := cf.ServiceNamesForProfiles(nil); strings.Join(got, ",") != "extra,web" {
		t.Errorf("with no profiles: got %v, want extra,web", got)
	}
}

// An override file that defines no services is legitimate.
func TestParseComposeFiles_OverrideWithoutServices(t *testing.T) {
	dir := t.TempDir()
	base := writeFile(t, dir, "base.yml", "services:\n  web:\n    image: nginx\n")
	empty := writeFile(t, dir, "empty.yml", "version: '3'\n")

	cf, err := ParseComposeFiles([]string{base, empty})
	if err != nil {
		t.Fatalf("an override without services should be allowed: %v", err)
	}
	if len(cf.Services) != 1 {
		t.Errorf("services = %v", cf.GetServiceNames())
	}
}

func TestParseComposeFiles_ErrorsAreSpecific(t *testing.T) {
	if _, err := ParseComposeFiles([]string{"/definitely/not/here.yml"}); err == nil {
		t.Error("a missing file should error")
	} else if !strings.Contains(err.Error(), "not/here.yml") {
		t.Errorf("error should name the file, got: %v", err)
	}

	dir := t.TempDir()
	noServices := writeFile(t, dir, "empty.yml", "version: '3'\n")
	if _, err := ParseComposeFiles([]string{noServices}); err == nil {
		t.Error("a file set with no services at all should error")
	}
}

// Compose exists as a `docker compose` subcommand and a standalone binary, and
// the up arguments must carry every configured option through.
func TestComposeOptions_UpArgs(t *testing.T) {
	opts := ComposeOptions{
		Files:       []string{"a.yml", "b.yml"},
		Profiles:    []string{"api", "jobs"},
		ProjectName: "proj",
		EnvFile:     ".env",
	}

	got := strings.Join(opts.UpArgs(), " ")
	want := "-f a.yml -f b.yml -p proj --env-file .env --profile api --profile jobs up -d"
	if got != want {
		t.Errorf("UpArgs =\n  %s\nwant\n  %s", got, want)
	}

	// Detached, because Running Man streams logs itself via the Docker API.
	if !strings.HasSuffix(got, "up -d") {
		t.Error("compose must be started detached")
	}

	// Nothing in this package may ever tear the stack down: it belongs to the
	// developer, before and after.
	for _, forbidden := range []string{"down", "stop", "rm", "kill"} {
		if strings.Contains(got, " "+forbidden) {
			t.Errorf("UpArgs must never contain %q", forbidden)
		}
	}
}

func TestComposeOptions_UpArgs_Minimal(t *testing.T) {
	opts := ComposeOptions{Files: []string{"docker-compose.yml"}}
	got := strings.Join(opts.UpArgs(), " ")
	if got != "-f docker-compose.yml up -d" {
		t.Errorf("UpArgs = %q", got)
	}
}

func TestComposeCommand_String(t *testing.T) {
	if got := (ComposeCommand{Name: "docker", Base: []string{"compose"}}).String(); got != "docker compose" {
		t.Errorf("got %q", got)
	}
	if got := (ComposeCommand{Name: "docker-compose"}).String(); got != "docker-compose" {
		t.Errorf("got %q", got)
	}
}

func TestDisplayCommand_ShowsExactlyWhatWillRun(t *testing.T) {
	cmd := ComposeCommand{Name: "docker", Base: []string{"compose"}}
	opts := ComposeOptions{Files: []string{"a.yml"}, Profiles: []string{"api"}}

	got := DisplayCommand(cmd, opts)
	want := "docker compose -f a.yml --profile api up -d"
	if got != want {
		t.Errorf("DisplayCommand = %q, want %q", got, want)
	}
}
