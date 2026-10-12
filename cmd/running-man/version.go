package main

import "runtime/debug"

// buildVersion is the version Go recorded in this binary: the tag for
// `go install github.com/elbeanio/the_running_man/cmd/running-man@v0.1.1`, or a
// pseudo-version derived from the latest tag for a build from a checkout. It
// replaced a hardcoded string, which said v0.1.0 whatever was installed.
func buildVersion(read func() (*debug.BuildInfo, bool)) string {
	if info, ok := read(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}
