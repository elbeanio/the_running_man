# The Running Man 🏃

**Run every moving part of your project under one roof — and make all of it inspectable by
you and by your coding agent, through the same interface.**

[![CI](https://github.com/elbeanio/the_running_man/actions/workflows/ci.yml/badge.svg)](https://github.com/elbeanio/the_running_man/actions/workflows/ci.yml)
[![Security Scan](https://github.com/elbeanio/the_running_man/actions/workflows/security.yml/badge.svg)](https://github.com/elbeanio/the_running_man/actions/workflows/security.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/elbeanio/the_running_man)](https://goreportcard.com/report/github.com/elbeanio/the_running_man)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

📖 **[Documentation](https://elbeanio.github.io/the_running_man/)**

## Why

A real project is not one process. It is a Node front-end build, a Python back-end, a
worker, a job that runs every minute, and a Docker Compose stack behind them. Normally that
means five terminal tabs, five scrollback buffers, and no way to ask a question that spans
them.

Running Man runs all of it, captures everything it emits into one searchable place, and
exposes that over an HTTP API. Every source can be queried together or separately, and any
process can be restarted without touching the others.

Both audiences use the same interface. The developer gets a TUI with a tab per source; a
coding agent gets the same data over `curl`. That matters because of what otherwise
happens: an agent that starts its own dev server and reads its own output works perfectly
well, and leaves the developer blind — unable to see the stack trace it is about to spend
five minutes reasoning about, and unable to say "I recognise that, it is the migration".

One copy of the stack. One record of what it did. Visible to everyone working on it.

> **Note:** the Security Scan badge is **expected to be red.** Two advisories in the Docker
> SDK are reported `Fixed in: N/A`. They are deliberately not suppressed — see
> [PROJECT.md](PROJECT.md).

## Install

```bash
go install github.com/elbeanio/the_running_man/cmd/running-man@latest
```

Requires Go 1.25+ on Linux or macOS. Windows is not supported: process supervision uses
`Setsid`, `syscall.Kill` and `ps`/`lsof`.

## Quick start

Describe the whole stack in `running-man.yml`:

```yaml
processes:
  - name: frontend
    type: web
    command: npm run dev
    depends_on: [backend]     # starts once the backend is ready

  - name: backend
    type: api
    command: cd api && python -m uvicorn main:app --reload
    depends_on: [postgres]    # a Compose service
    healthcheck:
      port: 8000              # ready once it accepts connections

  - name: worker
    type: worker
    command: cd api && python worker.py
    restart_on_crash: true

  - name: status-check
    command: ./scripts/status.sh
    interval: 1m        # recurring: runs on a timer, output captured like anything else

docker_compose:
  files: [docker-compose.yml]
  profiles: [backend]   # postgres, redis, whatever your stack needs
```

Then:

```bash
running-man run
```

That offers to bring the Compose stack up if it is not already running, starts the four
processes -- each one with a `depends_on` as soon as what it depends on is ready -- and opens
a TUI with a tab per source, the two containers included. While anything is still waiting,
the first tab shows startup: what is ready, what is waiting on what, and why anything failed.

Or without a config file:

```bash
running-man run --process "npm run dev" --process "python -m uvicorn main:app --reload"

running-man run --docker-compose ./docker-compose.yml --compose-profile backend

running-man run --process "pytest" --no-tui     # headless, for CI
```

## Inspect and control it

Everything below works identically for a person at a terminal and for an agent with
`curl`:

```bash
# The API is a Unix socket in the project, not a TCP port
SOCK=.running-man/api.sock

# What has gone wrong anywhere in the stack, in the last ten minutes
curl -s --unix-socket "$SOCK" 'http://localhost/errors?since=10m'

# One source
curl -s --unix-socket "$SOCK" 'http://localhost/logs?source=backend&since=5m&limit=50'

# Several at once, or a glob
curl -s --unix-socket "$SOCK" 'http://localhost/logs?source=frontend,worker&since=5m'
curl -s --unix-socket "$SOCK" 'http://localhost/logs?source=*&contains=timeout'

# What is up, what it is listening on, what exited
curl -s --unix-socket "$SOCK" http://localhost/processes

# Restart one thing after a code change, leaving the rest alone
curl -s -X POST --unix-socket "$SOCK" http://localhost/processes/backend/restart
```

Python tracebacks arrive as a single entry with the whole trace attached, rather than
forty lines to stitch back together. Container logs, process output and OpenTelemetry
spans all land in the same buffer, correlated by `trace_id` where the app provides one.

`GET /` lists every endpoint with a description, and `/openapi.yaml` serves the full
specification.

## Your agent finds it by itself

While an instance is running, `.running-man/instance.json` sits in the project root with
the API URL, the configured processes and ready-to-run `curl` hints — one file read, in a
directory agents already inspect.

Paired with [`skills/running-man/SKILL.md`](skills/running-man/SKILL.md), it answers the
question that matters before an agent starts anything: **is this already running?**

```bash
make skill:link   # symlink into ~/.claude/skills
                  # or: make skill:link SKILLS_DIR=~/somewhere/else
```

The skill is plain Markdown and the API is plain HTTP, so anything that can read a file and
run `curl` can use it. Running Man has no opinion about which agent you use.

## ⚠️ Network exposure

**The query API is not on the network at all.** It is a Unix socket at
`.running-man/api.sock`, mode 0600, so reading your logs means being you on this machine.

The OTLP receiver is the exception and has to be: it binds **all interfaces** on 4318 so
containers, browsers and other devices can export to it. It is write-only — it accepts
telemetry, it serves none — but anyone who can reach it can write into the buffer, and it
takes the source name from the sender. An entry attributed to `backend` is therefore not
evidence that it came from `backend`.

The receiver moves to the next free port above 4318 if something already holds it, which is
common — every other OTLP collector defaults to the same port. The port it settled on is in
`.running-man/instance.json` and on `/health`.

Full detail: [Network exposure](https://elbeanio.github.io/the_running_man/api-reference#network-exposure).

## Documentation

Full documentation lives here: **[elbeanio.github.io/the_running_man](https://elbeanio.github.io/the_running_man/)**

| | |
|---|---|
| [Overview](https://elbeanio.github.io/the_running_man/overview) | What it is and what problem it solves |
| [Getting started](https://elbeanio.github.io/the_running_man/getting-started) | Install, first run, first queries |
| [Configuration](https://elbeanio.github.io/the_running_man/configuration) | Every `running-man.yml` key and CLI flag |
| [API reference](https://elbeanio.github.io/the_running_man/api-reference) | Endpoints, parameters, network exposure |
| [Agent integration](https://elbeanio.github.io/the_running_man/agent-integration) | The instance marker and the skill |
| [Tracing](https://elbeanio.github.io/the_running_man/tracing) | OpenTelemetry setup |
| [Architecture](https://elbeanio.github.io/the_running_man/architecture) | How it fits together |
| [Troubleshooting](https://elbeanio.github.io/the_running_man/troubleshooting) | When something is wrong |
| [Development](https://elbeanio.github.io/the_running_man/development) | Building and contributing |

For contributors and agents working *on* Running Man, rather than with it:

- **[PROJECT.md](PROJECT.md)** — what this is for, its constraints, its non-goals, and the
  decisions that shaped it
- **[GLOSSARY.md](GLOSSARY.md)** — the locked vocabulary: **instance**, **source**,
  **managed process**, **observed port**, **retention limits**
- **[AGENTS.md](AGENTS.md)** — the rules for changing this repository

## Development

```bash
make build        # build
make test         # unit tests (seconds)
make test-race    # race detector
make lint         # golangci-lint
```

See [Development](https://elbeanio.github.io/the_running_man/development).

## License

MIT — see [LICENSE](LICENSE).
