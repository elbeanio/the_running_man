package instance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMarker_WriteAndRemove(t *testing.T) {
	dir := t.TempDir()

	m := New("http://localhost:9000", dir, []Process{
		{Name: "backend", Command: "python server.py", Type: "api", URL: "http://localhost:8000"},
		{Name: "checker", Command: "./check.sh", Interval: "1m"},
	})
	if err := m.Write(dir); err != nil {
		t.Fatalf("Write: %v", err)
	}

	path := Path(dir)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("marker not written: %v", err)
	}

	var got Marker
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("marker is not valid JSON: %v", err)
	}
	if got.API != "http://localhost:9000" {
		t.Errorf("api = %q", got.API)
	}
	if len(got.Processes) != 2 {
		t.Fatalf("got %d processes, want 2", len(got.Processes))
	}
	if got.Processes[0].Name != "backend" || got.Processes[1].Interval != "1m" {
		t.Errorf("process details not preserved: %+v", got.Processes)
	}
	if got.PID != os.Getpid() {
		t.Errorf("pid = %d, want %d", got.PID, os.Getpid())
	}

	// The note is the first thing a reader sees and carries the actual
	// instruction, so it must say what to do rather than merely announce
	// existence.
	if !strings.Contains(strings.ToLower(got.Note), "already running") {
		t.Errorf("note should tell the reader to check whether things are already running: %q", got.Note)
	}

	// Readable by other tools, not just the owner: os.CreateTemp defaults to
	// 0600, which would defeat the point of a discovery aid.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("marker mode = %v, want 0644", info.Mode().Perm())
	}

	if err := Remove(dir); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("marker still present after Remove")
	}
	// A clean exit should leave no trace.
	if _, err := os.Stat(filepath.Join(dir, DirName)); !os.IsNotExist(err) {
		t.Error("marker directory still present after Remove")
	}
}

// Remove runs on every shutdown path, so an absent marker must not be an error.
func TestMarker_RemoveIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	if err := Remove(dir); err != nil {
		t.Errorf("Remove on a missing marker should succeed, got: %v", err)
	}
	if err := Remove(dir); err != nil {
		t.Errorf("second Remove should succeed, got: %v", err)
	}
}

// The hints exist so that reading the marker is enough to act. HTML-escaped
// ampersands (\u0026, the encoder's default) survive JSON decoding but make the
// raw file unusable to copy from, which defeats the purpose.
func TestMarker_HintsAreCopyPasteable(t *testing.T) {
	dir := t.TempDir()
	m := New("http://localhost:9000", dir, nil)
	if err := m.Write(dir); err != nil {
		t.Fatalf("Write: %v", err)
	}

	raw, err := os.ReadFile(Path(dir))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(raw), `\u0026`) {
		t.Error("hints contain HTML-escaped ampersands; they cannot be copied from the raw file")
	}

	for _, key := range []string{"is_it_running", "recent_errors", "search_logs", "all_endpoints"} {
		hint, ok := m.Hints[key]
		if !ok {
			t.Errorf("missing hint %q", key)
			continue
		}
		if !strings.Contains(hint, "localhost:9000") {
			t.Errorf("hint %q does not reference the API: %q", key, hint)
		}
	}
}

// Writing must be atomic: an agent reading the file while it is written would
// otherwise get a parse error and reasonably conclude the tool is broken.
func TestMarker_WriteIsAtomicAndLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	m := New("http://localhost:9000", dir, nil)

	for i := 0; i < 5; i++ {
		if err := m.Write(dir); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	entries, err := os.ReadDir(filepath.Join(dir, DirName))
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != FileName {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("expected only %s, got %v", FileName, names)
	}
}

// Overwriting must replace the contents rather than append or merge.
func TestMarker_WriteOverwrites(t *testing.T) {
	dir := t.TempDir()

	if err := New("http://localhost:1111", dir, []Process{{Name: "old"}}).Write(dir); err != nil {
		t.Fatalf("first Write: %v", err)
	}
	if err := New("http://localhost:2222", dir, []Process{{Name: "new"}}).Write(dir); err != nil {
		t.Fatalf("second Write: %v", err)
	}

	data, err := os.ReadFile(Path(dir))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var got Marker
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("not valid JSON after overwrite: %v", err)
	}
	if got.API != "http://localhost:2222" || len(got.Processes) != 1 || got.Processes[0].Name != "new" {
		t.Errorf("overwrite did not replace contents: %+v", got)
	}
}
