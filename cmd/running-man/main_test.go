package main

import (
	"flag"
	"fmt"
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

func TestProcessFlags(t *testing.T) {
	var flags processFlags

	// Test initial state
	if flags.String() != "" {
		t.Errorf("Empty pf.String() = %q, want empty string", flags.String())
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
		t.Errorf("pf.String() = %q, want %q", str, expected)
	}
}

// parseFlagsAndValidate extracts flag parsing and validation for testing
func parseFlagsAndValidate(args []string) (procs []string, dockerCompose string, apiPort int, err error) {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	apiPortFlag := fs.Int("api-port", defaultAPIPort, "API server port")
	dockerComposeFlag := fs.String("docker-compose", "", "Path to docker-compose.yml file")

	var pf processFlags
	fs.Var(&pf, "process", "Process to run (can be specified multiple times)")

	// Parse flags
	if err := fs.Parse(args); err != nil {
		return nil, "", 0, err
	}

	// Validate
	if len(pf) == 0 && *dockerComposeFlag == "" {
		return nil, "", 0, fmt.Errorf("at least one --process flag or --docker-compose is required")
	}

	return pf, *dockerComposeFlag, *apiPortFlag, nil
}

func TestParseFlagsAndValidate(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantProcs   []string
		wantCompose string
		wantPort    int
		wantErr     bool
		errContains string
	}{
		{
			name:        "single process",
			args:        []string{"--process", "echo hello"},
			wantProcs:   []string{"echo hello"},
			wantCompose: "",
			wantPort:    9000,
			wantErr:     false,
		},
		{
			name:        "multiple processes",
			args:        []string{"--process", "echo hello", "--process", "echo world"},
			wantProcs:   []string{"echo hello", "echo world"},
			wantCompose: "",
			wantPort:    9000,
			wantErr:     false,
		},
		{
			name:        "docker-compose only",
			args:        []string{"--docker-compose", "./docker-compose.yml"},
			wantProcs:   []string{},
			wantCompose: "./docker-compose.yml",
			wantPort:    9000,
			wantErr:     false,
		},
		{
			name:        "process and docker-compose",
			args:        []string{"--process", "echo test", "--docker-compose", "./compose.yml"},
			wantProcs:   []string{"echo test"},
			wantCompose: "./compose.yml",
			wantPort:    9000,
			wantErr:     false,
		},
		{
			name:        "custom api port",
			args:        []string{"--process", "echo test", "--api-port", "8080"},
			wantProcs:   []string{"echo test"},
			wantCompose: "",
			wantPort:    8080,
			wantErr:     false,
		},
		{
			name:        "no sources - error",
			args:        []string{},
			wantErr:     true,
			errContains: "at least one",
		},
		{
			name:        "only api-port - error",
			args:        []string{"--api-port", "8080"},
			wantErr:     true,
			errContains: "at least one",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			procs, compose, port, err := parseFlagsAndValidate(tt.args)

			if (err != nil) != tt.wantErr {
				t.Errorf("parseFlagsAndValidate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}

			// Compare processes (handle nil vs empty slice)
			if len(procs) != len(tt.wantProcs) {
				t.Errorf("procs length = %d, want %d", len(procs), len(tt.wantProcs))
			} else {
				for i := range procs {
					if procs[i] != tt.wantProcs[i] {
						t.Errorf("procs[%d] = %v, want %v", i, procs[i], tt.wantProcs[i])
					}
				}
			}

			if compose != tt.wantCompose {
				t.Errorf("dockerCompose = %v, want %v", compose, tt.wantCompose)
			}

			if port != tt.wantPort {
				t.Errorf("apiPort = %v, want %v", port, tt.wantPort)
			}
		})
	}
}

func TestProcessConfigCreation(t *testing.T) {
	tests := []struct {
		name         string
		wraps        []string
		wantNames    []string
		wantCommands []string
	}{
		{
			name:         "single process",
			wraps:        []string{"echo hello"},
			wantNames:    []string{"echo-hello"},
			wantCommands: []string{"echo"},
		},
		{
			name:         "multiple processes",
			wraps:        []string{"echo hello", "npm run dev", "python server.py"},
			wantNames:    []string{"echo-hello", "npm-run-dev", "python-server-py"},
			wantCommands: []string{"echo", "npm", "python"},
		},
		{
			name:         "duplicate slugs get counters",
			wraps:        []string{"echo hello", "echo hello", "echo hello"},
			wantNames:    []string{"echo-hello", "echo-hello-2", "echo-hello-3"},
			wantCommands: []string{"echo", "echo", "echo"},
		},
		{
			name:         "complex commands",
			wraps:        []string{`sh -c "echo test"`, "python -m http.server"},
			wantNames:    []string{"sh-c-echo-test", "python-m-http-server"},
			wantCommands: []string{"sh", "python"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the process config creation logic
			nameMap := make(map[string]int)
			var configs []struct {
				Name    string
				Command string
			}

			for _, cmdStr := range tt.wraps {
				cmd, _, err := parseCommandString(cmdStr)
				if err != nil {
					t.Fatalf("parseCommandString(%q) failed: %v", cmdStr, err)
				}

				baseName := slugify(cmdStr)
				name := baseName

				if count, exists := nameMap[baseName]; exists {
					nameMap[baseName] = count + 1
					name = fmt.Sprintf("%s-%d", baseName, count+1)
				} else {
					nameMap[baseName] = 1
				}

				configs = append(configs, struct {
					Name    string
					Command string
				}{
					Name:    name,
					Command: cmd,
				})
			}

			// Verify names
			if len(configs) != len(tt.wantNames) {
				t.Fatalf("got %d configs, want %d", len(configs), len(tt.wantNames))
			}

			for i, cfg := range configs {
				if cfg.Name != tt.wantNames[i] {
					t.Errorf("config[%d].Name = %q, want %q", i, cfg.Name, tt.wantNames[i])
				}
				if cfg.Command != tt.wantCommands[i] {
					t.Errorf("config[%d].Command = %q, want %q", i, cfg.Command, tt.wantCommands[i])
				}
			}
		})
	}
}
