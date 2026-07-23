# Task worktrees, scope, and budgets

[中文](../cn/09-任务工作区范围与预算.md) | English

A Task converts one delivery objective into a controlled change boundary. CHASSISS creates an isolated Git worktree for active work and validates scope, budget, and exact version before Submission and Integration.

## Execution conditions

- The Mission is active
- The Task artifact is accepted
- Dependencies are complete
- `allowed_paths` do not conflict with active or blocked Tasks
- Project WIP has capacity
- The credential covers the Project, Mission, and Task

A cancelled dependency is complete. A superseded dependency follows its replacement chain.

## Claim and assignment

`task claim` lets an Orchestrator claim for the same actor when that actor has a valid Developer grant. `task assign` selects another Developer actor.

The default WIP limit is two active Tasks for the project. Additional sessions do not increase it.

## Linked worktree

`work open` creates these bindings.

```text
.chassis/worktrees/<lowercase-task-id>
chassiss/<lowercase-task-id>
```

The CLI records path, branch, worktree identity, and binding digest. Moving the directory, deleting the worktree, changing its branch, or replacing the path causes later commands to fail.

The formal project directory represents the controlled baseline. Developer changes belong in the Task worktree.

## Allowed paths

```yaml
allowed_paths:
  - src/service/**
  - tests/service/**
  - README.md
```

`*` does not cross a directory separator. `**` may cross multiple levels.

```text
src/*.go       matches src/a.go
src/*.go       does not match src/api/a.go
src/**         matches src/api/a.go
```

The CLI compares the complete baseline-to-candidate file set. One out-of-scope file blocks check, Submission, and Integration.

Path-conflict detection is conservative. If two patterns may cover the same path, the Tasks cannot execute together.

## Independent verification paths

Verification paths hold tests, fixtures, golden files, or other evidence maintained outside the Developer's Task scope.

- Patterns are unique and project-relative
- They do not overlap `allowed_paths`
- They match baseline files
- Their content stays unchanged during work and Integration

Updating verification sources requires separate planning and a different Task boundary.

## Budget

```yaml
budget:
  max_changed_files: 20
  max_diff_lines: 1000
  max_commits: 10
```

New project defaults are 100 files, 20000 diff lines, and 20 commits. A Task may use a smaller budget.

| Metric | Input |
| --- | --- |
| Changed files | Added, modified, deleted, and renamed files |
| Diff lines | Added plus deleted text lines |
| Commits | Task commits reachable after baseline |

Binary files count toward changed files. The CLI does not invent text line counts for binary content.

## Preflight

`work check` verifies these facts.

- Worktree and branch bindings remain valid
- Task Head descends from the frozen baseline
- Tracked and untracked changes form a candidate tree
- Every changed file is in scope
- File, line, and commit metrics fit the budget
- Verification sources equal the baseline

The CLI uses a temporary candidate commit for scope and metrics without advancing the Task branch or changing the Developer index.

## Checkpoint

`work checkpoint` records a signed text note for progress, risk, and next steps. It does not preserve source files, Git trees, or Check results.

## Budget exhaustion and contract change

Stop expanding work when the budget is reached. Orchestrator may submit a complete in-budget portion, create another Task, block pending design, or supersede the contract.

Accepted artifacts are never widened in place.

## Release, cancel, and supersede

`task release` requires an unsubmitted Task whose branch is still at baseline and whose worktree is clean. The CLI removes the worktree and unused branch, then returns the Task to ready.

Master uses `task cancel` when a Task will not run. Orchestrator uses `task supersede` to link a frozen Task to an accepted replacement.

Events and prior evidence remain available.

## Cleanup

After successful Integration, the CLI removes a Task worktree only when it is clean at the submitted Head. The Task branch remains for exact-Head audit evidence.

Do not manually remove `.chassis/worktrees` or prune CHASSISS worktrees.

## Next steps

- [Checks and independent verification sources](10-checks-and-verification.md)
- [Concurrency, locks, and state conflicts](12-concurrency-and-conflicts.md)
- [Crash recovery and integrity blocking](14-recovery-and-integrity.md)

