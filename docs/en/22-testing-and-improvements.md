# Test record, known issues, and improvement directions

[中文](../cn/22-测试记录已知问题与改进方向.md) | English

This chapter records reproducible test baselines, current limitations, and future work. Results apply to the listed source and environment.

## 2026-07-23 baseline

| Item | Value |
| --- | --- |
| Operating system | macOS Darwin 24.6.0 |
| Architecture | arm64 |
| Go | 1.25.6 |
| Git | 2.39.5 Apple Git-154 |
| Tested source baseline | `254e79d` |
| CLI | 0.3.0-dev |
| API | chassiss.dev/v2 |

Commands follow.

```sh
cd src
go test ./... -count=1
go test -race ./... -count=1
go test ./internal/app -list '^Test'
go vet ./...
go build ./...
```

Results follow.

- Standard test suite passed
- Race Detector suite passed
- `internal/app` contains 78 top-level tests
- `cmd/chassiss` has no separate test file
- `go vet` passed
- Every Go package built
- No failures occurred in this run

Bundled checksums also passed for macOS and Linux on amd64 and arm64.

```sh
cd cli
shasum -a 256 -c SHA256SUMS

cd skills/chassiss/bin
shasum -a 256 -c SHA256SUMS
```

## Current coverage

### Initialization and lifecycle

- Greenfield and Brownfield end-to-end flows
- All four artifact types
- Complete four-role Event V4 lifecycle
- Mission acceptance and return to idle

### Authorization

- Root and role credentials
- Auth issue recovery at every durable phase
- Revoke journal and idempotency
- Trust revision CAS
- Concurrent issue and concurrent issue plus revoke
- Validity windows and resource scopes
- Armor round trip and tamper rejection
- Diagnostic categories

### Checks and Git evidence

- Direct argv and paths with spaces
- Sanitized environment
- Timeout
- Symlink `cwd` escape rejection
- Content, file mode, and symlink digests
- Rename evidence
- File, line, binary, and commit metrics

### Review and Integration

- Rejection after approved branch movement
- Checks on merged candidate
- Merge parent and evidence binding
- Integration journal recovery
- Independent Reviewer
- Change-request loop

### Concurrency and recovery

- Kernel lock cannot be stolen by file age
- State revision conflict
- Crash injection at durable phases
- Untouched operation cancellation
- Exact post-state recovery
- Integrity block on mismatched site

### Owner and Publish

- Owner quiescence and protected paths
- Single commit and audit evidence
- Owner history
- Fast-forward Publish
- Non-fast-forward rejection
- Endpoint digest
- Publish recovery phases

### Protocol

- Event V4 canonical bytes
- Event and Trust golden digests
- Strict payload fields
- State rebuild from events
- Legacy Config, State, and Event rejection

## Known issues

### Control state does not synchronize between machines

Two clones with different `.chassis` copies cannot share one Trust revision, event chain, Task ownership, or journal state.

Maintain one authoritative control end. Git remote synchronizes formal code only.

Status is a known architecture limit.

### Windows writes are unavailable

The required lock backend is missing and writes return `CHS-LOCK-UNSUPPORTED`.

Use macOS or Linux.

Status is unimplemented.

### Credentials are persistent by default

Exposure lasts until Master revokes a credential.

Use TTL, narrow scopes, and regular rotation.

Status is a security and usability improvement area.

### `--persistent` input is not strict

The flag can appear with `--expires-at` or `--ttl-seconds`. The expiration still wins.

Use `--persistent` only without expiration options.

Status is pending argument validation.

### Credential files do not use an OS keychain

Private keys are mode `0600` YAML files. A shared user, malicious process, or unsafe backup may read them.

Use isolated accounts or runners.

Status is pending stronger storage isolation.

### Checks have no OS sandbox

Direct argv, limited environment, and `cwd` boundaries reduce exposure, while the process still has current-user permissions.

Run high-risk projects in containers or dedicated runners.

Status is pending execution isolation.

### `review check` does not rerun commands

Review verifies manifest, range, scope, budget, snapshot, and verification digests. It does not start Check processes.

Reviewer may manually test. `integrate apply` always reruns frozen Checks before advancing baseline.

Status is current workflow design. A signed Reviewer rerun may be added later.

### Some event text has no common size limit

Review reports and Owner reasons are limited to 64 KiB. Checkpoints, handoffs, Mission evidence, and some block reasons do not share this limit.

Keep event text short and reference project files or digests for larger evidence.

Status is pending unified input limits.

### Git is the only content backend

Config records `content_backend`, while version 0.3 implements only `local-git`.

Status is an unimplemented extension point.

### Legacy schemas have no migrator

The current CLI explicitly rejects old Config, State, and Event schemas.

Keep a compatible old CLI or establish a verified new V2 control end.

Status is missing upgrade tooling.

### Publish targets share one adapter

GitHub, GitLab, and remote-git differ in recorded target only. Platform APIs, branch protection, pull requests, and releases are not integrated.

Status is pending platform adapters.

### CLI is a development release

Version is `0.3.0-dev`. Release signing, a stable support period, and migration commitments are undefined.

Pin source commit, verify checksums, and keep every agent on one binary.

Status is pending release engineering.

### Test platform coverage is limited

The complete recorded run used macOS arm64. Other bundled targets passed checksum verification but did not run this native end-to-end suite.

Run tests and smoke tests on the target platform before deployment.

Status is pending a CI matrix.

## Improvement priorities

### P0 Control-state synchronization

- Remote signed-event storage
- Monotonic Trust revision
- Distributed operation ownership
- Offline-fork detection
- Control-end migration protocol
- Recovery evidence during backend failure

### P0 Credential isolation

Explore short-lived credentials, single-action tokens, and a broker that keeps Root inside a human or hardware boundary.

### P1 Check sandbox

Add filesystem, network, process, and resource limits with one evidence model across macOS, Linux containers, and CI.

### P1 Schema migration

Build an offline migrator that preserves source control state, validates every digest, writes a migration report, and leaves a recoverable source.

### P1 Release and CI

- Native macOS and Linux dual-architecture matrix
- Race Detector
- Bundled binary smoke tests
- Checksum parity
- Release signing
- SBOM
- Reproducible build record

### P2 Platform Publish adapters

Add GitHub and GitLab branch-protection checks, identity confirmation, and optional release evidence.

### P2 User experience

- Shell completion
- Safe cache cleanup
- Read-only journal inspection
- Task split assistance
- More compact bootstrap output
- Credential expiration alerts
- Automatically redacted issue bundles

### P2 Performance and maintenance

Measure event replay, Git diff, verification digest, and Task listing on large repositories. Any future cache must remain bound to the event chain and Git identity.

## Planned testing

| Area | Plan |
| --- | --- |
| Platforms | Linux amd64, Linux arm64, macOS amd64 native CI |
| Filesystems | Case-sensitive and insensitive paths, symlink boundaries |
| Git | Large repositories, binaries, shallow clones, remote latency |
| Concurrency | Long-running multi-process contention and fault injection |
| Auth | Large grant sets, bulk revocation, time boundaries |
| Recovery | Full disk, fsync failure, uncertain remote response |
| Security | Malicious artifacts, Check injection, Trust rollback |
| UX | Time from install to first Mission |
| Automation | Long unattended runs with multiple models |

## Future test record template

```text
Date
Source commit
CLI and schema versions
Operating system and architecture
Go and Git versions
Commands
Passed items
Failed items
New issues
Workaround
Owner
Next step
```

## Contributing test results

State whether the test used a native platform, container, or cross-compiled binary. Fault-injection reports should include the operation stage and journal state. Remove Root, credentials, tokens, remote URLs, and private source from every attachment.

## Related documentation

- [Error handling and troubleshooting](18-troubleshooting.md)
- [Security model and known risks](19-security-model.md)
- [Protocol, versions, and compatibility](20-protocol-and-compatibility.md)
- [Examples, automation, and FAQ](21-examples-automation-faq.md)
