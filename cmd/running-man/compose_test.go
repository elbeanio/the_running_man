package main

import (
	"flag"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/elbeanio/the_running_man/internal/config"
	"github.com/elbeanio/the_running_man/internal/docker"
)

// Starting containers is a consequential thing to do to someone's machine, so
// only an explicit yes counts. Everything else -- including no answer at all --
// means no.
func TestConfirmComposeUp(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"y\n", true},
		{"Y\n", true},
		{"yes\n", true},
		{"YES\n", true},
		{"  y  \n", true},

		{"n\n", false},
		{"no\n", false},
		{"\n", false},      // bare return: the [y/N] default
		{"", false},        // EOF, e.g. stdin closed
		{"maybe\n", false}, // anything unrecognised
		{"ye\n", false},
		{"1\n", false},
	}

	for _, tt := range tests {
		got := confirmComposeUp(strings.NewReader(tt.input), "proj", "docker compose up -d")
		if got != tt.want {
			t.Errorf("confirmComposeUp(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

// Tracing is on by default, so removing every `tracing:` key from
// running-man.yml does not disable it -- absence means "use the default", and
// the default is enabled.
//
// That distinction decides what happens when the OTLP port is already taken.
// If tracing was asked for, a conflict must be fatal: silently not doing what
// was asked is the failure this replaced. If it is on only because the default
// is on, refusing to start would block the whole tool over a feature nobody
// requested -- which is the situation whenever another collector holds 4318,
// and Arize Phoenix, the OTel Collector and Jaeger all default to it.
func TestTracingRequested(t *testing.T) {
	enabled := true
	disabled := false

	tests := []struct {
		name  string
		flags []string
		cfg   *config.TracingConfig
		want  bool
	}{
		{"nothing set at all", nil, nil, false},
		{"empty tracing block", nil, &config.TracingConfig{}, false},
		{"enabled: true", nil, &config.TracingConfig{Enabled: &enabled}, true},
		{"enabled: false", nil, &config.TracingConfig{Enabled: &disabled}, true},
		{"port set in config", nil, &config.TracingConfig{Port: 4319}, true},
		{"--tracing passed", []string{"--tracing=true"}, nil, true},
		{"--tracing=false passed", []string{"--tracing=false"}, nil, true},
		{"--tracing-port passed", []string{"--tracing-port=4319"}, nil, true},
		{"unrelated flag only", []string{"--api-port=9000"}, nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Mirrors the resolution in runCommand: an explicitly set flag, or
			// an explicitly present config key, counts as a request.
			fs := flag.NewFlagSet("run", flag.ContinueOnError)
			fs.Bool("tracing", true, "")
			fs.Int("tracing-port", 0, "")
			fs.Int("api-port", 0, "")
			if err := fs.Parse(tt.flags); err != nil {
				t.Fatalf("parsing %v: %v", tt.flags, err)
			}

			got := false
			fs.Visit(func(f *flag.Flag) {
				if f.Name == "tracing" || f.Name == "tracing-port" {
					got = true
				}
			})
			if tt.cfg != nil && tt.cfg.Enabled != nil {
				got = true
			}
			if tt.cfg != nil && tt.cfg.Port != 0 {
				got = true
			}

			if got != tt.want {
				t.Errorf("tracingRequested = %v, want %v", got, tt.want)
			}
		})
	}
}

// A service is missing when it has no running container and has not run to
// completion: one-shot jobs (migrations, seeding) exit by design, and counting
// them as missing would ask to start the stack on every run.
func TestMissingServices(t *testing.T) {
	expected := []string{"db", "db-migrate", "denodo", "keycloak", "vault-setup"}
	running := []docker.Container{{ServiceName: "denodo"}}
	completed := map[string]bool{"db-migrate": true}

	got := missingServices(expected, running, completed)
	want := []string{"db", "keycloak", "vault-setup"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("missing = %v, want %v", got, want)
	}

	if got := missingServices([]string{"db"}, []docker.Container{{ServiceName: "db"}}, nil); len(got) != 0 {
		t.Errorf("nothing should be missing: %v", got)
	}
}

// The prompt for a partly running stack names what is missing and the
// command, and only an explicit yes counts -- the same rule as starting the
// whole stack.
func TestConfirmStartMissing(t *testing.T) {
	for input, want := range map[string]bool{"y\n": true, "yes\n": true, "\n": false, "": false, "n\n": false} {
		out := captureStdout(t, func() {
			if got := confirmStartMissing(strings.NewReader(input), "duet",
				[]string{"db", "keycloak"}, "docker compose up -d --no-recreate db keycloak"); got != want {
				t.Errorf("answer %q: got %v, want %v", input, got, want)
			}
		})
		for _, s := range []string{"2 services", "not running: db, keycloak", "--no-recreate db keycloak"} {
			if !strings.Contains(out, s) {
				t.Errorf("prompt does not mention %q:\n%s", s, out)
			}
		}
	}
}

// captureStdout returns what fn printed.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	w.Close()
	os.Stdout = saved
	return <-done
}
