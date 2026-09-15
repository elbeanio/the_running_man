package process

import (
	"fmt"
	"net"
	"os"
	"testing"
	"time"
)

// Ports have to be observed rather than declared: nothing in running-man.yml
// records a port, so "is the thing I am about to start already running?"
// cannot be answered from config alone.
func TestPortFromAddr(t *testing.T) {
	tests := []struct {
		addr string
		want int
		ok   bool
	}{
		{"*:8123", 8123, true},
		{"127.0.0.1:5432", 5432, true},
		{"[::1]:8080", 8080, true},
		{"[fe80::1%en0]:443", 443, true},
		{"*:0", 0, false},
		{"*:99999", 0, false}, // out of range
		{"nonsense", 0, false},
		{"", 0, false},
		{"*:", 0, false},
	}
	for _, tt := range tests {
		got, ok := portFromAddr(tt.addr)
		if ok != tt.ok || (tt.ok && got != tt.want) {
			t.Errorf("portFromAddr(%q) = (%d, %v), want (%d, %v)", tt.addr, got, ok, tt.want, tt.ok)
		}
	}
}

// Port detection must degrade to "unknown" rather than failing: it is a
// helpful extra, never a precondition.
func TestListeningPorts_DegradesGracefully(t *testing.T) {
	if got := ListeningPorts(0); got != nil {
		t.Errorf("ListeningPorts(0) = %v, want nil", got)
	}
	if got := ListeningPorts(-1); got != nil {
		t.Errorf("ListeningPorts(-1) = %v, want nil", got)
	}
	// A pid that cannot exist.
	if got := ListeningPorts(0x7FFFFFFF); got != nil {
		t.Errorf("ListeningPorts(impossible) = %v, want nil", got)
	}
}

// The process tree must include the process itself, because a command run
// without shell metacharacters is exec'd directly and IS the listener.
func TestProcessTree_IncludesSelf(t *testing.T) {
	tree := processTree(os.Getpid())
	var found bool
	for _, p := range tree {
		if p == os.Getpid() {
			found = true
		}
	}
	if !found {
		t.Errorf("processTree(%d) does not include the pid itself: %v", os.Getpid(), tree)
	}
}

// End-to-end: a port this test process is listening on must be discovered.
// Covers the plumbing -- lsof invocation, -F n parsing, the cache -- against
// the real OS rather than a mock.
func TestListeningPorts_FindsARealListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot listen: %v", err)
	}
	defer ln.Close()

	wantPort := ln.Addr().(*net.TCPAddr).Port

	// The cache is keyed by pid and this process may already be cached from
	// another test, so clear it rather than waiting out the TTL.
	portCacheMu.Lock()
	delete(portCache, os.Getpid())
	portCacheMu.Unlock()

	ports := ListeningPorts(os.Getpid())
	if ports == nil {
		t.Skip("no ports reported; lsof is likely unavailable in this environment")
	}

	for _, p := range ports {
		if p == wantPort {
			return
		}
	}
	t.Errorf("listening on %d but ListeningPorts returned %v", wantPort, ports)
}

// The cache exists because /processes can be polled and shelling out per
// request would be wasteful.
func TestListeningPorts_UsesCache(t *testing.T) {
	pid := os.Getpid()

	portCacheMu.Lock()
	portCache[pid] = portCacheEntry{ports: []int{4242}, at: time.Now()}
	portCacheMu.Unlock()

	if got := fmt.Sprint(ListeningPorts(pid)); got != "[4242]" {
		t.Errorf("cached value not used: got %s", got)
	}

	// An expired entry must be recomputed rather than returned.
	portCacheMu.Lock()
	portCache[pid] = portCacheEntry{ports: []int{4242}, at: time.Now().Add(-2 * portCacheTTL)}
	portCacheMu.Unlock()

	if got := fmt.Sprint(ListeningPorts(pid)); got == "[4242]" {
		t.Error("expired cache entry was returned instead of being recomputed")
	}

	portCacheMu.Lock()
	delete(portCache, pid)
	portCacheMu.Unlock()
}
