## MODIFIED Requirements

### Requirement: 服务端聚合 (Server-Side Aggregation)
API Server 必须 (SHALL) 基于结构化 JSON 载荷进行任务聚合，并使用可区分批次的复合聚合键。

#### Scenario: 聚合任务
- **WHEN** Server 处理事件列表
- **THEN** 它必须 (MUST) 解析 JSON 载荷中的 `task`、`status`、`traceId` 字段
- **AND** 对于同一任务聚合必须使用复合键：`task + traceId + involvedObject.uid`
- **AND** 当 `traceId` 缺失时，必须使用 `task + involvedObject.uid + 启动锚点` 作为兜底键
- **AND** 对于无法解析为 JSON 的事件，必须忽略，不参与任务聚合

## ADDED Requirements

### Requirement: 资源历史与资源流 Kind 隔离
Server 必须 (MUST) 对资源历史与资源事件流执行 Kind 级别隔离，禁止同名异 Kind 串流。

#### Scenario: 资源历史接口隔离
- **WHEN** 客户端请求 `GET /apis/v1/:resource/:name/history`
- **THEN** 服务端必须按 `:resource` 映射目标 Kind
- **AND** 仅聚合 `involvedObject.name=:name` 且 `involvedObject.kind=目标Kind` 的事件

#### Scenario: 资源流接口隔离
- **WHEN** 客户端请求 `GET /apis/v1/watch/:resource/:name/events`
- **THEN** 服务端必须按 `:resource` 映射目标 Kind
- **AND** 仅推送 `involvedObject.name=:name` 且 `involvedObject.kind=目标Kind` 的事件

### Requirement: Watch 安全与结构化早返回
Server 必须 (MUST) 保证 watch 处理链路不输出敏感头信息，并对非结构化任务事件执行早返回。

#### Scenario: Header 日志安全
- **WHEN** 服务端处理全局事件 watch 请求
- **THEN** 服务端不得输出请求 Header 明文到日志

#### Scenario: 非三类 Reason 早返回
- **WHEN** `ConvertToTaskEventDTO` 收到非 `ExecutionStarted`、`ExecutionProgress`、`ExecutionFinished` 事件
- **THEN** 转换函数必须立即返回 `nil`

### Requirement: 诊断事件统一限频
Operator 必须 (MUST) 为诊断事件提供统一限频策略，并至少覆盖 `disasteroperation` 与 `cluster` 高频路径。

#### Scenario: 高频诊断事件抑制
- **WHEN** 同一对象在限频窗口内重复发射相同 `eventType + reason + message` 的诊断事件
- **THEN** Operator 必须抑制重复发射
- **AND** 窗口外首次事件必须发射

### Requirement: 前端全局通知窗口聚合
Web 前端必须 (MUST) 对全局事件通知执行窗口聚合，降低重复弹窗噪声。

#### Scenario: 相同任务通知聚合
- **WHEN** 在窗口内收到相同 `taskName + reason + status` 的 `ADDED` 事件
- **THEN** 前端不得创建新的 toast
- **AND** 前端必须刷新或累加同一提示
