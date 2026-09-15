# Overview

[Home](index.md) · **Overview** · [Getting started](getting-started.md) · [Configuration](configuration.md) · [API reference](api-reference.md) · [Agent integration](agent-integration.md) · [Tracing](tracing.md) · [Architecture](architecture.md) · [Troubleshooting](troubleshooting.md) · [Development](development.md)

---

## What it is

A process runner with a memory. You describe your project's processes in `running-man.yml`,
start an instance, and it spawns them and captures everything they emit — plus Docker
container logs and OpenTelemetry spans — into one in-memory buffer, queryable over HTTP.

## Why it exists

An AI coding agent that starts its own dev server and reads its own output works perfectly
well. The problem is that you cannot see any of it.

You cannot see the stack trace it is about to spend five minutes reasoning about. You cannot
tell it that you recognise the error instantly, or that it is testing a different server
from the one actually serving your requests. An experienced developer watching a run go
wrong is the cheapest debugging resource available, and the usual setup makes them blind.

Running Man keeps one copy of the stack running and serves what it captures to both of you
over the same API. The agent gets logs it would otherwise have to re-run the app to see, and
history that survives a crash. You get to watch, and to interject.

## The key property

Running Man runs **outside** your application. When a process dies on startup, Running Man
still has the output that explains why — and in headless mode it keeps serving it after the
process is gone, rather than exiting and taking the logs with it.

That makes it most useful in the hardest case: the app that will not start at all.

## What it does

- **Multi-process supervision** — several processes with full shell support (`cd`, `&&`,
  pipes), restartable individually
- **Recurring processes** — run a command on an interval, with its output captured like
  anything else
- **Docker Compose** — attaches to container logs, understands profiles, and offers to
  start the stack if it is not running
- **Smart parsing** — Python tracebacks grouped into one entry, JSON logs field-extracted,
  plain text level-detected
- **Ring buffer** — in-memory, bounded by retention limits (30 minutes / 10,000 entries /
  50MB by default)
- **REST API** — query by time, source, level or content; self-describing at `GET /` and
  `/docs`
- **OpenTelemetry** — built-in OTLP receiver, environment injected into managed processes,
  traces correlated to logs by `trace_id`
- **Interactive TUI** — a tab per source, live
- **Agent integration** — an instance marker agents discover on their own, plus a skill

## What it is not

- **Not production observability.** A local development tool: no persistence, no
  multi-user concerns, not a hosted service.
- **Not a Compose manager.** It offers to start your stack and then monitors it. Quitting
  leaves the stack exactly where it was.
- **Not an intervention tool.** It shows you what is happening; you talk to your agent
  yourself.

The full statement of purpose, constraints and non-goals is in
[PROJECT.md](https://github.com/elbeanio/the_running_man/blob/main/PROJECT.md).

## Example

```yaml
# running-man.yml
processes:
  - name: backend
    command: python server.py
  - name: frontend
    command: npm run dev

docker_compose: ./docker-compose.yml
```

```bash
running-man run

# from another terminal, or from your agent
curl -s 'http://localhost:9000/errors?since=30s'
```

Next: **[Getting started](getting-started.md)** for a full walkthrough, or
**[Configuration](configuration.md)** for every available option.
