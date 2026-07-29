# Master orchestration

Use this contract only when a Master Agent creates temporary subagents for
CHASSISS Tasks.

## Isolation unit

Treat every Task attempt as a disposable isolation unit. Before dispatch, Master
must allocate:

- one unique Actor, Ed25519 Key, and narrowly scoped current Grant;
- one CLI-managed worktree returned by `task start`;
- one dedicated temporary parent directory containing only that Agent's working
  material, reports, and non-secret scratch files.

Never share a worktree, Key, Grant, mutable checkout, or scratch directory
between concurrent subagents. Keep Master's default workspace and Root Key out
of every subagent directory. A directory boundary is workflow isolation, not an
operating-system security sandbox; use stronger process/container isolation when
subagents are not mutually trusted.

Generate the Agent Key before entering the Project, grant only the exact Task and
Resources required, then attach it explicitly:

```text
chassiss key generate --id <agent-key> --actor <unique-actor> --json
chassiss grant request ... --key <agent-key> --task-scope <task> ...
chassiss grant add ... --grant-id <agent-grant> --root-key <root-key> ...
chassiss key attach <agent-key> --select --json
chassiss task start <task> --key <agent-key> --grant <agent-grant> --json
```

Pass only the returned worktree path, Agent Key ID, Grant ID, Task ID, and
current Context to the subagent. Require explicit `--key` and `--grant` on
signed Task mutations; never let a parallel workflow rely on an implicitly
selected identity. The exact signer is returned in `operation.signer`.

## Reclaim on success

Integration removes the successful managed worktree and Work Ref. Master then
immediately reclaims authority and local secret material:

```text
chassiss grant revoke <agent-grant> --reason <reason> --root-key <root-key> --json
chassiss key remove <agent-key> --yes --json
```

Confirm the Grant is absent, the Key handle is gone, and the temporary parent
directory contains no retained material before deleting it. Do not reuse an
Actor/Key/Grant tuple for another attempt.

## Reclaim on failure

Do not delete the failed worktree first. Master records the failure and observed
Work Head/tree in signed history; the command also appends only a compact index
to State and then force-removes the failed managed worktree and Work Ref:

```text
chassiss attempt abandon <task> \
  --root-key <root-key> \
  --agent-key <agent-key> \
  --agent-grant <agent-grant> \
  --code <stable-code> \
  --summary <summary> \
  --reason <reason> \
  --json
```

After that Transition succeeds, revoke the Grant, remove the Key, and delete the
temporary parent directory. If cleanup returns a warning, reconcile the named
local artifact before deleting anything else. Inspect retained audit material
with:

```text
chassiss attempt failures <task> --json
chassiss attempt failures <task> --operation <operation-id> --json
```

Never preserve a private key or failed worktree merely for audit. Signed history
contains the full failure record; State contains only the CLI-resolvable index.
