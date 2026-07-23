# 崩溃恢复与完整性阻断

CHASSISS 的部分操作会跨越 Git、控制状态、credential 文件或远端仓库。进程可能在任意写入步骤退出。operation journal 保存操作的精确前态和预期后态，使 `recover` 能够确定完成或取消操作。

恢复过程不推测用户意图，也不选择新的 Git 结果。

## 三类 journal

| 目录 | 覆盖范围 |
| --- | --- |
| `.chassis/operations/` | Git 与项目事件 |
| `.chassis/auth-operations/` | credential 文件与 Trust store |
| `.chassis/publish-operations/` | 远端分支与 publication event |

journal 使用 JSON 保存，文件权限为 `0600`。每项操作包含版本、ID、actor、resource、revision、阶段、时间和必要证据。

## 项目 Git operation

项目 Git operation 的阶段如下。

```text
prepared
   |
   v
git_applied
   |
   v
state_committed
```

`prepared` 保存 Git 前态、操作意图、预期 Git 后态和待提交的签名事件。Git 动作完成并通过精确比较后进入 `git_applied`。事件和 State 提交后进入 `state_committed`，最后清理 journal。

当前使用该机制的动作包括 Artifact 接受、worktree 建立和释放、work submit、Integration 以及 Owner baseline 变更。

## 授权 operation

授权 journal 的阶段如下。

```text
prepared
   |
   v
credential_prepared
   |
   v
trust_committed
   |
   v
credential_published
```

签发时，CLI 先在受控临时位置准备 credential，再提交 Root 签名的 Trust store，最后原子发布 credential 文件。回收同样保存 Trust 前后 revision 和授权证据。

恢复只接受 journal 中记录的 credential ID、公钥、文件 digest 和 Trust revision。

## Publish operation

Publish journal 的阶段如下。

```text
prepared
   |
   v
remote_applied
   |
   v
state_committed
```

journal 记录 remote 名称、远端 URL digest、branch、推送前 Head、目标 Head 和签名事件。恢复时会重新查询远端，并确认 endpoint 未变化。

## 何时运行 `recover`

以下情况应运行恢复。

- CLI 返回 `CHS-OPERATION-RECOVERY-REQUIRED`
- 上一次写命令被终止
- 机器重启或 Session 意外退出
- publish push 的最终结果不明确

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/current.cred \
  recover
```

恢复命令会获取项目锁，验证 config、Trust、事件链、State 和所有 journal，再按固定规则处理。

## 恢复判定

项目 Git operation 只有三类安全结果。

| 现场状态 | 处理 |
| --- | --- |
| Git 与 journal 前态完全相同，操作尚未生效 | 取消安全的准备操作，或按 journal 完成 worktree 与 Integration 的固定计划 |
| Git 与 journal 后态完全相同 | 提交 journal 中已有的签名事件 |
| 事件已经精确写入 | 重建或确认 State，再清理 journal |

Auth 和 Publish 使用相同原则，只是比较对象分别换成 credential 与 Trust、远端 Head 与 endpoint digest。

## `CHS-INTEGRITY-BLOCKED`

现场无法与 journal 的前态或后态精确对应时，CLI 返回 `CHS-INTEGRITY-BLOCKED`。常见原因包括以下项目。

- 正式分支被手工 reset 或推进
- Task worktree 被移动、替换或切换分支
- journal、事件或 Submission 被编辑
- Trust store 被旧副本覆盖
- 待发布 remote URL 发生变化
- 远端 Head 同时偏离推送前和目标值
- 预期的 Git object 或 credential 文件缺失

完整性阻断表示自动恢复缺少唯一结论。继续写入可能把未经授权的状态变成正式事实。

## 遇到阻断时

1. 停止所有 CHASSISS 写操作
2. 保留 `.chassis`、Git refs、reflog 和远端状态
3. 运行只读的 `doctor --json` 与 `verify --json`
4. 记录错误码、diagnostic category 和 journal ID
5. 由 Master 检查 journal 的前态与后态
6. 在确认恢复方案前不要 reset、force push 或删除文件

当前版本不提供绕过完整性检查的强制参数。

## 安全重试

网络超时或锁冲突可能标记为 retryable，但也要先检查是否生成 journal。推荐顺序如下。

```text
原命令失败
    |
    v
运行 bootstrap 或 doctor
    |
是否要求 recover
   / \
 是   否
 |     |
recover 依据新 revision 重新判断
```

`recover` 成功后重新运行 `bootstrap`。原动作可能已经完成，不能直接重复提交。

## 恢复记录

恢复不会创建替代业务事实。它只完成 journal 中已经固定的事件、确认已经提交的 State，或取消确定未生效的准备操作。事件签名、actor 和资源绑定保持原值。

## 下一步

- [错误处理与故障排查](18-troubleshooting.md)
- [`.chassis/`、事件与状态投影](13-control-state.md)
- [测试记录、已知问题与改进方向](22-testing-and-improvements.md)
