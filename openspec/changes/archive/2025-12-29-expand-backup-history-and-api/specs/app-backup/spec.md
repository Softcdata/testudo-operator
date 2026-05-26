## MODIFIED Requirements

### Requirement: 备份历史与统计同步
控制器必须 (MUST) 定期同步目标集群的备份状态到本地 CR 的 History 中，并更新统计指标。

#### Scenario: 同步 Velero 备份历史详情
- **WHEN** 目标集群存在管理内的备份记录
- **THEN** 同步到 `Status.History` 时，必须包含完整的 Velero Backup Status 信息（通过 `VeleroStatus` 字段）
- **AND** 更新 `Status.TotalBackups`
