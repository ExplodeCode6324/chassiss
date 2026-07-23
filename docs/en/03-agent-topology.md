# Recommended agent topology

[中文](../cn/03-智能体协作拓扑.md) | English

CHASSISS represents identity with an actor and a role. An actor identifies a stable principal. A role limits the capabilities used by that principal in one command. One agent may hold multiple credentials, while each command uses one credential.

## Minimum topology

The recommended setup has two resident agents and one isolated Designer session.

```text
Master <-> Designer session
              |
              v
Build Agent ------> Review Agent
O + D               Reviewer
```

The Build Agent holds Orchestrator and Developer credentials with the same actor. The Review Agent uses a different actor.

Designer planning stays in a separate session. The same underlying agent may run the Designer and Build sessions at different times, but their contexts should not be mixed.

## Role boundaries

### Master

Master uses the Master Root.

- Issues and revokes role credentials
- Accepts or rejects artifacts
- Accepts Mission completion
- Cancels Tasks that will not continue
- Publishes when needed
- Reads Owner history

Master Root cannot run `owner apply`.

### Designer

- Obtains current templates from the CLI
- Writes Requirements, Architecture, Mission, and Task artifacts
- Maintains dependency and digest consistency
- Defines Task paths, budgets, and Checks
- Reviews design consistency from candidate diffs supplied by Build or Review
- Creates replacement Tasks when contracts change

Designer cannot accept its own artifacts.

### Orchestrator

- Activates, blocks, and resumes Missions
- Claims, assigns, blocks, resumes, releases, and supersedes Tasks
- Submits Mission acceptance evidence
- Publishes the formal baseline

Orchestrator cannot approve a Developer Submission.

### Developer

- Opens the controlled Task worktree
- Reads the frozen Task package and change requests
- Inspects status and diff
- Runs Checks
- Records progress notes
- Creates immutable Submissions through `work submit`
- Blocks owned work when safe completion is impossible

Developer cannot modify paths outside `allowed_paths`.

### Reviewer

- Reads the Submission, Task, file set, and diff
- Performs mechanical validation
- Records approve or request-changes verdicts
- Integrates the exact Submission it approved

Reviewer actor must differ from Submission actor.

### Owner

- Works only while the project is quiescent
- Applies ordinary project-file changes
- Lets the CLI create one formal commit
- Produces signed audit evidence

Only one unrevoked Owner grant may exist.

## Dual-role Build Agent

Use one actor with two explicitly named credential files.

```text
auth issue --actor build-1 --role orchestrator \
  --output ~/.chassiss/cred-build-orchestrator.yaml
auth issue --actor build-1 --role developer \
  --output ~/.chassiss/cred-build-developer.yaml
```

Orchestrator may run `task claim` when the same actor also has an active Developer grant. It may also assign explicitly with `task assign M001-T001 --owner build-1`.

Select the credential for each responsibility so events record the correct role.

## Multiple Developers

Large Missions may use several Developer actors. Parallel execution still requires all of the following.

- Project WIP remains below its limit
- Task dependencies are satisfied
- `allowed_paths` do not overlap active or blocked Tasks
- Every Task uses its own worktree
- Reviewer actor differs from each author

The default project WIP limit is 2. Version 0.3 does not expose an initialization flag for WIP.

## Session inputs

Each session needs the following minimum context.

- Project path
- Role assignment
- The matching credential armor
- Installed CHASSISS Skill

After import, the agent runs `bootstrap`. Master does not need to duplicate the complete role policy in the prompt.

## Experimental autonomous Master

A capable agent may act for the human Master, issue credentials, and create subagents. This topology is useful for workflow and model experiments.

Root and role credentials usually enter one trust domain, which disables secret isolation. Quality still depends on model capability, context management, tests, and human sampling.

Record model version, prompts, session topology, Task count, failures, and recovery steps. These details make successful and failed issue reports reproducible.

## Scheduling

Long-running Build and Review agents may be awakened every five minutes by cron or an automation service. A wakeup only starts a session. The agent still begins with `bootstrap`.

```text
*/5 * * * * cd /path/to/project && git fetch --prune && /path/to/wake-build
*/5 * * * * cd /path/to/project && git fetch --prune && /path/to/wake-review
```

`git fetch` updates remote references. Workflow authority remains in the single controlled `.chassis` instance.
