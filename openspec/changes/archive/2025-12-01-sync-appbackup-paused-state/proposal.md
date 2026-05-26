# Proposal: Sync AppBackup Paused State to Velero

## Why
目前 `AppBackup` 的 `Paused` 状态仅在 Operator 层面阻止了 Reconcile 的部分逻辑，但对于已经创建的 Velero `Schedule`，如果 `AppBackup` 被暂停，Velero 端可能仍然在按照原定计划执行备份。
为了确保用户暂停备份的意图能够完整传递到底层 Velero 资源，我们需要将 `AppBackup.Spec.Paused` 字段同步到 Velero `Schedule.Spec.Paused`。

## What Changes
- 修改 `AppBackupReconciler` 的逻辑。
- 在处理 Velero `Schedule` 时，检查并同步 `Paused` 状态。
- 确保当 `AppBackup` 暂停时，对应的 Velero `Schedule` 也被标记为暂停。
- 确保当 `AppBackup` 恢复时，对应的 Velero `Schedule` 也恢复运行。
