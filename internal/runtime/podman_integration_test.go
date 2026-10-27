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
	"testing"
	"time"
)

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

	// Substitute the discovery shim by writing a fake `podman` shim into a
	// temporary PATH. The shim prints the socket path; the rest of the
	// podman tooling picks it up unchanged.
	shimDir := t.TempDir()
	shimPath := shimDir + "/podman"
	body := "#!/bin/sh\necho '" + path + "'\n"
	if err := os.WriteFile(shimPath, []byte(body), 0o755); err != nil {
		t.Fatalf("shim: %v", err)
	}
	t.Setenv("PATH", shimDir+":"+os.Getenv("PATH"))

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

	shimDir := t.TempDir()
	shimPath := shimDir + "/podman"
	body := "#!/bin/sh\necho '" + path + "'\n"
	if err := os.WriteFile(shimPath, []byte(body), 0o755); err != nil {
		t.Fatalf("shim: %v", err)
	}
	t.Setenv("PATH", shimDir+":"+os.Getenv("PATH"))

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
