## ADDED Requirements

### Requirement: ResourceSync 主链路必须承接 scoped 精细化控制

当 `DisasterInstance.spec.restorePolicy.resourceSelection` 进入 scoped 模式时，ResourceSync 必须 (MUST) 将 scoped 选择投影到主链路 `AppBackup` 模板，而不是继续使用固定默认范围。

#### Scenario: scoped namespaced 资源投影到 AppBackup
- **Given** `resourceSelection` 配置了 namespaced scoped 字段
- **When** ResourceSync 构建 `AppBackup.Spec.Template`
- **Then** 模板必须包含对应的 namespaced scoped 过滤字段
- **And** 必须继续排除 `pods`、`persistentvolumeclaims`、`persistentvolumes`

#### Scenario: 未显式选择 cluster kinds 时不备份 cluster-scoped 资源
- **Given** `resourceSelection` 进入 scoped 模式
- **And** `includedClusterScopedResources` 为空
- **When** ResourceSync 构建 `AppBackup.Spec.Template`
- **Then** 主链路 backup 必须不包含 cluster-scoped 资源

#### Scenario: 显式选择 cluster kinds 时投影到 AppBackup
- **Given** `resourceSelection.includedClusterScopedResources` 非空
- **When** ResourceSync 构建 `AppBackup.Spec.Template`
- **Then** 模板必须包含对应的 cluster scoped include/exclude 字段

### Requirement: ResourceSync 必须在 scoped cluster 资源场景下分阶段恢复

当实例显式选择 scoped cluster kinds 时，ResourceSync 必须 (MUST) 将恢复拆成 cluster/namespaced 两个阶段，并使用不同的 existing resource policy。

#### Scenario: cluster phase 使用 none
- **Given** `resourceSelection.includedClusterScopedResources` 非空
- **When** ResourceSync 创建 cluster phase 的 AppRestore
- **Then** 该 restore 必须只恢复显式选中的 cluster kinds
- **And** `existingResourcePolicy` 必须为 `none`

#### Scenario: namespaced phase 使用 update
- **Given** ResourceSync 进入 namespaced phase
- **When** ResourceSync 创建 namespaced phase 的 AppRestore
- **Then** 该 restore 必须不恢复 cluster-scoped 资源
- **And** `existingResourcePolicy` 必须为 `update`
- **And** 必须继续排除 `pods`、`persistentvolumeclaims`、`persistentvolumes`

#### Scenario: 未选择 cluster kinds 时跳过 cluster phase
- **Given** `resourceSelection.includedClusterScopedResources` 为空
- **When** ResourceSync 执行恢复
- **Then** 系统必须跳过 cluster phase
- **And** 只执行 namespaced restore

### Requirement: ResourceSync 必须暴露分阶段 restore 状态

ResourceSync 必须 (MUST) 暴露 cluster/namespaced 两阶段 restore 的独立状态，便于上层定位失败点。

#### Scenario: cluster phase 状态可观测
- **When** ResourceSync 创建或轮询 cluster phase 的 AppRestore
- **Then** `status.lastClusterRestoreName` 与 `status.clusterRestoreStatus` 必须反映当前阶段信息

#### Scenario: namespaced phase 状态可观测
- **When** ResourceSync 创建或轮询 namespaced phase 的 AppRestore
- **Then** `status.lastNamespaceRestoreName` 与 `status.namespaceRestoreStatus` 必须反映当前阶段信息
- **And** 兼容字段 `status.lastRestoreName` 必须持续指向最后一个被处理的 restore 名称
