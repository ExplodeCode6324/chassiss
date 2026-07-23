# 协议、版本与兼容性

CHASSISS 将 Agent 可见 API、项目文件、事件、Trust、role policy 和 bootstrap 分别版本化。版本号用于冻结验证规则和签名字节，不能只按字段看起来相近就继续读取。

## 当前版本

| 协议或文件 | 当前值 |
| --- | --- |
| CLI | `0.3.0-dev` |
| API | `chassiss.dev/v2` |
| Config | `4` |
| State | `4` |
| Event | `4` |
| Trust | `1` |
| Credential | `1` |
| Role Policy | `3` |
| Bootstrap | `chassiss.bootstrap/v3` |
| Credential armor envelope | `1` |
| Project operation journal | `1` |
| Publish operation journal | `1` |

CLI 仍处于开发版本。自动化应同时检查 binary version 和 schema version。

## JSON API

`--json` 输出使用 `chassiss.dev/v2` envelope。字段供 Agent 和自动化读取，错误使用稳定 code、retryable、remediation 和 diagnostic category。

调用方应忽略自己不需要的展示字段，但不能忽略 schema version。状态写操作仍需使用当前 revision。

## 签名字节

事件和 Trust 在签名前生成确定性 JSON 字节。当前实现使用 Go `encoding/json` 对受控结构编码，并禁止协议结构出现浮点数。

该编码是 CHASSISS 当前协议的一部分，不声明兼容 RFC 8785 JCS。其他语言实现需要复现 golden vectors，不能用通用 canonical JSON 库自行推断。

digest 使用 SHA-256，角色和 Root 签名使用 Ed25519。

## Event V4

事件 envelope 包含以下核心字段。

- version
- ID
- Project ID
- sequence
- event type
- resource
- actor、role 和 credential ID
- occurred time
- previous digest
- payload
- digest
- signature

payload 只保存该状态转换所需事实。actor、revision 和时间来自签名 envelope，不在 payload 中重复维护。

事件按 sequence 排序，并由 `previous_digest` 连接。reducer 从初始事件开始确定性重建 State。

## 事件类型

当前 Event V4 包含以下类型。

```text
project.initialized
artifact.submitted
artifact.accepted
artifact.rejected
mission.activated
mission.blocked
mission.resumed
mission.acceptance_submitted
mission.completed
task.claimed
task.assigned
task.blocked
task.resumed
task.released
task.cancelled
task.superseded
work.opened
work.checked
work.checkpointed
work.submitted
work.blocked
review.approved
review.changes_requested
integration.applied
publication.applied
owner.baseline_applied
```

未知类型、额外 payload 字段、错误 resource 或非法状态转换都会被拒绝。

## Trust V1 与 Credential V1

Trust V1 记录 Root 公钥、Trust revision、grants、revocations、更新时间和 Root signature。

Credential V1 记录项目、Root fingerprint、actor、role、actions、resource scope、有效期和私钥。credential metadata 必须与当前 grant 完全匹配。

Armor envelope 独立版本化，并用 digest 检查被传输的 credential YAML。

## Config V4 与 State V4

Config V4 固定 Project ID、模式、默认分支、内容 backend、WIP limit、默认 Task budget 和 Root fingerprint。

State V4 是事件链投影。它记录当前 phase、baseline 和所有领域对象。State 能被重建，不能独立创造新事实。

## Role Policy V3

Role Policy 定义角色可见命令、action、参数、状态条件和通用 invariants。`bootstrap` 返回 policy digest，使 Agent 能发现正在使用的规则版本。

策略更新可能改变命令是否可见或某个动作的输入。Agent 不应把静态 prompt 当作永久授权表。

## Bootstrap V3

Bootstrap envelope 绑定 binary version、项目路径、State revision、Trust revision 和 principal。`available_actions` 是当前状态的投影，不是可以离线保存的授权 token。

revision、trust、credential 或目标资源变化后应刷新 bootstrap。

## Golden vectors

仓库中的协议 golden tests 冻结以下内容。

- Event V4 canonical bytes
- event digest
- signing bytes
- Trust V1 canonical bytes 与 digest

任何有意协议调整都应先定义新版本和迁移边界，再更新 golden vectors。只修改 golden 期望值会掩盖不兼容变化。

## 旧项目策略

当前 CLI 明确拒绝旧 Config、State 和 Event schema，并返回 `CHS-SCHEMA-UNSUPPORTED`。项目不会自动补字段或静默升级。

当前没有原地迁移工具。需要保留旧版本 CLI 读取旧控制端，或经过独立验证后创建新的 API V2 项目。

## 升级要求

未来协议升级至少需要以下工作。

1. 定义新 schema 和版本号
2. 固定签名字节与 golden vectors
3. 明确旧版本读取和写入策略
4. 提供离线、可审计的迁移工具
5. 验证迁移前后事件、State、Trust 和 Git baseline
6. 保留失败时可恢复的原控制端
7. 更新 Skill、bundled CLI 和所有 Agent

升级期间不能让不同协议版本的 CLI 并行写入同一控制端。

## backend 兼容边界

非 Git content backend 需要提供与现有生命周期等价的确定性能力。

- 不可变内容 identity
- baseline 与候选 Head
- 文件范围和变更指标
- 隔离工作区
- merged candidate
- 原子正式推进
- fast-forward 或同等单调关系
- 崩溃恢复前后态
- 可签名的精确证据

backend 名称本身不能降低 Task、review、integration 和 recovery 的约束。

## 下一步

- [`.chassis/`、事件与状态投影](13-control-state.md)
- [安装、平台与版本同步](04-installation-and-sync.md)
- [测试记录、已知问题与改进方向](22-testing-and-improvements.md)
