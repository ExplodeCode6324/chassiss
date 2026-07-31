# Verified context

Begin every project entry with:

```text
chassiss version --json
chassiss context --json
```

Context is a verified, deterministic working slice. Use it to learn the Project
ID, trust and offline status, main checkpoint, Architecture and Taskbook blobs,
current identity and Grant, available Tasks, pending operations, available
actions, local workflow commands, and the next safe read.

Request a Task-specific slice before work:

```text
chassiss context TASK-001 --json
```

Use the exact argv returned by Context when more information is needed. Fetch a
specific Requirement, Architecture Resource, dependency or impact view, or file
at main instead of loading the entire Taskbook or repository.

Refresh Context:

- after start, submit, review, integrate, block, resume, or any other mutation;
- after Architecture or Taskbook open, update, or archive;
- after identity or Grant changes;
- when switching Task or managed worktree;
- after a stale-main or main-changed error;
- after an empty, detached, timed-out, or otherwise ambiguous mutation result;
- whenever the CLI's remediation requests it.

A blocked Task must be resumed before any phase-changing action that requires an
unblocked Task. In particular, execute the exact `task resume` argv returned by
Context, refresh Context, and only then use `task release`. Context exposes
`task release` only for the current Task Actor when its Grant authorizes release,
the managed worktree is clean, and Work Head still equals the frozen base. The
mutation command rechecks every condition before publishing.

A successful release also reports whether the managed worktree, exact local
Work Ref, and registry record were removed. A cleanup warning does not undo the
published release: refresh Context and use `work remove` only when the CLI
recognizes a clean released-ready residue whose Head equals its registered base.

Context never grants authority by itself. Mutation commands must sync and verify
again internally. Offline Context is useful for inspection only, not mutation.

For ambiguous mutation results, inspect `status` and verified `log`, then run
`sync --json`. A no-remote Project still uses `sync` to verify local main,
reconcile exact published, deterministically failed, and unresolved pending
Operations, and advance its local checkpoint monotonically.

If the directory is not registered, use the CLI-provided clone remediation or
report it. Do not discover protocol state by parsing `.git`, State, refs, or
repository-controlled executables.
