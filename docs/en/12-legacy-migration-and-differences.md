# Legacy migration and differences

Legacy v0.x and current v1 are different protocols. There is no dual write,
silent upgrade, or reinterpretation of old commits as Transitions. Major
changes include:

- `.chassis/` control data becomes signed Git Transitions plus minimal
  `.chassiss/state.json`;
- fixed role credentials become Key, Grant, Capability, Scope, and Limit;
- `bootstrap` becomes verified `context`;
- Requirements/Architecture/Mission/Tasks become Architecture plus one active
  Taskbook;
- Git becomes the shared ledger and verification object model;
- Agents no longer perform direct Git mutations.

Recommended migration:

1. freeze the legacy project, pin a full source commit OID, and optionally
   curate a history summary;
2. have Master confirm the new Project ID and Root trust anchor;
3. run `bootstrap --source ... --ref <full-oid>` in an empty directory to
   import the exact ordinary snapshot;
4. confirm that `docs/chassiss/onboarding/source-history.md` labels the old
   commit/tree as non-authoritative;
5. have every Agent generate a key and proof-of-possession Grant Request;
6. have Root publish a global `architecture.establish` Grant;
7. let the Architecture Agent audit, validate, and establish the first
   Architecture;
8. create the first Taskbook for new requirements, including `writes`,
   `affects`, and Checks;
9. never copy legacy credentials, control events, `.git`, or private/local
   State into the new Git history.

See [Existing project onboarding](13-existing-project-onboarding.md) for the
complete workflow.

See the [formal implementation-differences register](../12-implementation-differences.md)
for the architecture mapping, implementation choices, and open gaps.
