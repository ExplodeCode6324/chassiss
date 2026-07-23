# CHASSISS 是什么

CHASSISS 是一个以 CLI 为控制面的多 Agent 软件开发工作流。它为需求、设计、任务、实现、复核、集成和验收提供统一的状态与权限边界。

Agent 负责需要语义判断的工作。CLI 负责可以确定性执行的工作。所有正式变更都经过签名事件、状态校验和 Git 证据检查。

## 设计目标

CHASSISS 处理以下问题。

- 多个 Agent 对角色和当前任务理解不一致
- Agent 绕过任务范围修改项目文件
- 作者自行接受设计或实现
- 并行任务写入相同路径
- 检查结果与最终提交内容不一致
- Git 已经变化但工作流状态尚未提交
- 凭据被回收后仍被旧 Session 使用
- 进程中断后缺少可验证的恢复路径

工作流面向遵守约定但可能误判、遗忘或误操作的 Agent。它不会隔离同一系统用户下的恶意进程，也不会替代操作系统权限、代码沙箱和人工质量判断。

## 职责划分

| 参与者 | 主要职责 |
| --- | --- |
| Master | 保管 Master Root，签发角色 credential，接受设计与 Mission 验收 |
| Designer | 编写 Requirements、Architecture、Mission 和 Task |
| Orchestrator | 激活 Mission，分派 Task，处理执行阶段的调度 |
| Developer | 在 Task worktree 中实现、检查并提交 |
| Reviewer | 独立复核 submission，记录 verdict 并执行集成 |
| Owner | 在项目静止期接管人类独立维护产生的普通文件变更 |
| CLI | 验证身份、状态、范围、预算、证据、并发和恢复条件 |

Master Root 代表项目根信任。其余角色使用独立 credential。角色由 credential 和当前 trust 推导，Agent 无法通过命令参数声明新的身份。

## 控制面与内容面

CHASSISS 将项目内容与控制状态分开保存。

```text
project/
├── .chassis/        CLI 控制状态
├── docs/            受控项目文档
└── source files     普通项目内容
```

`.chassis/` 保存配置、trust、事件、状态投影、operation journal、submission 和 worktree 绑定。该目录由 CLI 独占管理。

`docs/` 保存 Requirements、Architecture、Mission 和 Task。文档由 Designer 编写，Master 接受后进入受控状态。

普通项目内容由 Developer 在 Task worktree 中修改。项目静止时，人类也可以通过 Owner 流程接管普通文件变更。

## 权威来源

同一信息可能出现在 Skill、README、项目文档和 CLI 输出中。发生差异时采用以下顺序。

1. 可信 CLI 内的协议、角色策略和 validator
2. 已签名的 trust 与事件链
3. 由事件链重建的 State
4. 已接受的项目 Artifact
5. Agent Skill
6. 面向人的说明文档

`bootstrap` 提供当前 credential 的身份、能力、上下文请求和候选动作。每条写命令仍会重新验证全部前置条件。

## Git 与版本同步

CHASSISS 的工作流不依赖 GitHub 或 GitLab 的 Issue、Pull Request 和 Review 状态。当前 v0.3 使用本地 Git 保存 baseline、Task branch、worktree、diff 和正式集成历史，因此运行时需要 Git。

`publish` 可以把精确的正式 baseline 推送到 GitHub、GitLab 或普通 Git 远端。当前实现只同步代码 baseline，不同步 `.chassis/`。多个独立 clone 还不能同时作为等价控制端。

## 项目阶段

项目状态包含三个阶段。

| 阶段 | 含义 |
| --- | --- |
| `design` | Requirements 和 Architecture 尚在建立，或项目等待新的规划 |
| `execution` | 一个 Mission 处于 active、blocked 或 acceptance_pending |
| `idle` | 当前没有执行中的 Mission，可以执行 Owner 维护或准备下一批规划 |

同一项目最多有一个活动 Mission。Task 可以在 WIP 限制与路径不重叠的条件下并行。

## 下一步

- 首次使用参见 [五分钟 Quickstart](02-quickstart.md)
- 角色配置参见 [推荐的 Agent 协作拓扑](03-agent-topology.md)
- 安全边界参见 [安全模型与已知风险](19-security-model.md)
- 完整状态设计参见 [控制状态、事件与状态投影](13-control-state.md)
