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

- Event V2 已改为严格最小 payload、确定性 reducer 和 State/transition validator；持有合法角色私钥的主体不能再借自己的 action 写入任意 State delta。仍需通过 operation journal 把 Git 副作用纳入同一恢复协议。
- Git checkout/commit/merge 与 state/event 已纳入项目写锁和 operation journal；崩溃恢复只补全 journal 中预先签名且与 Git 精确一致的事件，不对正式分支隐式 reset/force。
- integration 已验证 task branch tip 等于获批 `HeadCommit`，在临时候选 worktree 合并精确提交并重跑 checks；正式分支推进和 `integration.applied` 由同一 journal 恢复协议保护。
- snapshot digest 目前绑定路径和普通文件内容，但没有完整绑定 symlink 目标、文件模式和其他 Git index 元数据。allowed-path 与快照检查需要改为 Git tree/index 级实现。
- 不带 credential 的 `doctor` 仍只能证明 `.chassis` 内部自洽；有能力整体替换项目控制目录的主体可以构造另一套自洽 Root。正式门禁必须传入 Master 分发的 credential，后续可增加 OS Keychain/可信 Root store。

优先级中：

- 长期 credential 泄漏后在回收前持续有效，同一系统用户下没有秘密隔离；这是 Master 已接受并在 README 明示的 v0.1 取舍。下一版可增加 rotate、TTL、Task/submission scope 和 broker。
- `trust.yaml` 更新与 credential 文件写出不是单个事务，且 trust version 与 workflow revision 分离。应增加授权 journal、原子签发协议和独立 trust revision 输出。
- Event V2 不再保存完整 State；`state.yaml` 是事件序列的可重建投影。长期项目如出现重放性能问题，再增加带链锚的周期 snapshot。
- acceptance command 使用安全但有限的 argv 切分，不支持引号、环境变量或管道。应改为模板中的结构化 argv/env/timeout，而不是默认开放 shell。
- Mission block 保留 Task 原状态，但已关闭 Developer、Reviewer 和 integration 的推进许可；恢复 Mission 后合法 Task 才能继续。
- 设计变更、Task release/supersede、transactional dry-run、credential rotate/TTL 和远端 publish adapter 尚未实现；CLI 文档已把它们标为后续范围。

## 建议的下一步

下一步把 Developer 工作区迁移到每 Task 独立 worktree，并把快照升级为 Git tree/index 摘要；之后完成授权 journal、resume/supersede 和 credential rotate/TTL。远端 publish adapter 仍在本地事务边界完全稳定后接入。
