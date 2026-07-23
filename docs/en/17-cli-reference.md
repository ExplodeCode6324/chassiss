# CLI command reference

[中文](../cn/17-命令行参考.md) | English

This chapter lists CHASSISS 0.3 commands by lifecycle. Agents should use `bootstrap --json` capabilities and available actions as the current machine-readable reference.

## Invocation

```text
chassiss [global-options] <group> <action> [arguments]
```

Global options must appear before the command group.

| Option | Purpose |
| --- | --- |
| `--root <path>` | Project root |
| `--credential <file>` | Root or role credential |
| `--json` | Stable agent-readable envelope |
| `--dry-run` | Preview marker, unsupported for mutations |
| `--expect-revision <n>` | State revision CAS |
| `--expect-trust-revision <n>` | Trust revision CAS |

Without `--root`, the CLI searches upward from the current directory for `.chassis`.

Transactional dry-run is not implemented. A write command with `--dry-run` returns `CHS-DRY-RUN-UNSUPPORTED`.

## Standalone commands

| Command | Purpose |
| --- | --- |
| `help`, `--help`, `-h` | Command overview |
| `version`, `--version` | CLI version |
| `bootstrap` | Verified identity, policy, capabilities, contexts, and actions |
| `status` | Project summary |
| `next --role <role> [--actor <actor>]` | Legacy unauthenticated role projection |
| `doctor` | Project integrity and Git health |
| `verify` | Integrity and optional credential anchor |
| `recover` | Deterministic journal recovery |
| `explain <error-code>` | Stable error explanation |

`status`, `next`, `doctor`, `verify`, `recover`, and `explain` can run without a role credential. `verify` checks an optional credential anchor. `bootstrap` requires an actual identity.

## `auth`

```text
auth master-init [--output <root-file-or-directory>]
auth inspect [credential-path]
auth issue --actor <actor> --role <role> [scope-options]
auth export <credential-path>
auth import --output <credential-path>
auth revoke <credential-id> [--reason <text>]
```

`auth issue` supports these options.

```text
--master-root <path>
--output <path>
--actions <a,b>
--not-before <RFC3339>
--expires-at <RFC3339>
--ttl-seconds <n>
--projects <id,...>
--missions <id,...>
--tasks <id,...>
--submissions <id,...>
--submission-digests <digest,...>
--heads <commit,...>
--baselines <commit,...>
--persistent
```

Master may issue Designer, Orchestrator, Developer, Reviewer, and Owner. Master identity comes from Root.

Credentials are persistent when no expiration option is present. In version 0.3, `--persistent` only expresses that intention. Do not combine it with a TTL or expiration.

## `project`

```text
project init <path> [--existing]
  [--master-root <path>]
  [--max-changed-files <n>]
  [--max-diff-lines <n>]
  [--max-commits <n>]
```

Greenfield creates Git, `main`, `.gitignore`, and an initial commit. Brownfield adopts a clean Git root with at least one commit.

## `template`

Designer commands follow.

```text
template list
template get <requirements|architecture|mission|task>
  [--id <id>]
  [--output <project-path>]
```

Existing output files are never overwritten.

## `artifact`

```text
artifact check <path>
artifact submit <path>
artifact list [--pending]
artifact context <submission-id>
artifact accept <submission-id>
artifact reject <submission-id> --reason <text>
```

Designer checks and submits. Master accepts or rejects. Designer, Reviewer, and Master have scoped read commands according to policy.

## `mission`

```text
mission list
mission context <mission-id>
mission activate <mission-id>
mission block <mission-id> --reason <text>
mission resume <mission-id>
mission submit-acceptance <mission-id> --evidence <file-or-text>
mission accept <mission-id>
```

Orchestrator manages execution and submits evidence. Master accepts completion.

## `task`

```text
task list [--ready|--active|--blocked|--review]
task context <task-id>
task claim <task-id>
task assign <task-id> --owner <developer-actor>
task block <task-id> --reason <text>
task resume <task-id>
task release <task-id>
task cancel <task-id> --reason <text>
task supersede <task-id> --replacement <new-task-id>
```

Orchestrator claims, assigns, blocks, resumes, releases, and supersedes. Claim requires a Developer grant for the same actor. Master cancels.

## `work`

Developer commands follow.

```text
work open <task-id>
work context <task-id>
work status <task-id>
work diff <task-id>
work check <task-id> (--all|--id <check-id>)
work checkpoint <task-id> --file <checkpoint-file-or-text>
work submit <task-id> --file <handoff-file-or-text>
  [--message <summary>]
work block <task-id> --reason <text>
```

`work check` requires `--all` or one Check ID. `work submit` creates the Task commit from current worktree changes.

## `review`

```text
review list
review history [--task <task-id>] [--submission <submission-id>]
review context <submission-id>
review check <submission-id>
review approve <submission-id> --report <file-or-text>
review request-changes <submission-id> --report <file-or-text>
```

Reviewer lists, reads, checks, and records verdicts. Developer, Orchestrator, Reviewer, and Master may read filtered history within their scopes.

## `integrate`

The same Reviewer actor that approved the Submission runs these commands.

```text
integrate check <submission-id>
integrate apply <submission-id>
```

Apply revalidates approval, exact Head, formal baseline, Checks, and merged tree.

## `publish`

Orchestrator or Master uses the Git remote adapter.

```text
publish check --target <github|gitlab|remote-git>
  [--remote <name>]
  [--branch <name>]

publish apply --target <github|gitlab|remote-git>
  [--remote <name>]
  [--branch <name>]
```

Defaults are `origin` and the configured formal branch.

## `owner`

```text
owner apply --reason <text>
owner history
```

Owner applies and reads history. Master can also read history.

## JSON envelope

Successful JSON responses contain API version, command result, and revisions. Errors contain stable code, message, retryable flag, remediation, and optional diagnostic category.

Automation parses fields and never terminal formatting or natural-language paragraphs.

## Discover current commands

```sh
chassiss --json --root /path/to/project \
  --credential /path/to/credential \
  bootstrap
```

`capabilities` lists possible commands for the role. `available_actions` lists current candidates. Execution still revalidates every constraint.

## Next steps

- [The bootstrap entry point](06-bootstrap.md)
- [Error handling and troubleshooting](18-troubleshooting.md)
- [Complete development lifecycle](08-lifecycle.md)

