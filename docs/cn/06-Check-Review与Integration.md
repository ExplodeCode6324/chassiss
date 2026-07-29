# Check、Review 与 Integration

Check 是 argv array，不是 shell string。执行 Context 绑定：

```text
Task + phase + exact head/tree + Architecture blob + Taskbook blob
```

stdout/stderr、exit status、duration 和 binding 进入 Check Result。历史 verifier
验证签名结果与 binding，不在回放历史时重新运行命令。

## Review

Reviewer 先准备 exact candidate：

```text
chassiss review TASK-001 \
  --prepare \
  --output /outside/review-context.json \
  --report-output /outside/review-report.json \
  --json
```

Reviewer 阅读 candidate、Requirements、Architecture、findings 和
reviewer-attention。Prepare 会返回 input schema，以及按每条 frozen attention
填充 response slot 的 Report template。Reviewer 填写
`chassiss.review-report/v1`，再执行：

```text
chassiss review TASK-001 \
  --verdict approve \
  --report /outside/review-report.json \
  --json
```

`request_changes` 会回到稀疏 active projection。Passing Checks 不能自动生成
approve。Reviewer 与 submitter 同 Actor/同 key 时协议仍可接受，但 CLI 必须向
Master 显示 warning，不能宣称独立 Review。

完整 Report 只保存在签名 history，State 保存轻量 audit index。使用
`review list TASK-001` 和 `review show TASK-001 --operation OPR-...` 解析。

## Integration

```text
chassiss integrate TASK-001 --json
```

CLI 重新分类 Review main 到 current main 的 drift。Unrelated drift 才能继续；
relevant、unknown 或 candidate conflict 必须重新 Review。Integration 在临时
isolated candidate worktree 重跑 frozen Checks，然后创建：

```text
parent[0] = exact current main
parent[1] = exact Attempt Head
tree      = exact recomputed candidate
```

成功后 State 只保留 `phase=closed`，Work Ref/worktree 进入安全清理流程。
