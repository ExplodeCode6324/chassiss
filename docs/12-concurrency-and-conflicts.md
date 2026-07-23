# 并发、锁与状态冲突

多个 Agent 可以同时读取项目，也可以在范围互不冲突的 Task worktree 中并行开发。受控写操作通过项目锁、revision CAS、WIP 限制和 operation journal 串行提交。

## 项目 advisory lock

所有会修改项目控制状态的命令先获取 `.chassis/lock` 对应的操作系统 advisory lock。锁的所有权由打开的文件描述符和内核状态决定。

锁文件可以长期存在。文件时间、进程名称或文本内容都不能证明锁已经失效。Agent 不应根据锁文件年龄删除或抢占它。

持锁进程异常退出后，操作系统会释放所有权。下一次命令获得锁后，再由 recovery 检查未完成 journal。

## State revision CAS

每次控制状态更新都会推进 State revision。Agent 可以使用 bootstrap 返回的 revision 约束写操作。

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/credentials/orchestrator.cred \
  --expect-revision 42 \
  task assign TSK-001 --owner developer-a
```

若其他 Agent 已先完成写操作，实际 revision 将不再是 42，CLI 返回冲突。调用方应重新 `bootstrap`、读取最新 context，再决定是否重试。

不要移除 `--expect-revision` 后盲目重复原命令。项目状态可能已经改变，原动作也可能不再有效。

## Trust revision CAS

授权写操作使用独立的 Trust revision。

```sh
chassiss --root /path/to/project \
  --credential ~/.chassiss/master-root.yaml \
  --expect-trust-revision 7 \
  auth issue --role developer --actor developer-b
```

签发、回收和授权恢复会推进 Trust revision。发生冲突时，Master 重新检查 trust 和目标 grant，避免重复签发或覆盖回收结果。

## WIP 限制

项目配置限制每个 Developer 同时持有的执行中 Task 数量。默认上限为 2。

WIP 检查使用 actor 身份，不使用 Session 数量。一个 Developer 启动多个 Agent Session 也不会得到额外名额。

WIP 已满时可以完成、释放或阻断已有 Task，再领取新任务。

Blocked Task 不计入 Active WIP，但仍保留写入范围。重叠 Task 需要等待它恢复并结束，或由 Orchestrator、Master 通过 release、supersede、cancel 等正式状态动作处理。

## 路径冲突

CHASSISS 比较 Active Task 的 `allowed_paths`。可能覆盖同一项目路径的 Task 不会同时进入执行。

```text
Task A  src/api/**
Task B  src/api/router.go
```

这两个范围存在重叠，后领取的 Task 会被拒绝。

```text
Task A  src/api/**
Task B  docs/**
```

两个范围独立，可以并行执行。Integration 仍会在最新正式 baseline 上构造候选 tree 并重跑 Checks。

模式交集采用保守判断。无法证明互斥时，CLI 优先阻止并行。Designer 可以用更精确的目录边界重新拆分 Task。

## Git 与控制状态的原子边界

部分命令同时修改 Git 和 `.chassis`。单个文件锁无法让两套存储形成数据库事务，因此 CHASSISS 使用 operation journal 记录预期前态、Git 动作和精确后态。

正常写入顺序如下。

```text
获取项目锁
    |
验证 revision 和资源状态
    |
写入 prepared journal
    |
执行 Git 或文件系统动作
    |
提交事件和 State
    |
完成 journal
    |
释放项目锁
```

进程在中间退出时，下一个受控写操作会先要求恢复。

## 自动化的安全重试

cron 或 Agent automation 可以定期轮询，但写操作应遵循固定循环。

1. 运行 `bootstrap --json`
2. 读取 `available_actions`
3. 获取所需 `context_requests`
4. 选择一个允许的动作
5. 带当前 revision 执行
6. 发生 revision 冲突时回到第一步
7. 遇到完整性错误时停止并通知 Master

读取队列和状态可以频繁执行。领取、审批、集成、发布和授权操作不能依据旧输出重复提交。

## 多个 Agent 的调度建议

- Designer 保持独立 Session，集中维护规划契约
- Build Agent 在一个循环中切换 Orchestrator 和 Developer credential
- Review Agent 使用独立 actor
- 多个 Developer 领取互不重叠的 Task
- 每个 Agent 在状态变化后刷新 bootstrap
- 定时监听间隔可从五分钟开始
- 写操作使用 CLI 返回的 revision

## 冲突处理表

| 情况 | 处理 |
| --- | --- |
| 项目锁正被占用 | 等待当前命令结束后重试 |
| State revision 冲突 | 重新 bootstrap 和读取 context |
| Trust revision 冲突 | Master 重新检查 grant |
| WIP 已满 | 完成、释放或阻断已有 Task |
| Task 路径重叠 | 调整计划或等待先行 Task 集成 |
| 正式 baseline 已推进 | 对最新状态重新执行 integration check |
| 存在未完成 journal | 运行 `recover` |
| 完整性阻断 | 停止写操作并交给 Master |

## 下一步

- [`.chassis/`、事件与状态投影](13-control-state.md)
- [崩溃恢复与完整性阻断](14-recovery-and-integrity.md)
- [示例项目、自动化与 FAQ](21-examples-automation-faq.md)
