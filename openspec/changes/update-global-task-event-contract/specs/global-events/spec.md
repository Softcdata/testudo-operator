## ADDED Requirements

### Requirement: Durable history 主链路事件必须保持稳定 identity 契约
系统必须 (MUST) 为进入 durable history 主链路的结构化任务事件保持稳定的 identity 契约。

#### Scenario: 同一次执行的 Started/Progress/Finished 保持同一 identity 语义
- **Given** 一次长耗时任务已经开始并会发射 `ExecutionStarted`、`ExecutionProgress` 与 `ExecutionFinished`
- **When** operator 为这次执行持续发射结构化任务事件
- **Then** 同一次执行中的事件必须保持一致的 `task`、`traceId` 与 owner resource identity 语义
- **And** `Source.Component` 必须为 `disaster-operator`
- **And** 不得在同一次执行中发生 trace 语义漂移

#### Scenario: 无外部 trace 时保持显式占位值
- **Given** 一次任务执行没有外部请求 trace
- **When** operator 发射 durable history 主链路事件
- **Then** operator 必须输出稳定的显式占位值，而不是在不同事件间时有时无

### Requirement: Durable history 主链路必须依赖带 label 的结构化任务事件与完整终态
系统必须 (MUST) 使用带 `testudo.softcdata.com/task-event=true` 的结构化任务事件作为 durable history 主链路，并保证终态事件完整。

#### Scenario: 删除或失败路径写入终态事件
- **Given** 一个纳入 durable history 的任务执行进入失败、删除或清理路径
- **When** operator 即将结束该执行或移除相关对象
- **Then** operator 必须先发射可持久化的 `ExecutionFinished` 事件
- **And** 不得在对象消失或 Finalizer 移除后再补发终态事件

#### Scenario: 诊断事件不能替代历史主链路事件
- **Given** 控制器同时需要输出结构化任务事件和诊断事件
- **When** server 依赖这些事件构建 durable history
- **Then** operator 必须提供带 label 的结构化任务事件作为事实来源
- **And** `Recorder.Event*` 诊断事件不得替代 durable history 主链路
