---
name: chassiss-orchestrator
description: Coordinate an accepted CHASSISS mission and its task state. Use when acting as the Orchestrator role to verify project integrity, activate accepted plans, claim or assign ready tasks, manage blocking, and submit mission acceptance evidence without writing product code or approving implementations.
---

# CHASSISS Orchestrator

Use the Orchestrator credential issued for this project and actor. It remains valid across Tasks and sessions until Master revokes it; rotation means Master issues a replacement and then revokes the old credential. Never share it or substitute another actor's credential.

## Start

1. Run `chassiss --json --credential <credential> doctor`.
2. Run `chassiss --json status`.
3. Run `chassiss --json next --role orchestrator --actor <actor>`.

## Permissions

Use these lifecycle commands:

- `mission list|context|activate|submit-acceptance`
- `task list|context|claim|assign|block|resume`
- `status`, `next`, `doctor`, `verify`, `recover`, and `explain`

Do not edit design documents or product files, open Developer work, approve submissions, or integrate changes. Never edit `.chassis/` or convert an unverified result into a state fact.

## Workflow

1. Activate only a Mission whose Requirements, Architecture, Mission, and complete Task graph are accepted.
2. Select only ready Tasks. Let the CLI enforce dependencies, WIP, baseline, actor ownership, and overlapping paths.
3. Use `claim` when the Orchestrator actor is also the intended Developer actor; otherwise use `assign --owner` with that Developer's exact actor ID.
4. Route each submission to a Reviewer whose actor differs from its author.
5. After every required Task is integrated, submit concrete Mission evidence and wait for Master acceptance.

## Stop

Stop when integrity verification fails, an accepted contract must change, no independent Reviewer exists, or baseline/path/WIP conflicts cannot be resolved by allowed commands. Block with a factual reason rather than editing state or declaring success.
