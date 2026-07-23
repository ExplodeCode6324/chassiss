# Five-minute quickstart

[中文](../cn/02-五分钟快速开始.md) | English

This chapter shows the shortest complete path. It uses an isolated Designer session, one Build Agent with Orchestrator and Developer roles, and an independent Review Agent.

Run project commands from the project root unless `--root` is present. Global options must appear before the command group.

## Prepare the CLI and Skill

Install `skills/chassiss/` for every agent and select the bundled CLI that matches the host.

```text
skills/chassiss/bin/darwin-arm64/chassiss
skills/chassiss/bin/darwin-amd64/chassiss
skills/chassiss/bin/linux-arm64/chassiss
skills/chassiss/bin/linux-amd64/chassiss
```

The examples use `chassiss` as the selected binary name.

## Create Root and initialize the project

```text
chassiss auth master-init
chassiss --credential ~/.chassiss/master-root.yaml \
  project init /path/to/project
```

An existing project must be a clean Git root with at least one commit.

```text
chassiss --credential ~/.chassiss/master-root.yaml \
  project init /path/to/project --existing
```

New projects default to 100 changed files, 20000 diff lines, and 20 commits per Task. Override the defaults during initialization when needed.

```text
chassiss --credential ~/.chassiss/master-root.yaml \
  project init /path/to/project \
  --max-changed-files 50 \
  --max-diff-lines 10000 \
  --max-commits 10
```

## Issue role credentials

```text
cd /path/to/project

chassiss --credential ~/.chassiss/master-root.yaml auth issue \
  --actor designer-1 --role designer \
  --output ~/.chassiss/cred-designer-1.yaml

chassiss --credential ~/.chassiss/master-root.yaml auth issue \
  --actor build-1 --role orchestrator \
  --output ~/.chassiss/cred-build-orchestrator.yaml

chassiss --credential ~/.chassiss/master-root.yaml auth issue \
  --actor build-1 --role developer \
  --output ~/.chassiss/cred-build-developer.yaml

chassiss --credential ~/.chassiss/master-root.yaml auth issue \
  --actor reviewer-1 --role reviewer \
  --output ~/.chassiss/cred-reviewer-1.yaml

chassiss --credential ~/.chassiss/master-root.yaml auth issue \
  --actor human-owner --role owner \
  --output ~/.chassiss/cred-human-owner.yaml
```

The Build Agent credentials use the same actor. The Orchestrator can assign a Task to `build-1`, then the Developer credential continues the same Task.

## Distribute credentials

Master exports one armor block for each session.

```text
chassiss auth export ~/.chassiss/cred-designer-1.yaml
chassiss auth export ~/.chassiss/cred-build-orchestrator.yaml
chassiss auth export ~/.chassiss/cred-build-developer.yaml
chassiss auth export ~/.chassiss/cred-reviewer-1.yaml
```

Each export produces three lines. Send the Designer armor to the Designer session, both Build armors to the Build Agent, and the Reviewer armor to the Review Agent through a controlled channel.

The receiving agent imports the armor from standard input.

```text
chassiss auth import --output ~/.chassiss/my-credential.yaml
```

Use different output paths for the two Build credentials. Keep the Master Root in the human-controlled environment. `auth export` refuses a Master Root.

## Start the Designer

```text
chassiss --json --root /path/to/project \
  --credential ~/.chassiss/cred-designer-1.yaml bootstrap
```

Designer creates and submits artifacts in dependency order.

```text
chassiss --credential ~/.chassiss/cred-designer-1.yaml \
  template get requirements --output docs/requirements.md

chassiss --credential ~/.chassiss/cred-designer-1.yaml \
  artifact check docs/requirements.md

chassiss --credential ~/.chassiss/cred-designer-1.yaml \
  artifact submit docs/requirements.md
```

Master reads the returned artifact submission ID and accepts or rejects it.

```text
chassiss --credential ~/.chassiss/master-root.yaml \
  artifact context <artifact-submission-id>

chassiss --credential ~/.chassiss/master-root.yaml \
  artifact accept <artifact-submission-id>
```

Continue with Architecture, Mission, and Task templates. Every document goes through `artifact check`, `artifact submit`, and Master acceptance.

```text
chassiss --credential ~/.chassiss/cred-designer-1.yaml \
  template get architecture --output docs/architecture.md

chassiss --credential ~/.chassiss/cred-designer-1.yaml \
  template get mission --id M001 --output docs/missions/M001.md

chassiss --credential ~/.chassiss/cred-designer-1.yaml \
  template get task --id M001-T001 --output docs/tasks/M001-T001.md
```

Use `bootstrap` context to obtain current accepted digests.

## Start execution

The Build Agent activates the Mission and assigns a Task with its Orchestrator credential.

```text
chassiss --credential ~/.chassiss/cred-build-orchestrator.yaml \
  mission activate M001

chassiss --credential ~/.chassiss/cred-build-orchestrator.yaml \
  task assign M001-T001 --owner build-1
```

It then switches to the Developer credential.

```text
chassiss --credential ~/.chassiss/cred-build-developer.yaml \
  work open M001-T001

chassiss --credential ~/.chassiss/cred-build-developer.yaml \
  work context M001-T001
```

Edit only the returned Task worktree. Do not create the Task commit manually. After implementation, run all Checks and submit the exact snapshot.

```text
chassiss --credential ~/.chassiss/cred-build-developer.yaml \
  work check M001-T001 --all

chassiss --credential ~/.chassiss/cred-build-developer.yaml \
  work submit M001-T001 \
  --file handoff.md \
  --message complete
```

`work submit` creates the Task commit and returns an immutable Submission ID.

## Review and integration

```text
chassiss --credential ~/.chassiss/cred-reviewer-1.yaml \
  review context <submission-id>

chassiss --credential ~/.chassiss/cred-reviewer-1.yaml \
  review check <submission-id>
```

`review check` performs mechanical validation. Reviewer then records a semantic verdict.

```text
chassiss --credential ~/.chassiss/cred-reviewer-1.yaml \
  review approve <submission-id> --report review.md

chassiss --credential ~/.chassiss/cred-reviewer-1.yaml \
  integrate check <submission-id>

chassiss --credential ~/.chassiss/cred-reviewer-1.yaml \
  integrate apply <submission-id>
```

The same Reviewer actor that approved the Submission performs integration. Use `review request-changes` when revision is required. Developer edits the existing worktree, reruns Checks, and creates a new Submission.

## Complete the Mission

After every Mission Task is integrated, cancelled, or superseded, Orchestrator submits completion evidence and Master accepts it.

```text
chassiss --credential ~/.chassiss/cred-build-orchestrator.yaml \
  mission submit-acceptance M001 --evidence mission-evidence.md

chassiss --credential ~/.chassiss/master-root.yaml \
  mission accept M001
```

The project returns to `idle`.

## Refresh rules

- Run `bootstrap` when an agent starts
- Use the returned `state_revision` for mutations
- Use `trust_revision` for authorization changes
- Refresh after success, conflict, rejection, credential rotation, or resource selection
- Read remediation before choosing a response to a CLI error

See [Recommended agent topology](03-agent-topology.md) and the [CLI command reference](17-cli-reference.md).
