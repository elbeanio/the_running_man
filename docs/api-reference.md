# API Reference

## Current Status

The Running Man exposes a REST API on `http://localhost:9000` (configurable via `--api-port` or `api_port` in config).

## OpenAPI Specification

**Coming in Phase 2.5:** Auto-generated OpenAPI 3.0 specification with interactive docs.

For now, see the quick reference below.

---

## Endpoints (Phase 2)

### GET /logs

Query log entries with filters.

**Query Parameters:**
- `since` - Time window (e.g., `30s`, `5m`, `1h`, `2024-01-15T10:00:00Z`)
- `source` - Filter by source name (e.g., `backend`, `postgres`, `docker-*`)
- `level` - Filter by level (comma-separated: `error,warn`)
- `contains` - Text search in message content
- `limit` - Max entries to return (default: 100)

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
      "Timestamp": "2024-01-15T10:30:00Z",
      "Level": "error",
      "Source": "backend",
      "Message": "Database connection failed",
      "Raw": "2024-01-15 10:30:00 ERROR Database connection failed",
      "IsError": true,
      "Stacktrace": ""
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

**Example:**
```bash
curl "http://localhost:9000/errors?since=1h"
```

**Response:** Same format as `/logs`

---

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
      "start_time": "2024-01-15T08:00:00Z"
    }
  ]
}
```

---

## MCP API (Phase 3 - Complete)

### GET /mcp

Model Context Protocol server for AI agent integration.

**Protocol:** MCP over HTTP/SSE
**Tools:** 8 debugging tools for AI agents
**Authentication:** None (local development tool)

**Available Tools via MCP:**
1. `search_logs` - Search logs with filters
2. `get_recent_errors` - Get errors with context
3. `get_process_status` - Process status monitoring
4. `get_startup_logs` - Startup log viewing
5. `get_health_status` - System health checks
6. `get_process_detail` - Detailed process info
7. `restart_process` - Process restart (with safety)
8. `stop_all_processes` - Stop all processes (requires confirm)

**Integration:**
- OpenCode: Direct remote MCP connection (no external commands needed)
- Claude Desktop: Requires HTTP proxy server
- Permissions: Add `running-man_*` to OpenCode permissions

See [agent-integration.md](agent-integration.md) for complete setup and usage guide.

---

## Coming in Phase 4: Traces API

### GET /traces

Query distributed traces (OTEL spans).

**Query Parameters:**
- `trace_id` - Specific trace ID
- `workflow_id` - All spans for a workflow
- `since` - Time window
- `status` - Filter by span status (`ok`, `error`)
- `min_duration` - Minimum span duration

Details TBD during Phase 4 implementation.

---

## Coming in Phase 5: Browser API

### POST /ingest/browser

Ingest browser log entries from the SDK.

### GET /browser

Query browser logs.

Details TBD during Phase 5 implementation.

---

## Error Responses

All endpoints return standard HTTP error codes:

**400 Bad Request:**
```json
{
  "error": "Invalid time format for 'since' parameter"
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

CORS is enabled for `localhost` and `127.0.0.1` (browser SDK support in Phase 5).

---

## Authentication

None (local development tool, not exposed to network).

---

**Full OpenAPI specification coming in Phase 2.5!**
