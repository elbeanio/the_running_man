# The Running Man 🏃

**A dev observability tool that captures logs, traces, and errors from local development environments.**

> **Stay running when your apps crash** - Capture everything automatically and expose it via queryable APIs for AI agents and developers.

[![CI](https://github.com/elbeanio/the_running_man/actions/workflows/ci.yml/badge.svg)](https://github.com/elbeanio/the_running_man/actions/workflows/ci.yml)
[![Security Scan](https://github.com/elbeanio/the_running_man/actions/workflows/security.yml/badge.svg)](https://github.com/elbeanio/the_running_man/actions/workflows/security.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/elbeanio/the_running_man)](https://goreportcard.com/report/github.com/elbeanio/the_running_man)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

## ✨ Features

- **📊 Multi-process Management** - Run and monitor multiple processes with shell support (cd, &&, pipes)
- **🐳 Docker Compose Integration** - Automatically capture logs from all containers
- **📱 Interactive TUI** - Real-time log viewer with tab switching between sources
- **🔍 Smart Log Parsing** - Detects Python tracebacks, JSON logs, and plain text
- **📡 OpenTelemetry Tracing** - Built-in OTLP receiver with automatic environment injection
- **🤖 AI Agent Integration** - MCP server with 10+ debugging tools for Claude Code/OpenCode
- **⚡ Ring Buffer Storage** - 30-minute retention survives app crashes
- **🔧 YAML Configuration** - Auto-discovery with CLI override support

## 🚀 Quick Start

### Installation

```bash
# Install via Go
go install github.com/elbeanio/the_running_man/cmd/running-man@latest

# Or download the latest binary from Releases
```

### Basic Usage

```bash
# Run a single process (TUI launches automatically)
running-man run --process "python server.py"

# Multiple processes - switch between them with Tab
running-man run --process "python server.py" --process "npm run dev"

# Docker Compose services
running-man run --docker-compose ./docker-compose.yml

# Headless mode for CI/automation
running-man run --process "pytest" --no-tui
```

### Configuration File

Create `running-man.yml` in your project root:

```yaml
processes:
  - name: backend
    command: python server.py
  - name: frontend
    command: npm run dev

docker_compose: ./docker-compose.yml
api_port: 9000
retention: 30m
shell: /bin/bash

tracing:
  enabled: true
  port: 4318
```

Then just run:
```bash
running-man run  # auto-discovers config
```

See [running-man.yml](running-man.yml) for all configuration options.

## 📖 Documentation

- **[Getting Started](docs/getting-started.md)** - Comprehensive guide for new users
- **[Configuration Guide](docs/configuration.md)** - All YAML options and CLI flags
- **[OpenTelemetry Tracing](docs/tracing.md)** - Complete OTEL setup and usage
- **[AI Agent Integration](docs/agent-integration.md)** - MCP setup for Claude Code/OpenCode
- **[API Reference](docs/api-reference.md)** - REST API and MCP tools documentation
- **[Architecture](docs/architecture.md)** - System design and components
- **[Development Guide](docs/development.md)** - Building and contributing

## 🏗️ Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    The Running Man                          │
├─────────────────────────────────────────────────────────────┤
│  Processes  │  Docker  │  OTEL Tracing  │  Configuration    │
│             │          │                │                   │
│  ┌─────────────────────────────────────────────────────┐   │
│  │                 Ring Buffer Storage                 │   │
│  │          (30min retention, 50MB limit)             │   │
│  └─────────────────────────────────────────────────────┘   │
│                                                            │
│  ┌─────────────────┐  ┌─────────────────┐  ┌────────────┐ │
│  │   REST API      │  │   MCP Server    │  │   TUI      │ │
│  │   (Port 9000)   │  │   (/mcp)        │  │   Viewer   │ │
│  └─────────────────┘  └─────────────────┘  └────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

## 🛠️ AI Agent Integration (MCP)

Running Man includes a built-in Model Context Protocol (MCP) server for seamless AI agent integration:

**Available MCP Tools:**
- **Log Tools:** `search_logs`, `get_recent_errors`, `get_startup_logs`
- **Process Tools:** `get_process_status`, `get_process_detail`, `restart_process`, `stop_all_processes`
- **System Tools:** `get_health_status`
- **Trace Tools:** `get_traces`, `get_trace`, `get_slow_traces`

**Quick OpenCode Setup:**
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

See [Agent Integration Guide](docs/agent-integration.md) for complete setup.

## 📈 OpenTelemetry Tracing

Running Man includes built-in OpenTelemetry support:

- **OTLP HTTP receiver** on port 4318
- **Automatic environment variable injection** for managed processes
- **Trace-log correlation** via `trace_id`
- **In-memory span storage** with configurable retention
- **MCP tools for trace exploration**

**Example Python setup:**
```python
# With Running Man, OTEL environment variables are automatically injected
from opentelemetry import trace
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter

# Uses OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318 from environment
otlp_exporter = OTLPSpanExporter()
```

See [Tracing Guide](docs/tracing.md) for complete setup instructions.

## 🗺️ Roadmap

- ✅ **Phase 1:** Core Foundation (COMPLETE)
- ✅ **Phase 2:** Multi-Source Capture (COMPLETE)
- ✅ **Phase 2.5:** Quality of Life & Bug Fixes (COMPLETE)
- ✅ **Phase 3:** Agent Integration (Claude Code, OpenCode) - COMPLETE
- ✅ **Phase 4:** OpenTelemetry Tracing - COMPLETE
- 📋 **Phase 5:** Browser Integration & Web UI
- 📋 **Phase 6:** Advanced Visualization & Analytics

See [Implementation History](docs/implementation-history.md) for detailed progress.

## 🚦 Quick Examples

### Debugging with AI Agent
```bash
# Start your stack
running-man run --process "python server.py" --process "npm run dev"

# Agent can now:
# - "Show me recent errors from the backend"
# - "Check if the frontend process is running"
# - "Search logs for 'database connection' issues"
# - "Get traces for slow API requests"
```

### OpenTelemetry Setup
```bash
# Tracing enabled by default
running-man run --process "python app.py"

# View traces via API
curl http://localhost:9000/traces?since=5m

# Or use MCP tools via AI agent
# "Show me traces with errors from the last 10 minutes"
```

### Docker Development
```bash
# Monitor your entire Docker Compose stack
running-man run --docker-compose docker-compose.yml

# All container logs in one TUI
# Filter by service, search content, view errors
```

## 🏗️ Development

```bash
# Build from source
go build -o running-man ./cmd/running-man

# Run tests
go test ./...

# Run locally
./running-man run --process "python -m http.server 8080"
```

See [Development Guide](docs/development.md) for contributor information.

## 📄 License

MIT License - see [LICENSE](LICENSE) for details.

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

See [docs/implementation-history.md](docs/implementation-history.md) for the full vision and historical development phases.

## CI/CD & Security

The Running Man uses GitHub Actions for continuous integration and security scanning:

### Automated Workflows:
- **CI Pipeline** - Runs tests, linting, and builds on every push/PR
- **Security Scanning** - Weekly vulnerability checks with CodeQL and govulncheck
- **Dependency Updates** - Automated PRs for dependency updates
- **SBOM Generation** - Software Bill of Materials for releases
- **License Compliance** - Checks for problematic licenses

### Quality Gates:
- ✅ All tests must pass
- ✅ No security vulnerabilities
- ✅ Code passes linting checks
- ✅ Builds successfully on Linux, macOS, and Windows

### Development

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
- [Getting Started](docs/getting-started.md) - Quick start guide
- [Configuration](docs/configuration.md) - YAML configuration reference
- [Architecture](docs/architecture.md) - How it works
- [Implementation History](docs/implementation-history.md) - Roadmap and historical phases
- [API Reference](docs/api-reference.md) - REST API documentation
- [Agent Integration](docs/agent-integration.md) - Using with AI coding assistants
- [OpenTelemetry Tracing](docs/tracing.md) - Distributed tracing setup
- [Development Guide](docs/development.md) - Building and contributing
- [Troubleshooting](docs/troubleshooting.md) - Common issues and solutions

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
