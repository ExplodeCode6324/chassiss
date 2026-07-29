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
- whenever the CLI's remediation requests it.

Context never grants authority by itself. Mutation commands must sync and verify
again internally. Offline Context is useful for inspection only, not mutation.

If the directory is not registered, use the CLI-provided clone remediation or
report it. Do not discover protocol state by parsing `.git`, State, refs, or
repository-controlled executables.
