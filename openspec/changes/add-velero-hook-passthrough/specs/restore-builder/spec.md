## ADDED Requirements

### Requirement: Restore Builder 支持 Data Restore Hook 透传
Restore Builder 必须 (MUST) 支持将调用方提供的 Velero Restore hooks 写入数据恢复类型的 AppRestoreSpec。

#### Scenario: Data restore 构建时写入 hooks
- **GIVEN** BuilderConfig 中提供了 DataRestoreHooks
- **AND** `RestoreType` 为 `data`
- **WHEN** 调用 `BuildAppRestoreSpec`
- **THEN** 返回的 `AppRestoreSpec.template.hooks` 必须等于 BuilderConfig 中的 DataRestoreHooks
- **AND** ExistingResourcePolicy、RestorePVs、IncludedResources、ResourceModifierRules 等既有字段必须保持现有语义

#### Scenario: Resource restore 默认不写入 hooks
- **GIVEN** BuilderConfig 中提供了 DataRestoreHooks
- **AND** `RestoreType` 为 `resource`
- **WHEN** 调用 `BuildAppRestoreSpec`
- **THEN** 返回的 `AppRestoreSpec.template.hooks` 默认必须为空
- **AND** 资源恢复继续排除 pods、persistentvolumeclaims、persistentvolumes

### Requirement: Data Restore PVC 清理规则必须幂等
Restore Builder 和 server AppRestore create/update 在为 `restorePVs=true` 或 `cleanVolumes=true` 注入 PVC `spec.volumeName` 清理规则时，必须 (MUST) 使用字段存在和不存在均不会触发 Velero ResourceModifier 错误的幂等 patch。

#### Scenario: PVC volumeName 缺失时不产生 Velero restore error
- **GIVEN** DataSync 首次数据恢复需要清理 PVC stale binding
- **AND** 备份中的某个 PVC 不包含 `spec.volumeName`
- **WHEN** Restore Builder 生成系统级 PVC 清理 ResourceModifier
- **THEN** 生成的 patch 不得使用非幂等 `remove /spec/volumeName`
- **AND** Velero 对该 PVC 应能继续恢复，不得因为清理规则自身导致 Restore `PartiallyFailed`

#### Scenario: 直接创建 AppRestore 时 PVC 清理规则幂等
- **GIVEN** 用户通过 server 创建 AppRestore 且 `restorePVs=true` 或 `cleanVolumes=true`
- **WHEN** server 写入 `AppRestore.spec.resourceModifierRules`
- **THEN** 自动追加的 PVC `spec.volumeName` 清理 patch 不得使用 `remove /spec/volumeName`
- **AND** 如果已有旧的 `remove /spec/volumeName` 规则，server 必须替换为幂等清理规则

#### Scenario: PVC volumeName 存在时清空绑定
- **GIVEN** 备份中的某个 PVC 包含 `spec.volumeName`
- **WHEN** Restore Builder 生成系统级 PVC 清理 ResourceModifier
- **THEN** patch 必须将 `spec.volumeName` 清空，使恢复后的 PVC 不携带旧 PV 绑定
