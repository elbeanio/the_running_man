# The Running Man

A dev observability tool that captures logs, traces, and errors from local development environments.

## Quick Start

### Installation

```bash
go install github.com/iangeorge/the_running_man/cmd/running-man@latest
```

### Usage

Wrap your development process:

```bash
# Python application
running-man run -- python server.py

# Node.js application
running-man run -- npm run dev

# Custom API port
running-man run --api-port 9000 -- python manage.py runserver
```

### Query API

Once running, query logs via HTTP:

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
go build -o running-man cmd/running-man/main.go

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
