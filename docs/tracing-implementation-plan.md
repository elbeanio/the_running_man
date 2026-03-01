# Phase 4: OTEL Tracing Implementation Plan

## Overview
Add OpenTelemetry tracing support to Running Man for full-stack observability during local development.

## Goals
- OTLP HTTP receiver (:4318) for trace ingestion
- Environment variable injection for auto-instrumentation
- Trace-log correlation via trace_id
- Basic TUI visualization
- MCP tools for trace querying
- Optional web UI for complex trace viewing

## Architecture Decisions
1. **Direct OTLP receiver** (no separate collector process)
2. **Full context propagation** (tracecontext, baggage)
3. **Integrated buffer** with logs (shared 50MB limit)
4. **No sampling initially** (local dev scale)
5. **HTTP OTLP only** initially (simpler)

## Task Breakdown

### 1. OTLP HTTP Receiver (`the_running_man-dtu`)
**Priority:** 2 | **Estimate:** 2-3 days

**Requirements:**
- HTTP endpoint at `/v1/traces` (OTLP standard)
- Parse OTLP protobuf/JSON payloads
- Basic span storage in memory
- Integration with existing API server
- Configurable port (default: 4318)
- Enable/disable via configuration

**Implementation:**
```go
// New package: internal/tracing
type Receiver struct {
    server *http.Server
    storage *TraceStorage
}

// Use existing OTEL SDK:
// go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp
```

**Dependencies:** None (foundation task)

### 2. Environment Variable Injection (`the_running_man-q2y`)
**Priority:** 2 | **Estimate:** 1-2 days

**Requirements:**
- Inject `OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318`
- Inject `OTEL_SERVICE_NAME=process_name`
- Inject `OTEL_PROPAGATORS=tracecontext,baggage`
- Configurable via `running-man.yml` (`tracing.enabled`)
- Works with both process and Docker Compose sources

**Implementation:**
```go
// Modify ProcessWrapper
func (w *ProcessWrapper) injectOTELEnv(env []string) []string {
    if w.config.Tracing.Enabled {
        env = append(env,
            "OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318",
            fmt.Sprintf("OTEL_SERVICE_NAME=%s", w.name),
            "OTEL_PROPAGATORS=tracecontext,baggage",
        )
    }
    return env
}
```

**Dependencies:** Blocks on OTLP receiver

### 3. Trace Storage & Correlation (`the_running_man-dvl`)
**Priority:** 2 | **Estimate:** 3-4 days

**Requirements:**
- Span storage in integrated ring buffer
- Trace ID indexing for fast lookup
- Correlation: `trace_id → []LogEntry`
- API endpoints: `/traces`, `/traces/{id}`, `/traces/{id}/logs`
- Same retention as logs (30min default)

**Implementation:**
```go
// Extend storage.RingBuffer
type RingBuffer struct {
    logs  []LogEntry
    spans []SpanEntry  // New
    traceIndex map[string][]LogEntry  // Correlation
}

// Add to LogEntry
type LogEntry struct {
    // ... existing fields
    TraceID string `json:"trace_id,omitempty"`  // New
}
```

**Dependencies:** Blocks on OTLP receiver

### 4. MCP Tools for Tracing (`the_running_man-5bk`)
**Priority:** 2 | **Estimate:** 2-3 days

**Requirements:**
- `get_traces`: List recent traces with filters
- `get_trace`: Get specific trace with spans
- `get_correlated_logs`: Logs for a trace_id
- `find_slow_traces`: Traces exceeding duration threshold
- `trace_errors`: Traces with error status

**Implementation:**
```go
// Extend internal/api/mcp.go
func (s *Server) registerGetTracesTool(server *mcp.Server) {
    mcp.AddTool(server, &mcp.Tool{
        Name:        "get_traces",
        Description: "Get recent traces with optional filters",
        // ... parameters
    })
}
```

**Dependencies:** Blocks on trace storage

### 5. TUI Trace Viewer (`the_running_man-zas`)
**Priority:** 3 | **Estimate:** 3-4 days

**Requirements:**
- New trace tab in TUI (press 't')
- List recent traces with summary (duration, status, service)
- Select trace to view spans tree
- Show correlated logs with trace
- Basic ASCII visualization of span hierarchy

**Implementation:**
```go
// Extend TUI model
type Model struct {
    // ... existing fields
    traceView TraceViewState  // New
}

// ASCII tree rendering
func renderSpanTree(spans []Span) string {
    // Simple indented tree
}
```

**Dependencies:** Blocks on trace storage

### 6. Configuration & Documentation (`the_running_man-4t3`)
**Priority:** 3 | **Estimate:** 1-2 days

**Requirements:**
- YAML schema for tracing configuration
- Environment variable support
- CLI flag support (`--tracing-port`, `--no-tracing`)
- Example configurations for Python/Node/Go
- Integration guide in docs/
- MCP tools documentation

**Implementation:**
```yaml
# running-man.yml
tracing:
  enabled: true
  port: 4318
  retention: 30m  # Same as logs
```

**Dependencies:** Blocks on trace storage

### 7. Optional Web UI (`the_running_man-a8t`)
**Priority:** 4 | **Estimate:** 4-5 days (optional)

**Requirements:**
- Optional flag `--web-ui` (default: disabled)
- Web server on different port (e.g., 9001)
- Flamegraph visualization using existing JS libraries
- Timeline view with correlated logs
- Export to Jaeger UI format
- Responsive design for local development

**Implementation:**
```go
// Separate package: internal/webui
type WebUIServer struct {
    traceStorage *TraceStorage
    server *http.Server
}
```

**Dependencies:** Optional, can be skipped

## Technical Details

### Full Context Propagation Support
**Why it matters:** Python/Node OTEL libs automatically use W3C Trace Context headers when `OTEL_PROPAGATORS=tracecontext,baggage` is set.

**What we get for free:**
- Automatic `trace_id` injection in logs
- Parent/child span relationships
- HTTP header propagation between services
- Baggage for custom attributes

**Python example (auto-instrumented):**
```python
# With OTEL env vars injected by Running Man
from opentelemetry import trace

# Automatic tracing
@app.route("/api/users")
def get_users():
    # trace_id automatically injected
    logger.info("Fetching users")  # Has trace_id
    # Child spans created automatically
```

### Storage Estimates
**Without optimization:**
- Span size: ~0.5KB (limited attributes)
- 100 spans/minute: ~30KB/30min
- **Total:** < 1MB/30min ✅ Fits in buffer

**With future optimization (if needed):**
- Sampling (10%): 0.1MB/30min
- Attribute truncation: 0.05MB/30min

### API Endpoints
```
GET  /traces                    # List traces
GET  /traces/{id}              # Get trace details
GET  /traces/{id}/logs         # Correlated logs
GET  /traces?since=5m&min_duration=100ms
GET  /traces?status=error
```

### MCP Tool Examples
```
"Show me slow traces from the last 5 minutes"
"Get traces with errors from the backend service"
"Find logs correlated with trace abc123"
"What's the slowest API endpoint?"
```

## Testing Strategy

### Unit Tests
- OTLP receiver parsing
- Trace storage operations
- Environment injection
- Correlation logic

### Integration Tests
- Python/Node example apps sending traces
- Trace-log correlation verification
- TUI rendering tests
- MCP tool responses

### Manual Testing
- Real microservices setup
- Performance under load
- Memory usage monitoring
- User experience feedback

## Success Criteria

1. **Python/Node apps can send traces automatically** with injected env vars
2. **Traces correlated with logs** in TUI and API
3. **MCP tools provide useful trace debugging** for AI agents
4. **TUI shows basic trace visualization** (list + detail view)
5. **Documentation enables easy setup** for common frameworks

## Risks & Mitigation

| Risk | Mitigation |
|------|------------|
| Storage overload | No sampling initially, monitor usage |
| Performance impact | Async processing, configurable limits |
| Complex visualization | Start with simple ASCII tree |
| Library compatibility | Test with Python/Node OTEL SDKs |
| Memory usage | Integrated buffer with shared limit |

## Timeline

**Week 1-2:** Foundation (OTLP receiver + env injection)
**Week 3:** Storage & correlation
**Week 4:** MCP tools + TUI viewer
**Week 5:** Configuration + documentation
**Week 6+:** Optional web UI (if needed)

## Next Steps

1. Start with `the_running_man-dtu` (OTLP receiver)
2. Implement incrementally, testing at each step
3. Gather feedback after basic functionality
4. Adjust plan based on real usage

---

*Last updated: March 1, 2026*  
*Phase 4 Epic: `the_running_man-yut`*