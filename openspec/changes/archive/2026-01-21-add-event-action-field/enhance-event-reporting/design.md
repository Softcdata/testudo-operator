# 设计方案：增强结构化事件发射

## 设计思路
利用 `record.EventRecorder` 发射带有特定格式的事件消息。通过在 `pkg/helper` 中建立通用的 `EventReporter` 辅助类，统一管理事件的结构化消息构造、耗时计算和触发人提取逻辑。

## 1. 结构化消息规范
所有核心任务在 **生命循环的关键点** 必须发射符合以下格式的消息：

**格式**: `[Task: %s] [Status: %s] [Duration: %s] [User: %s] %s`

- **Task**: 格式化的任务名称。
- **Status**: `InProgress`, `Success`, `Failed`, `Canceled`。
- **Duration**: 格式化后的耗时。终态任务由 Operator 计算；`InProgress` 任务固定填 `-`（由 Server 端动态通过当前时间计算显示）。
- **User**: 触发执行的用户。
- **Message**: 补充描述信息。

## 2. 核心逻辑实现

### 2.1 通用 Helper (`pkg/helper/event_reporter.go`)
提供 `ReportTaskStarted` 和 `ReportTaskFinished` 方法。

### 2.2 任务名称组装说明
- **AppBackup (单次)**: `AppBackup: [Object Name] ([Velero Backup Name])`
- **AppBackup (计划)**: `AppSchedule: [Object Name] (Instance: [Velero Backup Name])`
- **AppRestore**: `AppRestore: [Object Name]`

### 2.3 耗时计算策略 (仅针对 Finished)
耗时应从底层资源的状态中提取：
- **备份**: `Duration = veleroBackup.Status.CompletionTimestamp - veleroBackup.Status.StartTimestamp`
- **恢复**: `Duration = veleroRestore.Status.CompletionTimestamp - veleroRestore.Status.StartTimestamp`

## 3. 发射锚点 (Anchors)

### 起始点 (ExecutionStarted)
- **AppBackup**: 在 `PendingHandler` 成功创建 Velero Backup 资源后。
- **AppRestore**: 在 `PendingHandler` 成功创建 Velero Restore 资源后。

### 终点 (ExecutionFinished)
- **AppBackup**: 在 `ReadyHandler` 中监测到 Velero Backup 状态进入 `Completed / Failed / PartiallyFailed` 时。
- **AppRestore**: 当 `AppRestore.Status.Status` 变为 `Succeeded / Failed / Cancelled` 时。
