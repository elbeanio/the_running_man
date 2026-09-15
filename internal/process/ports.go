//go:build !windows
// +build !windows

package process

import (
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Listening-port detection.
//
// This exists to answer the one question that matters when an agent is about to
// start a dev server: is the thing I am about to start already running? Nothing
// in the config records a port -- ProcessConfig has only an optional free-text
// `url` field -- so ports have to be observed rather than declared.
//
// Two wrinkles make this less obvious than it looks:
//
//   - Commands run via `shell -c`, so the process we spawn is the shell. The
//     server that actually listens is usually a grandchild, and lsof against
//     the shell's pid alone finds nothing. Ports are therefore collected across
//     the whole descendant tree.
//   - A dev server binds a moment after starting, so ports are resolved when
//     asked rather than recorded once at startup.

// portCacheTTL bounds how often lsof is invoked. /processes can be polled (the
// TUI does), and shelling out per request would be wasteful; a port is not
// going to change within a second or two.
const portCacheTTL = 2 * time.Second

type portCacheEntry struct {
	ports []int
	at    time.Time
}

var (
	portCacheMu sync.Mutex
	portCache   = map[int]portCacheEntry{}
)

// ListeningPorts returns the TCP ports the process or any of its descendants
// are listening on, lowest first.
//
// Returns nil when nothing is listening, when the process is gone, or when
// lsof is unavailable. Port information is a helpful extra, never a
// precondition, so every failure path degrades to "unknown" rather than an
// error.
func ListeningPorts(pid int) []int {
	if pid <= 0 {
		return nil
	}

	portCacheMu.Lock()
	if entry, ok := portCache[pid]; ok && time.Since(entry.at) < portCacheTTL {
		portCacheMu.Unlock()
		return entry.ports
	}
	portCacheMu.Unlock()

	ports := listeningPortsUncached(pid)

	portCacheMu.Lock()
	portCache[pid] = portCacheEntry{ports: ports, at: time.Now()}
	portCacheMu.Unlock()

	return ports
}

func listeningPortsUncached(pid int) []int {
	pids := processTree(pid)
	if len(pids) == 0 {
		return nil
	}

	args := []string{"-nP", "-iTCP", "-sTCP:LISTEN", "-a", "-F", "n", "-p", joinInts(pids, ",")}

	// Parse the output even when lsof reports failure, and ignore the error.
	//
	// lsof exits non-zero if ANY pid it was given is invalid, or if nothing
	// matched -- but it still prints what it did find. Since the pid list comes
	// from a process-table snapshot, a short-lived child that has since exited
	// is normal, and treating that as total failure meant one dead pid hid every
	// real port. exec.Cmd.Output returns captured stdout alongside the error.
	output, _ := exec.Command("lsof", args...).Output()
	if len(output) == 0 {
		// Nothing listening, or lsof is unavailable. Both mean "no ports known";
		// port information is a hint, never a precondition.
		return nil
	}

	seen := map[int]bool{}
	for _, line := range strings.Split(string(output), "\n") {
		// -F n emits one field per line, prefixed by its type: "n*:8123",
		// "n127.0.0.1:5432", "n[::1]:8080".
		if !strings.HasPrefix(line, "n") {
			continue
		}
		if port, ok := portFromAddr(strings.TrimPrefix(line, "n")); ok {
			seen[port] = true
		}
	}

	ports := make([]int, 0, len(seen))
	for p := range seen {
		ports = append(ports, p)
	}
	sort.Ints(ports)
	if len(ports) == 0 {
		return nil
	}
	return ports
}

// portFromAddr extracts the port from an lsof address such as "*:8123",
// "127.0.0.1:5432" or "[::1]:8080".
func portFromAddr(addr string) (int, bool) {
	idx := strings.LastIndex(addr, ":")
	if idx < 0 || idx == len(addr)-1 {
		return 0, false
	}
	port, err := strconv.Atoi(strings.TrimSpace(addr[idx+1:]))
	if err != nil || port <= 0 || port > 65535 {
		return 0, false
	}
	return port, true
}

// processTree returns pid and all of its descendants.
//
// Needed because commands run through a shell: the listener is typically a
// grandchild, so asking lsof about the shell's pid alone finds nothing.
func processTree(pid int) []int {
	entries, err := listProcesses()
	if err != nil {
		// Without the process table we can still ask about the pid itself.
		return []int{pid}
	}

	tree := map[int]bool{pid: true}
	for changed := true; changed; {
		changed = false
		for _, e := range entries {
			if tree[e.pid] {
				continue
			}
			if tree[e.ppid] {
				tree[e.pid] = true
				changed = true
			}
		}
	}

	pids := make([]int, 0, len(tree))
	for p := range tree {
		pids = append(pids, p)
	}
	sort.Ints(pids)
	return pids
}

func joinInts(values []int, sep string) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, sep)
}
