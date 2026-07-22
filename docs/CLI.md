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
chassiss status
chassiss next --role <role> [--actor <actor>]
chassiss doctor
chassiss verify
chassiss recover
chassiss explain <error-code>
```

`status` 返回当前 Mission、ready/active/blocked Task、待复核 submission 和 revision。

`next` 按 role、actor 和当前状态返回候选动作；真正执行时仍会重新验证 credential、revision 和全部前置条件。

`doctor/verify` 在传入 `--credential` 时还会用该长期凭证锚定项目 Root；不传凭证只证明项目内部自洽。`recover` 先处理 authorization 与 Git/state operation journal：仅当外部结果精确匹配 journal 时补写或发布，随后从签名事件重建状态投影。不一致时进入 integrity blocked，不隐式 reset、force 或改写 trust。

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
```

`activate` 检查 Requirements、Architecture、Mission 和全部 Task 都已被正确接受，Task 图无环且写入范围可调度。

`claim/assign` 原子检查 Task 状态、依赖、WIP、路径冲突和 baseline；owner 必须在当前 trust 中拥有未回收的 Developer grant，事件会记录该 grant。后续 credential 轮换只要 actor 不变即可继续。

`task block` 释放该 Task 的 WIP 和路径调度占用，但保留冻结状态。`task resume` 会重新检查依赖、WIP、路径冲突、owner/branch/baseline、worktree，以及 review_pending/changes_requested/approved 的 submission 与 Review 证据；任何一项过期都拒绝恢复旧状态。

## Developer 工作

```text
chassiss work open <task-id>
chassiss work context <task-id>
chassiss work status <task-id>
chassiss work diff <task-id>
chassiss work check <task-id> [--all|--id CHECK-001]
chassiss work checkpoint <task-id> --file <checkpoint.yaml>
chassiss work submit <task-id> --file <handoff.yaml>
chassiss work block <task-id> --reason <text>
```

`work context` 是 Developer 的完整任务包；正常执行不要求再读取整个状态或其他 Task。

`work open` 在 `.chassis/worktrees/<task-id>/` 创建或恢复该 Task 的独立 linked worktree，不切换项目根目录分支。后续 `status/diff/check/checkpoint/submit` 都验证 Task、路径、branch、Git worktree 身份和绑定摘要。

`work check` 按原样执行 Task 声明的结构化 argv，使用隔离后的基础环境与显式 env，并保存 CheckSpec 摘要、退出码、结果和当前 Git tree/index 摘要。检查后修改内容、symlink 目标或 executable bit 都会使结果失效。

`work submit` 检查改动范围、baseline、依赖、必需 checks、检查快照和 handoff，成功后产生不可变 submission。

## Reviewer、集成和发布

```text
chassiss review list
chassiss review context <submission-id>
chassiss review check <submission-id>
chassiss review approve <submission-id> --report <file>
chassiss review request-changes <submission-id> --report <file>

chassiss integrate check <submission-id>
chassiss integrate apply <submission-id>

# publish 命令留待 adapter 阶段，不属于 v0.1
```

`review check` 做机器检查；Reviewer 仍须进行语义复核。批准绑定 submission 摘要，任何内容变化都会使批准失效。

`integrate apply` 只接受仍有效的 approved submission，要求 Task branch tip 仍等于获批 `HeadCommit`，在临时候选 worktree 合并精确 SHA 并重跑 checks；全部通过后才推进本地正式 baseline 和记录 integration。

`publish` 与集成分开。远端发布失败不会伪造成功，也不会损坏本地工作流状态。

## 身份和权限

```text
chassiss auth master-init --output <root-file-or-existing-directory>
chassiss --root <project> --credential <master-root> auth issue
                    --actor <actor> --role <role> [--actions <list>]
                    --output <credential-file>
chassiss auth inspect <credential-source>
chassiss --credential <master-root> auth revoke <credential-id> [--reason <text>]
```

`master-init` 只需由 Master 执行一次。第一版 credential 默认长期有效，由 Master 主动 `revoke`；不要求每个 Task 或 Session 重新签发。项目只保存 Master 签名的公钥授权和回收记录，不保存 Root 私钥或 credential 正文。

credential 必须按 Agent 身份签发，不能由多个 Agent 共享一个 Role credential。Developer 的实际 Task 范围、Reviewer 独立性和其他动态限制仍由状态机检查。

签发与回收使用独立 `trust_revision`、项目授权 journal 和项目写锁。自动化调用可传全局 `--expect-trust-revision <n>`；冲突时刷新 `status` 后重试。签发不会覆盖已有 credential 文件，trust 提交失败也不会发布看似有效的最终 credential。

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
task list/context/claim/assign/block/resume
work open/check/checkpoint/submit
work context/status/diff/block
review list/context/check/approve/request-changes
integrate check/apply
```

`publish` adapter、credential rotation/TTL、Task supersede/release 和可证明无副作用的 transactional dry-run 留待后续版本。
