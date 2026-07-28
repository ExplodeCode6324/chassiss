# CLI 与结构化错误

每条命令支持 `--json`。成功或失败都输出一个 `chassiss.cli/v1` envelope：

```text
ok
command
project
snapshot
identity
operation
result
available_actions
warnings
error
extensions
```

使用 `chassiss help --json` 获取完整 machine-readable command tree；使用
`chassiss help task start --json` 获取单条命令。Schema 中 arguments、options
和 possible_errors 始终是 array。

Error 固定包含：

```text
code, category, message, retryable, current_head,
operation_id, details, remediation[]
```

退出码：

| code | category |
|---:|---|
| 0 | success |
| 2 | usage |
| 3 | local |
| 4 | trust/protocol |
| 5 | authorization |
| 6 | validation/conflict |
| 7 | check/review |
| 8 | network |
| 9 | unsupported/internal |

Agent 只执行与当前目标一致的 `remediation[].argv`，不得把它拼成 shell string。
Capability、Scope、Root、rollback、Task conflict 与 relevant drift 必须报告
Master，不能通过换 key、remote 或 Operation ID 绕过。

完整命令和 Error 表见
[CLI 规范](../06-cli-command-specification.md) 与
[JSON API 规范](../10-cli-json-api-and-errors.md)。

