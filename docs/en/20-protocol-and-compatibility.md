# Protocol, versions, and compatibility

[中文](../cn/20-协议版本与兼容性.md) | English

CHASSISS versions the agent API, project files, events, Trust, role policy, Bootstrap, and journals separately. Similar field names do not make two schema versions compatible.

## Current versions

| Protocol or file | Value |
| --- | --- |
| CLI | `0.3.0-dev` |
| API | `chassiss.dev/v2` |
| Config | `4` |
| State | `4` |
| Event | `4` |
| Trust | `1` |
| Credential | `1` |
| Role Policy | `3` |
| Bootstrap | `chassiss.bootstrap/v3` |
| Credential armor envelope | `1` |
| Workflow operation journal | `1` |
| Publish operation journal | `1` |

Automation checks binary and schema versions.

## JSON API

`--json` uses the `chassiss.dev/v2` envelope. Errors expose stable code, retryable, remediation, and diagnostic category.

A caller may ignore unneeded display fields. Schema version and current revision still require validation.

## Signing bytes

Events and Trust use deterministic JSON bytes from Go `encoding/json` over controlled protocol structures. Floating-point values are forbidden.

This encoding is part of the CHASSISS protocol. It does not claim RFC 8785 JCS compatibility. Another language implementation must reproduce the repository golden vectors.

Digests use SHA-256. Signatures use Ed25519.

## Event V4

The envelope binds version, ID, Project ID, sequence, type, resource, actor, role, credential ID, time, previous digest, payload, digest, and signature.

Payload contains only facts required for one transition. Actor, revision, and time come from the signed envelope.

Events form a sequence and `previous_digest` chain. The reducer rebuilds State deterministically.

## Event types

```text
project.initialized
artifact.submitted
artifact.accepted
artifact.rejected
mission.activated
mission.blocked
mission.resumed
mission.acceptance_submitted
mission.completed
task.claimed
task.assigned
task.blocked
task.resumed
task.released
task.cancelled
task.superseded
work.opened
work.checked
work.checkpointed
work.submitted
work.blocked
review.approved
review.changes_requested
integration.applied
publication.applied
owner.baseline_applied
```

Unknown types, unknown payload fields, mismatched resources, and invalid transitions are rejected.

## Trust V1 and Credential V1

Trust contains Root public key, revision, grants, revocations, update time, and Root signature.

Credential contains project, Root fingerprint, actor, role, actions, resources, validity, and private key. Metadata must exactly match the active grant.

Armor has its own version and payload digest.

## Config V4 and State V4

Config fixes Project ID, mode, default branch, content backend, WIP limit, default Task budget, and Root fingerprint.

State is the event projection for current phase, baseline, and domain objects. State does not create facts independently.

## Role Policy V3

Role Policy defines visible commands, actions, parameters, state conditions, and common invariants. Bootstrap returns a policy digest.

Agents do not treat a static prompt as a permanent authorization table.

## Bootstrap V3

Bootstrap binds binary version, project path, State revision, Trust revision, and principal. `available_actions` is a current projection and not an offline token.

## Golden vectors

Protocol tests freeze Event V4 canonical bytes, event digests and signing bytes, and Trust V1 bytes and digests.

An intentional protocol change defines a new version and migration boundary before updating golden values.

## Legacy policy

The current CLI rejects old Config, State, and Event schemas with `CHS-SCHEMA-UNSUPPORTED`. It does not add missing fields or silently upgrade.

There is no in-place migration tool. Keep a compatible CLI for old control state or create a separately verified V2 project.

## Upgrade requirements

1. Define new schemas and version numbers
2. Freeze signing bytes and golden vectors
3. Define old-version read and write behavior
4. Provide an offline auditable migrator
5. Verify events, State, Trust, and Git baseline
6. Preserve the source control end after failure
7. Update Skill, binaries, and every agent

Different protocol versions must not write one control end concurrently.

## Backend compatibility

A non-Git backend needs immutable content identity, baseline and candidate Head, file scope and metrics, isolated workspaces, merged candidates, atomic formal advancement, monotonic ancestry, crash-recovery pre-state and post-state, and exact signable evidence.

A backend name cannot weaken Task, Review, Integration, or Recovery constraints.

## Next steps

- [`.chassis`, events, and State projection](13-control-state.md)
- [Installation and version synchronization](04-installation-and-sync.md)
- [Test record and known issues](22-testing-and-improvements.md)
