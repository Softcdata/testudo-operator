# Change: 修复 AppBackup 删除时因 DeleteBackupRequest 已存在导致的阻塞问题

## Why

当用户删除 `AppBackup` 资源时，控制器会执行 `deleteExternalResources` 来清理目标集群中的 Velero 资源。该过程会遍历所有关联的备份并为每一项创建 `DeleteBackupRequest`。

目前代码中，如果 `cli.Create` 返回错误，控制器会直接返回该错误并触发重新入队。在某些情况下（如之前 Reconcile 失败但请求已发出），`DeleteBackupRequest` 可能已经存在，导致 `cli.Create` 始终返回 `AlreadyExists` 错误。这使得控制器陷入死循环，无法进入移除 Finalizer 的逻辑，导致 `AppBackup` 资源永久卡在删除状态。

## What Changes

- **容错处理**: 在 `deleteExternalResources` 函数中，创建 `DeleteBackupRequest` 时增加对 `apierrors.IsAlreadyExists(err)` 的判断。
- **流程连续性**: 如果请求已存在，将其视为成功处理，并继续处理剩余的备份清理任务。

## Impact

- **受影响文件**: `internal/controller/appbackup/appbackup_controller.go`
- **受影响规范**: `app-backup-lifecycle`
