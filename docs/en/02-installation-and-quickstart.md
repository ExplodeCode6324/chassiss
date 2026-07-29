# Installation and quickstart

## Prerequisites

- Go 1.24;
- Git 2.34+;
- OpenSSH `ssh-keygen`;
- a directory with no Git history for init, or a trusted remote for clone.

Build locally:

```text
go build -o bin/chassiss ./cmd/chassiss
./bin/chassiss version --json
./bin/chassiss help --json
```

Do not execute a same-named binary supplied by a controlled Project. Agents use
the installed Skill's absolute `skills/chassiss/scripts/chassiss` path. Its
launcher selects a bundled macOS/Linux arm64/amd64 artifact and verifies the
manifest digest before execution.

## Initialize

Copy the templates outside the Project and adapt them. Keep Root private
material outside the Project as well:

```text
chassiss key generate --id KEY-ROOT-01 --actor master --json
cd /path/to/new-project
chassiss init \
  --project PRJ-EXAMPLE \
  --architecture /safe/drafts/architecture.yaml \
  --taskbook /safe/drafts/taskbook.yaml \
  --root-key KEY-ROOT-01 \
  --json
chassiss verify --full --json
chassiss context --json
```

`init` snapshots ordinary files, adds both contracts and State, and creates a
zero-parent Root self-signed Genesis. Git history owned by the target directory
is rejected; a parent directory's unrelated repository is not inherited.

## Trusted clone

Obtain the Project ID, Genesis Root fingerprint, and minimum checkpoint through
a trusted channel independent from the remote:

```text
chassiss clone <remote> <directory> \
  --project PRJ-EXAMPLE \
  --root-fingerprint SHA256:... \
  --checkpoint <full-oid> \
  --json
```

`--untrusted-read-only` performs an explicit one-shot consistency check and does
not establish mutation authority.

On every project entry run:

```text
chassiss version --json
chassiss context --json
```
