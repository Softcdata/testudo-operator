## ADDED Requirements

### Requirement: DisasterConfig 配置定义
`DisasterConfig` 必须 (MUST) 作为配置模板，定义集群对、存储和同步策略。

#### Scenario: 定义集群对配置
- **GIVEN** 用户希望配置多集群容灾关系
- **WHEN** 他们创建一个 `DisasterConfig` CR
- **THEN** 他们必须 (MUST) 指定 `sourceCluster` 引用源集群 Cluster CR
- **AND** 他们必须 (MUST) 指定 `targetCluster` 引用目标集群 Cluster CR
- **AND** 他们必须 (MUST) 指定 `storage` 引用 StorageRepository CR

#### Scenario: 定义同步策略
- **GIVEN** 用户创建 `DisasterConfig`
- **WHEN** 他们配置 `dataSyncPolicy`
- **THEN** `schedule` 字段必须 (MUST) 使用 Cron 表达式格式
- **AND** `incremental` 字段可选启用增量同步
- **AND** `retentionDays` 字段定义备份保留天数

### Requirement: DisasterInstance 核心定义
`DisasterInstance` (DRI) 必须 (MUST) 作为顶层编排对象，定义应用级别的容灾保护。

#### Scenario: 创建容灾实例
- **GIVEN** 用户希望保护一个应用
- **WHEN** 他们创建一个 `DisasterInstance` CR
- **THEN** 他们必须 (MUST) 指定 `config` 引用 DisasterConfig CR
- **AND** 他们可以 (MAY) 指定 `namespaces` 列表或 `labelSelector` 标识保护对象
- **AND** 他们可以 (MAY) 指定 `podRestoreMethod` 为 `replica` 或 `initContainer`

### Requirement: 状态机管理
DRI 必须 (MUST) 实现完整的状态机来管理容灾生命周期。

#### Scenario: 状态机初始化
- **GIVEN** 创建了一个新的 DRI
- **WHEN** 初始状态为空或 `Pending`
- **THEN** Controller 必须 (MUST) 创建 `DataSync` 和 `ResourceSync` 子资源
- **AND** 状态必须 (MUST) 转换为 `Initializing`

#### Scenario: 首次同步完成
- **GIVEN** DRI 处于 `Initializing` 状态
- **WHEN** DataSync 和 ResourceSync 首次同步成功
- **THEN** 状态必须 (MUST) 转换为 `Protected`
- **AND** `availableOperations` 必须 (MUST) 包含 `[failover, pause, synconce]`

#### Scenario: Failover 状态转换
- **GIVEN** DRI 处于 `Protected` 状态
- **WHEN** 用户创建 `DisasterOperation` (operationType=failover)
- **THEN** 状态必须 (MUST) 转换为 `FailingOver`
- **AND** 切换完成后状态必须 (MUST) 转换为 `Active`

### Requirement: 子资源自动编排
DRI Controller 必须 (MUST) 自动管理 `DataSync` 和 `ResourceSync` 的生命周期。

#### Scenario: 创建子资源
- **GIVEN** DRI 创建时关联了 DisasterConfig
- **WHEN** DRI Controller Reconcile 运行
- **THEN** 必须 (MUST) 创建 `DataSync` CR 并设置 OwnerReference
- **AND** 必须 (MUST) 创建 `ResourceSync` CR 并设置 OwnerReference
- **AND** 子资源必须 (MUST) 从 DisasterConfig 继承同步策略

#### Scenario: 级联删除
- **GIVEN** DRI 被删除
- **WHEN** Kubernetes 处理级联删除
- **THEN** DataSync 和 ResourceSync 必须 (MUST) 被自动删除

### Requirement: 状态聚合
DRI 必须 (MUST) 聚合子资源的状态到顶层。

#### Scenario: 同步时间聚合
- **GIVEN** DataSync 完成了一次同步
- **WHEN** DRI 更新状态
- **THEN** `status.lastDataSyncTime` 必须 (MUST) 反映最新同步时间

#### Scenario: 健康状态聚合
- **GIVEN** DataSync 为 'Ready' 但 ResourceSync 为 'Failed'
- **WHEN** DRI 计算聚合状态
- **THEN** DRI 的 Conditions 必须 (MUST) 包含警告或错误信息
