package runtime

import (
	"context"
	"encoding/json"
	"os/exec"
	"testing"
)

// Compile-time check: a freshly constructed *Podman must satisfy the
// Runtime interface. Repeating this assertion in the test file guards
// against accidental signature drift between interface and implementation.
// The same assertion lives at the bottom of podman.go; both compile
// independently so an interface breakage fails two checks at once.
var _ Runtime = (*Podman)(nil)

// --- Unit tests: JSON fixture -> runtime.Container. -----------------------

func TestPodmanSummaryToContainer(t *testing.T) {
	raw := []byte(`{
		"Id": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"Names": ["/signsite-db"],
		"Image": "docker.io/library/postgres:16",
		"State": "running",
		"Created": 1700000000,
		"Labels": {
			"com.docker.compose.project": "signsite",
			"com.docker.compose.service": "db"
		}
	}`)
	var s podmanContainerSummary
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := podmanSummaryToContainer(s)
	want := Container{
		ID:          "0123456789ab",
		Name:        "signsite-db",
		ServiceName: "db",
		ProjectName: "signsite",
		Image:       "docker.io/library/postgres:16",
		State:       "running",
		Created:     1700000000,
	}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestPodmanInspectToContainer(t *testing.T) {
	raw := []byte(`{
		"Id": "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		"Name": "/signsite-web",
		"Image": "ghcr.io/anomalyco/signsite:latest",
		"Created": "2025-09-30T12:00:00Z",
		"State": {
			"Status": "exited",
			"ExitCode": 137
		},
		"Config": {
			"Labels": {
				"com.docker.compose.project": "signsite",
				"com.docker.compose.service": "web"
			}
		}
	}`)
	var inf podmanContainerInspect
	if err := json.Unmarshal(raw, &inf); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := podmanInspectToContainer(inf)
	want := Container{
		ID:          "abcdef012345",
		Name:        "signsite-web",
		ServiceName: "web",
		ProjectName: "signsite",
		Image:       "ghcr.io/anomalyco/signsite:latest",
		State:       "exited",
		Exit:        137,
	}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestShortID(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"short", "short"},
		{"0123456789abcdef0123", "0123456789ab"},
	}
	for _, c := range cases {
		got := shortID(c.in)
		if got != c.want {
			t.Errorf("shortID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// --- Tests of the compose-CLI discovery path. ------------------------------

func TestPodmanFindComposeCommand_MissingBinary(t *testing.T) {
	t.Setenv("PATH", "/this/path/should/never/exist")
	if _, err := podmanFindComposeCommand(context.Background()); err == nil {
		t.Fatal("expected an error when neither podman nor podman-compose is on PATH")
	}
}

// podmanCLIOnPath reports whether podman is on the test PATH. Used by
// integration tests (which live in podman_integration_test.go) to skip
// themselves when running on a host without podman. Unit tests do not
// need the CLI on PATH; we expose the helper here, the gated tests
// below in a sibling file.
func podmanCLIOnPath() bool {
	_, err := exec.LookPath("podman")
	return err == nil
}
