## ADDED Requirements
### Requirement: AppBackup 状态机验证
AppBackup 控制器必须正确处理从 Pending 到 Ready 的转换过程，并能识别集群的可达性。

#### Scenario: 初始创建进入 Ready
- **WHEN** 创建一个指向有效集群的 AppBackup
- **THEN** 控制器添加 Finalizer
- **AND** 状态在成功连接集群后变为 Ready

### Requirement: 手动动作 (Manual Action) 响应
AppBackup 必须能够响应 Spec 中定义的 Action 请求，包括即时备份、重试和取消。

#### Scenario: 触发即时备份 (Backup)
- **WHEN** 设置 `Spec.Action.Type` 为 "Backup"
- **THEN** 在目标集群创建 `velerov1.Backup` 对象
- **AND** 更新 `Status.LastAction` 为当前请求

#### Scenario: 任务重试 (Retry)
- **WHEN** 设置 `Spec.Action.Type` 为 "Retry"
- **THEN** 删除旧的失败备份并重新创建同名备份

### Requirement: 备份历史与统计同步
控制器必须定期同步目标集群的备份状态到本地 CR 的 History 中，并更新统计指标。

#### Scenario: 同步 Velero 备份历史
- **WHEN** 目标集群存在管理内的备份记录
- **THEN** 同步到 `Status.History` 并更新 `Status.TotalBackups`
