package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestSlugify(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple command",
			input:    "echo hello world",
			expected: "echo-hello-world",
		},
		{
			name:     "mixed case",
			input:    "Echo HELLO",
			expected: "echo-hello",
		},
		{
			name:     "multiple spaces",
			input:    "echo  hello",
			expected: "echo-hello",
		},
		{
			name:     "special characters only",
			input:    "!!!!",
			expected: "process", // fallback
		},
		{
			name:     "empty string",
			input:    "",
			expected: "process", // fallback
		},
		{
			name:     "whitespace only",
			input:    "   ",
			expected: "process", // fallback
		},
		{
			name:     "very long string",
			input:    strings.Repeat("a", 100),
			expected: strings.Repeat("a", maxSlugLength),
		},
		{
			name:     "command with args",
			input:    "npm run dev --watch",
			expected: "npm-run-dev-watch",
		},
		{
			name:     "path with slashes",
			input:    "/usr/bin/python3 script.py",
			expected: "usr-bin-python3-script-py",
		},
		{
			name:     "trailing special chars",
			input:    "echo-hello---",
			expected: "echo-hello",
		},
		{
			name:     "unicode characters",
			input:    "echo café",
			expected: "echo-caf",
		},
		{
			name:     "long with truncation at dash",
			input:    strings.Repeat("ab-", 30),
			expected: "ab-ab-ab-ab-ab-ab-ab-ab-ab-ab-ab-ab-ab-ab-ab-ab-ab",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := slugify(tt.input)
			if got != tt.expected {
				t.Errorf("slugify(%q) = %q, want %q", tt.input, got, tt.expected)
			}
			// Verify it's within max length
			if len(got) > maxSlugLength {
				t.Errorf("slugify(%q) produced slug longer than %d: %d", tt.input, maxSlugLength, len(got))
			}
		})
	}
}

func TestParseCommandString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantCmd  string
		wantArgs []string
		wantErr  bool
	}{
		{
			name:     "simple command",
			input:    "echo hello",
			wantCmd:  "echo",
			wantArgs: []string{"hello"},
			wantErr:  false,
		},
		{
			name:     "command with multiple args",
			input:    "npm run dev",
			wantCmd:  "npm",
			wantArgs: []string{"run", "dev"},
			wantErr:  false,
		},
		{
			name:     "command with extra spaces",
			input:    "  echo  hello  ",
			wantCmd:  "echo",
			wantArgs: []string{"hello"},
			wantErr:  false,
		},
		{
			name:     "empty string",
			input:    "",
			wantCmd:  "",
			wantArgs: nil,
			wantErr:  true,
		},
		{
			name:     "whitespace only",
			input:    "   ",
			wantCmd:  "",
			wantArgs: nil,
			wantErr:  true,
		},
		{
			name:     "command with quoted args",
			input:    "echo 'hello world'",
			wantCmd:  "echo",
			wantArgs: []string{"hello world"},
			wantErr:  false,
		},
		{
			name:     "command with double quotes",
			input:    `echo "hello world"`,
			wantCmd:  "echo",
			wantArgs: []string{"hello world"},
			wantErr:  false,
		},
		{
			name:     "complex quoted command",
			input:    `curl -H "Content-Type: application/json"`,
			wantCmd:  "curl",
			wantArgs: []string{"-H", "Content-Type: application/json"},
			wantErr:  false,
		},
		{
			name:     "command only",
			input:    "python",
			wantCmd:  "python",
			wantArgs: []string{},
			wantErr:  false,
		},
		{
			name:     "command with flags",
			input:    "ls -la /tmp",
			wantCmd:  "ls",
			wantArgs: []string{"-la", "/tmp"},
			wantErr:  false,
		},
		{
			name:     "invalid unclosed quote",
			input:    `echo "hello`,
			wantCmd:  "",
			wantArgs: nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, args, err := parseCommandString(tt.input)

			if (err != nil) != tt.wantErr {
				t.Errorf("parseCommandString(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}

			if cmd != tt.wantCmd {
				t.Errorf("parseCommandString(%q) cmd = %q, want %q", tt.input, cmd, tt.wantCmd)
			}

			if !reflect.DeepEqual(args, tt.wantArgs) {
				t.Errorf("parseCommandString(%q) args = %v, want %v", tt.input, args, tt.wantArgs)
			}
		})
	}
}

func TestWrapFlags(t *testing.T) {
	var flags wrapFlags

	// Test initial state
	if flags.String() != "" {
		t.Errorf("Empty wrapFlags.String() = %q, want empty string", flags.String())
	}

	// Test adding values
	flags.Set("echo hello")
	flags.Set("npm run dev")

	if len(flags) != 2 {
		t.Errorf("After 2 Sets, len(flags) = %d, want 2", len(flags))
	}

	str := flags.String()
	expected := "echo hello, npm run dev"
	if str != expected {
		t.Errorf("wrapFlags.String() = %q, want %q", str, expected)
	}
}
