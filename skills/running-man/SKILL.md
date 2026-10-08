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
socket and every process the project is configured to run.

**Take the socket path from that file. Do not assume it.** It is usually
`.running-man/api.sock`, but not always, and the exception is not something you can work
out for yourself:

```bash
SOCK=$(sed -n 's/.*"socket": "\(.*\)",*/\1/p' .running-man/instance.json)
curl -s --unix-socket "$SOCK" http://localhost/processes
```

Every example below assumes `$SOCK` was set that way. The file's `hints` are also complete,
ready-to-run commands with the path already filled in, if you would rather copy one.

If the process you were about to start is already there with `"status": "running"`, **use
it**. Do not start a second copy.

If the file does not exist, or the socket does not respond, there is no instance — carry on
as normal and start your own processes.

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

**The API is a Unix socket, not a TCP port**, so `curl` needs `--unix-socket`. The host in
the URL is ignored; `localhost` is the convention. Every endpoint returns JSON.

`$SOCK` comes from the marker, as above. The reason to read it rather than hardcode
`.running-man/api.sock` is that a Unix socket path has a hard kernel length limit, and a
deeply nested project therefore gets a short socket under the temp directory instead —
named by a hash of the project path, which you cannot reconstruct and should not try to.
The marker records whichever applies.

The socket is why there is no port to guess and no wrong project to reach by accident: the
path derives from the project directory, so an agent working in one project cannot read
another's logs.

```bash
# What went wrong recently, across everything
curl -s --unix-socket "$SOCK" 'http://localhost/errors?since=10m&limit=50'

# One process
curl -s --unix-socket "$SOCK" 'http://localhost/logs?source=backend&since=5m&limit=50'

# Search for something specific
curl -s --unix-socket "$SOCK" 'http://localhost/logs?contains=connection%20refused&since=15m'

# Several processes, or a glob
curl -s --unix-socket "$SOCK" 'http://localhost/logs?source=api,worker&since=5m'
curl -s --unix-socket "$SOCK" 'http://localhost/logs?source=app-*&since=5m'

# Only errors and warnings
curl -s --unix-socket "$SOCK" 'http://localhost/logs?level=error,warn&since=10m'
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
curl -s --unix-socket "$SOCK" 'http://localhost/logs?level=warn,error&since=10m&limit=50'
```

Entries are snake_case: `timestamp`, `level`, `source`, `source_type`, `message`, `raw`,
`stacktrace`, `trace_id`. `level` is the only error signal: `/errors` returns exactly the
`error` entries. Python tracebacks arrive as **one** entry with the
whole trace in `stacktrace`, so you do not have to stitch lines together.

## Restarting after a change

You do not need to stop and re-start the stack yourself:

```bash
curl -s -X POST --unix-socket "$SOCK" http://localhost/processes/backend/restart
```

No permission check is needed any more: reaching the socket at all means being able to open
a file in the project directory, which is the same thing as being on this machine as this
user. The old loopback-only 403 went away with the TCP port.

## When something is wrong

1. `curl -s --unix-socket "$SOCK" 'http://localhost/errors?since=10m&limit=50'` — what has actually failed.
2. `curl -s --unix-socket "$SOCK" http://localhost/processes` — is anything not `running`? Check
   `exit_code`. For recurring processes, `waiting` is healthy (between runs); `failed`
   is not. With `depends_on` configured there are three more: `pending` and `starting`
   mean startup is still under way -- the process is coming, so do not start your own
   copy -- and `blocked` means it never will, because a dependency failed:
   `startup_error` says which, and the `dependencies` list shows Compose services. The
   dependency's own logs are the explanation; startup does not retry until Running Man is
   run again.
3. `curl -s --unix-socket "$SOCK" 'http://localhost/logs?source=NAME&since=5m'` — the full context from
   whichever process looks implicated.
4. If an entry has a `trace_id`, get everything correlated with it:
   `curl -s --unix-socket "$SOCK" http://localhost/traces/TRACE_ID/logs`

## Traces

Present when the app is OTEL-instrumented; Running Man injects the exporter configuration
into the processes it starts.

```bash
curl -s --unix-socket "$SOCK" 'http://localhost/traces?since=10m'
curl -s --unix-socket "$SOCK" 'http://localhost/traces?status=error&since=10m'
curl -s --unix-socket "$SOCK" http://localhost/traces/TRACE_ID          # every span
curl -s --unix-socket "$SOCK" http://localhost/traces/TRACE_ID/logs     # correlated log entries
```

## Notes

- **Discovering the API:** `curl -s --unix-socket "$SOCK" http://localhost/` lists every
  endpoint with a description, and `/openapi.yaml` serves the full specification.
- **Do not start Running Man yourself** unless asked. The developer normally starts it so
  they get the TUI; starting it yourself takes that away from them.
- **A stale marker is possible.** If `.running-man/instance.json` exists but
  `curl -s --unix-socket "$SOCK" http://localhost/health` fails, the instance died without
  cleaning up. Ignore the file.
- **`/health` proves which instance answered.** It returns `pid`, `project` and `started`.
  If `project` is not the directory you are working in, you are talking to something else —
  stop and re-read the marker.
- **Ports in `/processes` are observed**, not configured, so they are a strong hint rather
  than a guarantee. A process that has just started may not have bound yet.
- **`otlp` sources are not evidence of origin.** Anything that can reach the OTLP receiver
  can post log records and choose its own source name.
