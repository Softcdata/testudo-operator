## ADDED Requirements

### Requirement: ResourceSync 核心定义
`ResourceSync` 必须 (MUST) 专注于 K8s 资源对象的跨集群复制。

#### Scenario: 资源同步配置
- **GIVEN** ResourceSync 由 DisasterInstance 创建
- **WHEN** Controller 执行同步
- **THEN** 必须 (MUST) 创建 AppBackup 并配置 `SnapshotVolumes=false`
- **AND** 必须 (MUST) 配置 `ExcludedResources` 排除 `[pvc, pv]`
- **AND** 备份应当 (SHOULD) 包含 Deployment, StatefulSet, Service, ConfigMap 等资源

### Requirement: Standby 模式
ResourceSync 必须 (MUST) 在目标集群恢复资源时应用 Standby 模式。

#### Scenario: Scale to Zero
- **GIVEN** ResourceSync 配置了 `spec.standbyModifier.scaleToZero: true`
- **WHEN** 在目标集群执行 AppRestore
- **THEN** 必须 (MUST) 应用 ResourceModifier
- **AND** Deployment/StatefulSet 的 `spec.replicas` 必须 (MUST) 被设置为 0
- **AND** 原始副本数必须 (MUST) 保存到注解

#### Scenario: 保存原始副本数
- **GIVEN** Deployment 原始副本数为 3
- **WHEN** 应用 ResourceModifier
- **THEN** 必须 (MUST) 添加注解 `testudo.softcdata.com/original-replica-count: "3"`
- **AND** 此注解用于 Failover 时恢复副本数

### Requirement: 资源过滤
ResourceSync 必须 (MUST) 支持排除特定资源。

#### Scenario: 排除敏感资源
- **GIVEN** ResourceSync 配置了 `spec.excludeResources: [secrets]`
- **WHEN** 创建 AppBackup
- **THEN** AppBackup 的 `ExcludedResources` 必须 (MUST) 包含 `secrets`
- **AND** Secret 资源不得 (MUST NOT) 被同步到目标集群

#### Scenario: 按名称排除
- **GIVEN** ResourceSync 配置了排除 `names: [sensitive-*]`
- **WHEN** 执行同步
- **THEN** 匹配该模式的资源不得 (MUST NOT) 被备份

### Requirement: 定时触发
ResourceSync 必须 (MUST) 支持基于 Cron 表达式的定时同步。

#### Scenario: 低频同步
- **GIVEN** ResourceSync 配置了 `spec.trigger.schedule: "0 2 * * *"`
- **WHEN** 凌晨 2 点
- **THEN** Controller 必须 (MUST) 触发一次资源同步

### Requirement: 暂停功能
ResourceSync 必须 (MUST) 支持暂停以配合 Failover 操作。

#### Scenario: 暂停同步
- **GIVEN** `ResourceSync.spec.paused=true`
- **WHEN** 调度时间到达
- **THEN** Controller 不得 (MUST NOT) 触发新的同步任务

### Requirement: 状态报告
ResourceSync 必须 (MUST) 报告同步状态。

#### Scenario: 记录备份恢复名称
- **GIVEN** 同步完成
- **WHEN** 更新 Status
- **THEN** `status.lastBackupName` 必须 (MUST) 记录最近的 AppBackup 名称
- **AND** `status.lastRestoreName` 必须 (MUST) 记录最近的 AppRestore 名称
