package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func loadYAML(t *testing.T, content string) (*Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "running-man.yml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return LoadConfig(path)
}

// The keys parse into the structure the rest of the feature reads.
func TestDependsOn_Parses(t *testing.T) {
	cfg, err := loadYAML(t, `
processes:
  - name: backend
    command: uvicorn app:app
    healthcheck:
      port: 8000
      timeout: 90s
  - name: frontend
    command: npm run dev
    depends_on: [backend, db]
docker_compose:
  files: [docker-compose.yml]
  healthchecks:
    redis:
      log: "Ready to accept connections"
`)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if hc := cfg.Processes[0].Healthcheck; hc == nil || hc.Port != 8000 || hc.GetTimeout() != 90*time.Second {
		t.Errorf("backend healthcheck = %+v", hc)
	}
	if got := cfg.Processes[1].DependsOn; len(got) != 2 || got[0] != "backend" || got[1] != "db" {
		t.Errorf("frontend depends_on = %v", got)
	}
	if hc, ok := cfg.DockerCompose.Healthchecks["redis"]; !ok || hc.Log != "Ready to accept connections" {
		t.Errorf("compose healthchecks = %+v", cfg.DockerCompose.Healthchecks)
	}
}

// A healthcheck says one thing to check, and says it validly. Refused at load,
// before anything starts.
func TestHealthcheck_ShapeIsValidated(t *testing.T) {
	for _, tc := range []struct {
		name, healthcheck, want string
	}{
		{"none set", "timeout: 30s", "exactly one of port, http or log"},
		{"two set", "port: 80\n      log: up", "exactly one of port, http or log"},
		{"port out of range", "port: 70000", "port"},
		{"http not a URL", "http: localhost:8000/health", "http"},
		{"http wrong scheme", "http: ftp://localhost/x", "http"},
		{"bad timeout", "port: 80\n      timeout: soon", "timeout"},
		{"non-positive timeout", "port: 80\n      timeout: 0s", "timeout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadYAML(t, `
processes:
  - name: backend
    command: run
    healthcheck:
      `+tc.healthcheck+"\n")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want one mentioning %q", err, tc.want)
			}
		})
	}
}

func TestHealthcheck_DefaultTimeout(t *testing.T) {
	// 60s agreed in planning (plans/2026-10-06-depends-on-and-readiness.md).
	if got := (&HealthcheckConfig{Port: 80}).GetTimeout(); got != 60*time.Second {
		t.Errorf("default timeout = %v, want 60s", got)
	}
}

// Every way a dependency chain can fail to resolve is refused before anything
// starts, with an error naming the process and the problem.
func TestValidateDependencies_Refusals(t *testing.T) {
	compose := ComposeServices{
		Active:          map[string]bool{"db": true, "redis": true, "api": true},
		GatedOut:        map[string]bool{"debug-ui": true},
		WithHealthcheck: map[string]bool{"db": true},
	}
	for _, tc := range []struct {
		name, yaml, want string
	}{
		{"unknown name", `
processes:
  - {name: web, command: run, depends_on: [nope]}`, `web depends on "nope", which is neither a process nor a Compose service`},
		{"depends on itself", `
processes:
  - {name: web, command: run, depends_on: [web], healthcheck: {port: 80}}`, `web depends on itself`},
		{"listed twice", `
processes:
  - {name: a, command: run, healthcheck: {port: 80}}
  - {name: web, command: run, depends_on: [a, a]}`, `web lists "a" in depends_on more than once`},
		{"cycle", `
processes:
  - {name: a, command: run, depends_on: [b], healthcheck: {port: 80}}
  - {name: b, command: run, depends_on: [c], healthcheck: {port: 81}}
  - {name: c, command: run, depends_on: [a], healthcheck: {port: 82}}`, `dependency cycle: a -> b -> c -> a`},
		{"depends on a recurring process", `
processes:
  - {name: cron, command: run, interval: 1m}
  - {name: web, command: run, depends_on: [cron]}`, `web depends on "cron", a recurring process`},
		{"healthcheck on a recurring process", `
processes:
  - {name: cron, command: run, interval: 1m, healthcheck: {port: 80}}`, `cron is a recurring process`},
		{"process without healthcheck", `
processes:
  - {name: backend, command: run}
  - {name: web, command: run, depends_on: [backend]}`, `web depends on "backend", which has no healthcheck`},
		{"service without healthcheck", `
processes:
  - {name: web, command: run, depends_on: [redis]}`, `web depends on Compose service "redis", which has no healthcheck`},
		{"profile-gated service", `
processes:
  - {name: web, command: run, depends_on: [debug-ui]}`, `web depends on Compose service "debug-ui", which no active profile starts`},
		{"process and service share the name", `
processes:
  - {name: api, command: run, healthcheck: {port: 80}}
  - {name: web, command: run, depends_on: [api]}`, `"api" is both a process and a Compose service`},
		{"healthcheck declared twice", `
processes:
  - {name: web, command: run, depends_on: [db]}
docker_compose:
  files: [docker-compose.yml]
  healthchecks:
    db: {port: 5432}`, `Compose service "db" already has a healthcheck in its Compose file`},
		{"healthcheck for a service that does not exist", `
processes:
  - {name: web, command: run}
docker_compose:
  files: [docker-compose.yml]
  healthchecks:
    nosuch: {port: 5432}`, `docker_compose.healthchecks names "nosuch", which is not a Compose service`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := loadYAML(t, tc.yaml)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			err = cfg.ValidateDependencies(compose)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v\nwant one containing: %s", err, tc.want)
			}
		})
	}
}

// Valid graphs pass: a process dependency with a healthcheck, a Compose
// service with its own healthcheck, and one given a healthcheck here.
func TestValidateDependencies_Accepts(t *testing.T) {
	compose := ComposeServices{
		Active:          map[string]bool{"db": true, "redis": true},
		WithHealthcheck: map[string]bool{"db": true},
	}
	cfg, err := loadYAML(t, `
processes:
  - name: backend
    command: run
    depends_on: [db, redis]
    healthcheck: {port: 8000}
  - name: frontend
    command: run
    depends_on: [backend]
  - name: cron
    command: run
    interval: 1m
    depends_on: [backend]
docker_compose:
  files: [docker-compose.yml]
  healthchecks:
    redis: {port: 6379}
`)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := cfg.ValidateDependencies(compose); err != nil {
		t.Errorf("valid graph refused: %v", err)
	}
}

// Every problem is reported at once, not just the first: fixing a config one
// error per run is slow.
func TestValidateDependencies_ReportsEveryProblem(t *testing.T) {
	cfg, err := loadYAML(t, `
processes:
  - {name: a, command: run, depends_on: [nope]}
  - {name: b, command: run, depends_on: [also-nope]}`)
	if err != nil {
		t.Fatal(err)
	}
	err = cfg.ValidateDependencies(ComposeServices{})
	if err == nil || !strings.Contains(err.Error(), `"nope"`) || !strings.Contains(err.Error(), `"also-nope"`) {
		t.Errorf("error = %v, want both problems", err)
	}
}

// The dependency keys reach the process manager.
func TestToProcessConfigs_CarriesDependencies(t *testing.T) {
	cfg, err := loadYAML(t, `
processes:
  - name: backend
    command: run
    healthcheck: {http: "http://localhost:8000/health", timeout: 5s}
  - name: frontend
    command: run
    depends_on: [backend]
`)
	if err != nil {
		t.Fatal(err)
	}
	pcs := cfg.ToProcessConfigs()
	hc := pcs[0].Healthcheck
	if hc == nil || hc.HTTP != "http://localhost:8000/health" || hc.Timeout != 5*time.Second {
		t.Errorf("backend healthcheck = %+v", hc)
	}
	if pcs[1].Healthcheck != nil || len(pcs[1].DependsOn) != 1 || pcs[1].DependsOn[0] != "backend" {
		t.Errorf("frontend = %+v", pcs[1])
	}
}
