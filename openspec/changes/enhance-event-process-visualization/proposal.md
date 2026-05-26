# Change: Enhance Global Event Visualization (Timeline)

## Why
目前 Global Events 仅展示最终状态（"Started" -> "Finished"），用户无法观测长耗时任务（如 Velero 备份、集群创建）的中间执行过程。虽然 Server 端聚合逻辑正确（单次操作聚合为一个 Event），但缺乏对过程和时间轴的展示，导致可观测性不足。

## What Changes
- **Spec**: 更新 Global Event 标准，在聚合模型中引入 `Timeline` 概念。
- **Operator**:
  - 新增 `ReportTaskProgress` 辅助方法，支持发射 `InProgress` 状态的中间进度事件。
  - 在 Controller 关键节点（如 Velero Backup 创建后、等待状态中）上报进度。
- **Server**:
  - 更新 `TaskEvent` 数据模型，新增 `Timeline` 字段（包含 `Time`, `Status`, `Message`）。
  - 优化聚合逻辑：将同一 TaskID 的所有历史事件按时间顺序记录到 `Timeline` 中，而不是单纯覆盖 `Message`。

## Impact
- **Operator**: 需要修改 `pkg/helper/event_reporter.go` 和相关 Controller。
- **Server**: 需要修改 `internal/apis/event/v1/list.go` 和 `types.go`。
- **UI**: 前端需要适配新的 `Timeline` 字段来展示进度条或日志流（本次变更仅涉及后端 API）。

## Breaking Changes
- **API 变更**: `TaskEvent` 响应结构新增 `timeline` 字段。此为向后兼容变更，不破坏现有字段。
