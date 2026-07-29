# CHASSISS

[中文](README.md) | English

CHASSISS is a multi-agent project protocol that uses signed Git transitions as
its shared ledger and gives its CLI exclusive ownership of the supported Git
workflow. Architecture describes durable structure, a Taskbook describes one
round of work, and State contains only the projection needed to verify the next
transition. Root and Grants determine which Actor and Key may perform each
Action over which Tasks and Resources.

This repository is a new `chassiss/v1` implementation. It is not compatible with
the old project's v0.x control directory, credentials, or Mission model. The old
repository was used read-only as a documentation-structure reference. The
protocol documents still await Master's review, so this is a lock candidate,
not a frozen v1 release.

## Quick Start

This section follows the way a human coordinates a multi-Agent project. In the
examples, `chassiss` means the manifest-digest-verified CLI shipped with the
trusted Skill. Keep private keys, requests, and document drafts outside the
project directory.

> **One-time v1 bootstrap constraint:** Agents may submit public keys and Grant
> Requests first, but a Grant cannot enter the project ledger before Genesis.
> Genesis currently requires the first Architecture and Taskbook. For the first
> project setup, complete the human decisions in steps 0–4, then run `init`
> followed by `grant add` in the order shown in step 4. Authorizing an
> Architecture Agent in the ledger before the architecture discussion would
> require a new Root-only bootstrap protocol state, not merely a different
> command order.

### 0. Prepare the project

Create a project directory with no Git history and choose a stable Project ID,
such as `PRJ-EXAMPLE`. Do not run `git init` manually. The `chassiss init`
command in step 4 initializes Git, creates Genesis, and records the first
contracts.

### 1. Generate Root

The human project owner generates a Root key outside the project:

```text
chassiss key generate --id KEY-ROOT-01 --actor master --json
```

Root is the project's final authority. The current implementation permits the
same Root key to initialize several projects, while every project anchors Root,
maintains Grants, and verifies history independently. Reuse reduces key
management but expands the impact of one private-key compromise to every
project using it. Until Root rotation and higher-level delegation exist,
important projects should use separate Roots by default.

### 2. Collect every Agent's public key

Ask the Architecture, coding, Review, and Integration Agents to generate
separate keys outside the project and return proof-of-possession-signed Grant
Requests. A Request contains the public key, never the private key. Do not rely
on a bare public key that proves no possession.

```text
chassiss key generate --id KEY-AGENT-01 --actor agent-one --json
chassiss grant request \
  --project PRJ-EXAMPLE \
  --key KEY-AGENT-01 \
  --profile developer \
  --task-scope 'TASK-*' \
  --resource-scope 'module:*' \
  --limits bounded \
  --output /outside/project/agent-one-request.json \
  --json
```

### 3. Review authorization

The human owner checks each Agent's identity, public key, Capabilities, Task
scope, Resource scope, and limits, and creates an authorization plan. Root
approves only the authority needed for the current duty. Coding and Review use
different Keys and Grants; temporary sub-Agents also receive isolated working
directories, Keys, and Grants.

For the initial setup, finish the review now but do not fabricate a ledger entry
before Genesis. Publish the signed Grants immediately after Genesis in the next
step.

### 4. Discuss requirements and architecture, then establish the ledger

The human discusses requirements, durable architecture, acceptance criteria,
and task decomposition with the intended Architecture Agent. Produce drafts
outside the project from:

- [`docs/templates/architecture.yaml`](docs/templates/architecture.yaml)
- [`docs/templates/taskbook.yaml`](docs/templates/taskbook.yaml)

After human approval, Root creates Genesis, publishes each approved Grant, and
performs a full verification:

```text
chassiss init \
  --project PRJ-EXAMPLE \
  --architecture /outside/project/architecture.yaml \
  --taskbook /outside/project/taskbook.yaml \
  --root-key KEY-ROOT-01 \
  --json

chassiss grant add \
  --request /outside/project/agent-one-request.json \
  --grant-id GRT-AGENT-01 \
  --root-key KEY-ROOT-01 \
  --profile developer \
  --task-scope 'TASK-*' \
  --resource-scope 'module:*' \
  --limits bounded \
  --json

chassiss verify --full --json
chassiss context --json
```

Use the separately reviewed explicit Capabilities, scopes, and limits for each
duty; `developer` above only demonstrates a coding Agent. See
[Trust, keys, and Grants](docs/en/03-trust-keys-and-grants.md) for the complete
authorization flow.

### 5. Start the coding and Review Agents

Master assigns a Task to an Agent holding the corresponding coding Grant. The
Agent edits ordinary files only inside the CLI-managed worktree and within the
Task's `writes`:

```text
chassiss context TASK-001 --json
chassiss task start TASK-001 --json
chassiss work open TASK-001 --json
# Edit only in the returned managed worktree.
chassiss work status TASK-001 --json
chassiss work commit TASK-001 --message "implement TASK-001" --json
chassiss check TASK-001 --json
chassiss submit TASK-001 --json
```

An independent Agent with Review authority then uses `review --prepare` to
create a Review Report bound to the exact candidate, completes it, and signs the
Verdict. Only a candidate that passes Checks and receives approval goes to an
Agent with Integration authority for `integrate`.

### 6. Repeat until completion, then reclaim authority

Repeat step 5 for every Task. When all Tasks are terminal, a Reviewer completes
the Closure Report and runs `taskbook archive`. The human owner then runs
`grant revoke` for the round's temporary Grants; each key owner or the Master
orchestrator removes temporary private keys and isolated work directories.

Reclaim a successful temporary Agent immediately. On failure, first use
`attempt abandon` to sign the problem into State/history, then revoke its Grant,
delete its Key, and destroy its work directory. Historical signatures remain
auditable, while a revoked Grant cannot authorize future Transitions.

Do not directly run Git-mutating commands such as add, commit, branch, checkout,
worktree, merge, rebase, reset, push, or config. Reusable Agent instructions
live in [`skills/chassiss/`](skills/chassiss/), which bundles macOS/Linux
arm64/amd64 CLIs.

## Core boundaries

- `refs/heads/main` is the sole authoritative shared mainline; every main commit
  is an SSH-signed Transition.
- `.chassiss/state.json` is reducer-generated canonical JSON and must not be
  edited manually.
- `docs/architecture.yaml` and the active `docs/taskbook.yaml` change only
  through controlled commands.
- Agents edit ordinary files only inside a CLI-managed worktree and within the
  Task's declared `writes`.
- Check Results are mechanical evidence; a Review Verdict is a Reviewer's
  semantic judgment.
- Integration is a two-parent commit for the exact candidate. Relevant or
  unknown drift requires a new Review.
- Private keys, checkpoints, pending operations, worktree registrations, and
  caches remain in external Local State.
- The protocol does not depend on GitHub, GitLab, an external database, external
  CI, or a particular hosting provider.

## Documentation

- [Normative specification index](docs/README.md)
- [English guides](docs/en/README.md)
- [中文使用指南](docs/cn/README.md)
- [Implementation and legacy differences](docs/12-implementation-differences.md)
- [Cross-implementation fixtures](fixtures/README.md)

Use `chassiss help --json` as the machine-readable command source. Stable Error
codes, exit codes, and response envelopes are defined by the normative specs.

## Verification status

Local tests cover canonical JSON, domain-separated digests, strict YAML, State
invariants, reducers, SSHSIG, Git objects and topology, historical verification,
Local State, Task lifecycle, Review/Integration, Taskbook Closure, offline
proposals, and Owner Apply. Fourteen cross-implementation fixture categories are
present.

At Master's request, this pass did not perform real GitHub/network remote/system
secret-store integration tests. Remaining lock-candidate gaps and their security
impact are explicitly tracked in the
[implementation differences](docs/12-implementation-differences.md).
