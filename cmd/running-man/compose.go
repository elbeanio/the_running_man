package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/elbeanio/the_running_man/internal/config"
	"github.com/elbeanio/the_running_man/internal/docker"
)

// Offering to start a Compose stack.
//
// Running Man does not manage the stack. It offers to start it, then monitors
// the logs, and whatever it started is left running when Running Man exits.
// There is no teardown anywhere: the stack belongs to the developer, before and
// after. That one rule is what keeps this simple -- there is no ownership
// question to answer, and no surprise when you quit.

// offerToStartCompose is called when no containers were found. Depending on the
// configured mode it starts the stack, offers to, or declines to.
func offerToStartCompose(
	ctx context.Context,
	cfg config.DockerComposeConfig,
	projectName string,
	discover func() ([]docker.Container, error),
) ([]docker.Container, error) {
	mode := cfg.GetStart()

	notRunning := fmt.Errorf(
		"no running containers found for Compose project %q.\n"+
			"[running-man] Start it with `docker compose up -d`, or let running-man offer to "+
			"(--compose-start=ask requires a terminal; --compose-start=always never asks)",
		projectName)

	if mode == config.ComposeStartNever {
		return nil, notRunning
	}

	composeCmd, err := docker.FindComposeCommand(ctx)
	if err != nil {
		// Not a hard failure of ours: the stack simply is not running and we
		// cannot offer to help. Say both things.
		return nil, fmt.Errorf("%w.\n[running-man] Also: %v", notRunning, err)
	}

	opts := docker.ComposeOptions{
		Files:       cfg.Files,
		Profiles:    cfg.Profiles,
		ProjectName: cfg.ProjectName,
		EnvFile:     cfg.EnvFile,
	}

	if mode == config.ComposeStartAsk {
		// Only ask when someone can answer. In CI, stdout is not a terminal and
		// a prompt would hang the job -- the same reasoning as --keep-alive.
		if !stdoutIsTerminal() {
			return nil, fmt.Errorf("%w.\n"+
				"[running-man] Not prompting because stdout is not a terminal; "+
				"use --compose-start=always to start it without asking", notRunning)
		}
		if !confirmComposeUp(os.Stdin, projectName, docker.DisplayCommand(composeCmd, opts)) {
			return nil, fmt.Errorf("declined to start the Compose stack")
		}
	}

	fmt.Printf("[running-man] Starting Compose stack: %s\n", docker.DisplayCommand(composeCmd, opts))

	out, err := docker.Up(ctx, composeCmd, opts)
	if trimmed := strings.TrimSpace(out); trimmed != "" {
		// Compose's own output, indented so it is clearly not ours. Shown on
		// success too: it lists what was created, which is exactly what someone
		// who just consented wants to see.
		for _, line := range strings.Split(trimmed, "\n") {
			fmt.Printf("    %s\n", line)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("`%s` failed: %w", docker.DisplayCommand(composeCmd, opts), err)
	}

	// Containers do not register instantly, and failing immediately after being
	// told to start would be absurd. How long that takes is the project's
	// business: a stack that migrates a database before starting anything else
	// needs far longer than the default, hence start_timeout.
	timeout := cfg.GetStartTimeout()
	fmt.Printf("[running-man] Waiting for containers (up to %s)...\n", timeout)

	containers, err := waitForContainers(ctx, discover, timeout)
	if err != nil {
		return nil, err
	}

	fmt.Printf("[running-man] Stack started. It will be left running when running-man exits.\n")
	return containers, nil
}

// confirmComposeUp asks for permission, showing the exact command first.
//
// Showing the command is the point: it is the developer's stack, and they should
// see precisely what is about to happen to it before agreeing.
//
// Takes the input reader rather than using os.Stdin directly, so the answer
// handling is testable without a pseudo-terminal.
func confirmComposeUp(in io.Reader, projectName, command string) bool {
	fmt.Printf("\n[running-man] No containers are running for Compose project %q.\n", projectName)
	fmt.Printf("[running-man] Running Man can start it for you:\n\n    %s\n\n", command)
	fmt.Printf("[running-man] The stack is yours: it will be left running when running-man exits.\n")
	fmt.Printf("[running-man] Start it now? [y/N] ")

	reader := bufio.NewReader(in)
	answer, err := reader.ReadString('\n')
	if err != nil {
		// EOF or a closed stdin means no answer, which is not consent.
		fmt.Println()
		return false
	}

	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

// waitForContainers polls discovery until containers appear or time runs out.
func waitForContainers(
	ctx context.Context,
	discover func() ([]docker.Container, error),
	timeout time.Duration,
) ([]docker.Container, error) {
	deadline := time.Now().Add(timeout)

	for {
		containers, err := discover()
		if err != nil {
			return nil, fmt.Errorf("failed to discover containers after starting the stack: %w", err)
		}
		if len(containers) > 0 {
			return containers, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf(
				"the Compose stack was started but no containers appeared within %s.\n"+
					"[running-man] Check `docker compose ps` -- services may have exited immediately",
				timeout)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// missingServices returns the expected services with no running container that
// have not run to completion either. One-shot services -- a migration, a seed
// -- exit by design; counting them as missing would ask to start the stack on
// every run.
func missingServices(expected []string, running []docker.Container, completed map[string]bool) []string {
	up := make(map[string]bool, len(running))
	for _, c := range running {
		up[c.ServiceName] = true
	}
	var missing []string
	for _, name := range expected {
		if !up[name] && !completed[name] {
			missing = append(missing, name)
		}
	}
	return missing
}

// offerToStartMissing is called when part of the stack is running. It starts
// the missing services, offers to, or declines to, by the same start mode as
// offerToStartCompose -- but only those services, and never recreating a
// running container.
//
// It used to offer nothing in this case: the offer was made only when no
// container at all was running. A stack with three of eleven services up --
// the base services stopped, a profile's left running -- was watched as it
// was, and the missing ones were named only on the terminal, which the TUI
// then covered.
//
// Failing to start them is reported, not fatal: part of the stack was running
// before, and is still worth watching.
func offerToStartMissing(
	ctx context.Context,
	cfg config.DockerComposeConfig,
	projectName string,
	missing []string,
	discover func() ([]docker.Container, error),
	completed func() map[string]bool,
	current []docker.Container,
) []docker.Container {
	mode := cfg.GetStart()
	if mode == config.ComposeStartNever {
		return current
	}
	composeCmd, err := docker.FindComposeCommand(ctx)
	if err != nil {
		fmt.Printf("[running-man] Cannot offer to start the missing services: %v\n", err)
		return current
	}
	opts := docker.ComposeOptions{
		Files:       cfg.Files,
		Profiles:    cfg.Profiles,
		ProjectName: cfg.ProjectName,
		EnvFile:     cfg.EnvFile,
		Services:    missing,
	}
	command := docker.DisplayCommand(composeCmd, opts)

	if mode == config.ComposeStartAsk {
		if !stdoutIsTerminal() {
			fmt.Printf("[running-man] Not offering to start the missing services: stdout is not a "+
				"terminal. Use --compose-start=always, or run: %s\n", command)
			return current
		}
		if !confirmStartMissing(os.Stdin, projectName, missing, command) {
			return current
		}
	}

	fmt.Printf("[running-man] Starting: %s\n", command)
	out, err := docker.Up(ctx, composeCmd, opts)
	if trimmed := strings.TrimSpace(out); trimmed != "" {
		for _, line := range strings.Split(trimmed, "\n") {
			fmt.Printf("    %s\n", line)
		}
	}
	if err != nil {
		fmt.Printf("[running-man] `%s` failed: %v\n", command, err)
		return current
	}

	timeout := cfg.GetStartTimeout()
	fmt.Printf("[running-man] Waiting for them (up to %s)...\n", timeout)
	containers, still := waitForServices(ctx, discover, completed, missing, timeout)
	if containers == nil {
		containers = current
	}
	if len(still) > 0 {
		fmt.Printf("[running-man] Still not running after %s: %s. Carrying on with what is.\n",
			timeout, strings.Join(still, ", "))
	}
	return containers
}

// confirmStartMissing asks whether to start the missing services, showing
// which they are and the exact command.
func confirmStartMissing(in io.Reader, projectName string, missing []string, command string) bool {
	fmt.Printf("\n[running-man] %d services in Compose project %q are not running: %s\n",
		len(missing), projectName, strings.Join(missing, ", "))
	fmt.Printf("[running-man] Running Man can start just those, leaving the running ones alone:\n\n    %s\n\n", command)
	fmt.Printf("[running-man] They will be left running when running-man exits.\n")
	fmt.Printf("[running-man] Start them now? [y/N] ")

	answer, err := bufio.NewReader(in).ReadString('\n')
	if err != nil {
		fmt.Println()
		return false
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

// waitForServices polls until every one of services is running or has run to
// completion, or time runs out. It returns the latest running containers and
// the services still missing.
func waitForServices(
	ctx context.Context,
	discover func() ([]docker.Container, error),
	completed func() map[string]bool,
	services []string,
	timeout time.Duration,
) ([]docker.Container, []string) {
	deadline := time.Now().Add(timeout)
	var containers []docker.Container
	still := services
	for {
		if found, err := discover(); err == nil {
			containers = found
			still = missingServices(services, found, completed())
			if len(still) == 0 {
				return containers, nil
			}
		}
		if time.Now().After(deadline) {
			return containers, still
		}
		select {
		case <-ctx.Done():
			return containers, still
		case <-time.After(500 * time.Millisecond):
		}
	}
}
