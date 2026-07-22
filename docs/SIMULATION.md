# CHASSISS v0.1 推演记录

日期：2026-07-22
平台：macOS arm64
二进制：`bin/chassiss-darwin-arm64`
SHA-256：`1ee519401b7cdf0b96e49b052081525c42a4bdb6c96f7f1ecff9ca4883943b60`

## 结果

- Greenfield：从空目录初始化 Go greeting CLI，完整执行设计、Master 接受、Mission 激活、Task claim、Developer check/submit、独立 review、local integration 和 Mission 验收；最终 revision 19，签名事件和 Git worktree 验证通过。
- Brownfield：接管已有 `trunk` 分支和历史的 Go calculator，在不修改模块身份和既有 `Add` API 的前提下增加 `Multiply`；完整闭环后 revision 18，原历史保留，签名事件和 Git worktree 验证通过。
- 长期 credential：同一角色 credential 跨整个 Mission 和多个 CLI 调用持续有效；额外签发并回收一个测试 credential 后，后续写动作稳定返回 `CHS-AUTH-REVOKED`。
- 四个角色 Skill 均通过 system `skill-creator` 的 `quick_validate.py`。
- Go 验证：`go test ./...`、`go test -race ./...` 与 `go vet ./...` 通过。

## 推演发现并已修复

1. Greenfield 位于另一个 Git 仓库目录内时可能误用父仓库。现在要求目标自身成为独立仓库，并用真实路径比较处理 macOS `/var` 与 `/private/var` 别名。
2. `work diff` 对未跟踪文件只列路径、不显示内容。现在同时渲染 tracked 与 untracked diff。
3. Git porcelain 首行前导空格被清理，`calc/calc.go` 曾被误读为 `alc/calc.go`，范围门禁因此正确拒绝提交。现在不解析 porcelain 列偏移，改用 Git 的 tracked/untracked 文件列表并有回归测试。
4. 已通过的 check 原先没有绑定最终文件内容。现在保存 worktree snapshot digest；检查后改文件会返回 `CHS-WORK-CHECKS-STALE`。
5. submission digest 原先未覆盖 handoff 和 check 记录。现在绑定完整不可变 manifest，并在 review 时重算。
6. 读检查原先只验证项目内部签名。现在 `doctor/verify` 可用 Master Root 或角色 credential 锚定 Root fingerprint，并明确区分无 credential 的内部自洽检查。
7. 命令原先可能忽略拼错的 option。现在按命令拒绝未知 flags/options。
8. 所有领域写入默认锁定其读取到的 revision；显式旧 revision 会在产生 Git 副作用前拒绝。
9. 每个常规命令现在先验证 `state.yaml` 等于签名事件投影，避免把手工状态修改“洗入”下一事件；事件验证同时检查 event type 是否属于签名 credential 的 action 权限、时间是否单调、是否早于签发或晚于回收。

## 剩余风险与下一轮建议

优先级高：

- Event V2 已改为严格最小 payload、确定性 reducer 和 State/transition validator；持有合法角色私钥的主体不能再借自己的 action 写入任意 State delta。
- Git checkout/commit/merge 与 state/event 已纳入项目写锁和 operation journal；崩溃恢复只补全 journal 中预先签名且与 Git 精确一致的事件，不对正式分支隐式 reset/force。
- integration 已验证 task branch tip 等于获批 `HeadCommit`，在临时候选 worktree 合并精确提交并重跑 checks；正式分支推进和 `integration.applied` 由同一 journal 恢复协议保护。
- 每个 Active Task 使用独立 linked worktree；主 worktree 不再被 `work open` 切换。Task 状态绑定路径、Git worktree 身份、branch 和绑定摘要，删除、移动、错分支或重复绑定都会安全拒绝。
- snapshot digest 基于临时 Git index 的 tree 和 stage manifest，覆盖 symlink、executable bit、文件模式与 rename/type change，不修改真实 index。
- acceptance check 使用结构化 argv、相对 cwd、显式 env 和 timeout；默认不经过 shell，cwd 经符号链接解析后不能逃出 Task worktree。
- Task assign/claim 验证当前有效 Developer grant 并记录分派 grant；同 actor 可在旧 credential 回收后使用新 credential 继续。Task block 释放调度占用，resume 重新验证依赖、WIP、路径、Git/worktree、submission 和 Review 证据。
- `auth issue/revoke` 已使用统一项目锁、独立 trust revision 和授权 journal；签发按“临时 credential → trust 提交 → credential 发布”恢复，并发冲突不会丢失 grant/revocation。
- Task release 只清理无 submission 且干净位于 baseline 的 worktree/branch，并由 operation journal 恢复；Master cancel 保留取证状态；supersede 使用已接受的新 Task ID 和显式替换链，旧契约不被改写。
- credential 默认继续长效；显式签发时可增加 not_before/expires_at、Task/Mission/submission 白名单，以及 Reviewer/Integration digest/head/baseline scope。CLI 执行和历史事件重放都会验证这些限制。
- 不带 credential 的 `doctor` 仍只能证明 `.chassis` 内部自洽；有能力整体替换项目控制目录的主体可以构造另一套自洽 Root。正式门禁必须传入 Master 分发的 credential，后续可增加 OS Keychain/可信 Root store。

优先级中：

- 默认长期 credential 泄漏后在回收前持续有效，同一系统用户下没有秘密隔离；这是 Master 已接受并在 README 明示的 v0.1 取舍。高风险动作现在可显式使用 TTL/resource scope，下一版仍可增加 rotate 和 broker。
- Event V2 不再保存完整 State；`state.yaml` 是事件序列的可重建投影。长期项目如出现重放性能问题，再增加带链锚的周期 snapshot。
- Mission block 保留 Task 原状态，但已关闭 Developer、Reviewer 和 integration 的推进许可；恢复 Mission 后合法 Task 才能继续。
- Mission 级设计变更、transactional dry-run 和 credential rotate 命令尚未实现；CLI 文档已把它们标为后续范围。
- 远端 publish adapter 已实现：仅 fast-forward push 当前本地正式 baseline 的精确 SHA，publication 与 integration 分开记录；远端失败不改变本地状态，push 后崩溃由 publish journal 幂等恢复。

## 建议的下一步

先由 Master 复核现有安全边界和命令语义，再决定是否投入无人值守试运行。远端继续只同步正式代码，不成为工作流事实源。
