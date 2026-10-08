package main

import (
	"testing"

	"github.com/muesli/termenv"
)

// Startup screen frames, modelled on a real stack: two Compose services, a
// backend depending on one of them, and a frontend depending on the backend.

func startupInProgress() []startupRow {
	return []startupRow{
		{Name: "denodo-source-db", Kind: "service", State: "ready",
			Detail: "its Compose healthcheck passed",
			Lines:  []string{"database system is ready to accept connections"}},
		{Name: "denodo", Kind: "service", State: "checking",
			Detail: "waiting for port 9996",
			Lines: []string{
				"[VDP] INFO server.start - TLS is disabled on incoming connections",
				"[SSO] INFO Starting DenodoSSOWebApplication 9.4.0 using Java 17.0.17",
			}},
		{Name: "backend", Kind: "process", State: "pending", Detail: "waiting for denodo"},
		{Name: "frontend", Kind: "process", State: "running", Detail: "no dependencies",
			Lines: []string{"VITE v5.4.2  ready in 412 ms", "➜  Local:   http://localhost:5175/"}},
	}
}

func startupStopped() []startupRow {
	rows := startupInProgress()
	rows[1].State = "failed"
	rows[1].Detail = "its healthcheck (port 9996) did not pass within 3m"
	rows[2].State = "blocked"
	rows[2].Detail = "denodo was not ready"
	return rows
}

func startupDone() []startupRow {
	rows := startupInProgress()
	rows[1].State = "ready"
	rows[1].Detail = "port 9996 accepting connections"
	rows[2].State = "ready"
	rows[2].Detail = `its healthcheck (log "Application startup complete") passed`
	rows[2].Lines = []string{"INFO:     Waiting for application startup.", "INFO:     Application startup complete."}
	return rows
}

func startupModel(width, height int, rows []startupRow) model {
	m := fixtureModel(width, height)
	m.sources = []string{startupTab, "running-man", "backend", "frontend", "Traces"}
	m.sourceTypes[startupTab] = "startup"
	m.sourceTypes["frontend"] = "process"
	m.selectedSource = 0
	m.startupRows = rows
	return m
}

func TestGoldenStartup(t *testing.T) {
	for _, tc := range []struct {
		name string
		m    model
	}{
		{"startup-in-progress", startupModel(100, 24, startupInProgress())},
		{"startup-stopped", startupModel(100, 24, startupStopped())},
		{"startup-done", startupModel(100, 24, startupDone())},
		{"startup-narrow", startupModel(60, 20, startupInProgress())},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertGolden(t, tc.name, tc.m.View())
		})
	}
}

func TestGoldenStartupColour(t *testing.T) {
	withProfile(t, termenv.TrueColor)
	assertGolden(t, "startup-stopped-colour", startupModel(100, 24, startupStopped()).View())
}
