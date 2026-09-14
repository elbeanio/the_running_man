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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// DirName is the directory holding instance state, relative to the project root.
// A directory rather than a bare file so future instance state has somewhere to
// go without another top-level entry.
const DirName = ".running-man"

// FileName is the marker file within DirName.
const FileName = "instance.json"

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

	API       string    `json:"api"`
	PID       int       `json:"pid"`
	Started   time.Time `json:"started"`
	Project   string    `json:"project"`
	Processes []Process `json:"processes"`

	// Hints are ready-to-run commands. Included so that reading the marker is
	// sufficient to act, without a second lookup to learn the API surface.
	Hints map[string]string `json:"hints"`
}

// New builds a marker for a live instance.
func New(apiURL string, projectDir string, processes []Process) *Marker {
	return &Marker{
		Note: "A Running Man instance is supervising this project. Before starting a dev " +
			"server, test runner or other long-running process, check whether it is already " +
			"running here -- and if it is, use it instead of starting a second copy. Live " +
			"status and listening ports are at the /processes endpoint; this file lists only " +
			"what was configured. If the API does not respond, this file is stale and can be " +
			"ignored.",
		API:       apiURL,
		PID:       os.Getpid(),
		Started:   time.Now(),
		Project:   projectDir,
		Processes: processes,
		Hints: map[string]string{
			"is_it_running":  fmt.Sprintf("curl -s %s/processes", apiURL),
			"is_it_alive":    fmt.Sprintf("curl -s %s/health", apiURL),
			"recent_errors":  fmt.Sprintf("curl -s '%s/errors?since=10m&limit=50'", apiURL),
			"search_logs":    fmt.Sprintf("curl -s '%s/logs?contains=TEXT&since=10m&limit=50'", apiURL),
			"one_source":     fmt.Sprintf("curl -s '%s/logs?source=NAME&since=5m&limit=50'", apiURL),
			"restart_one":    fmt.Sprintf("curl -s -X POST %s/processes/NAME/restart", apiURL),
			"all_endpoints":  fmt.Sprintf("curl -s %s/", apiURL),
			"api_docs":       fmt.Sprintf("%s/docs", apiURL),
			"traces":         fmt.Sprintf("curl -s '%s/traces?since=10m'", apiURL),
			"logs_for_trace": fmt.Sprintf("curl -s %s/traces/TRACE_ID/logs", apiURL),
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
