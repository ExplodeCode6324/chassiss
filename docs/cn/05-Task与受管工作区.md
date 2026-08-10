# Task 与受管工作区

Task phase：

```text
ready → active → submitted → approved → closed
                    ↘ request_changes → active
ready/active/submitted/approved → cancelled|superseded
```

`blocked:true` 是非 terminal overlay。State 对每个 phase 使用严格稀疏投影，避免
旧 Attempt/Review 数据泄漏到新阶段。

## 开始工作

```text
chassiss context TASK-001 --json
chassiss task start TASK-001 --json
chassiss work open TASK-001 --json
```

后两条返回 managed worktree absolute path。所有源码编辑和本地构建都必须在该
目录完成。不要自行创建 branch、checkout 或 worktree。每次尝试都得到不复用
的 `<project>/<task>/<base-prefix>/<actor>` worktree path 和对应 Work Ref。

## Work loop

```text
chassiss work status TASK-001 --json
chassiss work diff TASK-001 --against base --json
chassiss work commit TASK-001 --message "..." --json
chassiss work log TASK-001 --json
chassiss check TASK-001 --json
chassiss submit TASK-001 --json
```

Work Commit 使用 frozen Task runtime 确定性解析出的 Task Actor Ed25519 key
签名，不接受 `--key/--grant`，也不依赖全局 selected identity。CLI 只 stage
指定路径，检查 symlink、protected path、frozen `writes`、linear parent chain
和 effective `max_changed_paths`。并行 Agent 在后续 `submit` Transition 上显式
传自己的 `--key/--grant`。

`work diff` 把 untracked file 作为新增内容显示，并使用 disposable index
计算，不改变 worktree 的真实 index。

`submit` 要求 clean worktree，在 exact Work Head 重跑 frozen Checks，发布
canonical Work Ref，并将 Submission Evidence 绑定 base/head/tree、changed
paths digest、contract blobs、submitter fingerprint 和 Check Results。

`work restore` 只接受 explicit paths；`work remove` 对 dirty 或 unreachable data
默认拒绝。`task release` 只允许未产生变化的 active work。

临时 Agent 失败时，Master 必须在删除前执行 `attempt abandon`。完整失败记录
进入签名 Transition，State 只保留轻量索引，CLI 随后销毁失败 worktree/Work
Ref。使用 `attempt failures TASK-001 [--operation OPR-...]` 查询。
