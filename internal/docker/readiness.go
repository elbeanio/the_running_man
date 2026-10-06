package docker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"

	"github.com/elbeanio/the_running_man/internal/health"
)

// pollReady retries check every health.PollInterval until it passes or ctx
// ends, returning the last reason it had not.
func pollReady(ctx context.Context, check func(context.Context) error) error {
	var last error
	for {
		if last = check(ctx); last == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w (last check: %v)", ctx.Err(), last)
		case <-time.After(health.PollInterval):
		}
	}
}

// runningContainers returns the service's running containers, or an error
// when there are none yet -- a service being started has none for a moment.
func (c *Client) runningContainers(ctx context.Context, project, service string) ([]Container, error) {
	cs, err := c.findServiceContainers(ctx, project, service)
	if err != nil {
		return nil, err
	}
	if len(cs) == 0 {
		return nil, errors.New("no running container")
	}
	return cs, nil
}

// WaitServiceHealthy passes once every running container of the service
// reports healthy: the service's Compose healthcheck, run by Docker.
func (c *Client) WaitServiceHealthy(ctx context.Context, project, service string) error {
	return pollReady(ctx, func(ctx context.Context) error {
		cs, err := c.runningContainers(ctx, project, service)
		if err != nil {
			return err
		}
		for _, ct := range cs {
			info, err := c.cli.ContainerInspect(ctx, ct.ID, client.ContainerInspectOptions{})
			if err != nil {
				return err
			}
			state := info.Container.State
			if state == nil || state.Health == nil {
				return fmt.Errorf("%s has no healthcheck", ct.Name)
			}
			if state.Health.Status != container.Healthy {
				return fmt.Errorf("%s is %s", ct.Name, state.Health.Status)
			}
		}
		return nil
	})
}

// WaitServiceLog passes once every running container of the service has
// written a line containing text in its current run.
//
// Each container's log is read from its current run's start, not from the
// history Running Man replays: a restarted container's previous run having
// said "ready" says nothing about this one, and a line written long ago, before
// the retention window, must still count.
func (c *Client) WaitServiceLog(ctx context.Context, project, service, text string) error {
	var cs []Container
	if err := pollReady(ctx, func(ctx context.Context) error {
		var err error
		cs, err = c.runningContainers(ctx, project, service)
		return err
	}); err != nil {
		return err
	}

	errs := make([]error, len(cs))
	var wg sync.WaitGroup
	for i, ct := range cs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = c.waitContainerLog(ctx, ct, text)
		}()
	}
	wg.Wait()
	return errors.Join(errs...)
}

// waitContainerLog waits for text in one container's current run.
func (c *Client) waitContainerLog(ctx context.Context, ct Container, text string) error {
	startedAt, err := c.containerStartedAt(ctx, ct.ID)
	if err != nil {
		return err
	}
	matcher := health.NewLineMatcher(text)
	s := NewContainerStreamer(c, ct.ID, ct.Name, func(_ string, line string, _ time.Time, _ bool) {
		matcher.Feed(line)
	}, 0)
	s.ReplayFrom(startedAt)
	// The watcher already shows these lines; this stream only reads them.
	s.Quiet()
	if err := s.Start(); err != nil {
		return err
	}
	defer func() { _ = s.Stop(); _ = s.Wait() }()
	return matcher.Wait(ctx)
}
