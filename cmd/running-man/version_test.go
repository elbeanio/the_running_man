package main

import (
	"runtime/debug"
	"testing"
)

// `running-man version` printed a hardcoded "v0.1.0 (Phase 1)" whatever was
// installed. The version is the one Go records in the binary: the tag for
// `go install ...@v0.1.1`, a pseudo-version for a build from a checkout.
func TestVersionComesFromTheBuild(t *testing.T) {
	info := func(v string) func() (*debug.BuildInfo, bool) {
		return func() (*debug.BuildInfo, bool) { return &debug.BuildInfo{Main: debug.Module{Version: v}}, true }
	}
	cases := []struct {
		name string
		read func() (*debug.BuildInfo, bool)
		want string
	}{
		{"tagged install", info("v0.1.1"), "v0.1.1"},
		{"checkout build", info("v0.1.2-0.20261012093000-abcdef123456+dirty"), "v0.1.2-0.20261012093000-abcdef123456+dirty"},
		{"no version recorded", info("(devel)"), "dev"},
		{"no build info", func() (*debug.BuildInfo, bool) { return nil, false }, "dev"},
	}
	for _, c := range cases {
		if got := buildVersion(c.read); got != c.want {
			t.Errorf("%s: version %q, want %q", c.name, got, c.want)
		}
	}
}
