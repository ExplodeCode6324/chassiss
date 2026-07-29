# CHASSISS

中文 | [English](README.en.md)

CHASSISS 是一个以签名 Git 转换为共享账本、由 CLI 独占 Git 工作流的多 Agent
项目协议。Architecture 定义长期结构，Taskbook 定义一轮工作合同，State 只保存
验证下一次转换所需的最小投影；Root 与 Grant 决定谁能对什么 Task/Resource
执行哪种 Action。

当前仓库是面向 `chassiss/v1` 的全新实现，不兼容旧项目的 v0.x 控制目录、
credential 或 Mission 模型。旧仓库只作为只读的文档组织参考。协议文档当前仍是
“待 Master 复核”，因此本实现是 lock candidate，而不是已经冻结的正式 v1。

## 核心边界

- `refs/heads/main` 是唯一权威共享主线；每个 main commit 都是 SSH 签名的
  Transition。
- `.chassiss/state.json` 是 Reducer 生成的 canonical JSON，不能手工编辑。
- `docs/architecture.yaml` 与当前 `docs/taskbook.yaml` 通过受控命令变更。
- Agent 只在 CLI 返回的 managed worktree 中、按 Task `writes` 修改普通文件。
- Check 是机械结果，Review 是 Reviewer 的语义判断，二者不能互相替代。
- Integration 是 exact candidate 的双 parent commit；relevant/unknown drift 必须
  重新 Review。
- private key、checkpoint、pending operation、worktree registry 与 cache 只存在
  于仓库外的 Local State。
- 不依赖 GitHub/GitLab、外部数据库、外部 CI 或特定托管平台。

## 构建

需要 Go 1.24、Git 2.34+（支持 SSH commit signing）和 `ssh-keygen`。

```text
go build -o bin/chassiss ./cmd/chassiss
go test ./...
go vet ./...
./bin/chassiss version --json
./bin/chassiss help --json
```

也可以使用 `make build`、`make test`、`make check`。发布构建通过 ldflags 注入
版本、构建摘要和发布身份；详见 [发布与测试指南](docs/cn/11-测试与发布.md)。

## 最小本地流程

先复制并按项目实际情况修改 `docs/templates/architecture.yaml` 与
`docs/templates/taskbook.yaml`，然后在尚无 Git history 的项目目录执行：

```text
chassiss key generate --id KEY-ROOT-01 --actor master --json
chassiss init \
  --project PRJ-EXAMPLE \
  --architecture /outside/project/architecture.yaml \
  --taskbook /outside/project/taskbook.yaml \
  --root-key KEY-ROOT-01 \
  --json
chassiss verify --full --json
chassiss context --json
```

开发者在仓库外生成 key 和 Grant Request；Root 审核 explicit Capability、
Task scope、Resource scope 与 limits 后签发 Grant。Task 工作循环是：

```text
chassiss context TASK-001 --json
chassiss task start TASK-001 --json
chassiss work open TASK-001 --json
# 在返回的 managed worktree 中编辑
chassiss work status TASK-001 --json
chassiss work commit TASK-001 --message "implement TASK-001" --json
chassiss check TASK-001 --json
chassiss submit TASK-001 --json
```

随后由 Reviewer 使用 `review --prepare` 和签名 Report 给出 Verdict，由
Integrator 执行 `integrate`。所有 Tasks terminal 后，Reviewer 填写 Closure
Report，再执行 `taskbook archive`。

不要直接运行会改变协议仓库的 `git add/commit/branch/checkout/worktree/merge/
rebase/reset/push/config`。通用 Agent 操作说明在
[`skills/chassiss/`](skills/chassiss/)；该 Skill 捆绑 macOS/Linux 的
arm64/amd64 CLI，并由 launcher 在执行前校验 manifest digest，但不自行解析
协议 State。Master 调度规范要求每个临时 Agent 使用独立 worktree/Key/Grant：
成功后回收；失败时先用 `attempt abandon` 留下签名记录，再销毁并回收。

## 文档

- [规范索引](docs/README.md)：11 份 normative v1 文档、模板与 lock 流程
- [中文使用指南](docs/cn/README.md)
- [English guides](docs/en/README.md)
- [实现与旧项目差异](docs/12-implementation-differences.md)
- [跨实现 fixtures](fixtures/README.md)

机器可读的完整命令树以 `chassiss help --json` 为准；稳定 Error code、退出码和
response envelope 以规范为准。

## 当前验证状态

本地测试覆盖 canonical JSON、domain-separated digest、严格 YAML、State
不变量、Reducer、SSHSIG、Git object/topology、历史 verifier、Local State、
Task lifecycle、Review/Integration、Taskbook Closure、offline proposal 与
Owner Apply。仓库还包含 14 类跨实现 fixture 目录。

按 Master 要求，本轮没有运行真实 GitHub/网络 remote/系统 secret-store 外部集成
测试。剩余 lock-candidate 差距和安全影响持续登记在
[实现差异文档](docs/12-implementation-differences.md)，不会用 README 宣称掩盖。
