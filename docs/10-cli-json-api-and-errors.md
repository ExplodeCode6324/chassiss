# CLI JSON API 与 Error Contract

## 1. Response envelope

所有命令 `--json` 返回一个 object：

```json
{
  "available_actions": [],
  "command": "task start",
  "error": null,
  "extensions": {},
  "identity": null,
  "ok": true,
  "operation": null,
  "project": null,
  "result": {},
  "schema": "chassiss.cli/v1",
  "snapshot": null,
  "warnings": []
}
```

字段始终存在；不适用时使用 `null`、空 array 或空 object，避免调用者通过字段
缺失猜测 command 类型。

Human output 必须从同一 response object 渲染。

## 2. Project

```json
{
  "id": "PRJ-EXAMPLE",
  "protocol": "chassiss/v1",
  "root_fingerprint": "SHA256:..."
}
```

`root_fingerprint` 只显示 public fingerprint。

## 3. Snapshot

```json
{
  "architecture_blob": "<blob>",
  "main_commit": "<commit>",
  "offline": false,
  "state_digest": "sha256:...",
  "taskbook_blob": "<blob-or-null>",
  "trust": "verified"
}
```

`trust`：

```text
verified
untrusted
invalid
rollback
```

Mutation 只允许 `verified` 且 `offline=false`。

## 4. Identity

```json
{
  "actor": "agent-builder-1",
  "capabilities": [
    "task.start",
    "task.submit"
  ],
  "grant_id": "GRT-BUILDER-01",
  "key_fingerprint": "SHA256:...",
  "key_id": "KEY-BUILDER-01",
  "limits": {},
  "scope": {}
}
```

只显示 current verified Grant。Private-key handle 默认不进入 response。

没有匹配 Grant 时 `identity=null`。

## 5. Operation

Mutation success：

```json
{
  "commit": "<transition-commit>",
  "evidence_attempt": 1,
  "evidence_digest": "sha256:...",
  "operation_digest": "sha256:...",
  "operation_id": "OPR-...",
  "status": "published"
}
```

可能 status：

```text
prepared
proposal
published
reconciled
```

Read-only command 使用 `operation=null`。

## 6. Available Action

```json
{
  "action": "task.started",
  "argv": [
    "chassiss",
    "task",
    "start",
    "TASK-001",
    "--json"
  ],
  "capability": "task.start",
  "target": "TASK-001"
}
```

`available_actions` 只包含在当前 snapshot 看来可执行的 Action。最终 mutation
仍会重新 sync/verify。

不能执行但对用户有价值的 Action 放在 `result.blocked_actions`：

```json
{
  "action": "task.started",
  "reasons": [
    {
      "code": "CHS_DEPENDENCY_UNSATISFIED",
      "details": {}
    }
  ],
  "target": "TASK-002"
}
```

## 7. Success envelope

```json
{
  "available_actions": [],
  "command": "task start",
  "error": null,
  "extensions": {},
  "identity": {
    "actor": "agent-builder-1",
    "capabilities": [
      "task.start"
    ],
    "grant_id": "GRT-BUILDER-01",
    "key_fingerprint": "SHA256:...",
    "key_id": "KEY-BUILDER-01",
    "limits": {},
    "scope": {}
  },
  "ok": true,
  "operation": {
    "commit": "<commit>",
    "evidence_attempt": 1,
    "evidence_digest": "sha256:...",
    "operation_digest": "sha256:...",
    "operation_id": "OPR-...",
    "status": "published"
  },
  "project": {
    "id": "PRJ-EXAMPLE",
    "protocol": "chassiss/v1",
    "root_fingerprint": "SHA256:..."
  },
  "result": {
    "task": "TASK-001",
    "worktree": "/local/path"
  },
  "schema": "chassiss.cli/v1",
  "snapshot": {
    "architecture_blob": "<blob>",
    "main_commit": "<commit>",
    "offline": false,
    "state_digest": "sha256:...",
    "taskbook_blob": "<blob>",
    "trust": "verified"
  },
  "warnings": []
}
```

## 8. Error envelope

```json
{
  "available_actions": [],
  "command": "submit",
  "error": {
    "category": "validation",
    "code": "CHS_SCOPE_VIOLATION",
    "current_head": "<commit>",
    "details": {
      "paths": [
        "outside/file"
      ]
    },
    "message": "Work tree contains paths outside the frozen Task Contract.",
    "operation_id": "OPR-...",
    "remediation": [
      {
        "argv": [
          "chassiss",
          "work",
          "diff",
          "TASK-001",
          "--json"
        ],
        "description": "Inspect the exact task diff."
      }
    ],
    "retryable": false
  },
  "extensions": {},
  "identity": null,
  "ok": false,
  "operation": null,
  "project": {
    "id": "PRJ-EXAMPLE",
    "protocol": "chassiss/v1",
    "root_fingerprint": "SHA256:..."
  },
  "result": {},
  "schema": "chassiss.cli/v1",
  "snapshot": {
    "architecture_blob": "<blob>",
    "main_commit": "<commit>",
    "offline": false,
    "state_digest": "sha256:...",
    "taskbook_blob": "<blob>",
    "trust": "verified"
  },
  "warnings": []
}
```

Error 固定字段：

| 字段 | 语义 |
|---|---|
| `code` | 稳定 machine-readable code |
| `message` | 人类可读、不得含 secret |
| `category` | Error 分类 |
| `retryable` | 原 Semantic Operation 在外部条件变化后是否可能成功 |
| `current_head` | 已验证或最后已验证 main |
| `operation_id` | 相关 Operation；无则 null |
| `details` | code-specific object |
| `remediation` | 安全的 argv array，不是 shell string |

CLI 已耗尽内部语义重试后才向用户返回 retryable concurrency error。

## 9. Error category

```text
usage
local
trust
protocol
authorization
validation
conflict
check
review
network
unsupported
```

## 10. Core Error codes

### 10.1 Usage/local

```text
CHS_USAGE_INVALID
CHS_PROJECT_NOT_FOUND
CHS_PROJECT_ALREADY_REGISTERED
CHS_WORKTREE_NOT_FOUND
CHS_PATH_NOT_FOUND
CHS_WORKTREE_DIRTY
CHS_LOCAL_STATE_CORRUPT
CHS_PENDING_OPERATION_UNRESOLVED
CHS_SECRET_KEY_NOT_FOUND
CHS_IDENTITY_AMBIGUOUS
CHS_CACHE_BUSY
```

### 10.2 Trust/protocol

```text
CHS_PROTOCOL_UNSUPPORTED
CHS_SCHEMA_INVALID
CHS_UNKNOWN_CORE_FIELD
CHS_GENESIS_INVALID
CHS_PROJECT_ID_MISMATCH
CHS_ROOT_FINGERPRINT_MISMATCH
CHS_MAINLINE_NON_LINEAR
CHS_MAINLINE_ROLLBACK
CHS_COMMIT_SIGNATURE_INVALID
CHS_TRAILER_INVALID
CHS_OPERATION_INVALID
CHS_OPERATION_DIGEST_MISMATCH
CHS_EVIDENCE_INVALID
CHS_EVIDENCE_DIGEST_MISMATCH
CHS_STATE_DIGEST_MISMATCH
CHS_REDUCER_MISMATCH
CHS_OPERATION_ID_COLLISION
```

### 10.3 Authorization

```text
CHS_GRANT_NOT_FOUND
CHS_GRANT_REVOKED
CHS_KEY_MISMATCH
CHS_CAPABILITY_DENIED
CHS_TASK_SCOPE_DENIED
CHS_RESOURCE_SCOPE_DENIED
CHS_LIMIT_EXCEEDED
```

### 10.4 Taskbook/validation

```text
CHS_TASKBOOK_STALE
CHS_ARCHITECTURE_STALE
CHS_TASKBOOK_INVALID
CHS_ARCHITECTURE_INVALID
CHS_TASKBOOK_NOT_ACTIVE
CHS_TASKBOOK_ALREADY_ACTIVE
CHS_TASKBOOK_NOT_COMPLETE
CHS_TASKBOOK_CLOSURE_STALE
CHS_REFERENCE_NOT_FOUND
CHS_GRAPH_CYCLE
CHS_PATH_SCOPE_INVALID
CHS_PATH_ENCODING_INVALID
CHS_SCOPE_VIOLATION
CHS_PROTECTED_PATH_CHANGED
CHS_DEPENDENCY_UNSATISFIED
CHS_TASK_CONFLICT
CHS_TASK_PHASE_INVALID
CHS_TASK_BLOCKED
CHS_TASK_ACTOR_MISMATCH
CHS_RELEASE_HAS_CHANGES
```

### 10.5 Check/Review/Integration

```text
CHS_CHECK_FAILED
CHS_CHECK_ERROR
CHS_ATTEMPT_STALE
CHS_ATTEMPT_UNREACHABLE
CHS_REVIEW_REPORT_INVALID
CHS_REVIEW_CONTEXT_STALE
CHS_REVIEW_REQUIRED
CHS_DRIFT_RELEVANT
CHS_CANDIDATE_CONFLICT
CHS_INTEGRATION_TREE_MISMATCH
```

### 10.6 Concurrency/network/unsupported

```text
CHS_CAS_RETRY_EXHAUSTED
CHS_REMOTE_UNREACHABLE
CHS_PUSH_RESULT_UNKNOWN
CHS_REMOTE_IDENTITY_MISMATCH
CHS_REMOTE_ATOMIC_UNSUPPORTED
CHS_ARCHIVE_REF_INVALID
CHS_PROPOSAL_STALE
CHS_DIRECT_GIT_STATE_DETECTED
CHS_OWNER_WORKFLOW_ACTIVE
CHS_FEATURE_NOT_IN_V1
```

## 11. Exit codes

| Exit | Category |
|---:|---|
| 0 | success |
| 2 | usage |
| 3 | local |
| 4 | trust/protocol |
| 5 | authorization |
| 6 | validation/conflict |
| 7 | check/review |
| 8 | network |
| 9 | unsupported/internal invariant |

不同 Error code 可以共享 exit code；自动化必须优先读取 JSON `error.code`。

## 12. Context schema

`chassiss context --json` 的 `result`：

```json
{
  "architecture": {
    "modules": [],
    "requires_closure": [],
    "resources": []
  },
  "blocked_actions": [],
  "contract": null,
  "dependencies": [],
  "project_summary": {},
  "read_commands": [],
  "schema": "chassiss.context/v1",
  "task": null,
  "workflow": null,
  "workflow_commands": [],
  "worktree": null
}
```

`read_commands`：

```json
{
  "argv": [
    "chassiss",
    "architecture",
    "show",
    "module:core",
    "--json"
  ],
  "description": "Read the exact Module definition."
}
```

不返回 shell string。

`workflow_commands` 使用同一 argv object，并额外声明
`kind=local|protocol-action`。它可以导航 `work status/commit/check` 等不产生
Action 的步骤；`available_actions` 仍只表示当前可授权的协议 Action。

## 13. Help schema

`help --json` 的 `result` 使用：

```text
chassiss.command-schema/v1
```

每个 command 声明：

```text
path
summary
mutating
arguments
options
required_capability
possible_errors
result_schema
```

Skill 使用该 schema，不硬编码完整命令参数。

## 14. Extensions

CLI response 的 `extensions` key 使用反向域名或组织 namespace：

```json
{
  "org.example/telemetry": {}
}
```

Extension：

- 不能改变 `ok`、Error、Snapshot、Identity 或 Operation；
- 不能要求 v1 verifier 读取；
- 不能包含 secret；
- unknown extension 必须可安全忽略。

Project State 不允许 extension。Architecture/Taskbook extension 不参与 Reducer
语义，只参与 exact blob 与 semantic-diff binding。

## 15. 输出安全

CLI response、Error、日志和 remediation 禁止输出：

```text
private key bytes
secret-store unlock data
remote password/token
environment secret
完整 credential helper output
未脱敏工具日志
```

Local absolute worktree path 只可出现在本地 response `result.worktree`，不得写入
Git 或签名 Operation/Evidence。

## 16. Warning codes

Warning 不改变 `ok=true`，使用与 Error 不同的稳定 code：

```text
CHS_WARN_REVIEW_SAME_ACTOR
CHS_WARN_REVIEW_SAME_KEY
CHS_WARN_OWNER_APPLY
```

每个 warning exact 字段为 `code`、`message`、`details`。Reviewer 与 submitter
相同属于可审计风险提示，不是协议拒绝条件。
