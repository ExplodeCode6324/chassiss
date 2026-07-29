# Security model

## Trust root

A trusted clone uses the Project ID, Genesis Root fingerprint, and minimum
checkpoint. Obtain them independently from the remote. Failed verification
never advances the checkpoint.

Existing-project bootstrap also requires Master to pin a full source commit
OID. The old commit/tree and
`docs/chassiss/onboarding/source-history.md` are Root-signed provenance
statements, not authority for the old history. Source `.git`, refs, credentials,
and local State must never be imported.

## Cryptography

- Git Transitions and Work Commits: Ed25519 SSH commit signatures;
- Grant Requests: Ed25519 SSHSIG in a fixed namespace;
- protocol digests: SHA-256 with object-type domain separation;
- State: RFC 8785-compatible canonical JSON plus one trailing LF;
- Operation/Evidence: canonical JSON bound into commit trailers with base64url.

## Data boundary

Never place these in Git, JSON responses, Errors, or logs:

```text
private-key bytes
secret-store unlock data
remote password/token
machine-local key/worktree paths in shared Evidence
session or cache content
```

Local State uses owner-only directories/files, temporary files, fsync, and
atomic replacement. POSIX permission bits are validated; Windows currently
relies on inherited user-profile ACLs, with explicit DACL construction and
verification remaining a pre-lock gap. Candidate worktrees and indices are
isolated. Git and Check processes receive argv arrays rather than shell commands.

## Out of scope

CHASSISS does not sandbox Task code or Check commands. Malicious code may read
anything accessible to the current OS user. Use isolated accounts, containers,
or controlled runners for high-risk builds. A same-Actor Review warning is not
organizational independence.

See the [implementation differences](../12-implementation-differences.md) for
the file-key backend and untested remote fault-injection gaps.
