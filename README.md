# CHASSISS

中文 | [English](README.en.md)

CHASSISS 是一个以 CLI 为核心的多 Agent 软件开发工作流。

Agent 负责需求、架构、实现和复核中的语义判断；CLI 负责模板、权限、状态、任务分派、范围检查、并发、审计和恢复。人类 Master 只需保管根密钥、接受关键设计，并在必要时以 Owner 身份接管项目；各 Agent 使用由根密钥签发的角色 credential 工作。

CHASSISS 不绑定 GitHub、GitLab 或某一种托管平台，也不要求 Agent 直接操作 Git；它只要求所有参与者能够同步到同一个正式版本。当前 v0.3 使用本地 Git 作为 baseline、worktree、diff 和集成 backend。

CHASSISS 面向守规但可能犯错的 Agent。它提供可恢复、可审计的协作流程，不承担恶意代码沙箱的职责。

## Quickstart

推荐的最小执行单元包含两个常驻 Agent。Build Agent 在同一 Session 中兼任 Orchestrator 和 Developer。Review Agent 独立担任 Reviewer。Designer 使用独立 Session，在规划阶段出现，设计需要变化时再召回。

```text
+--------+   discuss   +----------------------+
| Master | <---------> | Designer             |
+---+----+             | isolated session     |
    | accepts plan     +----------+-----------+
    +-----------------------------+
                                  v
                       +------------------------------+
                       | Build Agent                  |
                       | Orchestrator + Developer     |
                       | assign -> implement          |
                       +---------------+--------------+
                                       | submission
                                       v
                       +---------------+--------------+
                       | Review Agent                 |
                       | Reviewer                     |
                       | review -> integrate          |
                       +---------------+--------------+
                                       |
              +------------------------+-------------------------+
              | next Task ---------------------> Build Agent     |
              | contract or budget change -----> Designer        |
              | all Tasks done ----------------> Master accepts  |
              +--------------------------------------------------+
```

### 1. 安装 Skill

把 [`skills/chassiss/`](skills/chassiss/) 安装到每个 Agent，并使用 Skill 内与系统匹配的 CLI。

### 2. 初始化项目并签发角色 credential

创建 Master Root，然后初始化项目。

```text
chassiss auth master-init
chassiss --credential ~/.chassiss/master-root.yaml \
  project init /path/to/project
```

接管已有项目时，在 `project init` 命令末尾添加 `--existing`。

进入项目目录，签发 Designer、Build、Reviewer 和 Owner credential。

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

### 3. 启动 Designer Session

导出 Designer credential。

```text
chassiss auth export ~/.chassiss/cred-designer-1.yaml
```

新建独立 Session，告知 Agent 它担任 Designer，并发送项目路径和命令输出的三行 Base64 armor。Designer 会导入 credential，执行 `bootstrap`，获取模板并推进 Requirements、Architecture、Mission 和 Tasks。Master 与 Designer 完成讨论并接受提交的规划文档。

### 4. 启动 Build Agent 和 Review Agent

导出 Build Agent 的两份 credential 和 Reviewer credential。

```text
chassiss auth export ~/.chassiss/cred-build-orchestrator.yaml
chassiss auth export ~/.chassiss/cred-build-developer.yaml
chassiss auth export ~/.chassiss/cred-reviewer-1.yaml
```

将两份 Build armor 发给 Build Agent，告知它兼任 Orchestrator 和 Developer。将 Reviewer armor 发给独立的 Review Agent。两个 Agent 导入 credential 并执行 `bootstrap`，随后按 CLI 返回的当前动作工作。

### 5. 重复流程

Orchestrator 分派任务，Developer 实现并提交，Reviewer 复核并集成。每个 Agent 在状态变化后重新执行 `bootstrap`。

出现以下情况时回到 Designer。

- 当前批次的步骤预算或 Task 预算已经耗尽
- 需求或架构发生变化
- 冻结的 Task 契约需要替换

新一批规划被接受后，Build Agent 和 Review Agent 继续工作。全部 Task 完成后，Orchestrator 提交 Mission 验收，Master 接受 Mission。

完整命令和角色配置维护在 [`docs/`](docs/)。

> 如果你胆子大而且完全相信你的前沿 Agent 的能力（例如 GPT-5.6 Sol、Claude Fable、Kimi K3、Qwen 3.8 Max），可以试试让一个大 Agent 代行人类 Master，自动签发并调度子 Agent，完成整个项目。但此时 Master Root 和子 Agent credential 通常处于同一信任域，秘密隔离默认不生效；CHASSISS 只能帮助统一开发流程，不保证项目质量。这种用法不在设计目标内，但可以尝试。
>
> 如果你真的这样跑了，无论成功还是翻车，都欢迎把过程和结果提交为 [Issue](https://github.com/ExplodeCode6324/chassiss/issues)。

## 受控项目结构

```text
project-name/
├── .chassis/             # CLI 管理的权限、状态、事件与恢复数据
├── docs/
│   ├── requirements.md
│   ├── architecture.md
│   ├── missions/
│   └── tasks/
└── <项目源码和普通文件>
```

以下三条边界不可绕过。

- 不要手工修改或删除 `.chassis/`；其中的临时 cache 也应由 CLI 按当前 operation 状态处理。
- 不要手工改动 CHASSISS 创建的 Git refs、branches、linked worktrees，以及已进入受控状态的 Requirements、Architecture、Mission 和 Task。
- 普通项目文件只能在 CLI 创建的 Task worktree 中修改，或由人类通过 Owner 流程接管。

## 人类独立开发

人类需要跳过 Agent 流程时，以 Owner 身份修改普通项目文件，不要自行 commit，然后运行 `owner apply --reason <reason>`。CLI 会检查项目状态、创建正式提交并留下签名审计记录。

Owner 不能修改 `.chassis/`、Git 控制数据或已登记的项目文档。完整使用方法与限制见 [人类 Owner 接管](docs/cn/16-人类所有者接管.md)。

## 详细文档

从 [文档首页](docs/README.md) 选择语言，或直接进入 [中文文档](docs/cn/README.md) 和 [中文目录](docs/cn/文档目录.md)。Agent 的实际身份、权限、上下文和下一动作始终以可信 CLI 的 `bootstrap` 输出为准。
