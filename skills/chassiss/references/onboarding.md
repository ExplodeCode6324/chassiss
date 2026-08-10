# Existing project onboarding

Use this workflow only when Master asks to adopt an existing Git project.

1. Treat the source repository as read-only. Require a full source commit OID,
   an empty target directory, and a Root key outside both repositories.
2. Inspect the source for `.chassiss/**`, CHASSISS protected-path collisions,
   submodules, escaping symlinks, secrets, generated artifacts, and files that
   should not enter the new Project. Stop for Master if remediation changes the
   selected source snapshot.
3. Optionally prepare a human-authored Markdown summary of important legacy
   decisions, releases, migrations, and unresolved debt outside the target.
4. From the empty target, run:

   ```text
   chassiss bootstrap \
     --project <project-id> \
     --source <source-repository> \
     --ref <full-source-commit-oid> \
     --root-key <root-key-id> \
     [--history <outside-markdown>] \
     --json
   ```

5. Run `verify --full` and `context`. Confirm Architecture and Taskbook are null
   and `docs/chassiss/onboarding/source-history.md` identifies the exact source
   commit/tree as non-authoritative.
6. Collect a proof-of-possession Grant Request for
   `architecture.establish` with global Task/Resource scope. Root publishes the
   reviewed Grant.
7. Run `architecture draft --new --output <outside-yaml>`. Audit ordinary source
   files and build/dependency manifests, then replace the generic root node with
   accurate Module/API/Schema/Dependency/Config boundaries. The CLI verifies
   structure; a human or independent Reviewer verifies semantic accuracy.
8. Run `architecture validate --file <outside-yaml>`, then
   `architecture establish --file <outside-yaml> --reason <text>` with the
   explicit Key/Grant. Refresh `verify --full` and `context`.
9. Only after Architecture exists, use `taskbook draft --new`, discuss the next
   requirements, validate the candidate, and publish it with `taskbook open`.

Never import the source `.git` directory, old credentials, local state, or old
control files. Old commits remain outside the authoritative first-parent chain;
the signed bootstrap binds only their source commit/tree identifiers, the
curated history document, and the imported ordinary snapshot.
