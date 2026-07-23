# Checks and independent verification sources

[中文](../cn/10-检查与独立验证源.md) | English

Checks are repeatable commands frozen in a Task contract. Developer runs them before Submission. Reviewer verifies their exact evidence binding. Integration reruns them on the merged candidate.

Checks provide mechanical evidence. Agents still judge requirements, architecture, quality, and risk.

## CheckSpec

```yaml
acceptance_checks:
  - id: unit-tests
    argv:
      - go
      - test
      - ./...
    cwd: src
    env:
      GOFLAGS: "-count=1"
    timeout_seconds: 300
    verification_paths:
      - internal/testdata/**
```

| Field | Meaning |
| --- | --- |
| `id` | Stable Check identifier |
| `argv` | Program and argument array |
| `cwd` | Project-relative working directory |
| `env` | Explicit extra environment |
| `timeout_seconds` | Runtime limit |
| `verification_paths` | Independently maintained evidence |
| `shell` | Explicit shell execution |

Timeout must be between 1 and 86400 seconds.

## Shell policy

The default executes `argv` directly. Pipes, redirection, command substitution, and glob expansion receive no shell meaning.

With `shell: true`, `argv` must contain exactly one script string. Keep shell scripts small and independent from undeclared environment state.

## Working-directory boundary

`cwd` is relative to the Task worktree. The CLI resolves symlinks and confirms that the final directory stays inside that worktree.

Absolute paths, `..` escapes, external symlinks, missing paths, and non-directories are rejected.

## Environment

Checks receive only these base variables plus declared `env`.

- `PATH`
- Temporary `HOME`
- `TMPDIR`
- `GOCACHE`
- `GOMODCACHE`
- `LANG`

Other session variables are not inherited. Do not place secrets in artifacts.

## Verification sources

Verification paths may contain acceptance tests, golden vectors, fixtures, protocol samples, and security-policy data.

The CLI computes their baseline digest and recomputes it during Developer Check, Review, and Integration. Patterns must be nonempty, unique, project-relative, disjoint from `allowed_paths`, and matched by baseline files.

## Developer stage

`work check` runs selected frozen Checks, then validates scope, budget, snapshot, and verification evidence.

Output is trimmed to the last 4000 characters. A timeout records exit code 124.

Every required Check must pass for the same snapshot before `work submit`. Any change to file content, index, or Task Head makes the evidence stale.

## Reviewer stage

`review check` validates the Submission manifest, exact commit, file set, budget, scope, verification sources, and stored Developer Check evidence.

Version 0.3 does not execute the Check processes again during Review. It confirms spec, snapshot, and verification digests. Reviewer still performs semantic review and may manually run project tests.

## Integration stage

Integration merges the Task into an isolated candidate based on the current formal baseline. It reruns frozen Checks and recomputes verification evidence on the merged tree.

The formal branch advances only after the candidate passes.

## Evidence binding

Check evidence binds at least the following data.

- Task and Submission
- Baseline and candidate Head
- Worktree snapshot digest
- Changed files and metrics
- CheckSpec and results
- Verification digest

Copied terminal output or a verbal success report cannot replace CLI evidence.

## Writing effective Checks

- Keep commands repeatable
- Pin dependencies and fixtures
- Run fast Checks first
- Set explicit timeouts
- Avoid network and global user configuration
- Keep personal directories out of the command
- Put critical acceptance evidence in independent paths
- Preserve useful failure output

CHASSISS does not provision external services. Projects should provide deterministic local substitutes, containers, or documented test environments.

## Next steps

- [Review, integration, and audit](11-review-and-integration.md)
- [Security model and known risks](19-security-model.md)
- [Protocol, versions, and compatibility](20-protocol-and-compatibility.md)

