# CLI and structured errors

Every command supports `--json`. Success and failure both emit one
`chassiss.cli/v1` envelope:

```text
ok, command, project, snapshot, identity, operation, result,
available_actions, warnings, error, extensions
```

Use `chassiss help --json` for the complete machine-readable command tree and
`chassiss help task start --json` for one command. Arguments, options, and
possible-errors fields are always arrays.

An Error always contains:

```text
code, category, message, retryable, current_head,
operation_id, details, remediation[]
```

Exit codes:

| code | category |
|---:|---|
| 0 | success |
| 2 | usage |
| 3 | local |
| 4 | trust/protocol |
| 5 | authorization |
| 6 | validation/conflict |
| 7 | check/review |
| 8 | network |
| 9 | unsupported/internal |

Agents execute a `remediation[].argv` only when it matches Master's goal and
never turn it into a shell string. Capability, Scope, Root, rollback, Task
conflict, and relevant-drift failures must be reported, not evaded by changing a
key, remote, or Operation ID.

See the [CLI specification](../06-cli-command-specification.md) and
[JSON API contract](../10-cli-json-api-and-errors.md).

