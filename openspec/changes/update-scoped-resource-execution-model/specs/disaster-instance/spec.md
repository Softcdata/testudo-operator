## ADDED Requirements

### Requirement: DisasterInstance 恢复策略必须支持 scoped 资源选择字段
系统必须 (MUST) 允许 `DisasterInstance` 的恢复策略分别声明 namespace-scoped 与 cluster-scoped 资源种类范围。

#### Scenario: 实例恢复策略声明 scoped 四字段
- **Given** 用户创建或更新一个 `DisasterInstance`
- **And** 其恢复策略同时提供 namespace-scoped 与 cluster-scoped 资源范围
- **When** operator 持久化并调谐该实例
- **Then** operator 必须保存并承接 scoped 四字段
- **And** 受该实例托管的 DataSync、ResourceSync 与自动恢复路径必须读取这些字段

### Requirement: cluster-scoped scoped 字段首期仅表示 kind 级执行范围
系统必须 (MUST) 将 `includedClusterScopedResources` 与 `excludedClusterScopedResources` 解释为 cluster-scoped 资源种类范围，而不是对象级精确集合。

#### Scenario: cluster-scoped kind 被作为整类资源范围执行
- **Given** 一个 `DisasterInstance` 的恢复策略包含 `includedClusterScopedResources=["storageclasses.storage.k8s.io"]`
- **When** operator 构造受管备份或恢复范围
- **Then** operator 必须把该值解释为 `StorageClass` 这一整类 cluster-scoped 资源
- **And** 不得将其解释为某个具体 `StorageClass` 对象列表

#### Scenario: PersistentVolume 不承诺对象级精确恢复
- **Given** 一个 `DisasterInstance` 的恢复策略包含 `includedClusterScopedResources=["persistentvolumes"]`
- **When** operator 构造受管备份或恢复范围
- **Then** operator 只能保证 `PersistentVolume` 这一 kind 级资源范围语义
- **And** 不得宣称只会包含与某个 namespace 精确关联的 `PersistentVolume` 对象

### Requirement: includeClusterResources 必须优先于 scoped 四字段
系统必须 (MUST) 在 `includeClusterResources=true` 时按全量 cluster-scoped 资源路径执行，并忽略 scoped 四字段。

#### Scenario: includeClusterResources 覆盖 scoped 配置
- **Given** 一个 `DisasterInstance` 的恢复策略同时设置了 `includeClusterResources=true` 和 scoped 四字段
- **When** operator 构造受管备份或恢复范围
- **Then** operator 必须按包含全部 cluster-scoped 资源的旧路径执行
- **And** scoped 四字段不得改变该执行结果
