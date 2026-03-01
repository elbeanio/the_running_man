# Agent Integration Guide

Running Man provides a Model Context Protocol (MCP) server that allows AI coding assistants (Claude Code, OpenCode) to directly query logs, errors, and process information without manual copy-pasting.

## MCP Server Overview

The MCP server is automatically available when Running Man is running:

- **Endpoint:** `http://localhost:9000/mcp`
- **Protocol:** Model Context Protocol (MCP) over HTTP/SSE
- **Tools:** 8 debugging tools for AI agents
- **Authentication:** None required (local development tool)

## Available MCP Tools

### 1. `search_logs`
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
  "running-man_get_process_status": "allow",
  "running-man_get_startup_logs": "allow",
  "running-man_get_health_status": "allow",
  "running-man_get_process_detail": "allow",
  "running-man_restart_process": "allow",
  "running-man_stop_all_processes": "allow"
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

## Safety Features

1. **Read-only by default:** Most tools are read-only queries
2. **Confirmation required:** Destructive tools require explicit flags
3. **Error handling:** Tools handle missing processes gracefully
4. **Local only:** Server runs on localhost only

## Testing

All 8 MCP tools have been tested and verified:

- ✅ `search_logs` - Works with all filter combinations
- ✅ `get_recent_errors` - Returns errors with context
- ✅ `get_process_status` - Shows process information
- ✅ `get_startup_logs` - Returns startup logs
- ✅ `get_health_status` - Shows system health
- ✅ `get_process_detail` - Returns detailed process info
- ✅ `restart_process` - Error handling tested (safe)
- ✅ `stop_all_processes` - Error handling tested (safe)

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

**Phase 3 Complete:** Running Man now provides full AI agent integration via MCP protocol, enabling seamless debugging workflows with Claude Code and OpenCode.