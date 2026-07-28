# Owner Apply

Owner Apply 是 Master 明确要求的人工接管通道，不属于普通 Agent Work loop。

前置条件：

- 没有 active/submitted/approved Task；
- 没有 active/orphan managed worktree；
- 没有 unresolved pending Operation；
- source 是当前注册的 non-managed main worktree；
- main worktree 从 exact verified base 开始；
- 变更不含 `.chassiss/state.json`、Architecture、Taskbook、archive 或其他
  protected path；
- Grant 具有 `owner.apply` 和 global Task/Resource scope。

先预览：

```text
chassiss owner apply \
  --reason "emergency human repair" \
  --summary "what changed and why" \
  --json
```

CLI 返回 exact candidate tree、changed paths、warning 和 `requires_yes=true`。
Master 检查后执行同一命令并加 `--yes`。

发布的 `owner.applied` 是 signed single-parent Transition。State bytes 必须保持
不变，ordinary tree 按 candidate 更新。命令不接受任意 source path 参数，避免
从未注册目录注入 snapshot。

不要用 Owner Apply 绕过 Task `writes`、Review、Integration、Root/Grant 错误或
relevant drift。

