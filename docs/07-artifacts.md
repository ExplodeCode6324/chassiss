# Requirements、Architecture、Mission 与 Task

CHASSISS 使用四类 Markdown Artifact 固定项目意图与执行契约。

| Artifact | 默认路径 | 职责 |
| --- | --- | --- |
| Requirements | `docs/requirements.md` | 定义问题、行为和成功标准 |
| Architecture | `docs/architecture.md` | 定义边界、接口、状态、安全与验证 |
| Mission | `docs/missions/M###.md` | 定义一组 Task 的可验收结果 |
| Task | `docs/tasks/M###-T###.md` | 定义单个实施单元 |

CLI 内嵌模板与 validator 构成机器格式。Designer 通过 `template get` 获取当前版本。

## 获取模板

```text
chassiss template get requirements \
  --output docs/requirements.md

chassiss template get architecture \
  --output docs/architecture.md

chassiss template get mission \
  --id M001 \
  --output docs/missions/M001.md

chassiss template get task \
  --id M001-T001 \
  --output docs/tasks/M001-T001.md
```

这些命令需要 Designer credential。示例省略了全局 `--credential` 和 `--root`。

## 文档结构

每份 Artifact 包含简短 YAML front matter 和 Markdown 正文。front matter 保存 CLI 需要的机器字段。正文保存人类与 Agent 需要的语义内容。

Designer 应保留模板标题与必要章节，替换占位内容，并在提交前运行 `artifact check`。

## Requirements

front matter 固定为以下身份。

```yaml
kind: requirements
id: requirements
```

正文包含以下章节。

- Problem
- Required Behavior
- Success Criteria
- Scope
- Constraints
- Decisions Required from Master

Required Behavior 使用稳定 ID，例如 `REQ-001`。Success Criteria 使用稳定 ID，例如 `SC-001`。后续 Mission 可以引用这些 ID。

Requirements 接受后产生 digest。Architecture、Mission 和 Task 绑定该 digest。

## Architecture

Architecture front matter 绑定 Requirements。

```yaml
kind: architecture
id: architecture
requirements_digest: <accepted-requirements-digest>
```

正文覆盖以下内容。

- System Context
- Components and Boundaries
- Interfaces
- Data and State
- Security
- Validation Strategy
- Parallelization Boundaries
- Decisions Required from Master

边界需要足够具体，使 Designer 能够为 Task 指定不重叠的 `allowed_paths` 和独立验证源。

## Mission

Mission 表示一个可以由 Master 单独验收的 outcome。

```yaml
kind: mission
id: M001
requirements_digest: <accepted-requirements-digest>
architecture_digest: <accepted-architecture-digest>
task_ids:
  - M001-T001
```

正文覆盖以下内容。

- Outcome
- Requirements Covered
- Acceptance Criteria
- Constraints and Risks
- Completion Evidence

Mission 的 `task_ids` 建立初始 Task 图。所有列出的 Task 接受后，Orchestrator 才能激活 Mission。

## Task

Task 是一个 Agent Session 可以闭环的原子实施单元。

```yaml
kind: task
id: M001-T001
mission_id: M001
requirements_digest: <accepted-requirements-digest>
architecture_digest: <accepted-architecture-digest>
depends_on: []
allowed_paths:
  - src/component/**
budget:
  max_changed_files: 20
  max_diff_lines: 3000
  max_commits: 5
acceptance_checks:
  - id: CHECK-001
    argv:
      - go
      - test
      - ./...
    cwd: src
    env: {}
    timeout_seconds: 120
    verification_paths:
      - tests/**
```

正文覆盖 Objective、Inputs and Assumptions、Forbidden and Out of Scope、Deliverables、Stop Conditions 和 Reviewer Attention。

## 依赖与范围

`depends_on` 只能引用同一 Mission 中的 Task。依赖 Task integrated、cancelled 或由 replacement 关闭后，下游 Task 才能 ready。

`allowed_paths` 接受项目相对路径与受控 glob。CLI 对最终 submission 重新计算文件清单，任何越界文件都会阻止 check 与 submit。

`verification_paths` 必须与 Developer 的 `allowed_paths` 不重叠。这样 Developer 无法在同一 Task 中修改验收依据。

## 预算

Task 可以使用项目默认预算，也可以在 front matter 中提出单独预算。

| 值 | 含义 |
| --- | --- |
| `max_changed_files` | 最大变更文件数 |
| `max_diff_lines` | added 与 deleted 行数之和 |
| `max_commits` | baseline 到 submission 的提交数 |

某项为 0 时，该维度不设上限。预算接受后随 Task 冻结。

## 提交与接受

Designer 先检查并提交。

```text
chassiss artifact check docs/tasks/M001-T001.md
chassiss artifact submit docs/tasks/M001-T001.md
```

CLI 记录精确文件 digest 和 submission ID。Master 使用 context 读取相同内容。

```text
chassiss artifact context <artifact-submission-id>
chassiss artifact accept <artifact-submission-id>
```

接受动作把精确 Artifact 内容提交到 formal baseline，并记录 accepted commit。文件在 pending 期间发生变化时，digest 校验会阻止接受。

Master actor 必须与 `submitted_by` 不同。Designer actor 不应使用 `master`，否则 Master Root 无法接受或拒绝该提交。

Master 也可以拒绝。

```text
chassiss artifact reject <artifact-submission-id> \
  --reason <actionable-reason>
```

Designer 的下一次 `bootstrap` 会返回被拒 Artifact 的 context 请求与重新提交动作。

## 冻结与替换

已接受并进入执行的 Task 保持不可变。目标、依赖、范围、预算和 checks 不能原地扩大。

需要调整时执行以下流程。

1. Developer 或 Orchestrator 阻塞原 Task
2. Designer 创建新的 Task Artifact
3. Master 接受 replacement Task
4. Orchestrator 执行 `task supersede`

replacement 必须属于同一 Mission，保持 planned 状态，并在 accepted 后加入 Mission Task 图。

## 受控路径

Owner 和普通 Developer 都不能绕过 Artifact 流程修改已登记文档。Artifact 变更由 Designer 编写并由 Master 接受。

完整状态转换参见 [完整开发生命周期](08-lifecycle.md)。检查设计参见 [Checks 与独立验证源](10-checks-and-verification.md)。
