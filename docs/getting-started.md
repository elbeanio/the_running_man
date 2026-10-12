# Getting Started with The Running Man

[Home](index.md) · [Overview](overview.md) · **Getting started** · [Configuration](configuration.md) · [API reference](api-reference.md) · [Agent integration](agent-integration.md) · [Tracing](tracing.md) · [Architecture](architecture.md) · [Troubleshooting](troubleshooting.md) · [Development](development.md)

---

Welcome to The Running Man! This guide will help you get started with the dev observability tool that captures logs, traces, and errors from your local development environment.

## Installation

### Option 1: Install via Go (Recommended)

```bash
go install github.com/elbeanio/the_running_man/cmd/running-man@latest
```

### Option 2: Build from Source

```bash
git clone https://github.com/elbeanio/the_running_man.git
cd the_running_man
go build -o running-man ./cmd/running-man
sudo mv running-man /usr/local/bin/  # Or add to your PATH
```

### Verify Installation

```bash
running-man version
```

## Your First Run

### Basic Example

```bash
# Run a simple Python HTTP server
running-man run --process "python -m http.server 8080"
```

This will:
1. Start the Python HTTP server
2. Launch the TUI (Terminal User Interface) showing logs in real-time
3. Capture all stdout/stderr output
4. Make logs available over the API, on a Unix socket in the project directory

### Multiple Processes

```bash
# Run multiple development processes
running-man run \
  --process "python server.py" \
  --process "npm run dev" \
  --process "docker-compose up postgres"
```

Use **Tab** to switch between process logs in the TUI.

### Docker Compose Integration

```bash
# Monitor your entire Docker stack
running-man run --docker-compose ./docker-compose.yml
```

Running Man will:
- Parse your `docker-compose.yml` file
- Stream logs from all containers
- Show each service in a separate TUI tab
- Handle container restarts automatically

## Configuration

Most projects want a `running-man.yml` rather than long command lines. Auto-discovered from
the working directory:

```yaml
processes:
  - name: backend
    command: python server.py
  - name: frontend
    command: npm run dev

docker_compose: ./docker-compose.yml
```

Then `running-man run` needs no arguments.

Every key and every flag is documented in **[Configuration](configuration.md)**.

## Using the TUI

The Terminal User Interface (TUI) provides real-time log viewing:

### Navigation
- **Tab / →** - Switch to next source
- **Shift+Tab / ←** - Switch to previous source
- **↑/↓, PgUp/PgDn, Home/End** - Scroll; End returns to following the newest output
- **/ or Ctrl+F** - Search the current source; **n/p** for the next and previous match
- **t** - Show or hide trace IDs on log lines
- **r** - Restart the current source's process
- **q** - Quit TUI (stops all processes)

The help line at the bottom always shows the keys for the current view.

### Features
- **One tab per source** - each process and container, plus `running-man` for Running
  Man's own messages and **Traces** when tracing is on
- **Errors in red**; requests from coding agents to the API appear in cyan in the
  `running-man` tab, prefixed `api `
- **Follow mode** - each tab follows its newest output until you scroll up
- **Startup screen** - with `depends_on`, a startup tab shows each process waiting for its
  dependencies
- **Traces tab** - traces oldest to newest, opening on the newest. Enter opens a trace as a
  waterfall of its spans; Enter again opens a span's attributes, and again a whole value.
  Esc steps back a level.

### Headless Mode

For CI/CD or automation:

```bash
running-man run --process "pytest" --no-tui
# Runs processes, captures logs, but doesn't show TUI
```

## Querying Logs

### REST API

While Running Man is running, the API is served on a Unix socket in the project directory
rather than a TCP port. `curl` reaches it with `--unix-socket`, and the host in the URL is
ignored.

The path below is the usual one. A deeply nested project gets a short socket under the temp
directory instead, because a Unix socket path has a hard length limit — the `socket` field
of `.running-man/instance.json` always records which applies, and scripts and agents should
read it from there rather than hardcoding:

```bash
SOCK=.running-man/api.sock

# Recent logs
curl --unix-socket "$SOCK" "http://localhost/logs?since=30s"

# Errors only
curl --unix-socket "$SOCK" "http://localhost/errors?since=5m"

# Filter by source and level
curl --unix-socket "$SOCK" "http://localhost/logs?source=backend&level=error,warn"

# Search content
curl --unix-socket "$SOCK" "http://localhost/logs?contains=database"

# Health check
curl --unix-socket "$SOCK" "http://localhost/health"

# Process status
curl --unix-socket "$SOCK" "http://localhost/processes"
```

### Advanced Queries

```bash
# Multiple filters
curl --unix-socket "$SOCK" "http://localhost/logs?since=5m&source=backend&level=error&contains=timeout"

# The newest 100 matching entries (default 1000; limit=0 for all)
curl --unix-socket "$SOCK" "http://localhost/logs?limit=100"

# A time window back from now: a duration, or a number of seconds
curl --unix-socket "$SOCK" "http://localhost/logs?since=1h"
```

## Point your agent at it

While an instance is running, `.running-man/instance.json` sits in the project root with
the API socket path, the OTLP endpoint, the configured processes and ready-to-run `curl`
hints. Agents find it on
their own.

Install the skill so yours knows when to look:

```bash
make skill:link
```

See **[Agent integration](agent-integration.md)**.

## Tracing

Tracing is on by default. Running Man runs an OTLP receiver on port 4318 -- or the next
free port, if something else holds it -- and injects the matching `OTEL_*` variables into
the processes it starts, so an instrumented app finds it without configuration. Traces are
kept for 24 hours, on disk, so they survive a restart.

```bash
curl -s --unix-socket "$SOCK" 'http://localhost/traces?since=10m'
curl -s --unix-socket "$SOCK" http://localhost/traces/TRACE_ID/logs   # logs correlated to a trace
```

Setup for Python, Flask and Django: **[Tracing](tracing.md)**.

## Docker Compose

```bash
running-man run --docker-compose ./docker-compose.yml
```

Container logs join your process logs in the same buffer and get their own TUI tabs. If the
stack is not running, Running Man offers to start it — and leaves it running when you quit.

Profiles, multiple files and project names: **[Configuration](configuration.md#docker-integration)**.

## Common Workflows

### Development Workflow

1. **Start your development stack:**
   ```bash
   running-man run --process "python server.py" --process "npm run dev"
   ```

2. **Debug errors in TUI:**
   - Switch between processes with Tab
   - View color-coded error messages
   - See Python tracebacks grouped together

3. **Query logs via API:**
   ```bash
   curl --unix-socket "$SOCK" "http://localhost/errors?since=1m"
   ```

4. **Use AI agent for debugging:**
   - "Show me recent errors with stack traces"
   - "Check process status"
   - "Search for specific error messages"

### CI/CD Workflow

```bash
# Headless mode for tests
running-man run --process "pytest" --no-tui

# Capture test output
curl --unix-socket "$SOCK" "http://localhost/logs?since=0s" > test-output.json

# Check for errors
ERROR_COUNT=$(curl -s --unix-socket "$SOCK" "http://localhost/errors?since=0s" | jq '.count')
if [ "$ERROR_COUNT" -gt 0 ]; then
    echo "Tests failed with $ERROR_COUNT errors"
    exit 1
fi
```

### Microservices Debugging

```yaml
# running-man.yml for microservices
processes:
  - name: api-gateway
    command: go run cmd/gateway/main.go
  
  - name: user-service
    command: python services/user/main.py
  
  - name: product-service
    command: node services/product/index.js

tracing:
  enabled: true
```

```bash
# Start all services
running-man run

# Trace a request across services
# Ask an agent: "Show me traces for user ID 123"
# Or directly: curl --unix-socket "$SOCK" "http://localhost/traces?since=2m"
```

## If something is wrong

Start with what Running Man captured:

```bash
curl -s --unix-socket "$SOCK" 'http://localhost/errors?since=10m'
curl -s --unix-socket "$SOCK" http://localhost/processes        # is anything not running?
```

A process that exits non-zero is recorded as an error, so `/errors` tells you something
died even if the process itself said nothing recognisable. Check `level=warn` too — output
on stderr that matched no known error phrasing lands there.

Symptoms, causes and fixes: **[Troubleshooting](troubleshooting.md)**.

## Next Steps

Now that you're up and running, explore:

1. **[Configuration Guide](configuration.md)** - All YAML options and CLI flags
2. **[OpenTelemetry Tracing](tracing.md)** - Complete OTEL setup and examples
3. **[AI Agent Integration](agent-integration.md)** - Using the agent-facing API
4. **[API Reference](api-reference.md)** - Complete API documentation
5. **[Architecture](architecture.md)** - Understand how it works internally

Happy debugging! 🏃