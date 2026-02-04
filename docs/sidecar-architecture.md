# The Running Man: Dev Observability Tool

## Overview

A standalone process that provides unified observability for local development environments. Captures logs, traces, and errors from multiple sources (backend processes, Docker containers, browsers) and exposes them via a query API for agent consumption.

## Problem Statement

When developing apps with AI agents, debugging requires manually collecting context from multiple sources (server logs, docker containers, OTEL traces, frontend errors) and pasting into the agent. This is tedious and loses important context.

## Goals

- Capture stdout/stderr from wrapped processes (survives crashes)
- Tail logs from docker-compose containers
- Run lightweight OTEL collector for structured traces
- Expose unified query API for agents
- Keep instrumentation minimal in application code
- Reusable across multiple projects with same stack pattern

## Non-Goals (for now)

- Production deployment
- Distributed tracing across multiple hosts
- Long-term log storage/analysis

---

## Architecture

```
┌───────────────────────────────────────────────────────────────────────┐
│                         The Running Man                               │
│                                                                       │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐             │
│  │ Process  │  │  Docker  │  │ Browser  │  │   OTEL   │             │
│  │ Wrapper  │  │   Log    │  │   Log    │  │Collector │             │
│  │          │  │  Tailer  │  │ Capture  │  │          │             │
│  │ captures │  │ attaches │  │ receives │  │ receives │             │
│  │stdout/err│  │   to     │  │  POST    │  │  spans   │             │
│  │from child│  │containers│  │ /ingest  │  │gRPC/HTTP │             │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘  └────┬─────┘             │
│       │             │             │             │                    │
│       └─────────────┴─────────────┴─────────────┘                    │
│                              ▼                                        │
│                    ┌──────────────────┐                               │
│                    │   Ring Buffer    │                               │
│                    │                  │                               │
│                    │  - log entries   │                               │
│                    │  - browser logs  │                               │
│                    │  - spans/traces  │                               │
│                    │  - parsed errors │                               │
│                    └────────┬─────────┘                               │
│                             │                                         │
│                             ▼                                         │
│                    ┌──────────────────┐                               │
│                    │    Query API     │                               │
│                    │                  │                               │
│                    │  GET /logs       │                               │
│                    │  GET /browser    │                               │
│                    │  GET /traces     │                               │
│                    │  GET /errors     │                               │
│                    │  POST /ingest    │                               │
│                    │  WS  /stream     │                               │
│                    └──────────────────┘                               │
│                             │                                         │
└─────────────────────────────┼─────────────────────────────────────────┘
                              │
                              ▼
                       Agent queries
```

---

## Components

### 1. Process Wrapper

**Responsibility:** Spawn and monitor child processes, capture their output.

**Behavior:**
- Spawns child process with inherited environment
- Captures stdout/stderr streams
- Passes output through to terminal (so dev still sees logs)
- Parses output for structured data (JSON logs, Python tracebacks)
- Tags entries with process name/source
- Handles process restart on crash (optional, for autoreload scenarios)

**Input:** Command string to execute
**Output:** Stream of log entries to ring buffer

### 2. Docker Log Tailer

**Responsibility:** Attach to docker-compose containers and capture their logs.

**Behavior:**
- Parses docker-compose.yml to discover containers
- Attaches to each container's log stream
- Tags entries with container name
- Handles container restarts

**Input:** Path to docker-compose.yml
**Output:** Stream of log entries to ring buffer

### 3. Browser Log Capture

**Responsibility:** Capture console logs, errors, and network failures from the browser.

**Approach:** Lightweight JS snippet loaded in dev mode only.

**Behavior:**
- Hooks `console.log/warn/error/debug`
- Catches `window.onerror` and unhandled promise rejections
- Optionally hooks `fetch`/`XHR` to capture failed requests
- Batches and POSTs to sidecar ingest endpoint
- Includes active trace ID if present (correlates with backend spans)

**Integration:**
```javascript
// main.js - only loads in dev
if (import.meta.env.DEV) {
  import('./sidecar-client.js')
}
```

**Ingest endpoint:** `POST /ingest/browser`

**Payload:**
```json
{
  "entries": [
    {
      "type": "console",
      "level": "error",
      "message": "TypeError: Cannot read property 'foo' of undefined",
      "stack": "...",
      "timestamp": "2024-01-15T10:30:00Z",
      "trace_id": "abc123",
      "url": "http://localhost:5173/dashboard"
    },
    {
      "type": "network",
      "method": "POST",
      "url": "/api/users",
      "status": 500,
      "timestamp": "...",
      "trace_id": "abc123"
    }
  ]
}
```

**SDK features:**
- Auto-batching (send every N entries or M milliseconds)
- Configurable running man URL (defaults to `localhost:9000`)
- Trace ID propagation helper for fetch wrapper
- Minimal footprint (~2KB minified)

---

### 4. OTEL Collector

**Responsibility:** Receive OTEL spans from instrumented applications.

**Behavior:**
- Listens on configurable port (default 4317 gRPC, 4318 HTTP)
- Receives spans in OTLP format
- Extracts relevant fields (trace ID, span name, duration, status, attributes)
- Stores in ring buffer with indexing by trace/workflow ID

**Input:** OTLP spans
**Output:** Structured span data to ring buffer

### 5. Ring Buffer / Storage

**Responsibility:** Store recent logs, traces, and errors with fast query access.

**Implementation options:**
- In-memory with size/time cap (default)
- SQLite for persistence across restarts

**Schema (conceptual):**
```
LogEntry {
  timestamp: time
  source: string      // "python-server", "vue-dev", "postgres", "browser", etc.
  level: string       // "info", "error", "debug"
  message: string
  raw: string         // original line
  trace_id: string?   // if correlated
  metadata: map       // parsed fields from JSON logs
}

BrowserEntry {
  timestamp: time
  type: string        // "console", "network", "error"
  level: string?      // for console: "log", "warn", "error"
  message: string
  stack: string?      // for errors
  url: string         // page URL
  trace_id: string?
  // network-specific
  method: string?
  request_url: string?
  status: int?
}

Span {
  trace_id: string
  span_id: string
  parent_span_id: string?
  name: string
  start_time: time
  duration: duration
  status: string
  attributes: map
  workflow_id: string?  // custom attribute for DAG flows
}

Error {
  timestamp: time
  source: string
  type: string        // "python_traceback", "js_error", etc.
  message: string
  stacktrace: string
  trace_id: string?
}
```

**Retention:** Configurable, default 30 minutes or 50MB

### 6. Query API

**Responsibility:** Expose data for agent consumption.

**Endpoints:**

```
GET /logs
  ?since=30s           // time window
  ?source=python-*     // filter by source (glob)
  ?level=error,warn    // filter by level
  ?contains=traceback  // text search
  ?trace_id=abc123     // filter by correlation

GET /browser
  ?since=30s
  ?level=error,warn    // filter by console level
  ?type=console,network,error
  ?trace_id=abc123

GET /traces
  ?trace_id=abc123     // specific trace
  ?workflow_id=xyz     // all spans for a workflow
  ?since=5m            // recent traces
  ?status=error        // only errored spans

GET /errors
  ?since=5m
  ?source=*            // includes "browser" as source
  ?type=python_*

GET /health
  Returns status of all capture sources

WS /stream
  Live tail of logs/errors (optional, nice-to-have)

POST /ingest/browser
  Receives batched browser entries from JS SDK
```

**Response format:** JSON, structured for easy agent parsing

---

## CLI Interface

```bash
# Basic usage - wrap a single command
running-man run -- python server.py

# Multiple wrapped processes
running-man run \
  --wrap "python server.py" \
  --wrap "npm run dev"

# With docker-compose
running-man run \
  --wrap "python server.py" \
  --docker-compose ./docker-compose.yml

# Full config
running-man run \
  --wrap "python server.py" \
  --wrap "npm run dev" \
  --docker-compose ./docker-compose.yml \
  --otel-port 4317 \
  --api-port 9000 \
  --retention 30m
```

**Signals:**
- SIGINT/SIGTERM: Graceful shutdown, terminates child processes
- Child process exit: Log it, optionally restart

---

## Log Parsing

### Python Tracebacks

Detect multi-line tracebacks and group into single error entry:

```
Traceback (most recent call last):
  File "server.py", line 42, in handler
    result = process(data)
  File "lib.py", line 17, in process
    raise ValueError("bad input")
ValueError: bad input
```

→ Single `Error` entry with full stacktrace

### JSON Logs

Parse structured logs and extract fields:

```json
{"timestamp": "...", "level": "error", "message": "...", "trace_id": "..."}
```

→ `LogEntry` with populated metadata and trace_id

### Plain Text

Fall back to line-by-line with heuristic level detection:

```
[ERROR] Something went wrong
2024-01-15 10:30:00 WARNING: disk space low
```

---

## Trace Correlation

### Frontend → Backend

Lightweight approach without full frontend OTEL:

1. Frontend generates trace ID on user action
2. Passes as header on API calls: `X-Trace-ID: <id>`
3. Backend middleware extracts and sets on span context
4. All backend spans tagged with trace ID

### Workflow/DAG Tracking

For multi-step agentic flows:

1. Assign workflow ID at start of DAG
2. Pass as span attribute: `workflow.id`
3. Each step includes workflow ID + step name
4. Query API can retrieve entire workflow: `GET /traces?workflow_id=X`

---

## Implementation Phases

### Phase 1: Process Wrapper + Basic API
- Single process wrapping
- stdout/stderr capture with terminal passthrough
- Basic log parsing (tracebacks, levels)
- In-memory ring buffer
- `/logs` and `/errors` endpoints

### Phase 2: Multi-process + Docker
- Multiple `--wrap` commands
- Docker-compose log tailing
- Source filtering in API

### Phase 3: Browser Capture
- JS SDK for console/error capture
- `/ingest/browser` endpoint
- `/browser` query endpoint
- Trace ID propagation helpers

### Phase 4: OTEL Integration
- OTLP receiver (gRPC + HTTP)
- Span storage and indexing
- `/traces` endpoint
- Trace ID correlation across browser → backend

### Phase 5: Polish
- WebSocket streaming
- SQLite persistence option
- Config file support
- Better error detection heuristics

---

## Tech Stack

- **Language:** Go
- **HTTP:** net/http or chi router
- **OTEL:** go.opentelemetry.io/collector (or minimal custom receiver)
- **Docker:** docker client library
- **Storage:** In-memory maps + mutex, optional SQLite

---

## Open Questions

1. **Process management:** How much should the running man do? Restart on crash? Health checks? Or keep it simple and just observe?

2. **Browser SDK distribution:** npm package? Single file you copy into your project? Both?

3. **Config file:** YAML/TOML config for complex setups, or keep it CLI-only?

4. **Agent integration:** Should we define a standard "context dump" format that agents expect? Or let them query flexibly?

5. **Source maps:** Should the browser SDK attempt to resolve source maps for better stack traces? Adds complexity but improves error readability.