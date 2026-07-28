# Testing and release

Local quality gates:

```text
go mod tidy
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/chassiss
```

The complete CLI lifecycle test creates real temporary Git repositories,
Ed25519 keys, signed Transitions, and linked worktrees:

```text
Genesis → Grant → offline proposal/publish → Task start
→ Work Commit → submit → Review → Integration
→ Taskbook Closure/archive → Architecture update
→ Owner Apply → next Taskbook open → full verify
```

`fixtures/` contains fourteen cross-implementation categories. Tests directly
consume canonical JSON, digest, Architecture/Taskbook, State, and Genesis
Reducer vectors. Other scenario manifests pair with repository-backed Go tests.

A local loopback `git daemon` test starts an independent remote and two
checkouts, races a concurrent Root Grant against an Authority Transition, and
verifies Evidence-attempt-2 recomputation, preservation of the concurrent Grant,
and a trusted full verification from Genesis. It contacts no external service.

Release builds inject:

```text
Version
BuildDigest
ReleaseIdentity
```

Example:

```text
go build -trimpath \
  -ldflags "-s -w \
  -X github.com/ExplodeCode6324/chassiss/internal/cli.Version=v0.1.0 \
  -X github.com/ExplodeCode6324/chassiss/internal/cli.BuildDigest=<git-oid> \
  -X github.com/ExplodeCode6324/chassiss/internal/cli.ReleaseIdentity=<builder>" \
  -o dist/chassiss ./cmd/chassiss
```

Protocol lock still requires Master's documentation review, a SHA-256 docs
manifest, frozen fixtures, real remote fault injection, and cross-platform
evidence. This pass intentionally did not run real external integration tests.
