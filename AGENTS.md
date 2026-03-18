# Agent Instructions

This project uses **bd** (beads) for issue tracking. Run `bd onboard` to get started.

## Development

### Tests
- Keep a comprehensive but focussed set of test for the core functionality. It shouldn't take more than a second or two to run
- Any longer or more integrated tests should be tagged as such and only run at key stages such as before code review / commit
- When fixing bugs write a failing test first if possible

### Style
- Format code properly (with go fmt or whatever) before committing

## Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --status in_progress  # Claim work
bd close <id>         # Complete work
bd sync               # Sync with git
```

## Branch Protection Workflow

**MAIN BRANCH IS PROTECTED:** All changes must be made in feature branches and merged via pull requests.

**MANDATORY WORKFLOW FOR ALL CHANGES:**

1. **Check beads for related work:**
   ```bash
   bd ready              # Find available work
   bd show <id>          # View issue details
   bd update <id> --status in_progress  # Claim work
   ```

2. **Create feature branch (use bead ID when possible):**
   ```bash
   # When working on a beads issue (preferred):
   git checkout -b beads/<bead-id>-short-description
   # Example: git checkout -b beads/the_running_man-yut-otel-tracing
   
   # When no beads issue:
   git checkout -b feature/descriptive-name
   # or
   git checkout -b fix/issue-description
   # or  
   git checkout -b docs/topic-update
   ```

3. **Make changes and commit:**
   ```bash
   git add .
   git commit -m "Descriptive commit message"
   ```

4. **Push branch to remote:**
   ```bash
   git push -u origin branch-name
   ```

5. **Create pull request (reference beads issue in PR body):**
   ```bash
   gh pr create --title "PR Title" --body "Description of changes\n\nRelated to beads: <bead-id>"
   ```

6. **Wait for PR review/approval** before merging

## Landing the Plane (Session Completion)

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until changes are in a PR.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **CREATE/UPDATE PR** - This is MANDATORY:
   ```bash
   # If new branch:
   git push -u origin branch-name
   gh pr create --title "Title" --body "Description\n\nRelated to beads: <bead-id>"
   
   # If existing branch:
   git push
   # PR will auto-update
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes are in a PR (not necessarily merged)
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- ALWAYS check current branch with `git status` or `git branch --show-current` before making changes
- NEVER push directly to main branch
- ALWAYS create a feature branch for changes
- ALWAYS create a PR before ending session
- **NEVER merge a PR without explicit permission from the user**
- Work is NOT complete until changes are in a PR
- If PR creation fails, resolve and retry until it succeeds


<!-- BEGIN BEADS INTEGRATION v:1 profile:full hash:d4f96305 -->
## Issue Tracking with bd (beads)

**IMPORTANT**: This project uses **bd (beads)** for ALL issue tracking. Do NOT use markdown TODOs, task lists, or other tracking methods.

### Why bd?

- Dependency-aware: Track blockers and relationships between issues
- Git-friendly: Dolt-powered version control with native sync
- Agent-optimized: JSON output, ready work detection, discovered-from links
- Prevents duplicate tracking systems and confusion

### Quick Start

**Check for ready work:**

```bash
bd ready --json
```

**Create new issues:**

```bash
bd create "Issue title" --description="Detailed context" -t bug|feature|task -p 0-4 --json
bd create "Issue title" --description="What this issue is about" -p 1 --deps discovered-from:bd-123 --json
```

**Claim and update:**

```bash
bd update <id> --claim --json
bd update bd-42 --priority 1 --json
```

**Complete work:**

```bash
bd close bd-42 --reason "Completed" --json
```

### Issue Types

- `bug` - Something broken
- `feature` - New functionality
- `task` - Work item (tests, docs, refactoring)
- `epic` - Large feature with subtasks
- `chore` - Maintenance (dependencies, tooling)

### Priorities

- `0` - Critical (security, data loss, broken builds)
- `1` - High (major features, important bugs)
- `2` - Medium (default, nice-to-have)
- `3` - Low (polish, optimization)
- `4` - Backlog (future ideas)

### Workflow for AI Agents

1. **Check ready work**: `bd ready` shows unblocked issues
2. **Claim your task atomically**: `bd update <id> --claim`
3. **Work on it**: Implement, test, document
4. **Discover new work?** Create linked issue:
   - `bd create "Found bug" --description="Details about what was found" -p 1 --deps discovered-from:<parent-id>`
5. **Complete**: `bd close <id> --reason "Done"`

### Auto-Sync

bd automatically syncs via Dolt:

- Each write auto-commits to Dolt history
- Use `bd dolt push`/`bd dolt pull` for remote sync
- No manual export/import needed!

### Important Rules

- ✅ Use bd for ALL task tracking
- ✅ Always use `--json` flag for programmatic use
- ✅ Link discovered work with `discovered-from` dependencies
- ✅ Check `bd ready` before asking "what should I work on?"
- ❌ Do NOT create markdown TODO lists
- ❌ Do NOT use external issue trackers
- ❌ Do NOT duplicate tracking systems

For more details, see README.md and docs/QUICKSTART.md.

## Landing the Plane (Session Completion)

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   bd dolt push
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds

<!-- END BEADS INTEGRATION -->
