# 信任、密钥与 Grant

协议身份由以下 exact tuple 决定：

```text
Actor + Key + current Grant + Capability + Scope + Limit
```

Profile 只是创建 Grant 时展开 explicit capability/limit 的便捷方式，不是 State
中的角色。当前 `developer` profile 展开 Task 开发能力并要求 bounded limit。

## Agent 生成 Request

```text
chassiss key generate --id KEY-AGENT-01 --actor agent-one --json
chassiss grant request \
  --project PRJ-EXAMPLE \
  --key KEY-AGENT-01 \
  --profile developer \
  --task-scope 'TASK-*' \
  --resource-scope 'module:*' \
  --limits bounded \
  --output /outside/project/request.json \
  --json
```

Request 使用 Agent private key 对 domain-separated digest 做 SSHSIG
proof-of-possession。Request 不包含 private key。

## Root 审批

Root 必须明确审核 Capability、Task scope、Resource scope 和 limits：

```text
chassiss grant add \
  --request /outside/project/request.json \
  --grant-id GRT-AGENT-01 \
  --root-key KEY-ROOT-01 \
  --profile developer \
  --task-scope 'TASK-*' \
  --resource-scope 'module:*' \
  --limits bounded \
  --json
```

离线 Root 可以用 `--proposal <outside-bundle>` 创建签名 Transition，再由在线
checkout 执行 `transition inspect` 与 `transition publish`。Proposal parent
必须仍是 exact current main。

Revoke 使用 `grant revoke <grant-id> --reason ... --root-key ...`。历史签名仍可
验证，但 revoked Grant 不能授权未来 Transition。

当前实现只提供 owner-only `file:` key backend；POSIX 平台校验 mode bits，
Windows 当前依赖用户 profile 的继承 ACL，尚未独立构造和验证 DACL。安全差距见
[实现差异登记](../12-implementation-differences.md)。
