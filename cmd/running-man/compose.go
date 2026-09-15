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

// composeStartTimeout bounds how long to wait for containers to appear after
// starting the stack. Containers do not register instantly, and failing
// immediately after being told to start would be absurd.
const composeStartTimeout = 30 * time.Second

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

	fmt.Printf("[running-man] Waiting for containers (up to %s)...\n", composeStartTimeout)

	containers, err := waitForContainers(ctx, discover, composeStartTimeout)
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
