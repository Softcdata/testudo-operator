# Proposal: 修复 AppBackup 删除操作历史去重 (Fix AppBackup Delete Action History Cleanup)

## Why (背景与目标)
用户报告在对 `AppBackup` 执行 `Delete` Action 后，尽管 Velero 备份资源已被删除，但 `AppBackup` 的 History 中仍残留该备份记录，且状态显示为 `Canceled`。
原因分析：
1.  当前 `syncStatus` 逻辑中，当检测到 Velero 备份处于 `Deleting` Phase 时，会将其 `ManagedStatus` 映射为 `Canceled`。
2.  `syncStatus` 的清理逻辑（Garbage Collection）被设计为**保留**所有 `Canceled` 状态的记录（为了保留用户取消操作的历史）。
3.  当用户执行 `Delete` Action 时，Operator 虽然先从 History 中移除了记录，但在随后的 Reconcile 中，由于 Velero 资源可能处于短暂的 `Deleting` 状态，`syncStatus` 会重新将其作为“新发现”的备份加回 History，并因上述映射规则标记为 `Canceled`。
4.  一旦被标记为 `Canceled`，即便是 K8s 资源最终完全消失，该记录也会被永久保留在 History 中。

## What Changes (变更内容)
1.  **修改状态映射**: 修改 `getManagedStatus` 函数。对于 `velerov1.BackupPhaseDeleting`，不再映射为 `LastBackupStatusCanceled`，而是直接返回 `Deleting`（或保持原 Phase）。
    *   **Cancel 操作**: 代码中已显式将 Cancel Action 触发的记录状态设为 `Canceled`，且 `syncStatus` 会尊重这一显式设置（`if rec.ManagedStatus != Canceled`），因此取消操作的历史保留不受影响。
    *   **Delete 操作**: 处于 `Deleting` 状态的备份将被标记为 `Deleting`。
2.  **清理逻辑自动生效**: 现有的 `syncStatus` 清理逻辑是 `if rec.ManagedStatus != Canceled { delete() }`。既然 `Deleting` != `Canceled`，当 K8s 资源最终消失时，该记录将被正确清理。

## Impact (影响范围)
- **disaster-operator**:
    - `internal/controller/appbackup/appbackup_ready.go`: 修改 `getManagedStatus`。
- **Status Change**:
    - 正在被删除的备份，其 Status 将从 `Canceled` 变为 `Deleting`。这是一个更准确的状态描述。

## Implementation Details
```go
func getManagedStatus(phase velerov1.BackupPhase) string {
    // ...
    case velerov1.BackupPhaseDeleting:
        return string(velerov1.BackupPhaseDeleting) // Return "Deleting" instead of Canceled
    // ...
}
```
验证 `syncStatus` 中的逻辑：
```go
// 清理逻辑
if !observedNames[name] {
    if rec.ManagedStatus != disasterv1.LastBackupStatusCanceled {
        // "Deleting" != "Canceled"，会被清理。Correct.
        delete(recordMap, name)
    }
}
```

## Evidence
用户提到的相关提案是 `2026-01-20-fix-schedule-bsl-update`，该提案引入了强制删除逻辑以避免 Velero 卡死。本提案是对该提案的补充，解决了删除过程中的状态同步问题。
