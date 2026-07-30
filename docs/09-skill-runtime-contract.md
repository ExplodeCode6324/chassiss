# Skill Runtime Contract

## 1. Skill 定位

通用 CHASSISS Skill 只教 Agent：

```text
如何找到受信 CLI
如何把已有 Git snapshot 接入 Root-only bootstrap
如何取得 verified Context
如何使用 CLI 管理的 Task worktree
如何调用 available actions
哪些高代价边界不能绕过
```

Skill 不实现协议、不解析 State、不管理 Git、不复制项目 Taskbook。Skill
直接捆绑四个 release CLI artifact，统一 launcher 在执行前按 manifest 校验
SHA-256。

## 2. 分层

```mermaid
flowchart TD
    S["通用 CHASSISS Skill"] --> C["CLI / Protocol"]
    P["Project AGENTS.md / Skills"] --> A["Agent 工作方法"]
    C --> A
    T["Architecture + Taskbook"] --> C
    T --> A
```

| 层 | 可以定义 | 禁止定义 |
|---|---|---|
| 通用 Skill | CLI 启动、安全边界、Context 使用 | Capability、Task 内容、Git 实现 |
| CLI/Protocol | State、Action、Reducer、签名、Git workflow | 项目领域实现方法 |
| Architecture/Taskbook | 架构资源；本轮 Requirements、Task、Checks | private/local 状态 |
| Project AGENTS/Skills | 编码方法、领域工具、质量建议 | 扩权、绕过 CLI、覆盖协议合同 |

必须强制的架构边界进入 Architecture，当前工作要求进入 Taskbook；只写在
AGENTS/Skill 的内容是工作方法，不是协议合法性条件。

## 3. 安装与可信 CLI

Skill 包含以下静态 artifact：

```text
bin/darwin-arm64/chassiss
bin/darwin-amd64/chassiss
bin/linux-arm64/chassiss
bin/linux-amd64/chassiss
manifest.json
manifest.sha256
scripts/chassiss
```

Agent 只调用 Skill absolute path 下的 `scripts/chassiss`。Launcher 根据
`uname` 选择 artifact、从 `manifest.sha256` 读取 exact digest、使用平台
SHA-256 工具校验，然后 `exec`。缺少平台、manifest、校验器或 digest 不一致
都 fail closed。不得用 Project repository 中的 binary 或 PATH 同名命令覆盖。

`manifest.json` 绑定 Skill bundle schema、protocol、version、source commit、
release identity、每个平台 path/digest。发布者从 clean source commit
cross-build，原生平台再执行 wrapper smoke。

## 4. 每次进入 Project

Agent 首次进入目录：

```text
<skill>/scripts/chassiss version --json
<skill>/scripts/chassiss context --json
```

如果目录不是 registered Project，按 CLI remediation 使用 `clone` 或报告。
只有 Master 明确要求接入已有项目时，Skill 才可按 onboarding reference 在
空目标目录使用 `bootstrap`；不得把任意未注册目录静默转成 Project。

Context 返回：

- verified/untrusted/offline status；
- Project ID、main checkpoint、Architecture blob 和可选活动 Taskbook blob；
- current identity/Grant；
- available Tasks；
- pending local operations；
- available actions；
- local workflow commands；
- 下一条安全只读命令。

Agent 不自行扫描 `.git` 推断协议状态。

### 4.1 已有项目接入

Skill 把工作分成机械层和语义层：

1. CLI 从 Master 指定的 full source commit OID 只读导入 ordinary snapshot，
   拒绝 protected collision、submodule 与逃逸 symlink，并生成
   `docs/chassiss/onboarding/source-history.md`；
2. Root 创建 `project.bootstrap`，再审核带 proof-of-possession 的
   `architecture.establish` Grant；
3. Architecture Agent 只读审计普通源码和构建/依赖清单，在 Project 外编写
   candidate；
4. CLI 只验证 schema/graph/path，Master 或独立 Reviewer 负责语义准确性；
5. `architecture establish` 之后才进入 `taskbook draft --new/open`。

Skill 不读取旧 `.git` 来推断 CHASSISS Authority，不把旧 commits 称为
Transitions，也不让 Agent 在 Architecture 建立前执行普通 Task mutation。

## 5. 开始 Task

```text
chassiss context TASK-001 --json
chassiss task start TASK-001 --json
chassiss work open TASK-001 --json
```

`task start` response 返回 managed worktree absolute path。Agent 后续所有源码
编辑和构建命令必须在该 path 下执行。

Agent 不自行创建 branch、checkout 或 worktree。

### 5.1 Master 调度临时 Agent

Master 为每次 Task attempt 分配独立且不复用的 Actor、Key、narrow Grant、
CLI-managed worktree 与临时父目录。并行 Agent 不共享 mutable checkout、
worktree、Key、Grant 或 scratch directory；产生 Transition 且 command schema
暴露 signer 选项的 mutation 显式传 `--key/--grant`，不依赖全局 selected
identity。`work commit` 固定使用 frozen Task Actor key，不接受这两个选项。

成功 Integration 后立即 revoke 临时 Grant、remove Key、确认 worktree/Work
Ref 已清理，再销毁临时目录。失败时顺序固定：

```text
attempt abandon（签名正文 + State 轻量索引 + worktree/ref cleanup）
→ grant revoke
→ key remove
→ 删除临时目录
```

失败记录成功签名以前不得先销毁目录。目录隔离只是 workflow boundary；不互信
Agent 还必须使用 OS/container isolation。

## 6. 动态 Context

`chassiss.context/v1` 默认只内联当前 Task 所需的确定性 slice。

如果需要更多知识，Agent使用 Context 返回的 argv，例如：

```text
chassiss taskbook show --requirement REQ-001 --json
chassiss architecture show module:core --json
chassiss architecture requires module:core --transitive --json
chassiss architecture impact schema:state --json
chassiss file show src/core/state/model.go --at main --json
```

Skill 不把整个 Taskbook、所有命令帮助或所有 Architecture View 固定塞入
prompt。

## 7. Git 行为

Agent 可以：

- 在 managed worktree 使用编辑器创建/修改/删除 Task `writes` 内文件；
- 运行 Taskbook 未规定但只读或本地的开发工具；
- 调用 `chassiss work status/diff/log`；
- 调用 `chassiss work commit/restore/remove`；Work Ref 由 `submit` 自动发布。

Agent 禁止直接运行会改变 Git 的命令，包括：

```text
git add
git commit
git branch
git checkout/switch
git worktree
git merge
git rebase
git cherry-pick
git reset
git push
git config
```

Agent 需要 Git 结果时使用 CLI 等价命令。普通只读 Git 不改变协议安全，但
通用 Skill 不推荐或依赖它。

## 8. Work loop

```text
编辑 managed worktree
→ chassiss work status
→ chassiss work diff
→ 项目工具检查
→ chassiss work commit
→ chassiss check
→ chassiss submit
```

`submit` 会重新执行权威 preflight；Agent 不把之前的 Check 输出当作 submit
已成功。

## 9. Review loop

Reviewer：

```text
chassiss context TASK-001 --json
chassiss review TASK-001 --prepare --output <context-file> \
  --report-output <report-file> --json
<阅读 candidate、执行语义复核、填写 Report>
chassiss review TASK-001 --verdict ... --report <file> --json
chassiss review show TASK-001 --operation <operation-id> --json
```

Skill 必须区分：

```text
Check Result      机械命令结果
Review Verdict    Reviewer 语义判断
```

Check pass 不能自动生成 approve。
Reviewer 与 submitter 相同只产生协议 warning；Skill 应把 warning 明确呈现给
Master，不能伪称为独立 Review，也不能擅自拒绝 Master 允许的同主体 Review。

### 9.1 Workflow closure

全部 Tasks terminal 后，Reviewer 先用
`taskbook archive --prepare --output <report-file>` 生成逐 Task disposition
与 completion criteria response 模板，填写后调用
`taskbook archive --report <report-file>`。Skill 不自行运行一份结果来替代
协议检查：archive 命令必须在 exact current main 上重新执行全部
`workflow.checks`，并把 Context/Results 放入 Reviewer 签名 Evidence。
cancelled/superseded 不要求转成 closed，但必须有 Reviewer 的逐项非空确认。

## 10. Context 刷新

Agent 在以下情况重新执行 Context：

- start/submit/review/integrate 等 mutation 后；
- CLI 报 main changed/stale；
- identity/Grant 改变；
- Architecture/Taskbook updated/opened/archived；
- 从另一个 Task/worktree 切换；
- blocked/resumed/request_changes；
- CLI remediation 明确要求。

即使 Agent 未刷新，mutation CLI 仍必须内部 sync/verify，旧 Context 不提供
授权。

## 11. 结构化拒绝

Agent 收到 Error 时：

1. 读取 `code`、`retryable`、`details`；
2. 只执行 `remediation[].argv` 中符合当前用户目标的命令；
3. 不自行修改 State/Architecture/Taskbook/Grant；
4. Capability、Scope、Root、rollback、relevant drift 或 Task conflict 问题
   报告 Master；
5. 不通过换 key、换 remote 或新 Operation ID 绕过。

## 12. 最小安全边界

Skill 必须保留：

- 不直接修改 Git；
- 不直接编辑 State；
- Architecture/Taskbook 只经 draft/validate/diff/open/update/archive；
- 不伪造 Trailer、Semantic Operation、Execution Evidence、Report digest 或
  signature；
- 不提交 private key、secret、local path、cache、Session 或 token；
- 不信任过期/离线 Context 进行 mutation；
- 不推断未授予 Capability；
- 不扩大 Task `writes`/Resource scope；
- 不把 Check pass 当 Review approve；
- relevant/unknown drift 必须重新 Review；
- Root/Grant/rollback 错误必须停止。

`owner apply` 不属于普通 Agent Work loop。只有 Master 明确要求人工接管、且
CLI 报告不存在活动 Agent workflow 时，Skill 才可以导航该命令；不得用它绕过
Task Contract、Review 或 Integration。

## 13. Skill 目录

```text
SKILL.md
agents/
  openai.yaml
bin/
  darwin-arm64/chassiss
  darwin-amd64/chassiss
  linux-arm64/chassiss
  linux-amd64/chassiss
references/
  safety.md
  context.md
  master-orchestration.md
  onboarding.md
scripts/
  chassiss
  build-bundle.sh
manifest.json
manifest.sha256
```

`SKILL.md` 目标不超过约 100 行。`references` 只解释稳定概念，不复制 CLI
command tree、Error code table 或项目 Task 内容；这些由 CLI 动态返回。

## 14. Profile

builder、reviewer、integrator、planner 等 Profile 只属于项目/部署层，用来帮助
Master 组合 Grants。Profile 不进入 State 或 Reducer。

CLI 可以提供规范 `developer` 快捷模板；Skill 必须把它理解为一组 explicit
Capability/Limit 的展开，仍要求 Master 明确选择 Scope，不能把 profile 名称
当作运行时角色或隐藏权限。

协议身份只由：

```text
Actor + Key + current Grant + Capability + Scope
```

决定。
