# 实现与一致性要求

## 1. 目标

一个 CHASSISS v1 实现完成的标准不是“命令能够运行”，而是：

- 独立实现对相同 Git objects 得到相同验证结论；
- Reducer 对相同父 State/Semantic Operation/Execution Evidence/Git facts 生成
  相同 next State bytes；
- CLI 无法通过普通参数绕过 signature、Scope、Taskbook、Review 或 CAS；
- 所有受支持 Git 写操作都经过 CLI；
- 本地数据损坏不会静默改变共享协议事实。

## 2. 规范优先级

实现只以当前 `docs/` 目录为规范源。

优先级：

1. 对应主题的规范文档；
2. `docs/README.md` 的全局边界；
3. `docs/templates` 的结构示例。

模板与字段说明冲突时，以字段说明为准并修复模板。其他目录中的历史草案不能
作为实现例外依据。

## 3. 实现模块边界

建议模块：

```text
protocol/
  identifiers
  canonical-json
  digests
  operation
  execution-evidence
  trailers
  actions
  errors

crypto/
  ssh-sign
  ssh-verify
  fingerprints
  secret-handles

git/
  objects
  topology
  refs
  transport
  worktrees
  candidate-tree

state/
  schema
  reducer
  invariants

contracts/
  yaml-subset
  architecture-schema
  taskbook-schema
  semantic-diff
  graph
  context
  conflict

workflow/
  start
  work
  submit
  review
  integrate
  architecture
  taskbook
  owner
  authority
  retry

local/
  registry
  checkpoint
  pending-operation
  cache
  locks

cli/
  commands
  json-envelope
  human-renderer
```

Git command execution 必须使用 argv array，不通过 shell string。所有 Git
环境必须显式隔离关键 config，不能依赖用户 alias、hook 或当前 index 的偶然
状态。

## 4. 建议实现顺序

1. Canonical JSON、digest、ID 与 Error primitives；
2. State/Architecture/Taskbook parser 与 schema validation；
3. Git object/topology reader；
4. SSH signature verifier 与 Genesis full verification；
5. Semantic Operation、Execution Evidence 与 deterministic Reducers；
6. first-parent verifier/checkpoint；
7. local registry/pending Operation；
8. init/clone/sync/status/context；
9. Task start 与 managed worktree；
10. Work commit/check/submit；
11. Review Context/Report；
12. Drift 与 Integration；
13. Grant Request/add/revoke 与 offline proposal；
14. export/cache/retention；
15. Skill 与 release packaging。

Local registry、cache 和 Credential schema 不得包含 Grant object、面向发现
的 cached Grant ID 或持久化 Grant 匹配结果；Grant discovery 每次只读取
current verified State。Pending Semantic Operation 的 exact Authority 引用
不属于 discovery cache。

不得先实现自由 Git passthrough，再尝试在外围补协议校验。

## 5. Golden test vectors

Repository 必须维护跨实现 fixture：

```text
fixtures/
  canonical-json/
  digests/
  taskbook-valid/
  taskbook-invalid/
  architecture-valid/
  architecture-invalid/
  state-valid/
  state-invalid/
  reducers/
  signatures/
  mainline-valid/
  mainline-invalid/
  drift/
  integration/
  local-recovery/
```

每个 Reducer vector 至少包含：

```text
parent State bytes
Semantic Operation canonical bytes
Execution Evidence canonical bytes
Git facts
expected next State bytes
expected State digest
expected Error code（失败向量）
```

## 6. Canonicalization tests

必须覆盖：

- Unicode string 与 escaping；
- map key ordering；
- integer boundaries；
- duplicate JSON/YAML key；
- prohibited float/timestamp/tag/alias；
- LF/CRLF/BOM；
- State trailing LF；
- base64url Operation/Evidence blocks；
- digest domain separation；
- unknown core/extension fields。

## 7. Signature tests

必须覆盖：

- valid Root Genesis self-sign；
- wrong Root fingerprint；
- valid parent Grant signature；
- next State self-authorization attempt；
- wrong Authority trailer；
- revoked Grant；
- same Actor/different unauthorized key；
- commit message/tree/parent/Trailer tamper；
- Work Commit provenance signature；
- malformed OpenSSH public key；
- non-Ed25519 key rejection。

测试 private keys 只能是 fixture keys，不得复用真实 release/Project keys。

## 8. Topology tests

必须覆盖：

- zero-parent Genesis；
- one-parent ordinary Transition；
- two-parent exact Integration；
- Integration parent order swapped；
- unknown extra parent；
- unsigned/ordinary commit on main first-parent；
- Work merge/rebase chain；
- force-pushed Work Ref；
- stale proposal；
- Archive Ref exact OID validation、atomic create 与 deletion fail-stop。
- remote 不支持 atomic push 时有 Attempt 的 cancel/supersede 拒绝。

## 9. Reducer tests

每个 v1 Action至少具有：

- happy path；
- wrong phase；
- wrong actor；
- missing Capability；
- Task/Resource Scope denial；
- bounded Limit denial；
- protected path change；
- extra State field；
- omitted required State field；
- stale semantic precondition；
- Operation ID duplicate/collision；
- Action file-change whitelist violation。

## 10. Taskbook/Architecture tests

必须覆盖：

- minimal root Module；
- all optional Views；
- missing/duplicate Resource；
- requires cycle；
- owner missing；
- Architecture update only with no active Taskbook；
- Architecture semantic diff and removed-ID non-reuse；
- Taskbook open→all Tasks terminal→closure review→archive→next open；
- archive exact blob relocation and State task clearing；
- missing/empty `workflow.checks` rejection；
- Workflow/Task Check ID namespace collision rejection；
- Workflow Closure Check pass/fail/error 与 exact Context binding；
- historical verifier 验证 Workflow Check 签名/绑定但不重跑命令；
- cancelled/superseded disposition 仅凭 Reviewer 的签名逐项 response 可验收；
- Archive CAS 纯 Authority drift 后重跑 Checks；
- Archive CAS ordinary content/Task/contract drift 返回 closure stale；
- Task DAG cycle；
- writes 与 selected Module paths 完全不相交；
- intersecting but non-contained writes accepted；
- invalid UTF-8、non-NFC、control byte path rejection；
- exact/prefix path intersections；
- read/read no conflict；
- write/write、write/read conflicts；
- ready Task same-ID update；
- non-ready Task mutation rejection；
- stale Taskbook candidate；
- deterministic Context slice。

## 11. Review/Integration tests

必须覆盖：

- exact Attempt binding；
- Reviewer same Actor accepted with warning；
- Reviewer same key/different Actor accepted with warning；
- Report >65,536 bytes；
- blocking/advisory Findings；
- approve with failed result；
- request_changes State sparsification；
- unrelated pure State drift；
- unrelated disjoint content drift；
- relevant path/resource drift；
- unknown drift→relevant；
- candidate conflict；
- Integration Check failure；
- exact double-parent final tree；
- zero-Work-Commit/no-op Attempt and Integration；
- CAS failure 后重新计算 candidate。

## 12. Concurrency/recovery tests

必须覆盖：

- two unrelated Task starts；
- conflicting Task starts；
- concurrent Grant revoke vs Agent Action；
- push success/response loss；
- same Operation ID reconciliation；
- three-attempt CAS limit；
- same Operation/different Evidence attempts；
- same Operation ID/different Operation digest collision；
- push-unknown prohibits Evidence replacement；
- local pending store crash at prepared/signed/push-unknown；
- remote rollback below checkpoint；
- multiple local checkout checkpoint sharing；
- remote identity change。

## 13. CLI ownership tests

必须证明：

- Task start 创建固定 managed branch/worktree；
- Work commit 只 stage允许 paths；
- Work chain 每个 parent→child diff 都在 frozen writes 内；
- direct State/Architecture/Taskbook/archive worktree changes 被拒绝；
- 没有 generic branch/merge/rebase/reset/force flags；
- submit 要求 clean；
- task release 拒绝有变化的 Work Head；
- work restore 拒绝 broad target；
- work remove 拒绝 dirty/unreachable data，除非显式 destructive confirmation；
- cache clean 不删除 active/pending/trust data；
- remote set 先验证 identity/ancestry。
- Owner Apply 拒绝 active workflow、pending Operation、worktree 和协议文件；
- Owner Apply 只 snapshot 当前目录所属 registered non-managed worktree 的
  repository root，且不接受 source path 参数；
- Owner Apply 在静默 workflow 下生成 signed single-parent Transition，State
  bytes 保持不变。

## 14. Security requirements

- 不通过 shell 拼接 Git/Check argv；
- temporary file 使用 owner-only 权限；
- private-key handle 和 secret 从不进入 Git/message/JSON/error；
- candidate worktree 与 index 必须隔离；
- symlink path traversal 必须在 staging/check 时拒绝越界；
- path comparison 使用 repo-relative normalized `/`；
- remote content 在验证前视为不可信；
- YAML/JSON parser 配置 resource limits 防止内存/递归 DoS，但不得引入项目
  “修改预算”；
- verification failure 不更新 checkpoint；
- fsync/atomic rename 保护 pending Operation 与 local checkpoint。

## 15. CLI conformance

每个 command 必须：

- 支持 `--json`；
- 使用统一 envelope；
- Human output 从 envelope 渲染；
- 返回稳定 Error code/exit code；
- mutation 隐式 fetch/verify；
- destructive local action 精确 target；
- 不提供隐藏 bypass flag；
- `help --json` 与实际 parser 一致。

## 16. Protocol compatibility

CLI 必须声明支持的 exact protocol majors。

- 支持 `chassiss/v1`：可读写；
- 未知 major：拒绝 mutation；
- 若有专门 legacy reader：只读 verify/export；
- 不允许 silent upgrade、dual-write 或 automatic import。

## 17. v1 完成条件

实现进入 v1 lock candidate 前必须完成：

1. 全部 docs 字段/Action/Command 有 schema 或 parser；
2. 所有 Reducer 有 golden vectors；
3. full verify 可从 Genesis 重建 current State；
4. clone→Grant discover→Taskbook open→start→commit→submit→review→integrate→
   Workflow Closure Checks→Taskbook archive→next Taskbook 端到端通过；
5. concurrent CAS/retry 与 push-unknown 恢复通过；
6. offline Root proposal/publish 通过；
7. rollback/illegal main/ref tamper 测试通过；
8. 所有 templates 通过 parser；
9. Skill 只依赖 public CLI API；
10. 无 secret/local data 出现在测试 Git tree；
11. Local identity State/cache 中不存在 Grant object、cached Grant ID 或
    持久匹配结果；
12. Owner Apply 只在静默 workflow 条件下通过。

## 18. 锁版流程

当前 docs 状态是“待 Master 复核”。锁版时：

1. Master 逐项确认 docs；
2. 修复所有 cross-reference/schema/template 差异；
3. 生成 docs manifest 与每个文件 SHA-256；
4. 标记 protocol `chassiss/v1`；
5. 冻结 golden vectors；
6. 后续任何语义修改必须进入新评审或新 major。
