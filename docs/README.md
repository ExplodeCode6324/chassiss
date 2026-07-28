# CHASSISS v1 规范索引

状态：待 Master 复核，尚未锁版。

本目录是 CHASSISS 重构实现的唯一规范源。版本锁定后，CLI、Reducer、模板、
测试和 Skill 都必须以本目录为准。

其他目录中的历史草案只保存设计过程与决策来源；它们与当前 `docs/` 目录冲突
时，以当前目录为准。模板只提供可复制的结构，不承担字段解释；字段语义由
对应规范文档定义。

## 规范文档

1. [01-overall-architecture.md](01-overall-architecture.md)
   - 系统边界、组件、共享真相、CLI 控制面与核心工作流
2. [02-git-transition-protocol.md](02-git-transition-protocol.md)
   - Action、Semantic Operation、Execution Evidence、Reducer、Commit topology、
     Trailer 与验证算法
3. [03-state-model.md](03-state-model.md)
   - `state.json` 字段、稀疏 Task 投影与 State 不变量
4. [04-taskbook-and-architecture.md](04-taskbook-and-architecture.md)
   - 单轮 Taskbook lifecycle、持久 Architecture schema、Resource Graph、
     Context 与冲突算法
5. [05-task-lifecycle-review-integration.md](05-task-lifecycle-review-integration.md)
   - Task、Attempt、Review、Drift、Checks 与 Integration
6. [06-cli-command-specification.md](06-cli-command-specification.md)
   - CLI 命令树、参数边界、受管 Git 工作流与失败恢复
7. [07-cryptography-and-authority.md](07-cryptography-and-authority.md)
   - Root、Key、Grant、签名、摘要、Trust bootstrap 与 anti-rollback
8. [08-data-ownership-and-local-state.md](08-data-ownership-and-local-state.md)
   - Git 共享数据、本地持久状态、cache、ref 保留与只读导出
9. [09-skill-runtime-contract.md](09-skill-runtime-contract.md)
   - 通用 Skill、动态 Context 与项目方法层边界
10. [10-cli-json-api-and-errors.md](10-cli-json-api-and-errors.md)
    - JSON response envelope、Context schema、错误码与退出码
11. [11-implementation-conformance.md](11-implementation-conformance.md)
    - 实现模块、测试向量、验证顺序与 v1 完成条件
12. [12-implementation-differences.md](12-implementation-differences.md)
    - 旧项目到 v1 的架构变化、实现选择、已知 lock-candidate 差距与理由

## 使用指南

- [中文指南](cn/README.md)
- [English guides](en/README.md)

使用指南沿用旧项目从概览、安装、权限、工作流、安全到测试发布的阅读顺序，
但所有命令和概念均已按当前 v1 规范重写；它们不是第二份协议源。若指南与上述
规范冲突，以规范为准。

## 纯模板

- [templates/state.json](templates/state.json)
- [templates/architecture.yaml](templates/architecture.yaml)
- [templates/taskbook.yaml](templates/taskbook.yaml)
- [templates/review-report.json](templates/review-report.json)
- [templates/taskbook-closure-report.json](templates/taskbook-closure-report.json)
- [templates/grant-request.json](templates/grant-request.json)
- [templates/local-state.json](templates/local-state.json)

## 规范关键词

文档中的“必须”“禁止”是 v1 一致性要求；“应该”是默认实现要求，只有明确
记录理由时才能偏离；“可以”表示不影响协议一致性的选择。

## v1 核心边界

- 一个 Git repository 是一个 Project。
- `refs/heads/main` 是唯一权威共享主线。
- `.chassiss/state.json` 只保存验证下一次转换必需的当前投影。
- `docs/architecture.yaml` 是跨工作流架构合同。
- `docs/taskbook.yaml` 是当前单轮工作流合同；验收后归档，再创建下一轮。
- Taskbook 归档前必须在 exact current main 通过工作流级机械 Checks。
- CLI 接管全部受支持的 Git 工作流。
- Agent 只编辑 CLI 分配的 Task worktree 内容。
- 所有 mainline mutation 都是签名 Transition Commit。
- Task 是唯一稳定工作实体；Attempt/Review 是当前内嵌投影。
- 一个 Attempt 只有一个当前 Reviewer；独立 Reviewer 是建议，不是协议有效性
  条件。
- Integration 固定使用双 parent commit。
- Git history 是授权、Action、Review 和 Integration 的共享账本。
- Agent 私钥、Root 私钥、checkpoint、pending Operation 和 worktree registry
  只在本地。
- Grant Authority 只来自 verified shared State/history；本地不另存 Grant
  object 或 discovery cache。
- 不依赖 GitHub、GitLab、外部数据库、外部 CI 或 Artifact Store。
- 不提供跨版本自动迁移，只提供只读导出。

## 明确排除

```text
Mission / Task group
多 Reviewer / quorum
Architecture Waiver
Root rotation / recovery
Grant expiry / delegation
Global Budget / Reservation
平台 Adapter
外部 Evidence 权威
自由 Git branch / merge / rebase
自动 Taskbook merge
历史重写或裁剪
跨版本 import
模型、token、费用计量
```

Owner Apply 是受限的人工接管通道，不属于自由 Git workflow：只有不存在活动
Agent Task/worktree/pending mutation 时才可使用，并且仍由 CLI 产生签名
Transition。
