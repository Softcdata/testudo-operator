# Add Backup Restore Statistics

## Summary
引入 `BackupRestoreStatistics` CRD 和相应的控制器逻辑，用于统计和聚合备份与恢复操作的状态（如成功、失败、进行中等）。这允许用户和上层系统快速获取特定范围（如应用、命名空间、集群）内的备份恢复健康状况。

## Motivation
目前，要了解一个应用或整个集群的备份恢复情况，需要遍历所有的 `AppBackup` 和 `AppRestore` 资源，并检查它们关联的 Velero 资源状态。这在大规模环境中效率低下且难以聚合。
通过引入独立的统计资源 `BackupRestoreStatistics`，我们可以：
1.  提供实时的状态聚合视图。
2.  支持多维度的统计（按应用、按集群等）。
3.  简化监控和告警系统的集成。

## Proposed Changes
1.  **新增 CRD**: `BackupRestoreStatistics`
    - 存储统计数据：`Total`, `InProgress`, `Completed`, `Failed`, `Canceled`, `Unknown`。
    - 记录关联资源和统计事件。
2.  **新增 Helper 库**: `pkg/helper/statistics_helper.go`
    - 提供 `GetOrCreate`, `IncrementCounter`, `TransitionState` 等方法，封装统计逻辑。
    - 支持并发安全的计数器更新。
3.  **集成到 AppBackup 控制器**:
    - 在 `AppBackupReconciler` 中注入 `StatisticsHelper`。
    - 在 `Reconcile` 循环的末尾，调用 `syncStatistics` 方法。
    - `syncStatistics` 方法会：
        - 根据 `AppBackup` 的状态（`Status.Status`）和历史记录（`Status.History`）计算当前的统计数据。
        - 调用 `StatisticsHelper.SyncStats` 将计算结果同步到 `BackupRestoreStatistics` CRD。
        - 统计范围基于 `AppBackup` 的 UID（即每个 AppBackup 实例对应一个 Statistics 对象）。
4.  **集成到 AppRestore 控制器**:
    - 在 `AppRestoreReconciler` 中注入 `StatisticsHelper`。
    - 在 `Reconcile` 循环的末尾，调用 `syncStatistics` 方法。
    - `syncStatistics` 方法会：
        - 根据 `AppRestore` 的当前状态（`Status.Status`）更新统计数据。
        - 由于 `AppRestore` 是一次性操作，统计逻辑相对简单：
            - 创建时：`Total` + 1, `InProgress` + 1
            - 完成/失败时：`InProgress` - 1, `Completed`/`Failed` + 1
        - 统计范围可以基于 `AppRestore` 所属的 `AppBackup`（如果有关联）或者独立的 Scope。

## Implementation Details
- **Scope**: 统计对象基于 `ScopeReference`（如 AppBackup UID）创建，确保统计维度的准确性。
- **State Transition**: 使用 `TransitionState` 方法原子性地处理状态流转（例如从 `InProgress` 变为 `Completed`，会同时减少 `InProgress` 计数并增加 `Completed` 计数）。
- **Aggregation**: 提供 `AggregateStatistics` 方法，支持通过标签选择器聚合多个统计对象的数据。
