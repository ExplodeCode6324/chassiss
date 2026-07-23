# Installation, platforms, and version synchronization

[中文](../cn/04-安装平台与版本同步.md) | English

The repository contains source code, standalone CLI distributions, and an agent Skill.

```text
chassiss/
├── src/
├── cli/
└── skills/chassiss/
```

`src/` is the Go module. `cli/` holds standalone binaries. `skills/chassiss/` contains the agent Skill and the same-version binaries.

## Supported platforms

| System | Architecture | Path |
| --- | --- | --- |
| macOS | Intel | `darwin-amd64/chassiss` |
| macOS | Apple Silicon | `darwin-arm64/chassiss` |
| Linux | x86-64 | `linux-amd64/chassiss` |
| Linux | ARM64 | `linux-arm64/chassiss` |

Windows does not have the required advisory-lock backend. Write operations return `CHS-LOCK-UNSUPPORTED`.

## Install the Skill

Copy the complete `skills/chassiss/` directory, including `SKILL.md`, `agents/`, `bin/`, and `SHA256SUMS`.

The Skill requires the bundled binary that matches the host. This prevents an unrelated executable from `PATH` from changing protocol behavior.

Common platform mappings follow.

| `uname -s` | `uname -m` | Directory |
| --- | --- | --- |
| Darwin | arm64 | `darwin-arm64` |
| Darwin | x86_64 | `darwin-amd64` |
| Linux | aarch64 | `linux-arm64` |
| Linux | x86_64 | `linux-amd64` |

## Verify binaries

On macOS, run this command in `skills/chassiss/bin/`.

```text
shasum -a 256 -c SHA256SUMS
```

On Linux, use the following.

```text
sha256sum -c SHA256SUMS
```

The same platform binary in `cli/` and the Skill should have the same SHA-256.

## Build from source

The module requires Go 1.23 or newer.

```text
cd src
go test ./...
go test -race ./...
go vet ./...
go build ./...
```

A release build can use the following settings.

```text
CGO_ENABLED=0 go build \
  -trimpath \
  -buildvcs=false \
  -ldflags="-s -w" \
  ./cmd/chassiss
```

After rebuilding distributions, update both checksum files and keep `cli/` and Skill copies identical.

## Greenfield initialization

Without `--existing`, the CLI creates the target directory, initializes the `main` branch, adds `.chassis/` to `.gitignore`, and creates an initial commit.

An existing Git root with commits requires `--existing`.

## Brownfield adoption

`--existing` requires the target itself to be a Git root with all of the following properties.

- Clean worktree
- At least one commit
- Determinate current branch
- No existing `.chassis/`

The CLI preserves history and the current branch. It adds `.chassis/` to `.git/info/exclude` without changing the project `.gitignore`.

## Current Git backend

Version 0.3 fixes `content_backend` to `local-git`. Git provides formal baselines, Task branches, linked worktrees, file and diff metrics, Submission commits, merge candidates, and publication.

Agents do not need to run Git lifecycle commands. The CLI owns branch, worktree, commit, and merge creation.

## Version synchronization

`publish` supports `github`, `gitlab`, and `remote-git` targets. All three currently use the normal Git remote protocol and fast-forward the exact formal baseline.

The remote synchronizes project content, while `.chassis/` remains local control state. The safest topology uses one controlled project instance for all writes. Other machines may read or synchronize formal code versions.

An independent clone cannot recover complete Mission, Task, Review, and Trust state. See [Publishing and multi-machine collaboration](15-publish-and-multi-machine.md).
