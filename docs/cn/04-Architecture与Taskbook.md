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

Architecture update 只允许没有 active Taskbook 时执行。Taskbook update 只能修改
协议允许的 ready 范围，不能改变 non-ready Task 合同。

全部 Tasks terminal 后，Reviewer 先执行
`taskbook archive --prepare --output <file>`，得到按 exact Tasks 和 completion
criteria 填充 response slot 的 Closure Report，填写后执行
`taskbook archive --report <file>`。CLI 在 exact current main 重新运行全部
workflow Checks，保存签名 Evidence，移动 exact Taskbook blob，并清空 State
Task projection。
