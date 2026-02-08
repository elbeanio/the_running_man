package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/iangeorge/the_running_man/internal/api"
	"github.com/iangeorge/the_running_man/internal/docker"
	"github.com/iangeorge/the_running_man/internal/parser"
	"github.com/iangeorge/the_running_man/internal/storage"
	"github.com/iangeorge/the_running_man/internal/wrapper"
	"github.com/kballard/go-shellquote"
)

const (
	defaultAPIPort    = 9000
	defaultRetention  = 30 * time.Minute
	defaultMaxEntries = 10000
	defaultMaxBytes   = 50 * 1024 * 1024 // 50MB
	maxSlugLength     = 50
)

var (
	// Compiled regexes for slugification (performance optimization)
	nonAlphanumericRegex = regexp.MustCompile(`[^a-z0-9]+`)
	multipleDashesRegex  = regexp.MustCompile(`-+`)
)

// wrapFlags is a custom flag type for collecting multiple --wrap values
type wrapFlags []string

func (w *wrapFlags) String() string {
	return strings.Join(*w, ", ")
}

func (w *wrapFlags) Set(value string) error {
	*w = append(*w, value)
	return nil
}

// slugify converts a string to a URL-friendly slug
func slugify(s string) string {
	// Convert to lowercase
	s = strings.ToLower(s)

	// Replace non-alphanumeric characters with dashes
	s = nonAlphanumericRegex.ReplaceAllString(s, "-")

	// Remove leading and trailing dashes
	s = strings.Trim(s, "-")

	// Replace multiple consecutive dashes with single dash
	s = multipleDashesRegex.ReplaceAllString(s, "-")

	// Enforce max length
	if len(s) > maxSlugLength {
		s = s[:maxSlugLength]
		s = strings.TrimRight(s, "-")
	}

	// Fallback for empty slugs
	if s == "" {
		s = "process"
	}

	return s
}

// parseCommandString splits a command string into command and arguments
// Uses shellquote to properly handle quoted arguments
func parseCommandString(cmdStr string) (string, []string, error) {
	cmdStr = strings.TrimSpace(cmdStr)
	if cmdStr == "" {
		return "", nil, fmt.Errorf("empty command string")
	}

	parts, err := shellquote.Split(cmdStr)
	if err != nil {
		return "", nil, fmt.Errorf("invalid command syntax: %w", err)
	}

	if len(parts) == 0 {
		return "", nil, fmt.Errorf("no command found")
	}

	return parts[0], parts[1:], nil
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "run":
		runCommand(os.Args[2:])
	case "tui":
		tuiCommand(os.Args[2:])
	case "version":
		fmt.Println("The Running Man v0.1.0 (Phase 1)")
		os.Exit(0)
	case "help", "--help", "-h":
		printUsage()
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func runCommand(args []string) {
	// Setup flags
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	apiPort := fs.Int("api-port", defaultAPIPort, "API server port")
	dockerCompose := fs.String("docker-compose", "", "Path to docker-compose.yml file")

	var wraps wrapFlags
	fs.Var(&wraps, "wrap", "Process to wrap (can be specified multiple times)")

	fs.Parse(args)

	if len(wraps) == 0 && *dockerCompose == "" {
		fmt.Fprintln(os.Stderr, "Error: At least one --wrap flag or --docker-compose is required")
		printUsage()
		os.Exit(1)
	}

	// Parse wrapped processes
	var processes []wrapper.ProcessConfig
	nameMap := make(map[string]int)

	for _, cmdStr := range wraps {
		cmd, cmdArgs, err := parseCommandString(cmdStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing command '%s': %v\n", cmdStr, err)
			os.Exit(1)
		}

		// Generate slug name from the full command string
		baseName := slugify(cmdStr)
		name := baseName

		// Handle collisions by appending counter
		if count, exists := nameMap[baseName]; exists {
			nameMap[baseName] = count + 1
			name = fmt.Sprintf("%s-%d", baseName, count+1)
		} else {
			nameMap[baseName] = 1
		}

		processes = append(processes, wrapper.ProcessConfig{
			Name:    name,
			Command: cmd,
			Args:    cmdArgs,
		})
	}

	fmt.Println("The Running Man - Dev Observability Tool")

	// Show wrapped processes
	for _, proc := range processes {
		fmt.Printf("Wrapping [%s]: %s %v\n", proc.Name, proc.Command, proc.Args)
	}

	fmt.Printf("API: http://localhost:%d\n\n", *apiPort)

	// Create ring buffer
	buffer := storage.NewRingBuffer(defaultMaxEntries, defaultRetention, defaultMaxBytes)

	// Create parser
	multiParser := parser.NewMultiParser()

	// Setup line handler
	lineHandler := func(source string, line string, timestamp time.Time, isStderr bool) {
		entry := multiParser.ParseLine(source, line, timestamp)
		if entry != nil {
			buffer.Append(entry)
		}
	}

	// Docker Compose integration
	var containerStreamers []*docker.ContainerStreamer
	var dockerClient *docker.Client
	ctx := context.Background()

	if *dockerCompose != "" {
		// Parse compose file
		compose, err := docker.ParseComposeFile(*dockerCompose)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[running-man] Failed to parse docker-compose.yml: %v\n", err)
			os.Exit(1)
		}

		serviceNames := compose.GetServiceNames()
		if len(serviceNames) == 0 {
			fmt.Fprintf(os.Stderr, "[running-man] No services found in docker-compose.yml\n")
			os.Exit(1)
		}

		fmt.Printf("Docker Compose: %s (services: %s)\n", *dockerCompose, strings.Join(serviceNames, ", "))

		// Create Docker client
		dockerClient, err = docker.NewClient()
		if err != nil {
			fmt.Fprintf(os.Stderr, "[running-man] Failed to create Docker client: %v\n", err)
			fmt.Fprintf(os.Stderr, "[running-man] Make sure Docker is running and accessible\n")
			os.Exit(1)
		}
		defer dockerClient.Close()

		// Check Docker availability
		if err := dockerClient.Ping(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "[running-man] Docker daemon not available: %v\n", err)
			os.Exit(1)
		}

		// Discover running containers
		containers, err := dockerClient.DiscoverContainers(ctx, *dockerCompose, serviceNames)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[running-man] Failed to discover containers: %v\n", err)
			os.Exit(1)
		}

		if len(containers) == 0 {
			fmt.Fprintf(os.Stderr, "[running-man] No running containers found for docker-compose project\n")
			fmt.Fprintf(os.Stderr, "[running-man] Make sure to run 'docker-compose up' first\n")
			os.Exit(1)
		}

		fmt.Printf("Found %d running container(s):\n", len(containers))
		for _, container := range containers {
			fmt.Printf("  - [%s] %s\n", container.Name, container.ID[:12])
		}

		// Start log streamers for each container
		for _, container := range containers {
			streamer := docker.NewContainerStreamer(dockerClient, container.ID, container.Name, lineHandler)
			if err := streamer.Start(); err != nil {
				fmt.Fprintf(os.Stderr, "[running-man] Failed to start log streamer for %s: %v\n", container.Name, err)
				continue
			}
			containerStreamers = append(containerStreamers, streamer)
		}

		fmt.Println()
	}

	// Create process manager for all processes
	manager := wrapper.NewManager(processes, lineHandler)

	// Start API server in background
	apiServer := api.NewServer(buffer, *apiPort, lineHandler)
	go func() {
		if err := apiServer.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "[running-man] API server error: %v\n", err)
		}
	}()

	// Give API server time to start
	time.Sleep(100 * time.Millisecond)

	// Start all wrapped processes
	if err := manager.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "[running-man] Failed to start processes: %v\n", err)
		os.Exit(1)
	}

	// Wait for all processes to complete
	err := manager.Wait()

	// Stop all container streamers
	for _, streamer := range containerStreamers {
		streamer.Stop()
	}

	// Wait for container streamers to finish
	for _, streamer := range containerStreamers {
		streamer.Wait()
	}

	// Flush any remaining multi-line entries for all processes
	for _, proc := range processes {
		if flushed := multiParser.Flush(proc.Name); flushed != nil {
			buffer.Append(flushed)
		}
	}

	// Flush container streamers (if any)
	if *dockerCompose != "" {
		compose, _ := docker.ParseComposeFile(*dockerCompose)
		for _, serviceName := range compose.GetServiceNames() {
			if flushed := multiParser.Flush(serviceName); flushed != nil {
				buffer.Append(flushed)
			}
		}
	}

	// Get exit codes
	exitCodes := manager.ExitCodes()

	if err != nil {
		fmt.Fprintf(os.Stderr, "\n[running-man] One or more processes exited with error: %v\n", err)
	} else {
		fmt.Printf("\n[running-man] All processes completed successfully\n")
	}

	// Print exit codes for each process
	for name, code := range exitCodes {
		if code != 0 {
			fmt.Fprintf(os.Stderr, "[running-man] Process %s exited with code %d\n", name, code)
		}
	}

	fmt.Printf("[running-man] Captured %d log entries\n", buffer.Stats().TotalEntries)
	fmt.Printf("[running-man] API still available at http://localhost:%d\n", *apiPort)
	fmt.Printf("[running-man] Press Ctrl+C to exit\n")

	// Keep API server running (blocks forever - exit with Ctrl+C)
	select {}
}

func printUsage() {
	fmt.Print(`The Running Man - Dev Observability Tool

Usage:
  running-man run --wrap "command" [--wrap "command" ...] [flags]
  running-man run --docker-compose PATH [--wrap "command" ...] [flags]
  running-man tui [--api-port PORT]
  running-man version
  running-man help

Flags:
  --wrap "command"         Process to wrap (can be specified multiple times)
  --docker-compose PATH    Path to docker-compose.yml file
  --api-port PORT          API server port (default: 9000)

Examples:
  # Wrap a single process
  running-man run --wrap "python server.py"

  # Wrap multiple processes
  running-man run --wrap "python server.py" --wrap "npm run dev"

  # Tail Docker Compose services
  running-man run --docker-compose ./docker-compose.yml

  # Mix Docker and processes
  running-man run --docker-compose ./docker-compose.yml --wrap "npm run dev"

  # Custom API port
  running-man run --wrap "go run main.go" --api-port 8080

  # Query logs while running
  curl http://localhost:9000/logs?since=30s
  curl http://localhost:9000/errors
  curl http://localhost:9000/health

  # Launch TUI log viewer (connects to running instance)
  running-man tui
  running-man tui --api-port 8080

For more information, visit: github.com/iangeorge/the_running_man
`)
}
