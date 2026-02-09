.PHONY: build test clean install install-local run-tests help

# Default target
all: build

# Build the binary
build:
	@echo "Building running-man..."
	@go build -o running-man ./cmd/running-man
	@echo "✓ Build successful"

# Run all tests
test:
	@echo "Running tests..."
	@go test ./...

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	@go test ./... -cover

# Run integration tests
test-integration:
	@echo "Running Phase 1 integration tests..."
	@./test_phase1.sh
	@echo ""
	@echo "Running Phase 2 integration tests..."
	@./test_phase2.sh

# Run tests with race detector
test-race:
	@echo "Running race detector..."
	@go test -race ./...

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -f running-man
	@echo "✓ Clean complete"

# Install dependencies
deps:
	@echo "Installing dependencies..."
	@go mod download
	@go mod tidy

# Install binary to GOPATH
install:
	@echo "Installing to GOPATH..."
	@go install ./cmd/running-man

# Install binary to ~/bin
install-local: build
	@echo "Installing to ~/bin..."
	@mkdir -p ~/bin
	@cp running-man ~/bin/
	@echo "✓ Installed to ~/bin/running-man"

# Format code
fmt:
	@echo "Formatting code..."
	@go fmt ./...

# Run linter
lint:
	@echo "Running linter..."
	@golangci-lint run || echo "golangci-lint not installed, skipping"

# Show help
help:
	@echo "Available targets:"
	@echo "  build            - Build the binary (default)"
	@echo "  test             - Run unit tests"
	@echo "  test-coverage    - Run tests with coverage"
	@echo "  test-integration - Run integration test scripts"
	@echo "  test-race        - Run tests with race detector"
	@echo "  clean            - Remove build artifacts"
	@echo "  deps             - Install/update dependencies"
	@echo "  install          - Install to GOPATH"
	@echo "  install-local    - Install to ~/bin"
	@echo "  fmt              - Format code"
	@echo "  lint             - Run linter"
	@echo "  help             - Show this help"
