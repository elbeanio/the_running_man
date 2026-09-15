# Getting Started with The Running Man

Welcome to The Running Man! This guide will help you get started with the dev observability tool that captures logs, traces, and errors from your local development environment.

## Installation

### Option 1: Install via Go (Recommended)

```bash
go install github.com/elbeanio/the_running_man/cmd/running-man@latest
```

### Option 2: Download Binary

Download the latest binary from the [Releases page](https://github.com/elbeanio/the_running_man/releases) and add it to your PATH.

### Option 3: Build from Source

```bash
git clone https://github.com/elbeanio/the_running_man.git
cd the_running_man
go build -o running-man ./cmd/running-man
sudo mv running-man /usr/local/bin/  # Or add to your PATH
```

### Verify Installation

```bash
running-man --version
# Should show version information
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
4. Make logs available via API on port 9000

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
- **q** - Quit TUI (stops all processes)
- **↑/↓** - Scroll through logs (when not in follow mode)

### Features
- **Color-coded log levels** (ERROR=red, WARN=yellow, INFO=white, DEBUG=gray)
- **Real-time updates** - New logs appear automatically
- **Source filtering** - Each process/container in separate tab
- **Follow mode** - Automatically scrolls to newest logs

### Headless Mode

For CI/CD or automation:

```bash
running-man run --process "pytest" --no-tui
# Runs processes, captures logs, but doesn't show TUI
```

## Querying Logs

### REST API

While Running Man is running, access the API at `http://localhost:9000`:

```bash
# Recent logs
curl "http://localhost:9000/logs?since=30s"

# Errors only
curl "http://localhost:9000/errors?since=5m"

# Filter by source and level
curl "http://localhost:9000/logs?source=backend&level=error,warn"

# Search content
curl "http://localhost:9000/logs?contains=database"

# Health check
curl "http://localhost:9000/health"

# Process status
curl "http://localhost:9000/processes"
```

### Advanced Queries

```bash
# Multiple filters
curl "http://localhost:9000/logs?since=5m&source=backend&level=error&contains=timeout"

# Pagination
curl "http://localhost:9000/logs?limit=100&offset=0"

# Time ranges
curl "http://localhost:9000/logs?since=2024-01-15T10:00:00Z&until=2024-01-15T11:00:00Z"
```

## Point your agent at it

While an instance is running, `.running-man/instance.json` sits in the project root with
the API URL, the configured processes and ready-to-run `curl` hints. Agents find it on
their own.

Install the skill so yours knows when to look:

```bash
make skill:link
```

See **[Agent integration](agent-integration.md)**.

## Tracing

Tracing is on by default. Running Man runs an OTLP receiver on port 4318 and injects the
matching `OTEL_*` variables into the processes it starts, so an instrumented app finds it
without configuration.

```bash
curl -s 'http://localhost:9000/traces?since=10m'
curl -s http://localhost:9000/traces/TRACE_ID/logs   # logs correlated to a trace
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
   curl "http://localhost:9000/errors?since=1m"
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
curl "http://localhost:9000/logs?since=0s" > test-output.json

# Check for errors
ERROR_COUNT=$(curl -s "http://localhost:9000/errors?since=0s" | jq '.count')
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
# Or directly: curl "http://localhost:9000/traces?since=2m"
```

## If something is wrong

Start with what Running Man captured:

```bash
curl -s 'http://localhost:9000/errors?since=10m'
curl -s http://localhost:9000/processes        # is anything not running?
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