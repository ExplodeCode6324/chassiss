# Legacy migration and differences

Legacy v0.x and current v1 are different protocols. There is no automatic
migration, dual write, or silent upgrade. Major changes include:

- `.chassis/` control data becomes signed Git Transitions plus minimal
  `.chassiss/state.json`;
- fixed role credentials become Key, Grant, Capability, Scope, and Limit;
- `bootstrap` becomes verified `context`;
- Requirements/Architecture/Mission/Tasks become Architecture plus one active
  Taskbook;
- Git becomes the shared ledger and verification object model;
- Agents no longer perform direct Git mutations.

Recommended migration:

1. freeze the legacy project and create a read-only export;
2. have Master confirm the new Project ID, Root trust anchor, and initial
   checkpoint;
3. re-express durable structure as v1 Architecture;
4. re-express the current round as a Taskbook, including `writes`, `affects`,
   and Checks;
5. create Genesis in a new directory with no history;
6. have every Agent generate a new key and Grant Request;
7. never copy legacy credentials, control events, or private/local State into
   the new Git history.

See the [formal implementation-differences register](../12-implementation-differences.md)
for the architecture mapping, implementation choices, and open gaps.

