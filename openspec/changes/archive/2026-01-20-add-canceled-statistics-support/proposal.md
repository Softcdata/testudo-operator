# Proposal: 添加取消状态统计支持

## Why

当前，`AppBackup` 和 `AppRestore` 控制器生成的 `BackupRestoreStatistics` 没有正确统计 "Canceled" (已取消) 操作：
1.  **AppRestore**: `Cancelled` 阶段被错误地映射为了 `Failed`。
2.  **AppBackup**: 统计信息是通过列出存活的 Velero Backup 资源来计算的。由于被取消的备份通常会从 Velero 中删除，因此它们完全丢失了统计。

这种差异导致 `disaster-server` (以及 UI) 对取消操作的计数显示不正确或缺失，尽管 `AppBackup` 的历史记录实际上已经正确跟踪了它们。

## What Changes

1.  **AppRestore Statistics Mapping**:
    *   更新 `AppRestoreReconciler.syncStatistics`，将 `PhaseCancelled` 映射到 `Statistics.Canceled`，而不是 `Statistics.Failed`。

2.  **AppBackup Statistics Calculation**:
    *   更新 `AppBackupReconciler.syncStatistics`，基于 `AppBackup.Status.History` 而非列出 Velero Backups 来计算统计数据。
    *   遍历 `History` 记录并基于 `ManagedStatus` 进行计数：
        *   `LastBackupStatusCanceled` -> `Canceled`
        *   `LastBackupStatusCompleted` -> `Completed`
        *   `LastBackupStatusFailed` -> `Failed`
        *   `LastBackupStatusInProgress` -> `InProgress`
    *   这确保了即使是已删除（取消）但保留在历史记录中的备份也能被统计到。

## Impact

*   **AppBackup Controller**: `syncStatistics` 方法的数据源将变更。
*   **AppRestore Controller**: `syncStatistics` 方法的映射逻辑将变更。
*   **API/UI**: 统计数据将能准确反映已取消的操作。
