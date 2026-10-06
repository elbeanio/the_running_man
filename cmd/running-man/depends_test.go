package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/elbeanio/the_running_man/internal/docker"
)

// The summary dependency validation works from: which services the active
// profiles start, which they leave out, and which have a Compose healthcheck.
func TestComposeServices_Summary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "docker-compose.yml")
	content := `
services:
  db:
    image: postgres
    healthcheck:
      test: ["CMD", "pg_isready"]
  redis:
    image: redis
  debug-ui:
    image: x
    profiles: [debug]
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	cf, err := docker.ParseComposeFiles([]string{path})
	if err != nil {
		t.Fatal(err)
	}

	svcs := composeServices(cf, nil)
	if !svcs.Active["db"] || !svcs.Active["redis"] || svcs.Active["debug-ui"] {
		t.Errorf("active = %v, want db and redis", svcs.Active)
	}
	if !svcs.GatedOut["debug-ui"] {
		t.Errorf("gated out = %v, want debug-ui", svcs.GatedOut)
	}
	if !svcs.WithHealthcheck["db"] || svcs.WithHealthcheck["redis"] {
		t.Errorf("with healthcheck = %v, want db only", svcs.WithHealthcheck)
	}

	if svcs := composeServices(cf, []string{"debug"}); !svcs.Active["debug-ui"] || svcs.GatedOut["debug-ui"] {
		t.Errorf("with the debug profile active, debug-ui should be active: %+v", svcs)
	}
}
