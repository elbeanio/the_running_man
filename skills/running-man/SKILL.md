---
name: running-man
description: Use a running instance of The Running Man to reuse already-running dev servers instead of starting your own, and to read the logs, errors and traces of every process in the project. Check this before starting any long-running process (dev server, watcher, test runner) or when you need output from one that is already running.
license: MIT
metadata:
  audience: developers
  category: development
  tool: the-running-man
---

## Check this first

**Before you start a dev server, watcher, or any long-running process, check whether it is
already running.**

```bash
cat .running-man/instance.json 2>/dev/null
```

If that file exists, a Running Man instance is supervising this project. It lists the API
URL and every process the project is configured to run. For live state:

```bash
curl -s http://localhost:9000/processes
```

If the process you were about to start is already there with `"status": "running"`, **use
it**. Do not start a second copy.

If the file does not exist, or the API does not respond, there is no instance — carry on as
normal and start your own processes.

## Why this matters

Starting a second copy of something already running goes wrong in ways that are slow to
diagnose:

- It collides on the port, or silently binds a different one, so you end up testing a
  different server than the one that is actually serving.
- Its output goes only to you. The developer watching the project cannot see it, so they
  cannot tell you that the error you are about to spend five minutes on is one they
  recognise instantly.

Using the instance that is already there is also less work: the service is already up (no
waiting for a boot), its history is already captured (no re-running to see what happened),
and you can search across every process at once.

## Reading output

The API is on port 9000 unless `.running-man/instance.json` says otherwise. Every endpoint
returns JSON.

```bash
# What went wrong recently, across everything
curl -s 'http://localhost:9000/errors?since=10m&limit=50'

# One process
curl -s 'http://localhost:9000/logs?source=backend&since=5m&limit=50'

# Search for something specific
curl -s 'http://localhost:9000/logs?contains=connection%20refused&since=15m'

# Several processes, or a glob
curl -s 'http://localhost:9000/logs?source=api,worker&since=5m'
curl -s 'http://localhost:9000/logs?source=app-*&since=5m'

# Only errors and warnings
curl -s 'http://localhost:9000/logs?level=error,warn&since=10m'
```

Useful parameters on `/logs` and `/errors`: `since` (`30s`, `5m`, `1h`), `source`
(comma-separated, globs allowed), `level`, `contains`, `exclude`, `limit` (default 1000,
`limit=0` for everything).

**A process exiting non-zero is recorded by Running Man itself** — look for
`Process "name" failed: exited with code N` in `/errors`. So `/errors` tells you something
died even when the process's own output said nothing recognisable.

**Check `warn` when `/errors` looks thin.** Anything written to stderr that matched no known
error phrasing is recorded as `warn` rather than `info` — stderr is weak evidence on its
own, since plenty of tools write progress there, but it is still worth a look:

```bash
curl -s 'http://localhost:9000/logs?level=warn,error&since=10m&limit=50'
```

Entries are snake_case: `timestamp`, `level`, `source`, `source_type`, `message`, `raw`,
`is_error`, `stacktrace`, `trace_id`. Python tracebacks arrive as **one** entry with the
whole trace in `stacktrace`, so you do not have to stitch lines together.

## Restarting after a change

You do not need to stop and re-start the stack yourself:

```bash
curl -s -X POST http://localhost:9000/processes/backend/restart
```

This must be run **on the machine running Running Man**. It returns 403 from anywhere else,
with an explanation — that is deliberate, not a bug.

## When something is wrong

1. `curl -s 'http://localhost:9000/errors?since=10m&limit=50'` — what has actually failed.
2. `curl -s http://localhost:9000/processes` — is anything not `running`? Check
   `exit_code`. For recurring processes, `waiting` is healthy (between runs); `failed`
   is not.
3. `curl -s 'http://localhost:9000/logs?source=NAME&since=5m'` — the full context from
   whichever process looks implicated.
4. If an entry has a `trace_id`, get everything correlated with it:
   `curl -s http://localhost:9000/traces/TRACE_ID/logs`

## Traces

Present when the app is OTEL-instrumented; Running Man injects the exporter configuration
into the processes it starts.

```bash
curl -s 'http://localhost:9000/traces?since=10m'
curl -s 'http://localhost:9000/traces?status=error&since=10m'
curl -s http://localhost:9000/traces/TRACE_ID          # every span
curl -s http://localhost:9000/traces/TRACE_ID/logs     # correlated log entries
```

## Notes

- **Discovering the API:** `curl -s http://localhost:9000/` lists every endpoint;
  `http://localhost:9000/docs` serves interactive OpenAPI documentation.
- **Do not start Running Man yourself** unless asked. The developer normally starts it so
  they get the TUI; starting it yourself takes that away from them.
- **A stale marker is possible.** If `.running-man/instance.json` exists but
  `curl -s http://localhost:9000/health` fails, the instance died without cleaning up.
  Ignore the file.
- **Ports in `/processes` are observed**, not configured, so they are a strong hint rather
  than a guarantee. A process that has just started may not have bound yet.
- **`otlp` sources are not evidence of origin.** Anything that can reach the OTLP receiver
  can post log records and choose its own source name.
