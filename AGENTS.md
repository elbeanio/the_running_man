# Agent Instructions

Read [`PROJECT.md`](PROJECT.md) first: what this project is for, its constraints and its
non-goals. Then [`GLOSSARY.md`](GLOSSARY.md) for the locked vocabulary.

## Before starting any long-running process

**Check whether it is already running:**

```bash
cat .running-man/instance.json 2>/dev/null && curl -s http://localhost:9000/processes
```

If that file exists, a Running Man instance is supervising this project and the dev
servers are probably already up. Use them rather than starting a second copy — a second
copy collides on the port or silently binds a different one, and its output is invisible
to the developer watching the project.

The file lists the API URL, every configured process, and ready-to-run `curl` hints.
Full guidance is in `skills/running-man/SKILL.md`.

## Project Context

Read [`PROJECT.md`](PROJECT.md) for what this project is for, its constraints and its
non-goals. Ordered work lives in `plans/` (untracked).

### Vocabulary

Follow the locked terms in [`GLOSSARY.md`](GLOSSARY.md) for anything user-facing — config
keys, API fields, TUI labels, docs and commit messages. **Challenge any new term against
it** before introducing one: if a word for the concept already exists, use it; if it
doesn't, add an entry in the same change that ships the name.

Note in particular that **instance** (not "session") means one Running Man run, and
**retention limits** covers the whole eviction policy.

## Development

### Tests
- Keep a comprehensive but focussed set of test for the core functionality. It shouldn't take more than a second or two to run
- Any longer or more integrated tests should be tagged as such and only run at key stages such as before code review / commit
- When fixing bugs write a failing test first if possible

### Style
- Format code properly (with go fmt or whatever) before committing

## Issue tracking

**There is none, deliberately.** This project used beads (`bd`); it was removed in
`5405295` and is not coming back. Do not reintroduce it, and do not create markdown TODO
lists as a substitute.

Work is planned in `plans/<date>-<slug>.md`, one file per phase or change, with progress
appended as it lands. `plans/ideas.md` holds one-line notes for anything that is not the
current work. `plans/` is **untracked** — the GitHub repo is public and the planning notes
are not for publication. It is excluded via `.git/info/exclude`, not `.gitignore`.

## Quick reference

```bash
make test          # unit tests (seconds; anything slower is a bug)
make test-race     # race detector
make lint          # golangci-lint
make link-skill    # symlink the agent skill into ~/.claude/skills
go generate ./internal/api   # re-sync the embedded OpenAPI spec after editing docs/openapi.yaml
```

## Branch Protection Workflow

**MAIN BRANCH IS PROTECTED:** All changes must be made in feature branches and merged via pull requests.

**MANDATORY WORKFLOW FOR ALL CHANGES:**

1. **Check `plans/` for related work** — the phase plans and `plans/ideas.md`.

2. **Create a feature branch:**
   ```bash
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

5. **Create a pull request:**
   ```bash
   gh pr create --title "PR Title" --body "What changed and why"
   ```

6. **Wait for PR review/approval** before merging

## Landing the Plane (Session Completion)

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until changes are in a PR.

**MANDATORY WORKFLOW:**

1. **Record remaining work** - add anything needing follow-up to `plans/ideas.md`
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update the plan** - append what landed, and any departures from what was planned, to the phase plan's Progress section
4. **CREATE/UPDATE PR** - This is MANDATORY:
   ```bash
   # If new branch:
   git push -u origin branch-name
   gh pr create --title "Title" --body "What changed and why"
   
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
