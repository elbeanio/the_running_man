---
name: debug-logs
description: Debug applications using The Running Man's observability tools
license: MIT
compatibility: opencode
metadata:
  audience: developers
  category: debugging
  tool: the-running-man
---

## What I Do
I help you debug applications by searching logs, correlating traces, and managing processes using The Running Man.

## Quick Start
1. Ensure Running Man is running: `running-man run`
2. API available at: http://localhost:9000/
3. Interactive docs: http://localhost:9000/docs

## Common Debugging Workflows

### 1. When Users Report Issues
**Find recent errors:**
```bash
curl "http://localhost:9000/errors?since=10m"
```

**Search for specific error patterns:**
```bash
curl "http://localhost:9000/logs?contains='connection failed'&since=15m"
```

### 2. Cross-Source Investigation
**Search across all backend services:**
```bash
curl "http://localhost:9000/logs?source=app-*&since=5m"
```

**Compare multiple services:**
```bash
curl "http://localhost:9000/logs?source=api,worker,database&since=2m"
```

### 3. Trace-Log Correlation
**Find traces with errors:**
```bash
curl "http://localhost:9000/traces?status=error&since=5m"
```

**Get all logs for a specific trace:**
```bash
curl "http://localhost:9000/traces/{trace_id}/logs"
```

### 4. Process Management
**Check all processes:**
```bash
curl http://localhost:9000/processes
```

**Restart a failing process:**
```bash
curl -X POST http://localhost:9000/processes/{name}/restart
```

## Advanced Patterns

### Finding Related Actions Across Services
1. **Identify a trace ID** from error logs
2. **Get all spans** for that trace: `/traces/{id}`
3. **Find all logs** for the trace: `/traces/{id}/logs`
4. **Check each service** involved in the trace

### Performance Debugging
1. **Find slow traces**: `/traces?since=10m` (look for long durations)
2. **Examine span hierarchy** to identify bottlenecks
3. **Check logs** during slow periods

## API Reference
- **Root endpoint**: `/` - Lists all available endpoints
- **OpenAPI docs**: `/docs` - Interactive API documentation
- **Health check**: `/health` - System status and buffer stats

## Tips
- Use glob patterns (`app-*`) to search across related services
- The `since` parameter accepts durations like "30s", "5m", "1h"
- Logs automatically include trace IDs when available
- MCP tools available at `/mcp` for OpenCode/Claude Code