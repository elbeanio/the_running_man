package runtime

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
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

// --- Unit tests for the dispatch layer added in this branch. --------------
//
// These cover the parts of Podman that are pure Go and do not require
// a live podman socket:
//   - containerEventFromWire — the slim wire -> ContainerEvent bridge
//   - podmanMatchState — the line matcher used by WaitServiceLog
//   - Podman.Close — the idempotent no-op the dispatch relies on
//
// Gated integration tests live in podman_integration_test.go.

// TestContainerEventFromWire_ActionAndID pins the slim shape WatchEvents
// delivers today: Action + 12-char ContainerID, with ServiceName /
// Name / Image empty until the wire shape of Event grows. A future commit
// that adds attribute surface will change this test deliberately; do
// not "fix" it without first checking the plan to widen Event.
func TestContainerEventFromWire_ActionAndID(t *testing.T) {
	raw := Event{
		Action: "start",
		Actor:  "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Time:   1700000000,
	}
	got := (&Podman{}).containerEventFromWire(raw, "proj")
	want := ContainerEvent{Type: "start", ContainerID: "0123456789ab"}
	if got != want {
		t.Errorf("got %+v want %+v", got, want)
	}
}

// TestContainerEventFromWire_DieAction ensures actions other than start
// map through unchanged. Restart / die / stop / kill all funnel into the
// same ContainerEvent.Type field; the supervisor reads Type == "start"
// for attach and any non-start for stop-side cleanup, so the field
// must round-trip.
func TestContainerEventFromWire_DieAction(t *testing.T) {
	for _, action := range []string{"die", "stop", "kill", "restart", "create", "destroy"} {
		got := (&Podman{}).containerEventFromWire(Event{Action: action}, "proj")
		if got.Type != action {
			t.Errorf("action %q delivered as Type %q", action, got.Type)
		}
	}
}

// TestPodmanMatchState_FeedLines_FindsMarkerAcrossFrames pins the
// matcher's split-line tolerance. The docker-side lineWriter buffers
// partial lines and emits them only when the newline arrives; the
// podman-side WaitServiceLog path uses podmanMatchState for the same
// guarantee, but the wire comes in fixed-size frames. We feed a marker
// split across two buffers and expect the matcher to flip.
func TestPodmanMatchState_FeedLines_FindsMarkerAcrossFrames(t *testing.T) {
	m := newPodmanLineMatcher("READY")
	if m.matched() {
		t.Fatal("matches() already true before any bytes are fed")
	}
	m.FeedLines([]byte("hello\npartia"))
	if m.matched() {
		t.Fatal("matches() flipped on a partial line that does not yet contain the marker")
	}
	m.FeedLines([]byte("l READY line\n"))
	if !m.matched() {
		t.Fatal("matches() never flipped despite the marker arriving intact across two buffers")
	}
	if err := m.result(); err != nil {
		t.Errorf("after a match, result() returned %v, want nil", err)
	}
}

// TestPodmanMatchState_FeedLines_DoesNotMatch splits a single buffer
// where the marker is absent and asserts the matcher does not over-fire.
func TestPodmanMatchState_FeedLines_DoesNotMatch(t *testing.T) {
	m := newPodmanLineMatcher("READY")
	m.FeedLines([]byte("never seen, busy waiting\n"))
	if m.matched() {
		t.Fatal("matches() flipped on a buffer that does not contain the marker")
	}
	if err := m.result(); err == nil {
		t.Fatal("result() returned nil for an unmatched matcher")
	}
}

// TestPodmanMatchState_FeedLines_MatchInFirstChunk covers the
// fast-path case: the marker is in the first buffer delivered.
func TestPodmanMatchState_FeedLines_MatchInFirstChunk(t *testing.T) {
	m := newPodmanLineMatcher("READY")
	m.FeedLines([]byte("READY\n"))
	if !m.matched() {
		t.Fatal("matches() did not flip despite the marker being in the first chunk")
	}
}

// TestPodman_Close_Idempotent verifies Close is safe to call twice.
// The podman adapter holds no resources beyond an http.Client built on
// demand, so Close is a documented no-op; the test pins it because the
// dispatch layer in cmd/running-man registers it via defer and a
// double-call panic would crash startup.
func TestPodman_Close_Idempotent(t *testing.T) {
	p := NewPodman()
	if err := p.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// TestPodman_Endpoint_StableAcrossCalls checks that the socket path
// is resolved once and surfaced consistently across repeated reads,
// so the TUI and the REST API see the same value. A real podman
// machine is optional — when none is reachable the discovery path
// produces an empty endpoint, which is the stable form.
//
// We also enforce the prefix contract: every non-empty Endpoint must
// start with `unix://` so a caller that logs the value can tell it
// apart from the docker-side `docker://` shape.
func TestPodman_Endpoint_StableAcrossCalls(t *testing.T) {
	p := NewPodman()
	first := p.Endpoint()
	second := p.Endpoint()
	if first != second {
		t.Errorf("Endpoint resolved differently across calls: %q vs %q", first, second)
	}
	if first != "" && !strings.HasPrefix(first, "unix://") {
		t.Errorf("Endpoint not a unix:// URL or empty: %q", first)
	}
}

// TestPodman_LogOpts_TailString fixtures pin the LogOpts.TailString
// contract: zero means "all" expressed on the wire as empty; non-zero
// becomes a digit string. Both docker and podman pass this through
// to the engine.
func TestPodman_LogOpts_TailString(t *testing.T) {
	cases := []struct {
		tail int
		want string
	}{
		{0, ""},
		{1, "1"},
		{200, "200"},
	}
	for _, c := range cases {
		got := LogOpts{Tail: c.tail}.TailString()
		if got != c.want {
			t.Errorf("LogOpts{Tail: %d}.TailString() = %q, want %q", c.tail, got, c.want)
		}
	}
}

// TestPodman_ContainerTTY_RoundTripEmptyJSON ensures that an inspect
// response without a `Tty` field produces a Container with TTY=false
// rather than panicking on a missing JSON field. We do not have a
// happy-path test for TTY=true here; the docker-side dice already
// covers Tty=true via the moby code path.
func TestPodman_ContainerTTY_RoundTripEmptyJSON(t *testing.T) {
	raw := []byte(`{
		"Id": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"Name": "/signsite-web",
		"Image": "ghcr.io/anomalyco/signsite:latest",
		"State": {"Status": "exited", "ExitCode": 0},
		"Config": {"Labels": {}}
	}`)
	var inf podmanContainerInspect
	if err := json.Unmarshal(raw, &inf); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := podmanInspectToContainer(inf)
	if got.TTY {
		t.Error("TTY = true on a record missing the Tty field; want false")
	}
}

// TestPodman_ContainerTTY_RoundTripTrueJSON covers the side where
// podman reports the container is a TTY (Compose's `tty: true`).
func TestPodman_ContainerTTY_RoundTripTrueJSON(t *testing.T) {
	raw := []byte(`{
		"Id": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"Name": "/signsite-tty",
		"Image": "alpine",
		"State": {"Status": "running", "ExitCode": 0},
		"Config": {"Tty": true, "Labels": {}}
	}`)
	var inf podmanContainerInspect
	if err := json.Unmarshal(raw, &inf); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := podmanInspectToContainer(inf)
	if !got.TTY {
		t.Error("TTY = false on a record with the Tty field set; want true")
	}
}

// TestPodman_LongIDShortened covers that Container.ID is the 12-char
// prefix. Long IDs are returned by podman at full sha256 length; the
// supervisor code uses ID everywhere, so a future change that omits
// the prefix breaks the TUI's display.
func TestPodman_LongIDShortened(t *testing.T) {
	cs := []Container{{ID: shortID("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")}, {ID: shortID("short")}}
	if cs[0].ID != "0123456789ab" {
		t.Errorf("64-char id shortened to %q, want 0123456789ab", cs[0].ID)
	}
	if cs[1].ID != "short" {
		t.Errorf("5-char id preserved as %q, want short", cs[1].ID)
	}
}
