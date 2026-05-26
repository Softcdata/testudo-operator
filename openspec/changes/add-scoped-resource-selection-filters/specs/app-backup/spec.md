## ADDED Requirements
### Requirement: AppBackup scoped 资源过滤字段透传
AppBackup 控制器必须 (MUST) 透传 `spec.template` 中的 scoped 资源过滤字段到 Velero Backup/Schedule，不得丢失字段值。

#### Scenario: 创建单次备份透传 scoped 字段
- **WHEN** 创建 AppBackup，且 `spec.template` 包含 `includedNamespaceScopedResources`、`excludedNamespaceScopedResources`、`includedClusterScopedResources`、`excludedClusterScopedResources`
- **THEN** 创建的 Velero Backup `spec` 中必须包含对应字段值
- **AND** 字段顺序与去重行为保持输入顺序与内容

#### Scenario: 创建周期计划透传 scoped 字段
- **WHEN** 创建带 `spec.schedule` 的 AppBackup，且 `spec.template` 包含 scoped 资源过滤字段
- **THEN** 创建的 Velero Schedule `spec.template` 中必须包含对应字段值
- **AND** 后续由 Schedule 生成的 Backup 必须继承该模板字段
