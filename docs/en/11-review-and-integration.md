# Review, integration, and audit

[中文](../cn/11-复核集成与审计.md) | English

Review combines mechanical validation with semantic judgment. The CLI binds code and evidence to an exact version. Reviewer determines whether the implementation satisfies Requirements, Architecture, Task acceptance criteria, and project quality expectations.

## Immutable Submission

`work submit` creates a manifest containing these fields.

- Submission and Task IDs
- Developer actor
- Task baseline and exact Head commit
- Changed-file list
- Check results
- Handoff and commit message
- File, diff-line, and commit metrics
- Creation time

The Submission digest covers the complete manifest. Any field change produces a different digest.

## Independence

Reviewer actor must differ from Submission actor. A different credential file with the same actor does not satisfy independence.

The Build Agent may hold Orchestrator and Developer roles. The Review Agent uses another actor and an independent session.

## Read context

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/reviewer.cred \
  review list

chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/reviewer.cred \
  review context SUB-001
```

Context includes the Task contract, Submission manifest, handoff, file set, and complete diff. Reviewer also reads controlled Requirements and Architecture.

## Mechanical validation

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/reviewer.cred \
  review check SUB-001
```

The command verifies the following.

- Manifest digest
- Git diff and changed-file list
- Scope and budget
- Commit message
- Developer Check evidence and exact snapshot
- Independent verification sources

It does not rerun Check processes in version 0.3.

## Semantic Review

Reviewer examines acceptance criteria, architecture boundaries, failure handling, authorization and security, test coverage, compatibility impact, and handoff accuracy.

Designer may provide design-consistency feedback. Formal verdict remains a Reviewer action.

## Approve

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/reviewer.cred \
  review approve SUB-001 --report review.md
```

The report must be nonempty valid UTF-8 and no larger than 64 KiB. Approval binds the exact Submission digest.

## Request changes

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/reviewer.cred \
  review request-changes SUB-001 --report review.md
```

The Task enters `changes_requested`. Developer edits the worktree, reruns Checks, and uses `work submit` to create a new commit and Submission.

`review history` retains all decisions and reports.

## Integration preflight

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/reviewer.cred \
  integrate check SUB-001
```

This validates approval, manifest, and Task evidence. `integrate apply` obtains the lock and revalidates formal worktree, Task branch Head, and State baseline.

## Candidate integration

The same Reviewer actor that approved the Submission runs the following.

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/reviewer.cred \
  integrate apply SUB-001
```

The CLI performs these steps.

1. Creates a detached candidate from the current formal baseline
2. Merges the exact Task Head without committing
3. Produces one merged tree
4. Reruns frozen Checks
5. Verifies independent sources
6. Creates `Integrate <task-id>`
7. Confirms the commit tree equals the checked tree
8. Fast-forwards the formal branch
9. Records signed Integration evidence

A merge conflict, failed Check, or tree mismatch leaves the formal branch unchanged.

## Integration evidence

Evidence contains the prior baseline, Submission digest and Head, approving Review, merged-tree digest, Check results, integration commit, and new formal baseline.

The Task becomes `integrated`. Candidate and Task worktrees are cleaned only when their exact bindings remain safe. The Task branch stays for audit.

## Failed integration

Preserve diagnostics, refresh `bootstrap`, coordinate a controlled Developer revision, and produce a new Submission and Review. Do not force-merge or rewrite an approved branch.

If Git state disagrees with an operation journal, use `recover` and `verify` before any manual action.

## Next steps

- [Concurrency, locks, and state conflicts](12-concurrency-and-conflicts.md)
- [Crash recovery and integrity blocking](14-recovery-and-integrity.md)
- [Publishing and multi-machine collaboration](15-publish-and-multi-machine.md)

