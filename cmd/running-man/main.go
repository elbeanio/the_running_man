package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/elbeanio/the_running_man/internal/api"
	"github.com/elbeanio/the_running_man/internal/config"
	"github.com/elbeanio/the_running_man/internal/docker"
	"github.com/elbeanio/the_running_man/internal/instance"
	"github.com/elbeanio/the_running_man/internal/parser"
	"github.com/elbeanio/the_running_man/internal/process"
	"github.com/elbeanio/the_running_man/internal/storage"
	"github.com/elbeanio/the_running_man/internal/tracing"
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

// stringListFlag collects a repeatable string flag, e.g. --compose-profile.
type stringListFlag []string

func (f *stringListFlag) String() string { return strings.Join(*f, ",") }

func (f *stringListFlag) Set(value string) error {
	// Accept both repetition and a comma-separated list, since both are natural
	// and there is no reason to be fussy about it.
	for _, part := range strings.Split(value, ",") {
		if part = strings.TrimSpace(part); part != "" {
			*f = append(*f, part)
		}
	}
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
		TuiCommand(os.Args[2:])
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

// keepAliveMode decides what happens in headless mode once every process has
// exited.
const (
	keepAliveAuto   = "auto"
	keepAliveAlways = "always"
	keepAliveNever  = "never"
)

// stdoutIsTerminal reports whether stdout is a terminal, i.e. whether a human
// is plausibly watching.
func stdoutIsTerminal() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// shouldKeepAlive decides whether to hold the API and buffer open after a
// process has failed.
//
// The README promises "stay running when your apps crash", and the buffer is
// in-memory, so exiting immediately discards the logs explaining the crash --
// exactly when they are wanted. But holding the process open unconditionally
// would hang CI on the very failure CI exists to report.
//
// So the default is to keep the logs only when a human is plausibly watching:
// stdout a terminal means interactive, a pipe or file means automation.
func shouldKeepAlive(mode string) bool {
	switch mode {
	case keepAliveAlways:
		return true
	case keepAliveNever:
		return false
	default:
		return stdoutIsTerminal()
	}
}

// waitForInterrupt blocks until SIGINT or SIGTERM.
//
// Registers its own channel: the process manager also watches for signals, and
// signal.Notify delivers to every registered channel, so both see it.
func waitForInterrupt() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigChan)
	<-sigChan
}

// networkPostureNote summarises who can reach the API, for the startup banner.
// This is the one moment the user is guaranteed to be looking, and "reachable
// from the network" is worth knowing before logs start flowing through it.
func networkPostureNote(listenAddr string, allowRemoteControl bool) string {
	if ip := net.ParseIP(listenAddr); ip != nil && ip.IsLoopback() {
		return " (this machine only)"
	}
	if allowRemoteControl {
		return " (reachable on all interfaces; process control OPEN to remote callers)"
	}
	return " (reachable on all interfaces; process control local-only)"
}

func runCommand(args []string) {
	// Setup flags
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to running-man.yml config file")
	apiPort := fs.Int("api-port", 0, "API server port (overrides config file)")
	dockerCompose := fs.String("docker-compose", "", "Path to docker-compose.yml file (overrides config file)")
	noTUI := fs.Bool("no-tui", false, "Disable TUI and run in headless mode")
	listenAddr := fs.String("listen", api.DefaultListenAddr,
		"Address to bind the API to (use 127.0.0.1 to restrict to this machine)")
	allowRemoteControl := fs.Bool("allow-remote-control", false,
		"Serve process restart/stop endpoints to remote callers (default: loopback only)")
	composeProfiles := &stringListFlag{}
	fs.Var(composeProfiles, "compose-profile",
		"Active Docker Compose profile (repeatable; overrides config)")
	composeProject := fs.String("compose-project", "",
		"Docker Compose project name (overrides config and the directory-name default)")
	composeStart := fs.String("compose-start", "",
		"When the Compose stack is not running: ask|never|always (default: ask)")
	composeStartTimeout := fs.String("compose-start-timeout", "",
		"How long to wait for containers after starting the stack, e.g. 5m (default: 30s)")
	keepAlive := fs.String("keep-alive", keepAliveAuto,
		"After a process fails in headless mode, keep serving its logs: auto|always|never "+
			"(auto = only when stdout is a terminal)")
	tracingEnabled := fs.Bool("tracing", true, "Enable OTLP trace ingestion (default: true)")
	tracingPort := fs.Int("tracing-port", 0, "OTLP HTTP receiver port (overrides config file, default: 4318)")

	var procs processFlags
	fs.Var(&procs, "process", "Process to run (can be specified multiple times, overrides config file)")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing flags: %v\n", err)
		os.Exit(1)
	}

	// Load config file if specified or found
	cfg, err := config.LoadConfigOrDefault(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Merge config with CLI flags (CLI flags take precedence)

	// Resolve the Compose configuration: config file first, then CLI overrides.
	var finalCompose config.DockerComposeConfig
	if cfg != nil {
		finalCompose = cfg.DockerCompose
	}
	if *dockerCompose != "" {
		// An explicit --docker-compose replaces the configured file list rather
		// than adding to it, matching how the other overrides behave.
		finalCompose.Files = []string{*dockerCompose}
	}
	if len(*composeProfiles) > 0 {
		finalCompose.Profiles = *composeProfiles
	}
	if *composeProject != "" {
		finalCompose.ProjectName = *composeProject
	}
	if *composeStart != "" {
		finalCompose.Start = *composeStart
	}
	if *composeStartTimeout != "" {
		finalCompose.StartTimeout = *composeStartTimeout
	}
	if err := finalCompose.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	finalDockerCompose := finalCompose.PrimaryFile()

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

	// Simple logic: CLI flag overrides config
	// If user specifies --tracing=false, disable regardless of config
	// Otherwise, if config exists, use its value (defaults to true)
	// If no config, use CLI flag value (defaults to true)

	if cfg != nil {
		// We have a config file
		if !*tracingEnabled {
			// User explicitly disabled tracing via CLI flag
			finalTracingEnabled = false
		} else {
			// Use config value (defaults to true)
			finalTracingEnabled = cfg.Tracing.IsEnabled()
		}

		if finalTracingPort == 0 {
			finalTracingPort = cfg.Tracing.GetTracingPort()
		}
		finalMaxSpans = cfg.Tracing.GetMaxSpans()
		finalMaxSpanAge = cfg.Tracing.GetMaxSpanAgeDuration()
	} else {
		// No config file, use CLI flag value (defaults to true)
		// finalTracingEnabled already set to *tracingEnabled
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

	fmt.Printf("API: http://localhost:%d%s\n\n", finalAPIPort, networkPostureNote(*listenAddr, *allowRemoteControl))

	// Create ring buffer
	buffer := storage.NewRingBuffer(finalMaxEntries, finalRetention, finalMaxBytes)

	// Create tracing storage and receiver if enabled
	var tracingReceiver *tracing.Receiver
	var spanStorage *tracing.SpanStorage
	if finalTracingEnabled {
		fmt.Printf("Tracing: OTLP receiver on http://localhost:%d\n", finalTracingPort)
		spanStorage = tracing.NewSpanStorage(finalMaxSpans, finalMaxSpanAge)
		tracingReceiver = tracing.NewReceiver(spanStorage, buffer, finalTracingPort)
	}

	// Create parser
	multiParser := parser.NewMultiParser()

	// Setup line handlers.
	//
	// A single line can yield more than one entry: a line that is not part of a
	// Python traceback but arrives while one is open both completes the
	// traceback and is a log line itself.
	appendEntries := func(entries []*parser.LogEntry) {
		for _, entry := range entries {
			if entry != nil {
				buffer.Append(entry)
			}
		}
	}

	// isStderr is passed through rather than discarded: for a failure message
	// that matches none of the level patterns, it is the only signal there is.
	parse := func(source, sourceType, line string, timestamp time.Time, isStderr bool) []*parser.LogEntry {
		if isStderr {
			return multiParser.ParseStderrLine(source, sourceType, line, timestamp)
		}
		return multiParser.ParseLineWithType(source, sourceType, line, timestamp)
	}

	processLineHandler := func(source string, line string, timestamp time.Time, isStderr bool) {
		appendEntries(parse(source, "process", line, timestamp, isStderr))
	}

	dockerLineHandler := func(source string, line string, timestamp time.Time, isStderr bool) {
		appendEntries(parse(source, "docker", line, timestamp, isStderr))
	}

	systemLineHandler := func(source string, line string, timestamp time.Time, isStderr bool) {
		appendEntries(parse(source, "system", line, timestamp, isStderr))
	}

	// Docker Compose integration
	var containerStreamers []*docker.ContainerStreamer
	var dockerClient *docker.Client
	ctx := context.Background()

	if finalCompose.IsSet() {
		// Parse every configured Compose file, merging by service name.
		compose, err := docker.ParseComposeFiles(finalCompose.Files)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[running-man] Failed to parse compose file(s): %v\n", err)
			os.Exit(1)
		}

		// Only expect services the active profiles would actually start.
		serviceNames := compose.ServiceNamesForProfiles(finalCompose.Profiles)
		if len(serviceNames) == 0 {
			fmt.Fprintf(os.Stderr,
				"[running-man] No services to watch: every service in %s is gated behind a profile\n",
				strings.Join(finalCompose.Files, ", "))
			if len(finalCompose.Profiles) == 0 {
				fmt.Fprintf(os.Stderr, "[running-man] Set docker_compose.profiles or --compose-profile to activate one\n")
			}
			os.Exit(1)
		}

		fmt.Printf("Docker Compose: %s\n", strings.Join(finalCompose.Files, ", "))
		if len(finalCompose.Profiles) > 0 {
			fmt.Printf("  profiles: %s\n", strings.Join(finalCompose.Profiles, ", "))
		}
		fmt.Printf("  services: %s\n", strings.Join(serviceNames, ", "))
		// Say what is being left out, rather than silently ignoring it.
		if gated := compose.ServicesGatedOut(finalCompose.Profiles); len(gated) > 0 {
			fmt.Printf("  not watched (inactive profiles): %s\n", strings.Join(gated, ", "))
		}

		// Create Docker client
		dockerClient, err = docker.NewClient()
		if err != nil {
			fmt.Fprintf(os.Stderr, "[running-man] Failed to create Docker client: %v\n", err)
			fmt.Fprintf(os.Stderr, "[running-man] Make sure Docker is running and accessible\n")
			os.Exit(1)
		}
		defer dockerClient.Close()

		// Check Docker availability
		if err = dockerClient.Ping(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "[running-man] Docker daemon not available: %v\n", err)
			os.Exit(1)
		}

		projectName := finalCompose.ProjectName
		if projectName == "" {
			projectName = docker.GetProjectNameFromPath(finalCompose.PrimaryFile())
		}

		discover := func() ([]docker.Container, error) {
			return dockerClient.DiscoverContainersInProject(ctx, projectName, serviceNames)
		}

		containers, err := discover()
		if err != nil {
			fmt.Fprintf(os.Stderr, "[running-man] Failed to discover containers: %v\n", err)
			os.Exit(1)
		}

		if len(containers) == 0 {
			// Nothing is up. Offer to start it -- Running Man does not manage
			// the stack, so this is an offer, and whatever it starts is left
			// running when Running Man exits.
			containers, err = offerToStartCompose(ctx, finalCompose, projectName, discover)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[running-man] %v\n", err)
				os.Exit(1)
			}
		}

		fmt.Printf("Found %d running container(s):\n", len(containers))
		for _, container := range containers {
			fmt.Printf("  - [%s] %s\n", container.Name, container.ID[:12])
		}

		// Report expected services that are not running, rather than quietly
		// watching a partial stack.
		running := make(map[string]bool, len(containers))
		for _, c := range containers {
			running[c.ServiceName] = true
		}
		var missing []string
		for _, name := range serviceNames {
			if !running[name] {
				missing = append(missing, name)
			}
		}
		if len(missing) > 0 {
			fmt.Printf("  not running: %s\n", strings.Join(missing, ", "))
		}

		// Start log streamers for each container
		for _, container := range containers {
			streamer := docker.NewContainerStreamer(dockerClient, container.ID, container.Name, dockerLineHandler)
			if err := streamer.Start(); err != nil {
				fmt.Fprintf(os.Stderr, "[running-man] Failed to start log streamer for %s: %v\n", container.Name, err)
				continue
			}
			containerStreamers = append(containerStreamers, streamer)
		}

		fmt.Println()
	}

	// Create process manager for all processes
	var manager *process.Manager
	if finalTracingEnabled {
		// Use OTEL-enabled manager
		otelEndpoint := "http://localhost"
		// Silent mode when TUI is running (not headless mode)
		manager = process.NewManagerWithOTEL(processes, processLineHandler, otelEndpoint, finalTracingPort, true, !*noTUI)
	} else {
		// Use regular manager
		// Silent mode when TUI is running (not headless mode)
		manager = process.NewManagerWithOTEL(processes, processLineHandler, "", 0, false, !*noTUI)
	}

	// Start API server in background
	var traceStorage *tracing.SpanStorage
	if spanStorage != nil {
		traceStorage = spanStorage
	}
	apiServer := api.NewServer(buffer, finalAPIPort, systemLineHandler, manager, traceStorage)
	apiServer.SetListenAddr(*listenAddr)
	apiServer.SetAllowRemoteControl(*allowRemoteControl)
	go func() {
		if err := apiServer.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "[running-man] API server error: %v\n", err)
		}
	}()

	// Start tracing receiver and wait for it to be ready if enabled
	if tracingReceiver != nil {
		if err := tracingReceiver.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "[running-man] Tracing receiver error: %v\n", err)
			os.Exit(1)
		}

		// Wait for receiver to be ready before starting processes
		fmt.Printf("[running-man] Waiting for OTEL receiver to be ready...\n")
		if err := tracingReceiver.WaitForReady(10 * time.Second); err != nil {
			fmt.Fprintf(os.Stderr, "[running-man] OTEL receiver failed to start: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("[running-man] OTEL receiver ready on http://localhost:%d\n", finalTracingPort)
	}

	// Give API server time to start
	time.Sleep(100 * time.Millisecond)

	// Start all managed processes
	if err := manager.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "[running-man] Failed to start processes: %v\n", err)
		os.Exit(1)
	}

	// Write the instance marker so anything inspecting the project directory can
	// discover this instance cheaply -- notably a coding agent about to start a
	// dev server that is already running here.
	//
	// Written after the processes start so it is never present without an
	// instance behind it, and removed on every shutdown path below.
	projectDir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[running-man] Could not determine working directory: %v\n", err)
		projectDir = "."
	}

	marker := instance.New(
		fmt.Sprintf("http://localhost:%d", finalAPIPort),
		projectDir,
		markerProcesses(processes),
	)
	if err := marker.Write(projectDir); err != nil {
		// Not fatal: the marker is a discovery aid, and losing it must not stop
		// the tool doing its actual job.
		fmt.Fprintf(os.Stderr, "[running-man] Could not write instance marker: %v\n", err)
	} else {
		fmt.Printf("[running-man] Instance marker: %s\n", instance.Path(projectDir))
	}

	// Remove the marker however we leave: normal return, os.Exit paths below,
	// and signals. A marker outliving its instance points an agent at a dead
	// API, which is worse than no marker at all.
	removeMarker := func() {
		if err := instance.Remove(projectDir); err != nil {
			fmt.Fprintf(os.Stderr, "[running-man] Could not remove instance marker: %v\n", err)
		}
	}
	defer removeMarker()

	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan
		// The process manager handles the signal too and stops the processes;
		// this only cleans up the marker, because a deferred call does not run
		// when the process is signalled.
		removeMarker()
	}()

	// Launch TUI or run in headless mode (don't wait for processes to finish first)
	if *noTUI {
		// Headless mode - print info and wait for processes
		fmt.Printf("[running-man] API available at http://localhost:%d\n", finalAPIPort)
		fmt.Printf("[running-man] Running in headless mode (--no-tui)\n")
		fmt.Printf("[running-man] Press Ctrl+C to exit\n")

		// Wait for all processes to complete
		err := manager.Wait()

		// A Compose-only configuration has no managed processes, so Wait()
		// returns immediately -- and streaming container logs is the entire job
		// in that case, so exiting here would make `running-man run
		// --docker-compose ...` useless.
		//
		// This was broken by the Wait() fix: beforehand, Wait() blocked on
		// ctx.Done() forever, which accidentally kept Compose-only runs alive.
		// Now it is deliberate.
		if len(processes) == 0 && len(containerStreamers) > 0 {
			fmt.Printf("\n[running-man] Streaming container logs. Press Ctrl+C to quit.\n")
			fmt.Printf("[running-man] The Compose stack will be left running.\n")
			waitForInterrupt()
			fmt.Printf("\n[running-man] Exiting.\n")
		}

		// Stop all container streamers
		for _, streamer := range containerStreamers {
			if err := streamer.Stop(); err != nil {
				fmt.Fprintf(os.Stderr, "[running-man] Failed to stop container streamer: %v\n", err)
			}
		}

		// Wait for container streamers to finish
		for _, streamer := range containerStreamers {
			if err := streamer.Wait(); err != nil {
				fmt.Fprintf(os.Stderr, "[running-man] Error waiting for container streamer: %v\n", err)
			}
		}

		// Get exit codes
		exitCodes := manager.ExitCodes()

		failed := err != nil
		for _, code := range exitCodes {
			if code != 0 {
				failed = true
			}
		}

		if failed {
			if err != nil {
				fmt.Fprintf(os.Stderr, "\n[running-man] One or more processes exited with error: %v\n", err)
			} else {
				fmt.Fprintf(os.Stderr, "\n[running-man] One or more processes exited with a non-zero code\n")
			}
		} else {
			fmt.Printf("\n[running-man] All processes completed successfully\n")
		}

		// Print exit codes for each process
		for name, code := range exitCodes {
			if code != 0 {
				fmt.Fprintf(os.Stderr, "[running-man] Process %s exited with code %d\n", name, code)
			}
		}

		// Hold the buffer open so the logs explaining a crash survive it. The
		// buffer is in-memory, so exiting here destroys exactly the evidence
		// someone wants. Gated so CI is not left hanging -- see shouldKeepAlive.
		if failed && shouldKeepAlive(*keepAlive) {
			fmt.Printf("\n[running-man] Processes have exited, but their logs are still available:\n")
			fmt.Printf("[running-man]   http://localhost:%d/logs\n", finalAPIPort)
			fmt.Printf("[running-man]   http://localhost:%d/errors\n", finalAPIPort)
			fmt.Printf("[running-man] Press Ctrl+C to quit (--keep-alive=never to exit immediately).\n")
			waitForInterrupt()
			fmt.Printf("\n[running-man] Exiting.\n")
		}

		if failed {
			// Report failure to the caller. Headless mode is the CI/automation
			// path, and it previously exited 0 regardless, so a failing process
			// looked like success.
			//
			// os.Exit skips deferred calls, so the marker is removed explicitly.
			removeMarker()
			os.Exit(1)
		}
	} else {
		// TUI mode - launch interactive viewer immediately
		fmt.Printf("[running-man] Starting TUI viewer...\n")
		fmt.Printf("[running-man] API available at http://localhost:%d\n", finalAPIPort)
		time.Sleep(200 * time.Millisecond) // Give API a moment to stabilize

		// Run TUI with manager reference so it can stop processes on quit
		TuiCommandWithManager([]string{fmt.Sprintf("--api-port=%d", finalAPIPort)}, manager)

		// TUI exited (user pressed 'q') - stop processes and clean up
		fmt.Printf("\n[running-man] Shutting down processes...\n")

		// Stop all processes
		if err := manager.Stop(); err != nil {
			fmt.Fprintf(os.Stderr, "[running-man] Failed to stop manager: %v\n", err)
		}

		// Wait for processes to finish stopping
		if err := manager.Wait(); err != nil {
			fmt.Fprintf(os.Stderr, "[running-man] Error waiting for manager: %v\n", err)
		}

		// Stop all container streamers
		for _, streamer := range containerStreamers {
			if err := streamer.Stop(); err != nil {
				fmt.Fprintf(os.Stderr, "[running-man] Failed to stop container streamer: %v\n", err)
			}
		}

		// Wait for container streamers to finish
		for _, streamer := range containerStreamers {
			if err := streamer.Wait(); err != nil {
				fmt.Fprintf(os.Stderr, "[running-man] Error waiting for container streamer: %v\n", err)
			}
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
  --compose-profile NAME   Active Compose profile (repeatable, or comma-separated)
  --compose-project NAME   Compose project name (overrides the directory-name default)
  --compose-start MODE     When the stack is not running: ask|never|always
                           (default: ask; ask needs a terminal, so CI behaves as never)
  --compose-start-timeout DURATION
                           How long to wait for containers after starting the stack
                           (default: 30s; raise it for a stack that migrates a
                           database or starts services in sequence)
  --api-port PORT          API server port (default: 9000, overrides config)
  --listen ADDR            Address to bind the API to (default: 0.0.0.0, all
                           interfaces). Use 127.0.0.1 to restrict to this machine.
  --allow-remote-control   Serve process restart/stop endpoints to remote callers.
                           By default they are loopback-only and return 403.
  --no-tui                 Disable TUI and run in headless mode
  --keep-alive MODE        After a process fails in headless mode, keep serving its
                           logs: auto|always|never (default: auto, which keeps them
                           only when stdout is a terminal, so CI is not left hanging)

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

For more information, visit: github.com/elbeanio/the_running_man
`)
}

// markerProcesses converts the resolved process configs into the marker's
// stable view of them.
//
// Deliberately omits anything live (status, pid, ports): the marker records
// what this instance was configured to run, and points at /processes for the
// rest. See internal/instance.
func markerProcesses(configs []process.ProcessConfig) []instance.Process {
	out := make([]instance.Process, 0, len(configs))
	for _, c := range configs {
		out = append(out, instance.Process{
			Name:        c.Name,
			Command:     c.Command,
			Type:        c.Type,
			Description: c.Description,
			URL:         c.URL,
			Interval:    c.Interval,
		})
	}
	return out
}
