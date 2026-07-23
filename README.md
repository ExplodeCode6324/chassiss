# CHASSISS

CHASSISS 是一个以 CLI 为核心的软件开发工作流。

Agent 负责需求、架构、实现和复核中的语义判断；CLI 负责模板、权限、状态、任务分派、范围检查、并发、审计和恢复。人负责保管根密钥、接受关键设计，并在确有必要时以 Owner 身份接管项目。

CHASSISS 不要求 Agent 直接操作 Git，也不绑定 GitHub、GitLab 或某一种托管平台；它要求有可靠的版本同步能力，使不同机器上的 Agent 始终基于同一个正式版本工作。版本同步可以由 Git、其他版本控制系统或受控的文件/制品同步工具提供。

当前 v0.3 实现使用本地 Git 作为 baseline、worktree、diff 和集成 backend，因此运行当前版本仍需安装 Git。非 Git backend 是架构允许的扩展方向，目前尚未提供。

CHASSISS 的目标是约束守规 Agent、减少误操作并保留证据。它不是恶意代码沙箱，也不能在多个 Agent 共享同一系统用户和文件权限时提供真正的秘密隔离。

当前 v0.3 的 `publish` adapter 只同步正式代码 baseline，不同步 `.chassis/` 控制状态；多个独立项目副本还不能作为可互换的控制端。跨机器协作可以同步代码版本，但工作流写操作应指向同一个受控项目实例，直到后续提供控制状态同步方案。

当前安全支持的平台：

- macOS：Intel `amd64`、Apple Silicon `arm64`；
- Linux：`amd64`、`arm64`；
- Windows：暂不支持写操作；当前构建无法提供项目所需的 advisory lock。

## Quickstart

### 推荐使用方式

最小执行单元是两个常驻 Agent：

- Build Agent：在同一 Session 中持有 Orchestrator 和 Developer 两份 credential；
- Review Agent：作为独立 Agent，仅持有 Reviewer credential。

Designer 必须在独立 Session 中完成需求、架构和任务规划，不能与实施上下文混用；设计需要变化时再召回 Designer。Designer 可以由单独 Agent 承担，也可以由 Build Agent 在实施前另开独立 Session 承担。

也可以让一个能力足够强的 Agent 代行人类 Master，自动签发 credential、创建并调度子 Agent，管理整个项目。但此时 Master Root 和各角色 credential 通常处于同一信任域，秘密隔离默认不生效；CHASSISS 只负责统一开发流程，不保证项目质量。这种方式可以工作，但不是 CHASSISS 的设计目标。

### 1. 安装、获取根密钥并初始化项目

将 [`skills/chassiss/`](skills/chassiss/) 复制到每个 Agent 的 Skill 目录，并使用其中匹配系统的 `bin/<os>-<arch>/chassiss`。下文简称 `chassiss`。

下文所有项目命令默认在项目根目录运行；从其他目录运行时加上 `--root /path/to/project`。

```text
chassiss auth master-init
chassiss --credential ~/.chassiss/master-root.yaml project init /path/to/project
```

已有项目在第二条命令末尾加 `--existing`。Master Root 只由人类保管，不发送给任何 Agent，也不能通过 `auth export` 导出。

### 2. 用 Master Root 签发全部角色 credential

Build Agent 的两份 credential 使用同一 actor、不同输出文件：

```text
cd /path/to/project

chassiss --credential ~/.chassiss/master-root.yaml auth issue \
  --actor designer-1 --role designer \
  --output ~/.chassiss/cred-designer-1.yaml

chassiss --credential ~/.chassiss/master-root.yaml auth issue \
  --actor build-1 --role orchestrator \
  --output ~/.chassiss/cred-build-orchestrator.yaml

chassiss --credential ~/.chassiss/master-root.yaml auth issue \
  --actor build-1 --role developer \
  --output ~/.chassiss/cred-build-developer.yaml

chassiss --credential ~/.chassiss/master-root.yaml auth issue \
  --actor reviewer-1 --role reviewer \
  --output ~/.chassiss/cred-reviewer-1.yaml

chassiss --credential ~/.chassiss/master-root.yaml auth issue \
  --actor human-owner --role owner \
  --output ~/.chassiss/cred-human-owner.yaml
```

### 3. 以 Base64 armor 分发 credential

对每份角色 credential 分别导出：

```text
chassiss auth export ~/.chassiss/cred-designer-1.yaml
chassiss auth export ~/.chassiss/cred-build-orchestrator.yaml
chassiss auth export ~/.chassiss/cred-build-developer.yaml
chassiss auth export ~/.chassiss/cred-reviewer-1.yaml
chassiss auth export ~/.chassiss/cred-human-owner.yaml
```

每次导出都会在 stdout 生成三行 Base64 armor。通过秘密通道把 Designer 和 Reviewer credential 发给对应 Agent，把两份 Build credential 都发给 Build Agent；Owner credential 只发到人类控制的维护环境。Base64 不是加密，armor 仍然包含私钥。

接收方为每份 armor 分别执行：

```text
chassiss auth import --output ~/.chassiss/my-credential.yaml
# 粘贴三行 armor，然后按 Ctrl+D

chassiss --json --root /path/to/project \
  --credential ~/.chassiss/my-credential.yaml bootstrap
```

Build Agent 应使用不同输出文件导入自己的两份 credential。每个 Agent 启动、状态变化、冲突或 CLI 拒绝后都重新运行 `bootstrap`；实际能力和下一动作以它返回的 schema、context 和 `available_actions` 为准。

### 4. 与 Designer 完成规划

人类先与独立 Designer Session 讨论需求。Designer 从 CLI 获取模板，依次生成 Requirements、Architecture、Mission 和 Tasks：

```text
chassiss --credential <designer-credential> template get requirements \
  --output docs/requirements.md
chassiss --credential <designer-credential> artifact submit docs/requirements.md

chassiss --credential <designer-credential> template get architecture \
  --output docs/architecture.md
chassiss --credential <designer-credential> artifact submit docs/architecture.md

chassiss --credential <designer-credential> template get mission \
  --id M001 --output docs/missions/M001.md
chassiss --credential <designer-credential> \
  artifact submit docs/missions/M001.md

chassiss --credential <designer-credential> template get task \
  --id M001-T001 --output docs/tasks/M001-T001.md
chassiss --credential <designer-credential> \
  artifact submit docs/tasks/M001-T001.md
```

Master 在每次提交后检查精确内容并接受对应 submission：

```text
chassiss --credential ~/.chassiss/master-root.yaml \
  artifact accept <artifact-submission-id>
```

Requirements、Architecture、Mission 和各 Task 之间存在 digest 与状态依赖，应按 `bootstrap` 返回的动作逐项编写、提交和接受，不要一次性猜测全部字段。

### 5. 启动 Build/Review Agent 并监听正式版本

规划完成后启动 Build Agent 和 Review Agent。可以用 cron 或 Agent automation 每五分钟检查 Git 远端并唤醒 Agent：

```text
*/5 * * * * cd /path/to/project && git fetch --prune && /path/to/wake-build-agent
*/5 * * * * cd /path/to/project && git fetch --prune && /path/to/wake-review-agent
```

定时器只负责发现版本变化和唤醒 Agent；Agent 被唤醒后仍必须先执行 `bootstrap`。`git fetch` 不同步 `.chassis/`，也不能把多个 clone 变成等价控制端。

### 6. 循环开发直到项目完成

Orchestrator 激活 Mission 并把 Task 分派给 Developer actor：

```text
chassiss --credential <orchestrator-credential> mission activate M001
chassiss --credential <orchestrator-credential> \
  task assign M001-T001 --owner build-1
```

Developer 打开 CLI 创建的 Task worktree，在返回的路径中实现，然后检查并提交：

```text
chassiss --credential <developer-credential> work open M001-T001
chassiss --credential <developer-credential> work check M001-T001 --all
chassiss --credential <developer-credential> \
  work submit M001-T001 --file <handoff-file-or-text>
```

Designer 复核实现是否仍符合已接受的需求与架构；这项设计一致性复核不能代替 Reviewer 的正式 verdict。Reviewer 独立检查精确 submission，通过后集成：

```text
chassiss --credential <reviewer-credential> review context <submission-id>
chassiss --credential <reviewer-credential> review check <submission-id>
chassiss --credential <reviewer-credential> \
  review approve <submission-id> --report <review-report>
chassiss --credential <reviewer-credential> integrate apply <submission-id>
```

如果使用 Git 远端同步正式版本，Orchestrator 在集成后发布精确 baseline：

```text
chassiss --credential <orchestrator-credential> \
  publish check --target remote-git --remote origin --branch main
chassiss --credential <orchestrator-credential> \
  publish apply --target remote-git --remote origin --branch main
```

当当前批次的步骤预算或 Task 预算耗尽、或者冻结契约需要变化时，停止原任务并召回 Designer 规划下一批。已冻结 Task 不能原地改写；如需替换，应先由 Designer 提交新 Task、Master 接受，再由 Orchestrator 按 `bootstrap` 返回的动作执行 `task supersede`。重复“分派、开发、设计复核、Reviewer 复核和集成”，直到所有 Task 完成。

最后由 Orchestrator 提交 Mission 验收，Master 接受：

```text
chassiss --credential <orchestrator-credential> \
  mission submit-acceptance M001 --evidence <file-or-text>
chassiss --credential ~/.chassiss/master-root.yaml mission accept M001
```

## 受控项目结构

初始化后的项目遵循以下结构：

```text
project-name/
├── .chassis/
│   ├── config.yaml
│   ├── trust.yaml
│   ├── state.yaml
│   └── <events、operations、submissions、worktrees 等 CLI 数据>
├── docs/
│   ├── requirements.md
│   ├── architecture.md
│   ├── missions/
│   │   └── M001.md
│   └── tasks/
│       └── M001-T001.md
└── <项目源码和普通文件>
```

以下边界不可绕过，否则 CLI 将拒绝继续，或项目会失去可信状态：

- 不要手工修改或删除 `.chassis/` 中的任何文件；只有 `.chassis/cache/` 可删除并由 CLI 重建。
- 不要手工移动、删除或改写 CHASSISS 创建的 Git refs、branches 和 linked worktrees。
- Requirements、Architecture、Mission 和 Task 一旦进入受控状态，只能在对应状态与 CLI 流程允许时更新；不能绕过 digest、acceptance 或冻结规则直接改写或删除。
- 普通项目文件只能在 CLI 创建的 Task worktree 中修改，或由人类使用 Owner credential 接管；不要直接移动正式分支。

## 人类独立开发：Owner

人类需要越过 Agent 流程独立维护项目时，先在项目默认分支中修改普通项目文件，但不要自行 commit，然后执行：

```text
chassiss --root /path/to/project \
  --credential ~/.chassiss/cred-human-owner.yaml \
  owner apply --reason "说明本次独立维护的原因"
```

CLI 会检查项目是否处于可接管的静止状态、创建正式提交并留下签名审计记录。Owner 不能修改 `.chassis/`、Git 控制数据或已登记的 Requirements、Architecture、Mission、Task artifact。

```text
chassiss --root /path/to/project \
  --credential ~/.chassiss/cred-human-owner.yaml \
  owner history
```

## 详细文档

完整架构、命令、权限、协议和恢复机制将统一维护在 [`docs/`](docs/)；文章分类和具体内容将在后续确定。Agent 的机器契约仍以可信 CLI 的 `bootstrap`、内嵌模板和 validator 为准。
