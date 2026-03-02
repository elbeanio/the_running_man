# The Running Man

A dev observability tool that captures logs, traces, and errors from local development environments.

## Quick Start

### Installation

```bash
go install github.com/elbeanio/the_running_man/cmd/running-man@latest
```

### Usage

Run your development processes (TUI launches automatically):

```bash
# Python application - TUI shows logs in real-time
running-man run --process "python server.py"

# Multiple processes - switch between them with Tab
running-man run --process "python server.py" --process "npm run dev"

# Docker Compose services - all containers visible in TUI
running-man run --docker-compose ./docker-compose.yml

# Headless mode for CI/automation
running-man run --process "pytest" --no-tui
```

### Configuration File

Create a `running-man.yml` in your project root:

```yaml
processes:
  - name: backend
    command: python server.py
  - name: frontend
    command: npm run dev

docker_compose: ./docker-compose.yml
api_port: 9000
retention: 30m
shell: /bin/bash  # optional, defaults to /bin/sh
```

Then just run:
```bash
running-man run  # auto-discovers config
```

See [running-man.yml.example](running-man.yml.example) for all options.

### TUI Navigation

- **Tab / →**: Switch to next source
- **Shift+Tab / ←**: Switch to previous source
- **q**: Quit TUI (stops all processes and exits)

### Query API

The API is available while TUI is running (use a separate terminal):

```bash
# Recent logs
curl http://localhost:9000/logs?since=30s

# Errors only
curl http://localhost:9000/errors?since=5m

# Filter by level
curl http://localhost:9000/logs?level=error,warn

# Search content
curl http://localhost:9000/logs?contains=database

# Health check
curl http://localhost:9000/health
```

### AI Agent Integration (MCP)

Running Man includes a built-in Model Context Protocol (MCP) server for AI agent integration:

```bash
# MCP endpoint available at:
http://localhost:9000/mcp
```

**Available MCP Tools:**
1. `search_logs` - Search logs with filters
2. `get_recent_errors` - Get recent errors with context
3. `get_process_status` - Check status of managed processes
4. `get_startup_logs` - View logs from process startup
5. `get_health_status` - System health and buffer stats
6. `get_process_detail` - Detailed process information
7. `restart_process` - Restart a managed process
8. `stop_all_processes` - Stop all managed processes

**OpenCode Configuration:**
```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "running-man": {
      "enabled": true,
      "type": "remote",
      "url": "http://localhost:9000/mcp"
    }
  }
}
```

See [docs/agent-integration.md](docs/agent-integration.md) for complete setup instructions.

## Features

- **Process Management**: Run and monitor multiple processes with shell support (cd, &&, pipes)
- **TUI Log Viewer**: Interactive terminal UI with tab switching between sources
- **Docker Compose**: Automatically capture logs from all containers
- **YAML Configuration**: Auto-discovery with CLI flag override support
- **Smart Parsing**: Detects Python tracebacks, JSON logs, and plain text
- **Ring Buffer**: Efficient in-memory storage (30min or 50MB default)
- **Query API**: Filter logs by time, level, source, and content
- **Configurable Shell**: Use bash, zsh, or any shell per process

## Architecture

```
the_running_man/
├── cmd/running-man/        # CLI entry point
└── internal/
    ├── process/            # Process spawning and output capture
    ├── parser/             # Log format detection and parsing
    ├── storage/            # Ring buffer implementation
    ├── docker/             # Docker Compose integration
    └── api/                # HTTP query endpoints
```

## What's Next

**Phase 4 (Next):** OTEL tracing and visualization

**Future:** Browser SDK and more

See [docs/implementation-plan.md](docs/implementation-plan.md) for the full vision.

## Development

```bash
# Build
go build -o running-man ./cmd/running-man

# Run tests
go test ./...

# Test coverage
go test ./... -cover

# Run locally
./running-man run --process "python -m http.server 8080"
```

## Documentation

- [Overview](docs/overview.md) - What is The Running Man and why?
- [Architecture](docs/architecture.md) - How it works
- [Implementation Plan](docs/implementation-plan.md) - Roadmap and phases
- [API Reference](docs/api-reference.md) - REST API documentation
- [Agent Integration](docs/agent-integration.md) - Using with AI coding assistants (Phase 3)
- [User Testing](docs/user-testing.md) - How to provide feedback

## Roadmap

- ✅ **Phase 1:** Core Foundation (COMPLETE)
- ✅ **Phase 2:** Multi-Source Capture (COMPLETE)
- ✅ **Phase 2.5:** Quality of Life & Bug Fixes (COMPLETE)
- ✅ **Phase 3:** Agent Integration (Claude Code, OpenCode) - COMPLETE
- 📋 **Phase 4:** OTEL & Visualization
- 📋 **Phase 5:** Browser Integration

See [docs/implementation-plan.md](docs/implementation-plan.md) for detailed roadmap.

## License

MIT License - see [LICENSE](LICENSE) for details
