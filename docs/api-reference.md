# API Reference

## Overview

The Running Man exposes a REST API on `http://localhost:9000` (configurable via `--api-port` or `api_port` in config). All endpoints return JSON responses.

## Base URL

```
http://localhost:9000
```

Or if you've configured a different port:
```
http://localhost:<your-port>
```

---

## Log Endpoints

### GET /logs

Query log entries with filters.

**Query Parameters:**
- `since` - Time window (e.g., `30s`, `5m`, `1h`, `2024-01-15T10:00:00Z`)
- `source` - Filter by source name (e.g., `backend`, `postgres`, `docker-*`)
- `level` - Filter by level (comma-separated: `error,warn`)
- `contains` - Text search in message content
- `limit` - Max entries to return (default: 100)
- `offset` - Pagination offset (default: 0)

**Example:**
```bash
curl "http://localhost:9000/logs?since=30s&level=error&source=backend"
```

**Response:**
```json
{
  "count": 5,
  "logs": [
    {
      "timestamp": "2024-01-15T10:30:00Z",
      "level": "error",
      "source": "backend",
      "message": "Database connection failed",
      "raw": "2024-01-15 10:30:00 ERROR Database connection failed",
      "is_error": true,
      "stacktrace": "",
      "trace_id": "abc123def456"  # If correlated with trace
    }
  ]
}
```

---

### GET /errors

Recent error entries (convenience endpoint, equivalent to `/logs?level=error`).

**Query Parameters:**
- `since` - Time window (default: `5m`)
- `source` - Filter by source
- `limit` - Max entries (default: 50)
- `context` - Lines before/after each error (default: 10)

**Example:**
```bash
curl "http://localhost:9000/errors?since=1h&context=5"
```

**Response:** Same format as `/logs`

---

## System Endpoints

### GET /health

System status and buffer statistics.

**Example:**
```bash
curl "http://localhost:9000/health"
```

**Response:**
```json
{
  "status": "ok",
  "uptime": "2h30m",
  "buffer": {
    "entries": 1247,
    "size_bytes": 524288,
    "oldest": "2024-01-15T08:00:00Z"
  },
  "tracing": {
    "enabled": true,
    "spans": 245,
    "traces": 42
  },
  "sources": [
    {
      "name": "backend",
      "type": "process",
      "status": "running",
      "pid": 12345
    },
    {
      "name": "postgres",
      "type": "docker",
      "status": "running",
      "container_id": "abc123"
    }
  ]
}
```

---

### GET /processes

Status of managed processes.

**Example:**
```bash
curl "http://localhost:9000/processes"
```

**Response:**
```json
{
  "processes": [
    {
      "name": "backend",
      "command": "python server.py",
      "pid": 12345,
      "status": "running",
      "exit_code": -1,
      "start_time": "2024-01-15T08:00:00Z",
      "uptime": "2h30m"
    }
  ]
}
```

---

## Trace Endpoints (OpenTelemetry)

### GET /traces

Query distributed traces (OTEL spans).

**Query Parameters:**
- `since` - Time window (e.g., `5m`, `1h`, `30s`)
- `service_name` - Filter by service name
- `trace_id` - Get specific trace by ID
- `span_name` - Filter by span name (supports partial match)
- `status` - Filter by span status (`ok`, `error`, `unset`)
- `limit` - Maximum traces to return (default: 50, max: 1000)

**Example:**
```bash
curl "http://localhost:9000/traces?since=10m&status=error"
curl "http://localhost:9000/traces?service_name=database&limit=20"
curl "http://localhost:9000/traces?span_name=http.request&since=5m"
```

**Response:**
```json
{
  "count": 3,
  "traces": [
    {
      "trace_id": "abc123def456",
      "span_count": 5,
      "start_time": "2024-01-15T10:30:00Z",
      "end_time": "2024-01-15T10:30:01.5Z",
      "duration_ms": 1500,
      "has_error": true,
      "services": ["backend", "database"],
      "root_span": "process_order"
    }
  ]
}
```

---

### GET /traces/{trace_id}

Get detailed information about a specific trace including all spans.

**Path Parameter:**
- `trace_id` - The trace ID to retrieve

**Example:**
```bash
curl "http://localhost:9000/traces/abc123def456"
```

**Response:**
```json
{
  "trace_id": "abc123def456",
  "span_count": 5,
  "start_time": "2024-01-15T10:30:00Z",
  "end_time": "2024-01-15T10:30:01.5Z",
  "duration_ms": 1500,
  "has_error": true,
  "services": ["backend", "database"],
  "spans": [
    {
      "span_id": "span1",
      "parent_span_id": "",
      "name": "process_order",
      "start_time": "2024-01-15T10:30:00Z",
      "end_time": "2024-01-15T10:30:01.5Z",
      "duration_ms": 1500,
      "status": "error",
      "service_name": "backend",
      "attributes": {
        "order.id": "ORD-1001",
        "processing.stage": "started"
      }
    },
    {
      "span_id": "span2",
      "parent_span_id": "span1",
      "name": "validate_order",
      "start_time": "2024-01-15T10:30:00.1Z",
      "end_time": "2024-01-15T10:30:00.15Z",
      "duration_ms": 50,
      "status": "ok",
      "service_name": "backend",
      "attributes": {}
    }
  ]
}
```

---

### GET /traces/slow

Find traces exceeding duration thresholds.

**Query Parameters:**
- `since` - Time window
- `threshold` - Duration threshold (e.g., `1s`, `100ms`, `500ms`)
- `limit` - Maximum traces to return (default: 20, max: 100)

**Example:**
```bash
curl "http://localhost:9000/traces/slow?since=5m&threshold=1s"
curl "http://localhost:9000/traces/slow?threshold=500ms&limit=10"
```

**Response:** Same format as `/traces`

---

## MCP API

### GET /mcp

Model Context Protocol server for AI agent integration.

**Protocol:** MCP over HTTP/SSE
**Tools:** 11 debugging tools for AI agents
**Authentication:** None (local development tool)

**Available Tools via MCP:**

**Log Tools:**
1. `search_logs` - Search logs with filters
2. `get_recent_errors` - Get errors with context
3. `get_startup_logs` - View logs from process startup

**Process Management Tools:**
4. `get_process_status` - Check status of managed processes
5. `get_process_detail` - Detailed process information
6. `restart_process` - Restart a managed process (with safety checks)
7. `stop_all_processes` - Stop all processes (requires confirmation)

**System Tools:**
8. `get_health_status` - System health and buffer statistics

**Trace Tools:**
9. `get_traces` - List recent traces with filtering capabilities
10. `get_trace` - Get detailed trace information including all spans
11. `get_slow_traces` - Find traces exceeding duration thresholds

**Integration:**
- OpenCode: Direct remote MCP connection to `http://localhost:9000/mcp`
- Claude Desktop: Requires HTTP proxy server (`@modelcontextprotocol/server-http`)
- Permissions: Add `running-man_*` to OpenCode permissions

See [agent-integration.md](agent-integration.md) for complete setup and usage guide.

---

## Error Responses

All endpoints return standard HTTP error codes:

**400 Bad Request:**
```json
{
  "error": "Invalid time format for 'since' parameter"
}
```

**404 Not Found:**
```json
{
  "error": "Trace not found: abc123def456"
}
```

**500 Internal Server Error:**
```json
{
  "error": "Failed to query buffer: <details>"
}
```

---

## Rate Limiting

Currently no rate limiting (local development tool).

---

## CORS

CORS is enabled for `localhost` and `127.0.0.1`.

---

## Authentication

None required (local development tool, not exposed to network).

---

## Examples

### Complete Debugging Workflow

```bash
# 1. Check system health
curl "http://localhost:9000/health"

# 2. Look for recent errors
curl "http://localhost:9000/errors?since=5m"

# 3. If trace_id found in errors, investigate trace
curl "http://localhost:9000/traces/abc123def456"

# 4. Check process status
curl "http://localhost:9000/processes"

# 5. Search for related logs
curl "http://localhost:9000/logs?since=10m&contains=database&source=backend"
```

### Using with Scripts

```bash
#!/bin/bash

# Monitor for errors
while true; do
  ERROR_COUNT=$(curl -s "http://localhost:9000/errors?since=1m" | jq '.count')
  
  if [ "$ERROR_COUNT" -gt 0 ]; then
    echo "Found $ERROR_COUNT error(s) in the last minute"
    curl -s "http://localhost:9000/errors?since=1m" | jq '.logs[] | .message'
  fi
  
  sleep 60
done
```

### Python Integration

```python
import requests
import json

class RunningManClient:
    def __init__(self, base_url="http://localhost:9000"):
        self.base_url = base_url
    
    def get_errors(self, since="5m"):
        response = requests.get(f"{self.base_url}/errors", params={"since": since})
        response.raise_for_status()
        return response.json()
    
    def get_trace(self, trace_id):
        response = requests.get(f"{self.base_url}/traces/{trace_id}")
        response.raise_for_status()
        return response.json()
    
    def search_logs(self, **filters):
        response = requests.get(f"{self.base_url}/logs", params=filters)
        response.raise_for_status()
        return response.json()

# Usage
client = RunningManClient()
errors = client.get_errors(since="10m")
if errors["count"] > 0:
    for error in errors["logs"]:
        print(f"Error: {error['message']}")
        if error.get("trace_id"):
            trace = client.get_trace(error["trace_id"])
            print(f"Trace duration: {trace['duration_ms']}ms")
```

---

**Note:** All API endpoints are available only when Running Man is running. The server starts automatically when you run `running-man run`.