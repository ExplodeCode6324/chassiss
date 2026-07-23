# Crash recovery and integrity blocking

[中文](../cn/14-崩溃恢复与完整性阻断.md) | English

Some operations cross Git, control state, credential files, or a remote repository. A process can exit at any write boundary. Journals preserve the exact pre-state and intended post-state so `recover` can make a deterministic decision.

Recovery never guesses a new Git result or user intent.

## Journal classes

| Directory | Boundary |
| --- | --- |
| `.chassis/operations/` | Git and workflow events |
| `.chassis/auth-operations/` | Credential files and Trust |
| `.chassis/publish-operations/` | Remote branches and publication events |

Journal files use JSON and mode `0600`.

## Workflow Git operation

```text
prepared
   |
   v
git_applied
   |
   v
state_committed
```

The journal stores Git pre-state, intent, exact expected Git post-state, and a prepared signed event. This mechanism covers artifact acceptance, worktree open and release, work submit, Integration, and Owner changes.

## Authorization operation

```text
prepared
   |
   v
credential_prepared
   |
   v
trust_committed
   |
   v
credential_published
```

Recovery requires exact credential ID, key, file digest, and Trust revision evidence.

## Publish operation

```text
prepared
   |
   v
remote_applied
   |
   v
state_committed
```

The journal binds remote name, URL digest, branch, previous Head, target Head, and signed event.

## Run recovery

Use `recover` after `CHS-OPERATION-RECOVERY-REQUIRED`, an interrupted write, a host restart, or an uncertain remote push.

```sh
chassiss --root /path/to/project recover
```

The command obtains the project lock and validates Config, Trust, events, State, and all journals.

## Deterministic outcomes

| Observed state | Recovery |
| --- | --- |
| Exact journal pre-state | Cancel a safe untouched plan or complete a fixed worktree or Integration plan |
| Exact journal post-state | Commit the already prepared signed event |
| Exact event already stored | Rebuild or confirm State and remove the journal |

Auth and Publish compare Trust plus credential files, or remote Head plus endpoint digest.

## `CHS-INTEGRITY-BLOCKED`

Automatic recovery stops when observed state matches neither permitted end.

Common causes include a manually moved formal branch, replaced worktree, edited journal or event, old Trust copy, changed remote URL, unexpected remote Head, or missing Git object.

Version 0.3 has no force bypass for this condition.

## Response to an integrity block

1. Stop every CHASSISS write
2. Preserve `.chassis`, Git refs, reflog, and remote state
3. Run read-only `doctor --json` and `verify --json`
4. Record code, diagnostic category, and journal ID
5. Let Master compare exact pre-state and post-state
6. Avoid reset, force push, and file deletion

## Safe retry

After a network or lock error, check for recovery requirements before replaying the command. Run `bootstrap` after successful recovery because the original action may already be complete.

Recovery does not create substitute business facts. It only completes an already fixed signed event, confirms committed State, or cancels a provably untouched preparation.

## Next steps

- [Error handling and troubleshooting](18-troubleshooting.md)
- [`.chassis`, events, and State projection](13-control-state.md)
- [Test record and known issues](22-testing-and-improvements.md)

