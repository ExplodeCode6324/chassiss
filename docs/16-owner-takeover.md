# 人类 Owner 接管

Owner 为人类独立开发者提供受控的 baseline 入口。它适合小型维护、紧急修复和人类亲自完成的普通项目变更。

Owner credential 不能接受 Artifact、调度 Agent 或代替 Master Root。Master Root 也不能执行 `owner apply`。

## 使用条件

Owner 变更要求项目处于静止期。

- 没有 Active Mission
- 没有 Active Task
- 没有等待 Master 处理的 Artifact
- 当前分支是项目配置的默认分支
- 当前 Head 精确等于 State baseline
- 工作区包含至少一项未提交的普通项目文件变更

任何条件不满足时，CLI 都会拒绝创建 baseline commit。

## 签发 Owner credential

每个项目最多有一个未回收 Owner grant。

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/master-root.yaml \
  auth issue \
  --actor human-owner \
  --role owner \
  --output ~/.chassiss/cred-human-owner.yaml
```

Owner grant 即使过期也继续占用唯一位置。轮换时先由 Master 显式 revoke，再签发新 credential。

## 准备修改

在正式项目目录切换到默认分支，确认 Head 未移动，然后编辑普通项目文件。

不要预先执行 `git commit`。`owner apply` 只接收工作区中的未提交变化，并由 CLI 内部构造唯一 commit。

可以先检查当前状态。

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/cred-human-owner.yaml \
  bootstrap

git status --short
```

## 受保护内容

Owner 不能修改以下内容。

- `.chassis`
- `.git`
- 已纳入 CHASSISS 管理的 Requirements
- 已纳入 CHASSISS 管理的 Architecture
- 已纳入 CHASSISS 管理的 Mission
- 已纳入 CHASSISS 管理的 Task

受管理 Artifact 的路径来自当前 State。要更新这些文档，应回到 Designer 提交和 Master 接受流程。

## 应用变更

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/cred-human-owner.yaml \
  owner apply --reason "Update release metadata"
```

reason 不能为空，必须是有效 UTF-8，大小不超过 64 KiB。CLI 使用第一行非空文本生成 commit message。

```text
Owner baseline: Update release metadata
```

摘要最多保留 100 个字符，控制字符会被清理。

## CLI 执行的检查

`owner apply` 会完成以下操作。

1. 获取项目锁并检查 State revision
2. 验证 Owner grant 和 baseline scope
3. 确认项目处于静止期
4. 检查默认分支与正式 baseline
5. 收集工作区变化
6. 拒绝受保护路径
7. 在内部准备恰好一个 commit
8. 验证文件集合、metrics 和 tree digest
9. 推进默认分支
10. 写入 `owner.baseline_applied` 事件

命令保留 actor、credential ID、reason、旧 Head、新 Head、tree digest、文件列表、commit message 和变更指标。

## 查看历史

Owner 和 Master 可以读取审计记录。

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/cred-human-owner.yaml \
  owner history
```

记录能够把一次人类变更与具体 Git commit 和签名身份对应起来。

## 常见拒绝

| 错误码 | 含义 |
| --- | --- |
| `CHS-OWNER-PROJECT-ACTIVE` | Mission、Task 或 Artifact 仍在进行 |
| `CHS-OWNER-BRANCH` | 当前分支不是默认分支 |
| `CHS-OWNER-BASELINE-MOVED` | Head 已偏离正式 baseline |
| `CHS-OWNER-NO-CHANGES` | 没有未提交变更 |
| `CHS-OWNER-PROTECTED` | 修改触及控制数据或 Artifact |
| `CHS-AUTH-RESOURCE` | Owner grant 不覆盖当前 baseline |

已存在的人工 commit 不会被 Owner 自动采纳。应先由 Master 查明来源和处理方式，避免用 reset 覆盖未知工作。

## 发布

Owner apply 只推进本地 formal baseline。随后由 Orchestrator 或 Master 运行 `publish check` 与 `publish apply`。

## 下一步

- [Publish 与多机器协作](15-publish-and-multi-machine.md)
- [Root、角色 credential 与秘密分发](05-credentials.md)
- [示例项目、自动化与 FAQ](21-examples-automation-faq.md)
