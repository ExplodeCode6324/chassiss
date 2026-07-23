# Reviewer、集成与审计

复核由机械验证和语义判断组成。CLI 固定代码版本、重放 Checks 并保存证据。Reviewer 判断实现是否满足 Requirements、Architecture、Task 验收条件和项目质量要求。

## Submission

Developer 运行 `work submit` 后，CLI 生成不可变 Submission manifest。manifest 包含以下内容。

- Submission ID 和 Task ID
- Developer actor
- Task baseline 与精确 Head commit
- 修改文件列表
- Check 结果
- handoff
- commit message
- 文件、diff 行和 commit 数指标
- 创建时间

Submission digest 覆盖完整 manifest。任何字段变化都会得到不同 digest。

## 独立性约束

批准者 actor 必须与 Submission actor 不同。使用另一个 credential 文件但保持同一 actor，仍然不能通过独立性检查。

最小配置中的 Build Agent 可以同时承担 Orchestrator 和 Developer，但 Review Agent 应使用独立 actor 和独立 Session。

## 获取复核上下文

Reviewer 先运行 `bootstrap`，再读取队列和目标 Submission。

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/reviewer.cred \
  review list

chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/reviewer.cred \
  review context SUB-001
```

context 包含 Task 契约、Submission manifest、Developer handoff、文件列表和完整 diff。Reviewer 还应从受控项目文档读取 Requirements 与 Architecture，不应只依据 diff 摘要作出 verdict。

## 机械检查

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/reviewer.cred \
  review check SUB-001
```

检查内容包括以下项目。

- Submission digest 与 manifest 一致
- 文件列表与 Git diff 一致
- 修改范围和预算合规
- commit message 与 manifest 一致
- Developer Check 结果绑定精确 Head snapshot
- 独立验证源仍与冻结 baseline 相同

检查失败时 Reviewer 应停止审批，保存诊断结果并交还 Developer 或 Orchestrator 处理。

当前 `review check` 不重新执行 Check 命令。它验证已有结果的 spec digest、snapshot digest 和 verification digest。Integration 会在 merged candidate 上重新执行 Checks。

## 语义复核

机械检查通过后，Reviewer 至少检查以下方面。

- 验收条件是否逐项满足
- 实现是否遵守 Architecture 的边界
- 错误处理和失败路径是否完整
- 安全与权限边界是否收窄
- 测试是否覆盖关键行为
- 兼容性和迁移影响是否说明
- handoff 是否准确描述剩余风险

Designer 可以提供设计一致性意见。正式 verdict 由 Reviewer 使用自己的 credential 提交。

## 批准

Review report 应记录判断依据、检查范围和仍需关注的风险。

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/reviewer.cred \
  review approve SUB-001 --report review.md
```

report 必须是有效 UTF-8，内容不能为空，大小上限为 64 KiB。批准绑定精确 Submission digest。Developer 新提交一个版本后，旧批准不能转移。

## 退回修改

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/reviewer.cred \
  review request-changes SUB-001 --report review.md
```

Task 进入 `changes_requested`。Developer 根据 report 修改工作区、重新运行 `work check`，然后由 `work submit` 创建新的 Task commit 和 Submission。

`review history` 保留历次 Submission、Reviewer 和 verdict，便于判断问题是否确实解决。

## 集成前检查

作出批准的 Reviewer 对获批 Submission 运行以下命令。

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/reviewer.cred \
  integrate check SUB-001
```

`integrate check` 验证 Reviewer 批准仍指向相同 digest、Submission manifest 和 Task 证据仍然有效。正式项目目录、Task branch Head 和 State baseline 会在 `integrate apply` 取得项目锁后再次验证。

## 隔离候选集成

`integrate apply` 不直接在正式工作区试合并。CLI 在 `.chassis/cache` 下创建 detached 候选工作区，并执行以下过程。

执行者必须是批准该 Submission 的 Reviewer actor，且 approval 仍需绑定相同 digest。

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/reviewer.cred \
  integrate apply SUB-001
```

1. 从当前正式 baseline 构造候选版本
2. 对 Task Head 执行非 fast-forward、暂不提交的合并
3. 生成确定的 merged tree
4. 重跑冻结 Checks
5. 验证独立验证源
6. 创建 `Integrate <task-id>` commit
7. 确认 commit tree 与已检查 tree 完全相同
8. 以 fast-forward 推进正式分支
9. 写入集成事件和证据

合并冲突、Check 失败或 tree 不一致都会阻止正式分支更新。

## 集成证据

Integration 记录以下核心证据。

- 正式 baseline
- Task Head 与 Submission digest
- Reviewer approval
- merged tree digest
- Check 和验证源结果
- 集成 commit
- 新正式 baseline

事件写入完成后，Task 进入 `integrated`。CLI 会在安全条件满足时清理候选和 Task worktree，Task 分支继续保留。

## 失败后的处理

合并冲突通常表示 Task baseline 与新的正式 baseline 需要人工协调。Orchestrator 不应 force merge 或改写已批准分支。

推荐流程如下。

1. 保留失败诊断
2. 重新运行 `bootstrap`
3. 由 Orchestrator 协调 Developer 或创建后续 Task
4. 在受控 worktree 中解决冲突
5. 重新 check、submit 和 review
6. 由批准新 Submission 的 Reviewer 执行 integration

如果正式 Git 状态与 operation journal 不一致，先运行 `recover` 和 `verify`，不要手工 reset。

## 下一步

- [并发、锁与状态冲突](12-concurrency-and-conflicts.md)
- [崩溃恢复与完整性阻断](14-recovery-and-integrity.md)
- [Publish 与多机器协作](15-publish-and-multi-machine.md)
