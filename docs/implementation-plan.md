# The Running Man Implementation Plan

## Executive Summary

This document outlines the phased implementation of **the running man**, a dev observability tool that captures logs, traces, and errors from local development environments. The tool wraps processes, tails Docker logs, captures browser console output, collects OTEL spans, and exposes a unified query API for both developers and AI agents.

**Target Stack:** Go (backend) + tiny JS SDK (browser capture)  
**Deployment:** Local development only (not production)  
**Timeline:** 5 phases, MVP achievable in Phase 1-2

---

## Phase 1: Core Foundation (MVP)

**Goal:** Prove the core value proposition with minimal features

### Deliverables

1. **Single Process Wrapper**
   - Spawn child process with inherited environment
   - Capture stdout/stderr streams
   - Pass-through to terminal (dev still sees logs)
   - Tag entries with process name and timestamp
   - Graceful signal handling (SIGINT/SIGTERM)

2. **Log Parsing Engine**
   - Python traceback detection and grouping
   - JSON log parsing with field extraction
   - Plain text with heuristic level detection (ERROR, WARN, INFO)
   - Multi-line error aggregation

3. **In-Memory Ring Buffer**
   - Fixed size/time retention (default 30min or 50MB)
   - Efficient append and query operations
   - Thread-safe with mutex protection
   - Basic indexing by timestamp and level

4. **Query API - Core Endpoints**
   ```
   GET /logs
     ?since=30s
     ?level=error,warn
     ?contains=text
   
   GET /errors
     ?since=5m
   
   GET /health
   ```

5. **CLI Interface**
   ```bash
   running-man run -- python server.py
   running-man run --api-port 9000 -- npm run dev
   ```

### Success Criteria

- Can wrap a single Python/Node process
- Captures and parses errors correctly
- Agent can query recent errors via API
- Zero-config startup for simple cases

### Estimated Effort: 2-3 weeks

---

## Phase 2: Multi-Source Capture

**Goal:** Support real-world dev stacks with multiple processes and containers

### Deliverables

1. **Multi-Process Support**
   - Multiple `--process` flags
   - Unique source tagging per process
   - Parallel stream capture
   - Aggregate output in terminal

2. **Docker Compose Integration**
   - Parse docker-compose.yml to discover containers
   - Attach to container log streams via Docker API
   - Tag entries with container name
   - Handle container restart/recreation
   - Filter by container in API queries

3. **Enhanced Query Filters**
   ```
   GET /logs
     ?source=python-*        # glob matching
     ?source=postgres        # exact match
     ?exclude=debug-*        # inverse filter
   
   GET /health
     # Shows status of all capture sources
   ```

4. **Configuration File Support** (optional)
   ```yaml
   # running-man.yml
   processes:
     - name: backend
       command: python server.py
       env:
         DEBUG: "1"
     - name: frontend
       command: npm run dev
   
   docker_compose: ./docker-compose.yml
   api_port: 9000
   retention: 30m
   ```

### Success Criteria

- Can wrap multiple processes simultaneously
- Captures logs from Docker containers
- Source filtering works reliably
- Survives container restarts

### Estimated Effort: 2 weeks

---

## Phase 3: Browser Log Capture

**Goal:** Capture browser console logs, errors, and network failures

### Deliverables

1. **Browser SDK (JavaScript)**
   - Console hook (`console.log/warn/error/debug`)
   - Uncaught exception handler (`window.onerror`)
   - Unhandled promise rejection handler
   - Optional `fetch`/`XHR` interceptor for failed requests
   - Auto-batching (configurable: N entries or M milliseconds)
   - Configurable endpoint URL (default `localhost:9000`)
   - Trace ID propagation support
   - Minimal footprint (~2KB minified)

2. **Ingest Endpoint**
   ```
   POST /ingest/browser
   ```
   - Accepts batched log entries from browser SDK
   - Parses and stores in ring buffer
   - Tags with source="browser"
   - Extracts trace_id for correlation

3. **Browser Entry Schema**
   ```
   BrowserEntry {
     timestamp: time
     type: "console" | "network" | "error"
     level: "log" | "warn" | "error" | "debug"
     message: string
     stack: string?        // for errors
     url: string          // page URL
     trace_id: string?    // correlation
     // network-specific
     method: string?      // POST, GET, etc.
     request_url: string? // API endpoint
     status: int?         // HTTP status
   }
   ```

4. **Browser Query API**
   ```
   GET /browser
     ?since=30s
     ?level=error,warn
     ?type=console,network,error
     ?trace_id=abc123
     ?url=*dashboard*     // filter by page URL
   ```

5. **SDK Integration Docs**
   - Installation via npm or CDN
   - Dev-only loading pattern (Vite, webpack, etc.)
   - Configuration options
   - Trace ID propagation examples
   - Framework-specific guides (React, Vue, Svelte)

6. **SDK Distribution**
   - npm package: `@running-man/browser-client`
   - Standalone file for copy/paste
   - TypeScript types included

### Success Criteria

- Browser SDK captures console logs and errors
- Errors include stack traces
- Failed network requests captured (if enabled)
- Trace ID correlation works browser → backend
- SDK has minimal performance impact (<5ms per batch)
- Works in Chrome, Firefox, Safari

### Estimated Effort: 2-3 weeks

---

## Phase 4: OTEL Integration

**Goal:** Add distributed tracing support for instrumented applications

### Deliverables

1. **OTLP Receiver**
   - gRPC endpoint (default :4317)
   - HTTP endpoint (default :4318)
   - Span ingestion and parsing
   - Extract trace_id, span_id, parent_span_id, attributes

2. **Span Storage**
   - Store spans in ring buffer with trace indexing
   - Support nested span relationships
   - Extract workflow_id from custom attributes
   - Link spans to log entries via trace_id

3. **Trace Query API**
   ```
   GET /traces
     ?trace_id=abc123        # specific trace
     ?workflow_id=xyz        # all spans for workflow
     ?since=5m              # time window
     ?status=error          # only errors
     ?min_duration=100ms    # slow spans
   ```

4. **Trace Correlation**
   - Match log entries to spans via trace_id
   - Return correlated logs with trace queries
   - Support custom attributes for workflow tracking

5. **Minimal Instrumentation Guide**
   - Python SDK setup (OpenTelemetry)
   - Node.js SDK setup
   - Header-based trace propagation (X-Trace-ID)
   - Example: FastAPI/Flask middleware
   - Example: Express middleware

### Success Criteria

- Receives OTLP spans from instrumented apps
- Can query full trace trees
- Correlates logs with traces
- Workflow ID tracking works for multi-step flows

### Estimated Effort: 3 weeks

---

## Phase 5: Polish & Production-Ready

**Goal:** Improve UX, add nice-to-have features, make it production-quality

### Deliverables

1. **WebSocket Streaming**
   ```
   WS /stream
     ?sources=backend,frontend
     ?levels=error,warn
   ```
   - Live tail for real-time debugging
   - Efficient binary protocol or JSON lines
   - Reconnect handling

2. **SQLite Persistence** (optional flag)
   ```bash
   running-man run --persist ./running-man.db -- python server.py
   ```
   - Survives restarts
   - Query historical data across sessions
   - Configurable retention policy

3. **Enhanced Error Detection**
   - JavaScript/TypeScript stack traces
   - Go panic detection
   - Rust panic detection
   - SQL error patterns
   - Custom regex patterns via config

4. **Agent Integration Helpers**
   - Standardized "context dump" endpoint
   - GET /context?scenario=startup-failure
   - Returns everything relevant for common debugging scenarios
   - Markdown-formatted output option for LLM consumption

5. **Observability & Debugging**
   - Prometheus metrics endpoint
   - Internal diagnostics (buffer usage, drop rate)
   - Verbose logging mode for troubleshooting the running man itself

6. **Cross-Platform Support**
   - Test on macOS, Linux, Windows
   - Handle platform-specific process signals
   - Docker compatibility on all platforms

7. **Documentation & Examples**
   - Quick start guide
   - Integration examples (Django, FastAPI, Express, Next.js)
   - Agent query patterns
   - Troubleshooting guide

### Success Criteria

- Can stream logs in real-time
- Persistence works reliably
- Detects errors in major languages
- Full documentation and examples
- Works on all major platforms

### Estimated Effort: 3-4 weeks

---

## Technology Choices

### Core Stack

- **Language:** Go
  - Fast, easy concurrency (goroutines for stream capture)
  - Simple cross-compilation for macOS/Linux/Windows
  - Good libraries for process management, HTTP, Docker

- **HTTP Framework:** chi or standard library
  - Lightweight, minimal dependencies
  - Good enough for local dev tool

- **OTEL:** go.opentelemetry.io/collector components
  - Official OTLP receiver implementation
  - Or build minimal custom receiver (fewer deps)

- **Docker:** docker/client library
  - Official Go client for Docker API

- **Browser SDK:** Vanilla JavaScript
  - No framework dependencies
  - TypeScript for development, compile to ES5
  - Rollup/esbuild for minification

- **Storage:** 
  - Phase 1-4: In-memory (maps + sync.RWMutex)
  - Phase 5: Optional SQLite via mattn/go-sqlite3

### Dependencies

Keep minimal for fast startup and simple distribution:
- Docker client (only if --docker-compose used)
- OTEL collector (only if OTEL enabled)
- SQLite (only if --persist used)

---

## Open Questions & Decisions Needed

### 1. Process Management Philosophy

**Option A:** Passive observer only
- The running man captures logs but doesn't restart processes
- Dev uses their own autoreload tools (nodemon, watchdog, etc.)
- Simpler implementation, clear responsibilities

**Option B:** Active process supervisor
- Can restart on crash
- Configurable restart policies
- Health checks and alerts
- More complex but more integrated

**Recommendation:** Start with A (passive), add B in Phase 5 if needed

### 2. Browser SDK Distribution

**Option A:** npm package only
- Install via `npm install @running-man/browser-client`
- Standard dependency management
- Easier updates

**Option B:** Single file for copy/paste
- No npm dependency
- Drop file into project
- Simpler for quick testing

**Option C:** Both
- npm for most users
- Standalone file for demos/testing

**Recommendation:** C (both) - implemented in Phase 3

### 3. Configuration Complexity

**Option A:** CLI flags only
- Simple for common cases
- Can be verbose for complex setups

**Option B:** YAML config file support
- Better for multi-process setups
- Shareable across team
- Environment variable substitution

**Recommendation:** A for Phase 1-2, add B in Phase 3-4

### 4. API Design Philosophy

**Option A:** Flexible query API
- Many filter parameters
- Devs/agents compose queries as needed
- More powerful but requires learning

**Option B:** Scenario-based endpoints
- `/context/startup-failure`
- `/context/request-trace?trace_id=X`
- Easier for common cases, less flexible

**Recommendation:** A as base, add B shortcuts in Phase 5

### 5. Source Maps for Browser SDK

**Option A:** No source map support
- Show original minified stack traces
- Simpler implementation
- Less accurate for production builds

**Option B:** Automatic source map resolution
- Fetch and parse source maps
- Show original file/line numbers
- Better debugging experience
- More complex implementation

**Recommendation:** A for Phase 3, consider B for Phase 5 if needed

---

## Risk Mitigation

### Performance Impact

**Risk:** Capturing logs adds overhead to dev environment

**Mitigation:**
- Use buffered I/O for minimal latency
- Async processing (goroutines)
- Configurable buffer sizes
- Drop old entries if buffer full (don't block processes)

### Memory Usage

**Risk:** Long-running dev sessions fill memory

**Mitigation:**
- Fixed ring buffer size (default 50MB)
- Time-based eviction (default 30min)
- Expose /health with memory usage stats
- Optional disk persistence for longer retention

### Platform Compatibility

**Risk:** Process management differs on Windows

**Mitigation:**
- Abstract platform-specific code early
- Test on all platforms in CI
- Clear docs on platform limitations

### Docker Dependency

**Risk:** Not all devs use Docker

**Mitigation:**
- Make Docker support optional
- Clear error if --docker-compose used without Docker
- Tool works fine without Docker features

### Browser SDK Performance

**Risk:** SDK adds overhead to page load and runtime

**Mitigation:**
- Async loading (doesn't block rendering)
- Batching reduces network overhead
- Configurable sampling for high-traffic scenarios
- Dev-only by default (tree-shake for production)

---

## Success Metrics

### Phase 1 (MVP)
- [ ] Successful single-process wrapping
- [ ] 90%+ accuracy on Python traceback detection
- [ ] API responds in <100ms for typical queries
- [ ] Zero-config startup works

### Phase 2
- [ ] Can handle 5+ simultaneous processes
- [ ] Docker log capture with <5s lag
- [ ] Source filtering works correctly

### Phase 3
- [ ] Browser SDK captures console logs and errors
- [ ] SDK has <5ms overhead per batch
- [ ] Trace ID propagation works browser → backend
- [ ] Works in Chrome, Firefox, Safari

### Phase 4
- [ ] Receives and stores OTLP spans
- [ ] Trace query latency <200ms
- [ ] Correlation works across browser/logs/traces

### Phase 5
- [ ] Works on macOS, Linux, Windows
- [ ] 95%+ error detection across major languages
- [ ] Full documentation with 5+ examples
- [ ] Agent integration helpers tested with real agents

---

## Future Possibilities (Beyond Phase 5)

- **Remote deployment:** Run on remote dev server, query from local
- **Team collaboration:** Share data across team members
- **Snapshot/replay:** Capture and replay specific scenarios
- **Integration with IDEs:** VS Code extension for inline log queries
- **ML-powered error grouping:** Cluster similar errors automatically
- **Performance profiling:** CPU/memory traces alongside logs
- **Log analysis:** Suggest likely root causes based on patterns
- **Browser performance metrics:** Web Vitals, long tasks, etc.

---

## Getting Started

### Immediate Next Steps

1. **Setup project structure**
   ```
   the-running-man/
     cmd/running-man/main.go
     internal/
       wrapper/     # process wrapper
       parser/      # log parsing
       storage/     # ring buffer
       api/         # HTTP handlers
     browser-sdk/
       src/         # TypeScript source
       dist/        # Built JS files
     go.mod
     package.json   # For browser SDK build
   ```

2. **Prototype process wrapper** (Phase 1)
   - Basic exec with stdout/stderr capture
   - Terminal passthrough
   - Signal handling

3. **Build ring buffer** (Phase 1)
   - In-memory circular buffer
   - Time and size limits
   - Concurrent access

4. **Implement /logs endpoint** (Phase 1)
   - Basic filtering
   - JSON response format

5. **Add Python traceback parser** (Phase 1)
   - Regex patterns
   - Multi-line grouping

### Repository Setup

- [ ] Initialize Go module
- [ ] Initialize npm package for browser SDK
- [ ] Setup CI/CD (GitHub Actions)
  - Lint (golangci-lint for Go, eslint for JS)
  - Test (Go tests, browser SDK tests)
  - Build for multiple platforms (Go binaries)
  - Build and publish npm package
- [ ] Add README with quick start
- [ ] Create CONTRIBUTING.md
- [ ] Add LICENSE (MIT or Apache 2.0)

---

## Questions for Discussion

1. **Distribution:** Brew/apt/binary releases, or go install? npm global install?
2. **Target users:** Solo devs, teams, or both? Affects features.
3. **Agent integration:** Should we build for specific agents (Claude, Cursor) or generic?
4. **Pricing/model:** Open source? Commercial? Freemium?
5. **Browser SDK scope:** Just errors/logs or also performance metrics (Web Vitals)?

---

## Conclusion

This plan provides a clear path from MVP to production-quality tool. Phase 1-2 delivers immediate value for backend debugging. Phase 3 adds browser observability, completing full-stack coverage. Phase 4 adds powerful distributed tracing. Phase 5 polishes the experience and ensures reliability.

The key insight: by running outside the application and capturing browser logs too, **the running man** remains available even when the app crashes, providing complete visibility across the entire stack. This makes it invaluable for the hardest debugging scenarios in modern web development.

**Estimated total effort:** 13-16 weeks for full Phase 1-5 implementation (single developer)

**MVP timeline:** 4-5 weeks for Phase 1-2 (immediately useful for backend)

**Full-stack observability:** 6-8 weeks for Phase 1-3 (browser + backend)
