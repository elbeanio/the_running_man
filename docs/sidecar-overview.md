# The Running Man: Making AI-Assisted Development Less Painful

## The Problem

We've all been there: you're coding with an AI agent, something breaks, and now you're playing log detective. Tab over to the terminal, scroll through Python tracebacks, check docker logs, maybe dig through your OTEL UI, then copy-paste a bunch of context back into the agent. It works, but it's tedious and you inevitably miss something important.

The worst case is when your backend won't even start — a syntax error or missing env var — and your usual debugging tools aren't available because the server is down.

## The Idea

What if there was a lightweight tool that sat alongside your dev environment and captured everything automatically? Logs from your servers, output from docker containers, browser console output, traces from your instrumented code — all in one place, queryable by an agent (or you).

We're calling it **the running man**.

## How It Would Work

You'd start your dev environment through the running man instead of running things directly:

```bash
running-man run \
  --process "python server.py" \
  --process "npm run dev" \
  --docker-compose ./docker-compose.yml
```

This does a few things:

1. **Wraps your processes** — captures their stdout/stderr, parses for interesting stuff (tracebacks, errors, JSON logs), but still shows everything in your terminal like normal

2. **Tails docker logs** — attaches to your supporting services (database, redis, whatever) so those logs are captured too

3. **Captures browser logs** — a tiny JS SDK hooks console/errors and sends them to the running man

4. **Runs a mini OTEL collector** — your apps can send traces to localhost:4317 and the running man stores them

5. **Exposes a query API** — the agent (or you) can hit `localhost:9000/logs?since=30s&level=error` to get recent errors without manual copy-paste

The key insight is that this runs *outside* your application. If your Python server crashes on startup, the running man still has the traceback. If your frontend throws an error that traces back to a failed API call, both sides of that story are in one place.

## What Agents Would See

Instead of you pasting logs, the agent could query directly:

- "Show me errors from the last 30 seconds" → `GET /errors?since=30s`
- "What happened in the auth flow?" → `GET /traces?workflow_id=auth-login-123`
- "Why did the Python server restart?" → `GET /logs?source=python-server&since=1m`

The responses would be structured JSON, easy for an agent to parse and reason about.

## Browser Console Capture

One piece we're excited about: capturing browser console logs and errors too.

A tiny JS snippet (loaded only in dev mode) hooks into `console.log/warn/error`, catches uncaught exceptions, and optionally monitors failed network requests. It batches these up and sends them to the running man.

```javascript
// Only loads in development
if (import.meta.env.DEV) {
  import('./running-man-client.js')
}
```

Now when something breaks in the frontend, the agent can query `GET /browser?level=error&since=30s` instead of you opening devtools and copying stack traces.

Even better: if you're using trace IDs, the browser SDK includes them. So "user clicked button → API call failed → backend threw exception" becomes one queryable story across the whole stack.

## Trace Correlation

One nice side effect: we can tie frontend and backend together cheaply.

The frontend generates a trace ID when a user action starts (click a button, submit a form), passes it as a header on API calls, and the backend includes it in its spans. Now when something fails, you can query "show me everything related to trace X" and get the full picture — frontend action, API call, database queries, whatever.

For our multi-step agentic workflows (the DAG-style flows), same idea. Assign a workflow ID at the start, tag each step, query the whole workflow later.

## What It's Not

This isn't trying to be a production observability platform. No long-term storage, no fancy dashboards, no distributed tracing across hosts. It's dev tooling — lightweight, ephemeral, focused on the "something broke, help me understand why" loop.

## Open Questions

A few things we haven't settled:

1. **How much process management?** Should the running man restart crashed processes? Or just observe and let your existing autoreload handle it?

2. **Browser SDK packaging:** npm package you install? Single JS file you drop in? Both?

3. **Config files:** For complex setups, would a YAML config be useful? Or is CLI flags enough?

4. **Storage:** In-memory by default (simple, fast, dies when stopped). SQLite option for persistence? Probably overkill for dev but might be nice.

5. **Source maps:** Should the browser SDK resolve source maps for cleaner stack traces? More complex but nicer output.

## Why This Matters

The friction of getting context into an AI agent is real. Every time you have to manually collect logs and paste them, you lose momentum and probably miss something. A tool that makes that context automatically available could meaningfully improve the agent-assisted dev experience.

Plus, it's useful even without an agent — having all your logs in one queryable place beats juggling terminal tabs.

## Next Steps

If this sounds useful, let's chat about:

- Does this match problems you're hitting?
- What would you want from the query API?
- Any concerns about the approach?
- Want to help build it?