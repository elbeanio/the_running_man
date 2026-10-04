# The Running Man

**Home** · [Overview](overview.md) · [Getting started](getting-started.md) · [Configuration](configuration.md) · [API reference](api-reference.md) · [Agent integration](agent-integration.md) · [Tracing](tracing.md) · [Architecture](architecture.md) · [Troubleshooting](troubleshooting.md) · [Development](development.md)

---

**Run your project's processes, capture everything they emit, and make it equally available
to you and to your coding agent.**

When a coding agent starts its own dev server and reads its own output, it works fine — and
you are blind. You cannot see the stack trace it is about to spend five minutes on, so you
cannot tell it that you recognise the problem.

Running Man keeps one copy of your stack running, captures everything it emits, and serves
that to both of you over the same API.

## The pages

| | |
|---|---|
| [Overview](overview.md) | What it is, what problem it solves, what it deliberately is not |
| [Getting started](getting-started.md) | Install, first run, first queries |
| [Configuration](configuration.md) | Every `running-man.yml` key and every CLI flag |
| [API reference](api-reference.md) | Endpoints, parameters, response shapes, [network exposure](api-reference.md#network-exposure) |
| [Agent integration](agent-integration.md) | The instance marker and the skill |
| [Tracing](tracing.md) | OpenTelemetry setup and trace/log correlation |
| [Architecture](architecture.md) | Components and how data flows between them |
| [Troubleshooting](troubleshooting.md) | Symptoms, causes, fixes |
| [Development](development.md) | Building, testing, contributing |

## In 60 seconds

```bash
go install github.com/elbeanio/the_running_man/cmd/running-man@latest

# One process — the TUI launches automatically
running-man run --process "python server.py"

# Several — Tab between them
running-man run --process "python server.py" --process "npm run dev"
```

Then ask it what happened:

```bash
SOCK=.running-man/api.sock
curl -s --unix-socket "$SOCK" 'http://localhost/errors?since=10m'
curl -s --unix-socket "$SOCK" 'http://localhost/logs?source=backend&since=5m&limit=50'
curl -s --unix-socket "$SOCK" http://localhost/processes
```

`GET /` lists every endpoint; `/docs` serves interactive OpenAPI documentation.

## ⚠️ Read this before running it on a shared network

The query API is a Unix socket at `.running-man/api.sock`, mode 0600, so it is not on the
network at all. The OTLP receiver still is — it binds 4318 (or the next free port) so
containers and browsers can export to it — and it is unauthenticated, write-only ingest.
Full detail in [network exposure](api-reference.md#network-exposure).

---

*Also in the repository, for people and agents working **on** Running Man rather than with
it: [PROJECT.md](https://github.com/elbeanio/the_running_man/blob/main/PROJECT.md) (purpose,
constraints, non-goals),
[GLOSSARY.md](https://github.com/elbeanio/the_running_man/blob/main/GLOSSARY.md) (locked
vocabulary) and
[AGENTS.md](https://github.com/elbeanio/the_running_man/blob/main/AGENTS.md).*

*[Project history](implementation-history.md) records the development phases, including the
MCP server that was built and then removed.*
