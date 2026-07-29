# CHASSISS

中文 | [English](README.en.md)

CHASSISS 是一个以签名 Git 转换为共享账本、由 CLI 独占 Git 工作流的多 Agent
项目协议。Architecture 定义长期结构，Taskbook 定义一轮工作合同，State 只保存
验证下一次转换所需的最小投影；Root 与 Grant 决定谁能对什么 Task/Resource
执行哪种 Action。

当前仓库是面向 `chassiss/v1` 的全新实现，不兼容旧项目的 v0.x 控制目录、
credential 或 Mission 模型。旧仓库只作为只读的文档组织参考。协议文档当前仍是
“待 Master 复核”，因此本实现是 lock candidate，而不是已经冻结的正式 v1。

## Quick Start

下面按人类组织一次多 Agent 项目的顺序说明。命令中的 `chassiss` 指可信 Skill
内经 manifest digest 校验的 CLI；私钥、申请文件和文档草稿都应放在项目目录外。

> **现行 v1 的一次性引导约束：** Agent 可以先提交公钥和 Grant Request，但
> Grant 必须在 Genesis 之后才能写入项目账本；而 Genesis 又必须同时包含第一版
> Architecture 和 Taskbook。因此，首次启动时先完成第 0–4 步的人类决策，再按
> 第 4 步给出的顺序一次性执行 `init` 和 `grant add`。如果要求“先把 Grant
> 写入账本，再让 Architecture Agent 参与讨论”，需要新增只含 Root 的 bootstrap
> 协议状态，不能只靠调整命令顺序实现。

### 0. 准备项目

建立一个没有 Git history 的项目目录，选定稳定的 Project ID，例如
`PRJ-EXAMPLE`。不要手工运行 `git init`；第 4 步的 `chassiss init` 会初始化
Git、创建 Genesis 并写入第一版合同。

### 1. 生成 Root

由人类项目负责人在项目外生成 Root key：

```text
chassiss key generate --id KEY-ROOT-01 --actor master --json
```

Root 是项目的最终授权者。当前实现允许同一个 Root key 初始化多个项目，但每个
项目仍分别锚定 Root、维护 Grant 和验证历史。复用能减少密钥管理成本，也会把
单个私钥泄露的影响扩大到所有复用它的项目；在 Root rotation 和上级委托机制
完成前，重要项目默认使用独立 Root。

### 2. 收集各 Agent 的公钥

通知准备参与 Architecture、编码、Review 和 Integration 的 Agent：各自在项目
外生成独立 key，并返回带 proof-of-possession 签名的 Grant Request。Request
包含公钥，不包含私钥；不要只接收一段无法证明持有关系的裸公钥。

```text
chassiss key generate --id KEY-AGENT-01 --actor agent-one --json
chassiss grant request \
  --project PRJ-EXAMPLE \
  --key KEY-AGENT-01 \
  --profile developer \
  --task-scope 'TASK-*' \
  --resource-scope 'module:*' \
  --limits bounded \
  --output /outside/project/agent-one-request.json \
  --json
```

### 3. 审核授权

人类负责人逐一核对 Agent 的身份、公钥、Capability、Task scope、Resource scope
和 limits，形成授权清单。Root 只批准完成当前职责所需的最小权限；编码与 Review
使用不同的 Key/Grant，临时子 Agent 之间也使用互相隔离的工作目录、Key 和 Grant。

首次启动时先完成审核，不要伪造尚不存在的账本记录；签名发布在下一步 Genesis
建立后立即执行。

### 4. 讨论需求和架构，建立项目账本

人类先与拟授予 Architecture 讨论权限的 Agent 讨论需求、长期架构、验收条件和
任务拆分，并依据项目模板生成项目外草稿：

- [`docs/templates/architecture.yaml`](docs/templates/architecture.yaml)
- [`docs/templates/taskbook.yaml`](docs/templates/taskbook.yaml)

人类确认草稿后，由 Root 创建 Genesis；随后按照第 3 步的授权清单签名并发布
Grant，最后做一次完整验证：

```text
chassiss init \
  --project PRJ-EXAMPLE \
  --architecture /outside/project/architecture.yaml \
  --taskbook /outside/project/taskbook.yaml \
  --root-key KEY-ROOT-01 \
  --json

chassiss grant add \
  --request /outside/project/agent-one-request.json \
  --grant-id GRT-AGENT-01 \
  --root-key KEY-ROOT-01 \
  --profile developer \
  --task-scope 'TASK-*' \
  --resource-scope 'module:*' \
  --limits bounded \
  --json

chassiss verify --full --json
chassiss context --json
```

不同职责应使用各自审核后的 explicit Capability、scope 和 limit；上面的
`developer` 只演示编码 Agent。完整授权说明见
[信任、密钥与 Grant](docs/cn/03-信任密钥与Grant.md)。

### 5. 让编码 Agent 和复核 Agent 工作

Master 把 Task 交给拥有对应编码 Grant 的 Agent。Agent 只在 CLI 返回的 managed
worktree 中、按 Task `writes` 修改普通文件：

```text
chassiss context TASK-001 --json
chassiss task start TASK-001 --json
chassiss work open TASK-001 --json
# 在返回的 managed worktree 中编辑
chassiss work status TASK-001 --json
chassiss work commit TASK-001 --message "implement TASK-001" --json
chassiss check TASK-001 --json
chassiss submit TASK-001 --json
```

随后通知拥有复核权限的独立 Agent：用 `review --prepare` 生成与 exact candidate
绑定的 Review Report，填写并签名 Verdict。只有通过 Check 且被批准的 candidate
才交给拥有 Integration 权限的 Agent 执行 `integrate`。

### 6. 循环直到项目完成并回收权限

对每个 Task 重复第 5 步。所有 Tasks terminal 后，Reviewer 填写 Closure Report，
执行 `taskbook archive`。人类负责人随后对本轮临时 Grant 执行 `grant revoke`；
各 Key 所有者或 Master 调度器删除临时私钥和隔离工作目录。

临时 Agent 成功时直接回收；失败时先用 `attempt abandon` 把问题签入 State/history，
再 revoke Grant、删除 Key 并销毁工作目录。历史签名仍可审计，被撤销的 Grant
不能再授权未来 Transition。

不要直接运行会改变协议仓库的 `git add/commit/branch/checkout/worktree/merge/
rebase/reset/push/config`。通用 Agent 操作说明在
[`skills/chassiss/`](skills/chassiss/)；该 Skill 捆绑 macOS/Linux 的
arm64/amd64 CLI。

## 核心边界

- `refs/heads/main` 是唯一权威共享主线；每个 main commit 都是 SSH 签名的
  Transition。
- `.chassiss/state.json` 是 Reducer 生成的 canonical JSON，不能手工编辑。
- `docs/architecture.yaml` 与当前 `docs/taskbook.yaml` 通过受控命令变更。
- Agent 只在 CLI 返回的 managed worktree 中、按 Task `writes` 修改普通文件。
- Check 是机械结果，Review 是 Reviewer 的语义判断，二者不能互相替代。
- Integration 是 exact candidate 的双 parent commit；relevant/unknown drift 必须
  重新 Review。
- private key、checkpoint、pending operation、worktree registry 与 cache 只存在
  于仓库外的 Local State。
- 不依赖 GitHub/GitLab、外部数据库、外部 CI 或特定托管平台。

## 文档

- [规范索引](docs/README.md)：12 份 normative v1 文档、模板与 lock 流程
- [中文使用指南](docs/cn/README.md)
- [English guides](docs/en/README.md)
- [实现与旧项目差异](docs/12-implementation-differences.md)
- [跨实现 fixtures](fixtures/README.md)

机器可读的完整命令树以 `chassiss help --json` 为准；稳定 Error code、退出码和
response envelope 以规范为准。

## 当前验证状态

本地测试覆盖 canonical JSON、domain-separated digest、严格 YAML、State
不变量、Reducer、SSHSIG、Git object/topology、历史 verifier、Local State、
Task lifecycle、Review/Integration、Taskbook Closure、offline proposal 与
Owner Apply。仓库还包含 14 类跨实现 fixture 目录。

按 Master 要求，本轮没有运行真实 GitHub/网络 remote/系统 secret-store 外部集成
测试。剩余 lock-candidate 差距和安全影响持续登记在
[实现差异文档](docs/12-implementation-differences.md)，不会用 README 宣称掩盖。
