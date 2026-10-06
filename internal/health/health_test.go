package health

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// A short poll interval keeps the tests fast; production uses PollInterval.
func init() { pollInterval = 10 * time.Millisecond }

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

func TestPort_PassesWhenListening(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := Port(ctx, ln.Addr().(*net.TCPAddr).Port); err != nil {
		t.Errorf("Port = %v, want pass", err)
	}
}

// The common case: the check starts before the service has bound.
func TestPort_PassesOnceTheServiceBinds(t *testing.T) {
	port := freePort(t)
	go func() {
		time.Sleep(50 * time.Millisecond)
		ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
		if err == nil {
			time.Sleep(time.Second)
			ln.Close()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := Port(ctx, port); err != nil {
		t.Errorf("Port = %v, want pass after the listener appeared", err)
	}
}

func TestPort_TimesOut(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := Port(ctx, freePort(t)); err == nil {
		t.Error("Port passed with nothing listening")
	}
}

func TestHTTP_PassesOn2xx(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Not ready for the first two polls.
		if calls.Add(1) <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := HTTP(ctx, srv.URL); err != nil {
		t.Errorf("HTTP = %v, want pass once it answered 204", err)
	}
}

// A redirect is not readiness: a login page redirecting everything would
// otherwise pass, and a 3xx is not what was asked for.
func TestHTTP_RedirectDoesNotPass(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			http.Redirect(w, r, "/elsewhere", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := HTTP(ctx, srv.URL+"/health"); err == nil {
		t.Error("HTTP passed on a redirect")
	}
}

func TestHTTP_TimesOutWithNothingListening(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := HTTP(ctx, "http://127.0.0.1:"+strconv.Itoa(freePort(t))+"/"); err == nil {
		t.Error("HTTP passed with nothing listening")
	}
}

// The log check passes on a substring of a line, case-sensitively, and stays
// passed.
func TestLineMatcher(t *testing.T) {
	m := NewLineMatcher("Application startup complete")

	m.Feed("INFO: application startup complete") // wrong case
	m.Feed("INFO: Waiting for application startup")
	select {
	case <-m.Matched():
		t.Fatal("matched a line that does not contain the text")
	default:
	}

	m.Feed("INFO: Application startup complete.")
	m.Feed("INFO: Application startup complete.") // a second match is harmless
	select {
	case <-m.Matched():
	default:
		t.Fatal("did not match a line containing the text")
	}
}

func TestLineMatcher_Wait(t *testing.T) {
	m := NewLineMatcher("ready")
	go func() {
		time.Sleep(20 * time.Millisecond)
		m.Feed("server ready")
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := m.Wait(ctx); err != nil {
		t.Errorf("Wait = %v, want pass", err)
	}

	never := NewLineMatcher("ready")
	ctx2, cancel2 := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel2()
	if err := never.Wait(ctx2); err == nil {
		t.Error("Wait passed with no matching line")
	}
}
