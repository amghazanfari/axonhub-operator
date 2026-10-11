---
description: Fresh-context review of a branch or PR — verdict + findings, read-only
agent: general
---

Review: $ARGUMENTS

$ARGUMENTS is either a git branch name in this repository or a pull request
number. Resolve it first:

- **PR number**: use `gh pr view <number>` to get base and head SHAs; check out
  the head SHA if needed.
- **Branch name**: the diff is `git diff main...<branch>`.

You have NO prior context about this change — judge only what you can verify.
You are in READ-ONLY mode: do not create, edit, or delete any file. Do not run
tests, linters, or builds — CI already gates those.

Review for:
1. Correctness bugs in reconcile logic (idempotency, error handling, edge cases).
2. Kubernetes API conventions and controller best practices.
3. Security and RBAC correctness (over- or under-permission).
4. Test quality: do assertions actually verify behavior?
5. Compliance with the repository rulebook `AGENTS.md` (read it first).
6. CRD design: defaults, validation markers, future extensibility.

Output format (exact structure, Markdown):

- **VERDICT:** APPROVE / APPROVE WITH NITS / REQUEST CHANGES
- **Findings:** numbered list, max 10, most important first. Each item:
  `severity (blocker/major/minor/nit) — file:line — one-line explanation`.
- Be specific, not generic. If the change is clean, say so plainly instead of
  inventing findings.
