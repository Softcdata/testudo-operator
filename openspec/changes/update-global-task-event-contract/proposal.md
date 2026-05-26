# Change: 为 durable history 补齐全局任务事件发射契约

## Why
随着 server 侧引入 durable history，历史链路对 operator 发射事件的稳定性要求比“仅供实时展示”更严格。

当前 operator 的 `global-events` 能力已经定义了：
- 结构化 JSON 载荷
- `ExecutionStarted / ExecutionProgress / ExecutionFinished`
- task naming
- 运行期方向语义
- 诊断事件限频

但它还没有把以下“面向历史持久化”的要求显式写成契约：
- 同一次执行的稳定 identity 字段必须持续一致
- history 主链路必须走带 label 的结构化 `WithClient` 事件
- 删除/失败等终态事件必须稳定补齐，避免 history 出现悬挂批次

如果不先锁定这些发射端不变量，server 的 durable history 即使落库，也会出现：
- 同一次任务 trace 漂移
- started/progress/finished 无法稳定归并
- 终态缺失导致历史长期停留在 InProgress

## What Changes

### 1. 在现有 `global-events` capability 上补 durable history 发射契约
- 不新建 capability。
- 在现有 `global-events` 上增加“history-friendly event contract”要求。

### 2. 锁定 durable history 依赖的最小稳定字段集
- 对于进入 durable history 主链路的结构化任务事件，要求稳定提供：
  - `task`
  - `status`
  - `message`
  - `traceId`
  - `cluster`
  - `user`
  - `involvedObject.uid`
  - `source.component=disaster-operator`

### 3. 锁定单次执行内的 identity 连贯性
- started/progress/finished 必须保持同一条执行链路上的 `task` / `traceId` / owner resource identity 一致。
- 无外部 trace 时必须输出明确占位值，而不是在同一条执行链路中时有时无。

### 4. 锁定终态完整性
- 对纳入 durable history 的长耗时任务，必须有可持久化的终态 `ExecutionFinished`。
- 删除路径、失败路径、补偿路径都必须在对象丢失前完成终态发射。

### 5. 锁定历史主链路与诊断事件的分工
- durable history 主链路只能依赖带 `testudo.softcdata.com/task-event=true` 的结构化事件。
- `Recorder.Event*` 诊断事件继续保留，但不得替代 durable history 主链路。

## Non-Goals
- 不重新定义 JSON payload 格式。
- 不重写 trace-id 传播机制。
- 不设计 server 侧数据库表、查询 API 或 retention。
- 不把 server 的聚合键逻辑搬到 operator。

## Impact
- Affected specs:
  - `global-events`
- Affected code:
  - `pkg/helper/event_reporter.go`
  - `internal/controller/disasterinstance/*`
  - `internal/controller/datasync/*`
  - `internal/controller/resourcesync/*`
  - `internal/controller/disasteroperation/*`
  - `internal/controller/disasterdrill/*`
  - `internal/controller/cluster_controller.go`
- Cross-repo impact:
  - `disaster-server`：durable history 投影依赖本 contract
  - `cluster-disaster-web`：无需直接消费 operator 变更，但会通过 server 历史查询受益

## Relationship to Existing Changes
- 参考 active changes:
  - `add-v2-event-emission-coverage`
  - `update-global-events-end-to-end-noise-control`
- 本 change 不重写这些 change 已建立的格式、方向语义和降噪约束。
- 本 change 只补“为了 durable history，哪些事件发射不变量必须长期稳定”。
