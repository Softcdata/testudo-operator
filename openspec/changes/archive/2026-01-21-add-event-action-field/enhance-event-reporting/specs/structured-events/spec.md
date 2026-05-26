# 计算规范变更：结构化事件报告

## ADDED Requirements

### 事件消息格式格式规范
#### Scenario: 任务达成终态时的标准化输出
- **Requirement**: 事件消息必须遵循前缀标签格式：`[Task: <NAME>] [Status: <STATUS>] [Duration: <DURATION>] [User: <USER>] <MESSAGE>`。
- **Requirement**: `DURATION` 必须采用 `1h2m3s` 类似的人类可读格式。
- **Requirement**: `STATUS` 必须为 `Success`, `Failed` 或 `Canceled` 之一。

### 状态机集成
#### Scenario: 避免重复发射事件
- **Requirement**: Operator 必须确保每个任务（或计划任务的一个实例）仅在进入终态时发射一次此类结构化历史事件。
- **Requirement**: 若底层 Velero 资源缺失时间戳，Operator 必须降级使用资源创建时间/当前时间进行估算。
