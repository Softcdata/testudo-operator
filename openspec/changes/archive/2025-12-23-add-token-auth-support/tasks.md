# Tasks: 添加 Token 认证支持

- [x] 更新 `ClusterReconciler.Reconcile` 以处理 `ClusterSpec` 中的 `Token`。
- [x] 实现从 `Token` 和 `Endpoint`（如果可用）创建 REST 配置的逻辑，或推断端点。*注意：Token 通常需要端点。*
- [x] 更新 `GetRestConfig` 或创建一个新的辅助函数以支持 Token。
- [x] 确保 `IsVeleroInstalled` 和 `checkVeleroVersion` 使用正确的客户端。
- [x] 测试基于 Token 的集群协调。
- [x] 修复 `GetKubeClientSetWithCluster` 仅检查 KubeConfig 的 bug。
- [x] 修复 `DisasterBackupReconciler` 中 `getKubeConfigByClusterName` 不支持 Token 的问题。
