# Agent Integration Guide

*This documentation will be completed during Phase 3 implementation.*

## Overview

The Running Man provides a skills-based API for AI coding assistants (Claude Code, OpenCode) to query logs, errors, and traces without manual copy-pasting.

Instead of you gathering context and pasting it into your agent, the agent can directly query Running Man for what it needs.

## Supported Agents

- **Claude Code** (primary)
- **OpenCode** (primary)
- Generic REST API for other tools

## Current API (Phase 2)

For now, agents can query the basic endpoints:

```bash
# Recent errors
curl http://localhost:9000/errors?since=30s

# Logs from a specific source
curl http://localhost:9000/logs?source=backend&since=5m

# Search for content
curl http://localhost:9000/logs?contains=database&level=error
```

See [api-reference.md](api-reference.md) for complete endpoint documentation.

## Coming in Phase 3

### Skills Framework

Agents will be able to invoke debugging "skills" - pre-built patterns for common tasks:

```bash
# Example: Get errors with surrounding context
POST /skills/recent_errors
{
  "since": "5m",
  "context_lines": 10
}
```

**Planned Skills:**
- `recent_errors` - Errors with surrounding log context
- `startup_failures` - Why didn't the server start?
- `trace_request` - Follow a request through the logs
- `container_issues` - Docker container problems

### Agent Endpoints

Higher-level endpoints designed for agent consumption:

```
GET /context/errors      - Recent errors + stack traces + context
GET /context/startup     - Logs from process startup
GET /context/source/{id} - Complete history for a source
```

### Integration Guides

Documentation for:
- How to use Running Man from Claude Code
- How to use Running Man from OpenCode
- Common debugging workflows
- Example prompts
- Creating custom skills

### MCP Server (if needed)

If skills + REST API aren't enough, we'll add an MCP (Model Context Protocol) server for deeper integration with Claude Code.

---

**Want to contribute to Phase 3 design?** Open an issue with your use case!
