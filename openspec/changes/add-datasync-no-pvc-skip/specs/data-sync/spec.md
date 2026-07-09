## ADDED Requirements

### Requirement: DataSync 无可恢复 PVC 时必须跳过数据恢复

系统必须 (MUST) 在一次新的 DataSync 数据同步触发后、创建或触发 AppBackup 之前，检查本次保护范围内是否存在可恢复 PVC。若不存在可恢复 PVC，系统必须 (MUST) 将本次同步作为成功 no-op 结束，且不得创建或触发 AppBackup、Velero Backup、AppRestore、Velero Restore 或 trafficless Pod。

#### Scenario: 保护范围内没有 PVC
- **GIVEN** 一个 DataSync 关联的 DisasterInstance 保护命名空间 `app-a`
- **AND** 源集群中 `app-a` 没有非删除中的 PersistentVolumeClaim
- **WHEN** DataSync 执行首次同步或手动同步
- **THEN** DataSync 必须 (MUST) 进入 `Ready`
- **AND** 必须 (MUST) 更新 `status.lastSyncTime`
- **AND** 必须 (MUST) 记录本次同步被跳过的 condition 或 history
- **AND** 不得 (MUST NOT) 创建或触发 AppBackup
- **AND** 不得 (MUST NOT) 创建 AppRestore

#### Scenario: 保护范围内存在 PVC
- **GIVEN** 一个 DataSync 关联的 DisasterInstance 保护命名空间 `app-a`
- **AND** 源集群中 `app-a` 存在非删除中的 PersistentVolumeClaim
- **WHEN** DataSync 执行同步
- **THEN** DataSync 必须 (MUST) 继续现有 AppBackup 到 AppRestore 的数据同步流程
- **AND** 必须 (MUST) 保留现有 trafficless restore、hooks、resource modifier 和 initial PVC cleanup 语义

#### Scenario: labelSelector 只匹配 Pod
- **GIVEN** DisasterInstance 配置了 `spec.labelSelector`
- **AND** 源集群中某个 Pod 匹配该 selector
- **AND** 该 Pod 通过 `spec.volumes[].persistentVolumeClaim` 引用了 PVC
- **AND** 该 PVC 自身没有匹配该 selector 的标签
- **WHEN** DataSync 执行可恢复 PVC 检查
- **THEN** 系统必须 (MUST) 将该 PVC 视为可恢复 PVC
- **AND** 不得 (MUST NOT) 因 PVC 自身标签不匹配而跳过数据同步

#### Scenario: 无 PVC 跳过不依赖 StorageRepository 可用
- **GIVEN** 保护范围内没有可恢复 PVC
- **AND** DisasterConfig 引用的 StorageRepository 当前不可用
- **WHEN** DataSync 执行同步
- **THEN** DataSync 必须 (MUST) 成功跳过本次数据同步
- **AND** 不得 (MUST NOT) 因 StorageRepository 不可用进入 `Failed`

#### Scenario: 有 PVC 时 StorageRepository 不可用
- **GIVEN** 保护范围内存在可恢复 PVC
- **AND** DisasterConfig 引用的 StorageRepository 当前不可用
- **WHEN** DataSync 执行同步
- **THEN** DataSync 必须 (MUST) 保持现有失败语义
- **AND** 必须 (MUST) 进入 `Failed` 或保持等价错误状态

#### Scenario: 源集群资源发现失败
- **GIVEN** DataSync 无法在源集群 list Pods 或 PersistentVolumeClaims
- **WHEN** DataSync 执行可恢复 PVC 检查
- **THEN** DataSync 必须 (MUST) 将本次同步标记为失败
- **AND** 不得 (MUST NOT) 伪装成无 PVC 跳过

### Requirement: DataSync 跳过历史必须按成功统计

系统必须 (MUST) 将无 PVC no-op 跳过视为成功同步结果，避免实例初始化和同步统计把跳过结果误判为失败。

#### Scenario: Skipped 历史统计
- **GIVEN** DataSync 因无可恢复 PVC 追加了一条 `Status=Skipped` 的同步历史
- **WHEN** 控制器同步 BackupRestoreStatistics
- **THEN** 该历史必须 (MUST) 计入 completed
- **AND** 不得 (MUST NOT) 计入 failed
