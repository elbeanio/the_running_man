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

