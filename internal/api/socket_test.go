package api

import (
	"encoding/json"
	"errors"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/elbeanio/the_running_man/internal/storage"
)

// staleSocket leaves a socket file behind with nothing listening on it, which is
// what a SIGKILLed instance leaves on disk. Go unlinks a Unix socket when its
// listener is closed, so SetUnlinkOnClose(false) is needed to reproduce it.
func staleSocket(t *testing.T, path string) {
	t.Helper()

	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("creating socket to orphan: %v", err)
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	if err := ln.Close(); err != nil {
		t.Fatalf("closing socket: %v", err)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("socket file should have survived close: %v", err)
	}
}

func TestListen_FreshPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "api.sock")

	ln, err := Listen(path)
	if err != nil {
		t.Fatalf("Listen on a fresh path: %v", err)
	}
	defer ln.Close()

	if _, err := os.Lstat(path); err != nil {
		t.Errorf("socket was not created: %v", err)
	}
}

// A socket file outlives the process that made it, and net.Listen refuses a path
// that already exists. Without takeover, one SIGKILL makes the project
// permanently unstartable until someone deletes the file by hand.
func TestListen_TakesOverStaleSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "api.sock")
	staleSocket(t, path)

	// The behaviour being worked around: a plain bind to the orphaned path fails.
	if ln, err := net.Listen("unix", path); err == nil {
		ln.Close()
		t.Fatal("net.Listen unexpectedly accepted a path that already exists; " +
			"this test no longer reproduces the condition takeover exists for")
	}

	ln, err := Listen(path)
	if err != nil {
		t.Fatalf("Listen should take over a stale socket: %v", err)
	}
	defer ln.Close()

	// And the taken-over socket must actually serve.
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("dialling the taken-over socket: %v", err)
	}
	conn.Close()
}

// The second-instance bug: starting beside a live instance must refuse rather
// than adopt, overwrite or delete anything belonging to the first.
func TestListen_RefusesLiveSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "api.sock")

	first, err := Listen(path)
	if err != nil {
		t.Fatalf("first Listen: %v", err)
	}
	defer first.Close()

	second, err := Listen(path)
	if err == nil {
		second.Close()
		t.Fatal("second Listen should have refused while the first is live")
	}
	if !errors.Is(err, ErrInstanceLive) {
		t.Errorf("err = %v, want ErrInstanceLive", err)
	}

	// The first instance's socket must still be serving afterwards -- a refusal
	// that breaks what it refused to replace would be worse than the bug.
	conn, dialErr := net.Dial("unix", path)
	if dialErr != nil {
		t.Fatalf("first instance's socket stopped serving after the refusal: %v", dialErr)
	}
	conn.Close()
}

// 0600 is the whole access-control story now that there is no IP check: another
// user on the machine must not be able to read this project's logs.
func TestListen_SocketIsOwnerOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "api.sock")

	ln, err := Listen(path)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("socket mode = %#o, want 0600", perm)
	}
}

// Identity on /health is what turns instance discovery from assumed into
// checked. A response proving only that *something* is alive is what let a
// second instance pass a health check that was really answered by the first.
func TestHealth_ReportsIdentity(t *testing.T) {
	buffer := storage.NewRingBuffer(10, time.Minute, 1024)
	server := NewServer(buffer, "/projects/duet", nil, nil, nil)

	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, req)

	var got map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decoding /health: %v", err)
	}

	if pid, ok := got["pid"].(float64); !ok || int(pid) != os.Getpid() {
		t.Errorf("pid = %v, want %d", got["pid"], os.Getpid())
	}
	if got["project"] != "/projects/duet" {
		t.Errorf("project = %v, want /projects/duet", got["project"])
	}
	if _, ok := got["started"].(string); !ok {
		t.Errorf("started = %v, want an RFC3339 timestamp", got["started"])
	}
}

// The Swagger page is gone (its SRI hashes never matched, so it never rendered)
// and the spec moved out from under the /docs path that no longer exists.
func TestOpenAPISpec_ServedAtRoot(t *testing.T) {
	buffer := storage.NewRingBuffer(10, time.Minute, 1024)
	server := NewServer(buffer, "/projects/duet", nil, nil, nil)
	handler := server.routes()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/openapi.yaml", nil))
	if rec.Code != 200 {
		t.Errorf("GET /openapi.yaml = %d, want 200", rec.Code)
	}

	for _, gone := range []string{"/docs", "/docs/openapi.yaml"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest("GET", gone, nil))
		if rec.Code == 200 {
			t.Errorf("GET %s = 200, want it gone", gone)
		}
	}
}
