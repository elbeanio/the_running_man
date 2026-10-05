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

// portCacheTTL bounds how often lsof is invoked. /processes can be polled -- an
// agent checking whether a server is up will -- and shelling out per request
// would be wasteful; a port is not going to change within a second or two.
const portCacheTTL = 2 * time.Second

type portCacheEntry struct {
	ports []int
	at    time.Time
}

var (
	portCacheMu sync.Mutex
	portCache   = map[int]portCacheEntry{}
)

// The two commands port detection runs, behind variables so tests can count
// them and hold one open.
var (
	snapshotProcesses = listProcesses
	runLsof           = func(args ...string) []byte {
		// Output is returned even when lsof exits non-zero; see the caller.
		out, _ := exec.Command("lsof", args...).Output()
		return out
	}
)

// ListeningPorts returns the TCP ports the process or any of its descendants
// are listening on, lowest first.
//
// Returns nil when nothing is listening, when the process is gone, or when
// lsof is unavailable. Port information is a helpful extra, never a
// precondition, so every failure path degrades to "unknown" rather than an
// error.
func ListeningPorts(pid int) []int {
	return ListeningPortsFor([]int{pid})[pid]
}

// ListeningPortsFor is ListeningPorts for several processes at once, keyed by
// pid.
//
// Whatever is not cached is looked up together: one process-table snapshot and
// one lsof, however many processes there are. /processes used to look each one
// up separately -- a full snapshot and an lsof per process, 2N subprocesses in
// sequence, fetching the same table N times.
func ListeningPortsFor(pids []int) map[int][]int {
	result := make(map[int][]int, len(pids))
	var todo []int

	portCacheMu.Lock()
	now := time.Now()
	for _, pid := range pids {
		if pid <= 0 {
			continue
		}
		if entry, ok := portCache[pid]; ok && now.Sub(entry.at) < portCacheTTL {
			result[pid] = entry.ports
			continue
		}
		todo = append(todo, pid)
	}
	portCacheMu.Unlock()

	if len(todo) == 0 {
		return result
	}
	found := listeningPortsUncached(todo)

	portCacheMu.Lock()
	// Expired entries are swept here because nothing else removes them, and
	// every run of a recurring process has a new pid: left alone, the cache
	// grew for the life of the instance. The sweep is over entries written in
	// the last TTL plus the dead ones, which is a handful.
	now = time.Now()
	for p, entry := range portCache {
		if now.Sub(entry.at) >= portCacheTTL {
			delete(portCache, p)
		}
	}
	for _, pid := range todo {
		portCache[pid] = portCacheEntry{ports: found[pid], at: now}
		result[pid] = found[pid]
	}
	portCacheMu.Unlock()

	return result
}

// listeningPortsUncached looks up the listening ports of each root process's
// whole tree, with one snapshot and one lsof, keyed by root.
func listeningPortsUncached(roots []int) map[int][]int {
	owner := processTrees(roots)

	pids := make([]int, 0, len(owner))
	for p := range owner {
		pids = append(pids, p)
	}
	sort.Ints(pids)

	// -F pn: a "p<pid>" line, then an "n<address>" line per listening socket,
	// so each port can be attributed to the tree it came from.
	args := []string{"-nP", "-iTCP", "-sTCP:LISTEN", "-a", "-F", "pn", "-p", joinInts(pids, ",")}

	// Parse the output even when lsof reports failure, and ignore the error.
	//
	// lsof exits non-zero if ANY pid it was given is invalid, or if nothing
	// matched -- but it still prints what it did find. Since the pid list comes
	// from a process-table snapshot, a short-lived child that has since exited
	// is normal, and treating that as total failure meant one dead pid hid every
	// real port. runLsof returns captured stdout regardless.
	output := runLsof(args...)
	if len(output) == 0 {
		// Nothing listening, or lsof is unavailable. Both mean "no ports known";
		// port information is a hint, never a precondition.
		return nil
	}

	seen := map[int]map[int]bool{}
	current := 0
	for _, line := range strings.Split(string(output), "\n") {
		if line == "" {
			continue
		}
		switch line[0] {
		case 'p':
			current, _ = strconv.Atoi(line[1:])
		case 'n':
			// "n*:8123", "n127.0.0.1:5432", "n[::1]:8080".
			root, ok := owner[current]
			if !ok {
				continue
			}
			if port, ok := portFromAddr(line[1:]); ok {
				if seen[root] == nil {
					seen[root] = map[int]bool{}
				}
				seen[root][port] = true
			}
		}
	}

	result := make(map[int][]int, len(seen))
	for root, set := range seen {
		ports := make([]int, 0, len(set))
		for p := range set {
			ports = append(ports, p)
		}
		sort.Ints(ports)
		result[root] = ports
	}
	return result
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

// processTrees maps each root and every one of its descendants to the root,
// from a single snapshot of the process table.
//
// Needed because commands run through a shell: the listener is typically a
// grandchild, so asking lsof about the shell's pid alone finds nothing.
func processTrees(roots []int) map[int]int {
	owner := make(map[int]int, len(roots))
	for _, root := range roots {
		owner[root] = root
	}

	entries, err := snapshotProcesses()
	if err != nil {
		// Without the process table we can still ask about the roots themselves.
		return owner
	}

	children := map[int][]int{}
	for _, e := range entries {
		children[e.ppid] = append(children[e.ppid], e.pid)
	}
	for _, root := range roots {
		queue := []int{root}
		for len(queue) > 0 {
			p := queue[0]
			queue = queue[1:]
			for _, c := range children[p] {
				if _, done := owner[c]; !done {
					owner[c] = root
					queue = append(queue, c)
				}
			}
		}
	}
	return owner
}

func joinInts(values []int, sep string) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, sep)
}
