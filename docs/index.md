# The Running Man Documentation

Welcome to The Running Man documentation! This guide will help you navigate the available resources.

## 📚 Documentation Structure

### Getting Started
- **[README.md](../README.md)** - Project overview and quick start
- **[getting-started.md](getting-started.md)** - Comprehensive guide for new users
- **[overview.md](overview.md)** - What is The Running Man and why it exists

### Usage Guides
- **[configuration.md](configuration.md)** - Complete configuration reference
- **[tracing.md](tracing.md)** - OpenTelemetry setup and usage
- **[agent-integration.md](agent-integration.md)** - AI agent (MCP) integration

### Reference
- **[api-reference.md](api-reference.md)** - REST API and MCP tools documentation
- **[architecture.md](architecture.md)** - System design and components

### Development
- **[development.md](development.md)** - Building and contributing
- **[implementation-history.md](implementation-history.md)** - Historical development phases

### Troubleshooting
- **[troubleshooting.md](troubleshooting.md)** - Common issues and solutions

## 🚀 Quick Start Path

If you're new to The Running Man, follow this path:

1. **Start with the [README.md](../README.md)** for a quick overview
2. **Follow the [getting-started.md](getting-started.md)** for installation and basic usage
3. **Configure your project** using [configuration.md](configuration.md)
4. **Explore advanced features:**
   - [OpenTelemetry Tracing](tracing.md) for distributed tracing
   - [AI Agent Integration](agent-integration.md) for MCP tools
5. **Refer to the [API Reference](api-reference.md)** for programmatic access

## 🎯 Key Documentation by Use Case

### For New Users
- [getting-started.md](getting-started.md) - Complete beginner's guide
- [overview.md](overview.md) - Understanding the project vision
- [configuration.md](configuration.md) - Setting up your first project

### For Developers Adding Tracing
- [tracing.md](tracing.md) - OpenTelemetry setup guide
- Python, Flask, Django examples
- Configuration options for tracing

### For AI Agent Integration
- [agent-integration.md](agent-integration.md) - MCP setup guide
- OpenCode and Claude Desktop configuration
- Available MCP tools and usage examples

### For API Integration
- [api-reference.md](api-reference.md) - Complete API documentation
- REST endpoints for logs, traces, and system info
- Query parameters and response formats

### For Contributors
- [development.md](development.md) - Building from source
- [architecture.md](architecture.md) - Understanding the codebase
- [implementation-history.md](implementation-history.md) - Project evolution

## 🔧 Configuration Files

### Primary Configuration
- **[running-man.yml](../running-man.yml)** - Example configuration file
- **Environment variables** - Supported in all configuration fields
- **CLI flags** - Override configuration file values

### Example Configurations
See the [configuration.md](configuration.md) guide for:
- Basic multi-process setup
- Docker Compose integration
- OpenTelemetry tracing configuration
- Environment variable usage

## 🤖 AI Agent (MCP) Tools

The Running Man provides 11 MCP tools for AI agent integration:

### Log Tools
- `search_logs` - Search logs with filters
- `get_recent_errors` - Get errors with context
- `get_startup_logs` - View logs from process startup

### Process Tools
- `get_process_status` - Check status of managed processes
- `get_process_detail` - Detailed process information
- `restart_process` - Restart a managed process
- `stop_all_processes` - Stop all processes

### System Tools
- `get_health_status` - System health and buffer stats

### Trace Tools
- `get_traces` - List recent traces with filters
- `get_trace` - Get detailed trace information
- `get_slow_traces` - Find traces exceeding duration thresholds

See [agent-integration.md](agent-integration.md) for complete details.

## 📊 OpenTelemetry Tracing

### Key Features
- **OTLP HTTP receiver** on port 4318
- **Automatic environment injection** for managed processes
- **Trace-log correlation** via `trace_id`
- **In-memory span storage** with configurable retention

### Setup Guides
- [Python applications](tracing.md#python-setup-examples)
- [Flask web applications](tracing.md#flask-web-application)
- [Django applications](tracing.md#django-application)

### Querying Traces
- **REST API**: `/traces`, `/traces/{id}`, `/traces/slow`
- **MCP tools**: `get_traces`, `get_trace`, `get_slow_traces`
- **Filters**: by service, status, duration, trace ID

## 🐳 Docker Integration

### Features
- Automatic discovery of Docker Compose services
- Real-time log streaming from all containers
- Service filtering in TUI and API
- Handles container restarts automatically

### Usage
```bash
running-man run --docker-compose ./docker-compose.yml
```

## 🔍 API Reference

### Base URL
```
http://localhost:9000
```

### Key Endpoints
- `GET /logs` - Query log entries with filters
- `GET /errors` - Recent error entries
- `GET /traces` - Query distributed traces
- `GET /health` - System status and statistics
- `GET /processes` - Status of managed processes
- `GET /mcp` - MCP server for AI agents

See [api-reference.md](api-reference.md) for complete documentation.

## 🏗️ Architecture

### Core Components
- **Process Wrapper** - Spawns and monitors child processes
- **Docker Streamer** - Streams logs from Docker containers
- **Log Parser** - Detects Python tracebacks, JSON logs, plain text
- **Ring Buffer** - In-memory storage with time/size limits
- **API Server** - REST endpoints and MCP server
- **TUI Viewer** - Interactive terminal interface
- **OTEL Receiver** - OpenTelemetry trace ingestion

### Data Flow
1. Processes/Docker containers output logs
2. Running Man captures and parses logs
3. Logs stored in ring buffer (30min/50MB retention)
4. API serves queries, TUI shows real-time view
5. AI agents connect via MCP protocol

See [architecture.md](architecture.md) for detailed architecture.

## 🚨 Troubleshooting

Common issues and solutions:

### Installation Issues
- "Command not found: running-man"
- Port conflicts (9000, 4318)
- Docker daemon not running

### Configuration Issues
- Config file not found
- Environment variable substitution
- YAML syntax errors

### Runtime Issues
- TUI rendering problems
- Logs not appearing
- Tracing not working

See [troubleshooting.md](troubleshooting.md) for complete troubleshooting guide.

## 📖 Additional Resources

### Project Links
- **GitHub Repository**: https://github.com/elbeanio/the_running_man
- **Issue Tracker**: https://github.com/elbeanio/the_running_man/issues
- **Example Configuration**: [running-man.yml](../running-man.yml)

### External Resources
- [OpenTelemetry Documentation](https://opentelemetry.io/docs/)
- [Model Context Protocol](https://spec.modelcontextprotocol.io/)
- [Bubble Tea TUI Framework](https://github.com/charmbracelet/bubbletea)

## 🤝 Contributing

Interested in contributing? See:
- [development.md](development.md) for build instructions
- [architecture.md](architecture.md) for codebase understanding
- GitHub issues for current work items

## 📄 License

MIT License - see [LICENSE](../LICENSE) for details.

---

*Documentation last updated: March 2026*