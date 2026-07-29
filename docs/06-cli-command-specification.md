# CLI Command Specification

## 1. CLI 定位

`chassiss` 是：

```text
Protocol verifier
+ Git workspace manager
+ State transition engine
+ Authority signer frontend
+ deterministic Context provider
```

它是受支持工作流中的唯一 Git writer。它不编辑源码内容、不替代编译器，也不
实现 GitHub/GitLab/PR/CI 平台功能。

## 2. 全局规则

```text
chassiss <command> [arguments] [--json]
```

- 所有命令支持 `--json`；
- `--json` 时 stdout 恰好一个 `chassiss.cli/v1` object；
- progress/diagnostic 只写 stderr；
- mutation 可以接受 `--operation-id`，省略时 CLI 安全生成并持久化稳定
  Semantic Operation；每次 CAS attempt 的 Evidence 由 CLI 管理；
- mutation 可以接受 `--key <key-id>` 与 `--grant <grant-id>`；
- 只有一个 current Grant 能完整授权 Action 时 CLI 才自动选择；多个 Grant
  同时匹配时必须显式 `--grant`；
- 不建立本地 Grant object 或 discovery cache；显式或自动选择都只针对本次
  命令从 current verified State 得到的匹配结果。为恢复同一 pending
  Operation 而保存的 exact Authority 引用不视为 Grant cache；
- mutation 不提供 `--no-verify`、`--force`、`--no-sync`；
- read-only 命令可以使用 `--offline`，结果必须标记使用的 local checkpoint；
- 所有路径参数在使用前解析并验证；
- destructive local command 必须精确目标并要求交互确认或 `--yes`。

## 3. Public command tree

```text
chassiss version
chassiss help [command]

chassiss init
chassiss clone
chassiss sync
chassiss verify
chassiss status
chassiss context
chassiss log
chassiss file show

chassiss remote show
chassiss remote set

chassiss task list
chassiss task show
chassiss task start
chassiss task release
chassiss attempt abandon
chassiss attempt failures
chassiss task block
chassiss task resume
chassiss task cancel
chassiss task supersede

chassiss work open
chassiss work status
chassiss work diff
chassiss work log
chassiss work commit
chassiss work restore
chassiss work remove

chassiss check
chassiss submit
chassiss review
chassiss review list
chassiss review show
chassiss integrate

chassiss taskbook show
chassiss taskbook draft
chassiss taskbook validate
chassiss taskbook diff
chassiss taskbook open
chassiss taskbook update
chassiss taskbook archive

chassiss architecture show
chassiss architecture draft
chassiss architecture diff
chassiss architecture requires
chassiss architecture required-by
chassiss architecture impact
chassiss architecture validate
chassiss architecture update

chassiss key generate
chassiss key list
chassiss key show
chassiss key attach
chassiss identity select
chassiss key remove

chassiss grant request
chassiss grant list
chassiss grant show
chassiss grant add
chassiss grant revoke

chassiss transition inspect
chassiss transition publish

chassiss owner apply
chassiss export
chassiss cache clean
```

## 4. Project 与同步

### 4.1 `version`

```text
chassiss version [--json]
```

返回 CLI semantic version、支持的 protocol majors、build digest 与 release
identity。不读取 Project。

### 4.2 `help`

```text
chassiss help [command-path] [--json]
```

JSON 输出 machine-readable 参数 schema、mutating/read-only 分类、所需
Capability 和可能 Error codes。

### 4.3 `init`

```text
chassiss init
  --project <project-id>
  --architecture <candidate-yaml>
  --taskbook <candidate-yaml>
  --root-key <private-key-handle>
  [--remote <url>]
```

要求目标目录本身尚无 Git history。父目录即使属于另一个 Git repository 也不
算目标历史，CLI 会在目标目录建立独立 repository。CLI：

1. 验证 Architecture 与 Taskbook 及其交叉引用；
2. 从 Taskbook Tasks 生成 ready State；
3. 创建零 parent Root self-signed Genesis；
4. 建立本地 trust anchor/checkpoint；
5. 创建 `main`；
6. 可选配置 remote 并 CAS 创建 remote main。

目录中的现有普通文件可以进入 Genesis tree，但 private/local protected data
必须拒绝。

### 4.4 `clone`

```text
chassiss clone <remote> <directory>
  --project <project-id>
  --root-fingerprint <fingerprint>
  --checkpoint <commit>
  [--key <private-key-handle>]
```

CLI 执行 clone/fetch，验证 Genesis、Project ID、Root fingerprint、checkpoint
ancestry 和 current main 后才登记本地 Project。

缺少 Root fingerprint 或 checkpoint 时只允许显式
`--untrusted-read-only`；该 checkout 不能 mutation。

### 4.5 `sync`

```text
chassiss sync [--all-work] [--prune]
```

fetch main、Work Ref 与 Archive Ref，验证新增 first-parent history，reconcile
pending Operation，推进 local checkpoint。

`--prune` 只删除已确认进入 main 或已归档的本地/远程临时 ref，不删除 Archive
Ref 或 active worktree。

### 4.6 `verify`

```text
chassiss verify [--full] [--commit <oid>] [--ref <ref>]
```

默认验证 local checkpoint 到 current main；`--full` 从 Genesis 验证。
`--commit`/`--ref` 只读验证指定目标，不自动接受为 checkpoint。
默认与 full 都必须校验当前 submitted/approved Work Refs 以及历史要求保留的
Archive Refs；本地 verified index 可以缓存要求集合，但 cache miss 时必须从
history 重建。

### 4.7 `status`

```text
chassiss status [--task <task-id>] [--offline]
```

返回 Project、verified head、Architecture/Taskbook blobs、identity、pending
operations、Task phases、worktrees 和 remote divergence。它不返回完整
Contract。

### 4.8 `context`

```text
chassiss context [<task-id>]
  [--section <name>]
  [--resource <resource-id>]
  [--offline]
```

无 Task 时返回 Project/identity/current Architecture/active Taskbook/available
Task 摘要；有 Task 时返回 effective Contract、Architecture slice、
dependencies、runtime State、worktree 和 available actions。

### 4.9 `log`

```text
chassiss log
  [--task <task-id>]
  [--action <action>]
  [--operation <operation-id>]
  [--limit <n>]
```

解析 verified first-parent Transition history，不直接暴露未经验证的 raw Git
log 作为协议事实。

### 4.10 `file show`

```text
chassiss file show <repo-relative-path>
  [--at <main|task-base|work-head>]
  [--task <task-id>]
  [--output <outside-repo-path>]
```

默认读取 latest verified main。`task-base`/`work-head` 要求 Task ID。JSON 返回
blob OID、mode、size、encoding 与 content；binary content 使用 base64。Human
模式对 binary 只显示 metadata，除非给出 source repo 外 `--output`。

该命令让 Agent 在 relevant drift 后读取 latest main 内容，不需要直接调用
`git show`。

## 5. Remote

### 5.1 `remote show`

```text
chassiss remote show
```

返回 authoritative upstream URL fingerprint、fetch/push reachability 与 remote
Project identity。

### 5.2 `remote set`

```text
chassiss remote set <url>
```

先从候选 remote 读取并验证同 Project ID、同 Root fingerprint，且其 main 是
local checkpoint 的 descendant；成功后才原子更新本地 remote 配置。

v1 一个 checkout 只有一个 authoritative upstream。

## 6. Task

### 6.1 `task list`

```text
chassiss task list
  [--phase <phase>]
  [--available]
  [--actor <actor-id>]
```

### 6.2 `task show`

```text
chassiss task show <task-id> [--history]
```

默认返回当前 Task projection 与 effective Contract 摘要；`--history` 解析该
Task 的 verified Transition history。

### 6.3 `task start`

```text
chassiss task start <task-id> [--key <handle>]
```

产生 `task.started`，随后创建本次尝试专用的受管 branch/worktree：

```text
refs/heads/chassiss/work/<task-id>/<base-prefix>/<actor>
<local-data>/worktrees/<project>/<task-id>/<base-prefix>/<actor>
```

输出 worktree absolute path 只存在本地 CLI response，不写入 Git。新尝试不
复用旧目录；verifier 只读兼容早期 v1 未带 `base-prefix` 的 Work Ref。

### 6.4 `task release`

```text
chassiss task release <task-id> --reason <text>
```

只允许 active、unblocked、clean 且 Work Head 恰好等于 frozen base。

### 6.5 `attempt abandon` / `attempt failures`

```text
chassiss attempt abandon <task-id>
  --root-key <root-key-id>
  --agent-key <agent-key-id>
  --agent-grant <agent-grant-id>
  --code <stable-code>
  --summary <text>
  --reason <text>

chassiss attempt failures <task-id> [--operation <operation-id>]
```

`attempt abandon` 是 Master/Root 的失败回收路径。它先验证 active Agent
identity 与本地受管 worktree，把完整失败记录和 observed Work Head/tree
签入 `attempt.abandoned`，向 State 追加轻量 index，将 Task 恢复为 ready，
然后销毁 worktree/Work Ref。失败记录签名成功前不得清理。`attempt failures`
默认返回索引；指定 Operation 后从 verified history 返回完整正文。

### 6.6 `task block`

```text
chassiss task block <task-id> --reason <text>
```

### 6.7 `task resume`

```text
chassiss task resume <task-id> [--reason <text>]
```

重新验证全部当前前置条件后删除 `blocked`。

### 6.8 `task cancel`

```text
chassiss task cancel <task-id> --reason <text>
```

有 current Attempt 时使用 remote atomic push 同时 create-only 创建 Archive
Ref 并 CAS main；remote 不支持 atomic push 时拒绝。

### 6.9 `task supersede`

```text
chassiss task supersede <task-id>
  --reason <text>
  [--replacement <task-id>]
```

只对 Root 或 `task.supersede` Grant 可用。

## 7. Work

所有 Work 命令必须指定 Task，或在当前目录唯一匹配一个受管 Task worktree。

### 7.1 `work open`

```text
chassiss work open <task-id>
```

返回/恢复受管 worktree。它不启动 ready Task；必须先 `task start`。

### 7.2 `work status`

```text
chassiss work status <task-id>
```

返回 tracked/untracked changes、scope violations、base/head 和 publish 状态。

### 7.3 `work diff`

```text
chassiss work diff <task-id>
  [--path <repo-relative-path>]...
  [--against <base|main|head>]
  [--stat]
```

默认 `--against base`，右侧是 current worktree。`--against main` 用于查看
Work 期望内容与 latest verified main 的差异；`--against head` 只显示未提交
变化。未跟踪文件按新增内容进入 diff；CLI 使用 disposable index 计算，绝不
修改 worktree 的真实 index。

### 7.4 `work log`

```text
chassiss work log <task-id> [--limit <n>]
```

只显示 frozen base 到 managed Work Head 的线性提交。

### 7.5 `work commit`

```text
chassiss work commit <task-id>
  --message <text>
  [--path <repo-relative-path>]...
```

省略 `--path` 时 stage 当前 Task worktree 的全部允许变化。CLI 拒绝 protected
path、越界 path、空 commit 和非线性 parent；使用 Task Actor key SSH-sign。

不提供 amend。

### 7.6 `work restore`

```text
chassiss work restore <task-id>
  --path <repo-relative-path>...
  [--yes]
```

只恢复尚未 commit 的明确 paths。禁止 broad target、`**`、repo root、Taskbook
和 State。CLI 在执行前显示将丢失的文件。

v1 不提供 reset committed history。

### 7.7 `work remove`

```text
chassiss work remove <task-id>
  [--discard-unreachable]
  [--yes]
```

默认只删除 clean 且 Work Head 已由 main/Archive Ref 保持可达，或 Head 恰好
等于 frozen base 的非活跃 worktree。存在 dirty files 或不可达 local commits
时拒绝。

`--discard-unreachable --yes` 只允许 ready/terminal Task，必须先列出将丢失的
commit OIDs 与 paths；它是 v1 唯一删除不可达本地 Work history 的入口。

## 8. Check、Submit、Review、Integrate

### 8.1 `check`

```text
chassiss check <task-id>
```

要求 worktree clean，在 current Work Head 上运行 scope/limit preflight 与
frozen Checks。只产生本地结果，不产生 Action。

### 8.2 `submit`

```text
chassiss submit <task-id>
```

要求 clean worktree，重跑 preflight/Checks，publish exact Work Head，产生
`task.submitted`。

### 8.3 `review`

```text
chassiss review <task-id>
  --verdict <approve|request_changes>
  --report <review-report-json>
```

CLI 先生成最新 candidate、运行 Checks、输出 Review Context；只有给定 Report
与该 Context 匹配时才产生 `task.reviewed-indexed`。完整 Report 留在签名
history，State 只追加可由 CLI 解析的轻量索引。旧 `task.reviewed` history
继续验证，但新 CLI 不再产生它。

Reviewer 与 submitter actor/key 相同时 CLI 必须显示 warning 并写入 JSON
`warnings`，但不拒绝。

可以分两步使用：

```text
chassiss review <task-id> --prepare
  --output <local-context-file>
  [--report-output <hydrated-report-file>]
chassiss review <task-id> --verdict ... --report ...
chassiss review list <task-id>
chassiss review show <task-id> [--attempt <n> | --operation <operation-id>]
```

prepare response 总是包含 `report_schema` 和按 frozen reviewer-attention
填充的 `report_template`；`--report-output` 可直接写出模板。prepare 文件是
本地辅助数据，不是权威证明；最终命令重新同步并验证。`review list/show`
从 verified State index 和签名 Transition history 解析报告。

### 8.4 `integrate`

```text
chassiss integrate <task-id>
```

重新分类 drift、重算 candidate tree、重跑 Checks，并创建唯一规范双 parent
Integration Commit。没有 topology 选择 flag。

## 9. Taskbook

### 9.1 `taskbook show`

```text
chassiss taskbook show
  [--task <task-id>]
  [--requirement <id>]
  [--constraint <id>]
```

### 9.2 `taskbook draft`

```text
chassiss taskbook draft
  --output <path>
  [--new]
```

默认把 current exact Taskbook 复制到源仓库外 candidate path；`--new` 生成
下一轮空白模板，只允许当前无活动 Taskbook。两者都记录 base Taskbook/
Architecture blob sidecar metadata，不得写入受管 project worktree。

### 9.3 `taskbook validate`

```text
chassiss taskbook validate --file <candidate-yaml>
```

### 9.4 `taskbook diff`

```text
chassiss taskbook diff --file <candidate-yaml>
```

返回文本 diff 与 Requirement/Resource/Task semantic diff。

### 9.5 `taskbook open`

```text
chassiss taskbook open
  --file <candidate-yaml>
  --reason <text>
```

只允许 `project.taskbook=null`。创建 `docs/taskbook.yaml`、ready Task State 和
签名 `taskbook.opened`。

### 9.6 `taskbook update`

```text
chassiss taskbook update
  --file <candidate-yaml>
  --reason <text>
```

候选 base blob 必须等于 current Taskbook blob。CLI 不自动 merge stale
Taskbook。

### 9.7 `taskbook archive`

```text
chassiss taskbook archive
  --prepare --output <hydrated-closure-report-json>
chassiss taskbook archive
  --report <completed-taskbook-closure-report-json>
```

`--prepare` 根据 exact terminal Task projection 和 Taskbook completion criteria
生成逐项带 `response` 字段的模板，不产生 Transition。

要求全部 Tasks terminal，重新验证逐 Task disposition、所有 closing
Integration 和 completion criteria response；随后在 exact current main 的
isolated clean checkout 中按顺序运行全部 `workflow.checks`。只有全部 pass 且
main 未改变时，才签名 Check Results、原样归档 active blob、清空 current
Task projection 并产生 `taskbook.archived`。

CAS 期间只出现纯 Authority Transition 时 CLI 可以在新 parent 重跑 Checks；
普通项目内容或其他 closure-relevant facts 变化时返回
`CHS_TASKBOOK_CLOSURE_STALE`，要求 Reviewer 重新确认 Report。

## 10. Architecture

```text
chassiss architecture show <resource-id> [--file <candidate>]
chassiss architecture draft --output <outside-repo-path>
chassiss architecture diff --file <candidate>
chassiss architecture requires <resource-id> [--transitive]
chassiss architecture required-by <resource-id> [--transitive]
chassiss architecture impact <resource-id>
chassiss architecture validate [--file <candidate>]
chassiss architecture update --file <candidate> --reason <text>
```

`impact` 返回受影响 Resources、Modules、ready/nonterminal Tasks 与原因路径。
`draft/diff/validate` 可在任意时刻只读使用；`update` 只允许没有活动 Taskbook，
候选 base Architecture blob 必须匹配 current State。

## 11. Key 与 Grant

### 11.1 `key generate`

```text
chassiss key generate
  --id <key-id>
  --actor <actor-id>
  [--store <store-kind>]
```

生成 Ed25519 key pair；只把 private-key handle 写本地 secret store，输出 public
key/fingerprint。

Root key 也使用同一命令，但 `init --root-key` 决定其 Root 身份。

### 11.2 `key list/show/attach/select/remove`

```text
chassiss key list
chassiss key show <key-id>
chassiss key attach <key-id> [--select]
chassiss identity select --key <key-id>
chassiss key remove <key-id> [--orphan-grant] [--yes]
```

`attach` 只把已存在的外部本地 Key handle 绑定到 current verified Project；
CLI 必须从当前 Root/Grant 验证 public key、Key ID 和 Actor，不能凭本地标签
授予身份。`identity select` 明确选择已 attach 的 Key。多个并行临时 Agent
不得依赖隐式 selected identity，签名 mutation 应显式传 `--key/--grant`。

`remove` 只删除本地 private-key material/handle，不撤销 Git Grant。若 State
仍有 Grant，CLI 必须警告并默认拒绝，除非用户先由 Root revoke 或明确
`--orphan-grant --yes`。

被任何 registered Project 用作 current Root 的 key 禁止通过 v1 CLI remove；
没有 override。Root key 生命周期由外部 secret-store/backup 策略管理。

### 11.3 `grant request`

```text
chassiss grant request
  --project <project-id>
  --key <key-id>
  [--profile <developer>]
  [--capability <capability>...]
  --task-scope <pattern>...
  --resource-scope <pattern>...
  --limits <bounded|unbounded>
  [--max-active-tasks <n>]
  [--max-changed-paths <n>]
  --output <file>
```

输出 non-secret、proof-of-possession signed Grant Request。

`--profile developer` 展开规范 developer Capability/Limit 模板；Task/Resource
scope 仍必须显式提供。可以同时提供 `--capability` 以请求模板外权限，但 CLI
必须分别展示模板权限与额外请求。

### 11.4 `grant list/show`

```text
chassiss grant list [--actor <actor-id>]
chassiss grant show <grant-id>
```

只以 current verified State 为权威。命令不读取或写入本地 Grant object 或
discovery cache。

### 11.5 `grant add`

```text
chassiss grant add
  --request <grant-request-json>
  --grant-id <grant-id>
  --root-key <handle>
  [--profile <developer>]
  [--capability <approved-capability>...]
  [--task-scope <approved-pattern>...]
  [--resource-scope <approved-pattern>...]
  [--limits ...]
  [--proposal <bundle-or-ref-output>]
```

Root 必须显式批准最终 Capability/Scope/Limits；不能把 request 自动升级为
Grant。profile 只帮助构造最终 explicit Grant；签名前必须展示展开结果。

### 11.6 `grant revoke`

```text
chassiss grant revoke <grant-id>
  --reason <text>
  --root-key <handle>
  [--proposal <bundle-or-ref-output>]
```

## 12. Transition proposal

```text
chassiss transition inspect <ref-or-bundle>
chassiss transition publish <ref-or-bundle>
```

publish 要求 proposal parent 恰好等于 current verified main；否则返回 stale，
不重写签名对象。

## 13. Export

```text
chassiss export
  [--ref <ref>]
  [--output <outside-repo-path>]
  [--format <json|markdown>]
```

严格只读。不修改 source repo、refs、remote 或 State。导出物非权威、不可直接
import。

## 14. Owner Apply

```text
chassiss owner apply
  --reason <text>
  --summary <text>
  [--yes]
```

命令没有 source path 参数。当前目录必须属于 registered、non-managed Project
worktree；CLI 读取该 worktree repository root 的普通文件变化。要求没有
active/submitted/approved Task、没有 active/orphan worktree、没有 unresolved
pending Operation；ready/blocked-ready/terminal Tasks 可以存在。CLI 拒绝协议
文件变化，显示 exact changed paths/candidate tree，使用 `owner.apply` Grant
签名并 CAS push。它不提供自由 commit message、branch、merge、rebase 或 force
flag。

## 15. Cache

```text
chassiss cache clean
  [--project <project-id>]
  [--kind <history|architecture|taskbook|candidate|checks|all>]
  [--yes]
```

不得删除 active worktree、checkpoint、trust anchor、private key 或 pending
Operation。

## 16. 隐式 mutation pipeline

所有 mutation 命令自动：

```text
fetch
verify
reconcile Operation ID
validate semantic operation
authorize
validate
build execution evidence
reduce
canonicalize
sign
post-verify
CAS push
fetch/confirm
checkpoint
```

调用者不能只执行其中一部分。

## 17. 不提供的 Git 操作

```text
generic branch
generic checkout/switch
manual staging
commit --amend
merge
rebase
cherry-pick
reset committed history
force-push
direct main commit
arbitrary worktree add/remove
```

如果项目确实需要这些行为，必须先形成新的协议设计，不能通过隐藏 passthrough
flag 绕过 v1。
