# What CHASSISS is

[中文](../cn/01-项目概述.md) | English

CHASSISS is a CLI-controlled software development workflow for multiple agents. It gives requirements, design, tasks, implementation, review, integration, and acceptance one consistent state and authorization model.

Agents handle work that requires semantic judgment. The CLI handles deterministic enforcement. Formal changes are backed by signed events, state validation, and Git evidence.

## Design goals

CHASSISS addresses the following problems.

- Agents disagree about their roles or current tasks
- An agent edits files outside its Task scope
- An author accepts their own design or implementation
- Parallel Tasks write to overlapping paths
- Check output no longer matches the submitted content
- Git changes while workflow state remains uncommitted
- A revoked credential remains active in an old session
- A process exits without a verifiable recovery path

The workflow assumes cooperative agents that may still make mistakes. It does not isolate malicious processes running as the same operating-system user. It also does not replace OS permissions, a code sandbox, or human quality judgment.

## Responsibilities

| Participant | Main responsibility |
| --- | --- |
| Master | Keeps the Master Root, issues credentials, accepts designs and Mission completion |
| Designer | Writes Requirements, Architecture, Mission, and Task artifacts |
| Orchestrator | Activates Missions and schedules Tasks |
| Developer | Implements, checks, and submits work in a Task worktree |
| Reviewer | Independently reviews Submissions and performs integration |
| Owner | Applies direct human maintenance during a quiescent project state |
| CLI | Verifies identity, state, scope, budget, evidence, concurrency, and recovery |

The Master Root represents project root trust. Every other role uses a separate credential. Role identity comes from the credential and current Trust store.

## Control plane and content plane

```text
project/
├── .chassis/        CLI control state
├── docs/            controlled project artifacts
└── source files     ordinary project content
```

`.chassis/` stores configuration, Trust, events, State projection, operation journals, Submissions, and worktree bindings. Only the CLI manages this directory.

`docs/` stores Requirements, Architecture, Mission, and Task artifacts. Designer writes them and Master accepts their exact digests.

Developers change ordinary project content in Task worktrees. A human may also apply ordinary changes through the Owner workflow when the project is quiescent.

## Sources of authority

When the same information appears in multiple places, use this order.

1. Protocols, role policies, and validators in the trusted CLI
2. Signed Trust metadata and the event chain
3. State rebuilt from the event chain
4. Accepted project artifacts
5. The agent Skill
6. Human-facing documentation

`bootstrap` returns the current credential identity, capabilities, context requests, and candidate actions. Every write command revalidates its preconditions.

## Git and version synchronization

CHASSISS does not depend on GitHub or GitLab issue and review objects. Version 0.3 uses local Git for baselines, Task branches, worktrees, diffs, and integration, so Git is currently required at runtime.

`publish` can send the exact formal baseline to GitHub, GitLab, or any normal Git remote. It does not synchronize `.chassis/`. Independent clones cannot act as equivalent control ends.

## Project phases

| Phase | Meaning |
| --- | --- |
| `design` | Requirements and Architecture are being established or new planning is pending |
| `execution` | One Mission is active, blocked, or awaiting acceptance |
| `idle` | No Mission is running, so Owner maintenance or new planning can begin |

Only one Mission may be active. Tasks may run in parallel within WIP and path-overlap limits.

## Next steps

- [Five-minute quickstart](02-quickstart.md)
- [Recommended agent topology](03-agent-topology.md)
- [Security model and known risks](19-security-model.md)
- [`.chassis`, events, and State projection](13-control-state.md)
