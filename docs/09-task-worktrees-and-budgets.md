# Task worktree、范围与预算

Task 将交付目标转换为受控的修改边界。CHASSISS 为执行中的 Task 创建独立 Git worktree，并在提交和集成前验证范围、预算与精确版本。

## Task 的执行条件

Task 可执行前需要满足以下条件。

- 所属 Mission 已激活
- Task Artifact 已接受
- 前置 Task 已完成
- `allowed_paths` 不与其他 Active Task 冲突
- Developer 未超过 WIP 限制
- credential 覆盖目标 Project、Mission 和 Task

前置 Task 被取消时也视为已结束。前置 Task 被替代时，CLI 会沿 supersede 链检查新的 Task。

## 领取与分派

`task claim` 由 Orchestrator 为自己的 actor 领取，且同一 actor 必须具有有效 Developer grant。`task assign` 由 Orchestrator 指定其他 Developer actor。成功后，State 记录 actor、Task 与领取时的正式 baseline。

默认 WIP 限制为每名 Developer 同时处理两个 Task。限制来自项目配置，不能通过启动更多 Session 绕过。

## linked worktree

`work open` 创建 Task 专用工作区。

```text
.chassis/worktrees/<task-id-lowercase>
```

对应分支名称如下。

```text
chassiss/<task-id-lowercase>
```

CHASSISS 记录工作区路径、分支、Git worktree identity 和打开时的 digest。后续命令会重新验证这些绑定。移动目录、删除 worktree、切换分支或复用同名目录都会导致拒绝。

正式项目目录代表受控 baseline。Developer 的实现只应发生在 CLI 返回的 Task worktree 中。

## `allowed_paths`

每个 Task 必须声明允许修改的路径模式。路径使用项目根目录的相对形式。

```yaml
allowed_paths:
  - src/service/**
  - tests/service/**
  - README.md
```

匹配规则中，`*` 不跨越目录分隔符，`**` 可以跨越多个目录层级。

```text
src/*.go       匹配 src/a.go
src/*.go       不匹配 src/api/a.go
src/**         匹配 src/api/a.go
```

CLI 会比较 baseline 和候选 Head 的完整变更集合。只要有一个文件落在范围外，preflight、submit 和 integration 都会拒绝。

范围冲突检查采用保守策略。两个模式可能覆盖同一路径时，CHASSISS 会阻止对应 Task 并行执行。此时应拆分目录边界、调整计划或等待先行 Task 集成。

## 独立验证路径

Task 可以声明 `verification_paths`。这些路径存放测试、fixture、golden 文件或其他由 Developer 之外的角色维护的验证源。

验证路径需要满足以下条件。

- 使用唯一的项目相对模式
- 不与 `allowed_paths` 重叠
- 在 Task baseline 中能够匹配文件
- 检查和集成期间保持内容不变

Developer 无法通过 Task 工作范围修改这些文件。若验证源需要更新，应由 Designer 创建独立 Task 并重新规划验证边界。

## 预算

Task 预算限制候选变更规模。

```yaml
budget:
  max_changed_files: 20
  max_diff_lines: 1000
  max_commits: 10
```

项目初始化时的默认值为 100 个文件、20000 行 diff 和 20 个 commit。Task 可以声明更小的预算。

CLI 以 Task baseline 到候选 Head 的差异计算指标。

| 指标 | 计算对象 |
| --- | --- |
| 文件数 | 新增、修改、删除和重命名的文件 |
| diff 行数 | 文本 diff 中新增行与删除行之和 |
| commit 数 | baseline 之后可达的 Task commit |

二进制文件计入文件数。Git 无法提供稳定文本行数时，不为该文件虚构行数。

预算用于限制一次 Task 的审查规模。接近预算时应及时通知 Orchestrator，由其判断是否拆分后续 Task。

## preflight

`work check` 在运行 Task Checks 前验证候选版本。

- 工作区和分支绑定仍然有效
- Task Head 基于冻结 baseline
- 当前 tracked 与 untracked 变化可以形成候选 tree
- 修改文件全部位于 `allowed_paths`
- 文件、diff 行和 commit 数未超预算
- 独立验证源与 baseline 相同

CLI 对工作区中的未提交变化构造临时候选 commit 用于计算范围和预算，不移动 Task branch，也不改写 Developer 的 index。Check 结果与当前 worktree snapshot digest 绑定。

## checkpoint

`work checkpoint` 记录一段签名进度说明，适合长任务中的阶段状态、风险和后续计划。checkpoint 不保存文件内容、Git tree 或 Check 结果，不能替代源码备份。它不会改变正式 baseline，也不会获得 Reviewer 批准。

## 预算耗尽与契约变化

达到预算上限后，Developer 应停止扩大修改范围。Orchestrator 可以选择以下处理方式。

- 将当前满足契约的部分提交复核
- 新建后续 Task 承载剩余工作
- 暂时 block Task，等待 Designer 调整计划
- 用 `task supersede` 建立替代关系

已接受 Artifact 不原地改写。范围、依赖或验收标准发生实质变化时，Designer 提交新的 Task Artifact。

## release、cancel 与 supersede

`task release` 由 Orchestrator 释放尚未提交、分支仍在 baseline 且工作区干净的 Task。CLI 安全移除 linked worktree 和未推进的 Task branch，Task 回到 ready 后可以重新分派。事件历史继续保留。

`task cancel` 由 Master 确认 Task 不再执行。取消不会删除事件、Submission 或 Git 证据。

`task supersede` 将旧 Task 指向新的替代 Task。依赖解析会沿替代链继续检查，旧 Task 的审计记录保持可见。

这些命令都应带明确原因，并在执行后重新 `bootstrap`。

## 清理规则

成功集成后，CLI 只在确认绑定仍然安全时清理 Task worktree。Task 分支保留，便于审计精确 Head 和恢复证据。

不要手工删除 `.chassis/worktrees`、修改关联分支或运行 Git worktree 清理命令。需要排查时先运行 `doctor` 和 `verify`。

## 下一步

- [Checks 与独立验证源](10-checks-and-verification.md)
- [并发、锁与状态冲突](12-concurrency-and-conflicts.md)
- [崩溃恢复与完整性阻断](14-recovery-and-integrity.md)
