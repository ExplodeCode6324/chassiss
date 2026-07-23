# Human Owner takeover

[中文](../cn/16-人类所有者接管.md) | English

Owner gives an independent human developer a controlled formal-baseline entry point. It supports small maintenance work, emergency fixes, and direct human changes to ordinary project files.

Owner credential cannot accept artifacts, schedule agents, or replace the Master Root. Master Root cannot run `owner apply`.

## Preconditions

- No active Mission
- No active Task
- No artifact awaiting Master
- Current branch is the configured default branch
- Current Head exactly equals State baseline
- At least one uncommitted ordinary project-file change exists

The CLI refuses takeover when any condition fails.

## Issue Owner

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/master-root.yaml \
  auth issue \
  --actor human-owner \
  --role owner \
  --output ~/.chassiss/cred-human-owner.yaml
```

Only one unrevoked Owner grant may exist. Revoke an old or expired Owner before replacement.

## Prepare changes

Switch the formal project directory to its default branch and edit ordinary files. Do not create a commit. The CLI must prepare the unique Owner commit itself.

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/cred-human-owner.yaml \
  bootstrap

git status --short
```

## Protected content

Owner cannot modify `.chassis`, `.git`, or registered Requirements, Architecture, Mission, and Task files.

Update controlled artifacts through Designer submission and Master acceptance.

## Apply

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/cred-human-owner.yaml \
  owner apply --reason "Update release metadata"
```

Reason must be nonempty valid UTF-8 and no larger than 64 KiB. The first nonempty line forms a commit message with this shape.

```text
Owner baseline: Update release metadata
```

The summary is limited to 100 characters and control characters are removed.

## CLI checks

1. Obtain lock and validate State revision
2. Verify Owner grant and baseline scope
3. Confirm project quiescence
4. Check default branch and formal baseline
5. Collect working-tree changes
6. Reject protected paths
7. Prepare exactly one commit
8. Validate files, metrics, and tree digest
9. Advance the formal branch
10. Record `owner.baseline_applied`

Evidence includes actor, credential ID, reason, old and new Heads, tree digest, file list, commit message, and metrics.

## History

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/cred-human-owner.yaml \
  owner history
```

Owner and Master can read this audit history.

## Common refusals

| Code | Meaning |
| --- | --- |
| `CHS-OWNER-PROJECT-ACTIVE` | Mission, Task, or artifact still active |
| `CHS-OWNER-BRANCH` | Wrong branch |
| `CHS-OWNER-BASELINE-MOVED` | Head differs from formal baseline |
| `CHS-OWNER-NO-CHANGES` | No uncommitted changes |
| `CHS-OWNER-PROTECTED` | Controlled data was changed |
| `CHS-AUTH-RESOURCE` | Credential excludes this baseline |

Owner does not adopt a pre-existing human commit. Preserve unexpected history and let Master decide the recovery path.

Owner apply advances only the local baseline. Orchestrator or Master publishes later.

## Next steps

- [Publishing and multi-machine collaboration](15-publish-and-multi-machine.md)
- [Root and role credentials](05-credentials.md)
- [Examples, automation, and FAQ](21-examples-automation-faq.md)

