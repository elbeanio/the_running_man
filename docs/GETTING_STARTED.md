# Getting Started with The Running Man

Welcome to The Running Man! This guide will help you get started with the dev observability tool that captures logs, traces, and errors from your local development environment.

## 📦 Installation

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

## 🚀 Your First Run

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

## ⚙️ Configuration

### Configuration File

Create a `running-man.yml` file in your project root:

```yaml
# running-man.yml
processes:
  - name: backend
    command: python server.py
    restart_on_crash: true  # Auto-restart on failure

  - name: frontend
    command: npm run dev
    shell: /bin/bash  # Use bash for shell features

docker_compose: ./docker-compose.yml

api_port: 9000
retention: 30m  # Keep logs for 30 minutes

tracing:
  enabled: true
  port: 4318
  max_spans: 10000
```

### Auto-discovery

Running Man automatically searches up the directory tree for `running-man.yml`:

```bash
# From anywhere in your project
running-man run  # Finds and uses running-man.yml
```

### CLI Flags Override Config

```bash
# Override specific settings
running-man run \
  --api-port 8080 \
  --tracing false \
  --process "custom command"
```

## 🖥️ Using the TUI

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

## 🔍 Querying Logs

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

## 🤖 AI Agent Integration

### MCP Server

Running Man includes a Model Context Protocol (MCP) server for AI agent integration:

```bash
# MCP endpoint available at:
http://localhost:9000/mcp
```

### OpenCode Setup

Add to `~/.config/opencode/opencode.json`:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "running-man": {
      "enabled": true,
      "type": "remote",
      "url": "http://localhost:9000/mcp"
    }
  },
  "permission": {
    "running-man_*": "allow"
  }
}
```

### Claude Desktop Setup

Add to `~/Library/Application Support/Claude/claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "running-man": {
      "command": "npx",
      "args": [
        "-y",
        "@modelcontextprotocol/server-http",
        "http://localhost:9000/mcp"
      ]
    }
  }
}
```

### Agent Commands Examples

Once configured, your AI agent can:
- "Show me recent errors from the backend"
- "Check if the frontend process is running"
- "Search logs for 'database connection' issues"
- "Get startup logs to see why a process failed"
- "Show me slow traces from the last 5 minutes"

## 📊 OpenTelemetry Tracing

### Enable Tracing

Tracing is enabled by default. To verify:

```bash
running-man run --process "echo 'Hello'" --no-tui
# Should show: "Tracing: OTLP receiver on http://localhost:4318"
```

### Python Application Setup

**requirements.txt:**
```txt
opentelemetry-api==1.28.0
opentelemetry-sdk==1.28.0
opentelemetry-exporter-otlp==1.28.0
```

**app.py:**
```python
from opentelemetry import trace
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor

# Running Man automatically sets OTEL_EXPORTER_OTLP_ENDPOINT
trace.set_tracer_provider(TracerProvider())
tracer_provider = trace.get_tracer_provider()

otlp_exporter = OTLPSpanExporter()
span_processor = BatchSpanProcessor(otlp_exporter)
tracer_provider.add_span_processor(span_processor)

tracer = trace.get_tracer(__name__)

# Create spans
with tracer.start_as_current_span("my_operation"):
    # Your code here
    pass
```

### Query Traces

```bash
# Via REST API
curl "http://localhost:9000/traces?since=5m"
curl "http://localhost:9000/traces/abc123-def456"  # Specific trace

# Via MCP (through AI agent)
# "Show me traces with errors"
# "Find slow traces from the backend service"
```

## 🐳 Docker Development

### Basic Docker Compose

```yaml
# docker-compose.yml
version: '3.8'
services:
  postgres:
    image: postgres:15
    environment:
      POSTGRES_PASSWORD: password
  
  redis:
    image: redis:7-alpine
  
  app:
    build: .
    depends_on:
      - postgres
      - redis
```

```bash
# Monitor all services
running-man run --docker-compose ./docker-compose.yml
```

### Environment Variables

Running Man supports environment variable substitution in configuration:

```yaml
processes:
  - name: app
    command: python server.py --port ${PORT:-8000}
    # Uses PORT env var, defaults to 8000
```

## 🔧 Common Workflows

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
# Use MCP: "Show me traces for user ID 123"
# Or API: curl "http://localhost:9000/traces?since=2m"
```

## 🚨 Troubleshooting

### Common Issues

**"Port already in use"**
```bash
# Change API port
running-man run --api-port 9001

# Change tracing port
running-man run --tracing-port 4321
```

**"Command not found: running-man"**
- Ensure Go binary is in your PATH
- Or use full path: `~/go/bin/running-man`

**"TUI rendering issues"**
- Try a different terminal (iTerm2, Alacritty, WezTerm)
- Ensure terminal supports ANSI escape codes

**"Docker logs not appearing"**
- Ensure Docker daemon is running
- Check `docker ps` to see if containers are running
- Verify docker-compose.yml path is correct

### Getting Help

- Check the [Troubleshooting Guide](TROUBLESHOOTING.md)
- Review [Configuration Guide](CONFIGURATION.md) for all options
- See [API Reference](api-reference.md) for endpoint details
- File issues on [GitHub](https://github.com/elbeanio/the_running_man/issues)

## 📚 Next Steps

Now that you're up and running, explore:

1. **[Configuration Guide](CONFIGURATION.md)** - All YAML options and CLI flags
2. **[OpenTelemetry Tracing](TRACING.md)** - Complete OTEL setup and examples
3. **[AI Agent Integration](agent-integration.md)** - Advanced MCP usage
4. **[API Reference](api-reference.md)** - Complete API documentation
5. **[Architecture](architecture.md)** - Understand how it works internally

Happy debugging! 🏃