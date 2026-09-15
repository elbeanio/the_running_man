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

### Vocabulary

Follow the locked terms in [`GLOSSARY.md`](GLOSSARY.md) for anything user-facing — config
keys, API fields, TUI labels, documentation and commit messages. **Challenge any new term
against it** before introducing one: if a word for the concept already exists, use it; if
it does not, add an entry in the same change that ships the name.

Note in particular that **instance** (not "session") means one Running Man run, and
**retention limits** covers the whole eviction policy, not just the time window.

## Development

### Tests

- The core suite runs in a second or two. Anything slower is a bug in the test, not a fact
  about the suite.
- Tag longer or more integrated tests and run them only before review.
- **When fixing a bug, write the failing test first**, and confirm it fails for the right
  reason before fixing. A regression test that passes against the unfixed code is worthless
  — this has happened here: a volume-based test for lost output passed on the broken code,
  because a few hundred short lines fit in the pipe buffer.
- Prove a fix by reverting it in place and watching the test fail, rather than by reasoning
  that it must work.

### Style

- `gofmt` before committing. `make lint` must pass.
- Comment the *why*, not the *what* — particularly where a choice is not obvious, or where
  the obvious alternative is wrong. A comment explaining why a wait is bounded is worth
  more than one explaining that it waits.
- Do not delete the evidence of a bug to make a symptom go away. A scanner error was once
  silenced here on the reasoning that it was "expected behavior, not an error"; it was in
  fact a symptom of real data loss, which then went unnoticed.

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
make skill:link    # symlink the agent skill into ~/.claude/skills
go generate ./internal/api   # re-sync the embedded OpenAPI spec after editing docs/openapi.yaml
```

## Branching and pull requests

`main` is protected. All changes go through a feature branch and a pull request.

This holds even for small changes and even when working alone. The repository is public,
so the review mechanism is kept oiled for any external contribution that arrives — a
project where every change has always gone straight to `main` is a project nobody can
contribute to.

1. **Check `plans/`** for related work — the phase plans and `plans/ideas.md`.
2. **Branch** — `feature/`, `fix/`, `docs/` or `refactor/` plus a short description.
3. **Commit as you go.** Several small commits that each do one thing are easier to review
   than one large one, and the history is the record of why the code looks like this.
4. **Push and open a PR.**
5. **Wait for review.** Never merge without explicit permission.

### Stacked branches

Avoid them. If a branch must be based on another unmerged branch, say so in the PR body,
and **retarget it to `main` the moment its base merges** — squash-merging a base severs the
stack, and GitHub only auto-retargets when the base branch is deleted on merge.

## Writing commit messages and pull request descriptions

**Write about the change, not to the reader.** These are public, permanent, and read by
people with no memory of the conversation that produced them — including the author, later.

Take the tone from [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/):
declarative, impersonal, present tense. The standard itself is not required here, but its
register is.

**Do not:**

- address the reader — "you asked for", "as you noted", "per your instruction"
- narrate the author — "I found", "I tried X then Y", "I should have caught this"
- apologise, thank, or editorialise — "apologies for", "unfortunately", "nice catch"
- describe the process of arriving at the change, unless the process *is* the finding

**Do:**

- state what changed and why it needed changing
- give the evidence: the failing output, the wrong value, the false claim, verbatim
- name the consequence of the bug, not just its mechanism
- record decisions taken and alternatives rejected, with the reason
- flag anything a reviewer would want to argue with

**Instead of:**

> You were right that the docs were wrong. I found six flags that don't exist and fixed
> them — sorry about the earlier confusion. Let me know if the README is now too slim!

**Write:**

> `docs/configuration.md` documented six flags that do not exist: `--retention`,
> `--shell`, `--max-entries`, `--max-bytes`, `--max-spans`, `--max-span-age`. All six
> appeared in a reference table and in copy-pasteable examples; following any of them
> fails with `flag provided but not defined`. They are configuration-file keys only. The
> table is now exactly the 13 real flags, with an explicit note naming the six, because
> the incorrect version has been published.

Facts, consequences and decisions survive; pleasantries and narration do not. Conversation
belongs in the conversation, and anything worth keeping from it belongs in `plans/`.

## Ending a session

Work is not complete until it is in a pull request.

1. **Record remaining work** in `plans/ideas.md`.
2. **Run the quality gates** if code changed: `make test`, `make test-race`, `make lint`,
   `gofmt -l`.
3. **Update the plan** — append what landed to the Progress section, including anything
   done differently from what was planned, and why.
4. **Push and open or update the PR.**
5. **Clean up** — clear stashes, prune merged branches.
6. **Hand off** — leave enough context for the next session to resume.

### Rules

- Check the current branch before making changes.
- Never commit or push directly to `main`.
- Never merge a pull request without explicit permission.
- If a `git add -A` appears to have missed a file, check `.gitignore` before assuming it
  worked: an unanchored pattern can silently ignore a whole directory, and `git status`
  will not mention it.
- If a tool or approach has already failed once in a session, do not reach for it again
  without a reason to expect a different outcome.
