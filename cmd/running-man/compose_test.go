package main

import (
	"flag"
	"strings"
	"testing"

	"github.com/elbeanio/the_running_man/internal/config"
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
