# 规范：AppRestore 控制器

## Purpose
AppRestore 控制器负责管理应用恢复流程，包括从备份中恢复资源、处理跨环境的配置映射（如 StorageClass），并监控恢复任务的执行状态。
## Requirements
### Requirement: 恢复生命周期控制
AppRestore (MUST) 必须正确分发恢复请求到目标集群，并跟踪其完成状态。

#### Scenario: 成功创建恢复任务
- **WHEN** 指定一个存在的备份源创建 AppRestore
- **THEN** 在目标集群成功下发 `velerov1.Restore` 对象
- **AND** 最终同步状态为 Completed

### Requirement: 环境适配插件 (ConfigMap)
AppRestore (MUST) 必须根据 Spec 中的 Mapping 配置，自动在目标集群创建用于适配存储类或镜像的 ConfigMap。

#### Scenario: 自动生成存储类映射配置
- **WHEN** 设置了 `Spec.Action.ChangeStorageClasses` (注：假设路径)
- **THEN** 在目标集群 `velero` 命名空间创建带有 `velero.io/plugin-config` 标签的 ConfigMap

### Requirement: 取消阶段映射 (MUST)

在计算 `AppRestore` 的 `BackupRestoreStatistics` 时，控制器 (MUST) 必须将 `PhaseCancelled` 状态映射到 `Statistics.Canceled` 计数器。它 (SHALL NOT) 不得将 `PhaseCancelled` 映射到 `Statistics.Failed`。

#### Scenario: AppRestore 已取消
当 `AppRestore` 进入 `Cancelled` 阶段时，统计同步逻辑增加 `Canceled` 计数，并且 (NOT) 不增加 `Failed` 计数。

