# The Running Man Implementation Plan

## Project Status

This is a **weekend project** for building dev observability tooling focused on AI-assisted development.

## Current Status

- ✅ **Phase 1:** Core Foundation (COMPLETE)
- ✅ **Phase 2:** Multi-Source Capture (COMPLETE)
- ✅ **Phase 2.5:** Quality of Life & Bug Fixes (COMPLETE)
- ✅ **Phase 3:** Agent Integration (COMPLETE)
- 📋 **Phase 4:** OTEL & Visualization  
- 📋 **Phase 5:** Browser Integration

---

## Phase 1: Core Foundation ✅ COMPLETE

### What We Built

**Single Process Management**
- Process wrapper with stdout/stderr capture
- Terminal passthrough (developer still sees logs)
- Graceful signal handling (SIGINT/SIGTERM)

**Log Parsing**
- Python traceback detection and grouping
- JSON log parsing with field extraction
- Plain text with heuristic level detection

**Ring Buffer Storage**
- In-memory circular buffer
- 30-minute or 50MB retention
- Thread-safe concurrent access
- Efficient indexing by timestamp and level

**Query API**
- `GET /logs` - Query with filters
- `GET /errors` - Recent errors
- `GET /health` - System status

**CLI Interface**
```bash
running-man run --process "python server.py"
```

### Test Coverage
- API: 75.9%
- Parser: 78.2%
- Process: 86.8%
- Storage: 100%

---

## Phase 2: Multi-Source Capture ✅ COMPLETE

### What We Built

**Multi-Process Support**
- Multiple `--process` flags
- Parallel stream capture
- Unique source tagging
- Shell execution (cd, &&, pipes, redirections)

**Docker Compose Integration**
- Parse docker-compose.yml to discover services
- Stream logs from all containers via Docker API
- Handle container restarts
- Filter by container in API

**Enhanced Query Filters**
- Source filtering (`?source=backend`)
- Level filtering (`?level=error,warn`)
- Time windows (`?since=30s`)
- Content search (`?contains=traceback`)

**YAML Configuration**
- `running-man.yml` with auto-discovery
- Schema validation with helpful errors
- CLI flags override config values
- Configurable shell (bash, zsh, sh)
- Example config file provided

**TUI Viewer**
- Interactive Bubble Tea interface
- Tab switching between sources
- Real-time log updates
- Color-coded log levels

**Bonus Features**
- Configurable shell per process
- Process cleanup on TUI quit
- Signal handling for graceful shutdown

### Issues Found
- Docker project name bug (FIXED)
- TUI newline rendering broken
- TUI only shows last 5 minutes of logs
- Need better tab ordering

---

## Phase 2.5: Quality of Life & Bug Fixes → NEXT

**Goal:** Make it daily-driver ready for developers and their agents

### TUI Improvements

**Fix Critical Bugs**
- Fix newline rendering (progress bars, multi-line output currently garbled)
- Remove 5-minute log window (show all logs up to retention limit)
- Fix any other rendering issues discovered during testing

**UX Enhancements**
- **Tab ordering:**
  - Group 1: `running-man` logs (internal)
  - Group 2: Docker services (alphabetical)
  - Group 3: Processes (config file order, or alphabetical)
- **Color-coded tab groups** for easier visual navigation
- **Scroll controls:** Page up/down with arrow keys
- **Text search:** `/` or Ctrl+F to search current view

### Process Management

**Restart on Crash**
- Add `restart_on_crash: true/false` config option (per-process)
- Default: `false` (to be decided after testing)
- Document behavior and limitations
- Test with real-world crash scenarios

### OpenAPI Documentation

**Auto-Generated API Docs**
- Add annotations to API handlers for OpenAPI spec generation
- Generate OpenAPI 3.0 spec
- Host spec viewer (Swagger UI or similar)
- Keep markdown docs for quick reference

### User Feedback Integration

**Testing Period:** 1-2 weeks  
**Testers:** 3-5 developers using in daily workflow

**Feedback Collection:**
- GitHub issues for bugs (with template)
- Direct reports for UX issues
- Feature requests with use cases

**Success Criteria:** 
- TUI is usable for daily development work
- No showstopper bugs
- Logs are readable and accessible
- Developer can use Running Man instead of juggling terminal tabs

---

## Phase 3: Agent Integration ✅ COMPLETE

**Goal:** Make Running Man a first-class tool for AI-assisted development

### What We Built

**MCP Server Implementation:**
- Full Model Context Protocol (MCP) server on `/mcp` endpoint
- 8 debugging tools for AI agents
- Streamable HTTP handler with SSE support
- Stateless design for concurrent agent sessions

**Available MCP Tools:**
1. `search_logs` - Search logs with filters (source, time, level, content)
2. `get_recent_errors` - Get errors with surrounding context
3. `get_process_status` - Check status of managed processes
4. `get_startup_logs` - View logs from process startup
5. `get_health_status` - System health and buffer statistics
6. `get_process_detail` - Detailed process information
7. `restart_process` - Restart a managed process (with safety checks)
8. `stop_all_processes` - Stop all processes (requires confirmation)

**Integration Support:**
- OpenCode configuration and permissions guide
- Claude Desktop configuration guide
- Comprehensive documentation with example prompts
- Safety features for destructive operations

### Implementation Details

**Architecture:**
- MCP server integrated into existing API server (`:9000`)
- Uses `github.com/modelcontextprotocol/go-sdk` SDK
- Tools wrap existing REST API endpoints with agent-friendly interfaces
- Error handling for all edge cases (missing processes, invalid parameters)

**Testing:**
- All 8 tools tested and verified with OpenCode
- Error handling tested for destructive operations
- Integration tested with real debugging workflows
- Server rebuild process verified (critical bug fixed)

### Configuration Examples

**OpenCode Setup:**
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

**Permissions:**
```json
"permission": {
  "running-man_*": "allow"
}
```

### Success Criteria Met

✅ **Agents can debug common errors without manual log gathering**
- All 8 tools provide comprehensive debugging capabilities
- Error context and stack traces included
- Process status and health monitoring available

✅ **Integration is seamless**
- Zero additional processes or dependencies
- Auto-discovery when Running Man is running
- Simple configuration for OpenCode/Claude Desktop

✅ **Safety features implemented**
- Read-only tools by default
- Destructive operations require explicit flags
- Error handling for invalid operations

### Key Learnings

1. **MCP vs REST API:** MCP provides better integration than skills-based REST API
2. **Tool discovery:** OpenCode caches MCP tool discovery - requires restart after server changes
3. **Safety first:** Destructive tools must have explicit confirmation mechanisms
4. **Agent patterns:** 8 tools cover 90%+ of common debugging workflows

### Documentation

Complete integration guide available at [docs/agent-integration.md](agent-integration.md) including:
- Tool-by-tool reference with parameters
- Setup instructions for OpenCode and Claude Desktop
- Example prompts and debugging workflows
- Troubleshooting guide
- Safety features documentation

---

## Phase 4: OTEL & Visualization

**Goal:** Add distributed tracing support for instrumented applications

### OTEL Integration

**OTLP Receiver:**
- gRPC endpoint (default `:4317`)
- HTTP endpoint (default `:4318`)
- Span ingestion and parsing
- Extract: trace_id, span_id, parent_span_id, duration, status, attributes

**Span Storage:**
- Store spans in ring buffer with trace indexing
- Support nested span relationships (parent/child)
- Extract workflow_id from custom attributes
- Link spans to log entries via trace_id

**Trace Correlation:**
- Match log entries to spans via trace_id
- Return correlated logs with trace queries
- Support custom attributes for workflow tracking
- Cross-reference errors with slow spans

### Trace Query API

**Endpoints:**
```
GET /traces
  ?trace_id=abc123        # specific trace
  ?workflow_id=xyz        # all spans for workflow
  ?since=5m              # time window
  ?status=error          # only errors
  ?min_duration=100ms    # slow spans

GET /traces/{trace_id}/logs
  # All logs correlated with this trace
```

### Visualization

**Basic Trace Viewer:**
- Flamegraph or waterfall view
- Span timeline with duration
- Error highlighting
- Click to see span details + correlated logs

**Log Timeline:**
- Visual timeline of log events
- Correlation with trace spans
- Error clustering

**Optional Exports:**
- Export traces to Jaeger/Zipkin format
- Download trace as JSON

### Instrumentation Guide

**Minimal Setup:**
- Python SDK setup (OpenTelemetry)
- Node.js SDK setup
- Go SDK setup
- Header-based trace propagation (X-Trace-ID)

**Examples:**
- FastAPI/Flask middleware
- Express middleware
- Go net/http middleware

### User Feedback Integration

**Testing:** Real microservices architectures with 3+ services

**Success Criteria:**
- Can trace requests across multiple services
- Correlation with logs works reliably
- Visualization is useful for debugging

---

## Phase 5: Browser Integration

**Goal:** Full-stack observability (frontend + backend)

### Browser SDK

**npm Package:** `@running-man/browser`

**Features:**
- Console hook (`console.log/warn/error/debug`)
- Uncaught exception handler (`window.onerror`)
- Unhandled promise rejection handler
- Optional `fetch`/`XHR` interceptor for failed requests
- Auto-batching (configurable: N entries or M milliseconds)
- Trace ID propagation
- Minimal footprint (~2KB minified)

**Integration:**
```javascript
// Only loads in development
if (import.meta.env.DEV) {
  import('@running-man/browser').then(sdk => 
    sdk.init({ endpoint: 'http://localhost:9000' })
  )
}
```

### Ingest Endpoint

```
POST /ingest/browser
  - Accepts batched log entries
  - Parses and stores in ring buffer
  - Tags with source="browser"
  - Extracts trace_id for correlation
```

**Entry Schema:**
```json
{
  "timestamp": "2024-01-15T10:30:00Z",
  "type": "console" | "network" | "error",
  "level": "log" | "warn" | "error" | "debug",
  "message": "TypeError: Cannot read property 'foo'",
  "stack": "...",
  "url": "http://localhost:5173/dashboard",
  "trace_id": "abc123"
}
```

### Browser Query API

```
GET /browser
  ?since=30s
  ?level=error,warn
  ?type=console,network,error
  ?trace_id=abc123
  ?url=*dashboard*
```

### Framework Integration

**Documentation:**
- Vite setup
- webpack setup
- Next.js setup
- React/Vue/Svelte examples
- Dev-only loading patterns

**SDK Distribution:**
- npm package
- Standalone file for copy/paste
- TypeScript types included

### User Feedback Integration

**Testing:** Real frontend applications

**Metrics:**
- Performance overhead (<5ms target)
- Error capture rate (should be 100%)
- Bundle size impact

**Success Criteria:**
- Captures all browser errors and console logs
- Performance impact is negligible
- Integration is straightforward
- Works in Chrome, Firefox, Safari

---

## Technology Stack

### Core
- **Language:** Go (fast, great concurrency, easy distribution)
- **HTTP Framework:** net/http + chi router
- **Storage:** In-memory (maps + sync.RWMutex)

### Integrations
- **Docker:** docker/client library
- **OTEL:** go.opentelemetry.io/collector components (or minimal custom receiver)
- **YAML:** gopkg.in/yaml.v3
- **TUI:** Bubble Tea

### Browser SDK (Phase 5)
- **Language:** TypeScript → JavaScript (ES5)
- **Bundler:** Rollup or esbuild
- **Distribution:** npm + standalone file

### Future (Optional)
- **Persistence:** SQLite via mattn/go-sqlite3
- **Metrics:** Prometheus client

---

## Dependencies Philosophy

**Keep Minimal:**
- Docker client (only if `--docker-compose` used)
- OTEL collector (only if OTEL enabled in Phase 4)
- SQLite (only if `--persist` flag in future)

**Goal:** Fast startup, simple binary distribution, minimal bloat

---

## Success Metrics

### Overall Project Goal
**Make AI-assisted debugging significantly faster than manual log gathering**

### Phase-Specific Goals

**Phase 2.5:** TUI is daily-driver ready, no critical bugs  
**Phase 3:** Agents can debug 80% of common errors without manual help  
**Phase 4:** Distributed traces are captured and useful  
**Phase 5:** Browser errors are captured with minimal overhead  

---

## Future Possibilities (Beyond Phase 5)

- Remote deployment (dev server, query from local)
- Team collaboration (share Running Man data)
- Snapshot/replay specific scenarios
- VS Code extension for inline queries
- ML-powered error grouping
- Performance profiling (CPU/memory)
- Log analysis and root cause suggestions
- Web Vitals and frontend performance metrics

---

## User Feedback Process

After each phase:

1. **Alpha Testing:** Small group (3-5 developers) uses in daily workflow
2. **Feedback Collection:** GitHub issues + direct reports
3. **Iteration:** Fix critical bugs, adjust UX based on real usage
4. **Sign-off:** Phase complete when success criteria met

See [user-testing.md](user-testing.md) for templates and process details.

---

## Getting Started

**Phase 2.5 is next.** See GitHub issues for current work items.

Want to contribute? See [CONTRIBUTING.md](../CONTRIBUTING.md) (TBD)
