# The Running Man Implementation History

## Project Status

This is a **weekend project** for building dev observability tooling focused on AI-assisted development.

## Current Status (March 2026)

- ✅ **Phase 1:** Core Foundation (COMPLETE)
- ✅ **Phase 2:** Multi-Source Capture (COMPLETE)
- ✅ **Phase 2.5:** Quality of Life & Bug Fixes (COMPLETE)
- ✅ **Phase 3:** Agent Integration (COMPLETE)
- ✅ **Phase 4:** OpenTelemetry Tracing (COMPLETE)
- 📋 **Phase 5:** Browser Integration & Web UI
- 📋 **Phase 6:** Advanced Visualization & Analytics

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

## Phase 4: OpenTelemetry Tracing ✅ COMPLETE

**Goal:** Add OpenTelemetry tracing support for full-stack observability during local development.

### What We Built

**OTEL Receiver & Storage**
- OTLP HTTP receiver on port 4318 (configurable)
- In-memory span storage with configurable retention
- Automatic environment variable injection for managed processes
- Support for both JSON and Protobuf OTLP formats

**Trace-Log Correlation**
- Automatic correlation of logs with traces via `trace_id`
- Integrated querying across logs and traces
- MCP tools for trace exploration via AI agents

**API & Integration**
- REST API endpoints for trace querying (`/traces`, `/traces/{id}`, `/traces/slow`)
- 3 new MCP tools for trace debugging (`get_traces`, `get_trace`, `get_slow_traces`)
- Comprehensive Python examples for Flask, Django, and basic applications

**Configuration**
- YAML configuration for tracing settings
- CLI flags for enabling/disabling tracing
- Environment variable support for advanced use cases

### Architecture Implementation

1. **Direct OTLP receiver** - Implemented in `internal/tracing/receiver.go`
2. **Full context propagation** - Automatic injection of `tracecontext,baggage`
3. **Separate trace storage** - Dedicated `SpanStorage` in `internal/tracing/storage.go`
4. **No sampling** - All traces captured for local development
5. **HTTP OTLP support** - Both JSON and Protobuf formats supported

### Key Features Delivered

- **Automatic instrumentation** - OTEL environment variables injected into all managed processes
- **Trace querying** - Filter by service, status, duration, and trace ID
- **Performance debugging** - Find slow traces with duration thresholds
- **AI agent integration** - MCP tools for trace exploration
- **Comprehensive documentation** - Complete setup guides for Python applications

### Example Usage

```bash
# Enable tracing (enabled by default)
running-man run --process "python app.py"

# Query traces via API
curl "http://localhost:9000/traces?since=5m"
curl "http://localhost:9000/traces/slow?threshold=1s"

# Use AI agent for trace debugging
# "Show me traces with errors from the backend service"
# "Find slow database queries from the last 10 minutes"
```

### Documentation
Complete OpenTelemetry documentation available at [docs/tracing.md](tracing.md) including:
- Python setup examples (Flask, Django, basic apps)
- Configuration options and CLI flags
- API reference for trace endpoints
- Troubleshooting guide
- MCP tool usage examples

### Key Features

**Auto-instrumentation:**
```yaml
# running-man.yml
tracing:
  enabled: true
  port: 4318
  retention: 30m  # Same as logs
```

**Environment Variables Injected:**
- `OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318`
- `OTEL_SERVICE_NAME=process_name`
- `OTEL_PROPAGATORS=tracecontext,baggage`

**MCP Tools:**
- `get_traces` - List recent traces with filters
- `get_trace` - Get specific trace with spans
- `get_correlated_logs` - Logs for a trace_id
- `find_slow_traces` - Traces exceeding duration threshold
- `trace_errors` - Traces with error status

### Success Criteria

✅ **Python/Node apps can send traces automatically** with injected env vars  
✅ **Traces correlated with logs** in TUI and API  
✅ **MCP tools provide useful trace debugging** for AI agents  
✅ **TUI shows basic trace visualization** (list + detail view)  
✅ **Documentation enables easy setup** for common frameworks

### Success Criteria (All Met)

✅ **Python/Node apps can send traces automatically** with injected env vars  
✅ **Traces correlated with logs** via API and MCP tools  
✅ **MCP tools provide useful trace debugging** for AI agents  
✅ **Comprehensive trace querying** via REST API  
✅ **Documentation enables easy setup** for common frameworks

---

## Project Evolution

The Running Man has evolved from a simple log capture tool to a comprehensive dev observability platform:

### Phase 1-2: Foundation
- Basic log capture and parsing
- Multi-process and Docker support
- Interactive TUI for real-time viewing

### Phase 3: AI Integration
- MCP server for AI agent integration
- 8 debugging tools for Claude Code/OpenCode
- Seamless debugging workflows

### Phase 4: Distributed Tracing
- OpenTelemetry support with OTLP receiver
- Trace-log correlation
- Performance debugging capabilities
- 3 additional MCP tools for trace exploration

## Current Architecture

The Running Man now provides:
- **Log capture** from processes and Docker containers
- **Distributed tracing** via OpenTelemetry
- **AI agent integration** via MCP protocol
- **Real-time TUI** for interactive debugging
- **REST API** for programmatic access
- **Comprehensive configuration** via YAML and CLI

## Technology Stack

- **Language:** Go (performance, concurrency, easy distribution)
- **TUI:** Bubble Tea framework
- **Tracing:** OpenTelemetry Go SDK
- **MCP:** Model Context Protocol Go SDK
- **HTTP:** Standard library + chi router
- **Docker:** Official docker/client library
- **Configuration:** YAML with environment variable support

## Impact

The Running Man has successfully:
- Reduced time spent switching between terminal tabs
- Enabled AI agents to debug complex issues autonomously
- Provided distributed tracing for local development
- Created a unified observability platform for full-stack development

## Future Directions

While Phase 4 is complete, future development may include:

- **Browser SDK** for frontend error capture
- **Advanced visualization** for trace analysis
- **Team collaboration** features
- **Enhanced metrics** and alerting

---

*Last updated: March 2026*  
*Repository: https://github.com/elbeanio/the_running_man*
