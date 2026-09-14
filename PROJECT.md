# The Running Man — project document

Background for every session. Standing facts, constraints and vocabulary live here.
Ordered work lives in `plans/` (untracked — see "Planning" below).

## What this is for

Make it easy to run a project with several component parts, capture everything they
emit, and make that capture equally available to the developer and to a coding agent
working on the project.

The point is **keeping the human in the loop on agent-driven development**. When an
agent starts its own dev server and reads its own output, it works fine — but the
developer is blind — and an experienced developer watching a run go wrong is the cheapest
debugging resource available. Seeing it early and saying "that's the wrong approach" saves
minutes of reasoning churn. That saving is the product.

**North star:** both the developer and the agent find it useful enough that the idea
of losing it is annoying. A strong signal would be an agent starting Running Man
unprompted because it is the better option.

## Governing principle

**Everything done to make this better for an agent also makes it better for the human
collaborator.** The two audiences do not trade off against each other, so no design
decision needs to balance them. If a change only helps one, look again — it is
probably the wrong change.

## History, and the lesson from it

Built over several phases through early 2026: process capture, Docker Compose
integration, a TUI, an MCP server, an OTLP receiver. Then it fell out of use.

The original motivating bug: an agent would start its own server, hit a port conflict
on `:8000`, and kill the developer's running process to free the port. **That bug is
gone** — current models detect the conflict and choose another port. But the fix made
the underlying problem worse, because the agent's process is now somewhere the
developer cannot see at all.

Running Man went unused because **nothing cheaply told the agent it existed and covered
its need**. The expected cost of investigating an unknown tool exceeded the known cost
of `npm run dev 2>&1 | tail -50`. This is an information problem.

Two readings that were considered and rejected:

- *The agent refused to cooperate, so adoption must be enforced.* Wrong. An agent is a
  product designed to do as asked; routing around a tool is a verdict on the tool, not
  a behaviour to correct with instructions or coercion. Do not design around compliance.
- *The transport was wrong (MCP vs a skill).* Also wrong. The REST API is good and
  self-describing, and agents handle curl well. A different transport fixes nothing.

## Constraints

- **Per-project, not machine-wide.** Running Man is a process runner scoped to one
  project directory. Anything wanting a machine-wide view of many projects is a
  different product.
- **In-memory only.** Ring buffer, 30 minutes or 50MB by default. No database, no
  persistence across restarts. The buffer outliving a crashed app is the feature.
- **The developer must not lose visibility as a side effect of how a session starts.**
  Today this means the developer normally starts Running Man, since the TUI belongs to
  the starting process.
- **The agent must be able to fall back.** If Running Man is not running, the agent
  should get on with starting its own processes. It should never be blocked.
- **Tests stay fast.** The core suite runs in a second or two. Anything slower is
  tagged and run only before review.

## Non-goals

- **Intervention.** Running Man does not interject into the agent's session, inject
  context, or relay annotated log lines. The developer reads, then types in their own
  chat window. Visibility is the whole scope.
- **Production observability.** This is a local development tool. Not a monitoring
  product, not a hosted service, no multi-user concerns.
- **Persistence or long-term analytics.** No historical storage, no trend analysis.
- **MCP.** Removed deliberately (see phases). The scope was wrong: MCP wants a central
  dispatch server and this is a per-project runner. If MCP returns it will be against a
  differently shaped product.
- **A second way to do the same thing.** An agent-facing CLI was considered and
  rejected because the REST API already covers it. Prefer one good path.

## Architecture

```
cmd/running-man/        CLI entry point (run, tui, version, help) and the TUI
internal/process/       Process spawning, output capture, restart, recurring processes
internal/parser/        Log format detection (Python tracebacks, JSON, plain text)
internal/storage/       Ring buffer
internal/docker/        Docker Compose integration
internal/tracing/       OTLP receiver
internal/api/           REST query endpoints
internal/config/        YAML config and auto-discovery
```

Capture sources (processes, Docker containers, OTLP spans) all land in the ring buffer.
The REST API and the TUI are both readers of that buffer.

**The REST API is the agent-facing interface.** It is self-describing: `/` lists the
endpoints and `/docs` serves OpenAPI. `/logs` takes `since`, `level`, `source`,
`contains`, `exclude`, `limit`; there are also `/errors`, `/health`, `/processes`,
`/processes/{name}`, `/processes/{name}/restart`, `/processes/stop-all`, `/traces` with
filters, and `/traces/{id}/logs`. Default port 9000; OTLP on 4318.

Docker Compose support and the OTLP receiver both stay. OTLP has been less useful than
hoped, but some projects talk to OTLP servers when configured, so being able to develop
against it and confirm that path works has real value — and more logging available to
the agent is better.

## Vocabulary

The project's locked vocabulary lives in [`GLOSSARY.md`](GLOSSARY.md) — **instance**,
**source**, **managed process**, **ring buffer**, **retention limits** and the rest.
Follow it for anything user-facing, and challenge any new term against it before adding
one. Definitions live there only; do not restate them here.

One term belongs to planning rather than the product, so it is not in the glossary:

- **Soak** — using the tool on real work to find out whether a change paid off. Not a
  build step; there is nothing to implement.

## Phases

1. **Green build** — fix the process-manager hang behind the two failing tests; get
   `go test ./...` passing in seconds. First, because there is currently no working
   verification command for this repo.
2. **Remove MCP** — delete `internal/api/mcp.go` and its tests, config and docs.
3. **Instance marker and skill** — `.running-man/instance.json` plus a skill built around
   the REST API. This is the actual bet; phases 1 and 2 are groundwork.
4. **Honest docs and repo hygiene** — rewrite the README, fix dead links, strip beads
   from `AGENTS.md`, deal with the committed binary.

Then a soak, to find out whether phase 3 worked.

Known tension, recorded rather than resolved: if success means the agent starts Running
Man unprompted, then agent-started instances become the goal — and under today's
single-process design the developer cannot attach to one. A client/server split
(headless daemon plus detachable TUI) is the fix, and is deferred until phase 3 shows how
often it actually matters.

## Planning

Work is planned in `plans/<date>-<slug>.md`, one file per phase, with progress
appended as it lands. `plans/ideas.md` holds one-line notes for things that are not
this work.

`plans/` is **deliberately untracked** — the GitHub repo is public and the planning
notes are not for publication. It is excluded via `.git/info/exclude`, not
`.gitignore`. Do not commit it.

Issue tracking previously used beads; it was removed in `5405295` and is not coming
back. Plan files serve the same purpose without the churn.
