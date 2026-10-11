---
description: Guided loop — same pipeline as /feature but pauses for your approval at every stage
---

Implement this feature step by step, pausing for my approval between stages:

$ARGUMENTS

You are the BUILDER agent in a GUIDED pipeline. Same stages as the autonomous
loop, but after EVERY stage you must STOP, present a short report, and ask me
"Proceed to the next stage?" — and wait for my answer. If I say no, take my
feedback and redo the current stage before asking again.

## Stage 1 — Plan (STOP after this)
- Read `AGENTS.md` (the rulebook).
- Present: the slice you will implement (spec fields, reconcile wiring, tests,
  RBAC changes), the branch name, and the worktree path.
- Do NOT create the worktree yet — wait for approval.

## Stage 2 — Setup (STOP after this)
- Verify the working tree is clean.
- Create the worktree: `git worktree add /tmp/opencode/<branch> -b feat/<slug>`.
- Report the worktree path and confirm no files changed in the main tree.

## Stage 3 — Implement (STOP after this)
- Implement in the worktree only, following AGENTS.md hard rules
  (regenerate after API edits, never touch generated files).
- Present: list of changed files with a one-line purpose for each, and the key
  design decisions you made.

## Stage 4 — Gates (STOP after this)
- Run `make generate manifests`, then `make test`, then `make lint`.
- Fix failures and re-run until green. Present the final test summary line
  (with coverage) and lint result. If a gate cannot pass, stop and explain.

## Stage 5 — Fresh-context review (STOP after this)
- Spawn a REVIEWER subagent with no context from this conversation: worktree
  path, read `AGENTS.md`, review `git diff main`, read-only, verdict format
  (APPROVE / APPROVE WITH NITS / REQUEST CHANGES + numbered findings).
- Present the reviewer's full verdict verbatim.
- Propose your triage: which findings to fix now, which to file as issues.

## Stage 6 — Ship (STOP after this)
- Apply the approved triage, re-run gates if anything changed.
- Commit, push the branch, open the PR with `gh pr create` (body must mention
  that CI and the agent review run automatically).
- Remove the worktree and present the PR URL.
- Remind me that merging is my decision, and that the event-driven agent
  review + CI will report on the PR.
