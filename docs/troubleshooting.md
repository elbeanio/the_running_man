# Troubleshooting Guide

Common issues and solutions for The Running Man.

## 🚨 Installation Issues

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
# Should show Go 1.21 or later
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

## 🔧 Configuration Issues

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

## 🖥️ Runtime Issues

### Port Already in Use

**Problem:** Port 9000 (API) or 4318 (tracing) is occupied.

**Solutions:**
```bash
# Check what's using the port
lsof -i :9000
lsof -i :4318

# Change API port
running-man run --api-port 9001

# Change tracing port
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
RUNNING_MAN_DEBUG=1 running-man run --docker-compose docker-compose.yml
```

### TUI Rendering Issues

**Problem:** TUI displays incorrectly or freezes.

**Solutions:**
```bash
# Try different terminal
# Recommended: iTerm2, Alacritty, WezTerm

# Check terminal supports ANSI escape codes
echo -e "\033[31mRed Text\033[0m"

# Disable TUI for debugging
running-man run --process "python app.py" --no-tui

# Increase terminal scrollback buffer
# (Check terminal settings)
```

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

## 📊 OpenTelemetry Issues

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

## 🤖 MCP/AI Agent Issues

### MCP Tools Not Appearing

**Problem:** AI agent doesn't see Running Man tools.

**Solutions:**
```bash
# Verify Running Man is running
curl http://localhost:9000/health

# Check MCP endpoint
curl http://localhost:9000/mcp

# Restart AI agent (OpenCode/Claude Desktop)
# MCP discovery happens on startup

# Check OpenCode config
cat ~/.config/opencode/opencode.json

# Check Claude Desktop config
cat ~/Library/Application\ Support/Claude/claude_desktop_config.json
```

### Permission Denied in OpenCode

**Problem:** OpenCode shows permission errors.

**Solutions:**
```json
// OpenCode config
{
  "permission": {
    "running-man_*": "allow"
  }
}

// Or allow specific tools
{
  "permission": {
    "running-man_search_logs": "allow",
    "running-man_get_recent_errors": "allow"
  }
}
```

### Claude Desktop Proxy Issues

**Problem:** Claude Desktop can't connect to MCP server.

**Solutions:**
```bash
# Install HTTP proxy server
npm install -g @modelcontextprotocol/server-http

# Test proxy manually
npx @modelcontextprotocol/server-http http://localhost:9000/mcp

# Check Claude Desktop config
{
  "mcpServers": {
    "running-man": {
      "command": "npx",
      "args": [
        "-y",
        "@modelcontextprotocol/server-http",
        "http://localhost:9000/mcp"
      ]
    }
  }
}
```

## 🔍 API Issues

### API Not Responding

**Problem:** `curl http://localhost:9000/health` fails.

**Solutions:**
```bash
# Check if Running Man is running
ps aux | grep running-man

# Check port
netstat -an | grep 9000

# Try different port
running-man run --api-port 9001
curl http://localhost:9001/health

# Check firewall
sudo ufw status
```

### Invalid Query Parameters

**Problem:** API returns 400 Bad Request.

**Solutions:**
```bash
# Check parameter format
# since: duration string (5m, 1h, 30s)
# level: comma-separated (error,warn,info)

# Correct:
curl "http://localhost:9000/logs?since=5m&level=error,warn"

# Incorrect:
curl "http://localhost:9000/logs?since=5"  # Missing unit
curl "http://localhost:9000/logs?level=error warn"  # Space instead of comma
```

### No Logs in Response

**Problem:** API returns empty logs array.

**Solutions:**
```bash
# Check time window
# since=0s means "since startup"
curl "http://localhost:9000/logs?since=0s"

# Check if processes are producing output
# Some processes may buffer output

# Increase limit
curl "http://localhost:9000/logs?limit=1000"

# Check all sources
curl "http://localhost:9000/logs?source=*"
```

## 🐳 Docker-Specific Issues

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

## 🐍 Python-Specific Issues

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

## 🐛 Debugging Techniques

### Enable Debug Logging

```bash
# Set debug environment variable
RUNNING_MAN_DEBUG=1 running-man run --process "python app.py"

# Debug output includes:
# - Configuration loading
# - Process startup
# - API initialization
# - MCP registration
# - Trace ingestion
```

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
running-man run --api-port 9000 --no-tui
curl http://localhost:9000/health

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
curl http://localhost:9000/health

# Process status
curl http://localhost:9000/processes

# Buffer statistics
curl http://localhost:9000/health | jq '.buffer'

# Trace statistics
curl http://localhost:9000/health | jq '.tracing'
```

## 🔄 Performance Issues

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

## 🆘 Getting Help

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
RUNNING_MAN_DEBUG=1 running-man run --process "test" 2>&1

# API test
curl -v http://localhost:9000/health
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
curl http://localhost:9000/health
```

4. **Checking logs**:
```bash
RUNNING_MAN_DEBUG=1 running-man run --process "echo test" 2>&1 | tail -50
```

## 📚 Additional Resources

- [Getting Started Guide](getting-started.md)
- [Configuration Guide](configuration.md)
- [OpenTelemetry Guide](tracing.md)
- [API Reference](api-reference.md)
- [GitHub Repository](https://github.com/elbeanio/the_running_man)

---

*Still having issues?* Create a detailed issue on [GitHub](https://github.com/elbeanio/the_running_man/issues) with the diagnostic information above.