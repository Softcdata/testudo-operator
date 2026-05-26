## ADDED Requirements

### Requirement: 状态迁移事件语义一致且兼容现有接口
系统必须 (MUST) 为关键状态迁移提供统一事件语义，并在不改变现有全局事件接口响应结构的前提下输出迁移上下文。

#### Scenario: Operator 发射状态迁移上下文
- **WHEN** 核心资源（如 `DisasterInstance`、`DisasterOperation`、`DataSync`、`ResourceSync`、`DisasterGroup`）发生关键状态迁移
- **THEN** 结构化事件消息必须 (MUST) 包含 `fromState`、`toState`、`reason`
- **AND** 事件 `Reason` 必须 (MUST) 仍使用 `ExecutionStarted`、`ExecutionProgress`、`ExecutionFinished` 三类规范值
- **AND** 迁移上下文字段缺失时必须 (MUST) 使用可诊断的兜底值，禁止静默丢弃迁移信息

#### Scenario: Server 聚合与串流保留迁移上下文
- **WHEN** 服务端处理全局事件列表或 watch 串流
- **THEN** 必须 (MUST) 在保持现有响应结构不变的前提下透传或映射 `fromState`、`toState`、`reason`
- **AND** 不得 (MUST NOT) 因迁移上下文字段存在而改变现有字段命名与层级
- **AND** 客户端未消费迁移上下文时，原有展示与流程必须 (MUST) 保持兼容
