# API Reference

[Home](index.md) · [Overview](overview.md) · [Getting started](getting-started.md) · [Configuration](configuration.md) · **API reference** · [Agent integration](agent-integration.md) · [Tracing](tracing.md) · [Architecture](architecture.md) · [Troubleshooting](troubleshooting.md) · [Development](development.md)

---

## Overview

The Running Man exposes a REST API on a **Unix socket**, not a TCP port. All endpoints
return JSON responses.

## Base URL

The socket lives in the project directory:

```
.running-man/api.sock
```

`curl` reaches it with `--unix-socket`. The host in the URL is ignored, so `localhost` is
just a convention:

```bash
SOCK=.running-man/api.sock
curl -s --unix-socket "$SOCK" http://localhost/health
```

Every example below assumes `$SOCK` is set that way.

A Unix socket path has a hard length limit (`sun_path`: 104 bytes on macOS, 108 on Linux),
so a deeply nested project gets a short socket under the temp directory instead. The
`socket` field of `.running-man/instance.json` is always authoritative:

```bash
SOCK=$(sed -n 's/.*"socket": "\(.*\)",*/\1/p' .running-man/instance.json)
```

**Why a socket and not a port.** One instance per project, and a port gives an agent no way
to find *its* project's instance: `:9000` may answer from a different project entirely, and
the only fallback is scanning. A path derived from the project directory removes the
question — there is nothing to allocate, nothing to collide over, and an agent in one
project cannot read another's logs. It also replaces the access-control problem: the socket
is mode 0600, so reaching it means being this user on this machine.

---

## Log Endpoints

### GET /logs

Query log entries with filters.

**Query Parameters:**
- `since` - Time window (e.g., `30s`, `5m`, `1h`, `2024-01-15T10:00:00Z`)
- `source` - Filter by source name (e.g., `backend`, `postgres`, `docker-*`)
- `level` - Filter by level (comma-separated: `error,warn`)
- `contains` - Text search in message content
- `exclude` - Exclude sources by name or glob (comma-separated)
- `limit` - Maximum entries to return, **most recent first excluded last** — that is, the
  newest matches are kept (default: `1000`, `limit=0` for no limit)

The limit is applied *after* filtering, so `?level=error&limit=50` means "the 50 most
recent errors", not "errors among the 50 most recent entries".

There is no `offset`/pagination. Earlier versions of this document described one; it was
never implemented.

**Example:**
```bash
curl --unix-socket "$SOCK" "http://localhost/logs?since=30s&level=error&source=backend"

# The 20 most recent entries
curl --unix-socket "$SOCK" "http://localhost/logs?limit=20"

# Everything in the buffer, deliberately
curl --unix-socket "$SOCK" "http://localhost/logs?limit=0"
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
curl --unix-socket "$SOCK" "http://localhost/errors?since=1h&context=5"
```

**Response:** Same format as `/logs`

---

## System Endpoints

### GET /health

System status and buffer statistics.

**Example:**
```bash
curl --unix-socket "$SOCK" "http://localhost/health"
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
curl --unix-socket "$SOCK" "http://localhost/processes"
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
      "ports": [8000],
      "depends_on": ["postgres"],
      "healthcheck": "port 8000"
    },
    {
      "name": "frontend",
      "command": "npm run dev",
      "pid": -1,
      "status": "pending",
      "exit_code": -1,
      "start_time": "0001-01-01T00:00:00Z",
      "depends_on": ["backend"]
    }
  ],
  "count": 2,
  "dependencies": [
    {"name": "postgres", "state": "ready", "sources": ["myproject-postgres-1"]}
  ]
}
```

`status` is `running`, `stopped`, `failed`, `waiting` (a recurring process between runs),
or one of three from `depends_on`: `pending` (not started; a dependency is not ready),
`starting` (running, its own healthcheck not yet passed) and `blocked` (will not start:
a dependency never became ready; `startup_error` says why). `dependencies` lists the
Compose services processes depend on -- `pending`, `checking`, `ready` or `failed`, with
`detail` when failed. `ports` are observed, not configured.

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
curl --unix-socket "$SOCK" "http://localhost/traces?since=10m&status=error"
curl --unix-socket "$SOCK" "http://localhost/traces?service_name=database&limit=20"
curl --unix-socket "$SOCK" "http://localhost/traces?span_name=http.request&since=5m"
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
curl --unix-socket "$SOCK" "http://localhost/traces/abc123def456"
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

## Network exposure

**The query API is not on the network.** It is served on a Unix socket at
`.running-man/api.sock` (mode 0600), so there is no port, no bind address and no caller-IP
check. Reaching it requires permission to open a file in the project directory, which is
the same thing as being this user on this machine. There is no authentication because the
filesystem already is the authentication.

This replaced a TCP listener on all interfaces with no authentication at all. It also
removed the loopback guard on `POST /processes/{name}/restart` and `POST /processes/stop-all`
— those used to return 403 to non-loopback callers, and the flags `--listen` and
`--allow-remote-control` that configured it are gone.

### The OTLP receiver is still on TCP

It has to be: containers exporting to `host.docker.internal`, browsers exporting telemetry
and other devices all need a real port, and none of them can use a Unix socket.

| | Reachable from | Notes |
|---|---|---|
| Everything on `.running-man/api.sock` | this machine, this user | Mode 0600 |
| `POST /v1/traces`, `POST /v1/logs` (:4318) | anywhere on the network | Accepts data into the buffer |

- **Anyone who can reach the OTLP port can write into the buffer.** `/v1/logs` accepts log
  records and takes both the source name and the timestamp from the sender, so an entry
  attributed to `backend` is *not* evidence that it came from `backend`. Treat OTLP-sourced
  entries (source type `otlp`) as unauthenticated input.
- **The receiver serves nothing.** It is an ingest endpoint; queries are socket-only.
- CORS uses a wildcard origin (`Access-Control-Allow-Origin: *`) on the receiver, because
  browser-based OTLP export depends on it. The query API no longer sets CORS headers at
  all — a browser cannot reach a Unix socket.

### The OTLP port is not fixed

4318 is the OTLP/HTTP default, so every other collector wants it too — Arize Phoenix, the
OTel Collector, Jaeger, Grafana Alloy, SigNoz — and one Running Man instance per project
means several receivers on one machine. So the receiver takes 4318 or the next free port
above it.

Processes Running Man starts have the chosen endpoint injected, so they follow it without
being told. Anything exporting from outside does not, and should read the port from:

```bash
SOCK=.running-man/api.sock
curl -s --unix-socket "$SOCK" http://localhost/health | jq -r .otlp_endpoint
```

or from the `otlp_endpoint` field of `.running-man/instance.json`.

**Naming the port turns the conflict back into an error.** `--tracing-port` or
`tracing.port` in config means someone chose that port, and quietly using a different one
is the silent substitution this behaviour exists to avoid — so a conflict on a named port
fails at startup instead.
## Examples

### Complete Debugging Workflow

```bash
# 1. Check system health
curl --unix-socket "$SOCK" "http://localhost/health"

# 2. Look for recent errors
curl --unix-socket "$SOCK" "http://localhost/errors?since=5m"

# 3. If trace_id found in errors, investigate trace
curl --unix-socket "$SOCK" "http://localhost/traces/abc123def456"

# 4. Check process status
curl --unix-socket "$SOCK" "http://localhost/processes"

# 5. Search for related logs
curl --unix-socket "$SOCK" "http://localhost/logs?since=10m&contains=database&source=backend"
```

### Using with Scripts

```bash
#!/bin/bash

# Monitor for errors
while true; do
  ERROR_COUNT=$(curl -s --unix-socket "$SOCK" "http://localhost/errors?since=1m" | jq '.count')
  
  if [ "$ERROR_COUNT" -gt 0 ]; then
    echo "Found $ERROR_COUNT error(s) in the last minute"
    curl -s --unix-socket "$SOCK" "http://localhost/errors?since=1m" | jq '.errors[] | .message'
  fi
  
  sleep 60
done
```

### Python Integration

```python
import http.client
import json
import socket


class RunningManClient:
    """Talks to a Running Man instance over its Unix socket.

    http.client is used directly because requests has no Unix-socket support.
    httpx does (``httpx.HTTPTransport(uds=path)``) if you would rather have it.
    """

    def __init__(self, socket_path=".running-man/api.sock"):
        self.socket_path = socket_path

    def _get(self, path, params=None):
        if params:
            from urllib.parse import urlencode
            path = f"{path}?{urlencode(params)}"

        conn = http.client.HTTPConnection("localhost")
        conn.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        conn.sock.connect(self.socket_path)
        try:
            conn.request("GET", path)
            response = conn.getresponse()
            if response.status >= 400:
                raise RuntimeError(f"{path} returned {response.status}")
            return json.load(response)
        finally:
            conn.close()

    def health(self):
        return self._get("/health")

    def get_errors(self, since="5m"):
        return self._get("/errors", {"since": since})

    def get_trace(self, trace_id):
        return self._get(f"/traces/{trace_id}")

    def search_logs(self, **filters):
        return self._get("/logs", filters)


# Usage
client = RunningManClient()

# Confirm which instance answered before trusting anything it says.
print(client.health()["project"])

errors = client.get_errors(since="10m")
for error in errors["errors"]:
    print(f"Error: {error['message']}")
    if error.get("trace_id"):
        spans = client.get_trace(error["trace_id"])
        print(f"  {spans['count']} span(s) in that trace")
```

---

**Note:** All API endpoints are available only when Running Man is running. The server starts automatically when you run `running-man run`.