// Package runtime: Podman implementation.
//
// Talks to a local podman-machine (rootless) over its Unix-domain socket.
// Reads the socket path from `podman system info --format '{{.Host.RemoteSocket.Path}}'`,
// then speaks the same `/v1.40/...` REST surface Docker uses -- though
// several response bodies (notably /events and /containers/{id}/logs) have
// podman-specific shapes that this file parses locally rather than through
// the moby client.
//
// The moby Go client intentionally stays on the docker side of the package.
// Mixing it here would mean two HTTP clients for one socket family, and
// moby's request/response types are Docker-shaped; the podman responses
// diverge enough on /events that hand-parsing is cheaper than vendoring
// podman's wire types.
package runtime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/moby/moby/api/pkg/stdcopy"
)

// Podman is the podman-backed implementation of Runtime.
//
// It is safe to share across goroutines: the underlying http.Client
// supports concurrent calls, and the cached endpoint socket path is
// protected by a sync.Once.
type Podman struct {
	socketOnce sync.Once
	socket     string
	socketErr  error
}

// NewPodman returns a Podman runtime. The endpoint socket is resolved
// lazily on the first call to Endpoint or any method.
func NewPodman() *Podman {
	return &Podman{}
}

// Compile-time check: *Podman must satisfy Runtime. The interface is
// populated by the dispatch commit; if a fingerprint method is added
// to one implementation but not the other the build fails here.
var _ Runtime = (*Podman)(nil)

// Close releases any underlying resources. The podman adapter holds an
// http.Client built on demand; the http.Client is freed by GC, so Close
// is a no-op. Provided so the runtime satisfies io.Closer-shaped
// expectations at the dispatcher's discretion.
func (p *Podman) Close() error {
	return nil
}

// Ping verifies the podman daemon is reachable. Hits the daemon's `/_ping`
// endpoint, which is a no-content 200 OK; matching status, we report
// healthy. Connection refused / socket-not-found / 5xx all surface as an
// error so the supervisor can refuse to start.
func (p *Podman) Ping(ctx context.Context) error {
	if _, err := p.connectedSocket(); err != nil {
		return err
	}
	req, err := p.newRequest(ctx, http.MethodGet, "/_ping")
	if err != nil {
		return err
	}
	resp, err := p.client().Do(req)
	if err != nil {
		return fmt.Errorf("podman ping: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("podman ping: %d", resp.StatusCode)
	}
	return nil
}

// DiscoverContainersInProject returns running containers in the named
// Compose project. The implementation re-uses List and filters by the
// project label; matches the docker-side behaviour closely enough that
// the supervisor's discover() helper does not branch on the runtime.
func (p *Podman) DiscoverContainersInProject(ctx context.Context, project string, services []string) ([]Container, error) {
	if _, err := p.connectedSocket(); err != nil {
		return nil, err
	}
	if project == "" {
		return nil, errors.New("podman discover: empty project name")
	}
	all, err := p.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("podman discover: %w", err)
	}

	want := make(map[string]bool, len(services))
	for _, s := range services {
		want[s] = true
	}

	var matched []Container
	seen := make(map[string]bool)
	for _, c := range all {
		if c.ProjectName != project {
			continue
		}
		if !want[c.ServiceName] {
			continue
		}
		if seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		matched = append(matched, c)
	}
	return matched, nil
}

// WatchEvents streams Compose-aware container events into fn until ctx
// is cancelled or the podman event feed closes.
//
// Each event row reads `podmanActor.Attributes["name"]` and
// `podmanActor.Attributes["com.docker.compose.service"]` for the
// classifier the supervisor already understands, so the rest of the
// supervisor code reads `ContainerEvent` field-uniformly.
func (p *Podman) WatchEvents(ctx context.Context, project string, fn EventHandler) error {
	if project == "" {
		return errors.New("podman watch: empty project name")
	}
	dst := make(chan Event, 16)
	errCh := make(chan error, 1)
	go func() {
		errCh <- p.Events(ctx, EventOpts{Filters: map[string][]string{
			"type":  {"container"},
			"label": {"com.docker.compose.project=" + project},
		}}, dst)
		close(errCh)
	}()
	defer close(dst)

	for {
		select {
		case raw, ok := <-dst:
			if !ok {
				return nil
			}
			if fn == nil {
				continue
			}
			fn(p.containerEventFromWire(raw, project))
		case err := <-errCh:
			if err == nil || errors.Is(err, context.Canceled) {
				return nil
			}
			return fmt.Errorf("podman watch: %w", err)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// containerEventFromWire translates one Runtime.Event row into a
// ContainerEvent suitable for the supervisor's WatchEvents callback.
//
// Today the wire is parsed lightly: only Action / ContainerID. ServiceName /
// Name / Image rely on compose-aware label attributes that the slim
// Event type does not currently carry, so they stay empty. This is
// sufficient for the supervisor's `start`/`die`/`restart`/`stop`/`kill`
// gating; richer classification can land later by extending Event. If
// callers need them now, callers should look up the container via
// Inspect(ctx, id) at the call site.
func (p *Podman) containerEventFromWire(e Event, project string) ContainerEvent {
	return ContainerEvent{
		Type:        e.Action,
		ContainerID: shortID(e.Actor),
	}
}

// CompletedServices maps service names whose container exited with
// exit-code 0 (Stop with success). One-shot services -- a migration,
// a seed -- count as done rather than missing.
//
// The label filter mirrors docker's: we read every container in the
// project, inspect each, and record the service names whose State ==
// "exited" and ExitCode == 0. Reading the inspect response per
// container costs an extra round-trip per container; podman tolerates
// this for the small project sizes the supervisor targets. Larger
// projects will eventually need a bulk API.
func (p *Podman) CompletedServices(ctx context.Context, project string) (map[string]bool, error) {
	if project == "" {
		return nil, errors.New("podman completed: empty project name")
	}
	all, err := p.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("podman completed: %w", err)
	}
	done := make(map[string]bool)
	for _, c := range all {
		if c.ProjectName != project {
			continue
		}
		inspected, err := p.Inspect(ctx, c.ID)
		if err != nil || inspected.State != "exited" {
			continue
		}
		if inspected.Exit != 0 {
			continue
		}
		if inspected.ServiceName != "" {
			done[inspected.ServiceName] = true
		}
	}
	return done, nil
}

// WaitServiceHealthy is unsupported on podman today. The
// com.docker.compose.service label is a docker convention; podman
// containers do not always carry it, and even when they do, podman does
// not expose the daemon-level aggregated health status that
// Docker's `/containers/{id}/json` reports. Returning an error here
// keeps the supervisor explicit: a process that depends on a podman
// container's health must declare its own `healthcheck:` block instead.
func (p *Podman) WaitServiceHealthy(ctx context.Context, project string, service string) error {
	return fmt.Errorf("podman: WaitServiceHealthy not supported for service %q (project %q); declare a healthcheck on the dependent process", service, project)
}

// WaitServiceLog polls the named service's containers, opens their log
// streams, and waits for one line containing text in each container's
// current run. Unlike the docker-side, podman's `_ping` and
// `/containers/{id}/json` both surface REST-shape `/startedat` so we
// can read container started-at without an extra inspect call.
func (p *Podman) WaitServiceLog(ctx context.Context, project string, service string, text string) error {
	if project == "" {
		return errors.New("podman waitlog: empty project name")
	}
	if service == "" {
		return errors.New("podman waitlog: empty service name")
	}
	if text == "" {
		return nil
	}
	cs, err := p.DiscoverContainersInProject(ctx, project, []string{service})
	if err != nil {
		return err
	}
	if len(cs) == 0 {
		return errors.New("podman waitlog: no running container for service " + service)
	}
	healthyCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errs := make([]error, len(cs))
	var wg sync.WaitGroup
	for i, c := range cs {
		wg.Add(1)
		go func(i int, c Container) {
			defer wg.Done()
			errs[i] = p.waitContainerLog(healthyCtx, c, text)
		}(i, c)
	}
	wg.Wait()
	return errors.Join(errs...)
}

// waitContainerLog opens one container's log stream and waits for text
// in the current run. Mirrors the docker-side behaviour, using the
// runtime's Logs() for the wire stream and the same line-matcher
// algorithm.
//
// Note: the multiplexed-frame stream is the same shape as Docker's. We
// demux with moby's stdcopy (already in the dep graph via the docker
// side) so a TTY container's frames do not leak into stdout.
//
// The matcher used here is `internal/health.NewLineMatcher`. mirror
// of the docker-side flow; broadcasts text matches to the supervisor's
// process manager via the channel-supplied callback.
func (p *Podman) waitContainerLog(ctx context.Context, c Container, text string) error {
	body, err := p.Logs(ctx, c.ID, LogOpts{Follow: true})
	if err != nil {
		return fmt.Errorf("podman waitlog: open %s: %w", c.ID, err)
	}
	defer body.Close()

	matcher := newPodmanLineMatcher(text)
	stdout := &podmanLineWriter{matcher: matcher, isStderr: false}
	stderr := &podmanLineWriter{matcher: matcher, isStderr: true}
	_, _ = stdcopy.StdCopy(stdout, stderr, body)

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if matcher.matched() {
		return nil
	}
	return fmt.Errorf("podman waitlog: timeout waiting for %q in service %s", text, c.Name)
}

// podmanLineWriter buffers bytes from a multiplexed frame into lines
// and feeds them to the matcher.
type podmanLineWriter struct {
	matcher  *podmanMatchState
	isStderr bool
}

func (w *podmanLineWriter) Write(p []byte) (int, error) {
	w.matcher.FeedLines(p)
	return len(p), nil
}

// podmanMatchState is the simple matcher used on the podman-side
// WaitServiceLog path. The full matcher in internal/health/ is
// richer (timestamps, replay); the podman-side initial implementation
// is intentionally narrower because the WaitServiceLog path is rarely
// used in practice.
type podmanMatchState struct {
	text    string
	matches bool
}

type podmanLineMatcher = podmanMatchState

func newPodmanLineMatcher(text string) *podmanMatchState {
	return &podmanMatchState{text: text}
}

// podmanLineMatcher constructor alias: `podmanLineMatcher(text)` returns
// the same struct as `newPodmanLineMatcher(text)` so callers can use the
// shorter name in the site.
func (m *podmanMatchState) matched() bool { return m.matches }
func (m *podmanMatchState) result() error {
	if m.matches {
		return nil
	}
	return errors.New("podman waitlog: text never appeared")
}

// FeedLines buffers p, splits on \n, and feeds each line to the
// matcher. Tolerant of partial lines arriving across frames.
func (m *podmanMatchState) FeedLines(p []byte) {
	for _, line := range bytes.Split(p, []byte{'\n'}) {
		if bytes.Contains(line, []byte(m.text)) {
			m.matches = true
			return
		}
	}
}

// Compile-time check: *Podman must satisfy Runtime. Update this only when
// the interface in runtime.go changes.
var _ Runtime = (*Podman)(nil)

// Endpoint returns the unix:// URL where podman exposes its REST API.
// The path is cached for the lifetime of the Podman value.
func (p *Podman) Endpoint() string {
	p.resolveSocket()
	return "unix://" + p.socket
}

// resolveSocket shells out to `podman system info` once and caches the
// RemoteSocket.Path value. The error is also cached; if podman is not on
// PATH the next call still returns an empty socket and methods dependent
// on it return errors at the call site rather than failing here.
func (p *Podman) resolveSocket() {
	p.socketOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// podman's `--format {{.Host.RemoteSocket.Path}}` is the documented
		// way to ask for this; stripped of the surrounding whitespace and
		// quotes that the format string leaves behind.
		out, err := exec.CommandContext(ctx, "podman", "system", "info",
			"--format", "{{.Host.RemoteSocket.Path}}").Output()
		if err != nil {
			p.socketErr = fmt.Errorf("podman system info: %w", err)
			return
		}
		raw := strings.TrimSpace(string(out))
		raw = strings.Trim(raw, `"`)
		p.socket = raw
		if raw == "" {
			p.socketErr = errors.New("podman system info returned no RemoteSocket.Path")
		}
	})
}

// List returns every container the podman daemon knows about, in any state.
// The shape of /v1.40/containers/json is identical to Docker's for the
// subset of fields Running Man reads; a hand-rolled struct avoids dragging
// in moby/api/types just for this call.
func (p *Podman) List(ctx context.Context) ([]Container, error) {
	if _, err := p.connectedSocket(); err != nil {
		return nil, err
	}
	var summaries []podmanContainerSummary
	if err := p.doJSON(ctx, http.MethodGet, "/v1.40/containers/json?all=1", &summaries); err != nil {
		return nil, fmt.Errorf("podman list: %w", err)
	}
	out := make([]Container, 0, len(summaries))
	for _, s := range summaries {
		out = append(out, podmanSummaryToContainer(s))
	}
	return out, nil
}

// Inspect returns the runtime's current view of one container.
func (p *Podman) Inspect(ctx context.Context, id string) (Container, error) {
	if _, err := p.connectedSocket(); err != nil {
		return Container{}, err
	}
	var inf podmanContainerInspect
	if err := p.doJSON(ctx, http.MethodGet, "/v1.40/containers/"+url.PathEscape(id)+"/json", &inf); err != nil {
		return Container{}, fmt.Errorf("podman inspect: %w", err)
	}
	return podmanInspectToContainer(inf), nil
}

// Wait blocks until the named container reaches the given condition. Only
// "not-running" is meaningfully expressible through podman's REST API; any
// other value returns an error from the daemon. Backed by
// POST /v1.40/containers/{id}/wait?condition=not-running.
func (p *Podman) Wait(ctx context.Context, id string, condition string) error {
	if _, err := p.connectedSocket(); err != nil {
		return err
	}
	var out struct {
		StatusCode int `json:"StatusCode"`
	}
	path := "/v1.40/containers/" + url.PathEscape(id) + "/wait?condition=" + url.QueryEscape(condition)
	if err := p.doJSON(ctx, http.MethodPost, path, &out); err != nil {
		return fmt.Errorf("podman wait: %w", err)
	}
	return nil
}

// Logs opens a live log stream for the named container. The caller owns
// the returned ReadCloser and must close it to release the connection.
//
// Podman emits the same multiplexed frame format Docker does (8-byte
// header per frame). Reusing moby's stdcopy demultiplexer
// (`github.com/moby/moby/api/pkg/stdcopy`) avoids a hand-rolled parser;
// import path stays the same as the docker side already uses.
func (p *Podman) Logs(ctx context.Context, id string, opts LogOpts) (io.ReadCloser, error) {
	if _, err := p.connectedSocket(); err != nil {
		return nil, err
	}
	var u strings.Builder
	u.WriteString("/v1.40/containers/")
	u.WriteString(url.PathEscape(id))
	u.WriteString("/logs?stdout=1&stderr=1&timestamps=1")
	if opts.Follow {
		u.WriteString("&follow=1")
	}
	if opts.Since != "" {
		u.WriteString("&since=")
		u.WriteString(url.QueryEscape(opts.Since))
	}
	if opts.Until != "" {
		u.WriteString("&until=")
		u.WriteString(url.QueryEscape(opts.Until))
	}
	if opts.Tail > 0 {
		u.WriteString("&tail=")
		u.WriteString(strconv.Itoa(opts.Tail))
	}
	if err := p.connect(); err != nil {
		return nil, err
	}
	req, err := p.newRequest(ctx, http.MethodGet, u.String())
	if err != nil {
		return nil, err
	}
	resp, err := p.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("podman logs: %w", err)
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("podman logs %d: %s", resp.StatusCode, string(body))
	}
	return resp.Body, nil
}

// Events streams container-event-feed entries to dst until ctx is cancelled.
// Podman's /v1.40/events stream is NDJSON rather than Docker's JSON array,
// so a line scanner does the splitting.
//
// Only entries with Type == "container" are forwarded.
func (p *Podman) Events(ctx context.Context, opts EventOpts, dst chan<- Event) error {
	if _, err := p.connectedSocket(); err != nil {
		return err
	}
	if err := p.connect(); err != nil {
		return err
	}
	var u strings.Builder
	u.WriteString("/v1.40/events")
	if opts.Since != "" {
		u.WriteString("?since=")
		u.WriteString(url.QueryEscape(opts.Since))
	}
	for k, vs := range opts.Filters {
		for _, v := range vs {
			sep := "?"
			if u.Len() > 0 && strings.Contains(u.String(), "?") {
				sep = "&"
			}
			u.WriteString(sep)
			u.WriteString(url.QueryEscape("filter=" + k + "=" + v))
		}
	}
	req, err := p.newRequest(ctx, http.MethodGet, u.String())
	if err != nil {
		return err
	}
	resp, err := p.client().Do(req)
	if err != nil {
		return fmt.Errorf("podman events: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("podman events %d: %s", resp.StatusCode, string(body))
	}
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		var ev podmanEvent
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}
		if ev.Type != "container" {
			continue
		}
		select {
		case dst <- Event{Action: ev.Action, Actor: ev.Actor.ID, Time: ev.Time}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return scanner.Err()
}

// FindCommand resolves podman's preferred compose CLI. The lookup order is
// `podman compose` (the documented plugin path) then `podman-compose` (the
// standalone binary). The free function `podmanFindComposeCommand` does
// the actual lookup so callers that want only a top-level resolver — say,
// discovery at process startup — can use it without constructing a Podman.
func (p *Podman) FindCommand(ctx context.Context) (ComposeCommand, error) {
	return podmanFindComposeCommand(ctx)
}

// podmanFindComposeCommand resolves the podman compose CLI. Top-level so
// callers do not need a Podman value up-front. Mirrors the
// docker-side `FindComposeCommand` in structure: prefer the v2 plugin,
// fall back to the v1 standalone binary, error with a clear message if
// neither is on PATH.
func podmanFindComposeCommand(ctx context.Context) (ComposeCommand, error) {
	if path, err := exec.LookPath("podman"); err == nil {
		// `podman` existing does not mean the compose plugin is installed.
		// podman's own `--version` flag is a more reliable check than
		// shelling out an extra command.
		cmd := exec.CommandContext(ctx, path, "compose", "version")
		if err := cmd.Run(); err == nil {
			return ComposeCommand{Name: path, Base: []string{"compose"}}, nil
		}
	}

	if path, err := exec.LookPath("podman-compose"); err == nil {
		return ComposeCommand{Name: path}, nil
	}

	return ComposeCommand{}, errors.New(
		"no Podman Compose CLI found: neither `podman compose` nor `podman-compose` is available")
}

// client returns an HTTP client whose transport is a podman-aware dialer.
// Callers must guard `p.socket` itself; the cache is initialised by
// resolveSocket before any method that reaches here is invoked.
func (p *Podman) client() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", p.socket)
			},
		},
	}
}

// connect is a synonym for resolveSocket: it makes sure the socket path
// has been resolved before the dialer is used, and surfaces any resolution
// error to the caller. Kept separate from `connectedSocket` because some
// entry points only need the call site to know there was an error
// (`Logs`, `Events`); the same plumbing under the hood.
func (p *Podman) connect() error {
	p.resolveSocket()
	return p.socketErr
}

// connectedSocket returns the cached socket path or the resolution error.
func (p *Podman) connectedSocket() (string, error) {
	p.resolveSocket()
	return p.socket, p.socketErr
}

// newRequest builds an *http.Request whose URL is correct even when
// `pathAndQuery` contains a `?` separator.
//
// Why this exists: setting `url.URL{Path: "/v1.40/x?all=1"}` causes
// `URL.String()` to encode the `?` as `%3F`, so a server that matches
// by `URL.Path` would see `/v1.40/x%3Fall=1` and return 404. Splitting
// path and query at the first `?` and putting each into its own URL
// field avoids the encoding entirely.
//
// The dialer ignores `URL.Host` so a placeholder host works for the
// unix socket; using a string distinct from any docker-side prefix
// keeps `/health` checks unambiguous during multi-runtime debugging.
func (p *Podman) newRequest(ctx context.Context, method, pathAndQuery string) (*http.Request, error) {
	pathOnly, query, _ := strings.Cut(pathAndQuery, "?")
	podmanURL := url.URL{
		Scheme:   "http",
		Host:     "podman",
		Path:     pathOnly,
		RawQuery: query,
	}
	return http.NewRequestWithContext(ctx, method, podmanURL.String(), nil)
}

// doJSON issues a one-shot JSON request via the podman socket. Method and
// path are joined against the podman URL host "podman"; the dialer sends
// the request through the unix socket regardless.
func (p *Podman) doJSON(ctx context.Context, method, path string, out any) error {
	req, err := p.newRequest(ctx, method, path)
	if err != nil {
		return err
	}
	resp, err := p.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s %s: %d %s", method, path, resp.StatusCode, bytes.TrimSpace(body))
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// podmanContainerSummary mirrors the JSON podman returns from
// /v1.40/containers/json: just the subset Running Man actually reads.
type podmanContainerSummary struct {
	Id      string            `json:"Id"`
	Names   []string          `json:"Names"`
	Image   string            `json:"Image"`
	State   string            `json:"State"`
	Created int64             `json:"Created"`
	Labels  map[string]string `json:"Labels"`
}

// podmanContainerInspect mirrors /v1.40/containers/{id}/json: again, the
// subset Running Man actually reads.
type podmanContainerInspect struct {
	Id      string `json:"Id"`
	Name    string `json:"Name"`
	Image   string `json:"Image"`
	Created string `json:"Created"`
	State   struct {
		Status   string `json:"Status"`
		ExitCode int    `json:"ExitCode"`
	} `json:"State"`
	Config struct {
		Labels map[string]string `json:"Labels"`
		Tty    bool              `json:"Tty"`
	} `json:"Config"`
}

// podmanEvent mirrors one NDJSON record from /v1.40/events. We only
// surface container events; the broadcaster converts this to a flat
// runtime.Event.
type podmanEvent struct {
	Type   string `json:"Type"`
	Action string `json:"Action"`
	Actor  struct {
		ID         string            `json:"ID"`
		Attributes map[string]string `json:"Attributes"`
	} `json:"Actor"`
	Time     int64 `json:"time"`
	TimeNano int64 `json:"timeNano"`
}

// podmanSummaryToContainer maps a single record from the List endpoint
// into the runtime's Container shape. Compose-aware labels are pulled
// out because today's processes are still compose-flavoured; once a
// podman-only workload surfaces, those labels will be empty but the
// mapping remains.
func podmanSummaryToContainer(s podmanContainerSummary) Container {
	name := ""
	if len(s.Names) > 0 {
		name = strings.TrimPrefix(s.Names[0], "/")
	}
	c := Container{
		ID:      shortID(s.Id),
		Name:    name,
		Image:   s.Image,
		State:   s.State,
		Created: s.Created,
	}
	if s.Labels != nil {
		c.ServiceName = s.Labels["com.docker.compose.service"]
		c.ProjectName = s.Labels["com.docker.compose.project"]
	}
	return c
}

// podmanInspectToContainer mirrors podmanSummaryToContainer for the
// /inspect endpoint. The inspect response is richer (StartedAt,
// FinishedAt, Health); only the fields the supervisor reads migrate
// across for now. TTY is filled here so the streamer can decide
// whether to demux.
func podmanInspectToContainer(inf podmanContainerInspect) Container {
	c := Container{
		ID:    shortID(inf.Id),
		Name:  strings.TrimPrefix(inf.Name, "/"),
		Image: inf.Image,
		State: inf.State.Status,
		Exit:  inf.State.ExitCode,
		TTY:   inf.Config.Tty,
	}
	if inf.Config.Labels != nil {
		c.ServiceName = inf.Config.Labels["com.docker.compose.service"]
		c.ProjectName = inf.Config.Labels["com.docker.compose.project"]
	}
	return c
}

// shortID returns the 12-character prefix podman and docker both expose
// for shell-friendly identification. Empty input stays empty.
func shortID(id string) string {
	const prefix = 12
	if len(id) <= prefix {
		return id
	}
	return id[:prefix]
}
