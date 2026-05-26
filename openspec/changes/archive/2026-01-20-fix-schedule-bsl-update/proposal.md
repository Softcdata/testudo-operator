# Change: Fix Schedule BSL Update and Robust Deletion

## Why
1. **BSL Update Bug**: 在 `AppBackup` 的 Reconcile 逻辑中，更新 `Schedule` 时直接覆盖了 Spec，导致 `StorageLocation` 丢失集群后缀。这使得生成的备份找不到 BSL，状态变为 `FailedValidation`。
2. **Deletion Stuck**: 用户反馈这些 `FailedValidation` 的自动创建备份无法被删除。目前的级联删除逻辑是“Fire and Forget”（仅创建 `DeleteBackupRequest` 即退出），如果 Velero 因 BSL 错误无法处理删除请求，Backup 资源就会残留。
3. **Label Logic**: 自动创建的备份被错误标记为 `Manual` 类型。

## What Changes
1. **Fix BSL Injection**: 在更新 Schedule 时，强制重新注入带有 Cluster 后缀的 `StorageLocation`。
2. **Fix Type Labeling**: 基于 `Spec.Schedule` 是否存在来正确设置 `AppBackupType` 标签。
3. **Robust Cascading Deletion**: 
   - 修改 `deleteExternalResources` 逻辑，使其在创建删除请求后**等待** Backup 资源实际消失。
   - 引入超时机制（如 30秒），如果 Backup 仍未删除（通常因 Velero 卡住），Operator 将**强制移除 Finalizer** 并直接删除 Backup CR，确保不残留垃圾资源。

## Impact
- **Affected Code**: `internal/controller/appbackup/appbackup_ready.go`, `internal/controller/appbackup/appbackup_controller.go`
- **Affected Capabilities**: AppBackup 生命周期管理, 级联删除
- **Compatibility**: 增强了 API 行为的一致性（AppBackup 删除时，子资源保证被清理）。
