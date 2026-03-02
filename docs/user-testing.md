# User Testing & Feedback

## Overview

The Running Man is a weekend project, but we still want quality feedback after each phase. This doc describes how we collect and act on feedback.

## Process

After each phase:

1. **Alpha Testing:** Small group (3-5 developers) uses Running Man in their daily workflow
2. **Feedback Collection:** GitHub issues + direct reports
3. **Iteration:** Fix critical bugs, adjust UX based on real usage
4. **Sign-off:** Phase complete when success criteria met

Testing is informal but focused. The goal is real-world usage, not formal QA.

---

## Current Phase: 2.5 (Polish & Fixes)

**Testing Period:** 1-2 weeks  
**Focus:** TUI usability, bug-free daily usage  
**Testers:** Ian + 2-3 friends

**Success Criteria:**
- TUI is usable for daily work (no critical rendering bugs)
- Logs don't disappear unexpectedly
- Navigation feels natural
- No showstopper bugs

**How to Report:**
- **Bugs:** GitHub issue with reproduction steps
- **UX issues:** Description + suggested improvement
- **Feature requests:** Use case + why it's needed

---

## Bug Report Template

```markdown
**Description:**  
What's broken?

**Steps to Reproduce:**  
1. Start running-man with...
2. Open TUI
3. Do X, observe Y

**Expected:**  
What should happen

**Actual:**  
What actually happens

**Environment:**  
- OS: macOS 14.2
- Go version: 1.21
- Terminal: iTerm2 / Alacritty / etc.
- Config: (attach running-man.yml if relevant)

**Logs/Screenshots:**  
(if applicable)
```

---

## Feature Request Template

```markdown
**Use Case:**  
What are you trying to do?

**Current Workaround:**  
How do you do it now? Why is it painful?

**Proposed Solution:**  
What would help?

**Priority:**  
Critical / Nice-to-have

**Phase:**  
Which phase should this be in? (2.5, 3, 4, 5, or backlog)
```

---

## Phase-Specific Testing Goals

### Phase 2.5: Polish & Fixes

**What to test:**
- TUI rendering (progress bars, multi-line output, special characters)
- TUI navigation (tab switching, scrolling)
- Log retention (do logs disappear unexpectedly?)
- Config file loading (does auto-discovery work?)
- Process management (restart on crash behavior)

**What to report:**
- Rendering glitches
- Unexpected behavior
- Confusing UX
- Missing features that block daily use

### Phase 3: Agent Integration

**What to test:**
- Query Running Man from Claude Code / OpenCode
- Use skills for common debugging tasks
- Integration setup (is it easy?)
- API usefulness (does it save time?)

**What to report:**
- Skills that don't work or are confusing
- Missing skills for your workflows
- Integration friction
- API design issues

**Metrics to track:**
- Time saved vs manual log searching
- Percentage of debugging tasks where agent succeeds without manual help

### Phase 4: OTEL & Visualization

**What to test:**
- OTEL instrumentation setup
- Trace capture accuracy
- Trace visualization usefulness
- Log/trace correlation

**What to report:**
- Instrumentation difficulties
- Missing traces or spans
- Correlation failures
- Visualization UX issues

**Scenarios to test:**
- Microservices with 3+ services
- Multi-step workflows (DAGs)
- Error scenarios (failed requests)

### Phase 5: Browser Integration

**What to test:**
- Browser SDK setup (Vite, webpack, Next.js)
- Error capture completeness
- Performance impact
- Trace ID propagation

**What to report:**
- Missing errors or console logs
- Performance problems
- Integration friction
- Framework-specific issues

**Metrics to track:**
- Bundle size impact
- Runtime overhead (ms per batch)
- Error capture rate (should be 100%)

---

## Feedback Channels

### GitHub Issues

**For:** Bugs, feature requests, documentation issues

**Labels:**
- `bug` - Something is broken
- `enhancement` - Feature request or improvement
- `documentation` - Docs need updating
- `phase-2.5` / `phase-3` / etc. - Which phase this belongs to
- `help-wanted` - Good for contributors

### Direct Feedback

**For:** Quick questions, UX observations, informal discussion

**Where:** Direct message, Slack, etc. (whatever we're using)

### Informal Discussion

**For:** "Should this work like X or Y?" design questions

**Where:** GitHub Discussions (if we set it up) or direct chat

---

## What Makes Good Feedback

### Good Bug Reports

✅ **Clear reproduction steps:**
```
1. Start running-man with docker-compose
2. Switch to postgres tab
3. Scroll up
4. BUG: TUI freezes
```

✅ **Include context:** OS, terminal, config file

✅ **Screenshots/logs** if visual or log-related

❌ **Avoid vague:** "TUI is broken" (what exactly is broken?)

### Good Feature Requests

✅ **Explain the use case:**
"I'm debugging a multi-step workflow. I want to see all logs for workflow_id=X across all services."

✅ **Describe current pain:**
"Right now I have to query each service separately and manually correlate timestamps."

✅ **Suggest a solution** (but be open to alternatives):
"Maybe `GET /logs?workflow_id=X` could return correlated logs?"

❌ **Avoid:** "Add feature X" without explaining why

---

## Success Metrics by Phase

### Phase 2.5
- No critical TUI bugs reported after 1 week of use
- At least 2 testers using it daily
- Logs are readable and accessible

### Phase 3
- Agent can debug 80% of common errors without manual log gathering
- At least 5 skills used regularly
- Integration takes < 5 minutes to set up

### Phase 4
- Traces captured accurately for 95%+ of requests
- Log/trace correlation works reliably
- Visualization is useful (testers actually use it)

### Phase 5
- Browser SDK captures 100% of errors
- Performance overhead < 5ms per batch
- Integration guides work for all major frameworks

---

## Example Testing Scenarios

### Phase 2.5: Daily Development

**Scenario:** Use Running Man for your normal development workflow for 1 week

**Test:**
1. Start your usual stack with running-man
2. Debug errors using the TUI
3. Query logs via API when needed
4. Report any friction, bugs, or missing features

**Success:** You prefer using Running Man over juggling terminal tabs

### Phase 3: Agent Debugging

**Scenario:** Debug an application error using Claude Code + Running Man

**Test:**
1. Trigger an error in your app (or wait for one naturally)
2. Ask Claude Code to debug it
3. Claude Code should query Running Man for context
4. Report if this worked, and if not, why

**Success:** Agent finds root cause without you manually providing logs

### Phase 4: Distributed Tracing

**Scenario:** Debug a slow request across multiple services

**Test:**
1. Instrument 3+ services with OTEL
2. Make a request that touches all services
3. Query Running Man for the trace
4. Use visualization to find the slow span

**Success:** You can identify the bottleneck in < 1 minute

### Phase 5: Frontend Errors

**Scenario:** Debug a frontend error using browser SDK

**Test:**
1. Add Running Man SDK to your frontend
2. Trigger a frontend error (or wait for one)
3. Query Running Man for browser logs
4. Correlate with backend logs via trace ID

**Success:** You see the full frontend → backend error story in one place

---

## Triage & Prioritization

**Critical (fix immediately):**
- Crashes or data loss
- Showstopper bugs blocking daily use
- Security issues

**High (fix this phase):**
- Major UX issues
- Missing functionality blocking phase goals
- Significant performance problems

**Medium (fix next phase or backlog):**
- Minor UX friction
- Edge cases
- Nice-to-have features

**Low (backlog):**
- Polish
- Very rare edge cases
- Features for later phases

---

## Contributing

Want to help test? Here's how:

1. **Install:** `go install github.com/elbeanio/the_running_man/cmd/running-man@latest`
2. **Use:** Try it in your daily workflow for a week
3. **Report:** File issues on GitHub with good reproduction steps
4. **Discuss:** Join design discussions for upcoming phases

See you in the issues! 🏃
