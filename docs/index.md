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
curl -s 'http://localhost:9000/errors?since=10m'
curl -s 'http://localhost:9000/logs?source=backend&since=5m&limit=50'
curl -s http://localhost:9000/processes
```

`GET /` lists every endpoint; `/docs` serves interactive OpenAPI documentation.

## ⚠️ Read this before running it on a shared network

Running Man binds **all interfaces** by default and has **no authentication**, so anyone
who can reach port 9000 can read your captured logs — and dev servers routinely print
tokens and connection strings. Process control is restricted to this machine.

`running-man run --listen 127.0.0.1` restricts everything to the local machine. Full
detail in [network exposure](api-reference.md#network-exposure).

---

*Also in the repository, for people and agents working **on** Running Man rather than with
it: [PROJECT.md](https://github.com/elbeanio/the_running_man/blob/main/PROJECT.md) (purpose,
constraints, non-goals),
[GLOSSARY.md](https://github.com/elbeanio/the_running_man/blob/main/GLOSSARY.md) (locked
vocabulary) and
[AGENTS.md](https://github.com/elbeanio/the_running_man/blob/main/AGENTS.md).*

*[Project history](implementation-history.md) records the development phases, including the
MCP server that was built and then removed.*
