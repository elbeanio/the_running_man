# Sidecar: Dev Observability Tool

## Overview

A standalone process that provides unified observability for local development environments. Captures logs, traces, and errors from multiple sources and exposes them via a query API for agent consumption.

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
┌────────────────────────────────────────────────────────────────┐
│                         Sidecar                                │
│                                                                │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐         │
│  │   Process    │  │    Docker    │  │    OTEL      │         │
│  │   Wrapper    │  │  Log Tailer  │  │  Collector   │         │
│  │              │  │              │  │              │         │
│  │  captures    │  │  attaches to │  │  receives    │         │
│  │  stdout/err  │  │  containers  │  │  spans via   │         │
│  │  from child  │  │  from compose│  │  gRPC/HTTP   │         │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘         │
│         │                 │                 │                  │
│         └─────────────────┼─────────────────┘                  │
│                           ▼                                    │
│                 ┌──────────────────┐                           │
│                 │   Ring Buffer    │                           │
│                 │                  │                           │
│                 │  - log entries   │                           │
│                 │  - spans/traces  │                           │
│                 │  - parsed errors │                           │
│                 └────────┬─────────┘                           │
│                          │                                     │
│                          ▼                                     │
│                 ┌──────────────────┐                           │
│                 │    Query API     │                           │
│                 │                  │                           │
│                 │  GET /logs       │                           │
│                 │  GET /traces     │                           │
│                 │  GET /errors     │                           │
│                 │  WS  /stream     │                           │
│                 └──────────────────┘                           │
│                          │                                     │
└──────────────────────────┼─────────────────────────────────────┘
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

### 3. OTEL Collector

**Responsibility:** Receive OTEL spans from instrumented applications.

**Behavior:**
- Listens on configurable port (default 4317 gRPC, 4318 HTTP)
- Receives spans in OTLP format
- Extracts relevant fields (trace ID, span name, duration, status, attributes)
- Stores in ring buffer with indexing by trace/workflow ID

**Input:** OTLP spans
**Output:** Structured span data to ring buffer

### 4. Ring Buffer / Storage

**Responsibility:** Store recent logs, traces, and errors with fast query access.

**Implementation options:**
- In-memory with size/time cap (default)
- SQLite for persistence across restarts

**Schema (conceptual):**
```
LogEntry {
  timestamp: time
  source: string      // "python-server", "vue-dev", "postgres", etc.
  level: string       // "info", "error", "debug"
  message: string
  raw: string         // original line
  trace_id: string?   // if correlated
  metadata: map       // parsed fields from JSON logs
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

### 5. Query API

**Responsibility:** Expose data for agent consumption.

**Endpoints:**

```
GET /logs
  ?since=30s           // time window
  ?source=python-*     // filter by source (glob)
  ?level=error,warn    // filter by level
  ?contains=traceback  // text search
  ?trace_id=abc123     // filter by correlation

GET /traces
  ?trace_id=abc123     // specific trace
  ?workflow_id=xyz     // all spans for a workflow
  ?since=5m            // recent traces
  ?status=error        // only errored spans

GET /errors
  ?since=5m
  ?source=*
  ?type=python_*

GET /health
  Returns status of all capture sources

WS /stream
  Live tail of logs/errors (optional, nice-to-have)
```

**Response format:** JSON, structured for easy agent parsing

---

## CLI Interface

```bash
# Basic usage - wrap a single command
sidecar run -- python server.py

# Multiple wrapped processes
sidecar run \
  --wrap "python server.py" \
  --wrap "npm run dev"

# With docker-compose
sidecar run \
  --wrap "python server.py" \
  --docker-compose ./docker-compose.yml

# Full config
sidecar run \
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

### Phase 3: OTEL Integration
- OTLP receiver (gRPC + HTTP)
- Span storage and indexing
- `/traces` endpoint
- Trace ID correlation

### Phase 4: Polish
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

1. **Process management:** How much should sidecar do? Restart on crash? Health checks? Or keep it simple and just observe?

2. **Frontend SDK:** Worth building a tiny JS helper for trace ID propagation? Or just document the header convention?

3. **Config file:** YAML/TOML config for complex setups, or keep it CLI-only?

4. **Agent integration:** Should we define a standard "context dump" format that agents expect? Or let them query flexibly?
