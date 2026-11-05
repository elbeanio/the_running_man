//go:build podman_integration
// +build podman_integration

// Live-socket tests. Tagged so plain `make test` skips them on a host
// without a podman setup; run with
// `go test -tags podman_integration ./internal/runtime`.

package runtime

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// setUpPodmanShim writes a shell shim into a temp dir and prepends it
// to PATH so subsequent Podman construction discovers the supplied
// socket path via `podman system info`.
//
// Without the shim, every Podman method would either shell out to the
// real podman CLI (testing only what is on the test host) or fail
// closed. The shim lets us exercise the HTTP path through the unexported
// daemon in any environment.
func setUpPodmanShim(t *testing.T, socketPath string) {
	t.Helper()
	shimDir := t.TempDir()
	shimPath := shimDir + "/podman"
	body := "#!/bin/sh\necho '" + socketPath + "'\n"
	if err := os.WriteFile(shimPath, []byte(body), 0o755); err != nil {
		t.Fatalf("shim: %v", err)
	}
	t.Setenv("PATH", shimDir+":"+os.Getenv("PATH"))
}

func TestPodman_Endpoint_RealSocket(t *testing.T) {
	if !podmanCLIOnPath() {
		t.Skip("podman CLI not on PATH")
	}
	p := NewPodman()
	got := p.Endpoint()
	if got == "" {
		t.Fatal("Endpoint() empty; podman system info presumably failed")
	}
	if !strings.HasPrefix(got, "unix://") {
		t.Fatalf("Endpoint() = %q, want unix:// prefix", got)
	}
}

// TestPodman_Ping_FakeServer asserts Ping returns nil against an
// httptest unix server that answers 200 OK on /_ping, and surfaces
// errors when the path is wrong or the status is not 2xx.
//
// Ping is the first call the supervisor makes before allocating
// resources; a regression here means the supervisor mistakes an
// unavailable runtime for a Misconfigured yaml.
func TestPodman_Ping_FakeServer(t *testing.T) {
	var hits int32
	srv := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if r.URL.Path != "/_ping" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	path, stop := newPodmanSocketServer(t, srv)
	defer stop()
	setUpPodmanShim(t, path)

	p := NewPodman()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := p.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if atomic.LoadInt32(&hits) == 0 {
		t.Fatal("fake server recorded 0 hits; the ping went somewhere else")
	}
}

// TestPodman_Ping_404SurfacesAsError asserts a non-2xx response is
// surfaced to the caller. We want Ping to fail closed so the
// supervisor refuses to start, not to silently treat an HTTP 4xx as
// "running".
func TestPodman_Ping_404SurfacesAsError(t *testing.T) {
	srv := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	path, stop := newPodmanSocketServer(t, srv)
	defer stop()
	setUpPodmanShim(t, path)

	p := NewPodman()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := p.Ping(ctx); err == nil {
		t.Fatal("Ping returned nil on a 404; want error")
	}
}

func TestPodman_ListAndInspect_AgainstFakeserver(t *testing.T) {
	srv := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.40/containers/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"Id":"abc123","Names":["/test"],"Image":"alpine","State":"running","Created":1,"Labels":{}}]`))
		case "/v1.40/containers/abc123/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Id":"abc123","Name":"/test","Image":"alpine","Created":"2025","State":{"Status":"running","ExitCode":0},"Config":{"Labels":{}}}`))
		default:
			http.NotFound(w, r)
		}
	})
	path, stop := newPodmanSocketServer(t, srv)
	defer stop()
	setUpPodmanShim(t, path)

	p := NewPodman()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	got, err := p.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].Name != "test" {
		t.Fatalf("List returned %+v", got)
	}

	c, err := p.Inspect(ctx, "abc123")
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if c.State != "running" || c.Image != "alpine" {
		t.Fatalf("Inspect returned %+v", c)
	}
}

// TestPodman_DiscoverInProject_FakeServer asserts the supervisor's
// project-filter helper against a fake server with three containers
// in two projects. Only containers whose ProjectName == project AND
// whose ServiceName is in the wanted list should be returned.
func TestPodman_DiscoverInProject_FakeServer(t *testing.T) {
	srv := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1.40/containers/json" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"Id":"aaa111","Names":["/proj-api-1"],"Image":"alpine","State":"running","Labels":{"com.docker.compose.project":"proj","com.docker.compose.service":"api"}},
			{"Id":"bbb222","Names":["/proj-db-1"],"Image":"alpine","State":"running","Labels":{"com.docker.compose.project":"proj","com.docker.compose.service":"db"}},
			{"Id":"ccc333","Names":["/other-api-1"],"Image":"alpine","State":"running","Labels":{"com.docker.compose.project":"other","com.docker.compose.service":"api"}}
		]`))
	})
	path, stop := newPodmanSocketServer(t, srv)
	defer stop()
	setUpPodmanShim(t, path)

	p := NewPodman()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	got, err := p.DiscoverContainersInProject(ctx, "proj", []string{"api", "db"})
	if err != nil {
		t.Fatalf("DiscoverContainersInProject: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("found %d containers in project proj, want 2: %+v", len(got), got)
	}
	names := map[string]bool{}
	for _, c := range got {
		names[c.Name] = true
	}
	if !names["proj-api-1"] || !names["proj-db-1"] {
		t.Fatalf("expected proj-api-1 and proj-db-1, got %+v", got)
	}
	if names["other-api-1"] {
		t.Fatalf("did not filter out other project: %+v", got)
	}

	_, err = p.DiscoverContainersInProject(ctx, "proj", []string{"missing"})
	if err != nil {
		t.Fatalf("DiscoverContainersInProject should not error on services with no containers; got %v", err)
	}
}

// TestPodman_CompletedServices_FakeServer asserts every listed
// container is inspected to read its exit code, and only the
// State=exited + ExitCode=0 rows are reported as done.
//
// This is the supervisor's "find one-shot services" path: a
// migration job that ran successfully should not be reported as
// missing in the next round of discovery.
//
// Compose labels are mirrored into the inspect response because the
// supervisor reads ServiceName from the inspect response (not from
// the list summary) when filling the done map; without them every
// candidate is rejected.
func TestPodman_CompletedServices_FakeServer(t *testing.T) {
	inspectCalled := map[string]bool{}
	srv := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.40/containers/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{"Id":"c-done","Names":["/proj-seed-1"],"Image":"alpine","State":"exited","Labels":{"com.docker.compose.project":"proj","com.docker.compose.service":"seed"}},
				{"Id":"c-fail","Names":["/proj-fail-1"],"Image":"alpine","State":"exited","Labels":{"com.docker.compose.project":"proj","com.docker.compose.service":"fail"}},
				{"Id":"c-up","Names":["/proj-api-1"],"Image":"alpine","State":"running","Labels":{"com.docker.compose.project":"proj","com.docker.compose.service":"api"}}
			]`))
		case "/v1.40/containers/c-done/json":
			inspectCalled["c-done"] = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Id":"c-done","Name":"/proj-seed-1","Image":"alpine","State":{"Status":"exited","ExitCode":0},"Config":{"Labels":{"com.docker.compose.project":"proj","com.docker.compose.service":"seed"}}}`))
		case "/v1.40/containers/c-fail/json":
			inspectCalled["c-fail"] = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Id":"c-fail","Name":"/proj-fail-1","Image":"alpine","State":{"Status":"exited","ExitCode":1},"Config":{"Labels":{"com.docker.compose.project":"proj","com.docker.compose.service":"fail"}}}`))
		case "/v1.40/containers/c-up/json":
			inspectCalled["c-up"] = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Id":"c-up","Name":"/proj-api-1","Image":"alpine","State":{"Status":"running","ExitCode":0},"Config":{"Labels":{"com.docker.compose.project":"proj","com.docker.compose.service":"api"}}}`))
		default:
			http.NotFound(w, r)
		}
	})
	path, stop := newPodmanSocketServer(t, srv)
	defer stop()
	setUpPodmanShim(t, path)

	p := NewPodman()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	done, err := p.CompletedServices(ctx, "proj")
	if err != nil {
		t.Fatalf("CompletedServices: %v", err)
	}
	if !done["seed"] {
		t.Errorf("expected seed in done, got %+v", done)
	}
	if done["fail"] {
		t.Errorf("fail had exit 1, must not be done; got %+v", done)
	}
	if done["api"] {
		t.Errorf("api is running, not stopped; got %+v", done)
	}
	if !inspectCalled["c-done"] || !inspectCalled["c-fail"] || !inspectCalled["c-up"] {
		t.Errorf("expected three inspect calls, got %+v", inspectCalled)
	}
}

// TestPodman_WatchEvents_FakeServer asserts WatchEvents delivers one
// ContainerEvent per NDJSON record on /v1.40/events, with the slim
// bridge fields (Type, ContainerID) populated. Service-aware
// filtering by `com.docker.compose.project=` is delegated to the
// runtime's Events URL construction; the fake server is tolerant of
// any filter shape that's URL-decodable.
func TestPodman_WatchEvents_FakeServer(t *testing.T) {
	srv := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v1.40/events") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(
			"{\"Type\":\"container\",\"Action\":\"start\",\"Actor\":{\"ID\":\"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\"},\"time\":1700000000}\n",
		))
	})
	path, stop := newPodmanSocketServer(t, srv)
	defer stop()
	setUpPodmanShim(t, path)

	p := NewPodman()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	got := make(chan ContainerEvent, 1)
	go func() {
		_ = p.WatchEvents(ctx, "proj", func(e ContainerEvent) {
			got <- e
		})
		close(got)
	}()
	select {
	case e := <-got:
		if e.Type != "start" {
			t.Errorf("event Type = %q, want start", e.Type)
		}
		if e.ContainerID != "0123456789ab" {
			t.Errorf("event ContainerID = %q, want 0123456789ab", e.ContainerID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no event delivered within 3s")
	}
	<-got // wait for the goroutine to drain; otherwise its defer'd close
	// races with the channel close introduced by Podman's Events method.
}

// TestPodman_WaitServiceLog_FakeServer asserts WaitServiceLog opens
// the multiplexed log stream, demuxes frames through the matcher,
// and resolves when the marker "READY" arrives.
//
// The handler returns a stream of three frames: a stdout line that
// does not contain READY; the marker line itself; and a trailing
// stderr line.
func TestPodman_WaitServiceLog_FakeServer(t *testing.T) {
	listResp := `[
		{"Id":"abc","Names":["/proj-api-1"],"Image":"alpine","State":"running","Labels":{"com.docker.compose.project":"proj","com.docker.compose.service":"api"}}
	]`
	srv := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.40/containers/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(listResp))
		case "/v1.40/containers/abc/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Id":"abc","Name":"/proj-api-1","Image":"alpine","State":{"Status":"running","ExitCode":0},"Config":{"Labels":{}}}`))
		default:
			if !strings.HasPrefix(r.URL.Path, "/v1.40/containers/abc/logs") {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/vnd.docker.raw-stream")
			// Three frames: noise, marker, trailing noise.
			w.Write(multiplexFrame(1, []byte("booting up\n")))
			w.Write(multiplexFrame(1, []byte("READY api\n")))
			w.Write(multiplexFrame(2, []byte("warning: deprecated\n")))
		}
	})
	path, stop := newPodmanSocketServer(t, srv)
	defer stop()
	setUpPodmanShim(t, path)

	p := NewPodman()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := p.WaitServiceLog(ctx, "proj", "api", "READY")
	if err != nil {
		t.Fatalf("WaitServiceLog: %v", err)
	}
}

// TestPodman_WaitServiceLog_NoMatchSurfacesAsError asserts the
// method does not hang on a stream that never contains the marker
// and surfaces an error to the caller when ctx times out.
//
// Podman-side framing keeps emitting frames here; the supervisor sees
// an internal deadline. Today the implementation returns
// fmt.Errorf("podman waitlog: timeout..."). Pin the error shape.
func TestPodman_WaitServiceLog_NoMatchSurfacesAsError(t *testing.T) {
	listResp := `[
		{"Id":"abc","Names":["/proj-api-1"],"Image":"alpine","State":"running","Labels":{"com.docker.compose.project":"proj","com.docker.compose.service":"api"}}
	]`
	srv := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.40/containers/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(listResp))
		case "/v1.40/containers/abc/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Id":"abc","Name":"/proj-api-1","Image":"alpine","State":{"Status":"running","ExitCode":0},"Config":{"Labels":{}}}`))
		default:
			w.Header().Set("Content-Type", "application/vnd.docker.raw-stream")
			w.Write(multiplexFrame(1, []byte("non-matching line\n")))
		}
	})
	path, stop := newPodmanSocketServer(t, srv)
	defer stop()
	setUpPodmanShim(t, path)

	p := NewPodman()
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	err := p.WaitServiceLog(ctx, "proj", "api", "NEVER_MATCHES")
	if err == nil {
		t.Fatal("WaitServiceLog returned nil despite no match; want timeout error")
	}
}

// multiplexFrame returns the 8-byte header + payload that Docker and
// podman use to encode stdout/stderr in a multiplexed log stream.
// stream=1 is stdout, stream=2 is stderr. The func lives next to the
// tests that need it.
func multiplexFrame(stream byte, payload []byte) []byte {
	hdr := []byte{stream, 0, 0, 0, 0, 0, 0, byte(len(payload))}
	return append(hdr, payload...)
}

func TestPodman_Logs_FollowStream(t *testing.T) {
	srv := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v1.40/containers/") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.docker.raw-stream")
		// 8-byte header: stream_type=1 (stdout), 0, 0, 0, size=4
		_, _ = w.Write([]byte{1, 0, 0, 0, 0, 0, 0, 4})
		_, _ = w.Write([]byte("hi"))
	})
	path, stop := newPodmanSocketServer(t, srv)
	defer stop()
	setUpPodmanShim(t, path)

	p := NewPodman()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	body_rr, err := p.Logs(ctx, "abc", LogOpts{Follow: true})
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	defer body_rr.Close()
	buf := make([]byte, 1024)
	n, _ := body_rr.Read(buf)
	if n < 8 {
		t.Fatalf("Logs read %d bytes, want >=8", n)
	}
}

// newPodmanSocketServer serves an arbitrary http.Handler on a fresh
// unix-domain socket. Returned socket path can be passed to the podman
// discovery shim below.
func newPodmanSocketServer(t *testing.T, h http.Handler) (string, func()) {
	t.Helper()
	f, err := os.CreateTemp("", "rm-podman-test-*.sock")
	if err != nil {
		t.Fatalf("create sock file: %v", err)
	}
	path := f.Name()
	_ = f.Close()
	_ = os.Remove(path)

	lis, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	srv := httptest.NewUnstartedServer(h)
	srv.Listener.Close()
	srv.Listener = lis
	srv.Start()

	return path, func() {
		srv.Close()
		_ = os.Remove(path)
	}
}
