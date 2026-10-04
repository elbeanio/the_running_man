package api

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/elbeanio/the_running_man/internal/storage"
)

// Closing the listener is how shutdown unlinks the socket, so http.Serve returns
// net.ErrClosed -- not the http.ErrServerClosed that only Server.Shutdown
// produces. Treating that as an error printed "API server error: accept unix
// ...: use of closed network connection" after every quit.
func TestServeTreatsAClosedListenerAsACleanStop(t *testing.T) {
	// A short directory on purpose: t.TempDir() embeds the test's name, and this
	// test's name alone pushed the socket path past sun_path -- the very limit
	// instance.SocketPath has a fallback for.
	dir, err := os.MkdirTemp("", "rm")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, "api.sock")
	ln, err := Listen(path)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	server := NewServer(storage.NewRingBuffer(10, time.Minute, 1024), dir, nil, nil, nil)

	served := make(chan error, 1)
	go func() { served <- server.Serve(ln) }()

	// Let Serve reach its accept loop, then shut down the way the real shutdown
	// path does.
	if conn, err := net.DialTimeout("unix", path, time.Second); err == nil {
		conn.Close()
	}
	if err := ln.Close(); err != nil {
		t.Fatalf("closing the listener: %v", err)
	}

	select {
	case err := <-served:
		if err != nil {
			t.Errorf("Serve reported an error on a clean shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after the listener was closed")
	}
}
