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
目录完成。不要自行创建 branch、checkout 或 worktree。

## Work loop

```text
chassiss work status TASK-001 --json
chassiss work diff TASK-001 --against base --json
chassiss work commit TASK-001 --message "..." --json
chassiss work log TASK-001 --json
chassiss check TASK-001 --json
chassiss submit TASK-001 --json
```

Work Commit 使用 Task Actor 的 Ed25519 key 签名。CLI 只 stage 指定路径，检查
symlink、protected path、frozen `writes`、linear parent chain 和 effective
`max_changed_paths`。

`submit` 要求 clean worktree，在 exact Work Head 重跑 frozen Checks，发布
canonical Work Ref，并将 Submission Evidence 绑定 base/head/tree、changed
paths digest、contract blobs、submitter fingerprint 和 Check Results。

`work restore` 只接受 explicit paths；`work remove` 对 dirty 或 unreachable data
默认拒绝。`task release` 只允许未产生变化的 active work。

