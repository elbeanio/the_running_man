# The Running Man

## What It Is

A dev observability tool that captures logs from your local processes and Docker containers, staying alive even when your app crashes. Built for AI-assisted development.

## Why It Exists

When debugging with Claude Code or OpenCode, you spend too much time:
- Tab-switching between terminals to find the right logs
- Copy-pasting stack traces and error messages
- Missing important context (what happened before the error?)
- Starting over when the app crashes mid-debug

Running Man solves this by capturing everything automatically and exposing it via a queryable API that your coding agent can use.

## Current Capabilities

- **Multi-process management** - Run multiple processes with shell support (cd, &&, pipes, etc.)
- **Docker Compose integration** - Automatically capture logs from all your containers
- **YAML configuration** - Auto-discovery with CLI override support
- **Interactive TUI** - Tab switching between log sources, real-time updates
- **REST API** - Query logs by time, source, level, or content
- **OpenTelemetry Tracing** - Built-in OTLP receiver with automatic environment injection
- **MCP Server** - AI agent integration with 11 debugging tools (Claude Code, OpenCode)
- **Smart parsing** - Detects Python tracebacks, JSON logs, plain text
- **Ring buffer** - 30-minute retention survives app crashes
- **Trace storage** - In-memory span storage with configurable retention
- **Trace-log correlation** - Automatic correlation via `trace_id`
- **Configurable shell** - Use bash, zsh, or any shell you prefer

## What's Next

- **Phase 5:** Browser SDK for frontend observability
- **Phase 6:** Advanced visualization and analytics

**Current Status:** Complete AI agent integration via MCP protocol with 11 debugging tools, including OpenTelemetry trace exploration capabilities.

## Quick Example

```bash
# Create a config file
cat > running-man.yml <<EOF
processes:
  - name: backend
    command: python server.py
  - name: frontend  
    command: npm run dev

docker_compose: ./docker-compose.yml
api_port: 9000
EOF

# Start your stack (TUI launches automatically)
running-man run

# In another terminal, query from your agent (or manually)
curl http://localhost:9000/errors?since=30s
```

## The Key Insight

Running Man runs **outside** your application. When your Python server crashes on startup, Running Man still has the traceback. When your frontend throws an error, both sides of the story are in one queryable place.

This makes it invaluable for the hardest debugging scenarios: the ones where your app won't even start.

---

See [README.md](../README.md) for installation and detailed usage.

See [GETTING_STARTED.md](GETTING_STARTED.md) for a comprehensive getting started guide.

See [IMPLEMENTATION_HISTORY.md](IMPLEMENTATION_HISTORY.md) for historical development phases.
