# Proposal: DisasterConfig 删除保护

## Summary
为 `DisasterConfig` 引入 Finalizer 机制，防止误删正在被 `DisasterInstance` 使用的配置。

## Motivation
- `DisasterConfig` 是核心配置，定义了容灾集群、存储和策略。
- 此配置被 `DisasterInstance` 引用。如果配置被误删，会导致 ResourceSync/DataSync 等控制器报错（"DisasterConfig not found"），进而导致容灾操作失败。
- 需要参考 Cluster/StorageRepository 的删除保护规范，确保只有在无引用的情况下才能删除。

## Proposed Changes

### DisasterConfig Controller
1. **Add Finalizer**: 在 Reconcile loop 中，如果 `DisasterConfig` 没有 DeletionTimestamp，确保添加 Finalizer: `testudo.softcdata.com/config-finalizer`。
2. **Handle Deletion**:
   - 当检测到 DeletionTimestamp 不为空时：
     - 查询所有 `DisasterInstance`。
     - 检查是否有 Instance 的 `.Spec.Config` 引用了当前 Config。
     - 如果有引用：
       - 更新 Status (Condition 或者 Event) 提示用户 "Cannot delete: used by DisasterInstance <name>"。
       - 阻止删除（return & requeue）。
     - 如果无引用：
       - 移除 Finalizer，允许删除。

## API Changes
无 Spec 修改。Status 可能增加 Condition (如 `DeletionBlocked`)。
