# The Running Man

A dev observability tool that captures logs, traces, and errors from local development environments.

## Quick Start

### Installation

```bash
go install github.com/iangeorge/the_running_man/cmd/running-man@latest
```

### Usage

Wrap your development process (TUI launches automatically):

```bash
# Python application - TUI shows logs in real-time
running-man run --wrap "python server.py"

# Multiple processes - switch between them with Tab
running-man run --wrap "python server.py" --wrap "npm run dev"

# Docker Compose services - all containers visible in TUI
running-man run --docker-compose ./docker-compose.yml

# Headless mode for CI/automation
running-man run --wrap "pytest" --no-tui
```

### TUI Navigation

- **Tab / →**: Switch to next source
- **Shift+Tab / ←**: Switch to previous source
- **q**: Quit TUI (processes keep running)

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

## Features

- **Process Wrapping**: Capture stdout/stderr from any process
- **Smart Parsing**: Detects Python tracebacks, JSON logs, and plain text
- **Ring Buffer**: Efficient in-memory storage (30min or 50MB default)
- **Query API**: Filter logs by time, level, source, and content
- **Zero Config**: Works out of the box for simple cases

## Architecture

```
the_running_man/
├── cmd/running-man/        # CLI entry point
└── internal/
    ├── wrapper/            # Process spawning and output capture
    ├── parser/             # Log format detection and parsing
    ├── storage/            # Ring buffer implementation
    └── api/                # HTTP query endpoints
```

## Development

```bash
# Build
go build -o running-man ./cmd/running-man

# Run tests
go test ./...

# Run locally
./running-man run -- python -m http.server 8080
```

## Roadmap

- [x] Phase 1: Core Foundation (MVP)
- [ ] Phase 2: Multi-Source Capture (Docker, multiple processes)
- [ ] Phase 3: Browser Log Capture
- [ ] Phase 4: OTEL Integration
- [ ] Phase 5: Polish & Production-Ready

See [docs/implementation-plan.md](docs/implementation-plan.md) for detailed roadmap.

## License

MIT License - see [LICENSE](LICENSE) for details
