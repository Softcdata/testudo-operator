# Design: TraceID 传递与统一方案

## Context
目前 V1 控制器（如 `AppBackup`）已经实现了 TraceID 的提取和传播，但 V2 控制器（`DisasterInstance` 等）尚未统一实现。这导致在分布式追踪时无法串联 V2 流程中的操作，尤其是在跨组件调用（Server -> Operator -> Sub-Controller）时日志链路断裂。

## Goals
1.  **统一提取 (Extraction)**: 所有 V2 控制器必须尝试从 CRD 的 Annotation 中提取 TraceID。
2.  **上下文注入 (Injection)**: 必须将 TraceID 注入 `context.Context` 和 `logr.Logger`。
3.  **下游传播 (Propagation)**: 在创建子资源（如 `DataSync`, `ResourceSync`, `AppBackup`, `AppRestore`）时，必须将 TraceID 传递给子资源，形成完整的追踪链。

## Design Details

### 1. 键值定义
复用项目中现有的常量定义（位于 `pkg/metadata` 或 `internal/controller`）：
*   **Annotation Key**: `testudo.softcdata.com/trace-id` (对应常量 `AnnotationTraceID`)
*   **Log Key**: `trace_id` (统一使用下划线命名)
*   **Context Key**: 用于在函数间传递 TraceID 的 Context Key 类型。

### 2. Reconcile 入口标准化
在所有 V2 控制器的 `Reconcile` 方法入口处，执行以下标准逻辑：

```go
// 1. 获取 TraceID
traceID := instance.Annotations[AnnotationTraceID]

// 2. 注入 Logger (结构化日志)
// 注意：即使为空也可以记录，或者选择性不记录
if traceID != "" {
    log = log.WithValues("trace_id", traceID)
}

// 3. 注入 Context (用于传递给下游函数)
if traceID != "" {
    ctx = context.WithValue(ctx, TraceIDKey, traceID)
}
```

### 3. 子资源传播策略

#### 3.1 创建新资源 (Create)
在创建子资源时，**必须**显式复制 TraceID 到 `Annotations`。

#### 3.2 触发已有资源 (Update)
对于长期存在的子资源（如复用的 `AppBackup`，由 `DataSync/ResourceSync` 管理），每次触发新动作（如设置 `Spec.Action`）时，**必须更新 Annotations**。

**原因**: `AppBackup` 是长期对象，可能上次同步是 TraceID-A，本次是 TraceID-B。如果不更新，Velero Backup 将继承旧的 TraceID-A。

**示例 (DataSync 触发已存在的 AppBackup):**
```go
func (r *DataSyncReconciler) executeSync(...) {
    // ... 获取 existing AppBackup ...

    // 触发新动作前，更新 TraceID
    currentTraceID := dataSync.Annotations[AnnotationTraceID]
    if appBackup.Annotations == nil {
        appBackup.Annotations = make(map[string]string)
    }
    appBackup.Annotations[AnnotationTraceID] = currentTraceID
    
    // 设置动作
    appBackup.Spec.Action = &disasterv1.BackupAction{Type: "Backup", RequestAt: metav1.Now()}
    
    // 提交更新
    if err := r.Update(ctx, appBackup); err != nil { ... }
}
```

### 4. 涉及的完整链路
*   `DisasterOperation` (Log only)
*   `DisasterInstance` -> `DataSync` / `ResourceSync` (Updates on spec change)
*   `DataSync` -> `AppBackup` (Update Action + TraceID) -> `Velero Backup`
*   `DataSync` -> `AppRestore` (Always New) -> `Velero Restore`
*   `ResourceSync` -> `AppBackup` (Update Action + TraceID) -> `Velero Backup`
*   `ResourceSync` -> `AppRestore` (Always New) -> `Velero Restore`
*   `ResourceSync` -> `ConfigMap` (Update data + TraceID)

## Risks
*   **覆盖风险**: 在更新子资源时，注意不要意外清除现有的 Annotations。应使用 Merge 或仅在 TraceID 变更时更新。
*   **一致性**: 必须确保 V2 所有组件使用完全相同的 Key 常量。
