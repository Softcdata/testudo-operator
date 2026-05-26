## ADDED Requirements
### Requirement: AppBackup 调度管理
AppBackup 控制器必须 (MUST) 管理 Velero Schedule 资源的生命周期并确保配置一致性。

#### Scenario: Schedule 更新一致性
- **Given** 一个拥有 Schedule 且 `StorageLocation` 前缀有效的 AppBackup
- **When** 该 AppBackup 的 Spec 被更新（触发 Schedule 更新）
- **Then** 关联的 Velero Schedule 的 `Template.StorageLocation` 必须 (MUST) 保留正确的集群后缀（例如 `storage-cluster1`）
- **And** 它绝不能 (MUST NOT) 回退到 AppBackup Spec 中的原始前缀

#### Scenario: 正确的备份类型标签
- **Given** 一个配置了 `Spec.Schedule` 的 AppBackup
- **When** 创建 Velero Schedule 时
- **Then** 该 Schedule Template 必须 (MUST) 带有标签 `testudo.softcdata.com/app-backup-type: Schedule`
- **And** 由此创建的 Backups 必须 (MUST) 继承该标签
- **When** 一个 AppBackup 没有配置 `Spec.Schedule` (手动/一次性)
- **Then** 创建的 Backups 必须 (MUST) 带有标签 `testudo.softcdata.com/app-backup-type: Manual`

### Requirement: 健壮级联删除
AppBackup 的删除操作必须 (MUST) 确保成功移除所有关联的外部资源（Velero Backups 和 Schedules），即使它们处于异常状态。

#### Scenario: 健壮的删除逻辑
- **Given** 一个正在删除中的 AppBackup
- **When** 存在关联的 Velero Backups（即使处于 `FailedValidation` 状态）
- **Then** 控制器必须 (MUST) 为它们发出 `DeleteBackupRequest`
- **And** 控制器必须 (MUST) 等待这些 Backups 从集群中被移除
- **And** 如果删除超时（例如 > 30s），控制器必须 (MUST) 强制移除 Finalizers 并直接删除 Backup CRs
- **And** 只有在所有 Backups 都消失后，AppBackup 的 Finalizer 才能 (SHALL) 被移除
