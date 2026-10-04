// Package instance writes the instance marker: a small file that tells anything
// looking at the project directory that a Running Man instance is live, and how
// to talk to it.
//
// # Why this exists
//
// Running Man went unused not because agents refused it but because nothing
// cheaply told them it was there. The expected cost of investigating an unknown
// tool exceeded the known cost of `npm run dev 2>&1 | tail -50`, so the cheaper
// path won -- correctly, on the information available. The marker is the cheap
// signal: one file read, in a directory agents already inspect, answering the
// question that actually matters before starting a dev server.
//
// # Why it is static
//
// The marker holds only stable facts: where the API is, when the instance
// started, and what it was configured to run. Anything live -- status, ports,
// exit codes -- is deliberately absent, and the marker points at /processes
// instead.
//
// The alternative was rewriting the file on every process state change, which
// would put I/O on a hot path and, worse, leave a confidently wrong file behind
// after a crash. A marker that cannot go stale in its details is more useful
// than one that tries to mirror live state and sometimes lies.
package instance

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DirName is the directory holding instance state, relative to the project root.
// A directory rather than a bare file so future instance state has somewhere to
// go without another top-level entry.
const DirName = ".running-man"

// FileName is the marker file within DirName.
const FileName = "instance.json"

// SocketName is the Unix socket within DirName that serves the API.
//
// The path is derived from the project directory, which is the whole point: an
// agent working in project-b cannot reach project-a's instance by accident, and
// there is no port to allocate, collide over or scan for.
const SocketName = "api.sock"

// socketPathLimit is a conservative ceiling on a Unix socket path.
//
// sun_path is a fixed-size field in the sockaddr_un struct: 104 bytes on macOS,
// 108 on Linux. Exceed it and bind() fails with EINVAL -- reported by Go as the
// splendidly unhelpful "bind: invalid argument". 100 leaves room under the
// smaller of the two.
const socketPathLimit = 100

// Process describes one configured process, as a stable fact about the instance
// rather than a live status report.
type Process struct {
	Name        string `json:"name"`
	Command     string `json:"command"`
	Type        string `json:"type,omitempty"`
	Description string `json:"description,omitempty"`
	URL         string `json:"url,omitempty"`
	Interval    string `json:"interval,omitempty"`
}

// Marker is the content of .running-man/instance.json.
type Marker struct {
	// Note is first so that anything reading the file -- human or agent --
	// learns what to do with it before parsing anything else.
	Note string `json:"note"`

	// Socket is the Unix socket serving the API, as a path relative to nothing
	// -- it is absolute, so a reader in any working directory can use it.
	Socket    string    `json:"socket"`
	PID       int       `json:"pid"`
	Started   time.Time `json:"started"`
	Project   string    `json:"project"`
	Processes []Process `json:"processes"`

	// Hints are ready-to-run commands. Included so that reading the marker is
	// sufficient to act, without a second lookup to learn the API surface.
	Hints map[string]string `json:"hints"`
}

// New builds a marker for a live instance.
//
// socketPath is where the API is served. The hints are built from it rather
// than from a port, so they stay correct and copy-pasteable: the host in the URL
// is ignored by curl once --unix-socket is given, and "localhost" is the
// conventional placeholder.
func New(socketPath string, projectDir string, processes []Process) *Marker {
	// A URL carrying a query string is single-quoted, because '&' would
	// otherwise background the command in a shell. The whole URL is quoted, not
	// part of it -- the point of a hint is that it can be pasted as-is.
	curl := func(method, path string) string {
		url := "http://localhost" + path
		if strings.ContainsAny(path, "?&") {
			url = "'" + url + "'"
		}
		if method != "" {
			return fmt.Sprintf("curl -s -X %s --unix-socket %s %s", method, socketPath, url)
		}
		return fmt.Sprintf("curl -s --unix-socket %s %s", socketPath, url)
	}

	return &Marker{
		Note: "A Running Man instance is supervising this project. Before starting a dev " +
			"server, test runner or other long-running process, check whether it is already " +
			"running here -- and if it is, use it instead of starting a second copy. The API " +
			"is served on the Unix socket named below, not on a TCP port: use curl's " +
			"--unix-socket, as the hints show. Live status and listening ports are at the " +
			"/processes endpoint; this file lists only what was configured. If the socket " +
			"does not respond, this file is stale and can be ignored.",
		Socket:    socketPath,
		PID:       os.Getpid(),
		Started:   time.Now(),
		Project:   projectDir,
		Processes: processes,
		Hints: map[string]string{
			"is_it_running":  curl("", "/processes"),
			"is_it_alive":    curl("", "/health"),
			"recent_errors":  curl("", "/errors?since=10m&limit=50"),
			"search_logs":    curl("", "/logs?contains=TEXT&since=10m&limit=50"),
			"one_source":     curl("", "/logs?source=NAME&since=5m&limit=50"),
			"restart_one":    curl("POST", "/processes/NAME/restart"),
			"all_endpoints":  curl("", "/"),
			"api_spec":       curl("", "/openapi.yaml"),
			"traces":         curl("", "/traces?since=10m"),
			"logs_for_trace": curl("", "/traces/TRACE_ID/logs"),
		},
	}
}

// Write creates the marker under projectDir.
//
// Written atomically via a temp file and rename, so a reader never sees a
// half-written file -- an agent polling this while it is being written would
// otherwise get a JSON parse error and reasonably conclude the tool is broken.
func (m *Marker) Write(projectDir string) error {
	dir := filepath.Join(projectDir, DirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}

	// HTML escaping off: the hints are URLs containing '&', and the default
	// encoder turns those into \u0026. Valid JSON, but the point of the hints is
	// that a reader can copy them straight out of the file.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		return fmt.Errorf("encoding marker: %w", err)
	}
	data := buf.Bytes()

	final := filepath.Join(dir, FileName)
	tmp, err := os.CreateTemp(dir, FileName+".tmp*")
	if err != nil {
		return fmt.Errorf("creating temp marker: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("writing temp marker: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("closing temp marker: %w", err)
	}
	// os.CreateTemp makes the file 0600. This is a discovery aid meant to be
	// read by other tools, so widen it to the usual 0644.
	if err := os.Chmod(tmpName, 0o644); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("setting marker permissions: %w", err)
	}
	if err := os.Rename(tmpName, final); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("installing marker: %w", err)
	}
	return nil
}

// Remove deletes the marker. Missing is not an error: Remove is called on every
// shutdown path, and a marker that is already gone is the desired end state.
//
// The containing directory is removed too when empty, so a clean exit leaves no
// trace.
func Remove(projectDir string) error {
	dir := filepath.Join(projectDir, DirName)

	if err := os.Remove(filepath.Join(dir, FileName)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing marker: %w", err)
	}
	// Best effort: fails harmlessly if anything else lives here.
	_ = os.Remove(dir)
	return nil
}

// Path returns where the marker lives, for logging and error messages.
func Path(projectDir string) string {
	return filepath.Join(projectDir, DirName, FileName)
}

// SocketPath returns where the API socket lives for a project.
//
// Normally inside the project, beside the marker: short, stable, and writable in
// documentation as a literal. A deeply nested project directory can push that
// past socketPathLimit, though, and refusing to start over a long pathname would
// be absurd -- so such a project gets a short socket under the temp directory
// instead, keyed by a hash of its own path so it stays deterministic and still
// cannot collide with another project's.
//
// Either way the marker records the result, so discovery is unaffected: a reader
// that takes the path from .running-man/instance.json is always right.
func SocketPath(projectDir string) string {
	inProject := filepath.Join(projectDir, DirName, SocketName)
	if len(inProject) <= socketPathLimit {
		return inProject
	}

	// Hashed rather than sanitised: the point is a short name, and a hash is the
	// only thing guaranteed to be both short and unique to this project.
	sum := sha256.Sum256([]byte(projectDir))
	return filepath.Join(
		os.TempDir(),
		fmt.Sprintf("running-man-%d", os.Getuid()),
		hex.EncodeToString(sum[:6])+".sock",
	)
}

// Read loads an existing marker.
//
// Used when refusing to start beside a live instance: the socket proves someone
// is listening, and the marker is what can name them. A marker that is missing
// or unparseable is not fatal to that path -- the refusal just loses the PID --
// so callers are expected to tolerate the error.
func Read(projectDir string) (*Marker, error) {
	data, err := os.ReadFile(Path(projectDir))
	if err != nil {
		return nil, err
	}
	var m Marker
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", Path(projectDir), err)
	}
	return &m, nil
}
