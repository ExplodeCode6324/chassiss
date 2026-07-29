# Overview

CHASSISS separates multi-agent development into four layers:

1. Git stores signed history, ordinary project files, and shared contracts.
2. The reducer computes a complete next State from parent State, a Semantic
   Operation, Execution Evidence, and independently verified Git facts.
3. The CLI owns authorization, Git mutations, worktrees, Checks, signing, sync,
   and recovery.
4. Agents and humans provide requirements, design, implementation, and semantic
   Review judgments.

One Git repository is one Project. `refs/heads/main` is the sole authoritative
mainline. Architecture describes the durable Resource graph. One active
Taskbook describes the current round of Requirements, Constraints, Tasks, and
Checks; it must be archived before the next Taskbook opens.

Shared protocol data is limited to:

```text
.chassiss/state.json
docs/architecture.yaml
docs/taskbook.yaml
docs/taskbooks/archive/<taskbook-id>.yaml
signed Git history and retained refs
```

Private keys, remote trust anchors, minimum checkpoints, pending operations, and
managed-worktree registrations remain outside the repository. Local State does
not grant authority; current verified Grants do.

CHASSISS is designed for compliant but fallible agents. It constrains workflow
and audit boundaries; it is not a malicious-code sandbox and does not prove that
an implementation satisfies its requirements.

Continue with [Installation and quickstart](02-installation-and-quickstart.md).

