## ADDED Requirements
### Requirement: 实例恢复策略支持 scoped 资源选择模型
系统必须 (MUST) 在 `DisasterInstance.spec.restorePolicy.resourceSelection` 支持 scoped 资源选择字段，并将其确定性映射到恢复执行配置。

#### Scenario: includeClusterResources=true 时优先 old 路径
- **WHEN** `resourceSelection` 同时配置了 `includeClusterResources=true` 与 scoped 四字段
- **THEN** 控制器必须优先使用 old 路径
- **AND** scoped 四字段必须被忽略

#### Scenario: scoped 模式映射成功
- **WHEN** `resourceSelection` 配置 scoped 四字段，且 `includeClusterResources` 非 true
- **THEN** 控制器必须将字段映射到 `AppRestore.Spec.Template` 的 `includedResources`、`excludedResources`、`includeClusterResources`
- **AND** 映射规则必须在 DataSync、ResourceSync、Drill 路径保持一致

#### Scenario: scoped 组合冲突被拒绝
- **WHEN** scoped include/exclude 配置存在冲突（例如同一项同时出现在 include 与 exclude）
- **THEN** 提交期校验必须拒绝该请求
- **AND** 返回错误消息必须标识冲突字段与资源项
