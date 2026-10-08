// Package health implements the healthchecks that gate startup: is a
// dependency ready yet?
//
// Each check blocks until it passes or its context ends. The caller sets the
// timeout on the context, and a check that ends with an error has not passed.
package health

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// PollInterval is how often the port and HTTP checks retry.
const PollInterval = 500 * time.Millisecond

// pollInterval is PollInterval, shortened by tests.
var pollInterval = PollInterval

// poll runs try until it succeeds or ctx ends, returning the last failure.
func poll(ctx context.Context, what string, try func(context.Context) error) error {
	var last error
	for {
		if last = try(ctx); last == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s: %w (last attempt: %v)", what, ctx.Err(), last)
		case <-time.After(pollInterval):
		}
	}
}

// portSettle is how long a connection must stay open, silent or talking, to
// count as a server rather than a forwarder hanging up.
const portSettle = 250 * time.Millisecond

// Port passes once a TCP connection to localhost:port succeeds and is not
// closed at once.
//
// A successful connect is not enough. Docker publishes a container's port by
// listening on the host itself, so a connect succeeds as soon as the container
// exists, and the forwarder then hangs up when it finds nothing listening
// inside. A connect-only check declared Denodo ready seven seconds before its
// server had begun to start. So the check reads briefly: a real server either
// waits for the client to speak (Postgres) or greets it (MySQL), and the
// forwarder closes the connection within milliseconds.
func Port(ctx context.Context, port int) error {
	addr := net.JoinHostPort("localhost", strconv.Itoa(port))
	var d net.Dialer
	return poll(ctx, "port "+strconv.Itoa(port), func(ctx context.Context) error {
		attempt, cancel := context.WithTimeout(ctx, pollInterval)
		defer cancel()
		conn, err := d.DialContext(attempt, "tcp", addr)
		if err != nil {
			return err
		}
		defer conn.Close()

		if err := conn.SetReadDeadline(time.Now().Add(portSettle)); err != nil {
			return err
		}
		n, err := conn.Read(make([]byte, 1))
		var netErr net.Error
		switch {
		case n > 0:
			return nil // it greeted us
		case errors.As(err, &netErr) && netErr.Timeout():
			return nil // it is waiting for us to speak
		default:
			return fmt.Errorf("connection closed at once, so nothing is listening behind it yet: %v", err)
		}
	})
}

// httpClient does not follow redirects: a 3xx is not the 2xx asked for, and a
// service redirecting everything to a login page would otherwise pass.
var httpClient = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// HTTP passes once a GET of url returns any 2xx.
func HTTP(ctx context.Context, url string) error {
	return poll(ctx, "http "+url, func(ctx context.Context) error {
		attempt, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(attempt, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			return err
		}
		resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return fmt.Errorf("status %d", resp.StatusCode)
		}
		return nil
	})
}

// LineMatcher is the log check: it passes once a line fed to it contains the
// text, case-sensitively.
//
// It is fed rather than reading a log itself, so it can be attached to a
// process's output before the process starts. A fast process prints its ready
// line almost at once, and a check that started reading afterwards would miss
// it.
type LineMatcher struct {
	text    string
	once    sync.Once
	matched chan struct{}
}

// NewLineMatcher returns a matcher for text.
func NewLineMatcher(text string) *LineMatcher {
	return &LineMatcher{text: text, matched: make(chan struct{})}
}

// Feed offers one line.
func (m *LineMatcher) Feed(line string) {
	if strings.Contains(line, m.text) {
		m.once.Do(func() { close(m.matched) })
	}
}

// Matched is closed once a line has matched.
func (m *LineMatcher) Matched() <-chan struct{} {
	return m.matched
}

// Wait passes once a line has matched.
func (m *LineMatcher) Wait(ctx context.Context) error {
	select {
	case <-m.matched:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("log %q: %w (no matching line)", m.text, ctx.Err())
	}
}
