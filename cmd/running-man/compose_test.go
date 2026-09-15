package main

import (
	"strings"
	"testing"
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
