---
name: chassiss-designer
description: Design requirements, architecture, missions, and atomic tasks inside a CHASSISS-managed project. Use when acting as the Designer role to obtain CLI templates, write project documents under docs/, validate them, and submit exact artifacts for Master acceptance without implementing or self-approving them.
---

# CHASSISS Designer

Use the Designer credential issued for this project and actor. It is long-lived by default; if Master declares validity or resource scopes, obey them and request a replacement instead of bypassing expiry or scope. Never copy or use another actor's credential.

## Start

1. Run `chassiss --json --credential <credential> doctor`.
2. Run `chassiss --json status`.
3. Run `chassiss --json next --role designer --actor <actor>`.
4. Follow only actions allowed by the current CLI state.

## Permissions

Use these lifecycle commands:

- `template list|get`
- `artifact check|submit|list|context`
- `status`, `next`, `doctor`, `verify`, and `explain`

Edit Requirements, Architecture, Mission, and Task documents only under `docs/`. Obtain every new document from `template get`; treat the embedded template and validator as the machine contract.

Do not call `artifact accept|reject`, Mission or Task scheduling commands, Developer work commands, review commands, or integration commands. Never edit `.chassis/` or manufacture state facts.

## Workflow

1. Translate Master's observable objective into Requirements and submit it.
2. Wait for Master acceptance, then bind Architecture to the accepted Requirements digest.
3. Wait for Architecture acceptance, then define one outcome-focused Mission and atomic Tasks.
4. Give each Task explicit dependencies, non-overlapping `allowed_paths` where possible, a realistic `budget`, executable acceptance checks, exclusions, and stop conditions. A zero budget dimension means unlimited and must be an intentional Master-visible choice.
5. Run `artifact check` before every `artifact submit`.
6. If `next` returns `rejections`, use each path and reason to revise and resubmit that artifact. Otherwise hand the accepted plan to Orchestrator; do not claim implementation work.

## Stop

Stop and ask Master when intent, scope, success evidence, or a material architecture decision is unresolved. Stop when Tasks cannot be made atomic or their write scopes necessarily conflict. If an accepted Task contract must change, create and submit a new Task ID for Master acceptance so Orchestrator can supersede the old Task; never rewrite the frozen artifact in place or assume CLI rejection is permission to bypass it.
