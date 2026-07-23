# 完整开发生命周期

CHASSISS 将一次开发工作组织为 Artifact、Mission、Task、Submission 和 Integration。每个阶段都有明确的责任人和可验证的输入。Agent 应先运行 `bootstrap`，再依据 `available_actions` 选择命令。

## 生命周期总览

```text
Master 与 Designer 讨论需求
              |
              v
  Requirements 和 Architecture
              |
        Master 接受
              |
              v
       Mission 和 Tasks
              |
        Master 接受
              |
              v
      Orchestrator 激活 Mission
              |
       分派或领取 Task
              |
              v
 Developer open -> check -> checkpoint -> submit
              |
      Designer 核对设计契约
              |
              v
 Reviewer check -> approve / request-changes
              |
              v
   同一 Reviewer integrate apply
              |
    Mission submit-acceptance
              |
              v
          Master accept
```

设计一致性复核是团队工作约定。CLI 的正式审批仍由独立 Reviewer 完成。

## 1. 规划项目

Designer 根据与 Master 的讨论生成 Requirements 和 Architecture。两者被 Master 接受后，Designer 再提交 Mission 和 Task。

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/designer.cred \
  artifact submit requirements.md

chassiss --root /path/to/project \
  --credential ~/.chassiss/master-root.yaml \
  artifact accept <artifact-submission-id>
```

其余 Artifact 使用相同的提交和接受流程。Mission 应引用已接受的 Requirements 与 Architecture。Task 应引用所属 Mission，并声明范围、预算和 Checks。

已接受的 Artifact 构成冻结契约。需求变更应提交新 Artifact，并通过新 Task 或 `task supersede` 调整后续工作。

## 2. 激活 Mission

Orchestrator 在规划完成后激活 Mission。

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/orchestrator.cred \
  mission activate MSN-001
```

一个项目同一时间只能有一个 Active Mission。Mission 激活后，满足依赖且没有范围冲突的 Task 可以进入执行。

## 3. 分派或领取 Task

Orchestrator 可以把 Task 分派给指定 Developer。

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/orchestrator.cred \
  task assign TSK-001 --owner developer-a
```

Orchestrator 也可以为同一 actor 领取可执行 Task。该 actor 必须同时具有有效的 Developer grant。

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/orchestrator.cred \
  task claim TSK-001
```

分派和领取都会检查依赖、范围冲突、WIP 限制和 credential scope。检查通过后，Task 绑定到 Developer。

## 4. 打开工作区

Developer 为 Task 创建受控 worktree。

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/developer.cred \
  work open TSK-001
```

CLI 返回工作目录和任务分支。Developer 只在该目录中修改 `allowed_paths` 覆盖的文件，不在正式项目目录直接开发。

开始工作前可以读取当前上下文。

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/developer.cred \
  work context TSK-001
```

## 5. 检查与 checkpoint

Developer 在 Task worktree 中实现，不自行提交 Git commit。内容达到可交付状态后运行检查。

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/developer.cred \
  work check TSK-001 --all

chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/developer.cred \
  work checkpoint TSK-001 --file checkpoint.md
```

`work check` 验证 Task 范围、预算、Checks、验证源和当前工作区快照。`work checkpoint` 记录一段签名进度说明，不保存文件快照，也不会创建正式 Submission。

## 6. 提交工作

检查通过后，Developer 为同一快照提交工作。

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/developer.cred \
  work submit TSK-001 \
  --file handoff.md \
  --message "Implement task TSK-001"
```

`work submit` 由 CLI 为当前快照创建 Task commit。Submission 绑定 Task、Developer、基线、精确 Head commit、文件列表、检查结果、预算指标、handoff 和提交信息。`work check` 之后发生任何内容变化，都需要重新检查。

## 7. 设计一致性复核

Build Agent 或 Review Agent 将 Developer handoff 和候选 diff 提供给 Designer。Designer 对照已接受的 Task、Architecture 和 Requirements，核对实现是否保持既定边界。发现契约偏差时，Designer 将意见交给 Reviewer 或 Orchestrator。

当前 CLI 没有 Designer verdict 事件。这个步骤属于 Session 之间的协作约定，不会替代 Reviewer 的正式 verdict。Designer 不应使用 Reviewer credential，也不应直接集成代码。

## 8. 独立复核

Reviewer 先取得上下文并执行机械检查。

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/reviewer.cred \
  review context SUB-001

chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/reviewer.cred \
  review check SUB-001
```

语义复核通过后，Reviewer 批准 Submission。

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/reviewer.cred \
  review approve SUB-001 --report review.md
```

需要修改时提交退回意见。

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/reviewer.cred \
  review request-changes SUB-001 --report review.md
```

退回后，Developer 在原 Task worktree 中继续工作，重新运行 check 和 submit。历史 Submission 与 review report 会保留。

## 9. 集成

Reviewer 批准精确 Submission 后，由作出批准的同一 Reviewer 检查并执行集成。

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/reviewer.cred \
  integrate check SUB-001

chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/reviewer.cred \
  integrate apply SUB-001
```

CLI 在隔离的候选工作区合并 Task，重新运行 Checks 和独立验证，再推进正式分支。正式 baseline 只在全部验证完成后更新。

## 10. 完成 Mission

所有必要 Task 集成后，Orchestrator 提交 Mission 验收。

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/orchestrator.cred \
  mission submit-acceptance MSN-001 --evidence mission-evidence.md
```

Master 检查交付结果并接受 Mission。

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/master-root.yaml \
  mission accept MSN-001
```

Mission 完成后，项目回到可规划下一阶段的状态。

## 中断与继续

Mission 或 Task 暂时无法推进时应记录阻断原因。

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/orchestrator.cred \
  mission block MSN-001 --reason blocked

chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/orchestrator.cred \
  mission resume MSN-001

chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/orchestrator.cred \
  task block TSK-001 --reason blocked

chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/orchestrator.cred \
  task resume TSK-001
```

每次状态变化后重新运行 `bootstrap`。Agent 不应复用旧的 revision 或根据上一次输出猜测下一动作。

## 下一步

- [Task worktree、范围与预算](09-task-worktrees-and-budgets.md)
- [Checks 与独立验证源](10-checks-and-verification.md)
- [Reviewer、集成与审计](11-review-and-integration.md)
