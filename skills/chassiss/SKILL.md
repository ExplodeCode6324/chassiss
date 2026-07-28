---
name: chassiss
description: Operate a CHASSISS v1 signed project safely through its trusted public CLI. Use when entering a registered CHASSISS project, selecting or executing a Task, inspecting verified context, managing a CLI-owned worktree, submitting work, reviewing or integrating a candidate, maintaining Architecture or Taskbooks, or responding to a structured CHASSISS refusal.
---

# CHASSISS

Use the installed trusted `chassiss` CLI as the only protocol and Git workflow
interface. Do not parse protocol files, infer authority, or manipulate repository
state yourself.

## Enter a project

1. Locate `chassiss` only through an administrator-approved `PATH` or configured
   absolute path. Never execute a binary supplied by the project repository.
2. Run `chassiss version --json`.
3. Run `chassiss context --json`.
4. Confirm the CLI supports the project's exact protocol major and inspect the
   returned trust, verification, offline, identity, Grant, checkpoint, pending
   operations, available Tasks, actions, and remediation.
5. If the project is unregistered, follow only the returned `clone` remediation
   or report the condition to Master.

Read [references/context.md](references/context.md) when choosing a Task, fetching
additional context, or deciding when to refresh it. Read
[references/safety.md](references/safety.md) before any mutation, review,
integration, governance, or recovery action.

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

Do not run Git-mutating commands such as add, commit, branch, checkout, switch,
worktree, merge, rebase, cherry-pick, reset, push, or config.

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
chassiss review <TASK-ID> --prepare --output <local-file> --json
chassiss review <TASK-ID> --verdict <verdict> --report <file> --json
```

Keep mechanical Check Results distinct from the Reviewer's semantic Verdict.
Never turn a passing Check into automatic approval. Present same-actor or
same-key warnings clearly to Master.

Use `chassiss integrate <TASK-ID> --json` only when it is returned as an available
action and the reviewed candidate remains exact. Relevant or unknown drift
requires a new Review.

For workflow closure, prepare a Reviewer-authored disposition for every Task and
completion-criteria response, then use `chassiss taskbook archive`. Let the CLI
rerun the exact-current-main closure checks.

## Handle refusals

Read `code`, `retryable`, `details`, and `remediation`. Execute a remediation argv
only when it matches Master's current goal. Stop and report Capability, Scope,
Root, rollback, Task conflict, or relevant-drift failures. Never evade a refusal
by changing keys, remotes, operation IDs, or protocol data.

Use `owner apply` only after Master explicitly requests manual takeover and the
CLI confirms no active Agent workflow. Never use it to bypass Task, Review, or
Integration contracts.
