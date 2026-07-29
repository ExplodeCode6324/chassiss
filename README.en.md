# CHASSISS

[中文](README.md) | English

CHASSISS is a multi-agent project protocol that uses signed Git transitions as
its shared ledger and gives its CLI exclusive ownership of the supported Git
workflow. Architecture describes durable structure, a Taskbook describes one
round of work, and State contains only the projection needed to verify the next
transition. Root and Grants determine which Actor and Key may perform each
Action over which Tasks and Resources.

This repository is a new `chassiss/v1` implementation. It is not compatible with
the old project's v0.x control directory, credentials, or Mission model. The old
repository was used read-only as a documentation-structure reference. The
protocol documents still await Master's review, so this is a lock candidate,
not a frozen v1 release.

## Core boundaries

- `refs/heads/main` is the sole authoritative shared mainline; every main commit
  is an SSH-signed Transition.
- `.chassiss/state.json` is reducer-generated canonical JSON and must not be
  edited manually.
- `docs/architecture.yaml` and the active `docs/taskbook.yaml` change only
  through controlled commands.
- Agents edit ordinary files only inside a CLI-managed worktree and within the
  Task's declared `writes`.
- Check Results are mechanical evidence; a Review Verdict is a Reviewer's
  semantic judgment.
- Integration is a two-parent commit for the exact candidate. Relevant or
  unknown drift requires a new Review.
- Private keys, checkpoints, pending operations, worktree registrations, and
  caches remain in external Local State.
- The protocol does not depend on GitHub, GitLab, an external database, external
  CI, or a particular hosting provider.

## Build

Go 1.24, Git 2.34+ with SSH commit signing, and `ssh-keygen` are required.

```text
go build -o bin/chassiss ./cmd/chassiss
go test ./...
go vet ./...
./bin/chassiss version --json
./bin/chassiss help --json
```

`make build`, `make test`, and `make check` provide the same local entry points.
Release builds inject version, build digest, and release identity through
ldflags.

## Minimal local flow

Copy and adapt `docs/templates/architecture.yaml` and
`docs/templates/taskbook.yaml`, then run this in a project directory with no Git
history:

```text
chassiss key generate --id KEY-ROOT-01 --actor master --json
chassiss init \
  --project PRJ-EXAMPLE \
  --architecture /outside/project/architecture.yaml \
  --taskbook /outside/project/taskbook.yaml \
  --root-key KEY-ROOT-01 \
  --json
chassiss verify --full --json
chassiss context --json
```

Developers create a key and Grant Request outside the repository. Root reviews
the explicit Capability, Task scope, Resource scope, and limits before signing a
Grant. A normal Task loop is:

```text
chassiss context TASK-001 --json
chassiss task start TASK-001 --json
chassiss work open TASK-001 --json
# Edit only in the returned managed worktree.
chassiss work status TASK-001 --json
chassiss work commit TASK-001 --message "implement TASK-001" --json
chassiss check TASK-001 --json
chassiss submit TASK-001 --json
```

A Reviewer then prepares and signs a semantic Review Report, and an Integrator
applies the exact approved candidate. When all Tasks are terminal, a Reviewer
completes the Closure Report and runs `taskbook archive`.

Do not directly run Git-mutating commands such as add, commit, branch, checkout,
worktree, merge, rebase, reset, push, or config. The reusable Agent instructions
live in [`skills/chassiss/`](skills/chassiss/). The Skill bundles digest-verified
macOS/Linux arm64/amd64 CLIs but does not parse protocol State. Its Master
orchestration contract assigns every temporary Agent an isolated
worktree/Key/Grant, reclaims them after success, and records a signed
`attempt abandon` before failed-attempt cleanup.

## Documentation

- [Normative specification index](docs/README.md)
- [English guides](docs/en/README.md)
- [中文使用指南](docs/cn/README.md)
- [Implementation and legacy differences](docs/12-implementation-differences.md)
- [Cross-implementation fixtures](fixtures/README.md)

Use `chassiss help --json` as the machine-readable command source. Stable Error
codes, exit codes, and response envelopes are defined by the normative specs.

## Verification status

Local tests cover canonical JSON, domain-separated digests, strict YAML, State
invariants, reducers, SSHSIG, Git objects and topology, historical verification,
Local State, Task lifecycle, Review/Integration, Taskbook Closure, offline
proposals, and Owner Apply. Fourteen cross-implementation fixture categories are
present.

At Master's request, this pass did not perform real GitHub/network remote/system
secret-store integration tests. Remaining lock-candidate gaps and their security
impact are explicitly tracked in the
[implementation differences](docs/12-implementation-differences.md).
