# Root、角色 credential 与秘密分发

CHASSISS 使用 Ed25519 密钥建立项目根信任、角色授权和事件签名。

Master Root 代表项目所有权。角色 credential 代表一个 actor 在一个项目中的具体 role 与授权范围。

## Master Root

默认命令在 `~/.chassiss/master-root.yaml` 创建 Root。

```text
chassiss auth master-init
```

自定义路径可以指向文件或目录。

```text
chassiss auth master-init --output /secure/location/master-root.yaml
```

文件以 `0600` 写入，父目录以 `0700` 创建。已有文件不会被覆盖。

Root 包含公钥和私钥。项目只保存 Root 公钥、fingerprint 和 Root 签名的 trust。Root 文件应保存在项目目录之外，并由人类控制。

Master Root 可以执行以下动作。

- 签发 credential
- 回收 credential
- 接受或拒绝 Artifact
- 接受 Mission
- 取消 Task
- 执行 publish
- 读取 Owner history

Root 不能通过 `auth export` 传输，也不能执行 `owner apply`。

## 角色 credential

每份 credential 包含以下内容。

- 项目 ID
- Root fingerprint
- credential ID
- actor
- role
- actions
- 资源 scope
- 生效与过期时间
- 私钥

项目中的 `trust.yaml` 保存对应公钥、授权和回收记录。credential 文件保存私钥。

## 签发

```text
chassiss --root /path/to/project \
  --credential ~/.chassiss/master-root.yaml \
  auth issue \
  --actor developer-1 \
  --role developer \
  --output ~/.chassiss/cred-developer-1.yaml
```

如果省略 `--output`，CLI 使用 `~/.chassiss/cred-<actor>.yaml`。如果省略 Root 参数，CLI 会在默认 Root 位置和 `~/.chassiss/roots/` 中查找 fingerprint 匹配的唯一 Root。

actor 必须是稳定标识，长度为 1 到 128 个字符，且不能包含空白或控制字符。

## 时间限制

默认 credential 没有过期时间。可以使用绝对时间或 TTL。

```text
chassiss auth issue \
  --actor reviewer-1 \
  --role reviewer \
  --expires-at 2026-08-01T00:00:00Z
```

```text
chassiss auth issue \
  --actor reviewer-1 \
  --role reviewer \
  --ttl-seconds 86400
```

`--not-before` 与 `--expires-at` 使用 RFC3339。`--expires-at` 和 `--ttl-seconds` 不能同时使用。

TTL 范围为 1 到 315360000 秒。

## 资源 scope

credential 可以限制到具体资源。多个值使用逗号分隔。

```text
chassiss auth issue \
  --actor developer-1 \
  --role developer \
  --tasks M001-T001,M001-T002
```

支持的 scope 维度如下。

| 参数 | 资源 |
| --- | --- |
| `--projects` | 项目 ID |
| `--missions` | Mission ID |
| `--tasks` | Task ID |
| `--submissions` | submission ID |
| `--submission-digests` | submission digest |
| `--heads` | Git head |
| `--baselines` | formal baseline |

空 scope 表示该维度不受额外限制。命令执行时仍会验证角色、状态和资源所有权。

`--actions` 可以从角色允许的 action 中选择子集。它不能增加该角色原本没有的 action。

## Build Agent 的两份 credential

兼任 Orchestrator 与 Developer 的 Agent 使用相同 actor。

```text
chassiss auth issue \
  --actor build-1 \
  --role orchestrator \
  --output ~/.chassiss/cred-build-orchestrator.yaml

chassiss auth issue \
  --actor build-1 \
  --role developer \
  --output ~/.chassiss/cred-build-developer.yaml
```

同一 actor 使 Orchestrator 可以领取 Task，Developer 随后以相同身份打开 worktree。两条事件仍记录不同 role 和 credential ID。

## Owner 唯一性

一个项目最多有一个未回收 Owner grant。Owner 即使已经过期，也会占用该位置，直到 Master 显式回收。

轮换 Owner 时先回收旧 credential。

```text
chassiss --credential ~/.chassiss/master-root.yaml \
  auth revoke <owner-credential-id> \
  --reason owner-rotation
```

随后签发新的 Owner credential。

## Armor 导出与导入

角色 credential 可以导出为三行 armor。

```text
chassiss auth export ~/.chassiss/cred-developer-1.yaml
```

格式包含一行 header、一行 Base64 payload 和一行 footer。payload 封装原始 credential YAML、envelope version 和 SHA-256 digest。

Armor 输入大小上限为 256 KiB。

接收方执行以下命令。

```text
chassiss auth import --output ~/.chassiss/my-credential.yaml
```

导入过程验证以下内容。

- armor 结构
- Base64 编码
- envelope version
- payload digest
- YAML schema
- role 与 actions
- 私钥长度

输出文件以 `0600` 原子写入。已有路径不会被覆盖。

Armor 包含私钥。Base64 只提供文本编码。发送时应使用受控通道，并避免在长期聊天记录、公开日志和录屏中保留。

## 回收

先用 `auth inspect` 获取 credential ID。

```text
chassiss auth inspect ~/.chassiss/cred-developer-1.yaml
```

然后执行回收。

```text
chassiss --credential ~/.chassiss/master-root.yaml \
  auth revoke <credential-id> \
  --reason compromised
```

回收写入 Root 签名的 trust，并增加 `trust.revision`。后续使用该 credential 会返回 `CHS-AUTH-REVOKED`。

## 普通轮换

当前版本没有独立 `rotate` 命令。普通角色按以下顺序轮换。

1. 签发新 credential
2. 分发并完成 `bootstrap`
3. 确认新 credential 可用
4. 回收旧 credential

Task 记录分派时的 `owner_grant_id` 作为来源证据。相同 actor 的新 Developer credential 可以继续处理原 Task。

## trust 并发

授权变更使用独立的 `trust.revision`。自动化签发与回收时应传入 `--expect-trust-revision`。

```text
chassiss --expect-trust-revision 7 \
  --credential ~/.chassiss/master-root.yaml \
  auth issue \
  --actor developer-2 \
  --role developer
```

stale revision 会返回 `CHS-CONFLICT-TRUST-REVISION`。调用方需要重新读取项目并评估操作。

## 认证诊断

`--json` 模式提供稳定的 `diagnostic_category`。常见值包括以下内容。

- `grant_not_found`
- `revoked`
- `metadata_mismatch`
- `policy_mismatch`
- `key_invalid`
- `key_mismatch`
- `signature_invalid`
- `project_mismatch`

调用方应依据错误码与 remediation 处理，避免解析人类错误文本。
