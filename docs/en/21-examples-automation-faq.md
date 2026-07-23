# Examples, automation, and FAQ

[中文](../cn/21-示例自动化与常见问题.md) | English

Replace IDs, paths, and credentials in these examples with values returned by the CLI. Run `bootstrap` before a write and use its current revision.

## Greenfield example

Create Root and project.

```sh
chassiss auth master-init

chassiss --credential ~/.chassiss/master-root.yaml \
  project init /work/example
```

Issue the minimum roles. The Build credentials need separate files because they share an actor.

```sh
cd /work/example

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
```

Designer creates each artifact, checks it, submits it, and waits for Master acceptance.

```sh
chassiss --credential ~/.chassiss/cred-designer-1.yaml \
  template get requirements --output docs/requirements.md

chassiss --credential ~/.chassiss/cred-designer-1.yaml \
  artifact check docs/requirements.md

chassiss --credential ~/.chassiss/cred-designer-1.yaml \
  artifact submit docs/requirements.md

chassiss --credential ~/.chassiss/master-root.yaml \
  artifact accept <artifact-submission-id>
```

Repeat for Architecture, Mission, and Task.

Orchestrator activates and assigns.

```sh
chassiss --credential ~/.chassiss/cred-build-orchestrator.yaml \
  mission activate M001

chassiss --credential ~/.chassiss/cred-build-orchestrator.yaml \
  task assign M001-T001 --owner build-1
```

Developer opens the returned worktree, edits without committing, checks, and submits. The CLI creates the Task commit.

```sh
chassiss --credential ~/.chassiss/cred-build-developer.yaml \
  work open M001-T001

chassiss --credential ~/.chassiss/cred-build-developer.yaml \
  work check M001-T001 --all

chassiss --credential ~/.chassiss/cred-build-developer.yaml \
  work submit M001-T001 --file handoff.md --message complete
```

Reviewer checks, approves, and integrates.

```sh
chassiss --credential ~/.chassiss/cred-reviewer-1.yaml \
  review context <submission-id>

chassiss --credential ~/.chassiss/cred-reviewer-1.yaml \
  review check <submission-id>

chassiss --credential ~/.chassiss/cred-reviewer-1.yaml \
  review approve <submission-id> --report review.md

chassiss --credential ~/.chassiss/cred-reviewer-1.yaml \
  integrate apply <submission-id>
```

Orchestrator submits Mission evidence and Master accepts.

```sh
chassiss --credential ~/.chassiss/cred-build-orchestrator.yaml \
  mission submit-acceptance M001 --evidence mission-evidence.md

chassiss --credential ~/.chassiss/master-root.yaml \
  mission accept M001
```

## Brownfield example

The target must be a clean Git root with at least one commit.

```sh
cd /work/existing-project
git status --short
git log -1 --oneline

chassiss --credential ~/.chassiss/master-root.yaml \
  project init /work/existing-project --existing
```

Brownfield preserves branch and history and uses `.git/info/exclude` for `.chassis/`. Establish Requirements and Architecture after adoption.

## Change-request example

Reviewer records actionable revision requirements.

```sh
chassiss --credential ~/.chassiss/cred-reviewer-1.yaml \
  review request-changes <submission-id> --report review.md
```

Developer reads context and history.

```sh
chassiss --credential ~/.chassiss/cred-build-developer.yaml \
  work context M001-T001

chassiss --credential ~/.chassiss/cred-build-developer.yaml \
  review history --task M001-T001
```

After editing, Developer reruns Checks and creates a new Submission.

```sh
chassiss --credential ~/.chassiss/cred-build-developer.yaml \
  work check M001-T001 --all

chassiss --credential ~/.chassiss/cred-build-developer.yaml \
  work submit M001-T001 --file handoff-v2.md --message revised
```

Old Submissions, reports, and verdicts remain.

## Budget-overrun example

After `CHS-WORK-BUDGET-FILES`, Developer stops expanding the change. Designer creates a smaller accepted replacement and Orchestrator links it.

```sh
chassiss --credential ~/.chassiss/cred-build-orchestrator.yaml \
  task supersede M001-T001 --replacement M001-T002
```

The old contract and history remain immutable.

## Parallel Tasks

These scopes are disjoint.

```text
M001-T001  src/api/**
M001-T002  docs/**
M001-T003  tests/fixtures/**
```

Orchestrator may assign them to different Developer actors within the project WIP limit.

```sh
chassiss --credential ~/.chassiss/cred-orchestrator.yaml \
  task assign M001-T001 --owner developer-a

chassiss --credential ~/.chassiss/cred-orchestrator.yaml \
  task assign M001-T002 --owner developer-b
```

Each Integration uses the latest formal baseline. Overlapping scopes must wait or be replanned.

## Owner example

During quiescence, a human edits ordinary files in the formal project directory without committing.

```sh
chassiss --credential ~/.chassiss/cred-human-owner.yaml \
  owner apply --reason "Correct release metadata"

chassiss --credential ~/.chassiss/cred-human-owner.yaml \
  owner history
```

## Crash-recovery example

After an interrupted mutation or `CHS-OPERATION-RECOVERY-REQUIRED`, inspect and recover.

```sh
chassiss --json --root /work/example doctor
chassiss --json --root /work/example recover
```

Refresh bootstrap after success. Stop every write after `CHS-INTEGRITY-BLOCKED`.

## Remote Publish example

```sh
chassiss --credential ~/.chassiss/cred-build-orchestrator.yaml \
  publish check --target github --remote origin --branch main

chassiss --credential ~/.chassiss/cred-build-orchestrator.yaml \
  publish apply --target github --remote origin --branch main
```

The remote must be empty or an ancestor of the formal baseline.

## Five-minute polling template

This script performs a read-only poll.

```sh
#!/bin/sh
set -eu

CHASSISS_BIN=/opt/chassiss/chassiss
PROJECT_ROOT=/work/example
CREDENTIAL_FILE=/run/secrets/chassiss-role.yaml

"$CHASSISS_BIN" \
  --json \
  --root "$PROJECT_ROOT" \
  --credential "$CREDENTIAL_FILE" \
  bootstrap
```

```cron
*/5 * * * * /opt/chassiss/poll.sh >>/var/log/chassiss-poll.log 2>&1
```

Do not write credentials or complete Bootstrap output to public logs.

## Automated mutation loop

```text
bootstrap
   |
   v
read context_requests
   |
   v
select one available_action
   |
   v
execute with revision
   |
   +---- success or conflict ----> bootstrap
   |
   +---- integrity error --------> stop and notify Master
```

Never cache a batch of write commands.

## Autonomous Master experiment

A capable frontier model can hold the Master role, issue subagent credentials, and manage the project.

Good experiments are public, recoverable, small, strongly tested, and easy for a human to audit. Record Mission completion, change requests, budget use, intervention points, and defects.

This is outside the default design target. Shared execution disables secret isolation and does not guarantee quality.

## FAQ

### Does CHASSISS require GitHub

No. Version 0.3 requires local Git. Publish can target GitHub, GitLab, or another Git remote.

### Can Designer and Reviewer use the same actor

The CLI only enforces that Submission author and Reviewer actor differ. Separate Designer and Reviewer actors remain the recommended topology.

### Why does the Build Agent need two credentials

Orchestrator schedules Tasks. Developer operates worktrees. A shared actor hands one Task between these roles while actions remain separated.

### Can Developer claim directly

`task claim` uses an Orchestrator credential and requires a Developer grant for the same actor.

### Who integrates an approved Submission

The same Reviewer actor that approved it.

### Why is Review needed after Checks pass

Checks validate frozen mechanical evidence. They cannot judge intent, architecture, maintainability, or unmodeled risk.

### Can I repair `state.yaml` manually

No. Use `recover`, `doctor`, and `verify`.

### Can two machines keep independent `.chassis` copies

They cannot write in parallel in version 0.3. Git remote synchronizes code only.

### Is credential armor safe to paste into chat

Armor contains a private key. Use a controlled, short-lived channel and revoke after exposure.

### Is Windows supported

Bundled binaries currently target macOS and Linux. Windows writes return `CHS-LOCK-UNSUPPORTED`.

## High-quality Issue

Include binary and schema versions, OS and architecture, Go and Git versions, Greenfield or Brownfield mode, role and command shape, stable error code, minimal reproduction, expected and actual behavior, journal presence, and redacted doctor and verify output.

Never attach Root, armor, private keys, tokens, complete remote URLs, or private source.

## Next steps

- [Five-minute quickstart](02-quickstart.md)
- [Error handling and troubleshooting](18-troubleshooting.md)
- [Test record and known issues](22-testing-and-improvements.md)

