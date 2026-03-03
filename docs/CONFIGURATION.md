# Configuration Guide

The Running Man supports comprehensive configuration through YAML files, CLI flags, and environment variables.

## 📁 Configuration File

### File Location

Running Man automatically searches for configuration files in this order:

1. Path specified by `--config` flag
2. `running-man.yml` in current directory
3. `running-man.yml` in parent directories (up to 5 levels up)
4. `~/.config/running-man/config.yml` (user config)

### Example Configuration

```yaml
# running-man.yml
processes:
  - name: backend
    command: python server.py
    args: ["--port", "8080"]
    restart_on_crash: true
    shell: /bin/bash

  - name: frontend
    command: npm run dev
    # Shell features work! Use cd, &&, pipes, etc.

docker_compose: ./docker-compose.yml

api_port: 9000
retention: 30m
max_entries: 10000
max_bytes: 52428800

shell: /bin/sh

tracing:
  enabled: true
  port: 4318
  max_spans: 10000
  max_span_age: 30m
```

## ⚙️ Configuration Options

### Processes Configuration

#### `processes` (array)
List of processes to run and manage.

**Each process supports:**
- `name` (string, required): Unique identifier for the process
- `command` (string, required): Command to execute
- `args` (array of strings, optional): Command arguments
- `restart_on_crash` (boolean, optional): Auto-restart on non-zero exit (default: `false`)
- `shell` (string, optional): Shell to use for this process (overrides global `shell`)

**Example:**
```yaml
processes:
  - name: api-server
    command: go run cmd/api/main.go
    args: ["--port", "3000"]
    restart_on_crash: true

  - name: worker
    command: cd workers && python worker.py
    shell: /bin/bash  # Use bash for cd command
```

### Docker Integration

#### `docker_compose` (string, optional)
Path to Docker Compose file. Running Man will:
- Parse the compose file to discover services
- Stream logs from all containers
- Show each service in TUI tabs
- Handle container restarts automatically

**Example:**
```yaml
docker_compose: ./docker-compose.yml
docker_compose: ../docker/docker-compose.dev.yml
```

### API Configuration

#### `api_port` (integer, optional)
Port for the HTTP API server (default: `9000`).

**Example:**
```yaml
api_port: 8080
api_port: 9001
```

### Log Retention

#### `retention` (duration string, optional)
How long to keep logs in memory (default: `30m`).

**Supported formats:**
- `30s` - 30 seconds
- `5m` - 5 minutes
- `1h` - 1 hour
- `24h` - 24 hours

**Example:**
```yaml
retention: 1h
retention: 24h
```

#### `max_entries` (integer, optional)
Maximum number of log entries to keep (default: `10000`).

**Example:**
```yaml
max_entries: 5000
max_entries: 20000
```

#### `max_bytes` (integer, optional)
Maximum total bytes of logs to keep (default: `52428800` = 50MB).

**Example:**
```yaml
max_bytes: 10485760  # 10MB
max_bytes: 1073741824  # 1GB
```

### Shell Configuration

#### `shell` (string, optional)
Shell to use for process execution (default: `/bin/sh`).

Allows you to use shell-specific features:
- `/bin/sh` - POSIX shell (default)
- `/bin/bash` - Bash with arrays, `[[ ]]`, etc.
- `/bin/zsh` - Zsh with advanced globbing
- `/usr/bin/fish` - Fish shell

**Example:**
```yaml
shell: /bin/bash
shell: /bin/zsh
```

### OpenTelemetry Tracing

#### `tracing` (object, optional)
OpenTelemetry tracing configuration.

**Sub-fields:**
- `enabled` (boolean): Enable/disable tracing (default: `true`)
- `port` (integer): OTLP HTTP receiver port (default: `4318`)
- `max_spans` (integer): Maximum spans to store (default: `10000`)
- `max_span_age` (duration): How long to keep spans (default: `30m`)

**Example:**
```yaml
tracing:
  enabled: true
  port: 4321  # If 4318 is occupied
  max_spans: 5000
  max_span_age: 1h
```

## 🔧 Environment Variable Substitution

Running Man supports environment variable substitution in configuration values.

### Syntax
- `${VAR}` - Replace with environment variable `VAR`
- `${VAR:-default}` - Use `default` if `VAR` is not set
- `$VAR` - Simple variable expansion (no default)

### Example
```yaml
processes:
  - name: backend
    command: python server.py --port ${PORT:-8000}
    # Uses PORT env var, defaults to 8000

  - name: frontend
    command: npm run ${NODE_ENV:-development}
    # Uses NODE_ENV, defaults to "development"

docker_compose: ${DOCKER_COMPOSE_PATH:-./docker-compose.yml}
```

### Using in Shell Commands
```yaml
processes:
  - name: app
    command: cd $PROJECT_DIR && python main.py
    # Uses PROJECT_DIR environment variable
```

## 🚩 CLI Flags

CLI flags override configuration file values.

### Basic Flags
```bash
# Run processes
running-man run --process "python server.py"

# Multiple processes
running-man run \
  --process "python server.py" \
  --process "npm run dev"

# Docker Compose
running-man run --docker-compose ./docker-compose.yml

# Configuration file
running-man run --config my-config.yml
```

### Override Configuration
```bash
# Override API port
running-man run --api-port 8080

# Override retention
running-man run --retention 1h

# Disable TUI (headless mode)
running-man run --no-tui

# Override shell
running-man run --shell /bin/bash
```

### Tracing Flags
```bash
# Disable tracing
running-man run --tracing false

# Change tracing port
running-man run --tracing-port 4321

# Configure span limits
running-man run --max-spans 5000 --max-span-age 1h
```

### Complete Flag Reference

| Flag | Description | Default |
|------|-------------|---------|
| `--process` | Process to run (can be specified multiple times) | - |
| `--docker-compose` | Path to Docker Compose file | - |
| `--config` | Path to configuration file | auto-discover |
| `--api-port` | API server port | 9000 |
| `--retention` | Log retention duration | 30m |
| `--max-entries` | Maximum log entries | 10000 |
| `--max-bytes` | Maximum log bytes | 50MB |
| `--shell` | Shell for process execution | /bin/sh |
| `--tracing` | Enable/disable OpenTelemetry tracing | true |
| `--tracing-port` | OTLP receiver port | 4318 |
| `--max-spans` | Maximum spans to store | 10000 |
| `--max-span-age` | Maximum span age | 30m |
| `--no-tui` | Run in headless mode (no TUI) | false |
| `--help` | Show help | - |
| `--version` | Show version | - |

## 📋 Configuration Examples

### Basic Development Setup
```yaml
# running-man.yml
processes:
  - name: api
    command: go run cmd/api/main.go
    restart_on_crash: true

  - name: frontend
    command: npm run dev

  - name: database
    command: docker-compose up postgres

api_port: 9000
retention: 1h
shell: /bin/bash
```

### Microservices with Tracing
```yaml
# running-man.yml
processes:
  - name: gateway
    command: go run services/gateway/main.go

  - name: users
    command: python services/users/main.py

  - name: products
    command: node services/products/index.js

  - name: orders
    command: python services/orders/main.py

docker_compose: ./infra/docker-compose.yml

tracing:
  enabled: true
  max_spans: 20000
  max_span_age: 1h

api_port: 9000
retention: 2h
```

### CI/CD Pipeline
```yaml
# .github/running-man.yml
processes:
  - name: tests
    command: pytest tests/ --cov=app --cov-report=html

  - name: lint
    command: black --check . && isort --check . && flake8 .

  - name: build
    command: docker build -t myapp:latest .

api_port: 9000
retention: 10m  # Shorter for CI runs
max_entries: 1000
no_tui: true  # Headless mode for CI
```

### Environment-Specific Configuration
```yaml
# running-man.yml
processes:
  - name: app
    command: python main.py --env ${ENVIRONMENT:-development}
    args:
      - "--port"
      - "${PORT:-8000}"
      - "--debug"
      - "${DEBUG:-false}"

# Use different configs per environment
# ENVIRONMENT=production running-man run
# ENVIRONMENT=staging running-man run
```

## 🔄 Configuration Precedence

Running Man uses this precedence order (highest to lowest):

1. **CLI flags** - Direct command-line arguments
2. **Environment variables** - In configuration file values
3. **Configuration file** - YAML configuration
4. **Defaults** - Built-in default values

### Example
```bash
# CLI flag overrides config file
running-man run --api-port 8080
# Uses port 8080 even if config says 9000

# Environment variable in config
export PORT=3000
running-man run
# Config uses ${PORT:-8000} → 3000
```

## 🧪 Validation

Running Man validates configuration with helpful error messages:

### Common Validation Errors
```yaml
# Error: Missing required field
processes:
  - command: python app.py  # Missing 'name'

# Error: Invalid duration
retention: 30  # Should be '30s', '30m', etc.

# Error: Port in use
api_port: 80  # Requires root privileges

# Error: File not found
docker_compose: ./nonexistent.yml
```

### Debugging Configuration
```bash
# Dry run to validate config
running-man run --dry-run

# Show effective configuration
running-man run --verbose

# Check config file syntax
running-man validate --config my-config.yml
```

## 📁 Multiple Configuration Files

### Layered Configuration
You can use multiple configuration files:

```bash
# Base configuration
running-man run --config base.yml

# Override with environment-specific config
running-man run --config base.yml --config production.yml
```

### Example Layered Setup
```yaml
# base.yml (shared settings)
processes:
  - name: app
    command: python app.py

api_port: 9000
retention: 30m
```

```yaml
# development.yml (development overrides)
shell: /bin/bash
tracing:
  enabled: true
```

```yaml
# production.yml (production overrides)
retention: 5m  # Shorter retention in production
max_entries: 1000
```

## 🔍 Configuration Tips

### 1. Use Environment Variables for Secrets
```yaml
# Don't hardcode secrets
processes:
  - name: app
    command: python app.py --db-url ${DATABASE_URL}
```

### 2. Default Values for Flexibility
```yaml
processes:
  - name: app
    command: python app.py --port ${PORT:-8000} --workers ${WORKERS:-4}
```

### 3. Shell-Specific Configuration
```yaml
shell: /bin/bash

processes:
  - name: complex
    command: |
      cd /app && \
      source venv/bin/activate && \
      python -m uvicorn app:app --host 0.0.0.0 --port 8000
```

### 4. Process-Specific Shell
```yaml
processes:
  - name: bash-script
    command: ./script.sh
    shell: /bin/bash  # This script needs bash

  - name: python-app
    command: python app.py
    # Uses global shell or default
```

### 5. Conditional Configuration
```yaml
# Use different commands based on environment
processes:
  - name: app
    command: |
      if [ "$ENVIRONMENT" = "production" ]; then
        gunicorn app:app
      else
        python -m uvicorn app:app --reload
      fi
```

## 🚨 Common Issues

### Configuration File Not Found
```bash
# Error: No configuration file found
# Solution: Create running-man.yml or use --process flags
running-man run --process "python app.py"
```

### Permission Denied
```bash
# Error: Permission denied for shell
# Solution: Use absolute path or check permissions
shell: /usr/bin/bash  # Instead of /bin/bash
```

### Port Conflicts
```bash
# Error: Address already in use
# Solution: Change port or stop conflicting service
api_port: 9001
tracing_port: 4321
```

### Invalid Duration Format
```yaml
# Error: Invalid duration '30'
# Solution: Use proper duration format
retention: 30m  # Correct
retention: 30   # Incorrect
```

## 📚 Related Documentation

- [GETTING_STARTED.md](GETTING_STARTED.md) - Getting started guide
- [TRACING.md](TRACING.md) - OpenTelemetry configuration
- [api-reference.md](api-reference.md) - API configuration reference
- [TROUBLESHOOTING.md](TROUBLESHOOTING.md) - Troubleshooting configuration issues

---

*Need help?* Check the example [running-man.yml](../running-man.yml) file or file an issue on [GitHub](https://github.com/elbeanio/the_running_man/issues).