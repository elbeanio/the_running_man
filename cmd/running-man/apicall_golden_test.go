package main

import (
	"strings"
	"testing"

	"github.com/muesli/termenv"
)

func apiCallModel() model {
	m := fixtureModel(100, 16)
	m.selectedSource = 0 // running-man
	m.logs = []logEntry{
		{Timestamp: "2026-10-08T19:42:10Z", Level: "info", Source: "running-man",
			Message: "backend is waiting for denodo"},
		{Timestamp: "2026-10-08T19:42:11Z", Level: "info", Source: "running-man",
			Message: "Compose service denodo is ready"},
		{Timestamp: "2026-10-08T19:43:02Z", Level: "info", Source: "running-man",
			Message: "api GET /traces?since=10m 200 4.1 KB 2ms"},
		{Timestamp: "2026-10-08T19:43:05Z", Level: "info", Source: "running-man",
			Message: "api GET /traces/a52b1a91528093522205536890ef2f09 200 21.7 KB 6ms"},
		{Timestamp: "2026-10-08T19:43:09Z", Level: "warn", Source: "running-man",
			Message: "api GET /traces/nope 404 42 B 157µs"},
	}
	return m
}

// An agent's API calls share Running Man's own source, so they are drawn in
// their own colour to tell them from Running Man's messages at a glance.
func TestGoldenAPICalls(t *testing.T) {
	assertGolden(t, "running-man-api-calls", apiCallModel().View())

	withProfile(t, termenv.TrueColor)
	frame := apiCallModel().View()
	assertGolden(t, "running-man-api-calls-colour", frame)

	cyan := termenv.TrueColor.Color("44").Sequence(false)
	for _, line := range strings.Split(frame, "\n") {
		isCall := strings.Contains(line, "api GET")
		if isCall != strings.Contains(line, cyan) {
			t.Errorf("api call colour on the wrong line (call=%v): %q", isCall, line)
		}
	}
}
