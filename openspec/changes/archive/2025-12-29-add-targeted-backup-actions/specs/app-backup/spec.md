## MODIFIED Requirements

### Requirement: 手动动作 (Manual Action) 响应
AppBackup 必须 (MUST) 能够响应 Spec 中定义的 Action 请求，包括即时备份、重试、取消和删除。用户可以指定要操作的目标备份名称，如不指定则默认操作最新备份。

#### Scenario: 触发即时备份 (Backup)
- **WHEN** 设置 `Spec.Action.Type` 为 "Backup"
- **THEN** 在目标集群创建 `velerov1.Backup` 对象
- **AND** 更新 `Status.LastAction` 为当前请求

#### Scenario: 任务重试 (Retry) - 指定备份
- **GIVEN** `Spec.Action.TargetBackup` 指定了一个存在的失败备份名称
- **WHEN** 设置 `Spec.Action.Type` 为 "Retry"
- **THEN** 删除该指定的失败备份并重新创建同名备份

#### Scenario: 任务重试 (Retry) - 默认最新备份
- **GIVEN** `Spec.Action.TargetBackup` 为空
- **WHEN** 设置 `Spec.Action.Type` 为 "Retry"
- **THEN** 删除最新的失败备份并重新创建同名备份

#### Scenario: 取消任务 (Cancel) - 指定备份
- **GIVEN** `Spec.Action.TargetBackup` 指定了一个正在进行的备份名称
- **WHEN** 设置 `Spec.Action.Type` 为 "Cancel"
- **THEN** 取消该指定备份的执行

#### Scenario: 取消任务 (Cancel) - 默认最新备份
- **GIVEN** `Spec.Action.TargetBackup` 为空
- **WHEN** 设置 `Spec.Action.Type` 为 "Cancel"
- **THEN** 取消最新正在进行的备份

## ADDED Requirements

### Requirement: 删除指定 Velero 备份
AppBackup 必须 (MUST) 支持删除指定的 Velero 备份，以释放存储空间。

#### Scenario: 成功删除指定备份
- **GIVEN** `Spec.Action.TargetBackup` 指定了一个已完成或失败的备份名称
- **AND** 该备份存在于 `Status.History` 中
- **WHEN** 设置 `Spec.Action.Type` 为 "Delete"
- **THEN** 在目标集群删除对应的 `velerov1.Backup` 对象
- **AND** 从 `Status.History` 中移除该记录
- **AND** 更新 `Status.TotalBackups` 减 1
- **AND** 更新 `Status.LastAction` 为当前请求

#### Scenario: 删除不存在的备份返回错误
- **GIVEN** `Spec.Action.TargetBackup` 指定的备份不存在
- **WHEN** 设置 `Spec.Action.Type` 为 "Delete"
- **THEN** 更新 `Status.Reason` 为 "BackupNotFound"
- **AND** 记录 Warning Event

#### Scenario: 删除正在进行的备份应被阻止
- **GIVEN** `Spec.Action.TargetBackup` 指定的备份状态为 InProgress
- **WHEN** 设置 `Spec.Action.Type` 为 "Delete"
- **THEN** 操作被拒绝
- **AND** 更新 `Status.Reason` 为 "BackupInProgress"
- **AND** 记录 Warning Event 提示用户先取消备份
