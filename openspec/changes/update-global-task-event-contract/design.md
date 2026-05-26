# 设计：durable history 所需的 operator 事件发射契约

## 1. 设计目标
- 为 server 的 durable history 提供稳定可依赖的事件发射不变量。
- 不破坏 operator 已有的全局事件格式、方向语义和降噪策略。
- 把“结构化任务事件”和“诊断事件”在历史链路中的职责彻底分开。

## 2. 当前基础
operator 当前已经具备以下基础：
- `pkg/helper/event_reporter.go` 统一发射结构化 JSON 事件
- payload 已稳定包含 `task/status/message`，并默认补 `cluster/user/traceId`
- `Source.Component` 固定为 `disaster-operator`
- 结构化事件带 `testudo.softcdata.com/task-event=true`
- 多个核心控制器已接入 `ReportTaskStartedWithClient / Progress / FinishedWithClient`

这意味着本 proposal 不需要重新设计事件格式，只需要把 durable history 真正依赖的不变量正式化。

## 3. Durable History 依赖的发射不变量

### 3.1 稳定字段集
对进入 durable history 主链路的结构化任务事件，要求：
- `task` 必填
- `status` 必填
- `message` 必填
- `traceId` 必须稳定输出；无 trace 时输出显式占位值 `-`
- `cluster` / `user` 必须稳定输出；缺省时使用既定占位值
- `involvedObject.uid` 必须可用；为空时仅允许作为历史能力降级场景
- `Source.Component` 固定为 `disaster-operator`

### 3.2 单次执行连贯性
同一次执行的：
- `ExecutionStarted`
- `ExecutionProgress`
- `ExecutionFinished`

必须共享同一条 identity 语义：
- 同一 `task`
- 同一 `traceId` 语义
- 同一 owner resource identity

允许 message 随步骤变化，但不允许在一次执行中“traceId 漂移”或“started 有 trace、finished 无 trace”。

### 3.3 终态完整性
只要某次执行进入了 durable history 主链路：
- 成功路径必须补 `ExecutionFinished`
- 失败路径必须补 `ExecutionFinished`
- 删除/清理路径必须在对象丢失前补 `ExecutionFinished`

否则 server 会得到永久悬挂的 InProgress 历史记录。

## 4. 主链路与诊断链路分工

### 4.1 结构化任务事件
- 用于 server durable history
- 用于 server 实时 task aggregation
- 必须通过 `WithClient` 路径发射带 label 的 event

### 4.2 诊断事件
- 用于调试和补充运维信息
- 可继续使用 `Recorder.Event*`
- 不进入 durable history 主链路
- 不得代替结构化任务事件充当 Started/Finished 事实来源

## 5. 兼容策略
- 保留旧 `ReportTaskStarted / ReportTaskFinished` 作为历史兼容辅助
- 但对所有希望进入 durable history 的任务路径，必须迁移到 `WithClient` 发射路径
- 对尚未迁移的旧路径，只能视为诊断级可观测性，而不是 durable history 合规路径

## 6. 风险
- 若 controller 仍混用旧事件路径，历史查询会出现部分任务缺失。
- 若 traceId 语义在一次执行中漂移，server 会把同一次执行错误拆成多段。
- 若删除前未发终态，历史会长期悬挂。

## 7. 验证策略
- 单测：`event_reporter.go` 对默认值、Source.Component、trace 连贯性的覆盖
- 控制器回归：重点覆盖 `disasteroperation`、`resourcesync`、`disasterdrill`、`cluster`
- 联调：server durable history 对 started/progress/finished 的归并结果稳定
