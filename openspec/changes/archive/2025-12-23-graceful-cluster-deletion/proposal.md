# Proposal: 优雅的集群删除与 Velero 卸载

## Summary
引入 `Deleting` 状态和 Finalizer 机制，以支持优雅的集群删除流程。同时提供选项，允许在删除集群资源时选择是否卸载目标集群中的 Velero。

## Motivation
当前的 `ClusterReconciler` 缺乏对删除事件的专门处理，导致删除操作可能过于突然，且无法执行清理工作（如卸载 Velero）。用户希望在删除集群时能够控制是否清理 Velero 安装，以避免残留资源。

## Proposed Changes

### 1. 状态与 Finalizer
- **ClusterStatus**:
    - 添加 `Deleting` 状态常量。
    - **新增字段**: `Reason` (string) 和 `Message` (string)，用于反馈被阻止的原因或当前进度。
- **Finalizer**: 引入 `testudo.softcdata.com/cluster-finalizer`。
- **Annotation**: 引入 `testudo.softcdata.com/uninstall-velero` (值为 "true" 或 "false")，用于指示是否卸载 Velero。

### 2. ClusterReconciler 逻辑更新
- **Finalizer 管理**:
    - 在 `Reconcile` 开始时，如果对象没有 Finalizer 且未被标记删除，添加 Finalizer。
- **删除处理 (`handleDelete`)**:
    - 检查 `DeletionTimestamp`。如果非空：
        1. 更新 Status 为 `Deleting`。
        2. **依赖检查** (引用 `protect-used-clusters` 提案中的逻辑)。
           - 如果存在依赖：
             - 设置 `Status.Reason` = "DeletionBlocked"
             - 设置 `Status.Message` = "Cluster is in use by [Resource Kind] [Resource Name]"
             - 记录 Warning Event。
             - **返回并重试** (Requeue)，保持 Finalizer，阻止删除。
        3. **Velero 卸载检查**:
           - 检查 Annotation `testudo.softcdata.com/uninstall-velero`。
           - 如果为 "true"，执行 Velero 卸载逻辑（使用 Helm 卸载或删除相关资源）。
           - 更新 `Status.Message` = "Uninstalling Velero..."
           - 记录卸载结果 Event。
        4. **移除 Finalizer**:
           - 清理完成后，移除 Finalizer，允许 Kubernetes 删除 CR。

### 3. Velero 卸载逻辑
- 实现 `uninstallVelero(ctx, cluster)` 方法。
- 该方法应连接到目标集群，并删除 Velero 相关的 Namespace (通常是 `velero`) 或 Helm Release。

## Tasks
- [ ] 在 `pkg/apis/disaster/v1/cluster_types.go` 中添加 `ClusterStatusDeleting` 常量。
- [ ] 在 `pkg/metadata` 中定义 Finalizer 和 Annotation 常量。
- [ ] 更新 `ClusterReconciler` 以管理 Finalizer。
- [ ] 实现 `handleDelete` 方法，包含状态更新、依赖检查调用和 Velero 卸载逻辑。
- [ ] 实现 `uninstallVelero` 辅助函数。
