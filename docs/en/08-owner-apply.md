# Owner Apply

Owner Apply is a Master-directed human takeover path, not a normal Agent work
loop.

Preconditions:

- no active, submitted, or approved Task;
- no active or orphan managed worktree;
- no unresolved pending operation;
- the source is the registered non-managed main worktree;
- the worktree starts from exact verified base;
- changes exclude State, Architecture, Taskbook, archives, and all protected
  paths;
- the Grant has `owner.apply` and global Task/Resource scope.

Preview first:

```text
chassiss owner apply \
  --reason "emergency human repair" \
  --summary "what changed and why" \
  --json
```

The response includes the exact candidate tree, changed paths, a warning, and
`requires_yes=true`. After inspection, rerun with `--yes`.

The published `owner.applied` is a signed single-parent Transition. State bytes
remain unchanged while ordinary content adopts the candidate. The command does
not accept an arbitrary source path.

Never use Owner Apply to bypass Task `writes`, Review, Integration, Root/Grant
failures, or relevant drift.

