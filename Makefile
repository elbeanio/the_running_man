.PHONY: build test clean install install-local run-tests help

# Default target
all: build

# Build the binary
build:
	@echo "Generating embedded files..."
	@go generate ./internal/api/...
	@echo "Building running-man..."
	@go build -o running-man ./cmd/running-man
	@echo "✓ Build successful"

# Timeout for test runs. The suite completes in ~5s, so this only ever fires on a
# genuine hang -- which is a far better signal than the Go default of 10m.
TEST_TIMEOUT ?= 60s

# Run all tests
test:
	@echo "Running tests..."
	@go test ./... -timeout $(TEST_TIMEOUT)

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	@go test ./... -cover -timeout $(TEST_TIMEOUT)

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
	@go test -race ./... -timeout $(TEST_TIMEOUT)

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

# Install OpenCode skill to ~/.claude/skills
install-skill:
	@echo "Installing the running-man skill to ~/.claude/skills..."
	@mkdir -p ~/.claude/skills/running-man
	@if [ -d ".opencode/skills/running-man" ]; then \
		cp -r .opencode/skills/running-man/* ~/.claude/skills/running-man/; \
		echo "✓ Skill installed to ~/.claude/skills/running-man"; \
	else \
		echo "❌ Error: .opencode/skills/running-man not found"; \
		exit 1; \
	fi

# Install the skill for both Claude Code and OpenCode
install-skills: install-skill
	@echo "Installing the running-man skill to ~/.config/opencode/skills..."
	@mkdir -p ~/.config/opencode/skills/running-man
	@if [ -d ".opencode/skills/running-man" ]; then \
		cp -r .opencode/skills/running-man/* ~/.config/opencode/skills/running-man/; \
		echo "✓ Skill installed to ~/.config/opencode/skills/running-man"; \
	else \
		echo "❌ Error: .opencode/skills/running-man not found"; \
		exit 1; \
	fi

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
	@echo "  install-skill    - Install OpenCode skill to ~/.claude/skills"
	@echo "  install-skills   - Install skill to both OpenCode and Claude locations"
	@echo "  fmt              - Format code"
	@echo "  lint             - Run linter"
	@echo "  help             - Show this help"
