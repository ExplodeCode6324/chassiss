# CHASSISS CLI

> 面向 Agent 的 v0.1 命令索引。以 `chassiss help`、内嵌模板和实际 JSON 返回为机器事实源。

## 通用约定

```text
chassiss [--root <project>] [--json] [--credential <source>]
         [--expect-revision <n>] [--expect-trust-revision <n>]
         <group> <action>
```

- Agent 使用 `--json`；
- 读命令不修改项目；
- 写命令要求 credential；
- 写命令支持 `--expect-revision`；`--dry-run` 已保留但 v0.1 会明确拒绝，不产生伪预览；
- 成功返回结果、旧/新 revision 和允许的下一动作；
- 失败返回稳定错误码、事实、是否可重试和建议命令；
- 不提供 `state set`、任意 YAML patch 或 raw event 命令。

## 项目与恢复

```text
chassiss --credential <master-root> project init <path> [--existing]
         [--max-changed-files <n>] [--max-diff-lines <n>] [--max-commits <n>]
chassiss status
chassiss next --role <role> [--actor <actor>]
chassiss doctor
chassiss verify
chassiss recover
chassiss explain <error-code>
```

`status` 返回当前 Mission、ready/active/blocked Task、待复核 submission 和 revision。

`next` 按 role、actor 和当前状态返回候选动作；真正执行时仍会重新验证 credential、revision 和全部前置条件。

新项目的默认 Task 预算为 100 个变更文件、20,000 行增删和 20 个提交。三个 `project init` 参数可分别改写；`0` 表示该维度无限制。Task 模板会带入项目默认值，Master 接受 Task 后预算随契约冻结，不能在执行中原位放宽。

`doctor/verify` 在传入 `--credential` 时还会用该长期凭证锚定项目 Root；不传凭证只证明项目内部自洽。`recover` 依次处理 authorization、publish 与 Git/state operation journal：仅当外部结果精确匹配 journal 时补写或发布，随后从签名事件重建状态投影。不一致时进入 integrity blocked，不隐式 reset、force 或改写 trust。

## 模板和设计文档

```text
chassiss template list
chassiss template get requirements [--output docs/requirements.md]
chassiss template get architecture [--output docs/architecture.md]
chassiss template get mission --id M001 [--output docs/missions/M001.md]
chassiss template get task --id M001-T001 [--output docs/tasks/M001-T001.md]

chassiss artifact check <path>
chassiss artifact submit <path>
chassiss artifact list [--pending]
chassiss artifact context <submission-id>
chassiss artifact accept <submission-id>
chassiss artifact reject <submission-id> --reason <text>
```

`check` 验证格式、路径、ID、引用、依赖、allowed paths、结构化验收命令和冻结规则，但不宣称内容语义正确。验收项使用 `argv`、项目内相对 `cwd`、显式 `env` 和 `timeout_seconds`；默认不调用 shell，`shell: true` 必须写入由 Master 接受的 Task 契约。

`submit` 固定精确内容摘要。Master 的 `accept` 只接受该摘要；内容变化必须重新提交。

Designer 的 `next` 在 artifact 被拒绝时优先返回对应 `artifact.submit <path>`，JSON `result.rejections` 同时给出 artifact ID、路径和拒绝理由。Designer 应修订原文档并重新提交，不制造新状态。

## Mission 和 Task

```text
chassiss mission list
chassiss mission context <mission-id>
chassiss mission activate <mission-id>
chassiss mission block <mission-id> --reason <text>
chassiss mission resume <mission-id>
chassiss mission submit-acceptance <mission-id> --evidence <file-or-text>
chassiss mission accept <mission-id>

chassiss task list [--ready|--active|--blocked|--review]
chassiss task context <task-id>
chassiss task claim <task-id>
chassiss task assign <task-id> --owner <actor>
chassiss task block <task-id> --reason <text>
chassiss task resume <task-id>
chassiss task release <task-id>
chassiss task cancel <task-id> --reason <text>
chassiss task supersede <task-id> --replacement <new-task-id>
```

`activate` 检查 Requirements、Architecture、Mission 和全部 Task 都已被正确接受，Task 图无环且写入范围可调度。

`claim/assign` 原子检查 Task 状态、依赖、WIP、路径冲突和 baseline；owner 必须在当前 trust 中拥有未回收的 Developer grant，事件会记录该 grant。后续 credential 轮换只要 actor 不变即可继续。

`task block` 释放该 Task 的 WIP 和路径调度占用，但保留冻结状态。`task resume` 会重新检查依赖、WIP、路径冲突、owner/branch/baseline、worktree，以及 review_pending/changes_requested/approved 的 submission 与 Review 证据；任何一项过期都拒绝恢复旧状态。

`task release` 仅供 Orchestrator 释放没有任何 submission 的 claimed/in-progress Task；worktree 必须干净、Task branch 必须仍等于 baseline。CLI 通过 operation journal 删除 linked worktree 和精确匹配的 Task branch，再把 Task 恢复为 ready，不丢弃工作。`task cancel` 只能由 Master 执行，保留现有 branch/worktree 作为取证并记录理由。`task supersede` 要求 replacement 是同 Mission 下已由 Master 接受、尚未加入执行图的新 Task；旧 Task 永久标记 superseded，新 Task 使用新 ID 和冻结契约，依赖通过替换链解析。

## Developer 工作

```text
chassiss work open <task-id>
chassiss work context <task-id>
chassiss work status <task-id>
chassiss work diff <task-id>
chassiss work check <task-id> [--all|--id CHECK-001]
chassiss work checkpoint <task-id> --file <checkpoint.yaml>
chassiss work submit <task-id> --file <handoff.yaml> [--message <text>]
chassiss work block <task-id> --reason <text>
```

`work context` 是 Developer 的完整任务包；正常执行不要求再读取整个状态或其他 Task。

`work open` 在 `.chassis/worktrees/<task-id>/` 创建或恢复该 Task 的独立 linked worktree，不切换项目根目录分支。后续 `status/diff/check/checkpoint/submit` 都验证 Task、路径、branch、Git worktree 身份和绑定摘要。

`work check` 按原样执行 Task 声明的结构化 argv，使用隔离后的基础环境与显式 env，并保存 CheckSpec 摘要、退出码、结果和当前 Git tree/index 摘要。检查后修改内容、symlink 目标或 executable bit 都会使结果失效。

`work submit` 检查改动范围、baseline、依赖、必需 checks、检查快照、handoff 和冻结预算，成功后产生不可变 submission。预算证据记录变更文件数、文本增删行、提交数和二进制文件数，并在 review 时从 Git 精确范围重新计算。

`--message` 是可选单行摘要；CLI 会统一生成 `<task-id>: <摘要>`。未传时使用 handoff 的第一个非空行，再无内容才使用通用默认值。最终 Git subject、submission manifest 和 Reviewer 检查必须一致。

## Reviewer、集成和发布

```text
chassiss review list
chassiss review context <submission-id>
chassiss review check <submission-id>
chassiss review approve <submission-id> --report <file>
chassiss review request-changes <submission-id> --report <file>

chassiss integrate check <submission-id>
chassiss integrate apply <submission-id>

chassiss publish check --target <github|gitlab|remote-git>
                       [--remote origin] [--branch <default-branch>]
chassiss publish apply --target <github|gitlab|remote-git>
                       [--remote origin] [--branch <default-branch>]
```

`review check` 做机器检查；Reviewer 仍须进行语义复核。批准绑定 submission 摘要，任何内容变化都会使批准失效。

`integrate apply` 只接受仍有效的 approved submission，要求 Task branch tip 仍等于获批 `HeadCommit`，在临时候选 worktree 合并精确 SHA 并重跑 checks；全部通过后才推进本地正式 baseline 和记录 integration。

`publish check` 是只读预检；`publish apply` 需要 Master 或 Orchestrator credential。adapter 只把本地正式分支对应的精确 CLI baseline SHA fast-forward push 到指定远端分支，禁止隐式 merge、reset 和 force。目标名只选择适配器策略，不会把 GitHub/GitLab 的 Issue、PR 或 Review 当作状态源。

`publication.applied` 与 `integration.applied` 分开记录，并绑定远端名称、URL 摘要、分支和 SHA；CLI 不回显可能含凭据的原始远端 URL，也拒绝 Git external-helper URL。远端发布失败不会伪造成功，也不会损坏或撤销本地 integration。push 已成功但本地事件提交失败时，后续写操作会要求先运行 `recover`；只有远端 endpoint 和 SHA 与预写 journal 精确一致时才补记 publication。

## 身份和权限

```text
chassiss auth master-init --output <root-file-or-existing-directory>
chassiss --root <project> --credential <master-root> auth issue
                    --actor <actor> --role <role> [--actions <list>]
                    [--not-before <rfc3339>]
                    [--expires-at <rfc3339> | --ttl-seconds <n>]
                    [--projects <ids>] [--missions <ids>] [--tasks <ids>]
                    [--submissions <ids>] [--submission-digests <digests>]
                    [--heads <shas>] [--baselines <shas>]
                    --output <credential-file>
chassiss auth inspect <credential-source>
chassiss --credential <master-root> auth revoke <credential-id> [--reason <text>]
```

`master-init` 只需由 Master 执行一次。第一版 credential 默认长期有效，由 Master 主动 `revoke`；不要求每个 Task 或 Session 重新签发。项目只保存 Master 签名的公钥授权和回收记录，不保存 Root 私钥或 credential 正文。

credential 必须按 Agent 身份签发，不能由多个 Agent 共享一个 Role credential。Developer 的实际 Task 范围、Reviewer 独立性和其他动态限制仍由状态机检查。

签发与回收使用独立 `trust_revision`、项目授权 journal 和项目写锁。自动化调用可传全局 `--expect-trust-revision <n>`；冲突时刷新 `status` 后重试。签发不会覆盖已有 credential 文件，trust 提交失败也不会发布看似有效的最终 credential。

所有 validity/resource 参数都是可选的，未传时仍是 Master 选定的长效、项目级 credential。资源列表使用逗号分隔并按精确 ID/SHA 匹配：Developer grant 可绑定 Task；Reviewer 可绑定 submission 及 digest；Integration 还可绑定获批 head 和正式 baseline。事件签名重放会再次验证事件时间与这些 scope，不能只靠当前 CLI 前置检查。

未来可以增加 `rotate`、可选 `--ttl`、`--resource` 和 broker，不改变现有角色工作流。

## v0.1 已实现范围

```text
project init
status
next
doctor
verify/recover/explain
auth master-init/issue/inspect/revoke
template list/get
artifact check
artifact submit
artifact accept/reject
mission list/context/activate/block/resume/submit-acceptance/accept
task list/context/claim/assign/block/resume/release/cancel/supersede
work open/check/checkpoint/submit
work context/status/diff/block
review list/context/check/approve/request-changes
integrate check/apply
publish check/apply
```

credential rotate 命令和可证明无副作用的 transactional dry-run 留待后续版本。
