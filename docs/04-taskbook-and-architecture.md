# Taskbook 与架构文档规范

## 1. 两类静态合同

Project 使用两个协议级 YAML 合同：

```text
docs/architecture.yaml
docs/taskbook.yaml
```

Architecture 跨多轮需求持续存在，承载：

```text
Architecture overview/principles
Module/API/Schema/Dependency/Config Views
Resource Graph
```

Taskbook 只承载当前一轮完整工作流：

```text
Workflow outcome
Workflow closure Checks
Requirements
Constraints
Task graph
Task Contracts
Checks
Review attention
Completion criteria
```

一轮全部 Tasks terminal 后，由具有 `taskbook.archive` 的 Reviewer 验收每个
Task disposition 与整体
completion criteria；CLI 运行并签名工作流级机械 Checks 后，把活动文件归档到：

```text
docs/taskbooks/archive/<taskbook-id>.yaml
```

然后下一轮需求创建新的 `docs/taskbook.yaml`。不建立独立 Requirement、
Mission 或 Task 协议文件。普通项目文档可以存在，但不能替代 Architecture 或
Taskbook 中参与验证的结构化事实。

模板见 `templates/architecture.yaml` 与 `templates/taskbook.yaml`。

## 2. 编码约束

Architecture 与 Taskbook 都使用 UTF-8 YAML 1.2 Core subset：

- LF line ending；
- 禁止 BOM；
- mapping key 必须是 string；
- 禁止 duplicate key；
- 禁止 anchor、alias、merge key、自定义 tag 和 complex key；
- 禁止 float 和隐式 timestamp；
- 允许 string、integer、boolean、null、sequence 和 mapping；
- integer 必须是十进制且位于
  `[-9007199254740991, 9007199254740991]`；
- comment 可以存在，但 exact blob identity 会随 comment 改变；
- 未知 core 字段拒绝；
- 仅允许顶层 `extensions` 保存 namespaced、非权威扩展。

两者都不要求 canonical YAML serialization。exact 版本身份是 Git blob OID；
所有实现必须对允许的 YAML subset 得到相同数据模型。

以下 array 按 set 解释，禁止 duplicate，协议计算前按 UTF-8 byte order 排序：

```text
Requirement/Constraint/Task ID references
modules
depends_on
writes
affects
supersedes
Resource requires
Resource paths
```

以下 array 保持文档顺序：

```text
completion_criteria
acceptance
principles
deliverables
checks
out_of_scope
stop_conditions
reviewer_attention
```

## 3. Taskbook 顶层结构

```yaml
schema: chassiss.taskbook/v1
id: TASKBOOK-001
workflow: {}
requirements: {}
constraints: {}
tasks: {}
extensions: {}
```

| 字段 | 必须 | 语义 |
|---|---|---|
| `schema` | 是 | 固定 `chassiss.taskbook/v1` |
| `id` | 是 | 本轮 Taskbook stable ID；Project history 内不复用 |
| `workflow` | 是 | 本轮 title/outcome/completion criteria/closure checks |
| `requirements` | 是 | stable Requirement map，可为空 |
| `constraints` | 是 | stable Constraint map，可为空 |
| `tasks` | 是 | stable Task Contract map |
| `extensions` | 否 | namespaced 非权威数据，不参与 Reducer |

`extensions` key 使用反向域名或组织 namespace，例如
`org.example/planner-metadata`。Unknown extension 可忽略，且不能改变
Authorization、Reducer、Context 必需字段、冲突或 Review 结果。

## 4. Workflow

```yaml
workflow:
  title: Example
  outcome: |-
    本轮完成后的可观察结果。
  completion_criteria:
    - 所有必须交付物可验证。
  checks:
    - id: CHECK-WORKFLOW-001
      argv:
        - chassiss
        - verify
        - --full
      cwd: .
      timeout_seconds: 300
```

`outcome` 描述本轮成功后的外部结果，不描述执行步骤。
`completion_criteria` 是本轮整体验收条件，不替代每个 Task 的 deliverables。
`checks` 是 Taskbook 归档前在 exact current main 上执行的工作流级机械检查，
必须非空。Workflow 是 closed object，只允许 `title`、`outcome`、
`completion_criteria`、`checks`；前三者必须非空。

## 5. Requirements

```yaml
requirements:
  REQ-001:
    title: Deterministic verification
    statement: |-
      相同 Git 输入必须产生相同验证结论。
    acceptance:
      - 两个独立 CLI 实现通过同一测试向量。
```

Requirement 字段：

| 字段 | 必须 |
|---|---|
| `title` | 是 |
| `statement` | 是 |
| `acceptance` | 是，可为空 |

Requirement ID 在 Project history 中不复用。同一活动 Taskbook 更新期间不
改变既有含义或删除 entry；下一轮语义变化创建新 ID。

Task 通过 `requirements` 数组选择自己的适用 Requirements，CLI 只把这些条目
加载进普通 Task Context。

## 6. Constraints

```yaml
constraints:
  CON-001:
    title: No platform dependency
    rule: |-
      核心协议不得依赖托管平台 API。
```

Constraint 字段：

| 字段 | 必须 |
|---|---|
| `title` | 是 |
| `rule` | 是 |

Task 通过 `constraints` 数组选择适用约束。真正全局且每个 Task 都必须遵守的
约束应由每个 Task 显式引用，避免隐藏的上下文继承。

Constraint ID 在 Project history 中不复用。同一活动 Taskbook 更新期间不
改变既有含义或删除 entry；下一轮语义变化创建新 ID。

## 7. Architecture

```yaml
schema: chassiss.architecture/v1
id: ARCHITECTURE-001
overview: |-
  系统整体边界和关键数据流。
principles:
  - Git 是唯一共享真相源。
modules: {}
apis: {}
schemas: {}
dependencies: {}
configs: {}
extensions: {}
```

Architecture 顶层只允许以上字段。`id` 在 Project 生命周期内固定。Module
View 必须存在且至少包含一个节点，其他四个 View 可为空。`extensions` 与
Taskbook extension 使用相同的非权威规则。

### 7.1 Resource ID

View mapping key 与类型组合成 typed Resource ID：

```text
module:<key>
api:<key>
schema:<key>
dependency:<key>
config:<key>
```

Resource key 使用小写 ASCII 字母、数字和单个 `-` 分隔。Resource ID 在
Project history 中不复用。`architecture.updated` 与
`architecture.updated-compatible` 可以增加、修改或删除当前 Resource；旧语义
由历史 Architecture blob 保留，删除的 ID 后续不得复用。

### 7.2 Module View

```yaml
modules:
  core:
    title: Core
    description: |-
      协议验证与 Reducer。
    paths:
      - src/core/**
      - tests/core/**
    requires:
      - schema:state
```

Module 字段：

| 字段 | 必须 | 语义 |
|---|---|---|
| `title` | 是 | 人类可读名称 |
| `description` | 是 | 责任、边界与关键不变量 |
| `paths` | 是，非空 | Module path coverage |
| `requires` | 是，可为空 | 依赖的 typed Resource IDs |

简单项目使用：

```yaml
modules:
  root:
    title: Project
    description: |-
      整个项目。
    paths:
      - "**"
    requires: []
```

### 7.3 API、Schema、Dependency、Config View

非 Module Resource 使用统一字段：

| 字段 | 必须 | 语义 |
|---|---|---|
| `title` | 是 | 人类可读名称 |
| `description` | 是 | 语义、兼容边界和使用约束 |
| `owner` | 否 | `module:<id>` |
| `paths` | 是，可为空 | 定义或实现该 Resource 的 paths |
| `requires` | 是，可为空 | 上游 Resource IDs |

View 应分别描述：

| View | `description` 应覆盖 |
|---|---|
| API | 调用方、输入输出、错误和兼容边界 |
| Schema | 数据结构、持久化/消息语义和演进约束 |
| Dependency | 用途、版本/运行约束和依赖方 |
| Config | 配置语义、默认行为、消费者和安全边界 |

CLI 不理解 OpenAPI、SQL、Protobuf 或包管理器的专有语法；项目通过这些统一
节点表达语义。

## 8. Resource Graph

所有 `requires` 边形成一张有向无环图：

```text
node → node 所依赖的上游资源
```

必须满足：

- 所有引用存在；
- owner 指向存在的 Module；
- 无 self edge；
- 全图无环；
- Resource key/ID 唯一；
- Module View 非空。

`owner` 不自动产生 `requires` 边。

CLI 必须提供：

```text
show node
requires closure
required-by closure
impact query
validation
```

## 9. Path grammar

Architecture `paths` 与 Task `writes` 只允许：

```text
repo/relative/file
repo/relative/directory/**
**
```

禁止：

```text
absolute path
..
.
negative pattern
regex
brace
character class
中间 wildcard
```

Path 使用 `/`，并遵守：

- 必须是有效 UTF-8 且已经是 Unicode NFC；禁止非法 UTF-8 byte sequence；
- 禁止 NUL、ASCII control、反斜杠、空 segment、前导/尾随 `/` 和 `//`；
- `repo/relative/file` 只匹配 exact path；
- `repo/relative/directory/**` 只匹配该目录的非空 descendant，不匹配同名
  file；
- `**` 匹配全部普通 repo-relative paths；
- 实现不得按 locale、filesystem case-fold 或 Unicode normalization 隐式改名；
- 排序和 digest 使用 NFC 字符串的 UTF-8 bytes。

exact 与 prefix scope 的相交、包含和 changed-path matching 必须只用上述字符串
规则计算。Git tree 含不满足编码规则的 path 时 verifier 可以读取 object，但
必须拒绝任何会把该 path 纳入 CHASSISS changed paths、Context 或 mutation 的
Action，并返回 `CHS_PATH_ENCODING_INVALID`。

`.chassiss/state.json`、`docs/architecture.yaml`、`docs/taskbook.yaml` 与
`docs/taskbooks/archive/**` 是受保护路径，即使 scope 为 `**`，普通 Work
Commit 也不能修改。

Actual changed paths 从 frozen base tree 与 Work Head tree 递归比较：

- 不启用 Git rename/copy heuristic；
- add/delete/modify/type-change 各把该 repo-relative path 加入 set；
- rename 表示 old path delete + new path add；
- submodule entry 变化按其所在 path 计一个；
- 结果按 UTF-8 byte order 排序、去重。

`max_changed_paths` 与 `changed_paths_digest` 使用这一个算法。

### 9.1 Task path coverage

Task `writes` 是最终写权限，不要求被 Module paths 完整包含；但每一个 `writes`
scope 必须与该 Task 至少一个 selected Module 的 `paths` scope 相交。相交足以
表明 Architecture 入口与工作范围有关，同时允许一个 Task 有意跨 Module
边界。`writes: []` 合法并使本规则 vacuously true。

Module paths 不自动扩大 `writes`。例如 Module 为 `src/core/**`、Task writes
为 `src/core/state.go` 时只允许后者；Task writes 为 `**` 虽与 Module 相交，
仍必须经过显式 Taskbook Review，并会受到 Grant Scope、conflict 与
`max_changed_paths` 约束。

## 10. Task Contract

```yaml
TASK-001:
  title: Implement verifier
  goal: |-
    产生可验证的协议验证器。
  requirements:
    - REQ-001
  constraints:
    - CON-001
  deliverables:
    - Verifier implementation
  modules:
    - module:core
  depends_on: []
  writes:
    - src/core/**
    - tests/core/**
  affects:
    - schema:state
  checks:
    - id: CHECK-001
      argv:
        - go
        - test
        - ./...
      cwd: .
      timeout_seconds: 120
  change_limits:
    max_changed_paths: 40
  out_of_scope: []
  stop_conditions: []
  reviewer_attention: []
  supersedes: []
```

字段：

| 字段 | 必须 | 语义 |
|---|---|---|
| `title` | 是 | 简短名称 |
| `goal` | 是 | 单一、可观察结果 |
| `requirements` | 是，可为空 | 适用 Requirement IDs |
| `constraints` | 是，可为空 | 适用 Constraint IDs |
| `deliverables` | 是，可为空 | 文件、行为或验证结果 |
| `modules` | 是，非空 | Task 架构入口 |
| `depends_on` | 是，可为空 | 必须先 closed 的 Task IDs |
| `writes` | 是，可为空 | 实际允许修改 paths |
| `affects` | 是，可为空 | 会改变的 typed Resource IDs |
| `checks` | 是，可为空 | frozen CheckSpec |
| `change_limits` | 否 | 只能进一步收紧 Grant |
| `out_of_scope` | 是，可为空 | 明确排除 |
| `stop_conditions` | 是，可为空 | 必须停止并重新规划的条件 |
| `reviewer_attention` | 是，可为空 | 语义 Review 重点 |
| `supersedes` | 是，可为空 | 被本 Task 替换的旧 Task IDs |

Task ID 不删除、不复用。只有 `ready` Task 可以保持同 ID 更新。

## 11. CheckSpec

```yaml
- id: CHECK-001
  argv:
    - go
    - test
    - ./...
  cwd: .
  timeout_seconds: 120
```

| 字段 | 规则 |
|---|---|
| `id` | Taskbook 内唯一；Workflow 与全部 Task Checks 共用同一命名空间 |
| `argv` | 非空 string array；禁止 shell string |
| `cwd` | repo-relative directory |
| `timeout_seconds` | 正整数 |

v1 CheckSpec 没有 env、shell、CI provider、sandbox policy 或 external artifact。
Secret/env 配置属于本地执行环境，不得进入协议对象。它可能影响真实执行结果，
因此 Check Result 只是一份签名 execution attestation；历史 verifier 不声称
重现执行环境。

`workflow.checks` 必须至少包含一个 CheckSpec；Task `checks` 可以为空。两类
CheckSpec 使用相同 schema，但执行阶段和绑定对象不同，结果不能跨阶段复用。

`cwd: .` 明确表示 Task worktree root；其他 cwd 不允许绝对路径或 `..`。

所有 core timeout/limit 数值必须位于 `[1, 2147483647]`。

## 12. Limits

Task 可声明：

```yaml
change_limits:
  max_changed_paths: 20
```

Taskbook 只能收紧 Grant。有效 limit 取存在的最小值。Taskbook v1 不设置
`max_active_tasks`，因为并发信任属于 Grant。

## 13. Task dependency

- dependency target 必须存在；
- Task 不能依赖自身；
- dependency graph 必须无环；
- 只有 `closed` 满足依赖；
- cancelled/superseded 不满足；
- `approved` 尚未进入权威代码，不满足。

## 14. 混合冲突算法

对 Task `T`：

```text
P(T) = writes path scopes
W(T) = affects resources
R(T) = requires*(modules(T) ∪ W(T)) - W(T)
```

`requires*` 是包含起点自身的 reflexive transitive closure。因此
`affects: [module:x]` 会与任何把 `module:x` 作为工作上下文的 Task 冲突。

非终态 Task `A`、`B` 在以下任一条件成立时冲突：

```text
P(A) intersects P(B)
W(A) intersects W(B)
W(A) intersects R(B)
R(A) intersects W(B)
```

读读重叠不冲突。Module membership 本身不是锁；要独占 Module，必须把
`module:<id>` 放入 `affects`。

active/submitted/approved Task 即使 blocked 也继续占用资源。ready（包括
blocked ready）、closed、cancelled、superseded 不占用。

## 15. Initial Architecture establish

已有项目的 `project.bootstrap` 可以暂时令 Architecture 为 null。Root 审核
并发布 `architecture.establish` Grant 后，Architecture Agent 从项目外候选执行
`architecture.established`：

1. parent 必须是 source bootstrap，Architecture/Taskbook 均为 null；
2. 候选必须通过完整 YAML、closed schema、Resource Graph 与 path 校验；
3. Operation target 必须等于候选的 Architecture ID；
4. Grant 必须包含 `architecture.establish`、全局 Task scope 和全局 Resource
   scope；
5. exact candidate blob 写入 `docs/architecture.yaml` 和 State；
6. Source anchor 与 `docs/chassiss/onboarding/source-history.md` 保持不变；
7. Transition 完成后才允许 `taskbook.opened`。

CLI 的机械校验不证明 Module/API/Schema/Dependency/Config 描述在语义上准确；
首次 Architecture 必须由人类或独立 Reviewer 对照 adopted source snapshot
复核。

## 16. Architecture update

Architecture 更新保留两个不可互换的历史 Action：

- `architecture.updated` 是既有合同，只允许 `project.taskbook=null`；其
  Preconditions exact 字段为 `architecture_blob`、`taskbook=null`，Evidence
  facts exact 字段为 `old_blob`、`new_blob`、`semantic_diff`。
- `architecture.updated-compatible` 是 additive RC10 Action，只允许活动
  Taskbook 存在；其 Preconditions exact 字段为
  `all_tasks_quiescent=true`、`architecture_blob`、`taskbook_blob`，Evidence facts
  exact 字段为 `old_blob`、`new_blob`、`semantic_diff`、`taskbook_blob`。

两种 Action 都必须：

1. 接受 source repo 外候选 `architecture.yaml`；
2. 验证 YAML subset、closed schema、Resource Graph、paths 和 stable IDs；
3. 计算 canonical Architecture Semantic Diff；
4. 确认调用 Grant 具有 `architecture.update`，且所有 added/updated/removed
   Resources 都匹配 `scope.resources`；
5. 同时写入新 Architecture blob 与 State projection；
6. 只允许 `docs/architecture.yaml` 与 `.chassiss/state.json` 变化；
7. 生成签名 Transition 并 CAS push。

兼容更新还必须满足以下全部条件：

1. 每个 Task phase 都是 `ready|closed|cancelled|superseded`；blocked-ready 仍是
   静默，blocked-active 仍是 in-flight；
2. candidate Architecture 能完整解析 exact active Taskbook blob，包括所有
   Resource references、path coverage、Task DAG、Workflow 与 Check 合同；
3. Taskbook blob、Taskbook ID、Task projection 和普通项目文件保持 exact；只有
   Architecture blob 与 State 中对应投影改变；
4. 已开始、提交或批准的 Task 继续由它在 start 时冻结的
   Architecture/Taskbook blobs 验证，历史 Transition 继续按其原 Action schema
   验证，不被 RC10 重新解释。

Architecture candidate 文件与 `.chassiss.json` sidecar 写在项目外。sidecar 必须
同时绑定 draft 时的 Project、Architecture blob 和 Taskbook blob（无活动
Taskbook 时为 null）；任一 binding stale 都在选择 Authority 或创建 Operation
之前拒绝，错误响应 `operation=null`，main 保持不变。

Architecture Semantic Diff exact object：

```json
{
  "added_resources": [],
  "extensions_changed": false,
  "new_blob": "<blob>",
  "old_blob": "<blob>",
  "overview_changed": false,
  "principles_changed": false,
  "removed_resources": [],
  "schema": "chassiss.architecture-diff/v1",
  "updated_resources": []
}
```

所有 Resource arrays 规范排序、去重。调用者修改 overview/principles 时，
Grant `scope.resources` 必须包含 `*`。删除的 Resource ID 永不复用。

兼容更新的 CAS retry 不是复合 Architecture+Taskbook 事务。若 latest main 仅有
纯 State/Authority drift，CLI 可以在重验 quiescence 与 Taskbook compatibility
后，用同一 Semantic Operation 生成下一 Evidence attempt。若 Architecture、
Taskbook 或普通项目 tree 已变化，或者任何 Task 进入
`active|submitted|approved`，必须分别以 stale、candidate conflict 或
`CHS_TASKBOOK_NOT_QUIESCENT` fail closed；不得覆盖、合并、顺带更新 Taskbook，
也不得更换 Operation ID。Architecture Transition 发布后才形成新的共享边界，
因此调用方必须重新读取 Context，再单独起草任何 Taskbook update。

## 17. Taskbook open/update

`taskbook.opened` 只允许 `project.taskbook=null`。它接受一个 source repo 外
候选，要求全新 Taskbook/Requirement/Constraint/Task IDs，使用 current
Architecture 验证全部资源引用和 Task path coverage，并把全部 Tasks 以 ready
加入 State。

`taskbook.updated` 必须：

1. 接受一个源仓库外候选 YAML；
2. 验证 YAML subset 与 schema；
3. 验证 Requirement/Constraint/Task IDs；
4. 使用 State 指向的 current Architecture 验证 Resource references 和 Task DAG；
5. 验证 Task path coverage；
6. 确认现有非-ready Task Contract 未改变或删除；
7. 确认已使用 ID 没有改变含义；
8. 确认调用者有 `taskbook.update`；
9. 同时写入新 Taskbook blob 与新 State projection；
10. 生成签名 Transition 并 CAS push。

Taskbook Semantic Diff canonical object：

```json
{
  "added_constraints": [],
  "added_requirements": [],
  "added_tasks": [],
  "extensions_changed": false,
  "new_blob": "<blob>",
  "old_blob": "<blob>",
  "schema": "chassiss.taskbook-diff/v1",
  "updated_ready_tasks": [],
  "workflow_changed": false
}
```

所有 ID array 规范排序、去重。该对象使用 `taskbook-semantic-diff` domain
digest，写入 `taskbook.updated` Execution Evidence。任何 removed ID、
非-ready Task 更新或既有 Requirement/Constraint 语义更新必须拒绝。
一旦任一 Task 离开 ready，`workflow_changed` 必须为 false；本轮 outcome 与
completion criteria 及 workflow checks 从第一次 start 起冻结。

Taskbook Action scope：

- `taskbook.opened`：所有 Tasks 必须匹配 `scope.tasks`；所有
  `modules ∪ affects` 必须匹配 `scope.resources`；
- `taskbook.updated`：全部新增/修改 ready Tasks 使用同一规则；
- workflow/Requirement/Constraint/extension 的新增或修改不具有独立
  Resource ID，因此要求 Grant 的 Task scope 与 Resource scope 都包含 `*`。

stale candidate 不自动 merge。CLI 必须返回 current Taskbook/Architecture blob
和差异，由 Planner 重新生成候选。

## 18. Taskbook completion 与 archive

`taskbook.archived` 必须：

1. 当前 Taskbook 的每个 Task 都是 `closed|cancelled|superseded`；
2. signer Grant 具有 `taskbook.archive`，Task/Resource scope 都包含 `*`；
3. Operation 内联一个不超过 65,536 bytes 的 canonical
   `chassiss.taskbook-closure-report/v1`；
4. Report 为每个 Task 按 Task ID byte order 提供 `task`、exact terminal
   `phase` 和非空 `response`，并为每条 `workflow.completion_criteria` 按原顺序提供
   `criterion`、`status=pass`、`response`；
5. 在 isolated clean checkout 中，对归档 Transition 的 exact parent
   `head/tree` 按文档顺序执行全部 `workflow.checks`；每个 Check 都使用
   `phase=workflow-closure` 的独立 Check Execution Context，且必须 pass；
6. Check 后再次确认 parent `head/tree` 未改变；
7. Execution Evidence 绑定 Taskbook/Architecture blob、每个 closed Task 的
   closing Integration commit、全部 terminal phases、archive path/blob 与
   全部 workflow Check Results；
8. 使用 Reducer 验证 all-terminal projection、Context/Result binding 与
   all-pass 结论；
9. 原样删除 `docs/taskbook.yaml` 并以相同 blob 新增
   `docs/taskbooks/archive/<taskbook-id>.yaml`；
10. 清空 State tasks 并令 `project.taskbook=null`。

Archive path 是 create-only；Project history 中已使用的 Taskbook ID/path 不得
覆盖。Closure Report 可以含按顺序排列的 advisory findings，但 blocking
finding 或任一 criterion 非 pass 必须拒绝 archive。

Closure Report exact object：

```json
{
  "completion_criteria_responses": [
    {
      "criterion": "Every task has a Reviewer-accepted terminal disposition.",
      "response": "Verified from the current State.",
      "status": "pass"
    }
  ],
  "findings": [],
  "schema": "chassiss.taskbook-closure-report/v1",
  "summary": "The workflow outcome is accepted.",
  "task_responses": [
    {
      "phase": "closed",
      "response": "The integrated result is accepted.",
      "task": "TASK-001"
    }
  ],
  "taskbook": "TASKBOOK-001"
}
```

每个 finding exact 字段为 `severity`、`summary`、`paths`、`resources`；
`severity` 只允许 `blocking|advisory`，paths/resources 规范排序、去重。所有
string 必须非空且 Report 的 JCS bytes 不超过 65,536。未知字段拒绝。
`task_responses` 必须恰好覆盖 State 中全部 Tasks；它允许 Reviewer 明确接受
cancelled/superseded disposition，但不能跳过或伪报 phase。对这两种 disposition
不强制 replacement Task、补做或转成 closed；Reviewer 的签名非空 response
就是协议所需的最终确认。整体 outcome、completion criteria 或任一 workflow
Check 未达成时仍必须拒绝 archive；语义缺口使用 blocking finding，机械检查
失败使用对应 Check Result。Taskbook 若显式写入更严格的 completion criterion，
仍按本轮合同执行；协议本身不隐含该额外要求。
Evidence `terminal_tasks` 是 Task ID → terminal phase 的 canonical mapping；
`closing_integrations` 是所有且仅有 closed Task ID → closing Integration
commit 的 mapping。`check_results` 按 `workflow.checks` 文档顺序排列。
三者 unknown/missing/extra entry 都拒绝。

Archive CAS 失败后，只在新 first-parent history 全部是 Authority
Transitions，且 ordinary project tree、Architecture blob、Taskbook blob、
terminal Task projection 与 closing Integrations 均未改变时，CLI 才可在新
parent 上重新运行 workflow Checks、替换 Execution Evidence 并继续同一
Semantic Operation。任何 ordinary path、Architecture/Taskbook、Task
disposition 或 closure-relevant fact 变化都返回
`CHS_TASKBOOK_CLOSURE_STALE`，要求 Reviewer 重新确认；不得把旧 Closure
Report 当成对新成果的批准。`push-unknown` 仍必须先对账，不能直接换 Evidence。
Workflow Check 的 fail/error 在签名或 push archive Transition 前终止命令，
不改变 main、Taskbook 或 State。

## 19. Frozen Contract

`task.started` 同时冻结当前 Taskbook 与 Architecture blob：

```text
task.contract.taskbook_blob
task.contract.architecture_blob
```

effective Contract 始终为：

```text
parse(frozen_taskbook_blob).tasks[current_task_id]
parse(frozen_architecture_blob)
```

后续当前合同变化不改变 active/submitted/approved Task。Architecture 只允许在
没有活动 Taskbook 时更新，因此正常工作流不会跨 Architecture version；双
blob freeze 仍用于完整历史验证。

## 20. Context retrieval

普通 Task Context 只返回：

```text
Workflow outcome
selected Requirements
selected Constraints
Task Contract
selected Modules
affects Resources
requires closure
depends_on phases
current Task runtime State
available actions
```

Taskbook/Architecture Agent 可以按 stable ID 查询任意节点、闭包或 impact。

结果必须由 exact Taskbook/Architecture blobs 和显式 graph 确定生成。向量
搜索可以作为未来辅助，但不能参与 v1 授权、冲突、Review 或 Integration。
