package docker

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Invoking the Compose CLI.
//
// Running Man does NOT manage the Compose stack. It offers to start it and then
// monitors the logs; quitting Running Man leaves the stack exactly where it is.
// There is deliberately no `down`, no `stop` and no teardown anywhere in this
// file, because "we only ever start it" is what keeps the ownership question
// from arising at all: the stack is always the developer's.

// ComposeOptions are the Compose settings that affect both which containers to
// look for and how to start them.
type ComposeOptions struct {
	Files       []string
	Profiles    []string
	ProjectName string
	EnvFile     string
}

// ComposeCommand is a resolved Compose CLI entry point.
//
// Compose exists as both a `docker compose` subcommand (v2) and a standalone
// `docker-compose` binary (v1), and plenty of machines have only one.
type ComposeCommand struct {
	// Name is the executable, e.g. "docker" or "docker-compose".
	Name string
	// Base are the leading arguments, e.g. ["compose"] for v2.
	Base []string
}

// String renders the command for display.
func (c ComposeCommand) String() string {
	if len(c.Base) == 0 {
		return c.Name
	}
	return c.Name + " " + strings.Join(c.Base, " ")
}

// FindComposeCommand locates a usable Compose CLI, preferring v2.
func FindComposeCommand(ctx context.Context) (ComposeCommand, error) {
	if path, err := exec.LookPath("docker"); err == nil {
		// `docker` existing does not mean the compose plugin is installed.
		cmd := exec.CommandContext(ctx, path, "compose", "version")
		if err := cmd.Run(); err == nil {
			return ComposeCommand{Name: path, Base: []string{"compose"}}, nil
		}
	}

	if path, err := exec.LookPath("docker-compose"); err == nil {
		return ComposeCommand{Name: path}, nil
	}

	return ComposeCommand{}, fmt.Errorf(
		"no Docker Compose CLI found: neither `docker compose` nor `docker-compose` is available")
}

// UpArgs builds the arguments for bringing the stack up.
//
// Detached, because Running Man streams the logs itself through the Docker API
// and has no use for Compose holding the foreground.
func (o ComposeOptions) UpArgs() []string {
	var args []string

	for _, f := range o.Files {
		args = append(args, "-f", f)
	}
	if o.ProjectName != "" {
		args = append(args, "-p", o.ProjectName)
	}
	if o.EnvFile != "" {
		args = append(args, "--env-file", o.EnvFile)
	}
	for _, p := range o.Profiles {
		args = append(args, "--profile", p)
	}

	return append(args, "up", "-d")
}

// Up brings the stack up and returns Compose's combined output.
//
// The output is returned rather than swallowed: when Compose fails the reason is
// in there, and the caller has just asked the user for permission to run this,
// so hiding why it did not work would be indefensible.
func Up(ctx context.Context, cmd ComposeCommand, opts ComposeOptions) (string, error) {
	args := append(append([]string{}, cmd.Base...), opts.UpArgs()...)

	c := exec.CommandContext(ctx, cmd.Name, args...)
	out, err := c.CombinedOutput()
	return string(out), err
}

// DisplayCommand renders what would be run, for showing the user before asking.
//
// Showing the exact command is the point of the offer: it is their stack, and
// they should be able to see precisely what is about to happen to it.
func DisplayCommand(cmd ComposeCommand, opts ComposeOptions) string {
	args := append(append([]string{}, cmd.Base...), opts.UpArgs()...)
	return cmd.Name + " " + strings.Join(args, " ")
}
