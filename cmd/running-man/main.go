package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/elbeanio/the_running_man/internal/api"
	"github.com/elbeanio/the_running_man/internal/config"
	"github.com/elbeanio/the_running_man/internal/docker"
	"github.com/elbeanio/the_running_man/internal/health"
	"github.com/elbeanio/the_running_man/internal/instance"
	"github.com/elbeanio/the_running_man/internal/parser"
	"github.com/elbeanio/the_running_man/internal/process"
	"github.com/elbeanio/the_running_man/internal/storage"
	"github.com/elbeanio/the_running_man/internal/termout"
	"github.com/elbeanio/the_running_man/internal/tracing"
	"github.com/kballard/go-shellquote"

	"github.com/mattn/go-isatty"
)

const (
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

// lineSourceType is the source type for a line arriving through a handler of
// type via. Running Man's own lines are system output whichever handler they
// came through: the process manager reports startup and restarts through the
// process handler, under the name running-man.
func lineSourceType(source, via string) string {
	if source == "running-man" {
		return "system"
	}
	return via
}

// composeServices summarises the Compose stack for dependency validation.
func composeServices(compose *docker.ComposeFile, profiles []string) config.ComposeServices {
	svcs := config.ComposeServices{
		Active:          map[string]bool{},
		GatedOut:        map[string]bool{},
		WithHealthcheck: map[string]bool{},
	}
	for _, name := range compose.ServiceNamesForProfiles(profiles) {
		svcs.Active[name] = true
	}
	for _, name := range compose.ServicesGatedOut(profiles) {
		svcs.GatedOut[name] = true
	}
	for name, svc := range compose.Services {
		if svc.HasHealthcheck() {
			svcs.WithHealthcheck[name] = true
		}
	}
	return svcs
}

// composeReadiness is how the process manager learns that a Compose service
// a process depends on is ready: Docker's own health status for a service
// whose Compose file defines a healthcheck, otherwise the healthcheck declared
// under docker_compose.healthchecks. Validation has already ensured one exists.
func composeReadiness(client *docker.Client, project string, compose *docker.ComposeFile,
	declared map[string]config.HealthcheckConfig) process.ServiceReadiness {
	return func(ctx context.Context, service string) error {
		timeout := config.DefaultHealthcheckTimeout
		var what string
		var check func(context.Context) error

		if svc, ok := compose.Services[service]; ok && svc.HasHealthcheck() {
			what = "its Compose healthcheck"
			check = func(ctx context.Context) error { return client.WaitServiceHealthy(ctx, project, service) }
		} else if hc, ok := declared[service]; ok {
			timeout = hc.GetTimeout()
			switch {
			case hc.Port != 0:
				what = fmt.Sprintf("its healthcheck (port %d)", hc.Port)
				check = func(ctx context.Context) error { return health.Port(ctx, hc.Port) }
			case hc.HTTP != "":
				what = "its healthcheck (http " + hc.HTTP + ")"
				check = func(ctx context.Context) error { return health.HTTP(ctx, hc.HTTP) }
			default:
				what = fmt.Sprintf("its healthcheck (log %q)", hc.Log)
				check = func(ctx context.Context) error { return client.WaitServiceLog(ctx, project, service, hc.Log) }
			}
		} else {
			return fmt.Errorf("it has no healthcheck")
		}

		checkCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		err := check(checkCtx)
		if err != nil && ctx.Err() == nil && errors.Is(checkCtx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("%s did not pass within %s", what, timeout)
		}
		return err
	}
}

// checkDependencies exits if the configured dependency graph cannot be
// resolved, listing every problem.
func checkDependencies(cfg *config.Config, compose config.ComposeServices) {
	err := cfg.ValidateDependencies(compose)
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, "Error: the process dependencies cannot be resolved:")
	for _, line := range strings.Split(err.Error(), "\n") {
		fmt.Fprintf(os.Stderr, "  - %s\n", line)
	}
	os.Exit(1)
}

// processNamer names --process commands after their slug, adding -2, -3, ...
// until the name is free.
//
// It tracks the names actually taken, not a count per slug. Counting gave
// `foo`, `foo`, `foo 2` the names foo, foo-2 and foo-2: the manager keys
// processes by name, so one of the two was silently dropped. CLI processes do
// not pass through config validation, which is what catches duplicates in a
// config file.
type processNamer map[string]bool

// next returns the name for one more command.
func (n processNamer) next(cmdStr string) string {
	base := slugify(cmdStr)
	name := base
	for i := 2; n[name]; i++ {
		name = fmt.Sprintf("%s-%d", base, i)
	}
	n[name] = true
	return name
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
	// tracingPortSearchRange is how many consecutive ports to try for the OTLP
	// receiver before giving up. Enough for every instance on one machine;
	// small enough that an unbindable range reports a failure rather than
	// grinding through thousands of ports.
	tracingPortSearchRange = 20

	keepAliveAuto   = "auto"
	keepAliveAlways = "always"
	keepAliveNever  = "never"
)

// stdoutIsTerminal reports whether stdout is a terminal, i.e. whether a human
// is plausibly watching.
//
// Asked of the terminal driver, not the file mode: /dev/null is a character
// device too, and treating it as a terminal kept a headless instance waiting
// for a Ctrl-C nobody could send.
func stdoutIsTerminal() bool {
	return isatty.IsTerminal(os.Stdout.Fd())
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

// startTracingReceiver binds the OTLP receiver, moving aside from a busy port
// unless the port was named.
//
// The three outcomes, which are deliberately different:
//
//	port named, taken      fatal. Someone chose that port; using another one
//	                       silently is the substitution this guards against
//	port unnamed, taken    move up until something binds. 4318 is the OTLP/HTTP
//	                       default, so every collector wants it and one instance
//	                       per project means several receivers per machine
//	tracing unrequested,   warn and continue without tracing, rather than
//	nothing binds          blocking the whole tool over a feature nobody asked
//	                       for
//
// A nil return means the receiver is listening; an error means the caller should
// carry on with tracing disabled.
func startTracingReceiver(r *tracing.Receiver, port int, portNamed, requested bool) error {
	var err error
	if portNamed {
		err = r.Start()
	} else {
		err = r.StartOnFreePort(tracingPortSearchRange)
	}
	if err == nil {
		return nil
	}

	fmt.Fprintf(os.Stderr, "\n[running-man] Could not start the OTLP receiver: %v\n", err)
	if portNamed {
		fmt.Fprintf(os.Stderr, "[running-man] Port %d was requested explicitly, so it was not "+
			"moved. Something else is probably on it -- Arize Phoenix, the OTel Collector and "+
			"Jaeger all default to 4318.\n", port)
	}
	fmt.Fprintf(os.Stderr, "[running-man]   running-man run --tracing-port PORT   use a different port\n")
	fmt.Fprintf(os.Stderr, "[running-man]   running-man run --tracing=false        silence this\n")

	if requested {
		// Tracing was asked for. Carrying on without it would be the silent
		// failure this replaced.
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "[running-man] Continuing without tracing (it was not explicitly enabled).\n\n")
	return err
}

func runCommand(args []string) {
	// Setup flags
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to running-man.yml config file")
	dockerCompose := fs.String("docker-compose", "", "Path to docker-compose.yml file (overrides config file)")
	noTUI := fs.Bool("no-tui", false, "Disable TUI and run in headless mode")
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

	// Was tracing actually requested, or is it merely on by default?
	//
	// This matters when the OTLP port is already taken. If tracing was asked
	// for, a conflict is fatal -- silently not doing what was asked is worse.
	// If it is on only because the default is on, refusing to start would
	// block the whole tool over a feature nobody requested, which is how
	// Running Man behaves alongside any other collector on 4318.
	tracingRequested := false
	// Separately: was the *port* named? An unnamed port may move aside when it
	// is taken, because one instance per project means several receivers on one
	// machine. A named one may not -- quietly using a different port than the
	// one asked for is the silent substitution this guards against.
	tracingPortNamed := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "tracing" || f.Name == "tracing-port" {
			tracingRequested = true
		}
		if f.Name == "tracing-port" {
			tracingPortNamed = true
		}
	})
	if cfg != nil && cfg.Tracing.Enabled != nil {
		tracingRequested = true
	}
	if cfg != nil && cfg.Tracing.Port != 0 {
		tracingRequested = true
		tracingPortNamed = true
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
	names := processNamer{}

	// First, add processes from config file (if no --process flags provided)
	if len(procs) == 0 && cfg != nil {
		processes = cfg.ToProcessConfigs()
		for _, proc := range processes {
			names[proc.Name] = true
		}
	}

	// Then, add processes from CLI flags (these override config)
	for _, cmdStr := range procs {
		cmd, cmdArgs, err := parseCommandString(cmdStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing command '%s': %v\n", cmdStr, err)
			os.Exit(1)
		}

		name := names.next(cmdStr)

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

	// Parse the Compose files now, ahead of everything else that uses them,
	// because dependency validation needs them and must come first.
	var composeFile *docker.ComposeFile
	if finalCompose.IsSet() {
		// Every configured Compose file, merged by service name.
		composeFile, err = docker.ParseComposeFiles(finalCompose.Files)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[running-man] Failed to parse compose file(s): %v\n", err)
			os.Exit(1)
		}
	}

	// Refuse an unresolvable dependency graph before anything is printed,
	// bound or started -- including the offer to bring a Compose stack up.
	// --process flags replace the config's processes, and with them any
	// dependencies.
	if len(procs) == 0 && cfg != nil {
		svcs := config.ComposeServices{}
		if composeFile != nil {
			svcs = composeServices(composeFile, finalCompose.Profiles)
		}
		checkDependencies(cfg, svcs)
	}

	// The project directory is resolved before anything binds, because the socket
	// path derives from it and the socket is what proves whether this project
	// already has an instance.
	projectDir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[running-man] Could not determine working directory: %v\n", err)
		projectDir = "."
	}
	// Canonical, so that two spellings of one project -- a symlinked route, or
	// /tmp vs /private/tmp on macOS -- agree about which project this is. The
	// socket derivation resolves independently; this keeps the marker and
	// /health reporting the same directory it used.
	if resolved, resolveErr := filepath.EvalSymlinks(projectDir); resolveErr == nil {
		projectDir = resolved
	}
	socketPath := instance.SocketPath(projectDir)

	fmt.Println("The Running Man - Dev Observability Tool")

	// Show running processes
	for _, proc := range processes {
		fmt.Printf("Running [%s]: %s %v\n", proc.Name, proc.Command, proc.Args)
	}

	fmt.Printf("API: %s\n\n", socketPath)

	// Create ring buffer
	buffer := storage.NewRingBuffer(finalMaxEntries, finalRetention, finalMaxBytes)

	// Create tracing storage and receiver if enabled
	var tracingReceiver *tracing.Receiver
	var spanStorage *tracing.SpanStorage
	if finalTracingEnabled {
		spanStorage = tracing.NewSpanStorage(finalMaxSpans, finalMaxSpanAge)
		tracingReceiver = tracing.NewReceiver(spanStorage, buffer, finalTracingPort)

		// Bound here, before the process manager is built, because the port that
		// is actually bound gets injected into every process Running Man starts.
		// Settling it later would inject a port nothing is listening on.
		if err := startTracingReceiver(tracingReceiver, finalTracingPort, tracingPortNamed, tracingRequested); err != nil {
			tracingReceiver = nil
			spanStorage = nil
			finalTracingEnabled = false
		} else {
			finalTracingPort = tracingReceiver.Port()
			fmt.Printf("Tracing: OTLP receiver on http://localhost:%d\n", finalTracingPort)
		}
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
		appendEntries(parse(source, lineSourceType(source, "process"), line, timestamp, isStderr))
	}

	dockerLineHandler := func(source string, line string, timestamp time.Time, isStderr bool) {
		appendEntries(parse(source, "docker", line, timestamp, isStderr))
	}

	systemLineHandler := func(source string, line string, timestamp time.Time, isStderr bool) {
		appendEntries(parse(source, "system", line, timestamp, isStderr))
	}

	// Releases a traceback the parser is still holding when a stream ends.
	// Without it, a traceback that was a stream's last output -- cut short, or
	// ending in an exception line the parser does not recognise -- never
	// reached the buffer.
	flushStream := func(source string, isStderr bool) {
		appendEntries([]*parser.LogEntry{multiParser.Flush(source, isStderr)})
	}

	// Docker Compose integration
	var containerWatcher *docker.Watcher
	var dockerClient *docker.Client
	var serviceReady process.ServiceReadiness
	ctx := context.Background()

	if finalCompose.IsSet() {
		compose := composeFile

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

		projectName := docker.ProjectName(
			finalCompose.ProjectName, compose.Name, finalCompose.PrimaryFile())

		fmt.Printf("Docker Compose: %s\n", strings.Join(finalCompose.Files, ", "))
		// Named explicitly: discovery filters on it, and when it is wrong every
		// container is invisible with no other clue as to why.
		fmt.Printf("  project: %s\n", projectName)
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

		// Part of the stack running is not all of it. Offer to start the rest,
		// by the same rules as starting the whole stack.
		completed := func() map[string]bool {
			done, err := dockerClient.CompletedServices(ctx, projectName)
			if err != nil {
				return nil
			}
			return done
		}
		if missing := missingServices(serviceNames, containers, completed()); len(containers) > 0 && len(missing) > 0 {
			containers = offerToStartMissing(ctx, finalCompose, projectName, missing,
				discover, completed, containers)
		}

		fmt.Printf("Found %d running container(s):\n", len(containers))
		for _, container := range containers {
			fmt.Printf("  - [%s] %s\n", container.Name, container.ID[:12])
		}

		// Report expected services that are not running, rather than quietly
		// watching a partial stack.
		// One-shot services that ran to completion are done, not missing.
		if missing := missingServices(serviceNames, containers, completed()); len(missing) > 0 {
			fmt.Printf("  not running: %s\n", strings.Join(missing, ", "))
		}

		// Stream each container's logs, and keep streaming them: the watcher
		// attaches again when a container restarts or is recreated, and picks
		// up a service that starts later.
		//
		// Replays as much history as retention would keep, and no more:
		// replayed lines arrive now, so anything older would otherwise be held
		// for a full retention window regardless of its age.
		containerWatcher = docker.NewWatcher(dockerClient, projectName, serviceNames, finalRetention, dockerLineHandler, flushStream)
		for _, container := range containers {
			if err := containerWatcher.Attach(container); err != nil {
				fmt.Fprintf(os.Stderr, "[running-man] Failed to start log streamer for %s: %v\n", container.Name, err)
			}
		}
		containerWatcher.Watch(discover)
		serviceReady = composeReadiness(dockerClient, projectName, compose, finalCompose.Healthchecks)

		fmt.Println()
	}

	// Create process manager for all processes
	var manager *process.Manager
	if finalTracingEnabled {
		// Use OTEL-enabled manager
		otelEndpoint := "http://localhost"
		manager = process.NewManagerWithOTEL(processes, processLineHandler, otelEndpoint, finalTracingPort, true)
	} else {
		// Use regular manager
		manager = process.NewManagerWithOTEL(processes, processLineHandler, "", 0, false)
	}
	manager.OnStreamEnd(flushStream)
	if serviceReady != nil {
		manager.SetServiceReadiness(serviceReady)
	}
	if containerWatcher != nil {
		manager.SetServiceSources(containerWatcher.SourcesFor)
	}

	// A TUI is coming, so nothing else may write to this terminal. Silenced
	// here rather than when the TUI actually starts, because processes and
	// container streamers begin producing output at manager.Start() -- which is
	// well before the screen is handed over.
	//
	// restoreOutput is called as soon as the TUI gives the screen back, not at
	// the end of this function: shutdown errors go through termout too, and
	// they are the last thing a user sees.
	restoreOutput := func() {}
	if !*noTUI {
		restoreOutput = termout.Silence()
	}
	defer restoreOutput()

	// Start API server in background
	var traceStorage *tracing.SpanStorage
	if spanStorage != nil {
		traceStorage = spanStorage
	}
	apiServer := api.NewServer(buffer, projectDir, systemLineHandler, manager, traceStorage)
	if finalTracingEnabled {
		apiServer.SetOTLPEndpoint(fmt.Sprintf("http://localhost:%d", finalTracingPort))
	}

	// Bind before starting anything else. The previous version served in a
	// goroutine and only printed a bind failure to stderr, where the TUI hid
	// it -- so a second instance came up with no API and carried on as though
	// it had one, while agents reading the marker talked to the first.
	//
	// This happens before manager.Start(), so a refused instance has not
	// spawned a duplicate of anybody's dev server.
	apiListener, err := api.Listen(socketPath)
	if err != nil {
		if errors.Is(err, api.ErrInstanceLive) {
			fmt.Fprintf(os.Stderr, "\n[running-man] This project already has a Running Man instance.\n")
			fmt.Fprintf(os.Stderr, "[running-man]   socket: %s\n", socketPath)
			if m, readErr := instance.Read(projectDir); readErr == nil {
				fmt.Fprintf(os.Stderr, "[running-man]   owner:  PID %d, started %s\n",
					m.PID, m.Started.Format(time.RFC3339))
			}
			fmt.Fprintf(os.Stderr, "[running-man] Use it instead of starting a second copy:\n")
			fmt.Fprintf(os.Stderr, "[running-man]   curl -s --unix-socket %s http://localhost/processes\n", socketPath)
			fmt.Fprintf(os.Stderr, "[running-man] Or quit the other instance first.\n")
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "\n[running-man] Could not serve the API on %s: %v\n", socketPath, err)
		os.Exit(1)
	}
	go func() {
		if err := apiServer.Serve(apiListener); err != nil {
			fmt.Fprintf(os.Stderr, "[running-man] API server error: %v\n", err)
		}
	}()

	if tracingReceiver != nil {

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
	marker := instance.New(
		socketPath,
		projectDir,
		markerProcesses(processes),
	)
	if finalTracingEnabled {
		marker.OTLPEndpoint = fmt.Sprintf("http://localhost:%d", finalTracingPort)
	}
	if err := marker.Write(projectDir); err != nil {
		// Not fatal: the marker is a discovery aid, and losing it must not stop
		// the tool doing its actual job.
		fmt.Fprintf(os.Stderr, "[running-man] Could not write instance marker: %v\n", err)
	} else {
		fmt.Printf("[running-man] Instance marker: %s\n", instance.Path(projectDir))
	}

	// Clean up however we leave: normal return, os.Exit paths below, and
	// signals. A marker outliving its instance points an agent at a dead API,
	// which is worse than no marker at all.
	//
	// The listener is closed first because Go unlinks a Unix socket when its
	// listener closes, and instance.Remove cannot delete the directory while
	// the socket is still sitting in it. Leaving a dead socket behind is not
	// fatal -- the next instance takes it over -- but it makes a stale instance
	// look live to anything that only checks whether the path exists.
	removeMarker := func() {
		if err := apiListener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			fmt.Fprintf(os.Stderr, "[running-man] Could not close the API socket: %v\n", err)
		}
		// Before instance.Remove, which cannot delete the directory while this
		// is still in it.
		removeCrashLogIfEmpty()
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
		fmt.Printf("[running-man] API on %s\n", socketPath)
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
		if len(processes) == 0 && containerWatcher != nil {
			fmt.Printf("\n[running-man] Streaming container logs. Press Ctrl+C to quit.\n")
			fmt.Printf("[running-man] The Compose stack will be left running.\n")
			waitForInterrupt()
			fmt.Printf("\n[running-man] Exiting.\n")
		}

		// Stop all container streamers
		if containerWatcher != nil {
			containerWatcher.Stop()
			containerWatcher.Wait()
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

		// Print exit codes for each process. A process blocked by a failed
		// dependency never ran, so it has no exit code to report.
		for name, code := range exitCodes {
			if code == 0 {
				continue
			}
			if info, err := manager.GetProcess(name); err == nil && info.Status == process.StatusBlocked {
				fmt.Fprintf(os.Stderr, "[running-man] Process %s did not start: %s\n", name, info.StartupError)
				continue
			}
			fmt.Fprintf(os.Stderr, "[running-man] Process %s exited with code %d\n", name, code)
		}

		// Hold the buffer open so the logs explaining a crash survive it. The
		// buffer is in-memory, so exiting here destroys exactly the evidence
		// someone wants. Gated so CI is not left hanging -- see shouldKeepAlive.
		if failed && shouldKeepAlive(*keepAlive) {
			fmt.Printf("\n[running-man] Processes have exited, but their logs are still available:\n")
			fmt.Printf("[running-man]   curl -s --unix-socket %s http://localhost/logs\n", socketPath)
			fmt.Printf("[running-man]   curl -s --unix-socket %s http://localhost/errors\n", socketPath)
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
		fmt.Printf("[running-man] API on %s\n", socketPath)
		time.Sleep(200 * time.Millisecond) // Give API a moment to stabilize

		// Run TUI with manager reference so it can stop processes on quit
		TuiCommandWithManager([]string{"--socket=" + socketPath}, manager)

		// The screen is ours again, so diagnostics are worth printing.
		restoreOutput()

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
		if containerWatcher != nil {
			containerWatcher.Stop()
			containerWatcher.Wait()
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
  running-man tui [--socket PATH]
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
  --tracing                Enable OTLP trace ingestion (default: true)
  --tracing-port PORT      OTLP HTTP receiver port. Without this, the receiver
                           takes 4318 or the next free port above it; with it,
                           a conflict is a startup failure.
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

  # Connect the TUI to the instance running in this directory
  running-man tui

  # Query the API while the TUI is running (separate terminal). The API is on a
  # Unix socket in the project, not a TCP port -- .running-man/instance.json
  # records the exact path.
  SOCK=.running-man/api.sock
  curl -s --unix-socket $SOCK 'http://localhost/logs?since=30s'
  curl -s --unix-socket $SOCK http://localhost/errors
  curl -s --unix-socket $SOCK http://localhost/health

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
