# 安装、平台与版本同步

CHASSISS 仓库同时保存源码、独立 CLI 发行文件和 Agent Skill。

```text
chassiss/
├── src/
├── cli/
└── skills/chassiss/
```

`src/` 是 Go 源码根目录。`cli/` 保存独立发行二进制。`skills/chassiss/` 保存 Agent 使用的 Skill 和同版本二进制。

## 支持平台

当前发行文件包含以下目标。

| 系统 | 架构 | 路径 |
| --- | --- | --- |
| macOS | Intel | `darwin-amd64/chassiss` |
| macOS | Apple Silicon | `darwin-arm64/chassiss` |
| Linux | x86-64 | `linux-amd64/chassiss` |
| Linux | ARM64 | `linux-arm64/chassiss` |

Windows 构建无法提供当前项目写操作所需的 advisory lock。CLI 会以 `CHS-LOCK-UNSUPPORTED` 拒绝写操作。

## 安装 Skill

复制完整的 `skills/chassiss/` 目录。保留其中的 `SKILL.md`、`agents/`、`bin/` 和 `SHA256SUMS`。

Agent 必须按照当前系统与架构选择 `bin/<os>-<arch>/chassiss`。Skill 明确要求使用随包二进制，避免从 `PATH` 误用不同版本的程序。

macOS 可以通过以下命令查看系统架构。

```text
uname -s
uname -m
```

常见映射如下。

| `uname -s` | `uname -m` | 目录 |
| --- | --- | --- |
| Darwin | arm64 | `darwin-arm64` |
| Darwin | x86_64 | `darwin-amd64` |
| Linux | aarch64 | `linux-arm64` |
| Linux | x86_64 | `linux-amd64` |

## 校验二进制

在 `skills/chassiss/bin/` 中执行校验。

macOS 使用 `shasum`。

```text
shasum -a 256 -c SHA256SUMS
```

Linux 使用 `sha256sum`。

```text
sha256sum -c SHA256SUMS
```

CLI 与 Skill 中相同平台的二进制应具有相同 SHA-256。

## 从源码构建

源码要求 Go 1.23 或更高版本。

```text
cd src
go test ./...
go test -race ./...
go vet ./...
go build ./...
```

发行构建使用静态配置和可复现路径设置。

```text
CGO_ENABLED=0 go build \
  -trimpath \
  -buildvcs=false \
  -ldflags="-s -w" \
  ./cmd/chassiss
```

生成发行文件后需要更新两个 `SHA256SUMS`，并确保 `cli/` 与 Skill 中的副本一致。

## Greenfield 初始化

未传 `--existing` 时，CLI 创建目标目录并初始化 `main` 分支。新项目会写入 `.gitignore`，将 `.chassis/` 排除在 Git 提交之外。

如果目标目录已经是包含提交的 Git 根目录，CLI 会要求使用 `--existing`。

## Brownfield 接管

`--existing` 要求目标路径本身是 Git 根目录，并满足以下条件。

- worktree 干净
- 至少存在一个提交
- 当前分支可以确定
- 目标目录尚未包含 `.chassis/`

CLI 保留已有历史与默认分支，并把 `.chassis/` 写入 `.git/info/exclude`。该操作不会修改项目已有 `.gitignore`。

## 当前 Git backend

v0.3 的 `content_backend` 固定为 `local-git`。Git 提供以下能力。

- formal baseline
- Task branch
- linked worktree
- 文件范围与 diff 统计
- submission commit
- merge candidate
- publish

Agent 不需要直接执行 Git 生命周期命令。CLI 负责创建 branch、worktree、commit 和 merge。

## 版本同步

`publish` 支持 `github`、`gitlab` 和 `remote-git` target。三种 target 当前都使用 Git 远端协议，并把正式 baseline fast-forward 推送到目标 branch。

远端保存项目内容版本。`.chassis/` 保持本地控制状态，因此以下拓扑最稳妥。

- 所有写操作指向同一个受控项目实例
- 其他机器只读取或同步正式代码版本
- Agent 通过远程执行、共享受控主机或未来的控制状态同步机制参与

独立 clone 不能单独恢复完整 Mission、Task、Review 和 trust 状态。详细限制参见 [Publish 与多机器协作](15-publish-and-multi-machine.md)。
