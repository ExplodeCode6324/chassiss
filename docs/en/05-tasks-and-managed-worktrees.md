# Tasks and managed worktrees

Task phases:

```text
ready → active → submitted → approved → closed
                    ↘ request_changes → active
ready/active/submitted/approved → cancelled|superseded
```

`blocked:true` is a non-terminal overlay. Every phase uses a strict sparse State
projection so stale Attempt or Review data cannot leak into another phase.

## Start work

```text
chassiss context TASK-001 --json
chassiss task start TASK-001 --json
chassiss work open TASK-001 --json
```

The latter commands return the managed worktree's absolute path. All source
edits and local builds must happen there. Do not create a branch, checkout, or
worktree directly. Each attempt gets a non-reused
`<project>/<task>/<base-prefix>/<actor>` worktree path and matching Work Ref.

## Work loop

```text
chassiss work status TASK-001 --json
chassiss work diff TASK-001 --against base --json
chassiss work commit TASK-001 --message "..." --json
chassiss work log TASK-001 --json
chassiss check TASK-001 --json
chassiss submit TASK-001 --json
```

Work Commits use the Task Actor's Ed25519 key resolved deterministically from
the frozen Task runtime. They do not accept `--key` or `--grant` and do not
depend on the globally selected identity. The CLI stages only selected paths
and validates symlinks, protected paths, frozen `writes`, linear parent
provenance, and effective `max_changed_paths`. Parallel Agents pass explicit
`--key` and `--grant` to the later `submit` Transition.

`work diff` includes untracked files as additions and computes them through a
disposable index, leaving the real worktree index unchanged.

`submit` requires a clean worktree, reruns frozen Checks on the exact Work Head,
publishes the canonical Work Ref, and binds Submission Evidence to base, head,
tree, changed-path digest, contract blobs, submitter fingerprint, and Check
Results.

`work restore` accepts explicit paths only. `work remove` rejects dirty or
unreachable data by default. `task release` applies only to unchanged active
work.

For a failed temporary Agent, Master runs `attempt abandon` before deletion.
The signed transition retains the full failure record, State retains only its
compact index, and the CLI then destroys the failed worktree/Work Ref. Use
`attempt failures TASK-001 [--operation OPR-...]` to inspect it.
