# CHASSISS v0.3

> API V2、唯一 Owner credential、可审计基线接管、便捷 credential 传输、独立验收源、完整 review feedback/audit、提交前 preflight 与本地/远端完整性边界已实现。

CHASSISS 是一个以 CLI 为核心的软件开发工作流。Agent 负责需求、架构、实现和复核中的语义判断；CLI 负责模板、权限、状态、任务分派、范围检查、并发和恢复。

GitHub 不是核心依赖。v0.3 使用本地 Git 保存 baseline、diff 和正式集成历史，但 Agent 不需要直接操作 Git；GitHub、GitLab 或其他 Git 远端只通过 `publish` adapter 同步精确的正式 baseline。

## 仓库结构

```text
chassiss/
├── README.md
├── src/
│   ├── go.mod
│   ├── cmd/chassiss/       # CLI 入口
│   └── internal/app/       # 全部实现、模板和测试
├── cli/
│   ├── darwin-amd64/chassiss
│   ├── darwin-arm64/chassiss
│   ├── linux-amd64/chassiss
│   ├── linux-arm64/chassiss
│   └── SHA256SUMS
└── skills/chassiss/
    ├── SKILL.md
    ├── agents/openai.yaml
    └── bin/
        ├── darwin-amd64/chassiss
        ├── darwin-arm64/chassiss
        ├── linux-amd64/chassiss
        ├── linux-arm64/chassiss
        └── SHA256SUMS
```

`src/` 是唯一源码根目录。`cli/` 保存当前安全支持平台的可执行文件；Skill 自带相同的平台版本，复制或安装 Skill 后不依赖系统 `PATH` 中的同名程序。

当前支持：

- macOS：Intel `amd64`、Apple Silicon `arm64`；
- Linux：`amd64`、`arm64`；
- Windows：暂不支持，因为当前 Windows 构建会明确拒绝项目 advisory lock，不能安全执行写操作。

从源码验证或重建：

```text
cd src
go test ./...
go test -race ./...
go vet ./...
go build ./...
```

发布二进制使用 `CGO_ENABLED=0`、`-trimpath`、`-buildvcs=false` 和 `-ldflags "-s -w"`；产物不嵌入构建工作区的脏状态，并应与对应目录中的 `SHA256SUMS` 一致。

## 核心规则

1. Agent 不直接编辑 `.chassis/`。
2. Agent 启动时用自己的 credential 调用 `bootstrap`，身份和角色不由 Agent 自报。
3. Agent 不手工改变任务状态；写权限按动作和资源授予，不按 YAML 字段或声明式文档授予。
4. Designer 提交设计，Master 接受设计；作者不能自批。
5. Orchestrator 分派任务，但不能批准实现。
6. Developer 只修改任务允许的路径。
7. Reviewer 独立复核精确 submission，批准后才允许集成。
8. GitHub 只用于可选发布，不决定核心工作流状态。
9. CLI 拒绝时停止，根据错误返回的下一动作处理，不猜测修复。
10. 每个 acceptance check 必须声明与 Developer `allowed_paths` 不重叠的 `verification_paths`。
11. Master 的日常维护修改必须使用独立 Owner credential 通过 `owner apply` 接管，不能直接移动正式分支或编辑状态。

## 被治理项目结构

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

角色规范的机器事实源是可信 CLI 内的 Role Policy V3 registry，而不是 Skill Markdown。registry 同时驱动默认 credential actions、命令 option 校验、写命令识别和 `bootstrap` 输出，并以 policy digest 固定语义；reducer 和具体命令仍独立重验领域前置条件。这样角色边界主要由“credential 未授权、资源 scope 不匹配、当前 State 不允许时不返回或拒绝”表达，不维护另一套冗长规则。

Agent 使用 `chassiss --json --credential <credential> bootstrap` 获取自身 actor/role、实际 grant、资源 scope、policy version/digest、可用命令 schema、当前候选动作和按需 context argv。role 完全从已验证 credential 推导，`available_actions` 绑定返回时的 state/trust revision，只是当前投影而不是可复用授权票据；每条命令执行时继续完整复验。

Event V4 只携带当前动作的严格 payload。未知字段、未知事件和非法领域转换全部拒绝；CLI 通过确定性 reducer 重放事件，再用 State/transition validator 检查不变量。API V2 使用 Config/State/Event V4，旧项目明确拒绝，不提供迁移或兼容模式。

Event V4、Trust V1、CheckSpec 和 submission digest 使用当前 Go JSON 编码的精确字节协议，并由 golden vectors 和“签名结构禁止浮点字段”测试冻结。它不是 RFC 8785/JCS；未来若采用 JCS，必须再次提升协议版本并明确拒绝旧项目。

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

Reviewer 批准和集成都绑定 `submission.HeadCommit`。`review check` 只报告 mechanical validation 与 declared checks，不代表语义合格；Reviewer 必须另行给出 approve/request-changes verdict。集成在临时候选 worktree 合并精确 SHA、重跑 Task checks 并保存 merged-tree 证据，checks 通过后才推进正式分支并记录 `integration.applied`。

`publish` 与 local integration 是两个独立事实。adapter 只把 CLI 当前 baseline 的精确 SHA fast-forward push 到指定远端分支，不创建或读取 GitHub/GitLab 的 Issue、PR、Review 或工作流状态，也不允许 force push。publication 绑定远端名称、远端 URL 摘要、分支和 SHA，但不回显可能含凭据的 URL。远端失败不会撤销本地 integration；远端已更新而本地事件未提交时，由独立 publish journal 在 `recover` 中验证精确 endpoint 和 SHA 后补记 `publication.applied`。远端出现 journal 之外的结果时进入 integrity blocked，由 Master 人工判断。

每个 Active Task 固定使用 `.chassis/worktrees/<task-id>/` 下的独立 linked worktree。状态事件同时绑定路径、Git worktree 身份、Task branch 和绑定摘要；`status/diff/check/checkpoint/submit` 都重新验证该绑定。打开 Task 不切换项目根 worktree，两个无路径冲突的 Task 可在 WIP 限制内并行执行。Task 集成后，仅当 worktree 干净且仍位于获批提交时才受控清理，Task branch 保留。

Task check 使用结构化 `argv/cwd/env/timeout_seconds/verification_paths`。每个 check 的独立验证源必须位于 Developer `allowed_paths` 之外；Developer check、Reviewer check 和 Integration 都对照冻结 Task baseline 重算验证源摘要。`work check` 还会在记录结果前执行 scope、budget 与候选 submission preflight，越界快照不会得到 passed evidence 或推进 revision。默认不经过 shell，不继承任意宿主环境，`cwd` 必须经符号链接解析后仍位于 Task worktree；确需 shell 时必须在冻结 Task 契约中显式 `shell: true` 并由 Master 接受。检查结果绑定 CheckSpec、verification source 和由临时 Git index 生成的 tree/stage 摘要。

Reviewer 的每次 verdict 都保留完整 report 并绑定 submission digest。Developer 的 `work context <task-id>` 返回当前 `change_request` 与按时间排序的 `change_request_history`；`review history [--task ID] [--submission ID]` 在 Mission 完成后仍可读取审计记录。`work checkpoint` 在 `in_progress` 时作为 `optional: true` action 投递。

`trust.yaml` 不是秘密，只保存由 Master Root 签名的角色公钥、授权版本和回收记录。单独修改它会使签名失效。写命令以及带 `--credential` 的 `doctor/verify` 还会用 Master 分发的 Root/角色 credential 所携带的 Root fingerprint 锚定项目；不带 credential 的读检查只能证明项目内部自洽。私钥和角色 credential 不进入项目仓库。

授权使用独立 monotonic `trust.revision` 和同一项目写锁。`auth issue` 先在最终输出目录准备隐藏 credential 临时文件，再原子提交签名 trust，最后发布 credential；`auth revoke` 也由授权 journal 保护。崩溃后 `recover` 只补全与 journal 精确匹配的结果。并发授权更新由 `--expect-trust-revision` 做 CAS；旧 revision 稳定返回 `CHS-CONFLICT-TRUST-REVISION`。

Master Root 私钥不硬编码在二进制中：二进制需要分发给所有角色，内嵌秘密可以被提取，泄漏后还必须重新发布整个 CLI。Root 由 Master 独立保管，二进制只内置算法和规则；每个角色 credential 自带项目和 Root fingerprint，并拥有自己的私钥。

## Credential 签发与文本传输

默认本地目录是 `~/.chassiss/`。`auth master-init` 未传 `--output` 时生成 `~/.chassiss/master-root.yaml`；项目内执行 `auth issue` 时，CLI 会按项目 Root fingerprint 发现唯一匹配 Root，并默认生成 `~/.chassiss/cred-<actor>.yaml`。显式路径参数仍可覆盖默认值，已有文件永不覆盖。

Master：

```text
chassiss auth master-init
chassiss auth issue --actor codex --role developer
chassiss auth export ~/.chassiss/cred-codex.yaml
```

`auth export` 只向 stdout 写三行 CHASSISS armor。body 是单行 Base64，解码后包含 envelope version、完整原字节 credential YAML 和 SHA-256。Master Root 明确禁止 export。

Agent：

```text
chassiss auth import --output ~/.chassiss/my-cred.yaml
# 粘贴 armor，Ctrl+D
chassiss --credential ~/.chassiss/my-cred.yaml bootstrap
```

Import 严格校验 armor、Base64、envelope version、摘要、Credential schema、role actions 和私钥长度，以 `0600` 原子写入且拒绝覆盖。最终真实性仍由 bootstrap 对照 Root 签名 trust、项目 ID、grant metadata 和公私钥匹配验证。

使用 `--json` 时，认证失败还会在 `error.diagnostic_category` 返回稳定子原因，例如 `grant_not_found`、`revoked`、`metadata_mismatch`、`policy_mismatch`、`key_invalid`、`key_mismatch` 或 `signature_invalid`；调用方不需要解析人类错误文本。

credential 默认绑定项目、Agent 身份、角色和允许动作，但不绑定单个 Task。Task 权限由当前状态继续收窄：Developer 只能操作分配给自己身份的 Task；Reviewer 不能复核同一身份产生的 submission；Designer 不能接受自己的 artifact；Orchestrator 不能批准实现。

Task assign/claim 只接受当前 trust 中未回收且拥有 Developer 权限的 actor，并在事件中记录当时的 `owner_grant_id`。该字段是分派来源证据，不锁死旧密钥；旧 grant 回收后，同 actor 的新 Developer credential 仍可继续原 Task。

应当为不同 Agent 身份分别签发 credential，不要让全部 Developer 或 Reviewer 共用同一把角色密钥，否则无法独立回收、审计具体主体或证明 Reviewer 独立性。同一 Agent 可以持有多个角色 credential，但每次动作必须明确选择当前角色。

Master Root 私钥和 Agent credential 保存在项目目录之外。Armor 仍然包含私钥，应按 secret 对待；聊天记录、剪贴板和终端滚屏都会扩大暴露面。项目内的 `trust.yaml` 只保存公钥、授权和回收事实。

v0.3 没有独立 `rotate` 命令。普通角色需要轮换时先签发新 credential，确认可用后再回收旧 credential；Owner 是唯一例外，必须先显式回收当前 Owner，再签发替代者。

## Owner 基线接管

Owner 是 Master Root 签发的独立高权限角色，不是 Master Root 本身，也不是拥有额外 scope 的 Developer。Master Root 只能签发、回收 Owner 和读取 Owner 历史，不能直接执行 `owner apply`；这保证日常绕过流程的行为使用可单独回收的密钥签名。

每个项目的 Root 签名 trust 同时最多存在一个未回收 Owner grant。过期不会自动释放这个唯一席位，轮换必须显式 `auth revoke`，随后才能签发替代 Owner；并发签发也只能有一个成功。Owner credential 仍然绑定项目和 Root fingerprint。

签发与使用：

```text
chassiss auth issue --actor master-owner --role owner

# Master 在默认分支的工作区中修改普通项目文件
chassiss --credential ~/.chassiss/cred-master-owner.yaml owner apply \
  --reason "更新项目维护配置"

chassiss --credential ~/.chassiss/cred-master-owner.yaml owner history
```

`owner apply` 只在项目静止期可用：不能有 Active Mission、Active Task 或待审 artifact；当前分支必须是配置的默认分支，HEAD 必须仍等于签名 State 中的 formal baseline。命令只接收未提交的 working-tree 变更，由 CLI 内部制作为恰好一个提交并推进 baseline；它不会收编 Master 预先手工创建的提交。

Owner 不能修改 `.chassis/`、`.git/` 或任何已登记的 Requirements、Architecture、Mission、Task artifact 文件。每次成功接管都会产生签名 `owner.baseline_applied` 事件，并在 State 的 `owner_changes`/`last_owner_change_id` 中保留 Owner actor、credential ID、原因、前后 SHA、tree digest、文件清单、commit message、变更统计和时间。`owner history` 对当前 Owner 与 Master Root 可读。跨 Git ref、index、事件和 State 的崩溃恢复继续由 operation journal 完成。

### 已知安全风险

长效 credential 是当前有意接受的取舍：

- credential 一旦泄漏，在 Master 回收前可以持续使用；
- credential 不绑定 Task，失陷后的影响范围大于短期、任务级授权；
- 多个 Session 重复使用同一私钥会增加暴露时间；
- 如果多个 Agent 运行在同一系统用户下并能读取彼此凭据，CLI 无法提供真正的秘密隔离；
- 离线或回滚到旧 `trust.yaml` 的执行环境可能暂时看不到最新回收记录，因此正式集成必须验证最新授权版本；
- Agent 若能替换 CLI 二进制，可以伪造本地显示结果；正式集成和发布必须由 Master 分发的可信 CLI 重新验证签名、回收状态和事件链。

当前主要目标是防止守规 Agent 因遗忘规则而误操作，不声称可以抵御拥有同一用户全部文件权限的恶意 Agent。当前支持可选 `not_before/expires_at`、Task/Mission/submission 白名单、独立 verification source，以及 Reviewer/Integration 的 digest/head/baseline 绑定；默认仍按 Master 的选择保持长效且不绑定 Task。后续安全版本还可增加每 Session 临时密钥、操作系统钥匙串、独立 credential broker 和 proof-of-possession。

## 最小生命周期

```text
project init
→ 每个 Agent 用自己的 credential bootstrap
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

CLI 是权限、规则和状态的执行面。仓库只保留一个标准 Skill：[skills/chassiss/SKILL.md](skills/chassiss/SKILL.md)，内容限于：

- 如何用 credential 执行 `bootstrap`；
- 如何读取 `capabilities`、`available_actions` 和 `context_requests`；
- mutation 使用返回的 revision 做 CAS，变化后重新 bootstrap；
- 不绕过 CLI、不编辑 `.chassis/`、不泄露 credential。

四份静态 Role Skill 已删除。Agent 不加载其他角色说明，也不靠一大串声明式规则约束自己；它只看到 credential 实际授予的能力和当前 State 允许的候选动作。命令 schema 由 `bootstrap` 返回，人类 README 不参与授权判断。

## v0.3 实现状态

已完成 API V2、Event V4 reducer、Role Policy V3、唯一 Owner grant、可审计 Owner baseline 接管、credential armor、稳定 credential diagnostics、review feedback/history、Developer scope/budget preflight、独立 verification sources、可选 checkpoint action、完整 state validator、revision CAS、advisory 项目锁、授权/Git/publish operation journal、精确提交集成、独立 Task worktree、本地 Git 闭环、可选远端 publish adapter 和单一通用 Skill。Greenfield、Brownfield、Owner 接管/恢复、退回重提、越界 check、独立验证源与完整角色 CLI 生命周期均有自动化覆盖。当前不实现 credential rotate 命令和完整 Mission 级设计变更流程。

旧 CHASSIS 没有迁移，只用于提取状态机规则和失败案例，不作为事实源。

## Master 复核重点

1. 是否接受 `.chassis/` 保存状态，所有项目文档统一在 `docs/`；
2. 是否接受 v0.3 本地 Git 必需、GitHub 完全可选；
3. 是否接受“Master 接受设计、Reviewer 接受实现”的独立性；
4. 是否接受 v0.3 使用按 Agent 身份签发、持续到主动回收的长效角色 credential；
5. 是否接受 Role Policy registry + credential-derived bootstrap 取代静态角色文档；
6. 是否接受源码、分发二进制和 Agent Skill 完全分层的仓库结构。
