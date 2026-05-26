## ADDED Requirements
### Requirement: 恢复生命周期控制
AppRestore 必须正确分发恢复请求到目标集群，并跟踪其完成状态。

#### Scenario: 成功创建恢复任务
- **WHEN** 指定一个存在的备份源创建 AppRestore
- **THEN** 在目标集群成功下发 `velerov1.Restore` 对象
- **AND** 最终同步状态为 Completed

### Requirement: 环境适配插件 (ConfigMap)
AppRestore 必须根据 Spec 中的 Mapping 配置，自动在目标集群创建用于适配存储类或镜像的 ConfigMap。

#### Scenario: 自动生成存储类映射配置
- **WHEN** 设置了 `Spec.Action.ChangeStorageClasses` (注：假设路径)
- **THEN** 在目标集群 `velero` 命名空间创建带有 `velero.io/plugin-config` 标签的 ConfigMap
