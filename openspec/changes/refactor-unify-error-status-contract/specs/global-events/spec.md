## MODIFIED Requirements

### Requirement: 事件消息载荷格式 (Event Message Payload Format)
控制器发射的结构化任务事件消息体必须 (MUST) 使用 JSON 载荷，而不是标签拼接字符串。

#### Scenario: 结构化 JSON 载荷
- **WHEN** 控制器写入 `corev1.Event.message`
- **THEN** 消息体必须 (MUST) 是合法 JSON 字符串
- **AND** 载荷字段必须 (MUST) 包含 `task`、`status`、`message`
- **AND** 载荷字段应 (SHOULD) 包含 `cluster`、`user`、`traceId`、`duration`
- **AND** `duration` 仅在 `ExecutionFinished` 事件中填写，进行中事件可为空

#### Scenario: 失败结束事件载荷必须包含可机器识别错误码
- **Given** 控制器发射 `ExecutionFinished` 且任务状态为失败
- **When** 构建事件 JSON 载荷
- **Then** 载荷必须 (MUST) 包含 `errorCode`
- **And** `errorCode` 应 (SHOULD) 与资源 `status.reason`（或最新失败 condition reason）一致
- **And** 载荷中的 `message` 应 (SHOULD) 与资源 `status.message`（或最新失败 condition message）语义一致
