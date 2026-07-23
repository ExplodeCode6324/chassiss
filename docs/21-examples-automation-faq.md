# 示例项目、自动化与 FAQ

本章提供常见流程的组合示例。命令中的 ID、路径和 credential 文件需要替换为 CLI 返回的实际值。写操作前先运行 `bootstrap`，并使用当前 revision。

## Greenfield 示例

创建 Root 和新项目。

```sh
chassiss auth master-init

chassiss --credential ~/.chassiss/master-root.yaml \
  project init /work/example
```

签发最小角色组合。

```sh
cd /work/example

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
```

Designer 生成 Requirements、Architecture、Mission 和 Task。每份 Artifact 都先 check，再 submit，由 Master 接受。

```sh
chassiss --credential ~/.chassiss/cred-designer-1.yaml \
  template get requirements --output docs/requirements.md

chassiss --credential ~/.chassiss/cred-designer-1.yaml \
  artifact check docs/requirements.md

chassiss --credential ~/.chassiss/cred-designer-1.yaml \
  artifact submit docs/requirements.md

chassiss --credential ~/.chassiss/master-root.yaml \
  artifact accept <artifact-submission-id>
```

执行第一个 Task。

```sh
chassiss --credential ~/.chassiss/cred-build-orchestrator.yaml \
  mission activate M001

chassiss --credential ~/.chassiss/cred-build-orchestrator.yaml \
  task assign M001-T001 --owner build-1

chassiss --credential ~/.chassiss/cred-build-developer.yaml \
  work open M001-T001
```

Developer 在返回的 worktree 中完成代码，不自行创建 Git commit，然后运行 Checks。`work submit` 会为通过检查的精确快照创建 Task commit。

```sh
chassiss --credential ~/.chassiss/cred-build-developer.yaml \
  work check M001-T001 --all

chassiss --credential ~/.chassiss/cred-build-developer.yaml \
  work submit M001-T001 --file handoff.md --message complete
```

Reviewer 独立复核并集成。

```sh
chassiss --credential ~/.chassiss/cred-reviewer-1.yaml \
  review context <submission-id>

chassiss --credential ~/.chassiss/cred-reviewer-1.yaml \
  review check <submission-id>

chassiss --credential ~/.chassiss/cred-reviewer-1.yaml \
  review approve <submission-id> --report review.md

chassiss --credential ~/.chassiss/cred-reviewer-1.yaml \
  integrate apply <submission-id>
```

所有 Task 结束后提交 Mission 验收。

```sh
chassiss --credential ~/.chassiss/cred-build-orchestrator.yaml \
  mission submit-acceptance M001 --evidence mission-evidence.md

chassiss --credential ~/.chassiss/master-root.yaml \
  mission accept M001
```

## Brownfield 示例

已有项目必须是 Git 根目录，worktree 干净，并且至少有一个 commit。

```sh
cd /work/existing-project
git status --short
git log -1 --oneline

chassiss --credential ~/.chassiss/master-root.yaml \
  project init /work/existing-project --existing
```

Brownfield 初始化保留当前 branch 和历史，并通过 `.git/info/exclude` 忽略 `.chassis/`。初始化前存在的未提交变化会导致拒绝。

接管完成后仍需签发角色 credential，并从 Requirements 与 Architecture 开始建立契约。

## Reviewer 退回示例

Reviewer 在 report 中记录可执行的修改要求。

```sh
chassiss --credential ~/.chassiss/cred-reviewer-1.yaml \
  review request-changes <submission-id> --report review.md
```

Developer 读取 Task 和 review history。

```sh
chassiss --credential ~/.chassiss/cred-build-developer.yaml \
  work context M001-T001

chassiss --credential ~/.chassiss/cred-build-developer.yaml \
  review history --task M001-T001
```

完成修改后重新运行全部 Checks，再由 `work submit` 创建新的 Task commit 和 Submission。旧 Submission、report 和 verdict 保留。

```sh
chassiss --credential ~/.chassiss/cred-build-developer.yaml \
  work check M001-T001 --all

chassiss --credential ~/.chassiss/cred-build-developer.yaml \
  work submit M001-T001 --file handoff-v2.md --message revised
```

Reviewer 对新的 Submission 重新执行 context、check 和语义复核。旧 approval 不会转移到新 digest。

## 预算越界示例

假设 Task 最多允许 20 个文件，`work check` 报告 `CHS-WORK-BUDGET-FILES`。

Developer 停止扩大变更，并把当前情况交给 Orchestrator。Designer 提交一个范围更小的新 Task，Master 接受后，Orchestrator 建立替代关系。

```sh
chassiss --credential ~/.chassiss/cred-build-orchestrator.yaml \
  task supersede M001-T001 --replacement M001-T002
```

旧 Task 的契约和历史不变。新 Task 使用新的 `allowed_paths`、预算和验收条件。

如果当前修改中已经有可独立交付的部分，也可以先缩减到原预算内，完成 review 与 integration，再用后续 Task 处理剩余工作。

## 并行 Task 示例

以下 Task 范围互不重叠。

```text
M001-T001  src/api/**
M001-T002  docs/**
M001-T003  tests/fixtures/**
```

Orchestrator 可以把它们分派给三个 Developer actor。每名 Developer 使用独立 worktree。

```sh
chassiss --credential ~/.chassiss/cred-orchestrator.yaml \
  task assign M001-T001 --owner developer-a

chassiss --credential ~/.chassiss/cred-orchestrator.yaml \
  task assign M001-T002 --owner developer-b

chassiss --credential ~/.chassiss/cred-orchestrator.yaml \
  task assign M001-T003 --owner developer-c
```

Reviewer 逐一批准和集成。后集成的 Task 会在最新 formal baseline 上构造候选 merged tree。

若两个 `allowed_paths` 可能重叠，后分派的 Task 会被拒绝。不要通过手工分支绕过范围检查。

## Owner 接管示例

项目没有 Active Mission、Active Task 和 pending Artifact 时，人类可以直接编辑普通项目文件。

```sh
cd /work/example
git status --short

chassiss --credential ~/.chassiss/cred-human-owner.yaml \
  owner apply --reason "Correct release metadata"
```

不要预先 commit。CLI 创建唯一 commit，并将新 Head 记录为正式 baseline。

```sh
chassiss --credential ~/.chassiss/cred-human-owner.yaml \
  owner history
```

## 崩溃恢复示例

若写命令中途退出，下一次命令可能返回 `CHS-OPERATION-RECOVERY-REQUIRED`。

```sh
chassiss --json --root /work/example \
  --credential ~/.chassiss/cred-build-orchestrator.yaml \
  doctor

chassiss --json --root /work/example \
  --credential ~/.chassiss/cred-build-orchestrator.yaml \
  recover
```

恢复成功后重新 bootstrap。若返回 `CHS-INTEGRITY-BLOCKED`，保留 journal、Git refs 和错误输出，停止写操作。

## 远端发布示例

```sh
chassiss --credential ~/.chassiss/cred-build-orchestrator.yaml \
  publish check --target github --remote origin --branch main

chassiss --credential ~/.chassiss/cred-build-orchestrator.yaml \
  publish apply --target github --remote origin --branch main
```

远端为空或当前 Head 是本地 baseline 的祖先时可以发布。远端分叉时先将远端变更通过 Task 纳入正式历史。

## 五分钟轮询模板

下面的 shell 脚本展示只读轮询。它不会自动执行 mutation。

```sh
#!/bin/sh
set -eu

CHASSISS_BIN=/opt/chassiss/chassiss
PROJECT_ROOT=/work/example
CREDENTIAL_FILE=/run/secrets/chassiss-role.yaml

"$CHASSISS_BIN" \
  --json \
  --root "$PROJECT_ROOT" \
  --credential "$CREDENTIAL_FILE" \
  bootstrap
```

cron 可以每五分钟运行一次。

```cron
*/5 * * * * /opt/chassiss/poll.sh >>/var/log/chassiss-poll.log 2>&1
```

Agent 调度器读取 bootstrap 输出后，再让对应角色处理 `available_actions`。credential 文件和完整 bootstrap 输出不应写入公开日志。

## 自动执行循环

需要自动 mutation 时，调度器遵循以下状态机。

```text
bootstrap
   |
   v
读取 context_requests
   |
   v
选择 available_action
   |
   v
带 revision 执行一次
   |
   +---- 成功 ----> 回到 bootstrap
   |
   +---- 冲突 ----> 回到 bootstrap
   |
   +---- integrity error ----> 停止并通知 Master
```

每次只执行一个状态动作。不要批量缓存多个 mutation 命令。

## 实验性全自动 Master

前沿模型可以代行人类 Master，创建子 Agent、签发角色 credential、接受 Artifact 和 Mission，并管理整个项目。

适合测试的项目具有以下特征。

- 源码和讨论可以公开
- 失败后容易重建
- 需求规模较小
- 有明确自动测试
- 人类能够审阅完整事件和 Git 历史

可观察的成功指标包括 Mission 完成率、Reviewer 退回次数、预算命中率、人工介入点和最终缺陷。失败案例也很有价值，特别是模型错误接受 Artifact、共享秘密、忽略 bootstrap 或错误处理冲突的过程。

该模式不属于默认设计目标。共享执行环境中的 Root 和 credential 无法保持秘密隔离，项目质量也没有额外保证。

## FAQ

### CHASSISS 必须使用 GitHub 吗

不需要。当前 content backend 使用本地 Git，发布可以指向 GitHub、GitLab 或普通 Git remote。

### 可以让 Designer 和 Reviewer 是同一个 actor 吗

CLI 只强制 Submission 作者与批准 Reviewer 不同。为了保持需求、实现和复核的独立视角，推荐使用独立 Designer 和 Reviewer actor。

### Build Agent 为什么有两份 credential

Orchestrator 负责 Mission 和 Task 调度，Developer 负责 Task worktree。相同 actor 让两种角色在同一 Build Session 交接同一个 Task，同时保留各自 action 边界。

### Developer 可以直接领取 Task 吗

`task claim` 使用 Orchestrator credential。相同 actor 还必须有有效 Developer grant。也可以用 `task assign --owner <actor>` 显式分派。

### Reviewer 批准后谁执行 Integration

由作出批准的同一 Reviewer actor 执行。CLI 会核对 approval 中的 Reviewer 和 Submission digest。

### 为什么 Check 通过仍然需要 Reviewer

Check 验证可运行条件和冻结证据。它不能判断需求理解、设计边界、维护成本和未建模风险。

### 可以编辑 `.chassis/state.yaml` 修复状态吗

不能。State 来自签名事件链。使用 `recover`、`doctor` 和 `verify`。

### 多台机器可以各自保留 `.chassis` 吗

当前版本不支持多个控制副本并行写入。Git remote 只同步正式代码 baseline。

### credential armor 可以贴到聊天里吗

Armor 包含私钥。只在受控、短期和访问范围明确的通道分发，并在泄漏后立即 revoke。

### Windows 可以运行吗

当前 bundled CLI 只覆盖 macOS 和 Linux。Windows 写操作会返回 `CHS-LOCK-UNSUPPORTED`。

## 提交高质量 Issue

Issue 应包含以下信息。

- CHASSISS binary version
- Config、State、Event 和 Bootstrap version
- 操作系统、架构、Go 与 Git 版本
- Greenfield 或 Brownfield
- 角色和命令形式
- 稳定错误码与 diagnostic category
- 最小复现步骤
- 预期结果和实际结果
- 是否存在 operation journal
- `doctor --json` 与 `verify --json` 的脱敏结果

不要附上 Root、credential armor、私钥、token、完整远端 URL 或私有源码。

## 下一步

- [五分钟 Quickstart](02-quickstart.md)
- [错误处理与故障排查](18-troubleshooting.md)
- [测试记录、已知问题与改进方向](22-testing-and-improvements.md)
