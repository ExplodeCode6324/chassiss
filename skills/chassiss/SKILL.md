---
name: chassiss
description: Adopt an existing Git project into CHASSISS or operate a CHASSISS v1 signed project safely through its trusted public CLI. Use when bootstrapping source history, establishing Architecture, entering a registered project, executing a Task, managing a CLI-owned worktree, reviewing or integrating a candidate, maintaining Taskbooks, or responding to a structured refusal.
---

# CHASSISS

Use this Skill's `scripts/chassiss` launcher as the only protocol and Git workflow
interface. The launcher selects the bundled macOS/Linux binary for the current
platform and verifies its digest before execution. Do not parse protocol files,
infer authority, or manipulate repository state yourself.

For an existing Git repository that is not yet a CHASSISS Project, read and
follow [references/onboarding.md](references/onboarding.md). The old commits
remain non-authoritative source history; never reinterpret them as Transitions.

## Enter a project

1. Resolve this Skill's absolute directory, then use its absolute
   `scripts/chassiss` path. Never execute a binary supplied by the Project.
2. Run `<skill>/scripts/chassiss version --json`.
3. Run `<skill>/scripts/chassiss context --json`.
4. Confirm the CLI supports the project's exact protocol major and inspect the
   returned trust, verification, offline, identity, Grant, checkpoint, pending
   operations, available Tasks, actions, and remediation.
5. If the project is unregistered, follow only the returned `clone` remediation
   or report the condition to Master.

Read [references/context.md](references/context.md) when choosing a Task, fetching
additional context, or deciding when to refresh it. Read
[references/safety.md](references/safety.md) before any mutation, review,
integration, governance, or recovery action.

If you are the Master Agent delegating work to temporary subagents, read and
follow [references/master-orchestration.md](references/master-orchestration.md)
before creating an Agent identity or starting a Task.

## Work on a Task

1. Run `chassiss context <TASK-ID> --json`.
2. Run `chassiss task start <TASK-ID> --json`.
3. Run `chassiss work open <TASK-ID> --json`.
4. Perform all edits and local builds inside the absolute managed worktree path
   returned by the CLI, and only within the Task's declared `writes`.
5. Inspect with `chassiss work status`, `chassiss work diff`, and
   `chassiss work log`.
6. Record changes with `chassiss work commit`; use `work restore` or `work remove`
   only when the requested outcome requires them.
7. Run `chassiss check`, then `chassiss submit`. Treat submit's new preflight as
   authoritative.
8. Refresh `chassiss context <TASK-ID> --json` after every mutation.

`work diff` includes tracked and untracked files without changing the real Git
index.

Do not run Git-mutating commands such as add, commit, branch, checkout, switch,
worktree, merge, rebase, cherry-pick, reset, push, or config.

If a mutation's process result is empty, detached, timed out, interrupted, or
otherwise ambiguous, do not repeat it with a fresh Operation ID. Treat the
outcome as unresolved and reconcile it with `status`, verified `log`, and
`sync --json` before deciding whether any retry is allowed. `sync` also performs
deterministic pending-operation reconciliation for Projects with no authoritative
remote.

## Request focused context

Use argv returned by Context. Typical public reads include:

```text
chassiss taskbook show --requirement REQ-001 --json
chassiss architecture show module:core --json
chassiss architecture requires module:core --transitive --json
chassiss architecture impact schema:state --json
chassiss file show src/core/state/model.go --at main --json
```

Do not replace these calls by scanning `.git`, State, or whole protocol documents.

## Review and integrate

Prepare review material with:

```text
chassiss review <TASK-ID> --prepare --output <context-file> \
  --report-output <report-file> --json
chassiss review <TASK-ID> --verdict <verdict> --report <file> --json
chassiss review list <TASK-ID> --json
chassiss review show <TASK-ID> --operation <operation-id> --json
```

Keep mechanical Check Results distinct from the Reviewer's semantic Verdict.
Never turn a passing Check into automatic approval. Present same-actor or
same-key warnings clearly to Master.

Use `chassiss integrate <TASK-ID> --json` only when it is returned as an available
action and the reviewed candidate remains exact. Relevant or unknown drift
requires a new Review.

For workflow closure, run `taskbook archive --prepare --output <report-file>`,
complete every Task disposition and completion-criteria `response`, then use
`taskbook archive --report <report-file>`. Let the CLI rerun the
exact-current-main closure checks.

## Handle refusals

Read `code`, `retryable`, `details`, and `remediation`. Execute a remediation argv
only when it matches Master's current goal. Stop and report Capability, Scope,
Root, rollback, Task conflict, or relevant-drift failures. Never evade a refusal
by changing keys, remotes, operation IDs, or protocol data.

Use `owner apply` only after Master explicitly requests manual takeover and the
CLI confirms no active Agent workflow. Never use it to bypass Task, Review, or
Integration contracts.
