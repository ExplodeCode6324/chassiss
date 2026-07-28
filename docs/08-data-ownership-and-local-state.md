# 数据归属、本地状态与保留

## 1. 数据归属测试

新增数据只有满足至少一项才进入 Git：

1. 新 Agent clone 后必须取得它才能验证当前共享状态；
2. 多个 Agent 必须对它形成同一结论；
3. Review/Integration 必须长期绑定 exact 内容；
4. 原机器全部丢失后仍必须恢复该权威事实。

以下任一项成立时默认留在本地：

- 包含 secret；
- 描述当前机器、进程、Session 或 checkout；
- 只优化性能；
- 可以从 Git 确定重建；
- 只属于尚未成功的尝试；
- 不影响其他 Agent 对协议合法性的判断。

## 2. Repository layout

```text
<repo>/
├── .chassiss/
│   └── state.json
├── docs/
│   ├── architecture.yaml
│   ├── taskbook.yaml                 # 仅活动工作流存在
│   └── taskbooks/archive/
│       └── <taskbook-id>.yaml
├── <project source and ordinary documents>
└── .git/
```

项目 worktree 内禁止建立：

```text
.chassiss/cache
.chassiss/keys
.chassiss/credentials
.chassiss/worktrees
.chassiss/journal
.chassiss/locks
```

本地数据必须位于系统 data/cache/runtime/secret store。

## 3. Git 共享数据

| 数据 | 位置/身份 |
|---|---|
| 当前状态 | `.chassiss/state.json` |
| 当前 Architecture | `docs/architecture.yaml` |
| 当前工作流合同 | `docs/taskbook.yaml` |
| 已验收工作流 | immutable Taskbook archive path |
| Genesis 与 Action history | main first-parent commits |
| Root/Grant history | signed Authority Transitions |
| 当前有效 Grant | State `authority.grants` |
| Attempt | exact Work commit/tree |
| 当前 Review | State projection + signed Review Operation/Evidence |
| Review Report | Transition message inline Semantic Operation |
| Integration | signed double-parent main commit |
| 被终止 Attempt | create-only Archive Ref + continuous exact-OID verification |
| 项目成果 | normal Git tree |

## 4. Git refs

```text
refs/heads/main
refs/heads/chassiss/work/<task-id>/<actor-id>
refs/heads/chassiss/transition/<action>/<operation-id>
refs/chassiss/archive/<task-id>/<attempt-hex>
```

### 4.1 Main

唯一权威共享 State ref。禁止 force-push。

### 4.2 Work

- CLI 创建；
- 只 fast-forward；
- submit/approved 期间必须远程可达；
- request_changes 后继续追加；
- Integration 成功后可删除，因为 Work Head 是 main second parent。

### 4.3 Transition

离线/无 main push 权限签名者的临时 proposal。进入 main 或确定 stale/invalid
后删除。它不是权威 State。

### 4.4 Archive

cancelled/superseded 的已提交 Attempt 归档。Ref create-only，v1 不自动删除。
创建 Archive Ref 与对应 main Transition 必须使用 remote atomic push。
full verifier 必须持续确认每个规范 Archive Ref 指向 exact Attempt Head；缺失、
改写或对象不可达时 fail-stop。

## 5. 不进入 Git

```text
private key
secret-store handle
Root PIN/KMS token
remote password/token
local Root fingerprint/checkpoint
project registry
worktree absolute path
pending Operation/Evidence
process lock/PID/socket
parsed cache/index
candidate worktree
Check stdout/stderr
Session/chat/notification cursor
model/provider/token/cost data
```

Remote URL 不得内嵌 password/token。认证通过 Git credential helper、SSH agent
或本地 secret store。

## 6. Local directories

逻辑目录：

```text
data/
  local-state.json
  pending/
  worktrees/
cache/
  history/
  architecture/
  taskbook/
  graph/
  candidate/
  checks/
runtime/
  locks/
secret-store/
  <platform managed>
```

默认位置：

| Platform | Data | Cache | Runtime |
|---|---|---|---|
| macOS | `~/Library/Application Support/CHASSISS` | `~/Library/Caches/CHASSISS` | Data 下 `runtime` |
| Linux | `$XDG_DATA_HOME/chassiss` | `$XDG_CACHE_HOME/chassiss` | `$XDG_RUNTIME_DIR/chassiss` |
| Windows | `%LOCALAPPDATA%\\CHASSISS\\Data` | `%LOCALAPPDATA%\\CHASSISS\\Cache` | Data 下 `Runtime` |

实现可以允许管理员覆盖目录，但不得从 Project repository 中的不受信配置覆盖
CLI binary、secret store 或 trust anchor 路径。

## 7. Local State

Schema：

```text
chassiss.local/v1
```

模板见 `templates/local-state.json`。

每个 Project 保存：

```text
project_id
root_fingerprint
genesis_commit
minimum_checkpoint.commit
minimum_checkpoint.state_digest
remote.url
remote.url_fingerprint
repo_instances
identity.selected_key_id
identity.keys.<key_id>.private_key_handle
worktrees
pending_operations
```

Local State 可以包含 absolute path 和本地时间；它不参与共享签名或 Reducer。

### 7.1 Trust anchor

`root_fingerprint` 与 `genesis_commit` 从受信 bootstrap 建立。不得因 remote
提供不同值自动覆盖。

### 7.2 Checkpoint

一个 Project 的所有 repo instances 共享 minimum checkpoint。接受的新
checkpoint 必须是旧 checkpoint 的 verified descendant。

### 7.3 Identity

一个 Project 可以登记多把本地 key。只保存 private-key handles，不保存
private key bytes。`selected_key_id` 是 UX 默认值；命令 `--key` 可以显式
选择。Grant object 与面向发现的 cached Grant ID/匹配结果都不得写入 identity
Local State、cache 或本地 Credential；每次命令都必须从 current verified
State 重新发现并验证 exact Grant。实现可以在单次进程内保留临时匹配结果，
但进程结束即丢弃，且该结果不具有 Authority。为 crash recovery 持久化的
exact pending Semantic Operation 可以包含其 `authority=grant:<grant-id>`
协议引用；该引用不是 Credential 或可复用 Grant cache。

### 7.4 Repo instances

Project registry 支持同一 Project 的多个 checkout。每个记录：

```text
repo_path
git_dir
created_by_cli
last_verified_head
```

不同 checkout 观察到非 ancestry-compatible histories 时，全 Project mutation
停止，直到 Master 确认正确 remote/history。

## 8. Pending Operation

在第一次可能产生远程副作用前，CLI 原子持久化：

```json
{
  "authority_key_handle": "keychain:...",
  "candidate_commit": null,
  "expected_main": "<commit>",
  "candidate_evidence": {},
  "evidence_digest": "sha256:...",
  "semantic_operation": {},
  "operation_digest": "sha256:...",
  "operation_id": "OPR-...",
  "status": "prepared",
  "target_refs": {
    "refs/heads/main": "<candidate-commit>"
  }
}
```

cancel/supersede 有 Attempt 时，`target_refs` 还必须包含 create-only Archive Ref
与 exact Attempt Head，供 crash recovery 对整组 atomic push 对账。

状态：

```text
prepared
signed
push-unknown
published
reconciled
failed
```

`published/reconciled` 经 fetch 验证后可以删除。`failed` 只有确定无远程副作用
后才能清理。

一次明确 CAS rejection 后可以保留 `semantic_operation`、递增 Evidence
attempt 并替换 candidate Evidence/commit。`push-unknown` 阶段禁止替换
Evidence，必须先按 candidate commit 和 Operation ID 对账。

Pending Operation 不保存 private key 或 signature unlock secret。

## 9. Worktree data

Active Task worktree 是不可随意删除的本地工作数据。记录：

```text
task_id
actor
path
branch
base
head
dirty
published_head
```

Registry 丢失时，CLI 可以从受管 local branch/ref 重建；重建前不得自动删除
任何目录。

`task release`、cancel/supersede 和 Integration 后由 CLI 按协议决定 worktree
回收。含未提交内容时默认保留并报告，除非用户对精确 paths 执行
`work restore`。

如果 remote Task 已 cancelled/superseded，但本机存在尚未形成 Attempt 的
Work Commit 或未提交内容，CLI 必须把 worktree 标记为 local orphan 并保留；
不得自动删除或把它伪装成协议 Evidence。用户可以在源 repo 外导出 patch 后
再通过 `work remove` 显式清理。删除不可达 commits 必须使用
`--discard-unreachable --yes`。

## 10. Cache

可以无损删除：

- verified first-parent index；
- parsed State/Architecture/Taskbook；
- Resource Graph/closure；
- Context slice；
- candidate Review/Integration worktree；
- diff/resource analysis；
- Check dependency cache 与完整日志；
- Dashboard/search index。

`cache clean` 禁止触及：

```text
private key/handle
trust anchor
checkpoint
pending Operation
active worktree
Work/Archive ref
Project repo
```

活跃 CLI process 持有对应 runtime lock 时拒绝删除该 cache。

## 11. Check 与 Evidence

Shared：

- Check Result canonical summary；
- Review Report ≤65,536 bytes；
- digests、exact head/tree/context；
- signer identity/Grant。

Local：

- stdout/stderr；
- test binary；
- dependency cache；
- coverage/raw trace；
- 临时 candidate filesystem。

外部 CI/URL 可以用于人类便利，但不能进入 v1 权威验证链。

若大型/二进制文件是任务交付物，它作为普通 Git content 进入 `writes`、
Review 和 Integration；否则只留本地。

## 12. Remote 变更

Local checkout 只有一个 authoritative upstream。切换 remote 前 CLI 必须验证：

```text
same Project ID
same Root fingerprint
contains local Genesis
new main descends from local checkpoint
all new Transitions valid
```

完整 object graph 与 refs 原样搬迁不改变 Project identity，不属于 migration。

## 13. Read-only export

`chassiss export`：

- 只读指定 repo/ref；
- 不修改 worktree、index、refs、State、Architecture、Taskbook、config 或 remote；
- 支持时先验证 mainline；
- 输出 stdout 或用户指定的 source repo 外路径；
- schema 为 `chassiss.export/v1`；
- 明确标记 verified/unverified。

可以包含：

```text
source repo/ref/OIDs
Project ID/protocol/Root public facts
verified checkpoint
Architecture/Taskbook content/blob/digest
current Task projection
Transition summary
current public Grants
Review/Integration summary
warnings
```

必须排除：

```text
private key/secret
local path/registry
pending Operation
cache/log
environment secret
可直接恢复 Authority 的 Credential
```

Export 不是 State、Genesis、Operation/Evidence、Grant 或自动 import 包。

## 14. Backup

必须备份：

- Root/Agent private keys 或硬件恢复材料；
- Root fingerprint/Genesis/checkpoint trust records；
- 尚未 publish 的 active Task worktree；
- unresolved pending Operation。

可只通过 Git remote/mirror/bundle 备份：

- main；
- Architecture/Taskbook/State；
- Work/Transition/Archive refs；
- 完整 Git objects。

Cache 无需备份。

## 15. 损坏与恢复

| 丢失/损坏 | 结果 |
|---|---|
| Agent private key | 该 key 无法行动；Root add 新 Grant/revoke 旧 Grant |
| Root private key | Project 停止 mutation；只读 export + 新 Genesis |
| trust anchor/checkpoint | mutation 停止；从可信来源恢复 |
| repo instance registry | 重新发现并 full verify |
| worktree registry | 从 branch/ref 重建；先保护未知目录 |
| pending Operation | 扫描 Operation history 后对账 |
| cache | 重建，只影响性能 |
| remote | 使用同 identity 的完整 mirror/bundle 恢复 |
