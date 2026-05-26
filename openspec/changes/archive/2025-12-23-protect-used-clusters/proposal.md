# Proposal: 保护被引用的集群

## Summary
防止删除或修改已被上层应用（如 `AppBackup`, `AppRestore`, `DisasterConfig`）引用的 `Cluster` 资源。

## Motivation
集群是容灾系统的基础配置。一旦集群被用于备份、恢复或容灾配置，意外删除或修改该集群配置可能导致严重的后果，包括：
- 备份任务失败
- 无法恢复数据
- 容灾流程中断
- 孤儿资源（Orphaned Resources）

为了保证系统的稳定性，必须实施保护机制，确保只有在解除所有引用关系后，才能删除集群。

## Proposed Changes

### 1. Finalizer 机制
- **引入 Finalizer**: 在 `Cluster` 资源创建时（或首次协调时），自动添加一个 Finalizer，例如 `testudo.softcdata.com/in-use-protection`。
- **删除拦截**: 当 `Cluster` 被标记为删除（`DeletionTimestamp` 非空）时，控制器将执行依赖检查。

### 2. 依赖检查逻辑 (`ClusterReconciler`)
在 `Reconcile` 循环中处理删除事件：
1. **AppBackup 检查**:
   - 使用 Label Selector 查询 `AppBackupList`。
   - 标签: `testudo.softcdata.com/app-backup-cluster` (LabelAppBackupCluster) = 当前集群名称。
2. **AppRestore 检查**:
   - 使用 Label Selector 查询 `AppRestoreList`。
   - 标签: `testudo.softcdata.com/app-restore-cluster` (LabelAppRestoreCluster) = 当前集群名称。
3. **DisasterConfig 检查**:
   - 目前 `DisasterConfig` 尚未定义集群相关的 Label，需要先查询所有 `DisasterConfig` 并遍历检查 `spec.sourceCluster` 和 `spec.targetCluster`。
   - **优化建议**: 为 `DisasterConfig` 添加 `testudo.softcdata.com/source-cluster` 和 `testudo.softcdata.com/target-cluster` 标签以优化查询。
4. **StorageRepository 检查**:
   - 检查 `StorageRepository` 是否有特定于该集群的配置（如果适用）。
   - 目前 `StorageRepository` 主要是存储定义，通常不直接绑定单一集群，但需确认是否有 `VolumeSnapshotLocation` 或 `BackupStorageLocation` 强绑定。
5. **如果存在引用**:
   - 记录 Warning Event (e.g., `DeletionBlocked`).
   - 更新 Status 提示用户存在依赖资源。
   - **不移除** Finalizer，阻止删除完成。
6. **如果不存在引用**:
   - 移除 Finalizer，允许删除继续。

### 3. 修改限制 (Immutable Fields)
- 虽然 Kubernetes 原生支持通过 Validating Webhook 限制修改，但在没有 Webhook 的情况下，可以在 Controller 中实现“配置漂移检测与还原”或仅在 Server 端 API 层进行限制。
- **Server 端限制**: `disaster-server` 的 Update 接口应检查关键字段（如集群名称 - 虽不可变，但需注意）的变更。对于连接信息（Token/Endpoint/KubeConfig），通常允许修改以支持轮换，但应谨慎。
- **建议**: 本提案主要关注**删除保护**。对于修改，建议仅在 Server API 层做业务校验。

### 4. Server 端增强 (`disaster-server`)
- 在 `DeleteCluster` Handler 中，先执行一次快速的依赖检查（查询数据库或调用 K8s API），如果发现引用直接返回 400 错误，提供更友好的用户反馈，而不是依赖 K8s 的异步删除阻塞。

## Alternatives
- **Validating Admission Webhook**: 更严谨，能拦截 `kubectl` 操作，但部署和运维成本较高（需要证书管理）。鉴于目前架构，Controller Finalizer + Server API Check 是性价比最高的方案。
