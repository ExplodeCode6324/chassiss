# Concurrency, locks, and state conflicts

[中文](../cn/12-并发锁与状态冲突.md) | English

Agents may read concurrently and may develop non-overlapping Tasks in parallel. Controlled writes are serialized by an advisory lock, revision CAS, WIP limits, path-conflict checks, and operation journals.

## Project advisory lock

Every control-state mutation obtains the OS advisory lock associated with `.chassis/lock`.

Lock ownership belongs to an open file descriptor and kernel state. The lock file may remain on disk permanently. File age, process names, and text content do not prove that ownership is stale.

An abnormal process exit releases the kernel lock. The next write still checks for unfinished journals.

## State revision CAS

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/orchestrator.cred \
  --expect-revision 42 \
  task assign M001-T001 --owner developer-a
```

If another actor advances State first, the command returns `CHS-CONFLICT-REVISION`. Refresh `bootstrap`, read context, and reconsider the action.

Removing `--expect-revision` and replaying an old decision is unsafe.

## Trust revision CAS

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/master-root.yaml \
  --expect-trust-revision 7 \
  auth issue --role developer --actor developer-b
```

Issuance, revocation, and authorization recovery advance Trust revision. Master reloads grants after a conflict.

## WIP limit

The default project limit is two active Tasks. WIP uses actor and Task state, not session count.

Completing, releasing, or blocking a Task can free capacity. A blocked Task counts as inactive WIP while retaining its write scope, which can prevent an overlapping Task from starting.

## Path conflicts

```text
Task A  src/api/**
Task B  src/api/router.go
```

These scopes overlap.

```text
Task A  src/api/**
Task B  docs/**
```

These scopes can run in parallel. Integration still tests each candidate against the latest formal baseline.

Overlap detection is conservative. Designer should refine boundaries when the CLI cannot prove that patterns are disjoint.

## Git and control-state boundary

Git and `.chassis` cannot form one database transaction. CHASSISS journals the exact pre-state, intended Git operation, exact post-state, and signed event.

```text
acquire lock
    |
validate revisions and resources
    |
write prepared journal
    |
apply Git or filesystem operation
    |
commit event and State
    |
finish journal
    |
release lock
```

A later write requires recovery if the process exits mid-operation.

## Safe automation retry

1. Run `bootstrap --json`
2. Read `available_actions`
3. Fetch required context
4. Select one action
5. Execute it with the current revision
6. Return to bootstrap after success or conflict
7. Stop and notify Master after an integrity error

Read operations may poll frequently. Claim, approval, Integration, Publish, and Auth operations cannot replay stale output.

## Conflict response

| Condition | Response |
| --- | --- |
| Lock held | Wait for the current command |
| State revision conflict | Bootstrap and read context again |
| Trust revision conflict | Master reloads grants |
| WIP full | Complete, release, or block existing work |
| Path overlap | Replan or wait for prior Integration |
| Formal baseline advanced | Repeat Integration preflight |
| Pending journal | Run `recover` |
| Integrity blocked | Stop writes and notify Master |

## Next steps

- [`.chassis`, events, and State projection](13-control-state.md)
- [Crash recovery and integrity blocking](14-recovery-and-integrity.md)
- [Examples, automation, and FAQ](21-examples-automation-faq.md)
