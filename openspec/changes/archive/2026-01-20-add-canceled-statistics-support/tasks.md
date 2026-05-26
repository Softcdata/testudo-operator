# Tasks: 添加取消状态统计支持

- [x] **Spec Update**: 更新 AppBackup 和 AppRestore 规范，要求正确的 Canceled 统计。 <!-- id: spec-update -->
- [x] **AppRestore Stats**: 更新 `AppRestoreReconciler.syncStatistics` 以将 `Cancelled` 阶段映射为 `Canceled` 计数。 <!-- id: apprestore-stats -->
- [x] **AppBackup Stats**: 更新 `AppBackupReconciler.syncStatistics` 以使用 `Status.History` 进行计算。 <!-- id: appbackup-stats -->
- [x] **Verification**: 验证取消 AppBackup 和 AppRestore 操作能否正确增加统计中的 Canceled 计数。 <!-- id: verification -->
