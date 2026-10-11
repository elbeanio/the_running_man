# Troubleshooting Guide

[Home](index.md) · [Overview](overview.md) · [Getting started](getting-started.md) · [Configuration](configuration.md) · [API reference](api-reference.md) · [Agent integration](agent-integration.md) · [Tracing](tracing.md) · [Architecture](architecture.md) · **Troubleshooting** · [Development](development.md)

---

Common issues and solutions for The Running Man.

## Installation Issues

### "Command not found: running-man"

**Problem:** The `running-man` command is not in your PATH.

**Solutions:**
```bash
# Install via Go (recommended)
go install github.com/elbeanio/the_running_man/cmd/running-man@latest

# Check if it's installed
which running-man

# Add Go bin to PATH (if needed)
export PATH="$HOME/go/bin:$PATH"

# Or use full path
~/go/bin/running-man --version
```

### "Go: command not found"

**Problem:** Go is not installed.

**Solutions:**
1. Install Go from [go.dev/dl](https://go.dev/dl/)
2. Verify installation:
```bash
go version
# Should show Go 1.27 or later
```

### Permission Denied

**Problem:** Cannot write to installation directory.

**Solutions:**
```bash
# Check permissions
ls -la ~/go/bin/

# Fix permissions (if needed)
chmod +x ~/go/bin/running-man

# Install with sudo (not recommended)
sudo go install github.com/elbeanio/the_running_man/cmd/running-man@latest
```

## Configuration Issues

### Configuration File Not Found

**Problem:** `running-man.yml` not found.

**Solutions:**
```bash
# Create config file
cat > running-man.yml <<EOF
processes:
  - name: app
    command: python app.py
EOF

# Or specify config path
running-man run --config /path/to/config.yml

# Or use CLI flags instead
running-man run --process "python app.py"
```

### YAML Syntax Error

**Problem:** Invalid YAML syntax.

**Solutions:**
```yaml
# Common errors:
# - Missing colons
# - Incorrect indentation
# - Unquoted special characters

# Use a YAML validator
python -c "import yaml; yaml.safe_load(open('running-man.yml'))"

# Check indentation (2 spaces per level)
processes:
  - name: app      # 2 spaces
    command: python app.py  # 4 spaces
```

### Environment Variables Not Expanding

**Problem:** `${VAR}` not replaced with environment variable.

**Solutions:**
```bash
# Set environment variable
export PORT=3000

# Verify it's set
echo $PORT

# Use in config
processes:
  - name: app
    command: python app.py --port ${PORT}
```

## Runtime Issues

### Port Already in Use

**Problem:** port 4318 (the OTLP receiver) is occupied. The API has no port — it is a Unix
socket — so this only ever concerns tracing.

**Normally nothing to do:** the receiver takes the next free port above 4318 by itself, and
reports which one on startup. 4318 is the OTLP/HTTP default so every other collector wants
it too, and one instance per project means several receivers on one machine.

It fails instead of moving only when the port was named explicitly, with `--tracing-port`
or `tracing.port` in config — a named port is one somebody meant.

**Solutions:**
```bash
# Check what's using it
lsof -i :4318

# See which port the receiver actually took
SOCK=.running-man/api.sock
curl -s --unix-socket "$SOCK" http://localhost/health | jq -r .otlp_endpoint

# Pin a different one
running-man run --tracing-port 4321

# Kill conflicting process (if safe)
kill $(lsof -t -i :9000)
```

### Docker Integration Failing

**Problem:** Docker logs not appearing.

**Solutions:**
```bash
# Check Docker is running
docker ps

# Check docker-compose file exists
ls -la docker-compose.yml

# Test Docker connection
docker info

# Run with verbose logging
running-man run --docker-compose docker-compose.yml
```

### "No containers are running" for a stack that is running

**Problem:** `docker compose ps` shows the stack up, but Running Man reports:

```
[running-man] No containers are running for Compose project "eureka".
```

**Cause:** the project name Running Man looked for is not the one the containers carry.
Discovery filters on the `com.docker.compose.project` label, so the names have to match
exactly.

Check what each side thinks the project is:

```bash
# What Running Man resolved (printed at startup)
running-man run | head -5

# What the containers are actually labelled
docker ps --format '{{.Names}}\t{{.Label "com.docker.compose.project"}}'
```

**Solutions:**

- A `name:` key in the compose file is honoured, and wins over the directory name. If the
  two disagree and Running Man is using the directory name, the binary predates that
  support — rebuild it.
- `COMPOSE_PROJECT_NAME` and `docker compose -p` are invisible to Running Man. If you use
  either, set `project_name` in `running-man.yml` to the same value.
- A stale stack started under a different project name will hold the ports the real one
  needs, which usually shows up as containers stuck in `created`. `docker compose -p
  <name> down` clears them.

See [Configuration](configuration.md#the-project-name) for the full precedence order.

### The TUI quit on its own

**Problem:** you came back to a shell prompt and no Running Man.

**Look in the crash log first.** Recovered panics, unrecovered panics and fatal
runtime errors are all recorded there:

```bash
cat .running-man/crash.log
```

A report names the function, the error and the stack. If the file is absent, the
TUI did not crash — the instance was stopped some other way, or the process was
killed.

The TUI no longer exits on a panic while drawing: it keeps the session, shows a
warning in the footer naming the crash log, and carries on. A panic while
handling a message discards that message rather than the session.

### The display is corrupted or garbled

**Problem:** borders are broken, rows are misaligned, or part of the frame has
gone blank.

**Causes, both fixed, so check your version first:**

- Captured output containing terminal instructions. Dev servers emit colour
  codes, carriage returns for spinners and occasionally a clear-screen sequence;
  drawn into a full-screen frame those are instructions, not text, and one
  `ESC[2J` blanked the display. Lines are now cleaned on capture — a spinner is
  stored as its final state, so `building... 1\rbuild done` is kept as
  `build done`.
- Running Man printing over its own frame. The OTLP receiver logged a line per
  request, so an instrumented application corrupted the display continuously.
  Nothing but the TUI writes to the terminal during a session now.

If it still happens:

```bash
# Capture what is actually being drawn, rather than describing it
tmux new-session -d -s rm -x 120 -y 40
tmux send-keys -t rm "running-man run" Enter
sleep 5
tmux capture-pane -t rm -p    # the frame as plain text

# Turn on the TUI's own trace and the high-frequency diagnostics
RUNNING_MAN_DEBUG=1 running-man run
cat .running-man/tui-debug.log
```

`RUNNING_MAN_DEBUG` also re-enables the per-request OTLP lines, which are off by
default.

**Problem:** the TUI freezes, or the terminal itself misbehaves.

```bash
# Rule the TUI out entirely
running-man run --process "python app.py" --no-tui

# Check the terminal handles escape codes at all
printf '\033[31mRed Text\033[0m\n'
```

Recommended terminals: iTerm2, Alacritty, WezTerm, or tmux in any of them.

### Processes Not Starting

**Problem:** Managed processes fail to start.

**Solutions:**
```bash
# Check command exists
which python
which npm

# Test command manually
python app.py

# Check permissions
ls -la app.py

# Use absolute paths
processes:
  - name: app
    command: /usr/bin/python /full/path/app.py
```

## OpenTelemetry Issues

### Port 4318 is already in use

**Symptom:** Running Man exits at startup with:

```
[running-man] Could not start the OTLP receiver: cannot listen on :4318:
              bind: address already in use
```

**Cause:** something else holds the OTLP/HTTP port. 4318 is the OTLP standard, so every
other collector defaults to it too — **Arize Phoenix**, the OpenTelemetry Collector,
Jaeger, Grafana Alloy, SigNoz. If you are running one of those, it has the port.

**Solutions:**

```bash
# Find out what has it
lsof -i :4318 -sTCP:LISTEN

# Move Running Man's receiver
running-man run --tracing-port 4319

# Or run without tracing
running-man run --tracing=false
```

Or in `running-man.yml`:

```yaml
tracing:
  port: 4319
```

Moving the receiver means instrumented processes export to the new port: Running Man
injects `OTEL_EXPORTER_OTLP_ENDPOINT` into the processes it starts, so anything it
launches follows automatically. Anything started outside Running Man needs the new
endpoint configured itself.

**Tracing is on by default**, so removing every `tracing:` key from `running-man.yml` does
not turn it off — absence means "use the default", and the default is enabled. To disable
it, say so:

```yaml
tracing:
  enabled: false
```

What happens on a port conflict depends on whether tracing was actually asked for:

| Tracing | On a port conflict |
|---|---|
| Requested — `enabled:` set, `tracing.port` set, or `--tracing`/`--tracing-port` passed | **Fails at startup.** Silently not doing what was asked is worse than stopping. |
| On by default only | **Warns and continues without tracing.** Refusing to start would block everything over a feature that was never requested. |

> Running Man used to report the receiver as ready in this situation and carry on with
> tracing silently dead — spans went to the other collector and `/traces` stayed empty
> with no explanation.

### Tracing Not Enabled

**Problem:** No tracing output or "Tracing: OTLP receiver" message.

**Solutions:**
```bash
# Enable tracing (enabled by default)
running-man run --tracing true

# Check tracing port
running-man run --tracing-port 4318

# Verify in output
# Should show: "Tracing: OTLP receiver on http://localhost:4318"
```

### Spans Not Appearing

**Problem:** Application sends traces but they don't appear.

**Solutions:**
```bash
# Check OTEL environment variables
running-man run --process "env | grep OTEL" --no-tui

# Should show:
# OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
# OTEL_SERVICE_NAME=process-name

# Test OTLP endpoint
curl http://localhost:4318/health

# Check Python packages
pip list | grep opentelemetry
```

### Trace Storage Full

**Problem:** "Trace storage full" or missing old traces.

**Solutions:**
```yaml
# Increase storage limits
tracing:
  max_spans: 50000
  max_span_age: 1h

# Or reduce retention
tracing:
  max_spans: 5000
  max_span_age: 10m
```

## AI Agent Issues

### An agent isn't using Running Man

**Problem:** the agent starts its own processes instead of using the ones already running.

**Solutions:**
```bash
SOCK=.running-man/api.sock

# Verify Running Man is running and reachable
curl --unix-socket "$SOCK" http://localhost/health

# Confirm it can see what's running
curl --unix-socket "$SOCK" http://localhost/processes

# Confirm the endpoint list is discoverable
curl --unix-socket "$SOCK" http://localhost/
```

If the API responds but the agent still ignores it, the problem is discovery rather than
connectivity — the agent has no cheap signal that Running Man exists. Check that the
skill is installed (see `skills/running-man/`) and that `AGENTS.md` points at it.

### An agent can't reach the API

**Problem:** connection refused on port 9000.

**Solutions:**
```bash
# Is the socket there at all?
ls -l .running-man/api.sock

# Does it answer, and for which project?
curl -s --unix-socket .running-man/api.sock http://localhost/health | jq '{pid, project}'
```

A socket file with nothing behind it means the instance died without cleaning up; the next
`running-man run` takes it over. A deeply nested project keeps its socket under the temp
directory instead, and `.running-man/instance.json` records the real path.

## API Issues

### Cannot restart or stop a process

**Problem:** `POST /processes/{name}/restart` or `POST /processes/stop-all` does not work.

**Cause:** these used to return 403 to any caller that was not on loopback, because the API
was on a TCP port open to the network. That guard is gone along with the port: the API is a
Unix socket at mode 0600, so reaching it at all means being this user on this machine.
`--allow-remote-control` and `--listen` no longer exist.

If the call fails now, it is an ordinary failure — the process name is wrong, or no process
manager is attached:

```bash
SOCK=.running-man/api.sock

# What is actually running, and under what names
curl -s --unix-socket "$SOCK" http://localhost/processes | jq '.processes[].name'

# Restart one of them
curl -s -X POST --unix-socket "$SOCK" http://localhost/processes/backend/restart
```

See [api-reference.md → Network exposure](api-reference.md#network-exposure).

### API Not Responding

**Problem:** `curl --unix-socket "$SOCK" http://localhost/health` fails.

**Solutions:**
```bash
# Check if Running Man is running
ps aux | grep running-man

# Check the socket
ls -l .running-man/api.sock
curl -s --unix-socket .running-man/api.sock http://localhost/health
```

### Invalid Query Parameters

**Problem:** API returns 400 Bad Request.

**Solutions:**
```bash
# Check parameter format
# since: duration string (5m, 1h, 30s)
# level: comma-separated (error,warn,info)

# Correct:
curl --unix-socket "$SOCK" "http://localhost/logs?since=5m&level=error,warn"

# Incorrect:
curl --unix-socket "$SOCK" "http://localhost/logs?since=5"  # Missing unit
curl --unix-socket "$SOCK" "http://localhost/logs?level=error warn"  # Space instead of comma
```

### No Logs in Response

**Problem:** API returns empty logs array.

**Solutions:**
```bash
# Check time window
# since=0s means "since startup"
curl --unix-socket "$SOCK" "http://localhost/logs?since=0s"

# Check if processes are producing output
# Some processes may buffer output

# Increase limit
curl --unix-socket "$SOCK" "http://localhost/logs?limit=1000"

# Check all sources
curl --unix-socket "$SOCK" "http://localhost/logs?source=*"
```

## Docker-Specific Issues

### Docker Daemon Not Running

**Problem:** "Cannot connect to Docker daemon"

**Solutions:**
```bash
# Start Docker
sudo systemctl start docker
# or
open -a Docker

# Check Docker status
docker info

# Add user to docker group (Linux)
sudo usermod -aG docker $USER
# Log out and back in
```

### Docker Compose File Not Found

**Problem:** "Failed to parse docker-compose.yml"

**Solutions:**
```bash
# Check file exists
ls -la docker-compose.yml

# Use absolute path
running-man run --docker-compose /full/path/docker-compose.yml

# Check file permissions
chmod 644 docker-compose.yml

# Validate compose file
docker-compose config
```

### Container Logs Not Streaming

**Problem:** Docker containers running but no logs.

**Solutions:**
```bash
# Check containers are running
docker ps

# Check container logs directly
docker logs <container-name>

# Some containers may not output to stdout
# Check Dockerfile for logging configuration

# Restart containers
docker-compose restart
```

## Python-Specific Issues

### OpenTelemetry Python Packages Missing

**Problem:** "ModuleNotFoundError: No module named 'opentelemetry'"

**Solutions:**
```bash
# Install required packages
pip install opentelemetry-api opentelemetry-sdk opentelemetry-exporter-otlp

# For auto-instrumentation
pip install opentelemetry-instrumentation

# For Flask
pip install opentelemetry-instrumentation-flask

# For Django
pip install opentelemetry-instrumentation-django
```

### Python Tracebacks Not Grouped

**Problem:** Multi-line Python tracebacks appear as separate log entries.

**Solutions:**
```python
# Ensure proper traceback formatting
import traceback

try:
    # code that might fail
    pass
except Exception as e:
    # This will be properly grouped
    traceback.print_exc()
    
    # This won't be grouped as well
    print(f"Error: {e}")
```

### Environment Variables Not Set

**Problem:** Python app not receiving OTEL environment variables.

**Solutions:**
```bash
# Check environment variables are injected
running-man run --process "python -c 'import os; print(os.environ.get(\"OTEL_EXPORTER_OTLP_ENDPOINT\"))'"

# Manually set in Python if needed
import os
os.environ.setdefault('OTEL_EXPORTER_OTLP_ENDPOINT', 'http://localhost:4318')
```

## Debugging Techniques

### Seeing what Running Man is doing

There is no debug or verbosity flag. Running Man reports what it resolved on startup and
captures its own output as the `running-man` source, so its decisions are queryable like
anything else:

```bash
curl -s --unix-socket "$SOCK" 'http://localhost/logs?source=running-man&since=5m'
```

Startup prints the API address and network posture, the Compose files, profiles and
services being watched, which services are not running, and the instance marker path.
### Check Log Files

```bash
# Running Man doesn't create log files by default
# All output goes to stdout/stderr

# Redirect output to file for debugging
running-man run --process "python app.py" 2>&1 | tee running-man.log

# Check system logs (Linux/macOS)
journalctl -u docker  # Docker logs
dmesg | tail -20      # Kernel messages
```

### Test Components Independently

```bash
# Test API without processes
running-man run --docker-compose ./docker-compose.yml --no-tui
curl --unix-socket "$SOCK" http://localhost/health

# Test process wrapper
cd internal/process
go test -v

# Test OTLP receiver
cd internal/tracing
go test -v
```

### Use Diagnostic Endpoints

```bash
# Health check
curl --unix-socket "$SOCK" http://localhost/health

# Process status
curl --unix-socket "$SOCK" http://localhost/processes

# Buffer statistics
curl --unix-socket "$SOCK" http://localhost/health | jq '.buffer'

# Trace statistics
curl --unix-socket "$SOCK" http://localhost/health | jq '.tracing'
```

## Performance Issues

### High Memory Usage

**Problem:** Running Man using too much memory.

**Solutions:**
```yaml
# Reduce buffer sizes
max_entries: 1000
max_bytes: 10485760  # 10MB

# Reduce trace storage
tracing:
  max_spans: 1000
  max_span_age: 10m

# Monitor memory usage
ps aux | grep running-man
```

### Slow Log Querying

**Problem:** API responses are slow.

**Solutions:**
```bash
# Reduce query scope
# Instead of: since=24h
# Use: since=5m

# Use specific filters
# Instead of: contains=error
# Use: level=error&contains=database

# Increase limit gradually
# Start with: limit=10
# Then: limit=50, limit=100
```

### Process Startup Delay

**Problem:** Processes take long to start.

**Solutions:**
```bash
# Check process initialization
# Some processes may have slow startup (Java, etc.)

# Use shell commands for setup
processes:
  - name: app
    command: |
      # Pre-start setup
      source venv/bin/activate
      python app.py
```

## Getting Help

### Collect Diagnostic Information

When reporting issues, include:

```bash
# Version information
running-man --version

# Go version
go version

# System information
uname -a

# Configuration
cat running-man.yml

# Error output
running-man run --process "test" 2>&1

# API test
curl -v --unix-socket .running-man/api.sock http://localhost/health
```

### GitHub Issues

Create issues at: https://github.com/elbeanio/the_running_man/issues

Include:
1. **Description** of the problem
2. **Steps to reproduce**
3. **Expected behavior**
4. **Actual behavior**
5. **Diagnostic information** (above)
6. **Configuration files** (if relevant)

### Common Solutions

Most issues can be resolved by:

1. **Updating to latest version**:
```bash
go install github.com/elbeanio/the_running_man/cmd/running-man@latest
```

2. **Checking configuration syntax**:
```bash
python -c "import yaml; yaml.safe_load(open('running-man.yml'))"
```

3. **Testing components independently**:
```bash
# Test Docker
docker ps

# Test Python
python -c "import opentelemetry; print('OK')"

# Test API
curl --unix-socket "$SOCK" http://localhost/health
```

4. **Checking logs**:
```bash
running-man run --process "echo test" 2>&1 | tail -50
```
