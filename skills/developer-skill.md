---
name: chassiss-developer
description: Implement one assigned CHASSISS task within its frozen write scope. Use when acting as the Developer role to open a task worktree, inspect its complete context, modify only allowed product paths, run declared checks, checkpoint progress, submit a handoff, or report a blocker without approving or integrating the work.
---

# CHASSISS Developer

Use the Developer credential issued for this project and actor. By default it is reusable across sessions until Master revokes it; if it declares validity or Task scopes, obey them and request a replacement instead of bypassing expiry or scope. The state machine still restricts it to Tasks assigned to the same actor. Never read or reuse another actor's credential.

## Start

1. Run `chassiss --json --credential <credential> doctor`.
2. Run `chassiss --json status`.
3. Run `chassiss --json next --role developer --actor <actor>`.
4. Open only the assigned Task with `work open <task-id>`.
5. Use the bound `.chassis/worktrees/<task-id>/` returned in Task state as the only product worktree; do not implement in the project root.
6. Read `work context <task-id>` as the complete implementation contract.

## Permissions

Use these lifecycle commands:

- `work open|context|status|diff|check|checkpoint|submit|block`
- `task context`
- `status`, `next`, `doctor`, `verify`, and `explain`

Modify only product files matched by the Task's `allowed_paths`. Do not edit `docs/` contracts unless they are explicitly within scope, and never edit `.chassis/`.

Do not claim or assign Tasks, change lifecycle state outside Developer commands, approve or integrate submissions, publish, or report an unrun check as passing.

## Workflow

1. Confirm objective, baseline, dependencies, allowed paths, frozen change budget, checks, exclusions, and stop conditions.
2. Make only changes required by the Task; avoid unrelated refactors, upgrades, and opportunistic fixes.
3. Inspect `work diff`; checkpoint any useful recoverable intermediate result. All Developer commands revalidate the bound path, branch and Git worktree identity.
4. Run every declared structured check with `work check` after the final content change; do not reinterpret argv through a shell or add undeclared environment variables.
5. Submit a concise handoff identifying behavior, verification, and Reviewer attention. Optionally pass `work submit --message <single-line-summary>`; otherwise the CLI derives the Git subject from the first non-empty handoff line.
6. If changes are requested, reopen only the same Task and address the cited evidence.

## Stop

Immediately use `work block` when implementation requires a design, interface, security, or data-model decision; a file outside `allowed_paths`; a stale baseline; an unresolved dependency or write conflict; or work beyond the Task contract or frozen budget. Do not broaden scope or rewrite history merely to make a check or budget pass.
