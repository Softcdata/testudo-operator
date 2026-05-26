# 规范：AppBackup 控制器

## Purpose
定义 AppBackup 的核心行为、状态机流转、周期性备份调度以及手动动作（如备份、重试、删除）的响应逻辑，确保应用数据的安全与可恢复性。
## Requirements
### Requirement: AppBackup 状态机验证
AppBackup 控制器必须 (MUST) 正确处理从 Pending 到 Ready 的转换过程，并能识别集群的可达性。

#### Scenario: 初始创建进入 Ready
- **WHEN** 创建一个指向有效集群的 AppBackup
- **THEN** 控制器添加 Finalizer
- **AND** 状态在成功连接集群后变为 Ready

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

### Requirement: 备份历史与统计同步
控制器必须 (MUST) 定期同步目标集群的备份状态到本地 CR 的 History 中，并更新统计指标。

#### Scenario: 同步 Velero 备份历史详情
- **WHEN** 目标集群存在管理内的备份记录
- **THEN** 同步到 `Status.History` 时，必须包含完整的 Velero Backup Status 信息（通过 `VeleroStatus` 字段）
- **AND** 更新 `Status.TotalBackups`

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

### Requirement: 资源清理 (Garbage Collection)
AppBackup 删除时必须 (MUST) 触发关联资源的清理。控制器必须 (MUST) 在 Velero CRD 不可用时优雅降级,避免阻塞资源删除流程。

#### Scenario: 清理统计资源
- **WHEN** AppBackup 资源被删除
- **THEN** 关联的 `BackupRestoreStatistics` 资源必须随之删除 (通过 OwnerReference)

#### Scenario: Velero CRD 可用时正常清理外部资源
- **GIVEN** 目标集群的 Velero CRD 可访问
- **WHEN** AppBackup 资源被删除
- **THEN** 控制器删除所有关联的 Velero Schedule 资源 (通过 UID Label 匹配)
- **AND** 控制器为所有关联的 Velero Backup 创建 DeleteBackupRequest
- **AND** 移除 Finalizer 并完成删除

#### Scenario: Velero CRD 不可用时跳过外部资源清理
- **GIVEN** 目标集群的 Velero CRD 不存在 (返回 `meta.NoMatchError`)
- **WHEN** AppBackup 资源被删除
- **THEN** 控制器记录 Warning Event "VeleroCRDNotFound"
- **AND** 跳过 Velero Schedule 和 Backup 的删除操作
- **AND** 移除 Finalizer 并完成删除 (不阻塞)

#### Scenario: 目标集群不可达时跳过外部资源清理
- **GIVEN** 目标集群的 Cluster 资源不存在或无法连接
- **WHEN** AppBackup 资源被删除
- **THEN** 控制器记录 Warning Event "ClusterNotFound" 或 "ClusterUnreachable"
- **AND** 跳过外部资源清理
- **AND** 移除 Finalizer 并完成删除 (不阻塞)

### Requirement: 外部资源依赖检查与安全删除
在操作外部资源（如 Velero CRD）时，必须 (MUST) 检查其可用性，并在删除流程中提供容错保护。

#### Scenario: Velero CRD 缺失时的删除保护
- **GIVEN** AppBackup 正在被删除 (DeletionTimestamp 不为空)
- **AND** 集群中未安装 Velero (CRD 不存在)
- **WHEN** 控制器执行外部资源清理 (`deleteExternalResources`)
- **THEN** 控制器应记录 Warning Event
- **AND** 跳过 Velero 资源的删除操作
- **AND** 允许移除 Finalizer，确保 AppBackup 能被正常删除

#### Scenario: Velero CRD 存在时的正常清理
- **GIVEN** AppBackup 正在被删除
- **AND** 集群中已安装 Velero
- **WHEN** 控制器执行外部资源清理
- **THEN** 控制器应删除关联的 Velero Schedule 和 Backup 资源
- **AND** 移除 Finalizer

### Requirement: Velero CRD 可用性检查
控制器在操作 Velero 资源前,必须 (MUST) 检查对应 CRD 的可用性,避免因 CRD 不存在导致的操作失败。

#### Scenario: 使用 List 操作检测 CRD 可用性
- **WHEN** 控制器需要删除 Velero Schedule 资源
- **THEN** 先执行 `client.List(&velerov1.ScheduleList{}, client.Limit(1))` 探测 CRD
- **AND** 若返回 `meta.NoMatchError`,判定 CRD 不可用
- **AND** 若返回其他错误,继续抛出错误
- **AND** 若成功,继续执行 `DeleteAllOf` 操作

#### Scenario: 记录 CRD 不可用事件
- **WHEN** 检测到 Velero CRD 不可用
- **THEN** 记录 Kubernetes Event,类型为 Warning
- **AND** Event Reason 为 "VeleroCRDNotFound"
- **AND** Event Message 包含 "Velero CRD not available, external resources may not be cleaned"

### Requirement: AppBackup 调度管理
AppBackup 控制器必须 (MUST) 管理 Velero Schedule 资源的生命周期并确保配置一致性。

#### Scenario: Schedule 更新一致性
- **Given** 一个拥有 Schedule 且 `StorageLocation` 前缀有效的 AppBackup
- **When** 该 AppBackup 的 Spec 被更新（触发 Schedule 更新）
- **Then** 关联的 Velero Schedule 的 `Template.StorageLocation` 必须 (MUST) 保留正确的集群后缀（例如 `storage-cluster1`）
- **And** 它绝不能 (MUST NOT) 回退到 AppBackup Spec 中的原始前缀

#### Scenario: Velero Schedule 默认使用北京时间
- **Given** 一个配置了 `Spec.Schedule` 且表达式未包含 `CRON_TZ=` 或 `TZ=` 前缀的 AppBackup
- **When** 控制器创建或更新关联的 Velero Schedule
- **Then** 写入 Velero Schedule 的 `spec.schedule` 必须 (MUST) 添加 `CRON_TZ=Asia/Shanghai` 前缀
- **And** AppBackup 自身保存的 `Spec.Schedule` 必须 (MUST) 保持用户输入的原始表达式
- **Given** 一个 `Spec.Schedule` 已包含 `CRON_TZ=` 或 `TZ=` 前缀的 AppBackup
- **When** 控制器创建或更新关联的 Velero Schedule
- **Then** 写入 Velero Schedule 的 `spec.schedule` 必须 (MUST) 保持该表达式不变

#### Scenario: 正确的备份类型标签
- **Given** 一个配置了 `Spec.Schedule` 的 AppBackup
- **When** 创建 Velero Schedule 时
- **Then** 该 Schedule Template 必须 (MUST) 带有标签 `testudo.softcdata.com/app-backup-type: Schedule`
- **And** 由此创建的 Backups 必须 (MUST) 继承该标签
- **When** 一个 AppBackup 没有配置 `Spec.Schedule` (手动/一次性)
- **Then** 创建的 Backups 必须 (MUST) 带有标签 `testudo.softcdata.com/app-backup-type: Manual`

### Requirement: 健壮级联删除
AppBackup 的删除操作必须 (MUST) 确保成功移除所有关联的外部资源（Velero Backups 和 Schedules），即使它们处于异常状态。

#### Scenario: 健壮的删除逻辑
- **Given** 一个正在删除中的 AppBackup
- **When** 存在关联的 Velero Backups（即使处于 `FailedValidation` 状态）
- **Then** 控制器必须 (MUST) 为它们发出 `DeleteBackupRequest`
- **And** 控制器必须 (MUST) 等待这些 Backups 从集群中被移除
- **And** 如果删除超时（例如 > 30s），控制器必须 (MUST) 强制移除 Finalizers 并直接删除 Backup CRs
- **And** 只有在所有 Backups 都消失后，AppBackup 的 Finalizer 才能 (SHALL) 被移除

### Requirement: 资源生命周期审计
AppBackup 控制器必须 (MUST) 上报资源的生命周期事件，以便外部系统（如 Server）记录变更历史。

#### Scenario: 资源创建事件
- **Given** 一个新创建的 AppBackup 资源
- **When** 控制器首次处理该资源（Pending 阶段）
- **Then** 控制器必须 (MUST) 发出一个 `Created` 类型的结构化事件
- **And** 任务名称格式应为 `Resource: AppBackup <name> Created`

#### Scenario: 资源更新事件
- **Given** 一个已存在的 AppBackup 资源
- **When** 资源的 Spec 字段（如 Schedule, Action, Template）发生变更
- **Then** 控制器必须 (MUST) 发出一个 `Updated` 类型的结构化事件
- **And** 任务名称格式应为 `Resource: AppBackup <name> Updated`

#### Scenario: 资源删除事件
- **Given** 一个正在被删除的 AppBackup 资源（DeletionTimestamp 非空）
- **When** 控制器开始执行删除逻辑（Deleting Handler）
- **Then** 控制器必须 (MUST) 发出一个 `Deleted` 类型的结构化事件
- **And** 任务名称格式应为 `Resource: AppBackup <name> Deleted`

### Requirement: 统计计算来源 (MUST)

`AppBackup` 的 `BackupRestoreStatistics` (MUST) 必须基于 `AppBackup.Status.History` 字段进行计算。这确保了所有历史备份，包括那些已从底层备份系统中删除的（例如 Canceled 备份），都能被准确统计。控制器 (SHALL NOT) 不得仅依赖于列出存在的 Velero Backup 资源来生成统计信息。

#### Scenario: 取消备份计数
当 AppBackup 拥有一条 `ManagedStatus: Canceled` 的历史记录时，统计控制器通过遍历历史记录来计算 `Canceled` 计数，而不是去查找已不存在的 Velero Backup CR。

### Requirement: 取消计数 (MUST)

`Statistics.Canceled` 计数器 (MUST) 必须包含历史记录中 `ManagedStatus` 为 `Canceled` 的任何备份记录。

#### Scenario: 增加取消计数
当用户取消正在运行的备份时，`AppBackup` 状态将历史记录更新为 `Canceled`。统计同步逻辑随后在关联的 `BackupRestoreStatistics` 资源中增加 `Canceled` 计数。
