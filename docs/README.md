# CHASSISS v0.1

> CLI、四个角色 Skill、本地/远端边界及无人值守前的主要完整性修复已实现。

CHASSISS 是一个以 CLI 为核心的软件开发工作流。Agent 负责需求、架构、实现和复核中的语义判断；CLI 负责模板、权限、状态、任务分派、范围检查、并发和恢复。

GitHub 不是核心依赖。第一版使用本地 Git 保存 baseline、diff 和正式集成历史，但 Agent 不需要直接操作 Git；GitHub、GitLab 或其他 Git 远端只通过 `publish` adapter 同步精确的正式 baseline。

## 核心规则

1. Agent 不直接编辑 `.chassis/`。
2. Agent 不手工改变任务状态，只调用领域命令。
3. 写权限按动作授予，不按 YAML 字段授予。
4. Designer 提交设计，Master 接受设计；作者不能自批。
5. Orchestrator 分派任务，但不能批准实现。
6. Developer 只修改任务允许的路径。
7. Reviewer 独立复核精确 submission，批准后才允许集成。
8. GitHub 只用于可选发布，不决定核心工作流状态。
9. CLI 拒绝时停止，根据错误返回的下一动作处理，不猜测修复。

## 目标项目结构

```text
project-name/
├── .chassis/
│   ├── config.yaml       # 项目配置和策略
│   ├── trust.yaml        # Master 签名的角色授权和回收记录
│   ├── state.yaml        # 由事件重放生成的当前状态投影
│   ├── events/           # 原子、签名、最小 payload 的工作流事实源
│   ├── operations/       # 未完成跨介质操作的恢复 journal
│   ├── auth-operations/  # 未完成授权签发/回收的恢复 journal
│   ├── publish-operations/ # 未完成远端发布的恢复 journal
│   ├── submissions/      # 不可变 submission manifest
│   ├── worktrees/        # 每个 Active Task 的独立 Git worktree
│   ├── cache/            # 可删除、可重建，不提交
│   └── lock              # 本机写锁，不提交
├── docs/
│   ├── requirements.md
│   ├── architecture.md
│   ├── missions/
│   │   └── M001.md
│   └── tasks/
│       ├── M001-T001.md
│       └── M001-T002.md
└── <项目源码和普通文件>
```

所有需求、架构、任务书和原子任务都在 `docs/` 下。状态和运行数据集中放在根目录 `.chassis/`，不污染项目文档。

## 文档格式

CLI 内嵌 Requirements、Architecture、Mission、Task 四种模板。Agent 必须通过命令取得模板，不靠记忆重建格式：

```text
chassiss template get requirements
chassiss template get architecture
chassiss template get mission --id M001
chassiss template get task --id M001-T001
```

文档是 Markdown，开头有很短的 YAML front matter，保存 CLI 必须读取的 ID、baseline、依赖、允许路径和验收命令。具体字段以 CLI 内嵌模板和 validator 为唯一机器契约，不再单独维护一套长篇 Schema 文档。

模板职责：

- `requirements.md`：问题、目标行为、成功标准、范围、约束、待 Master 决策；
- `architecture.md`：边界、接口、数据、依赖、安全、验证和并行边界；
- `missions/M###.md`：一个可独立验收的 Outcome 及其 Task 集；
- `tasks/M###-T###.md`：一个 Agent 会话可以闭环的原子任务。

Task 进入 ready 后，目标、依赖、允许路径和验收冻结。需要改变时停止并进入后续设计变更流程，不原位改写契约。

新项目默认冻结每个 Task 的变更预算：最多 100 个文件、20,000 行增删和 20 个提交。Master 可在 `project init` 调整项目默认值，Designer 也可在 Task front matter 的 `budget` 中提出单 Task 值；该值随 Task artifact 被接受后冻结。某项为 `0` 表示该维度不设上限。二进制文件计入文件数，但因 Git 不提供文本行数而不计入增删行数。

## 状态与权限

`.chassis/events/` 是工作流唯一事实源；GitHub 只用于不同实例之间同步代码版本，不决定 Mission、Task、Review 或 Integration 状态。`.chassis/state.yaml` 是可重建投影，不是编辑接口。

Event V2 只携带当前动作的严格 payload。未知字段、未知事件和非法领域转换全部拒绝；CLI 通过确定性 reducer 重放事件，再用 State/transition validator 检查不变量。Config/Event V1 明确拒绝，不做隐式迁移。

Event V2、Trust V1、CheckSpec 和 submission digest 使用当前 Go JSON 编码的精确字节协议，并由 golden vectors 和“签名结构禁止浮点字段”测试冻结。它不是 RFC 8785/JCS；为避免让既有签名整体失效，本版本不会在原协议内替换序列化器。未来若采用 JCS，必须提升协议版本并提供显式迁移或拒绝策略。

所有状态写命令执行同一事务：

```text
获取项目写锁并验证 revision/credential
→ 写 prepared operation journal
→ 准备确定 Git SHA 和签名事件
→ 应用 Git 并标记 git_applied
→ 原子写事件与状态投影
→ 标记 state_committed 并清理 journal
```

所有正式 Git 副作用都在同一项目写锁和 operation journal 内。恢复只在 Git 精确匹配 journal 的 before/after 时补写状态；不匹配则进入 `CHS-INTEGRITY-BLOCKED`，不会猜测 reset 或 force。每次状态变化增加 revision；状态投影损坏后，CLI 使用 `.chassis/events/` 确定性重建。

项目写锁使用操作系统 advisory lock。`.chassis/lock` 是持久锁文件，PID 和获取时间只用于诊断；锁的所有权由内核和打开的文件描述符决定，不按文件年龄删除，因此长测试不会在五分钟后被另一个 Agent 抢锁，进程退出后锁会由内核释放。

Reviewer 批准和集成都绑定 `submission.HeadCommit`。集成在临时候选 worktree 合并精确 SHA、重跑 Task checks 并保存 merged-tree 证据，checks 通过后才推进正式分支并记录 `integration.applied`。

`publish` 与 local integration 是两个独立事实。adapter 只把 CLI 当前 baseline 的精确 SHA fast-forward push 到指定远端分支，不创建或读取 GitHub/GitLab 的 Issue、PR、Review 或工作流状态，也不允许 force push。publication 绑定远端名称、远端 URL 摘要、分支和 SHA，但不回显可能含凭据的 URL。远端失败不会撤销本地 integration；远端已更新而本地事件未提交时，由独立 publish journal 在 `recover` 中验证精确 endpoint 和 SHA 后补记 `publication.applied`。远端出现 journal 之外的结果时进入 integrity blocked，由 Master 人工判断。

每个 Active Task 固定使用 `.chassis/worktrees/<task-id>/` 下的独立 linked worktree。状态事件同时绑定路径、Git worktree 身份、Task branch 和绑定摘要；`status/diff/check/checkpoint/submit` 都重新验证该绑定。打开 Task 不切换项目根 worktree，两个无路径冲突的 Task 可在 WIP 限制内并行执行。Task 集成后，仅当 worktree 干净且仍位于获批提交时才受控清理，Task branch 保留。

Task check 使用结构化 `argv/cwd/env/timeout_seconds`。默认不经过 shell，不继承任意宿主环境，`cwd` 必须经符号链接解析后仍位于 Task worktree；确需 shell 时必须在冻结 Task 契约中显式 `shell: true` 并由 Master 接受。检查结果绑定 CheckSpec 摘要和由临时 Git index 生成的 tree/stage 摘要，因此覆盖 symlink 目标、executable bit、文件模式、rename/type change，且不会修改真实 index。

`trust.yaml` 不是秘密，只保存由 Master Root 签名的角色公钥、授权版本和回收记录。单独修改它会使签名失效。写命令以及带 `--credential` 的 `doctor/verify` 还会用 Master 分发的 Root/角色 credential 所携带的 Root fingerprint 锚定项目；不带 credential 的读检查只能证明项目内部自洽。私钥和角色 credential 不进入项目仓库。

授权使用独立 monotonic `trust.revision` 和同一项目写锁。`auth issue` 先在最终输出目录准备隐藏 credential 临时文件，再原子提交签名 trust，最后发布 credential；`auth revoke` 也由授权 journal 保护。崩溃后 `recover` 只补全与 journal 精确匹配的结果。并发授权更新由 `--expect-trust-revision` 做 CAS；旧 revision 稳定返回 `CHS-CONFLICT-TRUST-REVISION`。

Master Root 私钥不硬编码在二进制中：二进制需要分发给所有角色，内嵌秘密可以被提取，泄漏后还必须重新发布整个 CLI。v0.1 改为由 Master 独立保管 Root 私钥，二进制只内置算法和规则；每个角色 credential 自带项目和 Root fingerprint，并拥有自己的私钥。

## 第一版密钥使用方式

第一版以个人项目的便利性为优先，使用长效角色 credential，不要求 Master 为每个 Task 或每个 Session 重新签发。

预期流程：

```text
Master 首次运行 chassiss auth master-init
→ 生成一套长期 Master Root
→ 每个项目初始化时绑定该 Root
→ Master 为每个 Agent 身份和角色签发一次长效 credential
→ Agent 在后续 Session 中持续复用
→ 只有 Master 主动 revoke 后才失效
```

credential 默认绑定项目、Agent 身份、角色和允许动作，但不绑定单个 Task。Task 权限由当前状态继续收窄：Developer 只能操作分配给自己身份的 Task；Reviewer 不能复核同一身份产生的 submission；Designer 不能接受自己的 artifact；Orchestrator 不能批准实现。

Task assign/claim 只接受当前 trust 中未回收且拥有 Developer 权限的 actor，并在事件中记录当时的 `owner_grant_id`。该字段是分派来源证据，不锁死旧密钥；旧 grant 回收后，同 actor 的新 Developer credential 仍可继续原 Task。

应当为不同 Agent 身份分别签发 credential，不要让全部 Developer 或 Reviewer 共用同一把角色密钥，否则无法独立回收、审计具体主体或证明 Reviewer 独立性。同一 Agent 可以持有多个角色 credential，但每次动作必须明确选择当前角色。

Master Root 私钥和 Agent credential 保存在项目目录之外。第一版允许 Master 通过受限文件手工分发；推荐文件权限为仅所有者可读。项目内的 `trust.yaml` 只保存公钥、授权和回收事实。

v0.1 没有独立 `rotate` 命令；需要轮换时先签发新 credential，确认可用后再回收旧 credential。

### 已知安全风险

长效 credential 是有意接受的第一版取舍：

- credential 一旦泄漏，在 Master 回收前可以持续使用；
- credential 不绑定 Task，失陷后的影响范围大于短期、任务级授权；
- 多个 Session 重复使用同一私钥会增加暴露时间；
- 如果多个 Agent 运行在同一系统用户下并能读取彼此凭据，CLI 无法提供真正的秘密隔离；
- 离线或回滚到旧 `trust.yaml` 的执行环境可能暂时看不到最新回收记录，因此正式集成必须验证最新授权版本；
- Agent 若能替换 CLI 二进制，可以伪造本地显示结果；正式集成和发布必须由 Master 分发的可信 CLI 重新验证签名、回收状态和事件链。

第一版的主要目标是防止守规 Agent 因遗忘规则而误操作，不声称可以抵御拥有同一用户全部文件权限的恶意 Agent。当前已经支持可选 `not_before/expires_at`、Task/Mission/submission 白名单，以及 Reviewer/Integration 的 digest/head/baseline 绑定；默认仍按 Master 的选择保持长效且不绑定 Task。后续安全版本还可增加每 Session 临时密钥、操作系统钥匙串、独立 credential broker 和 proof-of-possession。

## 最小生命周期

```text
project init
→ Designer 获取模板、编写并提交 Requirements
→ Master 接受 Requirements
→ Designer 编写并提交 Architecture
→ Master 接受 Architecture
→ Designer 编写 Mission 和 Tasks
→ Master 接受计划
→ Orchestrator 激活 Mission、领取或分派 Task
→ Developer 实现、检查、checkpoint、submit
→ Reviewer 独立复核
→ Reviewer approve 或 request-changes
→ 已批准 submission 集成到本地正式 baseline
→ 可选 publish 到 GitHub/GitLab/其他仓库
→ Orchestrator 提交 Mission 验收
→ Master 关闭 Mission
```

## CLI 与 Skill 的关系

CLI 是规则和状态的执行面。未来 Skill 只保留：

- 启动运行 `chassiss status` 和 `chassiss next`；
- 只使用当前角色允许的命令；
- 只修改任务包给出的路径；
- 遇到设计变化、范围越界、baseline 过期或 CLI 拒绝时停止；
- 需要细节时运行 `chassiss explain`。

角色规则只维护在 `skills/*-skill.md`，避免重复契约漂移；命令见 [CLI.md](CLI.md)。

## v0.1 实现状态

已完成 Go CLI、严格目录、模板、artifact validator、Event V2 reducer、完整 state validator、revision CAS、advisory 项目锁、默认长期且可选 TTL/resource scope 的 credential、授权/Git/publish operation journal、Developer owner grant 校验、Task resume 完整复验、release/cancel/supersede 替换链、精确提交集成、独立 Task worktree、Git tree/index 摘要、结构化 checks、冻结 Task 预算、本地 Git 闭环、可选远端 publish adapter 和四个角色 Skill。Greenfield 与 Brownfield 的四角色 CLI 生命周期已固化为自动化测试。当前不实现 credential rotate 命令和完整 Mission 级设计变更流程。

旧 CHASSIS 没有迁移，只用于提取状态机规则和失败案例，不作为事实源。

## Master 复核重点

1. 是否接受 `.chassis/` 保存状态，所有项目文档统一在 `docs/`；
2. 是否接受第一版本地 Git 必需、GitHub 完全可选；
3. 是否接受“Master 接受设计、Reviewer 接受实现”的独立性；
4. 是否接受第一版使用按 Agent 身份签发、持续到主动回收的长效角色 credential；
5. 是否接受 [SIMULATION.md](SIMULATION.md) 中列出的剩余风险，再决定下一轮优先级。
