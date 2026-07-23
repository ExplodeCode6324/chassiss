# CHASSISS 文档写作目录

这是后续详细文档的暂定目录。每篇文章的标题、边界和主要内容先在这里固定；正式写作时可以根据内容长度合并或拆分，但不应遗漏所列机制。

## 1. CHASSISS 是什么

主要内容包括设计哲学、适用场景与非目标；Agent 与 CLI 的职责边界；为什么语义判断交给 Agent、确定性约束交给 CLI；CHASSISS 能防止什么、不能防止什么。

## 2. 五分钟 Quickstart

主要内容包括安装 Skill 和匹配平台的 CLI；创建 Master Root；初始化新项目或以 `--existing` 接管现有项目；签发最小角色组合；让每个 Agent 完成第一次 `bootstrap`；跑通第一个 Mission 和 Task。

## 3. 推荐的 Agent 协作拓扑

主要内容包括 Master、Designer、Orchestrator、Developer、Reviewer 的分工；Designer 独立 Session；Orchestrator 与 Developer 共处一个 Build Agent；Reviewer 独立运行；最小两 Agent 配置、更多 Agent 并行配置，以及一个前沿 Agent 代行 Master 的实验模式。

## 4. 安装、平台与版本同步

主要内容包括 Skill 目录结构、bundled CLI 选择方式和校验；macOS/Linux 支持范围与 Windows 写操作限制；当前 v0.3 为什么仍依赖本地 Git；GitHub/GitLab/remote-git 的可选关系；非 Git backend 的扩展方向。

## 5. Root、角色 credential 与秘密分发

主要内容包括 Master Root 与角色 credential 的区别；Designer、Orchestrator、Developer、Reviewer、Owner 的签发；同一 Build Agent 使用同 actor 的两份 credential；Base64 armor 的 export/import；为什么 Base64 不是加密；存储、分发、回收、轮换和泄漏处理；唯一 Owner grant 的限制。

## 6. Agent 的 `bootstrap` 入口

主要内容包括如何从 `principal`、`policy`、`capabilities`、`available_actions` 和 `context_requests` 获取身份与下一动作；revision-bound action 的含义；何时必须重新 bootstrap；为什么 Agent 不能自报角色、猜测权限或依赖静态角色说明。

## 7. Requirements、Architecture、Mission 与 Task

主要内容包括四种 Artifact 的职责、依赖关系和内嵌模板；YAML front matter 与 Markdown 正文；digest 绑定；Designer 提交、Master 接受和作者不能自批；冻结后的契约为什么不能原地改写。

## 8. 完整开发生命周期

主要内容包括从需求讨论到 Mission 验收的逐步命令；Designer 规划；Orchestrator 激活 Mission、分派或领取 Task；Developer open/check/checkpoint/submit；Designer 做设计一致性复核；Reviewer context/check/approve/request-changes；integrate apply；Mission submit-acceptance 与 Master accept。

## 9. Task worktree、范围与预算

主要内容包括每个 Active Task 的独立 linked worktree；`allowed_paths`、依赖和并行边界；文件数、diff 行数、提交数预算；scope/budget preflight；预算耗尽或契约变化后的停止条件；新 Task、`task supersede`、block/resume/release/cancel 的使用边界。

## 10. Checks 与独立验证源

主要内容包括结构化 `argv/cwd/env/timeout_seconds/verification_paths`；为什么默认不经过 shell；Developer 不能修改验证源；Developer、Reviewer 与 Integration 如何对同一冻结证据重复验证；check 通过与语义正确之间的区别。

## 11. Reviewer、集成与审计

主要内容包括 Reviewer 独立性的身份约束；submission digest 与精确 HeadCommit；机械检查、语义 verdict 和 review report；request-changes 历史；候选 worktree 集成、checks 重跑、merged-tree 证据和正式 baseline 推进。

## 12. 并发、锁与状态冲突

主要内容包括项目 advisory lock、revision CAS、trust revision CAS 和 WIP 限制；并行 Task 的路径冲突检查；为什么不能按锁文件年龄抢锁；冲突后重新 bootstrap 与安全重试方式。

## 13. `.chassis/`、事件与状态投影

主要内容包括 `config.yaml`、`trust.yaml`、`state.yaml`、`events/`、`operations/`、`submissions/`、`worktrees/`、`cache/` 和 `lock` 的职责；事件作为事实源、State 作为可重建投影；哪些内容绝对不能手工修改或删除。

## 14. 崩溃恢复与完整性阻断

主要内容包括 Git、授权和 publish operation journal；prepared、applied、committed 阶段；`recover` 如何只完成精确匹配的操作；什么情况下进入 `CHS-INTEGRITY-BLOCKED`；为什么系统不会猜测 reset、force 或覆盖状态。

## 15. Publish 与多机器协作

主要内容包括 local integration 与 publication 是两个独立事实；`publish check/apply`、fast-forward 和禁止 force push；远端 endpoint 与 SHA 绑定；使用 cron 或 Agent automation 每五分钟监听正式版本；当前 publish 只同步代码 baseline、不同步 `.chassis/`，因此多个 clone 还不能成为等价控制端。

## 16. 人类 Owner 接管

主要内容包括 Owner 与 Master Root 的区别；何时允许 `owner apply`；为什么人类修改后不能预先 commit；静止期、默认分支和 formal baseline 检查；禁止修改的受控路径；签名审计记录、`owner history`、轮换与恢复。

## 17. CLI 命令参考

主要内容包括全局参数；auth、project、template、artifact、mission、task、work、review、integrate、publish、owner、doctor、verify、recover、explain 等命令；按角色和生命周期阶段组织示例，机器 schema 仍由 CLI 提供。

## 18. 错误处理与故障排查

主要内容包括稳定错误码与 diagnostic category；认证失败、scope 越界、revision 冲突、检查失败、worktree 损坏、远端偏离和 integrity blocked 的排查顺序；CLI 拒绝后应停止什么、可以安全重试什么、何时必须交给 Master。

## 19. 安全模型与已知风险

主要内容包括守规 Agent 威胁模型；共享系统用户时秘密隔离失效；长效 credential 的暴露面；旧 trust 副本、被替换 CLI、聊天记录和剪贴板风险；当前支持的 scope、有效期、回收和验证能力；未来的短期密钥、钥匙串与 credential broker 方向。

## 20. 协议、版本与兼容性

主要内容包括 API、Event、Trust、Role Policy 和 Bootstrap schema 版本；签名与 digest 的字节协议；golden vectors；旧项目拒绝策略；未来协议升级、迁移工具和非 Git backend 需要满足的兼容边界。

## 21. 示例项目、自动化与 FAQ

主要内容包括 Greenfield、Brownfield、Owner 接管、Reviewer 退回、预算越界、并行 Task、崩溃恢复和远端发布的完整示例；cron/automation 模板；常见误解；实验性全自动 Master 模式的成功与失败案例；如何提交高质量 Issue。

## 22. 测试记录、已知问题与改进方向

主要内容包括各版本的测试环境、执行过程、命令、结果与失败记录；已知问题的复现条件、影响范围、临时处理方式和当前状态；根据测试结果评估安全边界、控制状态同步、backend 扩展、使用体验、性能和可维护性，并记录后续改进方向。
