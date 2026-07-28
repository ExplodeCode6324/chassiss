# Git Transition Protocol

## 1. Protocol identity

CHASSISS v1 的权威协议标识：

```text
chassiss/v1
```

Transition Trailer 使用整数 major：

```text
CHASSISS-Protocol: 1
```

一个 major 共同控制 Action、Reducer、canonical bytes、signature、State、
Architecture 和 Taskbook。未知 major 必须拒绝 mutation。

## 2. Commit 类型

### 2.1 Genesis Commit

- Action 为 `project.genesis`；
- 零 parent；
- tree 包含初始 `.chassiss/state.json`、`docs/architecture.yaml`、
  `docs/taskbook.yaml` 和项目初始内容；
- 由新 State 声明的 Root key 自签；
- Project ID、Root fingerprint 和 Genesis commit 构成项目 identity bootstrap。

### 2.2 Transition Commit

- first parent 必须是创建时的 current verified main；
- tree 包含 Reducer 生成的 exact next State；
- message 包含 canonical Semantic Operation、Execution Evidence 与规范 Trailer；
- 由父 State 中的 Root 或有效 Grant key 签名；
- 只能具有 Action 白名单允许的 tree 变化。

普通 Transition 恰好一个 parent。

### 2.3 Work Commit

- 由 CLI 在 Task 受管 branch/worktree 创建；
- 形成从 frozen Task base 开始的单 parent 追加链；
- 不得修改 `.chassiss/state.json`、`docs/architecture.yaml`、
  `docs/taskbook.yaml` 或 `docs/taskbooks/archive/**`；
- 使用当前 Task Actor key 做 Git SSH signing；
- 不包含 CHASSISS Action Trailer；
- 不改变共享 State；
- `task.submitted` 绑定 exact Work Head 后才成为 Attempt。

### 2.4 Integration Commit

- Action 为 `integration.applied`；
- parent 1 = current verified main；
- parent 2 = approved exact Work Head；
- tree = CLI 在 parent 1 上重新计算并验证的 candidate tree；
- 同时写入 Reducer 生成的 closed Task State；
- 由具备 `integration.apply` 的 key 签名。

Integration 恰好两个 parent。v1 没有 squash、cherry-pick、rebase 或其他
Integration topology。

### 2.5 Candidate Integration Tree

v1 不调用可随 Git 版本、rename heuristic、attribute 或 custom merge driver
变化的文本 merge。Candidate 使用 deterministic tree overlay。

输入：

```text
B = frozen Task base tree
W = exact Work Head tree
M = target verified main tree
D = canonical changed-path set(B, W)
```

算法：

1. 验证 `D` 在 frozen `writes` 内且不含 protected path；
2. 把 B/W/M 递归展开为 non-tree entry 的
   `path → (mode, object-oid)` mapping；
3. `D` 是 B/W mapping 值不同的全部 paths；
4. 复制 M mapping；
5. 对每个 `p ∈ D`：W 有 entry 则设置 W 的 exact tuple，否则删除；
6. 验证结果不存在 file/symlink/gitlink 与 descendant path 的 prefix collision；
7. 验证 M 到 overlay mapping 的 changed paths 是 `D` 的子集，拒绝任何
   collateral deletion/change；
8. 按 Git tree entry byte ordering 从 mapping 唯一生成 tree objects；
9. 从 target main State 生成 hypothetical `integration.applied` closed State；
10. 用该 canonical State blob 替换 `.chassiss/state.json`；
11. 得到 candidate integration tree `C`。

Review 使用 `M=review_main` 计算 `C`；虽然 Review Action 本身产生 approved
State，Review 绑定的是“若立即完成 Integration 将进入 main 的完整 tree”。
Review 阶段的 hypothetical State projection 只把目标 Task 规范化为
`{"phase":"closed"}`，其他 State 保持不变；它不声称在尚未 approve 时已经
执行了 Integration Action。

Integration 使用 `M=current verified main` 重算 `C`，其 commit tree 必须恰好
等于 `C`。如果没有 drift，两者通常相同；unrelated drift 可以使 OID 不同，
但必须重新运行 Checks 并由 Integrator 签名。relevant/unknown drift 要求新
Review。

Overlay 是 whole-file/tree-entry replacement，不执行 line merge。相关 main
变化需要 Agent 在 Work Head 中显式形成新的完整期望内容并重新 Review。

## 3. Action 集

| Action | Authority/Capability | State 结果 |
|---|---|---|
| `project.genesis` | 新 Root 自签 | 创建 Project、Root、Architecture、Taskbook 与 ready Tasks |
| `architecture.updated` | `architecture.update` | 无活动 Taskbook 时更新 Architecture blob |
| `taskbook.opened` | `taskbook.open` | 创建下一轮活动 Taskbook 与 ready Tasks |
| `taskbook.updated` | `taskbook.update` | 更新 Taskbook blob；增加/更新 ready Tasks |
| `taskbook.archived` | `taskbook.archive` | 全部 Task terminal、整体验收及 Workflow Checks 通过后归档 Taskbook 并清空当前 Task 投影 |
| `task.started` | `task.start` | `ready → active` |
| `task.released` | `task.release` | 无改动的 `active → ready` |
| `task.blocked` | `task.block` | 非终态增加 `blocked: true` |
| `task.resumed` | `task.resume` | 删除 `blocked` |
| `task.submitted` | `task.submit` | `active → submitted`，固定 Attempt |
| `task.reviewed` | `review.attest` | submitted/approved 上 approve→`approved`；request_changes→`active` |
| `task.cancelled` | `task.cancel` | 非终态→`cancelled` |
| `task.superseded` | `task.supersede` 或 Root | 非终态→`superseded` |
| `integration.applied` | `integration.apply` | `approved → closed` |
| `authority.grant-added` | Root | 添加当前 Grant |
| `authority.grant-revoked` | Root | 删除当前 Grant |
| `owner.applied` | `owner.apply` | 无活动 Agent workflow 时应用人工候选 tree |

v1 没有独立 `task.created`；Task 创建属于 `taskbook.opened` 或
`taskbook.updated`。Check、Context、Work Commit、Work Ref publish、verify 和
export 不产生 Action。

### 3.1 Semantic Operation payload

Semantic Operation 只保存用户稳定选择，不保存会随 CAS parent 改变的执行事实：

| Action | Operation payload exact 字段 |
|---|---|
| `project.genesis` | `project_id`、`root_key_id`、`root_public_key`、`architecture_blob`、`taskbook_blob` |
| `architecture.updated` | `candidate_blob`、`reason` |
| `taskbook.opened` | `taskbook_id`、`candidate_blob`、`reason` |
| `taskbook.updated` | `candidate_blob`、`reason` |
| `taskbook.archived` | `closure_report` |
| `task.started` | 空 object |
| `task.released` | `reason` |
| `task.blocked` | `reason` |
| `task.resumed` | `reason`，string 或 null |
| `task.submitted` | `head` |
| `task.reviewed` | `verdict`、`report` |
| `task.cancelled` | `reason` |
| `task.superseded` | `reason`、`replacement_task`，string 或 null |
| `integration.applied` | 空 object |
| `authority.grant-added` | `grant_id`、`grant`、`request_digest`，后者为 digest 或 null |
| `authority.grant-revoked` | `grant_id`、`reason` |
| `owner.applied` | `reason`、`summary` |

每个 payload schema 都是 closed object；表中未声明字段必须拒绝。用户原因、
summary、Review Report 和 closure report 进入 Operation，不能使用 commit
message 自由文本绕过 schema。

### 3.2 Execution Evidence facts

Execution Evidence 保存本次 exact parent 下可重算或可签名证明的执行事实：

| Action | Evidence `facts` exact 字段 |
|---|---|
| `project.genesis` | `architecture_blob`、`taskbook_blob`、`initial_tree` |
| `architecture.updated` | `old_blob`、`new_blob`、`semantic_diff` |
| `taskbook.opened` | `architecture_blob`、`taskbook_blob`、`ready_tasks` |
| `taskbook.updated` | `old_blob`、`new_blob`、`semantic_diff` |
| `taskbook.archived` | `active_blob`、`architecture_blob`、`archive_path`、`archive_blob`、`terminal_tasks`、`closing_integrations`、`check_results` |
| `task.started` | `actor`、`base`、`taskbook_blob`、`architecture_blob` |
| `task.released` | `base`、`observed_work_head`、`observed_work_tree` |
| `task.blocked` / `task.resumed` | 空 object |
| `task.submitted` | `submission_evidence` |
| `task.reviewed` | `attempt_digest`、`review_context`、`check_results` |
| `task.cancelled` / `task.superseded` | `attempt_digest`、`archive_ref`、`archive_head`，三者同时为 null 或同时非 null |
| `integration.applied` | `attempt_head`、`review_context_digest`、`review_report_digest`、`drift_classification`、`candidate_tree`、`check_results` |
| Authority Actions | 空 object |
| `owner.applied` | `source_base`、`source_tree`、`candidate_tree`、`changed_paths`、`changed_paths_digest` |

Evidence 字段仍必须由 verifier 从 Git objects、父 State 和 frozen contracts
重算；“Evidence”不表示盲目信任调用者输入。无法从共享 Git 重放的
`worktree clean` 只属于 release 的本地 CLI preflight，不是 Reducer 不变量。
每个 `facts` 都是 closed object；未知、缺失、多余或 nullability 不匹配的字段
必须拒绝。

## 4. Capability 集

```text
taskbook.update
taskbook.open
taskbook.archive
architecture.update
task.start
task.release
task.block
task.resume
task.submit
task.cancel
task.supersede
review.attest
integration.apply
owner.apply
```

Authority Action 只允许 Root，不通过普通 Capability delegation。未知
Capability 拒绝。

## 5. Semantic Operation 与 Execution Evidence

调用者提交稳定 Semantic Operation，而不是 State patch：

```json
{
  "action": "task.submitted",
  "authority": "grant:GRT-001",
  "operation_id": "OPR-01ARZ3NDEKTSV4RRFFQ69G5FAV",
  "payload": {
    "head": "0123456789abcdef0123456789abcdef01234567"
  },
  "preconditions": {
    "architecture_blob": "0123456789abcdef0123456789abcdef01234567",
    "actor": "agent-builder-1",
    "base": "0123456789abcdef0123456789abcdef01234567",
    "phase": "active",
    "taskbook_blob": "0123456789abcdef0123456789abcdef01234567"
  },
  "project": "PRJ-EXAMPLE",
  "schema": "chassiss.operation/v1",
  "target": "TASK-001"
}
```

Operation exact 字段：

- `schema`；
- `operation_id`；
- `action`；
- `project`；
- `authority`；
- `target`，Project/Authority Action 无 Task target 时为相应对象 ID；
- stable semantic `preconditions`；
- Action-specific `payload`。

Action-specific `preconditions` 也是 closed object：

| Action | Preconditions exact 字段 |
|---|---|
| `project.genesis` | 空 object |
| `architecture.updated` | `architecture_blob`、`taskbook`，后者必须为 null |
| `taskbook.opened` | `architecture_blob`、`taskbook`，后者必须为 null |
| `taskbook.updated` | `taskbook_blob` |
| `taskbook.archived` | `taskbook_blob`、`all_tasks_terminal=true` |
| `task.started` | `phase=ready`、`taskbook_blob`、`architecture_blob` |
| `task.released` | `phase=active`、`actor`、`base` |
| `task.blocked` | `phase`、`blocked=false` |
| `task.resumed` | `phase`、`blocked=true` |
| `task.submitted` | `phase=active`、`actor`、`base`、`taskbook_blob`、`architecture_blob` |
| `task.reviewed` | `phase`、`attempt_digest` |
| `task.cancelled` / `task.superseded` | `phase`、`attempt_digest`，后者可为 null |
| `integration.applied` | `phase=approved`、`attempt_digest`、`review_context_digest`、`review_report_digest` |
| `authority.grant-added` | `root_key_id`、`grant_absent=true` |
| `authority.grant-revoked` | `root_key_id`、`grant_id` |
| `owner.applied` | `no_active_agent_workflow=true`、`source_base`、`source_tree` |

`phase` 只允许 Action 状态图声明的 exact 值。Boolean sentinel 必须是 JSON
boolean，不接受 truthy string。表中没有的字段一律拒绝。

`expected_main`、next State、State digest、candidate tree、drift 和 Check
Results 不进入 Operation。Operation 使用 `operation` domain digest。

每次签名尝试另建：

```json
{
  "action": "task.submitted",
  "attempt": 1,
  "facts": {
    "submission_evidence": {}
  },
  "operation_digest": "sha256:...",
  "parent": "0123456789abcdef0123456789abcdef01234567",
  "schema": "chassiss.execution-evidence/v1"
}
```

Evidence exact 字段为 `schema`、`operation_digest`、`action`、`attempt`、
`parent`、`facts`。Genesis 的 `parent` 必须为 null，其他 Action 必须为 full
Git OID。`attempt` 从 1 开始；只有远端明确拒绝 CAS 且确认旧候选未发布后才能
递增，v1 只允许整数 `1..3`。历史 verifier 不要求前序失败 Evidence 存在于
Git。Evidence 使用 `execution-evidence` domain digest。

## 6. Transition message

Transition Commit message 使用以下固定结构：

```text
CHASSISS <action>

-----BEGIN CHASSISS OPERATION-----
<base64url-without-padding(JCS(operation))>
-----END CHASSISS OPERATION-----

-----BEGIN CHASSISS EXECUTION EVIDENCE-----
<base64url-without-padding(JCS(execution-evidence))>
-----END CHASSISS EXECUTION EVIDENCE-----

CHASSISS-Protocol: 1
CHASSISS-Action: <action>
CHASSISS-Project: <project-id>
CHASSISS-State-Digest: sha256:<lowercase-hex>
CHASSISS-Authority: root:<key-id>|grant:<grant-id>
CHASSISS-Operation-ID: <operation-id>
CHASSISS-Operation-Digest: sha256:<lowercase-hex>
CHASSISS-Evidence-Digest: sha256:<lowercase-hex>
CHASSISS-Task: <task-id>
```

`CHASSISS-Task` 只在 Task Action 中出现。其他字段全部必须且只能出现一次。
字段顺序固定。两个 block 都必须只有一个 base64url 行；解码后必须分别等于
Operation 和 Evidence 的 RFC 8785 canonical JSON bytes。

Commit message 不允许额外自由文本。原因、summary、Review Report、Finding
和 replacement Task 等内容必须位于 Operation payload；执行结果必须位于
Evidence，从而全部被同一个 Git SSH signature 绑定。

## 7. 文件变化白名单

| Action | 允许变化 |
|---|---|
| `project.genesis` | 初始完整 tree |
| `architecture.updated` | `docs/architecture.yaml`、`.chassiss/state.json` |
| `taskbook.opened` | `docs/taskbook.yaml`、`.chassiss/state.json` |
| `taskbook.updated` | `docs/taskbook.yaml`、`.chassiss/state.json` |
| `taskbook.archived` | 删除 `docs/taskbook.yaml`、新增唯一 archive path、`.chassiss/state.json` |
| `integration.applied` | approved Attempt 的 frozen `writes`、`.chassiss/state.json` |
| `owner.applied` | 普通项目文件；`.chassiss/state.json` 必须保持父 blob exact bytes |
| 其他 Transition | 仅 `.chassiss/state.json` |

任何 Transition 都禁止修改本地数据、private key 或 Git config。Integration
和 Owner Apply 禁止修改 Architecture、Taskbook、Taskbook archive 或其他
协议文件，即使 path scope 使用 `**`；Owner Apply 还禁止改变 State bytes。
`owner.applied` 必须满足第 15 节的静默工作流条件。

## 8. Reducer

每个 Action 定义：

```text
next_state =
  reduce(parent_state, verified_operation, verified_evidence, verified_git_facts)
```

Reducer 顺序：

1. 验证 Protocol、Project 和 parent；
2. 验证 commit signature 与 Authority；
3. 验证 Operation/Evidence schema、digest、action 和相互绑定；
4. 验证 Capability、Scope 和 Limits；
5. 解析父 State 指向或 Task frozen 的 exact Architecture/Taskbook blob；
6. 验证 Action stable preconditions；
7. 从 Git object 重算 changed paths、trees、heads、ancestry 和 Evidence facts；
8. 应用该 Action 唯一语义变化；
9. 删除新 phase 不允许的稀疏字段；
10. 验证全局 Task/Authority 不变量；
11. 生成 canonical State bytes；
12. 与 commit tree 中 State blob 逐字节比较；
13. 验证 State digest 与文件变化白名单。

Reducer 不接受 JSON Patch、用户提供的 next State、用户提供且不可重算的
Evidence facts 或“只验证字段 diff”模式。

## 9. Authority 取值

非 Genesis Action 始终依据父 State：

```text
root:<key-id>
grant:<grant-id>
```

候选 next State 中新增的 Root/Grant 不能授权产生自己的同一个 commit。

Root 只可执行 `authority.grant-added`、`authority.grant-revoked`，并可直接
执行 `task.superseded`。Root 也可以通过普通 Grant 获得其他 Task Capability；
此时 Authority 必须写对应 Grant。

## 10. Operation ID

Operation ID 使用：

```text
OPR- + 26 个 Crockford Base32 随机字符
```

不得依赖 wall clock 保证唯一性。

验证规则：

- first-parent history 已有相同 ID 和相同 Operation Digest：返回原结果；
- 相同 ID、不同 Operation Digest：`CHS_OPERATION_ID_COLLISION`；
- 不存在：可以执行。

同一稳定 Operation 可以在本地形成多份 Evidence 和候选 commit，但 main
history 最多接受其中一份。Operation ID 不进入 State。索引可以缓存，但权威
结果必须能从 history 重建。

## 11. CAS 与语义重试

Main 推进必须等价于：

```text
push new_main only if remote main == expected_parent
```

non-fast-forward 后 CLI：

1. fetch latest main；
2. 验证新增 history 与 local checkpoint；
3. 查询 Operation ID；
4. 确认旧 push 得到明确 CAS rejection；`push-unknown` 必须先按旧 candidate
   commit 对账；
5. 在最新父 State 上重新验证原 Semantic Operation；
6. 重新计算 Git facts、candidate tree、Checks 与 Execution Evidence；
7. Evidence `attempt` 加一，重新 reduce、创建并签名 Transition；
8. 再次 CAS push。

一次用户命令最多三次 CAS。任一 stable semantic precondition 不再成立时停止，
不得自动改 Semantic Operation、合并 State、force-push 或换 Operation ID。
Review/Integration 若新 parent 造成 relevant drift，必须停止并取得新的语义
Review，不能把它伪装成 Evidence retry。

`taskbook.archived` 使用更窄的 retry 规则：只有新增 history 全是 Authority
Transitions，且 ordinary project tree、Architecture/Taskbook blobs、terminal
Task projection 与 closing Integrations 均未改变，才可在新 parent 重跑
`workflow.checks` 并替换 Evidence。其他变化返回
`CHS_TASKBOOK_CLOSURE_STALE`，要求 Reviewer 重新确认 Closure Report。

## 12. Mainline 验证

从 pinned Genesis 或 verified checkpoint 向目标 main：

1. 验证目标是 checkpoint 的 descendant；
2. 沿 first-parent 顺序读取每个 Transition；
3. 检查 parent count 与 Action topology；
4. 解析唯一 Operation/Evidence blocks 和 Trailer；
5. 验证 Project、Protocol、Operation ID、Operation/Evidence digest 和绑定；
6. 使用父 State Root/Grant public key 验证 Git SSH signature；
7. 执行 Authorization 与 Reducer；
8. 检查 tree diff；
9. 接受新的 State 与 checkpoint。

first-parent 上出现未签名、未知 Action、未知协议、错误 State 或错误 topology
时，目标 head 无效。Verifier 只保留最后一个完整验证通过的 checkpoint。

## 13. Proposal ref

无法直接推进 main 的签名设备可以输出：

```text
refs/heads/chassiss/transition/<action>/<operation-id>
```

或包含该 ref 的 Git bundle。`transition publish` 只能在 proposal parent 仍等于
current verified main 时原样 fast-forward 发布。禁止 merge/rebase/squash/
cherry-pick。stale proposal 必须返回原签名设备重新生成。

## 14. Work 与 Archive ref

Work Ref 只允许 CLI fast-forward 更新。`submit` 自动确保 exact head 远程
可达。处于 submitted/approved 的 Task，其规范 Work Ref 必须解析为 State 中
的 exact Attempt Head；缺失或不一致时 verifier fail-stop。

Integration 成功后 exact Work Head 由 main second parent 保持可达，可以删除
Work Ref。cancelled/superseded 的已提交 Attempt 固定到不可变 Archive Ref，
v1 不自动删除 Archive Ref。Archive Ref 的末段使用 Attempt SHA-256 digest 的
64 lowercase hex，不包含 `sha256:`：

```text
refs/chassiss/archive/<task-id>/<attempt-hex>
```

创建 Archive 与推进 cancel/supersede main 必须使用一次 remote atomic push：

```text
create archive only if archive ref does not exist
and
update main only if main == expected parent
```

remote 不支持 atomic push 时拒绝带 current Attempt 的 cancel/supersede。
Verifier 必须为历史上每个这类终止 Action 重算 Attempt digest，并确认规范
Archive Ref 仍解析到 exact Attempt Head；缺失、被改写或对象不可达都使目标
main 无法完整验证。该规则提供检测和 fail-stop，不声称抵抗 remote 删除数据。

## 15. Owner Apply

`owner.applied` 是人工紧急修改普通项目内容的受限通道。它必须同时满足：

- 没有 active/submitted/approved Task；
- 没有 registered active/orphan worktree；
- 没有 unresolved pending Operation；
- source worktree cleanly snapshots 为一个 exact source tree；
- changed paths 不含 State、Architecture、active/archived Taskbook；
- 当前 Grant 具有 `owner.apply`，Task scope 为 `*`，Resource scope 为 `*`；
- CLI 显示 exact changed paths、candidate tree 和 reason，并要求交互确认或
  `--yes`。

Owner Apply 生成普通单 parent Transition，不允许 merge、rebase、force-push
或直接 commit。命令没有 source path 参数；当前目录必须解析到一个 registered、
non-managed Project worktree，CLI snapshot 该 worktree 的 repository root。
CLI 先把首次准备时的 current main 作为 `source_base=B`，把 snapshot 写成
source tree `S`，计算 `D=changed-paths(B,S)`，再用第 2.5 节的相同 exact
entry overlay 把 `D` 应用到 current parent `M`。State blob 保持父 State exact
bytes。若 CAS retry 时 `M` 在 `D` 上发生变化、出现 prefix collision 或产生
collateral change，返回 conflict，不自动覆盖。Owner Apply Evidence 内联
canonical changed-path array。Verifier 从 `source_base`、`D` 与 final
candidate entries 重建 source mapping 并确认 `source_tree` OID，因此不要求
不可达的 source tree object 被 remote 单独保留。Owner Apply 是 CLI workflow，
不恢复旧版自由 Owner baseline 行为。
