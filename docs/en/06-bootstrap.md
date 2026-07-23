# The agent `bootstrap` entry point

[中文](../cn/06-智能体启动入口.md) | English

`bootstrap` verifies a credential and returns its current identity, role policy, capabilities, contexts, and candidate actions.

```text
chassiss --json \
  --root /path/to/project \
  --credential /path/to/credential.yaml \
  bootstrap
```

Every agent session starts here.

## Result fields

The current schema is `chassiss.bootstrap/v3`.

| Field | Purpose |
| --- | --- |
| `schema_version` | Bootstrap envelope version |
| `binary_version` | CLI version |
| `project_root` | Resolved project root |
| `state_revision` | Current workflow revision |
| `trust_revision` | Current authorization revision |
| `principal` | Verified actor, role, actions, and scopes |
| `policy` | Role Policy version, digest, and invariants |
| `capabilities` | Commands callable by this credential |
| `available_actions` | Candidate mutations for current State |
| `context_requests` | Context to read before acting |
| `refresh_on` | Conditions that require another bootstrap |

## Principal

`principal` comes from the credential and matching Trust grant. It includes credential ID, actor, role, actions, resources, validity window, and persistence metadata.

An agent cannot pass a role to `bootstrap`. The CLI derives it from the credential.

## Policy

Role Policy version is 3. Its digest binds the command-to-role mapping and common invariants.

- Trusted CLI, signed events, current Trust, and projected State are authoritative
- Agents never edit `.chassis`
- `available_actions` is bound to the current revision
- Every command performs authorization and state validation again
- Conflicts, rejection, Trust changes, and credential rotation require refresh

## Capabilities

Each capability describes command usage, summary, accepted values and flags, and whether the command mutates state.

Agents use this schema for command discovery. Human documentation may lag a binary, while the binary capability schema remains authoritative.

## Available actions

Actions are filtered by role, actor, credential actions, resource scope, State, and Trust. Each item may contain argv, resource, reason, required inputs, optional inputs, and an optional marker.

For example, a Developer `work.check` action includes `--all`. A `work.submit` action describes the required handoff and optional message.

An action is a candidate for one revision. Another actor may advance State or revoke the credential before execution.

## Context requests

| Role | Common context |
| --- | --- |
| Designer | Rejected artifact content |
| Orchestrator | Active Mission and Task states |
| Developer | Complete owned Task package |
| Reviewer | Exact pending Submission |
| Master | Pending artifact and Mission evidence |

Run these argv requests before changing a resource or recording a verdict.

## Revision CAS

Use the latest `state_revision` for a mutation.

```text
chassiss --expect-revision 12 \
  --credential /path/to/credential.yaml \
  work check M001-T001 --all
```

`CHS-CONFLICT-REVISION` is retryable only after another `bootstrap` and a new decision. Authorization mutations use `--expect-trust-revision`.

## Refresh conditions

Refresh at least after these events.

- Successful mutation
- State or Trust revision conflict
- CLI rejection
- Artifact rejection
- Credential rotation or revocation
- New Mission, Task, or Submission selection
- Session wakeup or restart

Long-running agents should never cache old `available_actions`.

## Legacy `next`

`next --role` produces an unauthenticated role projection. It is useful for diagnostics, but it cannot represent one credential's real scope.

Agent automation uses `bootstrap`.

## Recommended loop

```text
bootstrap
  |
  v
read context_requests
  |
  v
select available_action
  |
  v
execute with state_revision
  |
  v
bootstrap again
```

If no forward action is available, read current context and error evidence, then send the block to Master when necessary.

