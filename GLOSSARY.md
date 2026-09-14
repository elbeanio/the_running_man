# Glossary

The locked vocabulary for The Running Man — the words used in the config, the code, the
TUI, the API and the docs. One word per concept, one concept per word.

If you need a name for something new, check here first and add it here when it ships.

---

The Running Man is a per-project process runner with a memory. You describe your
project's **managed processes** in `running-man.yml`, start an **instance**, and it
spawns them and captures every line they emit — along with **container** logs and OTLP
**spans** — into a single **ring buffer**. Every producer is a **source**; the **TUI**
gives you a tab per source, and the **REST API** serves the same data to a coding agent,
so the developer and the agent are looking at the same run.

---

## The tool and its lifetime

- **The Running Man** — the project. Written `running-man` as a command, binary or config
  filename; "The Running Man" in prose. Never "Runner" or "RM".
- **Instance** — one Running Man process supervising one project directory. Started by
  `running-man run`. The unit that owns the **ring buffer**, the **REST API** and the
  **OTLP receiver**.
  *("Session" is reserved — see below. An instance is not a session.)*
- **Session** — **reserved, and not used for Running Man's own lifetime.** In this project
  "session" means either a coding agent's conversation, or a POSIX session
  (`Setsid` in `process/wrapper.go`, used to kill whole process trees). Use **instance**
  for a Running Man run.
- **Instance marker** — `.running-man/instance.json`, written while an instance is live so
  an agent can cheaply discover it and see what is already running. Holds only **stable**
  facts (API URL, pid, configured processes, curl hints); live state comes from
  `/processes`, so the marker cannot drift out of date about status or ports. Removed on
  clean exit; if it exists but `/health` does not answer, it is stale and can be ignored.

## Running things

- **Managed process** — a process Running Man started from a `processes:` entry and can
  restart. Distinct from a **container**, which Running Man attaches to but never starts.
- **Recurring process** — a **managed process** with an `interval`, re-run on a timer
  rather than supervised as long-lived. Recurrence is determined **only** by `interval`
  being set.
- **Process type** — the `type:` field (`web`, `api`, `worker`, `database`, `cache`).
  **Descriptive metadata only** — it is exposed on the API for humans and agents to read
  and never changes behaviour. `type: recurring` is not a thing; see **recurring process**.
- **Observed port** — a TCP port a **managed process** (or any of its descendants) is
  listening on, reported in `/processes`. *Observed* because nothing in the config records
  a port: it is read from the OS, so it is a strong hint rather than a guarantee — a
  process that has just started may not have bound yet.
- **Status** — a managed process's state: `running`, `stopped`, `failed` or `waiting`.
  Carries an **exit code** (`-1` while running). **Waiting** applies only to a **recurring
  process** between runs whose last run succeeded — distinct from `stopped`, which would
  read as "down", and from `running`, which would be untrue.
- **Restart on crash** — the `restart_on_crash` flag: respawn a managed process when it
  exits non-zero. Not used for **recurring processes**, which re-run on schedule anyway.
- **Wrapper** — internal: the object owning one managed process's OS handle and output
  capture (`ProcessWrapper`). Not a user-facing term.

## Capture

- **Source** — anything that feeds lines or spans into the **ring buffer**. Every entry
  has a source, identified by name. Sources are what the **TUI** tabs between.
- **Source type** — what kind of producer a source is: `process`, `docker`, `system`,
  `traces` or `otlp`. Distinct from **process type**, which is free-text metadata about a
  managed process; source type is a closed set used by the buffer and the TUI.
- **OTLP source** — log records POSTed to the OTLP receiver's `/v1/logs`, typically from a
  browser, which has no stdout to capture. Distinct from a **managed process**: the source
  name and timestamp come from the sender, so an `otlp` entry is *not* evidence of its own
  origin. Grouped with processes in the **TUI**, being application output either way.
- **System source** — the source named for Running Man's own output. Its own logs are
  captured alongside everything else, so a startup failure is visible in the same place.
- **Traces source** — spans arriving over OTLP, treated as a data source for the ring
  buffer like any other. Traces are a source even though they are not log lines.

## Log data

- **Log entry** — one captured line, parsed: **timestamp**, **level**, **source**, source
  type, **message**, **raw**, optional **stacktrace** and **trace ID**.
- **Level** — `debug`, `info`, `warn` or `error`. **The single authority on whether an
  entry is an error**; `/errors` selects on it. A stack trace found at a lower level
  promotes the entry to `error`.
  *(A legacy `IsError` flag on `LogEntry` duplicates this and is being retired.)*
- **Message** — the human-readable content of an entry, after parsing. Distinct from
  **raw**, which is the unmodified line as captured; for a Python traceback the message is
  the summary and the raw is the whole trace.
- **Stacktrace** — a multi-line trace grouped onto a single entry rather than scattered
  across many. Python tracebacks and JSON `stack`/`stacktrace` fields both land here.
- **Parser** — the component deciding an entry's shape. Three exist: Python traceback,
  JSON, and plain text (which infers **level** heuristically).

## Storage

- **Ring buffer** — the single in-memory circular store holding every entry from every
  **source**. Oldest entries are evicted first. There is no database and nothing survives
  an instance exiting; the buffer outliving a *crashed app* is the point.
- **Retention limits** — collectively, the bounds on what the ring buffer keeps: **max
  age**, **max entries**, **max bytes**. Configured under `limits:`. Use "retention
  limits" for the policy and the specific limit name for one of them — never "retention"
  on its own for either.
- **Max age / max entries / max bytes** — the three retention limits: how old, how many,
  how large. Whichever binds first wins.

## Tracing

- **Span** — one OpenTelemetry operation: name, service, start, duration, status,
  attributes. The unit the OTLP receiver stores.
- **Trace** — a set of **spans** sharing a **trace ID**, forming one distributed
  operation. A trace is never stored as an object; it is spans grouped by ID.
- **OTLP receiver** — the endpoint accepting spans over OTLP/HTTP (default port 4318).
  Running Man injects the matching `OTEL_*` variables into **managed processes**, so an
  instrumented app finds it without configuration.
- **Trace ID** — the correlation key. Present on **spans** and, when the app emits it, on
  **log entries** too — which is what makes "show me the logs for this trace" possible.
- **Service name** — the OTEL service a span came from. Related to but not the same as a
  **source** name: one managed process may report a different service name.

## Docker

- **Compose file** — the `docker-compose.yml` Running Man reads to discover what to watch.
- **Compose service** — a service defined in the compose file. Used to *find* containers;
  it is not itself a **source**.
- **Container** — a running Docker container Running Man attaches to. A container is a
  **source**, named by its **container name** (not its compose service name). Running Man
  never starts or stops containers — contrast **managed process**.

## Interfaces

- **REST API** — the query interface (default port 9000), and the agent-facing interface.
  Self-describing: `/` lists endpoints, `/docs` serves OpenAPI. The only programmatic
  path — there is deliberately no agent-facing CLI.
- **TUI** — the interactive terminal viewer, one tab per **source**, owned by the process
  that started the instance.
- **Skill** — the instructions telling a coding agent when and how to reach for Running
  Man. Its job is to answer "is what I'm about to start already running?" before it
  answers anything else.
- **Headless** — running with `--no-tui`: capture and API, no viewer. For CI and
  automation.

---

## Spelling and conventions

- **British spelling in prose, docs and this file** — "colour", "behaviour", "initialise".
- **American spelling in identifiers where a library forces it.** `lipgloss.Color` and
  friends mean `tui.go` is full of `color`; matching the library beats consistency with
  the docs, because half-anglicised identifiers are worse than either. Expect this
  discrepancy at the code/prose boundary — it is deliberate, not drift.
- **Singular for entity names** — `log entry`, `span`, `source`, `managed process`.
- **Internal names may differ from user-facing ones** where flagged (e.g. **wrapper**).
  Anything a user or agent reads uses the locked term.
