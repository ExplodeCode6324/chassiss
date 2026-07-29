# Trust, keys, and Grants

The exact protocol identity is:

```text
Actor + Key + current Grant + Capability + Scope + Limit
```

A profile is only a convenience that expands explicit capabilities and limits
when a Grant is created. It is not a runtime role stored in State. The current
`developer` profile expands Task-development capabilities and requires bounded
limits.

## Create a Grant Request

```text
chassiss key generate --id KEY-AGENT-01 --actor agent-one --json
chassiss grant request \
  --project PRJ-EXAMPLE \
  --key KEY-AGENT-01 \
  --profile developer \
  --task-scope 'TASK-*' \
  --resource-scope 'module:*' \
  --limits bounded \
  --output /outside/project/request.json \
  --json
```

The Agent signs a domain-separated proof-of-possession digest with SSHSIG. The
Request never contains the private key.

After Root publishes the Grant, attach and select the already-generated local
Key in the verified Project:

```text
chassiss key attach KEY-AGENT-01 --select --json
chassiss identity select --key KEY-AGENT-01 --json
```

Attachment verifies the Key cryptographically against the current Root/Grant;
it does not create authority. Signed mutation responses expose the exact
authority in `operation.signer`.

## Root approval

Root explicitly reviews capabilities, Task scope, Resource scope, and limits:

```text
chassiss grant add \
  --request /outside/project/request.json \
  --grant-id GRT-AGENT-01 \
  --root-key KEY-ROOT-01 \
  --profile developer \
  --task-scope 'TASK-*' \
  --resource-scope 'module:*' \
  --limits bounded \
  --json
```

An offline Root may create a signed bundle with `--proposal`, followed by
`transition inspect` and `transition publish` in an online checkout. The
proposal parent must still be exact current main.

Revocation uses `grant revoke <grant-id> --reason ... --root-key ...`. Historical
signatures remain verifiable, while the revoked Grant cannot authorize a future
Transition.

Master-orchestrated Agent Grants and Keys are temporary per attempt. After
success, or after a failed attempt has been signed with `attempt abandon`,
Master revokes the Grant and removes the local Key material.

The current implementation provides only an owner-only `file:` key backend.
POSIX mode bits are validated; Windows currently relies on the inherited user
profile ACL and does not yet construct and verify a dedicated DACL. See the
documented gaps in the
[implementation differences](../12-implementation-differences.md).
