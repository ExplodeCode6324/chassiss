# Checks 与独立验证源

Checks 是 Task 契约中的可重复命令。Developer 在提交前执行冻结 Checks，Reviewer 验证结果与精确 Head 的绑定，Integration 在 merged candidate 上重新执行，使审批对应到可复现的代码快照。

Checks 只提供机械证据。需求理解、架构一致性、风险判断和代码质量仍需要 Agent 复核。

## CheckSpec

每项 Check 使用结构化字段。

```yaml
checks:
  - id: unit-tests
    argv:
      - go
      - test
      - ./...
    cwd: src
    env:
      GOFLAGS: "-count=1"
    timeout_seconds: 300
    verification_paths:
      - internal/testdata/**
```

| 字段 | 含义 |
| --- | --- |
| `id` | Check 的稳定名称 |
| `argv` | 程序和参数数组 |
| `cwd` | 项目内的工作目录 |
| `env` | 额外环境变量 |
| `timeout_seconds` | 允许的执行时间 |
| `verification_paths` | 独立维护的验证源 |
| `shell` | 是否显式交给 shell |

超时时间范围为 1 到 86400 秒。

## 默认不使用 shell

`argv` 默认直接启动程序。参数不会经过 shell 展开，因此重定向、管道、命令替换和通配符不会获得额外含义。这减少了环境差异和字符串注入风险。

确实需要 shell 时可显式设置 `shell: true`。此时 `argv` 必须只包含一段脚本。脚本应保持短小，并避免读取未声明的环境状态。

```yaml
checks:
  - name: generated-files
    shell: true
    argv:
      - test -z "$(git status --porcelain)"
    timeout_seconds: 60
```

## 工作目录边界

`cwd` 使用项目相对路径。CLI 会解析符号链接并确认最终目录仍在 Task worktree 内。

以下做法会被拒绝。

- 绝对路径
- `..` 逃逸
- 指向项目外部的符号链接
- 不存在或并非目录的路径

Check 不应依赖调用 Agent 当时所在的目录。

## 环境变量

CLI 使用受限环境运行 Checks。基础环境只保留执行和构建所需项目。

- `PATH`
- 临时 `HOME`
- `TMPDIR`
- `GOCACHE`
- `GOMODCACHE`
- `LANG`

Task 中声明的 `env` 会加入该环境。其他 Session 环境变量不会自动继承。测试若需要服务地址、fixture 路径或构建开关，应在 CheckSpec 中明确声明。秘密不应写入 Artifact。

## 独立验证源

`verification_paths` 用于绑定 Developer 无权修改的验证材料。常见内容包括以下几类。

- 验收测试
- golden vectors
- 固定输入输出
- 协议样本
- 安全策略 fixture

CLI 在 Task baseline 保存验证源 digest，并在 work check、review check 和 integration 期间重新计算。内容变化会使原证据失效。

验证路径不得与 `allowed_paths` 重叠，并且需要在冻结 baseline 中匹配实际文件。空模式、重复模式和项目外路径都会被拒绝。

## Developer 阶段

`work check` 执行选定的冻结 Checks，再完成范围、预算、快照和验证源 preflight。CLI 记录每项命令、退出结果以及当前代码和验证源 digest。

输出只保留末尾 4000 个字符，避免事件或错误消息无限增长。超时的命令以退出码 124 记录。

全部 Checks 通过后，Developer 才能为同一快照运行 `work submit`。文件内容、index 或 Task Head 变化都会要求重新检查。

## Reviewer 阶段

`review check` 验证 Submission manifest、精确 Head、范围、预算、验证源和 Developer check 证据。它确认每项结果来自 Submission 的精确 snapshot，但当前版本不会重新执行 Check 命令。

机械检查通过不代表 Reviewer 必须批准。Reviewer 仍要阅读代码、需求、架构、Task 验收条件和 Developer handoff。

## Integration 阶段

Integration 在隔离的候选 worktree 将 Task 与当前正式 baseline 合并。CLI 对合并后的 tree 再次运行相同 Checks，并重新验证独立验证源。

这一步覆盖 Task 提交后正式分支已经集成其他并行工作的情况。只有候选 merged tree 通过，正式分支才会推进。

## 证据绑定

一次通过结果至少与以下内容绑定。

- Task 和 Submission
- baseline commit
- 候选 Head commit
- worktree snapshot digest
- 修改文件与预算指标
- Checks 定义与结果
- 验证源 digest

因此，复制旧输出、修改 commit 后复用结果或仅口头报告测试通过，都不能替代 CLI 证据。

## 编写 Checks 的建议

- 命令应可重复执行
- 固定依赖版本和 fixture
- 将快速检查放在前面
- 给每项命令设置合理超时
- 避免网络和全局用户配置
- 不读取开发者个人目录
- 将关键验收条件放入独立验证源
- 保持错误输出能够定位问题

如果测试需要外部服务，项目应提供固定的本地替代、容器配置或明确的测试环境约定。当前 CHASSISS 不负责供应外部服务。

## 下一步

- [Reviewer、集成与审计](11-review-and-integration.md)
- [安全模型与已知风险](19-security-model.md)
- [协议、版本与兼容性](20-protocol-and-compatibility.md)
