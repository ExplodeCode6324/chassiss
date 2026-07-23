# Complete development lifecycle

[中文](../cn/08-完整开发生命周期.md) | English

CHASSISS organizes development into artifacts, Missions, Tasks, Submissions, Reviews, and Integrations. Every stage has a responsible role and verifiable inputs.

```text
Master and Designer discuss requirements
                 |
                 v
      Requirements and Architecture
                 |
           Master accepts
                 |
                 v
          Mission and Tasks
                 |
           Master accepts
                 |
                 v
       Orchestrator activates Mission
                 |
          claim or assign Task
                 |
                 v
    Developer open -> check -> submit
                 |
       Designer consistency review
                 |
                 v
 Reviewer check -> approve / request changes
                 |
                 v
      same Reviewer integrates
                 |
       Mission acceptance evidence
                 |
                 v
            Master accepts
```

The design-consistency step is a team convention. The independent Reviewer still owns the formal verdict.

## Plan the project

Designer submits Requirements and Architecture. After Master accepts both, Designer submits Mission and Task artifacts.

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/designer.cred \
  artifact submit requirements.md

chassiss --root /path/to/project \
  --credential ~/.chassiss/master-root.yaml \
  artifact accept <artifact-submission-id>
```

Accepted artifacts are frozen contracts. Material changes use new artifacts and replacement Tasks.

## Activate a Mission

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/orchestrator.cred \
  mission activate M001
```

Only one Mission may be active. Tasks become ready when their dependencies are satisfied.

## Assign or claim a Task

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/orchestrator.cred \
  task assign M001-T001 --owner developer-a
```

An Orchestrator may claim for its own actor when that actor also has an active Developer grant.

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/orchestrator.cred \
  task claim M001-T001
```

Both operations validate dependencies, WIP, path conflicts, and credential scopes.

## Open the worktree

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/developer.cred \
  work open M001-T001

chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/developer.cred \
  work context M001-T001
```

Edit only the returned Task worktree and only paths covered by `allowed_paths`.

## Check and record progress

Do not create the Task commit manually. Run Checks against the implementation snapshot.

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/developer.cred \
  work check M001-T001 --all

chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/developer.cred \
  work checkpoint M001-T001 --file checkpoint.md
```

`work checkpoint` stores a signed progress note. It does not save file content or create a Submission.

## Submit work

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/developer.cred \
  work submit M001-T001 \
  --file handoff.md \
  --message "Implement task M001-T001"
```

The CLI creates the Task commit. The Submission binds Task, actor, baseline, exact Head, files, Check evidence, metrics, handoff, and commit message. Any content change after `work check` requires another check.

## Design consistency review

Build or Review supplies the candidate diff and handoff to Designer. Designer compares them with accepted Requirements, Architecture, and Task contracts.

Version 0.3 has no Designer verdict event. This collaboration step does not replace formal Review.

## Independent Review

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/reviewer.cred \
  review context SUB-001

chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/reviewer.cred \
  review check SUB-001
```

Approve after semantic review.

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/reviewer.cred \
  review approve SUB-001 --report review.md
```

Request revision when needed.

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/reviewer.cred \
  review request-changes SUB-001 --report review.md
```

Developer revises the same worktree, reruns Checks, and creates a new immutable Submission.

## Integrate

The same Reviewer actor that approved the Submission performs both commands.

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/reviewer.cred \
  integrate check SUB-001

chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/reviewer.cred \
  integrate apply SUB-001
```

The CLI merges in an isolated candidate worktree, reruns Checks and independent verification, then advances the formal branch.

## Complete the Mission

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/orchestrator.cred \
  mission submit-acceptance M001 --evidence mission-evidence.md

chassiss --root /path/to/project \
  --credential ~/.chassiss/master-root.yaml \
  mission accept M001
```

The project returns to a state where another planning phase can begin.

## Block and resume

Orchestrator can block and resume Missions or Tasks with explicit reasons. Developer can run `work block` for owned work. Every state transition requires a fresh `bootstrap`.

## Next steps

- [Task worktrees, scope, and budgets](09-task-worktrees-and-budgets.md)
- [Checks and independent verification sources](10-checks-and-verification.md)
- [Review, integration, and audit](11-review-and-integration.md)

