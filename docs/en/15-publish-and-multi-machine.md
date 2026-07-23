# Publishing and multi-machine collaboration

[中文](../cn/15-发布与多机协作.md) | English

Integration advances the local formal baseline. Publish fast-forwards that exact baseline to a Git remote. They produce separate `integration.applied` and `publication.applied` events.

`github`, `gitlab`, and `remote-git` currently use the same Git remote adapter. The target records intent and does not call platform-specific APIs.

## Configure a remote

```sh
git remote add origin <remote-url>
```

The default remote is `origin`. The default target branch is the configured formal branch.

## Publish check

Orchestrator or Master can run the following.

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/orchestrator.cred \
  publish check --target github
```

Explicit remote and branch are supported.

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/orchestrator.cred \
  publish check --target remote-git \
  --remote upstream \
  --branch main
```

The check requires a clean formal worktree, a default-branch Head equal to State baseline, a valid remote, and a valid target branch.

Statuses include `ready`, `up_to_date`, `remote_unknown`, and `diverged`.

## Publish apply

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/orchestrator.cred \
  publish apply --target github
```

The remote branch must be empty or an ancestor of the local baseline. The CLI pushes the exact commit to the target ref without force, then confirms the resulting remote Head before recording the event.

## Divergence

`CHS-PUBLISH-NON-FAST-FORWARD` means the remote contains history absent from the local baseline. Inspect and incorporate that work through an explicit Task. Never use force push.

`remote_unknown` means the remote commit is not available locally during check. Apply fetches the exact object before its final ancestry decision.

## Endpoint binding

State and journal store remote name and SHA-256 of the remote URL. They do not store the raw URL because it may contain credentials.

Changing the endpoint during an unfinished Publish causes an integrity block.

## Publish recovery

- Remote still at pre-push Head and journal is prepared, so recovery cancels
- Remote equals target baseline, so recovery commits the prepared publication event
- Remote has any other Head, so recovery blocks

Run `recover` after an uncertain push.

## Five-minute monitoring

Agents may poll `bootstrap --json` every five minutes. Build checks actionable Tasks, Review checks pending Submissions, and Orchestrator or Master checks unpublished baselines.

Polling must never force push, remove locks, edit `.chassis`, or ignore revision conflicts.

## Version synchronization

GitHub, GitLab, a self-hosted remote, or another reliable Git transport can synchronize formal project commits.

1. The authoritative control end integrates
2. Orchestrator or Master publishes
3. Other machines obtain the exact commit
4. Agents confirm commit and CLI version
5. Controlled writes still run against the authoritative `.chassis`

## Current control-end limitation

Publish does not synchronize Trust, events, State, journals, Submission manifests, or worktree bindings. Another clone has the same code and a different control history.

Maintain one authoritative control end. A safe migration stops all writes, copies complete `.chassis` plus Git objects, and verifies the destination. Parallel control copies are unsupported.

Multi-control operation requires a future backend for signed-event synchronization, monotonic Trust revisions, operation ownership, and offline-fork detection.

## Next steps

- [Installation and version synchronization](04-installation-and-sync.md)
- [Concurrency, locks, and state conflicts](12-concurrency-and-conflicts.md)
- [Security model and known risks](19-security-model.md)

