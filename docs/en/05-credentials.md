# Root, role credentials, and secret distribution

[中文](../cn/05-根密钥角色凭据与分发.md) | English

CHASSISS uses Ed25519 keys for project root trust, role grants, and signed events.

The Master Root represents project ownership. A role credential represents one actor using one role with a defined authorization scope.

## Master Root

The default command creates `~/.chassiss/master-root.yaml`.

```text
chassiss auth master-init
```

Use `--output` for a different file or directory.

```text
chassiss auth master-init --output /secure/location/master-root.yaml
```

The CLI writes the file with mode `0600` and creates parent directories with mode `0700`. Existing files are never overwritten.

The Root contains a public and private key. A project stores only the public key, fingerprint, and Root-signed Trust metadata. Keep the Root outside the project under human control.

Master Root can issue and revoke credentials, accept or reject artifacts, accept Missions, cancel Tasks, publish, and read Owner history. It cannot run `owner apply` or pass through `auth export`.

## Role credentials

A role credential contains the following data.

- Project ID and Root fingerprint
- Credential ID
- Actor and role
- Allowed actions
- Resource scope
- Validity window
- Private key

`trust.yaml` stores the matching public key, grant, and revocation records.

## Issuance

```text
chassiss --root /path/to/project \
  --credential ~/.chassiss/master-root.yaml \
  auth issue \
  --actor developer-1 \
  --role developer \
  --output ~/.chassiss/cred-developer-1.yaml
```

Without `--output`, the default is `~/.chassiss/cred-<actor>.yaml`. The same actor needs explicit output paths when receiving more than one role.

If no Root path is present, the CLI searches the default Root and `~/.chassiss/roots/` for one unique matching fingerprint.

Actor identifiers must contain 1 to 128 characters and cannot contain whitespace or control characters.

## Validity windows

Credentials have no expiration by default. Set an absolute time or TTL when a shorter lifetime is required.

```text
chassiss auth issue \
  --actor reviewer-1 \
  --role reviewer \
  --expires-at 2026-08-01T00:00:00Z
```

```text
chassiss auth issue \
  --actor reviewer-1 \
  --role reviewer \
  --ttl-seconds 86400
```

`--not-before` and `--expires-at` use RFC3339. `--expires-at` and `--ttl-seconds` are mutually exclusive. TTL must be between 1 and 315360000 seconds.

## Resource and action scopes

Comma-separated options can restrict a credential to exact resources.

```text
chassiss auth issue \
  --actor developer-1 \
  --role developer \
  --tasks M001-T001,M001-T002
```

| Option | Resource |
| --- | --- |
| `--projects` | Project IDs |
| `--missions` | Mission IDs |
| `--tasks` | Task IDs |
| `--submissions` | Submission IDs |
| `--submission-digests` | Submission digests |
| `--heads` | Git Heads |
| `--baselines` | Formal baselines |

An empty dimension adds no extra restriction for that dimension. Role, state, and ownership checks still apply.

`--actions` selects a subset of actions already allowed by the role. It cannot add a role capability.

## Build Agent credentials

The Orchestrator and Developer credentials use the same actor and separate files.

```text
chassiss auth issue \
  --actor build-1 \
  --role orchestrator \
  --output ~/.chassiss/cred-build-orchestrator.yaml

chassiss auth issue \
  --actor build-1 \
  --role developer \
  --output ~/.chassiss/cred-build-developer.yaml
```

Orchestrator can claim or assign a Task for `build-1`. The Developer credential then opens and operates the worktree. Events still record distinct roles and credential IDs.

## Unique Owner grant

Only one unrevoked Owner grant may exist. An expired Owner still occupies that slot until Master explicitly revokes it.

```text
chassiss --credential ~/.chassiss/master-root.yaml \
  auth revoke <owner-credential-id> \
  --reason owner-rotation
```

Issue the replacement after revocation.

## Armor export and import

```text
chassiss auth export ~/.chassiss/cred-developer-1.yaml
```

The armor contains exactly one header line, one Base64 payload line, and one footer line. The payload includes the original credential YAML, envelope version, and SHA-256 digest. Input is limited to 256 KiB.

```text
chassiss auth import --output ~/.chassiss/my-credential.yaml
```

Import validates armor structure, strict Base64, envelope version, digest, YAML schema, role actions, and private-key length. It writes mode `0600` and refuses to overwrite any existing path.

Armor contains the private key. Base64 is a text encoding and provides no secrecy. Use a controlled channel and avoid persistent chat history, public logs, and recordings.

## Revocation and rotation

Inspect metadata, then revoke by credential ID.

```text
chassiss auth inspect ~/.chassiss/cred-developer-1.yaml

chassiss --credential ~/.chassiss/master-root.yaml \
  auth revoke <credential-id> \
  --reason compromised
```

Revocation advances `trust.revision`. Future use returns `CHS-AUTH-REVOKED`.

Version 0.3 has no `rotate` command. Issue and validate a replacement, then revoke the old credential. A Task records its original `owner_grant_id`, while another active Developer credential with the same actor can continue the Task.

## Trust concurrency

Authorization changes use `--expect-trust-revision`.

```text
chassiss --expect-trust-revision 7 \
  --credential ~/.chassiss/master-root.yaml \
  auth issue --actor developer-2 --role developer
```

A stale value returns `CHS-CONFLICT-TRUST-REVISION`. Reload Trust metadata before deciding whether to retry.

## Authentication diagnostics

JSON errors may include these stable diagnostic categories.

- `grant_not_found`
- `revoked`
- `metadata_mismatch`
- `policy_mismatch`
- `key_invalid`
- `key_mismatch`
- `signature_invalid`
- `project_mismatch`

Automation should use codes and remediation fields instead of parsing human messages.

