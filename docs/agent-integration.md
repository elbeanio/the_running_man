# Agent Integration Guide

Running Man provides a Model Context Protocol (MCP) server that allows AI coding assistants (Claude Code, OpenCode) to directly query logs, errors, and process information without manual copy-pasting.

## MCP Server Overview

The MCP server is automatically available when Running Man is running:

- **Endpoint:** `http://localhost:9000/mcp`
- **Protocol:** Model Context Protocol (MCP) over HTTP/SSE
- **Tools:** 8 debugging tools for AI agents
- **Authentication:** None required (local development tool)

## Available MCP Tools

### Log Tools

#### 1. `search_logs`
Search log entries with flexible filters.

**Parameters:**
- `source` (optional): Filter by process name (supports glob patterns like `app-*`)
- `since` (optional): Time window (e.g., `5m`, `1h`, `30s`)
- `level` (optional): Log level filter (`error`, `warn`, `info`, `debug`)
- `contains` (optional): Text search in log messages
- `limit` (optional): Maximum entries to return (default: 50)

**Example prompts:**
- "Search logs for 'database' errors in the last 5 minutes"
- "Show me info logs from the backend process"
- "Find logs containing 'connection failed'"

### 2. `get_recent_errors`
Get recent error log entries with surrounding context.

**Parameters:**
- `source` (optional): Filter by process name
- `since` (optional): Time window (default: `30m`)
- `context` (optional): Lines before/after each error (default: 10)
- `limit` (optional): Maximum errors to return (default: 20)

**Example prompts:**
- "Show me recent errors with stacktraces"
- "Get errors from the database process with context"
- "Check for errors in the last hour"

### 3. `get_process_status`
Check status of processes managed by Running Man.

**Parameters:**
- `name` (optional): Specific process name (if omitted, lists all processes)

**Example prompts:**
- "What processes is running-man managing?"
- "Check status of the backend process"
- "Show all managed processes"

### 4. `get_startup_logs`
View logs from when a process started.

**Parameters:**
- `source` (required): Process name
- `limit` (optional): Maximum log entries (default: 50)

**Example prompts:**
- "Show me startup logs for the backend"
- "Get initial logs when the database started"
- "Why won't the server start?"

### 5. `get_health_status`
Get system health information and buffer statistics.

**Parameters:** None

**Example prompts:**
- "Check running-man system health"
- "Show buffer statistics"
- "What's the system status?"

### 6. `get_process_detail`
Get detailed information about a specific process.

**Parameters:**
- `name` (required): Process name

**Example prompts:**
- "Get detailed info about the backend process"
- "Show process details for llamaindex-pg"
- "What's the uptime of the frontend process?"

### 7. `restart_process`
Restart a managed process.

**Parameters:**
- `name` (required): Process name to restart

**Safety:** Includes error handling for non-existent processes

**Example prompts:**
- "Restart the backend process" (safe - will actually restart)
- "Restart non-existent process" (safe - tests error handling)

### 8. `stop_all_processes`
Stop all managed processes.

**Parameters:**
- `confirm` (required): Must be `true` to proceed

**Safety:** Requires explicit confirmation flag

**Example prompts:**
- "Stop all processes" (requires confirm=true)
- "Test stop_all_processes error handling" (safe without confirm flag)

### Trace Tools (When OTEL Enabled)

#### 9. `get_traces`
List recent traces captured by Running Man's OpenTelemetry tracing system.

**Parameters:**
- `since` (optional): Search traces from this time window (e.g., '5m', '1h', '30s')
- `service_name` (optional): Filter by service name from traces
- `trace_id` (optional): Get specific trace by ID
- `span_name` (optional): Filter by span name (supports partial match)
- `status` (optional): Filter by span status (`ok`, `error`, `unset`)
- `limit` (optional): Maximum traces to return (default: 50, max: 1000)

**Example prompts:**
- "List recent traces: {}"
- "Find traces with errors: {\"status\": \"error\", \"since\": \"10m\"}"
- "Search for database traces: {\"service_name\": \"database\", \"limit\": 20}"
- "Find traces containing API calls: {\"span_name\": \"api\", \"since\": \"5m\"}"

#### 10. `get_trace`
Get detailed information about a specific trace including all spans.

**Parameters:**
- `trace_id` (required): The trace ID to retrieve (e.g., "abc123def456")

**Example prompts:**
- "Get trace details: {\"trace_id\": \"abc123def456\"}"
- "Show me all spans for trace XYZ"

#### 11. `get_slow_traces`
Find traces exceeding duration thresholds.

**Parameters:**
- `since` (optional): Search traces from this time window
- `threshold` (optional): Duration threshold for slow traces (e.g., '1s', '100ms')
- `limit` (optional): Maximum traces to return (default: 20, max: 100)

**Example prompts:**
- "Find slow traces: {\"threshold\": \"1s\", \"since\": \"5m\"}"
- "Show me traces longer than 500ms: {\"threshold\": \"500ms\"}"
- "Find the slowest API calls from the last hour"

## Setup Instructions

### OpenCode Configuration

Add to `~/.config/opencode/opencode.json`:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "running-man": {
      "enabled": true,
      "type": "remote",
      "url": "http://localhost:9000/mcp"
    }
  }
}
```

### OpenCode Permissions

Add to `~/.config/opencode/opencode.json` permissions section:

```json
"permission": {
  "running-man_*": "allow"
}
```

Or allow individual tools:

```json
"permission": {
  "running-man_search_logs": "allow",
  "running-man_get_recent_errors": "allow",
  "running-man_get_startup_logs": "allow",
  "running-man_get_process_status": "allow",
  "running-man_get_process_detail": "allow",
  "running-man_restart_process": "allow",
  "running-man_stop_all_processes": "allow",
  "running-man_get_health_status": "allow",
  "running-man_get_traces": "allow",
  "running-man_get_trace": "allow",
  "running-man_get_slow_traces": "allow"
}
```

### Claude Desktop Configuration

Add to `~/Library/Application Support/Claude/claude_desktop_config.json`:

```json
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

**Note:** Claude Desktop requires the HTTP proxy server, while OpenCode supports direct remote MCP connections.

## Usage Examples

### Basic Debugging Workflow

1. **Start Running Man:**
   ```bash
   running-man run --process "python server.py" --process "npm run dev"
   ```

2. **Agent can now:**
   - "Show me recent errors from the backend"
   - "Check if the frontend process is running"
   - "Search logs for 'database connection' issues"
   - "Get startup logs to see why a process failed"

### Common Debugging Scenarios

**Scenario 1: Server won't start**
```
Agent: "Why won't the backend server start?"
→ Uses get_startup_logs tool with source="backend"
→ Returns error logs from startup with context
```

**Scenario 2: Intermittent errors**
```
Agent: "Show me recent database errors"
→ Uses get_recent_errors tool with source="database"
→ Returns errors with surrounding log context
```

**Scenario 3: Process monitoring**
```
Agent: "Check status of all processes"
→ Uses get_process_status tool
→ Returns process list with status, PID, uptime
```

**Scenario 4: Performance debugging**
```
Agent: "Find slow API requests in the last 5 minutes"
→ Uses get_slow_traces tool with threshold="1s", since="5m"
→ Returns traces exceeding 1 second duration
```

**Scenario 5: Distributed tracing**
```
Agent: "Show me traces with errors from the payment service"
→ Uses get_traces tool with service_name="payment", status="error"
→ Returns error traces with span details
```

**Scenario 6: End-to-end request tracing**
```
Agent: "Get details for trace abc123def456"
→ Uses get_trace tool with trace_id="abc123def456"
→ Returns complete span tree for the request
```

## Safety Features

1. **Read-only by default:** Most tools are read-only queries
2. **Confirmation required:** Destructive tools require explicit flags
3. **Error handling:** Tools handle missing processes gracefully
4. **Local only:** Server runs on localhost only

## Testing

All 11 MCP tools have been tested and verified:

**Log Tools:**
- ✅ `search_logs` - Works with all filter combinations
- ✅ `get_recent_errors` - Returns errors with context
- ✅ `get_startup_logs` - Returns startup logs

**Process Tools:**
- ✅ `get_process_status` - Shows process information
- ✅ `get_process_detail` - Returns detailed process info
- ✅ `restart_process` - Error handling tested (safe)
- ✅ `stop_all_processes` - Error handling tested (safe)

**System Tools:**
- ✅ `get_health_status` - Shows system health

**Trace Tools (when OTEL enabled):**
- ✅ `get_traces` - Lists traces with filtering
- ✅ `get_trace` - Returns detailed trace information
- ✅ `get_slow_traces` - Finds traces exceeding thresholds

## Troubleshooting

### MCP Tools Not Appearing
1. Ensure Running Man is running (`running-man run`)
2. Check server is on `localhost:9000`
3. Restart OpenCode/Claude Desktop to refresh MCP discovery
4. Verify configuration files are in correct locations

### Permission Denied
1. Check OpenCode permissions include `running-man_*` or individual tools
2. Ensure `uv` and `mcp` CLI are installed
3. Verify network access to `localhost:9000`

### Server Not Starting
1. Check port 9000 is not in use
2. Verify binary is built with latest code
3. Check logs for initialization errors

## REST API (Alternative)

If MCP is not available, agents can use the REST API:

```bash
# Recent errors
curl "http://localhost:9000/errors?since=5m"

# Process status
curl "http://localhost:9000/processes"

# Health check
curl "http://localhost:9000/health"
```

See [api-reference.md](api-reference.md) for complete API documentation.

## Contributing

Want to add new MCP tools or improve existing ones?

1. Check `internal/api/mcp.go` for implementation
2. Follow existing patterns for tool registration
3. Add comprehensive tests
4. Update this documentation

---

**Current Status:** Running Man provides complete AI agent integration via MCP protocol with 11 debugging tools, including OpenTelemetry trace exploration capabilities.