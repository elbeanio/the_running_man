# Development Guide

[Home](index.md) · [Overview](overview.md) · [Getting started](getting-started.md) · [Configuration](configuration.md) · [API reference](api-reference.md) · [Agent integration](agent-integration.md) · [Tracing](tracing.md) · [Architecture](architecture.md) · [Troubleshooting](troubleshooting.md) · **Development**

---

This guide covers how to build, test, and contribute to The Running Man.

## Building from Source

### Prerequisites

- **Go 1.21+** - [Download Go](https://go.dev/dl/)
- **Git** - For version control
- **Docker** (optional) - For Docker integration testing

### Clone the Repository

```bash
git clone https://github.com/elbeanio/the_running_man.git
cd the_running_man
```

### Build the Binary

```bash
# Build for current platform
go build -o running-man ./cmd/running-man

# Build with version information
go build -ldflags "-X main.version=$(git describe --tags)" -o running-man ./cmd/running-man

# Install to $GOPATH/bin
go install ./cmd/running-man
```

### Cross-Compilation

```bash
# Build for Linux
GOOS=linux GOARCH=amd64 go build -o running-man-linux ./cmd/running-man

# Build for macOS (Apple Silicon)
GOOS=darwin GOARCH=arm64 go build -o running-man-macos-arm64 ./cmd/running-man

# Windows is not supported. internal/process is //go:build !windows with no
# Windows variant, because process supervision uses Setsid, syscall.Kill and
# ps/lsof. A Windows port would need a separate implementation of that package.
```

## Testing

### Run All Tests

```bash
# Run all tests
go test ./...

# Run tests with coverage
go test ./... -cover

# Run tests with verbose output
go test ./... -v
```

### Test Specific Packages

```bash
# Test API package
go test ./internal/api/...

# Test tracing package
go test ./internal/tracing/...

# Test with race detector
go test -race ./internal/process/...
```

### Integration Tests

```bash
# Run integration tests (requires Docker)
go test -tags=integration ./...

# Run specific integration test
go test -tags=integration ./internal/docker/...
```

### Test Coverage Report

```bash
# Generate coverage report
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out -o coverage.html

# View coverage by function
go tool cover -func=coverage.out
```

## Running Locally

### Development Build

```bash
# Build and run
go run ./cmd/running-man run --process "python -m http.server 8080"

# Build with debug symbols
go build -gcflags="all=-N -l" -o running-man-debug ./cmd/running-man
```

### Debugging

```bash
# Run with debug logging
./running-man run --process "python app.py"

# Use delve debugger
dlv debug ./cmd/running-man -- run --process "python app.py"
```

## Project Structure

```
the_running_man/
├── cmd/running-man/          # CLI entry point
│   ├── main.go              # Command parsing, orchestration
│   └── tui.go               # Bubble Tea TUI viewer
│
├── internal/                # Core packages
│   ├── api/                # HTTP server, REST endpoints
│   ├── config/             # YAML schema, loading, validation
│   ├── docker/             # Compose parsing, log streaming
│   ├── parser/             # Format detection, extraction
│   ├── process/            # Wrapper, manager, shell execution
│   ├── storage/            # Ring buffer implementation
│   └── tracing/            # OTEL receiver, trace storage
│
├── docs/                   # Documentation
├── running-man.yml         # Example configuration
└── go.mod                  # Go module definition
```

### Key Packages

#### `internal/api`
- **server.go** - HTTP server implementation
- **handlers.go** - REST API handlers

#### `internal/tracing`
- **receiver.go** - OTLP HTTP receiver
- **storage.go** - Span storage and querying
- **env.go** - Environment variable injection

#### `internal/process`
- **manager.go** - Process lifecycle management
- **wrapper.go** - Process execution and output capture
- **config.go** - Process configuration

#### `internal/parser`
- **parser.go** - Log format detection
- **formats/** - Specific format parsers (python, json, plain)

## Adding New Features

### Adding a New API Endpoint

1. **Add route** in `internal/api/server.go`:
```go
router.Get("/new-endpoint", s.handleNewEndpoint)
```

2. **Implement handler**:
```go
func (s *Server) handleNewEndpoint(w http.ResponseWriter, r *http.Request) {
    // Parse query parameters
    // Implement logic
    // Return JSON response
}
```

3. **Add tests** in `internal/api/server_test.go`.

### Adding a New Log Parser

1. **Create parser** in `internal/parser/formats/`:
```go
package formats

type NewFormatParser struct{}

func (p *NewFormatParser) Parse(line string, timestamp time.Time) *storage.LogEntry {
    // Parse logic
}
```

2. **Register parser** in `internal/parser/parser.go`:
```go
func NewMultiParser() *MultiParser {
    p := &MultiParser{}
    p.Register(&formats.NewFormatParser{})
    return p
}
```

## Code Style

### Formatting

```bash
# Format all code
go fmt ./...

# Run go vet for static analysis
go vet ./...

# Run staticcheck for additional checks
staticcheck ./...
```

### Naming Conventions

- **Packages**: lowercase, single word
- **Interfaces**: `-er` suffix (e.g., `Parser`, `Storage`)
- **Test files**: `_test.go` suffix
- **Test functions**: `TestXxx` prefix

### Error Handling

```go
// Use errors.New for simple errors
return errors.New("invalid configuration")

// Use fmt.Errorf for formatted errors
return fmt.Errorf("failed to parse %s: %w", filename, err)

// Use sentinel errors for specific cases
var ErrNotFound = errors.New("not found")
```

## Dependencies

### Adding a Dependency

```bash
# Add new dependency
go get github.com/example/new-package

# Update go.mod and go.sum
go mod tidy
```

### Current Dependencies

- **go.opentelemetry.io/proto/otlp** - OpenTelemetry protobufs
- **github.com/docker/docker** - Docker client
- **github.com/charmbracelet/bubbletea** - TUI framework
- **gopkg.in/yaml.v3** - YAML parsing

## Debugging

### Common Issues

#### Test Dependencies Missing
```bash
# Error: cannot import testify
# Solution: Install test dependencies
go get github.com/stretchr/testify
```

#### Docker Integration Failing
```bash
# Error: Cannot connect to Docker daemon
# Solution: Ensure Docker is running
docker ps  # Test connection
```

#### OTLP Receiver Port Conflict
```bash
# Error: Address already in use: 4318
# Solution: Change tracing port
running-man run --tracing-port 4321
```

### Debug Logging

Enable debug logging with environment variable:
```bash
./running-man run --process "python app.py"
```

Debug output includes:
- Configuration loading
- Process startup
- API server initialization
- Trace ingestion

## Release Process

### Versioning

The Running Man uses semantic versioning: `MAJOR.MINOR.PATCH`

- **MAJOR**: Breaking changes
- **MINOR**: New features, backwards compatible
- **PATCH**: Bug fixes, backwards compatible

### Creating a Release

1. **Update version** in code if needed
2. **Run tests**:
```bash
go test ./...
go test -tags=integration ./...
```

3. **Build binaries**:
```bash
./scripts/build-release.sh
```

4. **Create GitHub release** with:
   - Release notes
   - Binary attachments
   - Checksums

5. **Update documentation** if needed

### Release Checklist

- [ ] All tests pass
- [ ] Documentation updated
- [ ] Version tags updated
- [ ] Binaries built for all platforms
- [ ] Release notes written
- [ ] GitHub release created

## Contributing

### Workflow

1. **Fork the repository**
2. **Create a feature branch**:
```bash
git checkout -b feature/new-feature
```

3. **Make changes** with tests
4. **Run tests**:
```bash
go test ./...
go fmt ./...
```

5. **Commit changes**:
```bash
git commit -m "Add new feature"
```

6. **Push to fork**:
```bash
git push origin feature/new-feature
```

7. **Create pull request**

### Pull Request Guidelines

- **Description**: Clearly describe changes and motivation
- **Tests**: Include tests for new functionality
- **Documentation**: Update relevant documentation
- **Backwards compatibility**: Note any breaking changes
- **Review**: Address review comments promptly

### Code Review Checklist

- [ ] Code follows project conventions
- [ ] Tests are comprehensive
- [ ] Documentation is updated
- [ ] No breaking changes (or clearly documented)
- [ ] Performance considerations addressed
- [ ] Security implications considered

## Performance Considerations

### Memory Usage

- **Ring buffer**: Configurable size (default 50MB)
- **Trace storage**: Configurable span count (default 10,000)
- **Process management**: Minimal overhead per process

### Optimization Tips

1. **Use pointers for large structs** in slices
2. **Pre-allocate slices** when size is known
3. **Use sync.Pool** for frequently allocated objects
4. **Avoid global variables** in concurrent code
5. **Profile before optimizing**:
```bash
go test -bench=. -cpuprofile=cpu.prof
go tool pprof cpu.prof
```

## Security

### Best Practices

1. **The API is bound to all interfaces by default** (`0.0.0.0`), so anyone on the network
   can read captured logs — which often contain secrets printed by dev servers. Use
   `--listen 127.0.0.1` to restrict it. See
   [api-reference.md → Network exposure](api-reference.md#network-exposure).
2. **State-changing endpoints are loopback-only.** `POST /processes/{name}/restart` and
   `POST /processes/stop-all` return 403 for non-loopback callers unless
   `--allow-remote-control` is set. New state-changing endpoints must call
   `requireLocalControl` — do not add one without it.
3. **OTLP ingestion is unauthenticated input.** `/v1/logs` takes the source name and
   timestamp from the sender, so `otlp` entries cannot be trusted as to origin.
4. **Process isolation**: Each process runs with its environment
5. **Input validation**: Validate all API inputs
6. **No secrets in logs**: Environment variables not logged
7. **Dependency scanning**: Regular security updates

### Security Considerations

- **Process endpoints**: `/processes/{name}/restart` and `/processes/stop-all` change state
- **Docker integration**: Requires Docker socket access
- **Environment variables**: May contain secrets (not logged)
- **Configuration files**: May be in source control
