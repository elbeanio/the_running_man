# Configuration Guide

[Home](index.md) · [Overview](overview.md) · [Getting started](getting-started.md) · **Configuration** · [API reference](api-reference.md) · [Agent integration](agent-integration.md) · [Tracing](tracing.md) · [Architecture](architecture.md) · [Troubleshooting](troubleshooting.md) · [Development](development.md)

---

The Running Man supports comprehensive configuration through YAML files, CLI flags, and environment variables.

## Configuration File

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
    type: api
    description: "Python API server"
    command: python server.py
    args: ["--port", "8080"]
    restart_on_crash: true
    shell: /bin/bash

  - name: frontend
    type: web
    description: "React frontend with Vite"
    url: http://localhost:5173
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

## Configuration Options

### Processes Configuration

#### `processes` (array)
List of processes to run and manage.

**Each process supports:**
- `name` (string, required): Unique identifier for the process
- `type` (string, optional): Process type (web, api, worker, database, cache, etc.)
- `description` (string, optional): Free-text description of the process
- `url` (string, optional): URL for web applications (e.g., http://localhost:3000)
- `command` (string, required): Command to execute
- `args` (array of strings, optional): Command arguments
- `restart_on_crash` (boolean, optional): Auto-restart on non-zero exit (default: `false`)
- `shell` (string, optional): Shell to use for this process (overrides global `shell`)
- `interval` (string, optional): Interval for recurring execution (e.g., "30s", "1m", "5m", "1h"). Must be **positive** — a zero or negative interval is rejected at startup.

**Example:**
```yaml
processes:
  - name: api-server
    type: api
    description: "Go REST API server"
    command: go run cmd/api/main.go
    args: ["--port", "3000"]
    restart_on_crash: true

  - name: frontend
    type: web
    description: "React frontend with Vite"
    url: http://localhost:5173
    command: npm run dev

  - name: worker
    type: worker
    description: "Background job processor"
    command: cd workers && python worker.py
    shell: /bin/bash  # Use bash for cd command

  # Recurring processes (run at specified intervals)
  - name: health-check
    type: recurring
    description: "Health check that runs every minute"
    command: ./scripts/check-health.sh
    interval: 1m  # Run every minute

  - name: db-backup
    type: recurring
    description: "Hourly database backup"
    command: ./scripts/backup-db.sh
    interval: 1h  # Run every hour
```

#### Recurring Processes

Processes with an `interval` field run repeatedly at the specified interval until The Running Man is stopped.

**Features:**
- **Immediate execution**: Runs immediately when started, then at each interval
- **Log capture**: All output is captured and available via logs/API
- **Independent runs**: Each execution is independent (no state between runs)
- **Error handling**: Failed executions don't stop the recurring schedule

**Supported interval formats:**
- `30s` - 30 seconds
- `1m` - 1 minute
- `5m` - 5 minutes
- `1h` - 1 hour
- `24h` - 24 hours

**Use cases:**
- Health checks and monitoring scripts
- Scheduled backups
- Periodic data synchronization
- Cron-like tasks without cron
- Regular maintenance scripts

**Note**: Recurring processes run forever until The Running Man is stopped. They don't participate in the normal process wait/exit logic.

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
```

#### Structured form

For projects with profiles, several Compose files, or a project name that is not the
directory name:

```yaml
docker_compose:
  files:
    - docker-compose.yml
    - docker-compose.override.yml   # equivalent to repeated -f; later files win
  profiles: [api, workers]
  project_name: myproject
  env_file: .env
  start: ask                        # ask | never | always
  start_timeout: 5m                 # how long to wait for containers (default: 30s)
```

| Key | Meaning |
|---|---|
| `files` | Compose files in order. Merged by service name, later files winning. |
| `profiles` | Active Compose profiles. Services gated behind an inactive profile are not expected, and are reported as "not watched" rather than silently ignored. |
| `project_name` | Overrides the project name. **Set this if you use `docker compose -p` or `COMPOSE_PROJECT_NAME`** — container discovery filters on the project label, so a mismatch finds nothing at all and Running Man will tell you to start a stack you already started. |
| `env_file` | Passed to Compose as `--env-file`. |
| `start` | What to do when nothing is running: `ask` (default), `never`, `always`. |
| `start_timeout` | How long to wait for containers to appear after starting the stack (default: `30s`). Raise it for a stack that brings up a database, migrates it, then starts services behind that. |

The plain string form is still valid and equivalent to `files: [path]`.

#### Offering to start the stack

When no containers are running, Running Man offers to start them:

```
[running-man] No containers are running for Compose project "myproject".
[running-man] Running Man can start it for you:

    docker compose -f docker-compose.yml --profile api up -d

[running-man] The stack is yours: it will be left running when running-man exits.
[running-man] Start it now? [y/N]
```

**Running Man does not manage your stack.** It offers to start it and then monitors the
logs. There is no teardown: whatever it starts is left running when you quit, exactly as
if you had run `docker compose up -d` yourself. The exact command is always shown before
anything happens.

`ask` requires a terminal to answer, so in CI or when output is piped it behaves as
`never` and reports that the stack is not running. Use `start: always` or
`--compose-start=always` to start without asking.

After starting, Running Man waits up to `start_timeout` (30s by default) for the first
container to appear, then reports that none did and suggests `docker compose ps`. A stack
that has to bring up a database, run migrations and only then start the services behind
them will exceed that — give it a `start_timeout` that covers a cold start.

Only offered when **nothing** is running. If some expected services are up and others are
not, the missing ones are listed and Running Man watches what exists — starting more
services than you expected is worse than an honest warning.

**Compose CLI:** prefers `docker compose` (v2), falling back to the standalone
`docker-compose` binary.

**Limitation:** `${VAR}` interpolation inside the Compose file is not performed. Only
service names and their `profiles` are read, which are rarely interpolated.

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
How long to keep logs in memory (default: `30m`). Must be **positive** — a zero or
negative retention is rejected at startup, since it would discard every log entry the
moment it arrived.

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
- `enabled` (boolean): Enable/disable tracing (default: `true`). **Removing the `tracing:`
  block does not disable tracing** — absence means the default applies. Set `enabled: false`.
- `port` (integer): OTLP HTTP receiver port (default: `4318`). Other collectors default to
  4318 as well — Arize Phoenix, the OpenTelemetry Collector, Jaeger — so change this if one
  of them is already running. A conflict fails at startup rather than silently disabling
  tracing.
- `max_spans` (integer): Maximum spans to store (default: `10000`)
- `max_span_age` (duration): How long to keep spans (default: `30m`). Must be **positive**.

**Example:**
```yaml
tracing:
  enabled: true
  port: 4321  # If 4318 is occupied
  max_spans: 5000
  max_span_age: 1h
```

## Environment Variable Substitution

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

## CLI Flags

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

### Overriding config from the command line

Only some settings have flags. Everything else is configuration-file only — there are no
`--retention`, `--shell`, `--max-entries`, `--max-bytes`, `--max-spans` or `--max-span-age`
flags, despite earlier versions of this page claiming otherwise.

```bash
# Override the API port
running-man run --api-port 8080

# Restrict the API to this machine
running-man run --listen 127.0.0.1

# Headless mode, for CI
running-man run --no-tui

# Tracing
running-man run --tracing=false
running-man run --tracing-port 4321
```

### Complete flag reference

Every flag `running-man run` accepts:

| Flag | Description | Default |
|------|-------------|---------|
| `--config PATH` | Path to configuration file | auto-discover |
| `--process "CMD"` | Process to run (repeatable) | - |
| `--docker-compose PATH` | Compose file (replaces the configured list) | - |
| `--compose-profile NAME` | Active Compose profile (repeatable or comma-separated) | - |
| `--compose-project NAME` | Compose project name | directory name |
| `--compose-start MODE` | When the stack is down: `ask`, `never`, `always` | `ask` |
| `--compose-start-timeout DURATION` | How long to wait for containers after starting the stack | `30s` |
| `--api-port PORT` | API server port | 9000 |
| `--listen ADDR` | Address to bind the API to | `0.0.0.0` |
| `--allow-remote-control` | Serve process restart/stop to remote callers | false |
| `--no-tui` | Run headless (no TUI) | false |
| `--keep-alive MODE` | After a failure in headless mode: `auto`, `always`, `never` | `auto` |
| `--tracing` | Enable OpenTelemetry tracing | true |
| `--tracing-port PORT` | OTLP receiver port | 4318 |

Settings with **no flag** — use `running-man.yml`: `retention`, `max_entries`,
`max_bytes`, `shell`, and everything under `tracing:` except port and enablement.

`version` and `help` are subcommands, not flags:

```bash
running-man version
running-man help
```

### Keeping logs after a crash

The ring buffer is in memory, so when Running Man exits its logs are gone. In headless
mode that used to happen the moment the last process exited — destroying exactly the
logs that explain a crash.

`--keep-alive` controls what happens after a process exits non-zero in headless mode:

| Mode | Behaviour |
|---|---|
| `auto` (default) | Keep serving logs **only when stdout is a terminal**, i.e. when someone is plausibly watching. In a pipe or a CI job, exit immediately. |
| `always` | Always keep serving. Requires Ctrl+C to quit. |
| `never` | Always exit immediately, even interactively. |

The `auto` default exists because the two needs conflict: interactively you want the logs
to survive the crash, but in CI a process that never exits would hang the job on the very
failure it is meant to report.

Headless mode **exits non-zero** when any managed process fails, in every mode.

## Configuration Examples

### Basic Development Setup
```yaml
# running-man.yml
processes:
  - name: api
    type: api
    description: "Go API server"
    command: go run cmd/api/main.go
    restart_on_crash: true

  - name: frontend
    type: web
    description: "React frontend"
    url: http://localhost:3000
    command: npm run dev

  - name: database
    type: database
    description: "PostgreSQL database"
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
    type: api
    description: "API Gateway"
    command: go run services/gateway/main.go

  - name: users
    type: api
    description: "User service"
    command: python services/users/main.py

  - name: products
    type: api
    description: "Product catalog service"
    command: node services/products/index.js

  - name: orders
    type: api
    description: "Order processing service"
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

## Configuration Precedence

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

## Validation

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

### Checking your configuration

There is no dry-run, no `validate` subcommand and no verbosity flag — earlier versions of
this page described all three. Configuration is validated when Running Man starts, before
any process is launched, and errors name the offending key:

```
Error loading config: invalid config in ./running-man.yml: process 'bad' has a
non-positive interval '-1m': recurring processes need a positive interval such
as "30s" or "1m"
```

To see what Running Man resolved, start it and read the banner and the startup lines: they
report the API address and posture, the Compose files and profiles, the services being
watched, and which are not.

## Multiple Configuration Files

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

## Configuration Tips

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

## Common Issues

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

## Related Documentation

- [getting-started.md](getting-started.md) - Getting started guide
- [tracing.md](tracing.md) - OpenTelemetry configuration
- [api-reference.md](api-reference.md) - API configuration reference
- [troubleshooting.md](troubleshooting.md) - Troubleshooting configuration issues

---

*Need help?* Check the example [running-man.yml](https://github.com/elbeanio/the_running_man/blob/main/running-man.yml) file or file an issue on [GitHub](https://github.com/elbeanio/the_running_man/issues).