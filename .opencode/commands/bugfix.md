---
description: Bugfix loop — reproduce with a failing test, fix, gate, review, PR
---

Fix this bug in the axonhub-operator project:

$ARGUMENTS

$ARGUMENTS is either a GitHub issue number (read it with `gh issue view
<number>`) or a plain description of the bug. You are the BUILDER agent in a
bugfix pipeline. Execute every stage without asking for confirmation.

## Stage 1 — Setup
1. Read `AGENTS.md` at the repo root; comply with its Hard Rules throughout.
2. Verify the working tree is clean (`git status`). If not, STOP and report.
3. Understand the bug before touching anything:
   - Reproduce it if possible (run the relevant test, or write a quick
     reproduction). If you cannot reproduce it, STOP and report what you tried.
   - Identify the root cause by reading the code — do not patch symptoms.
4. Create a worktree: `git worktree add /tmp/opencode/<branch> -b fix/<slug>`.

## Stage 2 — Failing test first
- Write a regression test that fails because of the bug (envtest for
  controller behavior, table tests for pure functions).
- Run it, confirm it FAILS for the right reason, and show the failure output.
- A bugfix without a failing test first is not a bugfix.

## Stage 3 — Fix
- Make the minimal change that turns the failing test green.
- Do not refactor, rename, or "improve" anything unrelated in the same change.
- Follow AGENTS.md conventions (regenerate after API edits, error wrapping,
  conditions, RBAC markers).

## Stage 4 — Gates
Run from the worktree until green, fixing failures before continuing:
1. `make generate manifests` (if any API/RBAC files changed)
2. `make test` — the regression test must pass now, and nothing else may break
3. `make lint`

## Stage 5 — Fresh-context review
Spawn a REVIEWER subagent with NO context from this conversation. Give it only:
- the worktree path and the instruction to read `AGENTS.md` first
- the diff to review (`git diff main` inside the worktree)
- the original bug description, so it can judge whether the fix addresses it
- read-only verification; no test/lint runs, no file modifications
- output: VERDICT (APPROVE / APPROVE WITH NITS / REQUEST CHANGES) + numbered
  findings with severity and file:line.

Ask the reviewer explicitly: "Does this fix the described bug at its root
cause, and is the regression test sufficient to prevent it?"

## Stage 6 — Triage
- Fix blockers and majors in the worktree, re-run the gates.
- File deferred minors/nits as GitHub issues.

## Stage 7 — Ship
1. Commit with `fix: <what and why>` — reference the issue number
   (`Fixes #<n>`) when one exists, so the issue auto-closes on merge.
2. Push the branch and open a PR with `gh pr create`. The body must include:
   root cause, the failing-test evidence, and the fix summary.
3. Remove the worktree and report the PR URL.

Do not merge the PR. The human decides that.
