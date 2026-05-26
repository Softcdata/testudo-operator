# Tasks: 实现集群保护

## Operator
- [ ] 在 `ClusterReconciler` 中定义 Finalizer 常量 `testudo.softcdata.com/in-use-protection`。
- [ ] 在 `Reconcile` 中实现 Finalizer 添加逻辑（如果不存在则添加）。
- [ ] 实现 `checkDependencies` 辅助函数：
    - [ ] 查询 `AppBackupList` (使用 Label: `testudo.softcdata.com/app-backup-cluster`)
    - [ ] 查询 `AppRestoreList` (使用 Label: `testudo.softcdata.com/app-restore-cluster`)
    - [ ] 查询 `DisasterConfigList` (遍历检查或添加新 Label)
    - [ ] (可选) 查询 `DisasterBackupList`
- [ ] 在 `Reconcile` 的删除流程中调用 `checkDependencies`。
- [ ] 如果存在依赖，记录 Event 并 Requeue。
- [ ] 如果无依赖，移除 Finalizer。

## Server
- [ ] 在 `internal/apis/disaster_cluster` 中实现 `CheckClusterUsage` 服务方法。
- [ ] 在 `DeleteCluster` Handler 中调用 `CheckClusterUsage`。
- [ ] 如果被使用，返回 HTTP 400 及详细错误信息。
