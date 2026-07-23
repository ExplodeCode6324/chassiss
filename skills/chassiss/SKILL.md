---
name: chassiss
description: Bootstrap and operate any CHASSISS-managed project through the credential-derived role policy returned by the trusted CLI. Use when an Agent needs to discover its identity, capabilities, current resource context, or revision-bound next actions without loading static role instructions.
---

# CHASSISS

Treat the trusted CLI as the execution and policy authority. Let the credential select the actor, role, grants, and resource scopes; never self-declare or substitute a role.

## CLI

Select the bundled CLI that matches the current operating system and architecture from `bin/<os>-<arch>/chassiss`. Resolve this path relative to the directory containing `SKILL.md`, never relative to the managed project or current working directory. Stop and report an unsupported system if no matching binary exists; do not substitute a `chassiss` found on `PATH`.

## Bootstrap

1. Locate the project root and the credential assigned by Master.
2. Run:

   ```text
   <skill-dir>/bin/<os>-<arch>/chassiss --json --root <project> --credential <credential> bootstrap
   ```

3. Read `principal`, `policy`, `capabilities`, `available_actions`, and `context_requests` from the JSON result.
4. Run the returned context `argv` before acting on a resource.
5. Use only command schemas present in `capabilities`. Prefer `available_actions`; append their declared required inputs without invoking a shell. An action marked `optional: true` is available but not required for progress.
6. For a mutation, pass the returned `state_revision` as global `--expect-revision`.
7. Bootstrap again after a mutation, conflict, rejection, trust change, credential rotation, or selected-resource change.

## Review and checks

- For Developer work, read `change_request` and `change_request_history` from `work context` before revising a returned Task.
- Treat `work check` as both declared-check execution and submission scope/budget preflight. A failure does not create passed evidence or advance the state revision.
- Treat `review check` as mechanical validation only. It never establishes semantic correctness; Reviewer must inspect the implementation and record an explicit verdict with a substantive report.
- Use the optional `work checkpoint` action only at a meaningful progress boundary.

## Owner

- Treat `owner.apply` as the only authorized way for an Owner to adopt Master-authored working-tree maintenance changes into the formal baseline.
- Supply a substantive `--reason`, keep the configured default branch at the signed baseline, and do not create a Git commit before invoking the command.
- Do not use Owner to modify `.chassis`, Git control data, or managed Requirements, Architecture, Mission, or Task artifact files.
- Bootstrap again after Owner apply and use `owner history` when the retained signed evidence must be inspected.

## Boundaries

- Never edit `.chassis/`, forge state or events, expose credential material, or replace CLI lifecycle operations with direct Git workflow changes.
- Treat `available_actions` as a current-state projection, not as proof that a later command will succeed; every command revalidates authorization and state.
- Stop and report the CLI error when no returned capability can resolve the situation. Do not invent permissions or broaden a frozen contract.
