# CHASSISS 详细文档

本目录说明 CHASSISS 的完整工作机制。初次使用可以先阅读仓库根目录的 [README](../README.md)，再按当前工作进入对应章节。

## 入门

1. [CHASSISS 是什么](01-overview.md)
2. [五分钟 Quickstart](02-quickstart.md)
3. [推荐的 Agent 协作拓扑](03-agent-topology.md)
4. [安装、平台与版本同步](04-installation-and-sync.md)
5. [Root、角色 credential 与秘密分发](05-credentials.md)
6. [Agent 的 bootstrap 入口](06-bootstrap.md)

## 规划与交付

7. [Requirements、Architecture、Mission 与 Task](07-artifacts.md)
8. [完整开发生命周期](08-lifecycle.md)
9. [Task worktree、范围与预算](09-task-worktrees-and-budgets.md)
10. [Checks 与独立验证源](10-checks-and-verification.md)
11. [Reviewer、集成与审计](11-review-and-integration.md)
12. [并发、锁与状态冲突](12-concurrency-and-conflicts.md)

## 控制、恢复与协作

13. [`.chassis/`、事件与状态投影](13-control-state.md)
14. [崩溃恢复与完整性阻断](14-recovery-and-integrity.md)
15. [Publish 与多机器协作](15-publish-and-multi-machine.md)
16. [人类 Owner 接管](16-owner-takeover.md)

## 参考与维护

17. [CLI 命令参考](17-cli-reference.md)
18. [错误处理与故障排查](18-troubleshooting.md)
19. [安全模型与已知风险](19-security-model.md)
20. [协议、版本与兼容性](20-protocol-and-compatibility.md)
21. [示例项目、自动化与 FAQ](21-examples-automation-faq.md)
22. [测试记录、已知问题与改进方向](22-testing-and-improvements.md)

每个 Agent 的实际权限、当前动作和命令参数以可信 CLI 的 `bootstrap` 输出为准。Artifact 的机器格式以内嵌模板和 validator 为准。

[写作目录](menu.md) 保留每章的内容边界，供后续维护使用。
