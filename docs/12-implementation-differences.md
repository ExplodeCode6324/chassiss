# 实现与旧项目差异登记

状态：lock-candidate 工作记录；随实现和 Master 复核持续更新。

## 1. 参考边界

旧仓库 `/Users/muy/Desktop/Codex_Work/chassiss-old` 在本轮始终只读。新仓库没有
修改、提交或重写旧仓库内容，也没有把旧 runtime 复制为兼容层。旧文档只用于
保留成熟的阅读顺序：概览 → 安装/权限 → 合同 → 生命周期 → 并发/恢复 →
安全 → CLI → 测试发布。

## 2. 架构重构

| 旧项目 v0.x | 当前 `chassiss/v1` | 原因 |
|---|---|---|
| `.chassis/` 控制目录保存事件、状态和恢复数据 | Git 中仅有 `.chassiss/state.json` 最小投影；private/local 数据全部外置 | 让共享事实可由 Git history 独立重建，避免 secret 和机器路径进入仓库 |
| YAML credential 与固定 Designer/Orchestrator/Developer/Reviewer/Owner role | `Actor + Key + current Grant + Capability + Scope + Limit` | 消除运行时隐藏角色与隐式扩权，授权只来自 parent verified State |
| `bootstrap` 返回角色工作入口 | `version --json` + `context --json` 返回 verified 动态切片和 available actions | Context 与 exact blobs/checkpoint 绑定，不依赖 prompt 中的静态角色说明 |
| Requirements、Architecture、Mission、Tasks 多套受控文档 | 持久 Architecture + 单轮 Taskbook；Task 是唯一稳定工作实体 | 缩小 Reducer 与 lifecycle，避免 Mission/Task group 的重叠状态 |
| CLI 维护控制事件和状态投影 | 每次共享 mutation 是签名 Transition Commit；Reducer 从 parent State 重算 next State | 历史成为授权、操作、Evidence、Review 与 Integration 的统一账本 |
| Git 主要作为本地 baseline/worktree/diff backend | 标准 Git object/ref/transport + SSH commit signing 是协议承载层 | 支持普通 bare remote，不绑定托管平台或数据库 |
| Role credential 分发 | Agent 本地生成 Ed25519 key 和 proof-of-possession Grant Request；Root 离线审批/签名 | Root 不接触 Agent private key，Grant 内容显式可审计 |
| Reviewer/Owner 由 role 名称表达 | Review 由 capability 与签名 Report 表达；Owner Apply 是受限 Action | 协议身份不依赖 profile 名称，人工接管仍受 State 与 whitelist 约束 |

这些变化是语义重写，不提供自动 import、dual-write 或 silent upgrade。旧项目应
先做只读导出，再由 Master 为新 v1 Project 明确创建 Architecture、Taskbook、
Root trust anchor 和 Genesis。

## 3. 当前实现选择

- Go 单二进制；Git 通过隔离 argv 调用，不使用 shell command 拼接。
- 支持 SHA-1 与 SHA-256 repository object format。
- canonical JSON 使用 RFC 8785 的 UTF-16 key ordering，并禁止 float 与超出
  JavaScript safe-integer 范围的整数。
- YAML 限制为封闭的安全子集，拒绝 duplicate key、float、timestamp、tag、
  alias、BOM、CRLF、non-NFC 和资源超限。
- commit、SSHSIG proof、Work provenance 全部限定 Ed25519。
- Local State 使用 owner-only 目录/文件、临时文件、fsync 和 atomic rename；
  pending Operation 中保存 canonical Semantic Operation/Evidence，不缓存 Grant
  object 或 Grant discovery 结果。
- Candidate/Closure Checks 在临时 linked worktree 中运行，并临时注册 exact
  checkout，使子进程 `chassiss verify` 看到正确上下文。
- 通用 Skill 只调用公开 CLI，不解析 State/Git；捆绑 macOS/Linux
  arm64/amd64 静态 CLI，并由 launcher 校验 manifest digest。

## 4. 已知 lock-candidate 差距

以下项目不会被包装成“已完成”：

1. **真实外部集成测试未运行。** 按 Master 本轮要求，没有对真实 GitHub
   remote、网络故障注入、操作系统 keychain/secret service 做测试；当前证据是
   本地 Git 仓库与本地生命周期测试。
2. **Secret backend。** 当前只实现 `file:` owner-only Ed25519 private-key
   handle；非 file backend 返回 `CHS_FEATURE_NOT_IN_V1`。文件权限与目录隔离已
   在 POSIX 平台验证；Windows 当前依赖用户 profile 的继承 ACL，尚未独立构造和
   校验 DACL。尚未提供 keychain、硬件 key 或加密-at-rest backend。
3. **并发语义重试覆盖深度。** Runtime 已实现最多三次“重新 fetch/verify →
   对账 Operation ID → 重新计算 tree/facts/Checks/Evidence → 重签 → CAS 重推”。
   Review/Integration 会重新分类 drift，Closure 只允许纯 State/Authority drift，
   path collision 会 fail closed。本地 loopback remote 已验证 Authority Transition
   的 attempt 2；该 `git daemon` receive-pack fixture 只在 macOS/Linux 执行，
   Windows 仍执行其余 CLI/协议测试。所有 Action 与连续三次竞争的
   fault-injection matrix 尚未完成。
4. **Push response loss 覆盖深度。** Main Transition push error 后 Runtime 会
   立即 fetch/verify，以 Operation ID + digest 对账远端 history；已发布则收敛到
   实际 commit，否则才 retry 或进入 `push-unknown`。Genesis、exact proposal 与
   Work Ref 也会在错误后读取 exact remote ref/history 再判定结果；`sync` 会
   reconcile 已进入 main 的 pending candidate。所有“远端成功但响应丢失”
   transport 组合的故障注入尚未完成。
5. **Untrusted clone。** `clone --untrusted-read-only` 做一次自洽验证但不登记
   trust，因此后续 project-aware CLI read 也会 fail closed；正式 v1 需决定是
   扩展 Local State trust mode，还是明确保持 one-shot reader。
6. **Golden vectors 深度。** 14 个规范目录齐全，canonical/digest/contract/
   State/Genesis Reducer 是可执行跨实现向量；其余 security/topology/drift/
   integration/recovery 目录当前以场景清单加 Go repository-backed tests 为主，
   尚未为每个 v1 Action 提供完整 success/failure 文件六件套。
7. **协议未锁版。** `docs/` 状态仍是“待 Master 复核”，未生成正式锁版 manifest，
   也未冻结 fixture。实现不能先于规范被宣称为正式 v1。

## 5. 关闭差距的验收证据

正式 lock 前至少需要：

- 在两个相互独立 checkout/CLI 进程上扩展全部 Action、连续三次竞争与多种
  response-loss transport fault injection；
- 在受控测试 remote 上覆盖 clone/sync/Work Ref/Archive Ref/atomic push；
- 为每个 Action 固化 Reducer success 与 rejection vectors；
- 在 macOS、Linux、Windows 验证 Local State path、权限/ACL 与 secret backend；
- 执行 `go test ./...`、race、vet、静态分析、跨平台 build；
- Master 逐项复核规范，生成 docs SHA-256 manifest，冻结 v1 fixtures。
