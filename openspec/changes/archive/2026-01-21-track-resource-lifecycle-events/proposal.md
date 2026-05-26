# Proposal: 追踪资源生命周期与变更历史事件

## Why (背景与目标)
当前系统主要关注任务执行（如备份、恢复操作）的开始与结束事件，缺乏对资源本身（AppBackup, AppRestore）生命周期的追踪。为了提供完整的审计能力和变更历史记录，我们需要能够捕捉资源的创建、关键配置更新以及删除事件。这对于排查配置错误和满足合规性要求至关重要。

## What Changes (变更内容)
1.  **AppBackup 控制器增强**:
    - **创建事件**: 在资源首次被 Reconcile 且无 Status 时发出。
    - **更新事件**: 能够在 Spec 发生关键变更（如 Schedule, Template, Action）时发出事件。
    - **删除事件**: 在 Deleting Handler 处理开始时发出。
2.  **AppRestore 控制器增强**:
    - 类似地，添加创建和删除事件（AppRestore 通常是一次性的，更新可能较少，但需覆盖）。
3.  **事件标准化**:
    - 使用统一的 Task Name 格式，例如 `Resource: <Kind> <Name> <Action>`。
    - 确保事件包含 TraceID 以串联操作。

## Impact (影响范围)
- **disaster-operator**:
    - `internal/controller/appbackup/*`
    - `internal/controller/apprestore/*`
- **disaster-server**: 无需代码变更，能够自动摄取新的 K8s Event 并展示在全局历史中。

## Implementation Details
- 使用 `pkg/helper.ReportTaskStarted` 和 `ReportTaskFinished` (或者 ReportEvent 如果有) 来发送这些事件。
- 为了区分“操作任务”和“生命周期事件”，我们可能需要在 TaskName 上做区分，或者通过 Status 字段。
- **Action (Type)** 定义:
    - `Create`: 资源创建。
    - `Update`: 资源 Spec 更新。
    - `Delete`: 资源删除。
