# The Running Man 🏃

**Run your project's processes, capture everything they emit, and make it equally available
to you and to your coding agent.**

[![CI](https://github.com/elbeanio/the_running_man/actions/workflows/ci.yml/badge.svg)](https://github.com/elbeanio/the_running_man/actions/workflows/ci.yml)
[![Security Scan](https://github.com/elbeanio/the_running_man/actions/workflows/security.yml/badge.svg)](https://github.com/elbeanio/the_running_man/actions/workflows/security.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/elbeanio/the_running_man)](https://goreportcard.com/report/github.com/elbeanio/the_running_man)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

📖 **[Documentation](https://elbeanio.github.io/the_running_man/)**

## Why

When a coding agent starts its own dev server and reads its own output, it works fine — and
you are blind. You cannot see the stack trace it is about to spend five minutes on, so you
cannot tell it that you recognise the problem.

Running Man keeps one copy of your stack running, captures everything it emits, and serves
that to both of you over the same API. The agent gets logs it would otherwise have to
re-run the app to see; you get to watch, and to interject.

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

```bash
# One process — the TUI launches automatically
running-man run --process "python server.py"

# Several — Tab between them
running-man run --process "python server.py" --process "npm run dev"

# A Docker Compose stack (offers to start it if it is not up)
running-man run --docker-compose ./docker-compose.yml

# Headless, for CI
running-man run --process "pytest" --no-tui
```

Or put it in `running-man.yml` and just run `running-man run`:

```yaml
processes:
  - name: backend
    command: python server.py
  - name: frontend
    command: npm run dev

docker_compose: ./docker-compose.yml
```

Then query it:

```bash
curl -s 'http://localhost:9000/errors?since=10m'
curl -s 'http://localhost:9000/logs?source=backend&since=5m&limit=50'
curl -s http://localhost:9000/processes
```

`GET /` lists every endpoint and `/docs` serves interactive OpenAPI documentation.

## Your agent finds it by itself

While an instance is running, `.running-man/instance.json` sits in the project root with
the API URL, the configured processes and ready-to-run `curl` hints — one file read, in a
directory agents already inspect.

Paired with [`skills/running-man/SKILL.md`](skills/running-man/SKILL.md), it answers the
question that matters before an agent starts anything: **is this already running?**

```bash
make link-skill   # symlink into ~/.claude/skills
                  # or: make link-skill SKILLS_DIR=~/somewhere/else
```

The skill is plain Markdown and the API is plain HTTP, so anything that can read a file and
run `curl` can use it. Running Man has no opinion about which agent you use.

## ⚠️ Network exposure

Running Man binds **all interfaces** by default, on the API port (9000) and the OTLP
receiver (4318), so containers, browsers and other devices can reach it. **There is no
authentication**, which means anyone on your network can read your captured logs — and dev
servers routinely print tokens and connection strings.

Process control is the exception: `/processes/{name}/restart` and `/processes/stop-all` are
served to this machine only and return 403 otherwise.

```bash
running-man run --listen 127.0.0.1       # restrict everything to this machine
running-man run --allow-remote-control   # open process control (think first)
```

Full detail: [Network exposure](https://elbeanio.github.io/the_running_man/api-reference#network-exposure).

## Documentation

Everything is at **[elbeanio.github.io/the_running_man](https://elbeanio.github.io/the_running_man/)**:

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
