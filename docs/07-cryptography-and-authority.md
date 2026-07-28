# 密码学与 Authority 设计

## 1. 安全目标

CHASSISS v1 的密码学必须证明：

- 这是预期 Project；
- mainline 是已接受历史的后代，没有 remote rollback；
- Transition 由当时有权的 key 签发；
- 签名绑定 exact parent、tree、State、Semantic Operation、Execution Evidence
  和 Action；
- Agent 不能用候选 next State 给自己扩权；
- Review 绑定 exact Attempt、Context 与 candidate tree；
- Integration 绑定最终进入 main 的 exact tree。

它不证明：

- Actor 背后运行的模型或人类；
- Reviewer 实际使用的 Session/设备；
- wall-clock 时间；
- 外部 CI、Provider 或 Artifact Store 的真实性；
- private key 从未被复制。

## 2. 算法

v1 固定：

```text
Key algorithm       Ed25519
Public key format   OpenSSH ssh-ed25519
Commit signature    Git SSH signing
Digest              SHA-256
JSON canonical      RFC 8785 JCS
```

不允许 RSA、ECDSA、OpenPGP、X.509 或混合算法作为 v1 Authority key。

Public key 在 State 中使用：

```text
ssh-ed25519 <base64-key>
```

禁止尾部 comment。Fingerprint 使用 OpenSSH 形式：

```text
SHA256:<base64-without-padding>
```

## 3. Key 类型

### 3.1 Root key

Root 是 Project trust anchor。Root private key：

- 只在 Master 控制的 secret store、硬件设备或离线签发环境；
- 不写入 repo、State、Grant Request 或回执；
- 只用于 Genesis、Grant add/revoke 和紧急 supersede；
- 不为普通 Agent Action 逐次签名。

### 3.2 Agent key

Agent 本地生成项目专用 Ed25519 key pair：

```text
private key  local secret store
public key   Grant Request / State Grant
```

一个 Actor 可以有多把 key；每个 Grant 只绑定一把 key。Key ID 和 Grant ID
在 Project 历史中不复用。

同一 key fingerprint/Key ID 在一个 Project 历史中只能映射到一个 Actor。
同一 key 可以为该 Actor 获得多个 Grants，但不能换 Actor 名称制造独立身份；
每个 Action 必须在 Trailer 中选择一个 exact Grant。

### 3.3 Work Commit key

CLI Work Commit 使用当前 Task Actor key SSH-sign。该签名是 provenance，不是
State mutation authorization。权威 Attempt 仍由 `task.submitted` Transition
签名确定。

## 4. Identifier

协议 ID 使用 ASCII：

```text
Project ID      PRJ-<stable-token>
Taskbook ID     TASKBOOK-<stable-token>
Architecture ID ARCHITECTURE-<stable-token>
Task ID         TASK-<stable-token>
Requirement ID  REQ-<stable-token>
Constraint ID   CON-<stable-token>
Check ID        CHECK-<stable-token>
Grant ID        GRT-<stable-token>
Key ID          KEY-<stable-token>
Operation ID    OPR-<26 Crockford Base32 random chars>
```

`stable-token` 由大写 ASCII 字母、数字和单个 `-` 分隔组成，最长 64 字符。
Actor ID 使用小写 ASCII 字母、数字和单个 `-` 分隔，最长 64 字符。

所有 ID 区分大小写。ID 在 Project history 中不得复用。

## 5. Canonical digest

Digest 文本：

```text
sha256:<64 lowercase hex>
```

### 5.1 State

```text
state_bytes  = JCS(state) || LF
state_digest = SHA-256(state_bytes)
```

Digest 覆盖 State Git blob 的 exact bytes。

### 5.2 结构化对象

Semantic Operation、Execution Evidence、Attempt、Submission Evidence、
Review Context、Review Report、CheckSpec、Check Execution Context、Check
Results、Drift Classification 与 Taskbook Closure Report 使用：

```text
object_digest =
  SHA-256(
    UTF8("CHASSISS\0v1\0")
    || UTF8(object_type)
    || NUL
    || JCS(object)
  )
```

固定 `object_type`：

```text
operation
execution-evidence
attempt
submission-evidence
review-context
review-report
check-spec
check-execution-context
check-results
drift-classification
changed-paths
commit-range
taskbook-semantic-diff
architecture-semantic-diff
taskbook-closure-report
grant-request-body
grant-request
```

不同 object type 即使 JSON 内容相同也不能复用 digest。

### 5.3 Git object

Commit/tree/blob 使用 repository 自身 object format 的 OID。CLI 必须支持该
repository 已声明的 SHA-1 或 SHA-256 Git object format，不把 Git OID 伪装为
CHASSISS SHA-256 digest。

OID 文本使用与 repository object format 匹配的 40 或 64 位 lowercase hex，
不得使用 abbreviated OID。

## 6. Git SSH signature

Transition Commit 使用标准 Git SSH signing。签名覆盖 Git 规定的 commit
payload，包括：

```text
tree
parent(s)
author/committer metadata
commit message
Operation block
Execution Evidence block
CHASSISS Trailer
```

Verifier 不信任全局 Git `allowedSignersFile` 作为 Authority。它根据
`CHASSISS-Authority` 从父 State 选择 expected public key，在隔离 verifier
环境中验证 commit signature。

Commit author、committer、name、email 与 timestamp 不构成协议身份，也不参与
Grant 有效期判断。

v1 不增加第二层 Transition signature。

## 7. Genesis 与 trust bootstrap

Genesis 是零 parent commit，由其 State 声明的 Root public key 对应 private
key 自签。

自签只能证明 Genesis 内部一致，不能证明它是 Master 预期的 Project。首次
clone 必须从受信渠道获得：

```text
Project ID
Root fingerprint
minimum main checkpoint
remote location
```

CLI 只有同时验证 Project ID、Root fingerprint、checkpoint ancestry 和
signature 后才建立可 mutation 的本地 trust anchor。

缺少任一 out-of-band anchor 时只能使用显式 untrusted read-only 模式。

## 8. Grant Request

Grant Request 是 non-secret、non-authoritative 的 proof-of-possession 请求。
模板见 `templates/grant-request.json`。

Request canonical fields：

```text
schema
project_id
actor
key_id
public_key
requested_capabilities
requested_scope
requested_limits
nonce
proof
```

`proof` 是 Agent key 对不含 `proof` 字段的 `grant-request-body` domain
digest 的 32 raw bytes 做的 SSHSIG signature，namespace：

```text
chassiss-grant-request
```

`nonce` 使用 128-bit cryptographic random value 的 26 字符 Crockford Base32
编码，防止同一 public key Request 被误当成另一轮审批输入。

完整 Request（包含 proof）再使用 `grant-request` domain 计算 Request digest；
`authority.grant-added` 可以绑定该 digest。

Root CLI 必须验证 proof，但 Request 不产生权限。Master/Root 必须独立决定
最终 Actor、Capability、Scope 和 Limits。

## 9. Grant

Root 对 `authority.grant-added` Transition 签名，表达：

> 父 State Root 授权该 public key 以指定 Actor 身份，在指定 Scope/Limits 内
> 执行列出的 Capability。

Agent Action 有效当且仅当：

```text
commit signature verifies with Grant public key
Grant exists in parent State
Authority trailer selects that Grant
Grant actor matches the Action-specific actor matrix
Action maps to an exact Grant Capability
Task and Resource targets match Grant Scope
Grant/Task Limits pass
Reducer accepts Operation, Evidence and Git facts
```

Public key 证明持有 private key；Grant 才证明有权行动。

## 10. Scope

Capabilities 只允许 exact string，不允许 wildcard。

Task scope：

```text
TASK-001
TASK-*
*
```

Resource scope：

```text
module:core
module:*
api:*
schema:state
*
```

Scope pattern 只允许 exact、单个 terminal `*` prefix 或全局 `*`。禁止中间
wildcard、否定、正则和字符类。

Action 同时满足 Task 与相关 Architecture Resource scope 才能执行。

Action scope 计算：

- Task lifecycle、Review、Integration：Task ID 必须匹配 `scope.tasks`，Task
  Contract 的 `modules ∪ affects` 必须全部匹配 `scope.resources`；
- Taskbook open/update：所有新增/修改 ready Task 必须匹配 `scope.tasks`，
  Task 的 `modules ∪ affects` 必须匹配 `scope.resources`；修改无独立 ID 的
  workflow/Requirement/Constraint/extension 时两个 scope 都必须包含 `*`；
- Taskbook archive：两个 scope 都必须包含 `*`；
- Architecture update：全部 added/updated/removed Resources 必须匹配
  `scope.resources`；修改 overview/principles 时 Resource scope 必须包含 `*`；
- Resource `requires` closure 是只读依赖，不要求写 Scope，但仍参与冲突与
  drift 判断；
- Owner Apply：Task/Resource scope 都必须覆盖 `*`；
- Authority Action 由 Root 执行，不使用 Grant scope。

## 11. Limits

```text
mode=unbounded
```

表示没有额外数字限制，不绕过 Capability、Scope、Contract、Checks 或 Review。

```text
mode=bounded
max_active_tasks
max_changed_paths
```

bounded 至少包含一个正整数。Task `change_limits` 只能取更严格值。

`max_active_tasks` 统计同一 Actor 当前处于 active/submitted/approved 的 Tasks，
blocked Task 仍计入。`max_changed_paths` 统计 frozen base 到 exact Work Head
的唯一 changed paths。前者只在 `task.started` 执行，后者只在
`task.submitted` 执行；Review/Integration 仍重验原 Attempt/Contract facts，
不把 Reviewer/Integrator 的数字 limit 当成新的 Task budget。

## 12. Grant 激活与发现

Grant 只有在 Root-signed Transition 成为 verified main 或其 first-parent
祖先后生效。

Agent：

1. fetch main；
2. 验证 Genesis、Root、checkpoint 和 Transition chain；
3. 用本地 public key fingerprint 搜索 current Grants；
4. 确认 private-key handle 匹配；
5. 使用 Grant 签名获准 Action。

Root 无需返回权威 Credential。可选回执只包含 Grant ID/commit/digest，不能
替代 current State。

本地 Credential 的含义仅是：

```text
Project ID
Root fingerprint
minimum checkpoint
private-key handle
```

Grant object 与面向发现的 cached Grant ID/匹配结果不得持久化为本地
Credential 或 cache。CLI 每次从 current verified State 发现 exact Grant；
只有该 Git State 中的 current Grant 提供 Capability。Pending Semantic
Operation 为保持 crash recovery 和 Operation bytes 稳定，可以保存其 exact
`authority=grant:<grant-id>` 引用；该引用不提供离线 Authority。

## 13. 父 State 授权

候选 State 中新增 Grant 不能授权产生该 State 的同一个 Action。

```text
authorize(action_n) from state_(n-1)
reduce → state_n
```

撤销与 Agent Action 并发时，main CAS 顺序决定结果，不使用 wall clock：

```text
revoke first  → stale Agent Action revalidate 后失败
Agent first   → 当时合法，随后 revoke 从新 head 生效
```

## 14. Revocation

`authority.grant-revoked`：

- 必须由 current Root 签名；
- Semantic Operation 包含 Grant ID 和 reason；
- Reducer 从 next State 删除 Grant；
- 撤销历史留在 Git；
- Grant ID 不复用；
- 撤销不追溯否定此前合法 Transition。

Limit 或 Scope 调整通过新增 Grant 与撤销旧 Grant 完成。

## 15. Offline Root

Root CLI 可以：

1. 导入 non-secret Grant Request；
2. 导入包含 current main 的 Git bundle；
3. 完整验证 current State；
4. 构造并签名 Authority Transition；
5. 输出 Git bundle/Transition Ref；
6. 由联网 CLI `transition publish`。

Publisher 只验证并原样发布 exact signed commit。proposal parent stale 时不能
改写或代签，必须返回 Root 设备重新生成。

## 16. Anti-rollback

每个本地 Project 保存：

```text
Root fingerprint
minimum verified main checkpoint
checkpoint State digest
```

接受新 main 前必须验证：

- Project ID/Root fingerprint 相同；
- 新 main 是 checkpoint 的 descendant；
- 中间 first-parent history 全部有效。

remote 指向历史上签名合法但低于 checkpoint 的 commit 时，CLI 必须报 rollback
并拒绝 mutation。

一个 Project 的多个 local checkout 共享单调推进的 checkpoint。

## 17. Key storage

CLI 可以支持：

```text
OS keychain handle
SSH agent handle
hardware/KMS handle
encrypted local key-file handle
```

协议和 local state 只保存 opaque handle，不保存 private key bytes。

CLI 不得把 key path、PIN、token 或 secret 进入 Git、JSON response、日志或
Error details。

## 18. Root loss 与 compromise

v1 不提供 Root rotation、recovery quorum 或 retrospective compromise cutoff。

Root private key 丢失或必须更换时：

1. 停止旧 Project mutation；
2. 保持旧 Git history 只读；
3. 执行只读 export；
4. 以新 Project ID、新 Root 和新 Genesis 建立新 Project；
5. 重新签发全部 Grants。

这条边界牺牲便利性，以避免未经定义的信任根替换。

## 19. Threat response

| Threat | v1 response |
|---|---|
| Agent 伪报 Actor | signature 必须匹配 parent Grant |
| 候选 State 自我扩权 | parent State authorization |
| remote force-push rollback | local checkpoint ancestry |
| main 被写入非法 commit | full verifier 拒绝 |
| Work Ref force-push | CLI FF rule；Attempt exact head 复核 |
| Review 换实现 | exact Attempt/candidate digests |
| Integration 换 tree | signed double-parent Integration tree |
| push 响应丢失 | Operation ID reconciliation |
| private key 泄漏 | Root revoke；不追溯历史 |
| Root key 丢失 | read-only export + new Genesis |

## 20. 授权快捷模板

快捷模板是 Root CLI 的 UX 展开规则，不是协议角色。State、Grant、Operation
和 verifier 只看展开后的 exact Capability/Scope/Limits，不保存 profile 名称。

v1 标准 `developer` 模板展开为：

```json
{
  "capabilities": [
    "task.block",
    "task.cancel",
    "task.release",
    "task.resume",
    "task.start",
    "task.submit"
  ],
  "limits": {
    "max_active_tasks": 1,
    "mode": "bounded"
  }
}
```

使用模板时仍必须显式给出 Task Scope 与 Resource Scope；CLI 不默认为 `*`。
Root 在签名前必须展示完整展开 Grant，并可以删除 Capability、收紧 Scope 或
Limits，禁止静默增加模板外权限。`--profile developer` 与手工输入上述
Capability 产生完全相同的 Grant bytes；future template 只能新增名称，修改
既有模板展开必须随 CLI release identity 明确版本化。
