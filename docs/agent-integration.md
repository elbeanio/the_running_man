# Agent Integration Guide

[Home](index.md) · [Overview](overview.md) · [Getting started](getting-started.md) · [Configuration](configuration.md) · [API reference](api-reference.md) · **Agent integration** · [Tracing](tracing.md) · [Architecture](architecture.md) · [Troubleshooting](troubleshooting.md) · [Development](development.md)

---

Running Man exposes everything it captures over a **REST API**, which is the interface
coding agents use. It is self-describing: `GET /` lists every endpoint and `/docs` serves
interactive OpenAPI documentation, so an agent can discover the whole surface with one
request and no prior knowledge.

There is deliberately no MCP server and no agent-facing CLI. An MCP server existed until
it was removed: its scope was wrong for a per-project process runner, and the REST API
already covers the same ground in a form agents handle well. See `PROJECT.md` for the
reasoning.

## The question an agent should ask first

Before starting a dev server, check whether one is already running:

```bash
SOCK=.running-man/api.sock
curl -s --unix-socket "$SOCK" http://localhost/processes
```

If Running Man is already supervising the process you were about to start, use it — the
service is up, its logs are already being captured, and starting a second copy will
either collide on the port or split the output across two places the developer cannot
see together.

## Using the API

### Quick Start
```bash
SOCK=.running-man/api.sock

# The full OpenAPI specification
curl -s --unix-socket "$SOCK" http://localhost/openapi.yaml

# List all available endpoints
curl --unix-socket "$SOCK" http://localhost/

# System health and buffer stats
curl --unix-socket "$SOCK" http://localhost/health
```

### Common Debugging Patterns

#### 1. Error Investigation
```bash
# Recent errors with context
curl --unix-socket "$SOCK" "http://localhost/errors?since=10m"

# Search for specific error patterns
curl --unix-socket "$SOCK" "http://localhost/logs?contains='connection failed'&since=15m"
```

#### 2. Cross-Source Search
```bash
# Search across all backend services
curl --unix-socket "$SOCK" "http://localhost/logs?source=app-*&since=5m"

# Compare multiple services
curl --unix-socket "$SOCK" "http://localhost/logs?source=api,worker,database&since=2m"
```

#### 3. Trace-Log Correlation
```bash
# Find traces with errors
curl --unix-socket "$SOCK" "http://localhost/traces?status=error&since=5m"

# Get all logs for a specific trace
curl --unix-socket "$SOCK" "http://localhost/traces/{trace_id}/logs"
```

#### 4. Process Management
```bash
# Check all processes
curl --unix-socket "$SOCK" http://localhost/processes

# Restart a failing process
curl -X POST --unix-socket "$SOCK" http://localhost/processes/{name}/restart
```

### The skill

`skills/running-man/SKILL.md` tells an agent **when** to reach for Running Man, not just
how. Its first instruction is the one that matters: before starting a dev server, check
whether one is already running.

Skills are picked up from different places depending on the agent you use, so installation
is a symlink into whichever directory yours reads:

```bash
make skill:link                              # default: ~/.claude/skills
make skill:link SKILLS_DIR=~/some/other/dir  # anywhere else
make skill:unlink                            # remove it
```

A symlink rather than a copy, so editing `skills/running-man/SKILL.md` takes effect
immediately instead of silently drifting from whatever was installed. Agents generally
discover skills at session start, so restart yours after linking.

### The instance marker

While an instance is running, `.running-man/instance.json` exists in the project root:

```bash
cat .running-man/instance.json
```

It holds the API socket path, the instance's pid and start time, the OTLP endpoint when
tracing is on, every configured process, and ready-to-run `curl` hints. It is the cheap
signal that makes the rest discoverable — one file read, in a directory agents already
inspect.

The `socket` field is authoritative. It is normally `.running-man/api.sock`, but a Unix
socket path has a hard length limit, so a deeply nested project gets a short socket under
the temp directory instead:

```bash
SOCK=$(sed -n 's/.*"socket": "\(.*\)",*/\1/p' .running-man/instance.json)
```

`otlp_endpoint` matters for anything exporting telemetry from outside — a browser, or a
container with a hardcoded endpoint. The receiver takes the next free port above 4318 when
something else holds it, so the port is not fixed. Processes Running Man starts have it
injected and need no configuration.

The marker records only **stable** facts. Anything live — status, listening ports, exit
codes — comes from `/processes`, so the file cannot drift out of date about them. It is
written after the processes start and removed on exit; if it exists but `/health` does not
respond, the instance died without cleaning up and the file can be ignored.

`/health` also returns `pid`, `project` and `started`, which is how an agent confirms
*which* instance answered rather than assuming. If `project` is not the directory being
worked in, the answer belongs to something else.

### API Features
- **Glob pattern support**: Use `*` in source names (e.g., `app-*`)
- **Flexible time filters**: `since` parameter accepts durations like "30s", "5m", "1h"
- **Multi-source queries**: Comma-separated source lists
- **Trace correlation**: Automatic trace ID extraction from logs
- **OpenAPI specification**: the full spec at `/openapi.yaml`

See [api-reference.md](api-reference.md) for complete API documentation.
## Contributing

Improvements to the API surface belong in `internal/api/server.go`. Add tests alongside
the existing ones in `internal/api/server_test.go`, and update
[api-reference.md](api-reference.md) and `docs/openapi.yaml` in the same change.

Follow the locked vocabulary in [../GLOSSARY.md](../GLOSSARY.md) for any new field or
endpoint name.
