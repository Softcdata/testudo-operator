## MODIFIED Requirements

### Requirement: 数据恢复必须在同步模式下应用 Trafficless 配置
DataSync 的数据恢复在同步模式下必须 (MUST) 应用 trafficless 资源修改规则，并且该规则与基础配置镜像映射策略分离。

#### Scenario: DataSync 保持 trafficless 独立语义
- **Given** `DataSync` 执行同步模式的数据恢复
- **When** 构建 `AppRestoreSpec`
- **Then** 系统必须 (MUST) 继续应用 trafficless 镜像与标签规则
- **And** 不得 (MUST NOT) 复用 `DisasterConfig.spec.imageRewrite` 覆盖 trafficless 语义

## ADDED Requirements

### Requirement: 资源恢复必须支持镜像源映射替换
ResourceSync 和 Drill 的资源恢复路径必须 (MUST) 支持按 `DisasterConfig.spec.imageRewrite` 生成镜像替换补丁。

#### Scenario: 覆盖多容器与 InitContainer
- **Given** 一个 Deployment 或 StatefulSet 包含多个 containers 与 initContainers
- **And** 对应镜像命中基础配置映射规则
- **When** 系统构建资源恢复的 `ResourceModifierRules`
- **Then** 系统必须 (MUST) 对所有命中的 containers 与 initContainers 生成替换补丁

#### Scenario: failover 后角色切换仍正确替换
- **Given** 实例发生过 failover，当前 primary/secondary 与初始配置互换
- **When** 系统计算镜像映射并执行恢复
- **Then** 系统必须 (MUST) 按“当前 source/target 角色”计算映射
- **And** 不得 (MUST NOT) 使用静态方向导致反向替换

#### Scenario: reprotect 后反向保护同步仍正确替换
- **Given** 实例已执行 failover 并切换角色
- **And** 实例已执行 reprotect 并进入反向保护
- **When** 触发 `sync-resource` 并计算镜像映射
- **Then** 系统必须 (MUST) 允许按当前 source/target 方向解析映射别名对
- **And** 不得 (MUST NOT) 因静态 `sourceCluster/targetCluster` 方向假设导致构建失败
