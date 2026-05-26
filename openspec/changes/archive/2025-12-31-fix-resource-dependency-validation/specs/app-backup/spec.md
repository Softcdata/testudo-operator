## MODIFIED Requirements

### Requirement: 资源清理 (Garbage Collection)
AppBackup 删除时必须 (MUST) 触发关联资源的清理。控制器必须 (MUST) 在 Velero CRD 不可用时优雅降级,避免阻塞资源删除流程。

#### Scenario: 清理统计资源
- **WHEN** AppBackup 资源被删除
- **THEN** 关联的 `BackupRestoreStatistics` 资源必须随之删除 (通过 OwnerReference)

#### Scenario: Velero CRD 可用时正常清理外部资源
- **GIVEN** 目标集群的 Velero CRD 可访问
- **WHEN** AppBackup 资源被删除
- **THEN** 控制器删除所有关联的 Velero Schedule 资源 (通过 UID Label 匹配)
- **AND** 控制器为所有关联的 Velero Backup 创建 DeleteBackupRequest
- **AND** 移除 Finalizer 并完成删除

#### Scenario: Velero CRD 不可用时跳过外部资源清理
- **GIVEN** 目标集群的 Velero CRD 不存在 (返回 `meta.NoMatchError`)
- **WHEN** AppBackup 资源被删除
- **THEN** 控制器记录 Warning Event "VeleroCRDNotFound"
- **AND** 跳过 Velero Schedule 和 Backup 的删除操作
- **AND** 移除 Finalizer 并完成删除 (不阻塞)

#### Scenario: 目标集群不可达时跳过外部资源清理
- **GIVEN** 目标集群的 Cluster 资源不存在或无法连接
- **WHEN** AppBackup 资源被删除
- **THEN** 控制器记录 Warning Event "ClusterNotFound" 或 "ClusterUnreachable"
- **AND** 跳过外部资源清理
- **AND** 移除 Finalizer 并完成删除 (不阻塞)

## ADDED Requirements

### Requirement: Velero CRD 可用性检查
控制器在操作 Velero 资源前,必须 (MUST) 检查对应 CRD 的可用性,避免因 CRD 不存在导致的操作失败。

#### Scenario: 使用 List 操作检测 CRD 可用性
- **WHEN** 控制器需要删除 Velero Schedule 资源
- **THEN** 先执行 `client.List(&velerov1.ScheduleList{}, client.Limit(1))` 探测 CRD
- **AND** 若返回 `meta.NoMatchError`,判定 CRD 不可用
- **AND** 若返回其他错误,继续抛出错误
- **AND** 若成功,继续执行 `DeleteAllOf` 操作

#### Scenario: 记录 CRD 不可用事件
- **WHEN** 检测到 Velero CRD 不可用
- **THEN** 记录 Kubernetes Event,类型为 Warning
- **AND** Event Reason 为 "VeleroCRDNotFound"
- **AND** Event Message 包含 "Velero CRD not available, external resources may not be cleaned"
