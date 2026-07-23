# CHASSISS

CHASSISS 是一个以 CLI 为核心的多 Agent 软件开发工作流。

Agent 负责需求、架构、实现和复核中的语义判断；CLI 负责模板、权限、状态、任务分派、范围检查、并发、审计和恢复。人类 Master 只需保管根密钥、接受关键设计，并在必要时以 Owner 身份接管项目；各 Agent 使用由根密钥签发的角色 credential 工作。

CHASSISS 不绑定 GitHub、GitLab 或某一种托管平台，也不要求 Agent 直接操作 Git；它只要求所有参与者能够同步到同一个正式版本。当前 v0.3 使用本地 Git 作为 baseline、worktree、diff 和集成 backend。

它的目标不是把 Agent 关进恶意代码沙箱，而是让一群守规但可能犯错的 Agent 在同一套可恢复、可审计的流程里协作。

## Quickstart

推荐的最小执行单元是两个常驻 Agent：Build Agent 在同一 Session 中兼任 Orchestrator 和 Developer，Review Agent 独立担任 Reviewer。Designer 始终使用独立 Session，在规划阶段出现，设计需要变化时再召回。

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

运行方式很简单：

1. 把 [`skills/chassiss/`](skills/chassiss/) 安装到每个 Agent，并使用 Skill 内与系统匹配的 CLI。
2. Master 初始化新项目或接管已有项目，与独立 Designer Session 完成 Requirements、Architecture、Mission 和 Tasks。
3. 启动 Build Agent 与 Review Agent；Agent 每次被唤醒时先执行 `bootstrap`，然后只按 CLI 返回的当前动作工作。
4. 重复“分派、实现、复核、集成”；预算或契约需要变化时回到 Designer，全部 Task 完成后由 Master 接受 Mission。

完整安装、角色配置和逐条命令将维护在 [`docs/`](docs/)。

> 如果你胆子大而且完全相信你的前沿 Agent 的能力（例如 GPT-5.6 Sol、Claude Fable、Kimi K3、Qwen 3.8 Max），可以试试让一个大 Agent 代行人类 Master，自动签发并调度子 Agent，完成整个项目。但此时 Master Root 和子 Agent credential 通常处于同一信任域，秘密隔离默认不生效；CHASSISS 只能帮助统一开发流程，不保证项目质量。这不是设计目标用法，但可以尝试。
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

有三条不可绕过的边界：

- 不要手工修改或删除 `.chassis/`；只有其中的 `cache/` 可以安全重建。
- 不要手工改动 CHASSISS 创建的 Git refs、branches、linked worktrees，以及已进入受控状态的 Requirements、Architecture、Mission 和 Task。
- 普通项目文件只能在 CLI 创建的 Task worktree 中修改，或由人类通过 Owner 流程接管。

## 人类独立开发

人类需要跳过 Agent 流程时，以 Owner 身份修改普通项目文件，不要自行 commit，然后运行 `owner apply --reason "本次维护原因"`。CLI 会检查项目状态、创建正式提交并留下签名审计记录。

Owner 不能修改 `.chassis/`、Git 控制数据或已登记的项目文档。完整使用方法与限制将在详细文档中展开。

## 详细文档

后续文章的写作目录见 [`docs/menu.md`](docs/menu.md)。Agent 的实际身份、权限、上下文和下一动作始终以可信 CLI 的 `bootstrap` 输出为准。
