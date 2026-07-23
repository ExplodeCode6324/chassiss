# Error handling and troubleshooting

[中文](../cn/18-错误处理与故障排查.md) | English

CHASSISS errors include a stable code, process exit code, retryable flag, remediation, and optional diagnostic category. Automation reads JSON. Humans can use `explain`.

```sh
chassiss explain CHS-CONFLICT-REVISION
```

## Response order

1. Stop the current write
2. Preserve the complete JSON error
3. Run `bootstrap`
4. Read requested context
5. Run `recover` if required
6. Run `doctor` and `verify`
7. Reconsider the action with the new revision

Never edit `.chassis` or Git refs after an integrity error.

## Exit-code classes

| Exit code | Common meaning |
| --- | --- |
| 1 | Unclassified internal error |
| 10 | Resource state rejects the action |
| 11 | Identity, role, or scope rejection |
| 12 | Revision or Git conflict |
| 20 | Invalid command input |
| 30 | Temporary external-system failure |
| 40 | Integrity or protocol failure |

Use the stable error code for exact behavior.

## Authentication

| Code | Check |
| --- | --- |
| `CHS-AUTH-MISSING` | `--credential` is present |
| `CHS-AUTH-CREDENTIAL` | Project, metadata, policy, and key match |
| `CHS-AUTH-REVOKED` | Master revoked the grant |
| `CHS-AUTH-NOT-YET-VALID` | Validity start is in the future |
| `CHS-AUTH-EXPIRED` | Credential expired |
| `CHS-AUTH-DENIED` | Role and action allow the command |
| `CHS-AUTH-RESOURCE` | Resource scope covers the target |

Do not edit a credential to extend or widen it. Master issues a new least-privilege credential.

Diagnostic categories include `grant_not_found`, `revoked`, `metadata_mismatch`, `policy_mismatch`, `key_invalid`, `key_mismatch`, `signature_invalid`, and `project_mismatch`.

## Revision conflicts

`CHS-CONFLICT-REVISION` means State changed after it was read. `CHS-CONFLICT-TRUST-REVISION` means authorization changed.

Refresh bootstrap, read target context, confirm the action is still needed, then use the new revision.

## Project lock

`CHS-CONFLICT-LOCKED` means another process owns the kernel lock. Wait for that command. Never use lock-file age as stale evidence.

## Scope and budget

| Code | Response |
| --- | --- |
| `CHS-WORK-SCOPE` | Move out-of-scope work to another Task |
| `CHS-WORK-BUDGET-FILES` | Split by file boundary |
| `CHS-WORK-BUDGET-LINES` | Reduce delivery or replace the Task |
| `CHS-WORK-BUDGET-COMMITS` | Create a clean replacement Task |
| `CHS-TASK-PATH-CONFLICT` | Wait or replan paths |
| `CHS-TASK-WIP` | Complete, release, or block existing work |

Frozen budgets do not widen during execution.

## Failed Checks

Read Check ID, exit code, and output tail. Inspect current Head, `cwd`, dependencies, timeout, declared environment, verification sources, and snapshot binding.

Fix the worktree and rerun `work check`. A failure during Review or Integration requires a new controlled Submission path.

## Worktree damage

Stop using a moved, replaced, or wrong-branch worktree. Run `work status`, `doctor`, and `verify`, then `recover` when required. Do not create a directory that imitates the old binding.

## Review and Integration

| Code | Response |
| --- | --- |
| `CHS-REVIEW-INDEPENDENCE` | Use a different Reviewer actor |
| `CHS-INTEGRATION-NOT-APPROVED` | Inspect Task and verdict |
| `CHS-INTEGRATION-REVIEW` | Use the approving Reviewer actor |
| `CHS-INTEGRATION-HEAD-MOVED` | Submit and review the new Head |
| `CHS-INTEGRATION-BASELINE-MOVED` | Verify and avoid force |
| `CHS-INTEGRATION-CONFLICT` | Resolve through controlled Task work |
| `CHS-INTEGRATION-CHECKS` | Fix and produce a new Submission |

## Publish

| Code | Response |
| --- | --- |
| `CHS-PUBLISH-DIRTY` | Remove or formally apply local changes |
| `CHS-PUBLISH-BASELINE` | Compare default branch with State |
| `CHS-PUBLISH-NON-FAST-FORWARD` | Bring remote history through a Task |
| `CHS-PUBLISH-PUSH` | Recover before retry |
| `CHS-PUBLISH-REMOTE-UNSAFE` | Use a safe Git URL |
| `CHS-PUBLISH-REMOTE-MISMATCH` | Stop and inspect the remote |

Publish never force-pushes.

## Integrity errors

`CHS-INTEGRITY-EVENTS`, `CHS-INTEGRITY-TRUST`, `CHS-STATE-INVALID`, and `CHS-INTEGRITY-BLOCKED` mean control evidence failed verification.

Preserve the JSON error, a read-only `.chassis` copy, Git status and reflog, doctor and verify output, journal ID, and last successful command. Do not delete journals, rewrite events, overwrite Trust, or reset the formal branch.

## Legacy schema

`CHS-SCHEMA-UNSUPPORTED` rejects a project outside the current API V2 schemas. Version 0.3 has no in-place migrator. Keep a compatible CLI for old control state or create a verified new V2 control end.

## Issue reports

Remove Root, credentials, tokens, remote URLs, and private source. Include version, platform, command shape, stable error code, minimal reproduction, expected and actual behavior, and journal presence.

## Next steps

- [Crash recovery and integrity blocking](14-recovery-and-integrity.md)
- [Security model and known risks](19-security-model.md)
- [Test record and known issues](22-testing-and-improvements.md)

