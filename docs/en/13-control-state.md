# `.chassis`, events, and State projection

[中文](../cn/13-控制目录事件与状态投影.md) | English

`.chassis/` is the local project control end. It stores Trust, events, State projection, Submissions, journals, and worktree bindings. A code repository alone cannot replace this state.

## Layout

Directories such as `worktrees/` appear when first needed.

```text
.chassis/
├── config.yaml
├── trust.yaml
├── state.yaml
├── lock
├── events/
├── operations/
├── auth-operations/
├── publish-operations/
├── submissions/
├── worktrees/
└── cache/
```

Controlled project artifacts live in project `docs/` paths recorded by State.

## `config.yaml`

Config records schema version, Project ID, content backend, formal branch, default Task budget, and Developer WIP limit.

The current Config version is 4 and backend is `local-git`. Manual edits break protocol and State bindings.

## `trust.yaml`

Trust stores the Root public key, Trust revision, grants, and revocations. Each grant binds actor, role, public key, validity, resources, and actions.

The complete Trust store is Root-signed. Never overwrite it with an old copy.

## `events/`

Events are the control-plane source of truth. Each event contains version, sequence, type, actor, resource, previous digest, payload, time, digest, and signature.

The CLI verifies continuous sequence, canonical digest, chain linkage, valid Root or role signature, role policy, minimal payload, and deterministic State transition.

Master events use the matching Root. Other events use a grant valid for the event.

Event files cannot be reordered, deleted, renamed, or edited.

## `state.yaml`

State is the current projection of the complete event chain. It contains revision, formal baseline, artifacts, Missions, Tasks, Submissions, Reviews, Publications, and Owner history.

State can be rebuilt from events. It cannot create a new workflow fact through manual editing.

## `submissions/`

This directory stores immutable Submission manifests. Their digests are referenced by Reviews and Integrations.

## Operation directories

`operations/` journals Git and workflow state. Typical phases are `prepared`, `git_applied`, and `state_committed`.

`auth-operations/` journals credential files and Trust. Phases include `prepared`, `credential_prepared`, `trust_committed`, and `credential_published`.

`publish-operations/` journals remote Head and publication events with phases `prepared`, `remote_applied`, and `state_committed`.

## `worktrees/` and `cache/`

Active Task worktrees live under `worktrees/`. The CLI binds their path, branch, Git identity, and digest.

`cache/` holds Integration candidates and reconstructible temporary content. Do not delete cache that may be referenced by an active operation.

## `lock`

The lock file is a stable advisory-lock entry. File existence and age do not indicate current ownership. Do not delete it.

## Permissions and Git ignore

Role credentials live outside project `.chassis`. Default credential directories use mode `0700` and files use `0600`.

Greenfield initialization adds `.chassis/` to `.gitignore`. Brownfield uses `.git/info/exclude`.

## Multi-machine boundary

A Git remote synchronizes the formal code baseline and does not synchronize Trust, events, State, journals, manifests, or worktree bindings.

Maintain one authoritative `.chassis`. Migrate it only while all writes are stopped and after copying complete control state and Git objects. Two copies must not write in parallel.

## Protected control data

Never manually modify or delete the following.

- Config, Trust, and State
- Events and all operation journals
- Submission manifests
- Active worktrees
- Cache referenced by active operations
- Lock

Use `doctor`, `verify`, and `recover`. Preserve the site and notify Master after an integrity block.

## Next steps

- [Crash recovery and integrity blocking](14-recovery-and-integrity.md)
- [Publishing and multi-machine collaboration](15-publish-and-multi-machine.md)
- [Protocol, versions, and compatibility](20-protocol-and-compatibility.md)

