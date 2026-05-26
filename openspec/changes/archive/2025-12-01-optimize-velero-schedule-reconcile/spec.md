# Spec: Optimize Velero Schedule Reconciliation

## Requirements

### Requirement: 创建后立即返回 (Return After Creation)
当控制器成功创建一个新的 `Velero Schedule` 资源后，必须立即更新 `AppBackup` 的状态并结束当前的 Reconcile 循环。

#### Scenario: 创建新 Schedule
- **Given** `AppBackup` 对应的 `Velero Schedule` 在集群中不存在
- **When** 控制器执行创建操作并成功
- **Then** 控制器应更新 `AppBackup.Status` (记录 ScheduleStatus)
- **And** 控制器应返回 `ctrl.Result{}` (或带 Requeue) 以结束本次 Reconcile

### Requirement: 按需更新 (Update On Diff)
在更新已存在的 `Velero Schedule` 之前，必须检查期望状态 (`AppBackup.Spec`) 与实际状态 (`Schedule.Spec`) 是否一致。

#### Scenario: 检查更新必要性
- **Given** `AppBackup` 对应的 `Velero Schedule` 已存在
- **When** 控制器准备同步 Spec (如 `Schedule`, `Template`, `Paused`)
- **Then** 比较关键字段是否一致
- **And** 仅当字段不一致时，才调用 `Client.Update`
- **And** 如果字段一致，则跳过更新操作，避免无用的 API 调用
