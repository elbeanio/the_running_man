.PHONY: build test test-coverage test-integration test-race clean deps install install-local fmt lint help

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

# Link the skill into an agent's skills directory.
#
# Deliberately not opinionated about which agent harness you use: SKILLS_DIR is
# a variable, and the default is only a default. A symlink rather than a copy,
# so editing skills/running-man/SKILL.md takes effect immediately instead of
# silently drifting from whatever was installed.
SKILLS_DIR ?= $(HOME)/.claude/skills

# Colons in target names must be escaped in the Makefile but are written
# plainly on the command line: `make skill:link`.
#
# The FORCE prerequisite is not decoration. .PHONY does not work with an
# escaped colon in GNU Make 3.81 (which macOS ships), so without FORCE these
# targets would be silently skipped as "up to date" if a file named
# `skill:link` ever existed.
FORCE:

skill\:link: FORCE
	@if [ ! -d "skills/running-man" ]; then \
		echo "Error: skills/running-man not found"; \
		exit 1; \
	fi
	@mkdir -p "$(SKILLS_DIR)"
	@rm -rf "$(SKILLS_DIR)/running-man"
	@ln -s "$(CURDIR)/skills/running-man" "$(SKILLS_DIR)/running-man"
	@echo "Linked $(SKILLS_DIR)/running-man -> $(CURDIR)/skills/running-man"

skill\:unlink: FORCE
	@rm -rf "$(SKILLS_DIR)/running-man"
	@echo "Removed $(SKILLS_DIR)/running-man"

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
	@echo "  skill:link       - Symlink the agent skill into SKILLS_DIR"
	@echo "                     (default ~/.claude/skills; override with SKILLS_DIR=...)"
	@echo "  skill:unlink     - Remove that symlink"
	@echo "  fmt              - Format code"
	@echo "  lint             - Run linter"
	@echo "  help             - Show this help"
