# Security model and known risks

[中文](../cn/19-安全模型与已知风险.md) | English

CHASSISS is designed for cooperative agents and human developers that follow the project workflow. The CLI enforces permissions, state, scope, evidence, and recovery. It cannot create strong secret isolation inside a fully shared operating-system account.

## Protected assets

- Master Root private key
- Role credential private keys
- Root-signed Trust store
- Signed event chain
- Formal baseline
- Artifact and Task contracts
- Submission, Review, and Integration evidence
- Operation journals

## Trust base

Project Config records the Root fingerprint. Trust contains the Root public key, grants, revocations, and Root signature. A role private key must match the current grant.

Each write validates credential project, Root fingerprint, grant and revocation, validity window, role policy, resource scope, State transition, revision, and Git evidence.

## Enforced cooperative-agent controls

CHASSISS can reject self-declared roles, revoked credentials, out-of-scope files, changed verification sources, same-actor self-review, unapproved Integration, unchecked merged trees, non-fast-forward Publish, protected Owner changes, and ambiguous recovery.

## Shared-user limitation

Agents running as the same OS user may read one another's credential files, Git configuration, shell history, clipboard, and chat history.

Role separation in this deployment provides workflow control and audit evidence. It does not defend secrets from a malicious peer process.

Use separate OS users, containers, or remote execution boundaries for stronger isolation.

## Long-lived credentials

Role credentials do not expire by default. Production use should add TTL or expiration, narrow resources and actions, rotate before expiry, and revoke old grants.

Version 0.3 has no online credential broker or one-action token.

## Base64 armor

Armor provides transport framing, versioning, and digest validation. It contains the private key in recoverable text.

Avoid public issues, normal logs, screen recordings, persistent shared chats, and untrusted clipboard services. Revoke immediately after exposure and inspect events signed during the exposure window.

## Root risk

Root compromise can break the complete project identity boundary.

- Store Root outside the project
- Restrict it to a human-controlled account
- Use encrypted storage and reliable backups
- Keep it out of agent sessions
- Never place it in CI logs or snapshots

The Root YAML has no password encryption or hardware-key support.

## Trust rollback

Root signature and revision detect ordinary Trust modification. Version 0.3 has no external transparency log or remote monotonic counter. An attacker controlling the complete local control end may attempt a coordinated rollback.

Never overwrite current Trust with an old backup. Back up complete `.chassis` with clear timestamps.

## CLI integrity

Authorization depends on the executable. A replaced binary could bypass local rules or leak credentials.

Verify bundled `SHA256SUMS`, keep every agent on one version, and use a protected install directory. Signed releases and reproducible-build verification remain future work.

## Git and external tools

Direct edits to formal refs, worktree refs, or remotes break evidence binding. `verify` detects many deviations but cannot undo leaked code or secrets.

Restrict tools and remote tokens available to agents.

## Check execution

Checks run project-declared commands as the current OS user. Direct argv, a limited environment, and `cwd` boundaries reduce risk, but version 0.3 has no OS-level Check sandbox.

Master reviews CheckSpec before artifact acceptance. Run high-risk projects in an isolated runner.

## Logs and chat

Agent context, terminal history, and external Check output may expose paths, source, or secrets. Remove credentials, tokens, URLs, and private code from issue reports.

## Autonomous Master experiment

A frontier agent may control Root and delegate the remaining roles. Shared execution commonly exposes every credential to one trust domain.

Use this mode for public, recoverable experiments with clear tests. Record model, prompt, failures, and human intervention. Workflow consistency remains available, while secret isolation and project quality are not guaranteed.

## Improvement directions

- Short-lived and single-action credentials
- OS keychain and hardware-key support
- Credential broker
- Signed releases and reproducible builds
- Check sandbox
- Remote event transparency
- Multi-control consistency protocol
- Credential-use alerts and audit export

## Next steps

- [Root and role credentials](05-credentials.md)
- [Protocol, versions, and compatibility](20-protocol-and-compatibility.md)
- [Test record and known issues](22-testing-and-improvements.md)

