# Existing project onboarding

An existing Git project enters CHASSISS through a Root-only source bootstrap.
The new authoritative first-parent history starts at the bootstrap commit. The
old commit/tree remains a source reference and receives no CHASSISS authority.

## 1. Prepare the source

Freeze the old repository and select a full commit OID:

```text
git rev-parse HEAD
```

Before adoption, check that:

- the commit is the exact snapshot Master intends to adopt;
- no `.chassiss/**` or CHASSISS protected-path collision exists;
- submodules are flattened, removed, or handled separately;
- symlinks cannot escape the repository;
- secrets, credentials, `.env` files, generated artifacts, and large files will
  not enter by mistake;
- optional Markdown history notes contain only useful releases, decisions,
  migrations, and known debt, never local paths or secrets.

If remediation changes the snapshot, Master must select the new full commit OID.
The CLI never silently edits the source.

## 2. Create the Root-only bootstrap

Generate Root outside both source and target, then enter an empty target:

```text
chassiss bootstrap \
  --project PRJ-EXAMPLE \
  --source /path/to/existing-repository \
  --ref <full-source-commit-oid> \
  --root-key KEY-ROOT-01 \
  --history /outside/project/history-notes.md \
  --json
```

`--history` is optional. The CLI:

1. reads the exact source commit/tree without mutation;
2. rewrites ordinary blobs and safe relative symlinks;
3. rejects protected collisions, submodules, invalid paths, and escaping
   symlinks;
4. never copies source `.git`, refs, or credentials;
5. generates protected
   `docs/chassiss/onboarding/source-history.md`;
6. creates a zero-parent, Root-self-signed `project.bootstrap`.

Run `verify --full` and `context`. Architecture, Taskbook, and Tasks must be
null/null/empty, and State `project.source` must match the source-history
document.

## 3. Authorize the Architecture Agent

The Agent creates a key and proof-of-possession Request outside the Project:

```text
chassiss key generate --id KEY-ARCHITECT-01 --actor architect --json
chassiss grant request \
  --project PRJ-EXAMPLE \
  --key KEY-ARCHITECT-01 \
  --capability architecture.establish \
  --task-scope '*' \
  --resource-scope '*' \
  --limits unbounded \
  --output /outside/project/architect-request.json \
  --json
```

Root reviews and publishes the same explicit capability and global scopes. The
Architecture Agent attaches its own key; Root private material is never shared.

## 4. Audit and establish Architecture

Create a project-bound candidate:

```text
chassiss architecture draft \
  --new \
  --output /outside/project/architecture.yaml \
  --json
```

Audit Module responsibilities and dependency direction, API compatibility,
Schema evolution, Dependency constraints, Config security/defaults, build and
test entry points, and unresolved risks. A scanner or Agent may create a
skeleton, but structural CLI validation cannot prove semantic accuracy; Master
or an independent Reviewer must check it.

```text
chassiss architecture validate \
  --file /outside/project/architecture.yaml \
  --json
chassiss architecture establish \
  --file /outside/project/architecture.yaml \
  --reason "audited adopted source snapshot" \
  --key KEY-ARCHITECT-01 \
  --grant GRT-ARCHITECT-01 \
  --json
chassiss verify --full --json
```

The source anchor and history document remain exact after establishment.
Architecture can never return to null.

## 5. Enter the normal development workflow

Discuss new requirements against the established Architecture, create and
validate a new Taskbook, then publish it with `taskbook open`. Root issues
separate least-authority Grants to Builders, Reviewers, and Integrators. The
normal Task→Work→Check→Submit→Review→Integration→Taskbook archive loop then
applies.

The legacy history is context only. Future authority, Task state, and acceptance
must come from verified State and signed Transitions after bootstrap.
