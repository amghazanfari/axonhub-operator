---
description: Full autonomous loop — implement a feature, gate it, review it, open a PR
---

Implement this feature for the axonhub-operator project end to end:

$ARGUMENTS

You are the BUILDER agent in an autonomous pipeline. Execute every stage without
asking for confirmation. Work strictly in this order:

## Stage 1 — Setup
1. Read `AGENTS.md` at the repo root. It is the rulebook; every stage must comply
   with its Hard Rules and Conventions.
2. Verify the working tree is clean (`git status`). If it is not, STOP and report.
3. Create a git worktree for the work:
   `git worktree add /tmp/opencode/<branch> -b feat/<short-slug>`
   All edits happen in the worktree, never in the main tree.

## Stage 2 — Implement
- Work in small vertical slices: one spec field + its reconcile wiring + tests.
- After editing `api/v1alpha1/*.go`, ALWAYS run `make generate && make manifests`.
- Never hand-edit generated files (`zz_generated.deepcopy.go`, `config/crd/bases/*`).
- Add or adjust RBAC markers for any new resource types, then regenerate.

## Stage 3 — Gates (mechanical, not optional)
Run from the worktree, in this order, fixing failures before continuing:
1. `make generate manifests`
2. `make test`  (envtest; must pass with the new assertions)
3. `make lint`

## Stage 4 — Fresh-context review
Spawn a REVIEWER subagent with NO context from this conversation. Give it only:
- the worktree path
- the instruction to read `AGENTS.md` first
- the diff to review (`git diff main` inside the worktree)
- read-only verification; it must not run tests or modify files
- output format: VERDICT (APPROVE / APPROVE WITH NITS / REQUEST CHANGES) +
  numbered findings with severity, file:line, and one-line explanation.

## Stage 5 — Triage
- Fix every blocker and major finding in the worktree, then re-run Stage 3 gates.
- File deferred minor/nit findings as GitHub issues
  (`gh issue create`) so nothing is silently dropped.

## Stage 6 — Ship
1. Commit in the worktree with a conventional message describing the change.
2. Push the branch and open a PR:
   `gh pr create --title <conventional title> --body <summary + review process>`
   The PR body must mention that CI (Lint/Tests/E2E) and the event-driven agent
   review run automatically.
3. Remove the worktree (`git worktree remove`), prune, and report the PR URL.

Do not merge the PR. The human decides that.
