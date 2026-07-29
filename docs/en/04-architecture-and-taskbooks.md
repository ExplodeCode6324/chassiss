# Architecture and Taskbooks

Architecture is the durable cross-workflow contract. It contains Modules, APIs,
Schemas, Dependencies, and Configs. Resources use typed IDs such as
`module:core` and form an acyclic `requires` graph.

A Taskbook describes one work round:

- workflow outcome, completion criteria, and workflow Checks;
- Requirements and acceptance criteria;
- Constraints;
- Tasks, dependencies, deliverables, Modules, `writes`, `affects`, Checks,
  limits, stop conditions, and reviewer attention.

The YAML parser accepts a closed safe subset. It rejects duplicate keys, floats,
timestamps, tags, aliases, BOM, CRLF, invalid UTF-8, non-NFC paths, and unknown
core fields.

## Query

```text
chassiss taskbook show --task TASK-001 --json
chassiss architecture show module:core --json
chassiss architecture requires module:core --transitive --json
chassiss architecture required-by schema:state --transitive --json
chassiss architecture impact schema:state --json
```

## Controlled changes

Drafts must live outside the Project. The CLI creates a sidecar bound to the
exact base blobs:

```text
chassiss taskbook draft --output /outside/taskbook.yaml --json
chassiss taskbook validate --file /outside/taskbook.yaml --json
chassiss taskbook diff --file /outside/taskbook.yaml --json
chassiss taskbook update --file /outside/taskbook.yaml --reason "..." --json
```

Architecture can change only without an active Taskbook. Taskbook updates may
change only the protocol-permitted ready portions and cannot mutate a non-ready
Task contract.

After every Task is terminal, a Reviewer runs
`taskbook archive --prepare --output <file>` to create a hydrated Closure Report
with exact Task and completion-criteria response slots, completes it, and runs
`taskbook archive --report <file>`. The CLI reruns all workflow Checks on exact
current main, records signed Evidence, relocates the exact Taskbook blob, and
clears the State Task projection.
