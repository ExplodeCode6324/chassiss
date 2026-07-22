---
name: chassiss
description: Bootstrap and operate any CHASSISS-managed project through the credential-derived role policy returned by the trusted CLI. Use when an Agent needs to discover its identity, capabilities, current resource context, or revision-bound next actions without loading static role instructions.
---

# CHASSISS

Treat the trusted CLI as the execution and policy authority. Let the credential select the actor, role, grants, and resource scopes; never self-declare or substitute a role.

## CLI

Resolve executable paths relative to the directory containing this `SKILL.md`, never relative to the managed project or the current working directory.

- Linux `x86_64`: `bin/linux-amd64/chassiss`
- Linux `aarch64` or `arm64`: `bin/linux-arm64/chassiss`

Use `uname -m` only to select the matching bundled executable. Do not substitute a `chassiss` found on `PATH` when a matching bundled binary exists.

## Bootstrap

1. Locate the project root and the credential assigned by Master.
2. Run:

   ```text
   <skill-dir>/bin/linux-<arch>/chassiss --json --root <project> --credential <credential> bootstrap
   ```

3. Read `principal`, `policy`, `capabilities`, `available_actions`, and `context_requests` from the JSON result.
4. Run the returned context `argv` before acting on a resource.
5. Use only command schemas present in `capabilities`. Prefer `available_actions`; append their declared required inputs without invoking a shell.
6. For a mutation, pass the returned `state_revision` as global `--expect-revision`.
7. Bootstrap again after a mutation, conflict, rejection, trust change, credential rotation, or selected-resource change.

## Boundaries

- Never edit `.chassis/`, forge state or events, expose credential material, or replace CLI lifecycle operations with direct Git workflow changes.
- Treat `available_actions` as a current-state projection, not as proof that a later command will succeed; every command revalidates authorization and state.
- Stop and report the CLI error when no returned capability can resolve the situation. Do not invent permissions or broaden a frozen contract.
