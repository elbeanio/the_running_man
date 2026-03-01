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
	"github.com/iangeorge/the_running_man/internal/config"
	"github.com/iangeorge/the_running_man/internal/docker"
	"github.com/iangeorge/the_running_man/internal/parser"
	"github.com/iangeorge/the_running_man/internal/process"
	"github.com/iangeorge/the_running_man/internal/storage"
	"github.com/iangeorge/the_running_man/internal/tracing"
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

// processFlags is a custom flag type for collecting multiple --process values
type processFlags []string

func (p *processFlags) String() string {
	return strings.Join(*p, ", ")
}

func (p *processFlags) Set(value string) error {
	*p = append(*p, value)
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
	configPath := fs.String("config", "", "Path to running-man.yml config file")
	apiPort := fs.Int("api-port", 0, "API server port (overrides config file)")
	dockerCompose := fs.String("docker-compose", "", "Path to docker-compose.yml file (overrides config file)")
	noTUI := fs.Bool("no-tui", false, "Disable TUI and run in headless mode")
	tracingEnabled := fs.Bool("tracing", true, "Enable OTLP trace ingestion (default: true)")
	tracingPort := fs.Int("tracing-port", 0, "OTLP HTTP receiver port (overrides config file, default: 4318)")

	var procs processFlags
	fs.Var(&procs, "process", "Process to run (can be specified multiple times, overrides config file)")

	fs.Parse(args)

	// Load config file if specified or found
	cfg, err := config.LoadConfigOrDefault(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Merge config with CLI flags (CLI flags take precedence)

	// Use config docker-compose if not provided via CLI
	finalDockerCompose := *dockerCompose
	if finalDockerCompose == "" && cfg != nil {
		finalDockerCompose = cfg.DockerCompose
	}

	// Use config API port if not provided via CLI
	finalAPIPort := *apiPort
	if finalAPIPort == 0 {
		if cfg != nil {
			finalAPIPort = cfg.GetAPIPort()
		} else {
			finalAPIPort = defaultAPIPort
		}
	}

	// Get retention, buffer, and shell settings from config (no CLI flags for these yet)
	finalRetention := defaultRetention
	finalMaxEntries := defaultMaxEntries
	finalMaxBytes := int64(defaultMaxBytes)
	finalShell := "/bin/sh"
	if cfg != nil {
		finalRetention = cfg.GetRetentionDuration()
		finalMaxEntries = cfg.GetMaxEntries()
		finalMaxBytes = cfg.GetMaxBytes()
		finalShell = cfg.GetShell()
	}

	// Get tracing configuration
	finalTracingEnabled := *tracingEnabled
	finalTracingPort := *tracingPort
	finalMaxSpans := config.DefaultMaxSpans
	finalMaxSpanAge := config.DefaultMaxSpanAge
	if cfg != nil {
		// CLI flags override config
		if !*tracingEnabled && *tracingEnabled != cfg.Tracing.IsEnabled() {
			finalTracingEnabled = false
		} else {
			finalTracingEnabled = cfg.Tracing.IsEnabled()
		}
		if finalTracingPort == 0 {
			finalTracingPort = cfg.Tracing.GetTracingPort()
		}
		finalMaxSpans = cfg.Tracing.GetMaxSpans()
		finalMaxSpanAge = cfg.Tracing.GetMaxSpanAgeDuration()
	} else {
		// Use defaults if no config
		if finalTracingPort == 0 {
			finalTracingPort = config.DefaultTracingPort
		}
	}

	// Parse process configurations
	var processes []process.ProcessConfig
	nameMap := make(map[string]int)

	// First, add processes from config file (if no --process flags provided)
	if len(procs) == 0 && cfg != nil {
		processes = cfg.ToProcessConfigs()
		// Build nameMap for config processes to avoid collisions
		for _, proc := range processes {
			nameMap[proc.Name] = 1
		}
	}

	// Then, add processes from CLI flags (these override config)
	for _, cmdStr := range procs {
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

		processes = append(processes, process.ProcessConfig{
			Name:    name,
			Command: cmd,
			Args:    cmdArgs,
			Shell:   finalShell,
		})
	}

	// Validate that we have at least one source
	if len(processes) == 0 && finalDockerCompose == "" {
		fmt.Fprintln(os.Stderr, "Error: At least one --process flag, --docker-compose, or config file with processes is required")
		printUsage()
		os.Exit(1)
	}

	fmt.Println("The Running Man - Dev Observability Tool")

	// Show running processes
	for _, proc := range processes {
		fmt.Printf("Running [%s]: %s %v\n", proc.Name, proc.Command, proc.Args)
	}

	fmt.Printf("API: http://localhost:%d\n\n", finalAPIPort)

	// Create ring buffer
	buffer := storage.NewRingBuffer(finalMaxEntries, finalRetention, finalMaxBytes)

	// Create tracing storage and receiver if enabled
	var tracingReceiver *tracing.Receiver
	if finalTracingEnabled {
		fmt.Printf("Tracing: OTLP receiver on http://localhost:%d\n", finalTracingPort)
		spanStorage := tracing.NewSpanStorage(finalMaxSpans, finalMaxSpanAge)
		tracingReceiver = tracing.NewReceiver(spanStorage, finalTracingPort)
	}

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

	if finalDockerCompose != "" {
		// Parse compose file
		compose, err := docker.ParseComposeFile(finalDockerCompose)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[running-man] Failed to parse docker-compose.yml: %v\n", err)
			os.Exit(1)
		}

		serviceNames := compose.GetServiceNames()
		if len(serviceNames) == 0 {
			fmt.Fprintf(os.Stderr, "[running-man] No services found in docker-compose.yml\n")
			os.Exit(1)
		}

		fmt.Printf("Docker Compose: %s (services: %s)\n", finalDockerCompose, strings.Join(serviceNames, ", "))

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
		containers, err := dockerClient.DiscoverContainers(ctx, finalDockerCompose, serviceNames)
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
	manager := process.NewManager(processes, lineHandler)

	// Start API server in background
	apiServer := api.NewServer(buffer, finalAPIPort, lineHandler, manager)
	go func() {
		if err := apiServer.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "[running-man] API server error: %v\n", err)
		}
	}()

	// Start tracing receiver in background if enabled
	if tracingReceiver != nil {
		go func() {
			if err := tracingReceiver.Start(); err != nil {
				fmt.Fprintf(os.Stderr, "[running-man] Tracing receiver error: %v\n", err)
			}
		}()
	}

	// Give API server time to start
	time.Sleep(100 * time.Millisecond)

	// Start all managed processes
	if err := manager.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "[running-man] Failed to start processes: %v\n", err)
		os.Exit(1)
	}

	// Launch TUI or run in headless mode (don't wait for processes to finish first)
	if *noTUI {
		// Headless mode - print info and wait for processes
		fmt.Printf("[running-man] API available at http://localhost:%d\n", finalAPIPort)
		fmt.Printf("[running-man] Running in headless mode (--no-tui)\n")
		fmt.Printf("[running-man] Press Ctrl+C to exit\n")

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
	} else {
		// TUI mode - launch interactive viewer immediately
		fmt.Printf("[running-man] Starting TUI viewer...\n")
		fmt.Printf("[running-man] API available at http://localhost:%d\n", finalAPIPort)
		time.Sleep(200 * time.Millisecond) // Give API a moment to stabilize

		// Run TUI with manager reference so it can stop processes on quit
		tuiCommandWithManager([]string{fmt.Sprintf("--api-port=%d", finalAPIPort)}, manager)

		// TUI exited (user pressed 'q') - stop processes and clean up
		fmt.Printf("\n[running-man] Shutting down processes...\n")

		// Stop all processes
		manager.Stop()

		// Wait for processes to finish stopping
		manager.Wait()

		// Stop all container streamers
		for _, streamer := range containerStreamers {
			streamer.Stop()
		}

		// Wait for container streamers to finish
		for _, streamer := range containerStreamers {
			streamer.Wait()
		}

		// Stop tracing receiver if enabled
		if tracingReceiver != nil {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := tracingReceiver.Stop(shutdownCtx); err != nil {
				fmt.Fprintf(os.Stderr, "[running-man] Tracing receiver shutdown error: %v\n", err)
			}
		}

		fmt.Printf("[running-man] Shutdown complete\n")
		// NOTE: API server (goroutine) is not gracefully shut down
		// It will be cleaned up when the program exits
		// TODO: Add api.Server.Shutdown() method for graceful shutdown
	}
}

func printUsage() {
	fmt.Print(`The Running Man - Dev Observability Tool

Usage:
  running-man run [--config PATH] [flags]
  running-man run --process "command" [--process "command" ...] [flags]
  running-man run --docker-compose PATH [--process "command" ...] [flags]
  running-man tui [--api-port PORT]
  running-man version
  running-man help

Flags:
  --config PATH            Path to running-man.yml config file
  --process "command"      Process to run (can be specified multiple times, overrides config)
  --docker-compose PATH    Path to docker-compose.yml file (overrides config)
  --api-port PORT          API server port (default: 9000, overrides config)
  --no-tui                 Disable TUI and run in headless mode

Examples:
  # Run a single process (TUI launches automatically)
  running-man run --process "python server.py"

  # Run multiple processes (TUI shows all sources with tab switching)
  running-man run --process "python server.py" --process "npm run dev"

  # Monitor Docker Compose services (TUI shows all containers)
  running-man run --docker-compose ./docker-compose.yml

  # Mix Docker and processes
  running-man run --docker-compose ./docker-compose.yml --process "npm run dev"

  # Headless mode for CI/automation (no TUI)
  running-man run --process "go run main.go" --no-tui

  # Connect TUI to existing running instance
  running-man tui --api-port 9000

  # Query logs via API while TUI is running (separate terminal)
  curl http://localhost:9000/logs?since=30s
  curl http://localhost:9000/errors
  curl http://localhost:9000/health

For more information, visit: github.com/iangeorge/the_running_man
`)
}
