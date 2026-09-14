# Agent Integration Guide

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
curl -s http://localhost:9000/processes
```

If Running Man is already supervising the process you were about to start, use it — the
service is up, its logs are already being captured, and starting a second copy will
either collide on the port or split the output across two places the developer cannot
see together.

## Using the API

### Quick Start
```bash
# Interactive API documentation
open http://localhost:9000/docs

# List all available endpoints
curl http://localhost:9000/

# System health and buffer stats
curl http://localhost:9000/health
```

### Common Debugging Patterns

#### 1. Error Investigation
```bash
# Recent errors with context
curl "http://localhost:9000/errors?since=10m"

# Search for specific error patterns
curl "http://localhost:9000/logs?contains='connection failed'&since=15m"
```

#### 2. Cross-Source Search
```bash
# Search across all backend services
curl "http://localhost:9000/logs?source=app-*&since=5m"

# Compare multiple services
curl "http://localhost:9000/logs?source=api,worker,database&since=2m"
```

#### 3. Trace-Log Correlation
```bash
# Find traces with errors
curl "http://localhost:9000/traces?status=error&since=5m"

# Get all logs for a specific trace
curl "http://localhost:9000/traces/{trace_id}/logs"
```

#### 4. Process Management
```bash
# Check all processes
curl http://localhost:9000/processes

# Restart a failing process
curl -X POST http://localhost:9000/processes/{name}/restart
```

### OpenCode Skill
For OpenCode users, load the `debug-logs` skill for comprehensive debugging guidance:

```bash
# The skill is automatically discovered when .opencode/skills/debug-logs/SKILL.md exists
# Agents can load it with: skill({ name: "debug-logs" })
```

**Installation:**
```bash
# Install skill globally for all projects
make install-skills

# Or install just to Claude skills location
make install-skill
```

The skill provides guidance on using Running Man's REST API for debugging web applications.

### API Features
- **Glob pattern support**: Use `*` in source names (e.g., `app-*`)
- **Flexible time filters**: `since` parameter accepts durations like "30s", "5m", "1h"
- **Multi-source queries**: Comma-separated source lists
- **Trace correlation**: Automatic trace ID extraction from logs
- **OpenAPI documentation**: Interactive docs at `/docs`

See [api-reference.md](api-reference.md) for complete API documentation.
## Contributing

Improvements to the API surface belong in `internal/api/server.go`. Add tests alongside
the existing ones in `internal/api/server_test.go`, and update
[api-reference.md](api-reference.md) and `docs/openapi.yaml` in the same change.

Follow the locked vocabulary in [../GLOSSARY.md](../GLOSSARY.md) for any new field or
endpoint name.
