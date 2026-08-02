# Architecture 与 Taskbook

Architecture 是跨工作流的长期合同，包含 Module、API、Schema、Dependency 和
Config 五类 Resource。Resource 使用 typed ID，例如 `module:core`，并通过
`requires` 形成无环图。

Taskbook 是一轮工作合同，包含：

- workflow outcome、completion criteria 与 workflow Checks；
- Requirements 和 acceptance；
- Constraints；
- Tasks、依赖、deliverables、Modules、`writes`、`affects`、Checks、limits、
  stop conditions 与 reviewer attention。

YAML parser 只接受安全封闭子集。Duplicate key、float、timestamp、tag、alias、
BOM、CRLF、invalid UTF-8、non-NFC 路径和 unknown core field 都会拒绝。

## 查询

```text
chassiss taskbook show --task TASK-001 --json
chassiss architecture show module:core --json
chassiss architecture requires module:core --transitive --json
chassiss architecture required-by schema:state --transitive --json
chassiss architecture impact schema:state --json
```

## 受控变更

Draft 必须写到 Project 外，同时生成绑定 exact base blobs 的 sidecar：

```text
chassiss taskbook draft --output /outside/taskbook.yaml --json
chassiss taskbook validate --file /outside/taskbook.yaml --json
chassiss taskbook diff --file /outside/taskbook.yaml --json
chassiss taskbook update --file /outside/taskbook.yaml --reason "..." --json
```

Architecture draft sidecar 同时绑定 exact current Architecture；有 active
Taskbook 时也绑定其 exact blob。没有 active Taskbook 时，update 继续产生 rc9 的
`architecture.updated`。若 active Taskbook 的所有 Task 都是
`ready/closed/cancelled/superseded`，且 exact Taskbook 能在候选 Architecture 下
重新验证，update 产生 additive `architecture.updated-compatible`，仍使用
`architecture.update` capability。任一 `active/submitted/approved` Task 都返回
`CHS_TASKBOOK_NOT_QUIESCENT`；不允许换 Key/Grant/Operation ID 规避。成功更新不改
Taskbook、Task 状态或历史 frozen Contract。若候选使 active Taskbook 不兼容，
返回 `CHS_TASKBOOK_INVALID`；Architecture 与 Taskbook 不在一个 Transition 中复合
修改。Taskbook update 仍只能修改协议允许的 ready 范围，不能改变 non-ready
Task 合同。

`architecture validate --file` 只验证 Architecture 自身。使用
`architecture diff --file` 查看 active Taskbook blob、quiescence 和 compatibility
预检；最终以 `architecture update` 中 CLI、Reducer、Verifier 的重复验证为准。

已有项目的 Root-only bootstrap 初始没有 Architecture。使用
`architecture draft --new` 创建外部候选，再由具有
`architecture.establish` 和 global scope 的 Agent 执行
`architecture establish --file ... --reason ...`。Architecture 建立前不能打开
Taskbook 或运行普通开发 Task。

全部 Tasks terminal 后，Reviewer 先执行
`taskbook archive --prepare --output <file>`，得到按 exact Tasks 和 completion
criteria 填充 response slot 的 Closure Report，填写后执行
`taskbook archive --report <file>`。CLI 在 exact current main 重新运行全部
workflow Checks，保存签名 Evidence，移动 exact Taskbook blob，并清空 State
Task projection。
